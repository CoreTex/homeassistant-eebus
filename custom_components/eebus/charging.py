"""Wallbox charge-mode controller (PV surplus, minimum, fast, manual).

Design:

* The bridge enforces safety limits (§14a share, failsafe) on its own, so they
  keep working while Home Assistant restarts. This controller only does the
  comfort part and asks the bridge for a power (or current) per wallbox; the
  bridge applies the lower of that and any §14a share.
* Wallboxes are controlled either via OPEV (charging current per phase, for
  wallboxes that report the vehicle) or via LPC (power limit in watts, e.g. the
  KATHREIN AC40, which only offers §14a LPC). The controller works in watts and
  converts for OPEV.
* PV surplus is derived from the grid power (positive = import):
  ``available = wallbox draw - grid power - reserve``. When a wallbox does not
  report its own consumption (LPC-only boxes), the last commanded power is
  used as the estimate, which turns the loop into a simple integrating
  controller that converges within a few cycles.
* PV mode uses start/stop delays so clouds do not toggle charging every minute.

``compute_commands`` is pure and unit-tested; ``ChargeController`` wires it to
Home Assistant (options, grid sensor, timer, bridge API).
"""

from __future__ import annotations

import logging
import math
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any

from homeassistant.const import STATE_UNAVAILABLE, STATE_UNKNOWN
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.event import async_track_time_interval

from .api import EebusApiError
from .const import (
    CHARGE_MODE_AUTO,
    CHARGE_MODE_FAST,
    CHARGE_MODE_MANUAL,
    CHARGE_MODE_MIN_PV,
    CHARGE_MODE_OFF,
    CHARGE_MODE_PV,
    CONF_CONTROL_INTERVAL_S,
    CONF_GRID_POWER_ENTITY,
    CONF_GRID_POWER_INVERTED,
    CONF_MAX_CHARGE_POWER_W,
    CONF_MIN_CHARGE_POWER_W,
    CONF_START_DELAY_S,
    CONF_STOP_DELAY_S,
    CONF_SURPLUS_RESERVE_W,
    DEFAULT_CONTROL_INTERVAL_S,
    DEFAULT_MAX_CHARGE_POWER_W,
    DEFAULT_MIN_CHARGE_POWER_W,
    DEFAULT_START_DELAY_S,
    DEFAULT_STOP_DELAY_S,
    DEFAULT_SURPLUS_RESERVE_W,
    NOMINAL_PHASE_VOLTAGE,
)
from .remote import connections, device_data, get

_LOGGER = logging.getLogger(__name__)

VIA_OPEV = "opev"
VIA_LPC = "lpc"

CMD_RELEASE = "release"  # hand control back to the wallbox
CMD_FULL = "full"  # no limit / maximum current
CMD_POWER = "power"  # limit to value_w

# Changes smaller than this are not sent (avoids chatter on the bus).
MIN_CHANGE_W = 100.0


@dataclass(frozen=True)
class Settings:
    """Controller tuning (from the integration options)."""

    reserve_w: float = DEFAULT_SURPLUS_RESERVE_W
    min_power_w: float = DEFAULT_MIN_CHARGE_POWER_W
    max_power_w: float = DEFAULT_MAX_CHARGE_POWER_W
    start_delay_s: float = DEFAULT_START_DELAY_S
    stop_delay_s: float = DEFAULT_STOP_DELAY_S


@dataclass(frozen=True)
class Wallbox:
    """Controller view of one wallbox."""

    ski: str
    mode: str
    via: str
    min_w: float
    max_w: float
    phases: int = 3
    # None = unknown (LPC-only wallboxes do not report the vehicle)
    plugged: bool | None = None
    measured_w: float | None = None
    manual_w: float | None = None


@dataclass
class Runtime:
    """Per-wallbox controller memory between cycles."""

    commanded_w: float | None = None  # last power we asked for
    charging: bool = False  # PV mode state (hysteresis)
    above_since: float | None = None
    below_since: float | None = None


@dataclass(frozen=True)
class Command:
    """What to tell the bridge for one wallbox."""

    kind: str
    value_w: float = 0.0


def _clamp(value: float, low: float, high: float) -> float:
    return max(low, min(high, value))


def available_surplus(
    wallboxes: list[Wallbox],
    runtime: dict[str, Runtime],
    grid_power_w: float | None,
    reserve_w: float,
) -> float | None:
    """Power available to the PV-mode wallboxes (W), or None if unknown."""
    if grid_power_w is None:
        return None
    draw = 0.0
    for wb in wallboxes:
        if wb.measured_w is not None:
            draw += max(0.0, wb.measured_w)
        else:
            draw += runtime.get(wb.ski, Runtime()).commanded_w or 0.0
    return draw - grid_power_w - reserve_w


def compute_commands(
    wallboxes: list[Wallbox],
    grid_power_w: float | None,
    settings: Settings,
    runtime: dict[str, Runtime],
    now: float,
) -> dict[str, Command]:
    """Decide a command per wallbox and update ``runtime`` (hysteresis)."""
    pv_boxes = [
        wb
        for wb in wallboxes
        if wb.mode in (CHARGE_MODE_PV, CHARGE_MODE_MIN_PV) and wb.plugged is not False
    ]
    surplus = available_surplus(pv_boxes, runtime, grid_power_w, settings.reserve_w)
    share = surplus / len(pv_boxes) if (surplus is not None and pv_boxes) else None

    commands: dict[str, Command] = {}
    for wb in wallboxes:
        rt = runtime.setdefault(wb.ski, Runtime())
        if wb.mode == CHARGE_MODE_AUTO:
            commands[wb.ski] = Command(CMD_RELEASE)
            rt.commanded_w, rt.charging = None, False
            continue
        parked = wb.mode in (CHARGE_MODE_PV, CHARGE_MODE_MIN_PV) and wb.plugged is False
        if wb.mode == CHARGE_MODE_OFF or parked:
            # Off, or a PV mode without a vehicle: hold at 0 W so a car that is
            # plugged in does not start at full power before the next cycle.
            commands[wb.ski] = Command(CMD_POWER, 0.0)
            rt.commanded_w, rt.charging = 0.0, False
            rt.above_since = rt.below_since = None
            continue
        if wb.mode == CHARGE_MODE_FAST:
            commands[wb.ski] = Command(CMD_FULL, wb.max_w)
            rt.commanded_w = wb.max_w
            continue
        if wb.mode == CHARGE_MODE_MANUAL:
            value = _clamp(wb.manual_w if wb.manual_w is not None else wb.min_w, 0.0, wb.max_w)
            commands[wb.ski] = Command(CMD_POWER, value)
            rt.commanded_w = value
            continue

        # PV modes
        if wb.mode == CHARGE_MODE_MIN_PV:
            target = _clamp(max(share or 0.0, wb.min_w), wb.min_w, wb.max_w)
            rt.charging = True
        else:  # CHARGE_MODE_PV
            target = _pv_target(wb, share, settings, rt, now)
        target = _round_target(target, wb.min_w)
        commands[wb.ski] = Command(CMD_POWER, target)
        rt.commanded_w = target
    return commands


def _round_target(target: float, min_w: float) -> float:
    """Round down to 50 W steps (less bus chatter) without ever dropping a
    charging target below the wallbox minimum (it would stop charging)."""
    if target <= 0:
        return 0.0
    return max(math.floor(target / 50.0) * 50.0, min_w)


def _pv_target(wb: Wallbox, share: float | None, settings: Settings, rt: Runtime, now: float) -> float:
    """PV-only mode with start/stop delays."""
    if share is None:
        # Without a grid measurement we cannot know the surplus: do not charge.
        rt.charging, rt.above_since, rt.below_since = False, None, None
        return 0.0
    enough = share >= wb.min_w
    if not rt.charging:
        rt.below_since = None
        if not enough:
            rt.above_since = None
            return 0.0
        if rt.above_since is None:
            rt.above_since = now
        if now - rt.above_since < settings.start_delay_s:
            return 0.0
        rt.charging, rt.above_since = True, None
        return _clamp(share, wb.min_w, wb.max_w)
    # charging
    rt.above_since = None
    if enough:
        rt.below_since = None
        return _clamp(share, wb.min_w, wb.max_w)
    if rt.below_since is None:
        rt.below_since = now
    if now - rt.below_since < settings.stop_delay_s:
        return wb.min_w  # ride through short dips at minimum power
    rt.charging, rt.below_since = False, None
    return 0.0


def power_to_current(power_w: float, phases: int) -> float:
    """Whole amperes per phase for a power (never negative)."""
    phases = phases if phases > 0 else 3
    return max(0.0, math.floor(power_w / (NOMINAL_PHASE_VOLTAGE * phases)))


# --- Home Assistant wiring ---


def wallbox_control_path(dev: dict[str, Any]) -> str | None:
    """How the bridge can control this device's charging, if at all."""
    if dev.get("category") != "wallbox":
        return None
    if get(dev, "ev", "control_supported"):
        return VIA_OPEV
    if get(dev, "control", "lpc_supported"):
        return VIA_LPC
    return None


@dataclass
class ChargeController:
    """Runs the charge modes for all wallboxes of one bridge."""

    hass: HomeAssistant
    coordinator: Any
    modes: dict[str, str] = field(default_factory=dict)
    manual_w: dict[str, float] = field(default_factory=dict)
    runtime: dict[str, Runtime] = field(default_factory=dict)
    last_sent: dict[str, Command] = field(default_factory=dict)
    surplus_w: float | None = None
    # SKIs whose next command is sent even if unchanged (after a mode change).
    force: set[str] = field(default_factory=set)
    _unsub: Callable[[], None] | None = None
    _listeners: list[Callable[[], None]] = field(default_factory=list)

    # --- lifecycle ---

    @callback
    def async_start(self) -> None:
        interval = self._option(CONF_CONTROL_INTERVAL_S, DEFAULT_CONTROL_INTERVAL_S)
        self._unsub = async_track_time_interval(
            self.hass, self._async_tick, timedelta(seconds=max(10, interval))
        )

    @callback
    def async_stop(self) -> None:
        if self._unsub:
            self._unsub()
            self._unsub = None

    @callback
    def async_add_listener(self, cb: Callable[[], None]) -> Callable[[], None]:
        self._listeners.append(cb)
        return lambda: self._listeners.remove(cb)

    # --- state set by entities ---

    async def async_set_mode(self, ski: str, mode: str, *, run: bool = True) -> None:
        self.modes[ski] = mode
        self.runtime.pop(ski, None)  # restart hysteresis
        # Keep last_sent (it records that we control this wallbox, so "auto"
        # can release it) but send the next command unconditionally.
        self.force.add(ski)
        if run:
            await self._async_tick()

    async def async_set_manual(self, ski: str, value_w: float, *, run: bool = True) -> None:
        self.manual_w[ski] = value_w
        if run and self.modes.get(ski) == CHARGE_MODE_MANUAL:
            await self._async_tick()

    # --- inputs ---

    def _option(self, key: str, default: Any) -> Any:
        return self.coordinator.entry.options.get(key, default)

    def _settings(self) -> Settings:
        return Settings(
            reserve_w=float(self._option(CONF_SURPLUS_RESERVE_W, DEFAULT_SURPLUS_RESERVE_W)),
            min_power_w=float(self._option(CONF_MIN_CHARGE_POWER_W, DEFAULT_MIN_CHARGE_POWER_W)),
            max_power_w=float(self._option(CONF_MAX_CHARGE_POWER_W, DEFAULT_MAX_CHARGE_POWER_W)),
            start_delay_s=float(self._option(CONF_START_DELAY_S, DEFAULT_START_DELAY_S)),
            stop_delay_s=float(self._option(CONF_STOP_DELAY_S, DEFAULT_STOP_DELAY_S)),
        )

    def grid_power_w(self) -> float | None:
        """Grid power (W, positive = import) from the option sensor or the bridge."""
        entity_id = self._option(CONF_GRID_POWER_ENTITY, None)
        if entity_id:
            state = self.hass.states.get(entity_id)
            if state is None or state.state in (STATE_UNAVAILABLE, STATE_UNKNOWN):
                return None
            try:
                value = float(state.state)
            except ValueError:
                return None
            unit = state.attributes.get("unit_of_measurement")
            if unit == "kW":
                value *= 1000.0
            return -value if self._option(CONF_GRID_POWER_INVERTED, False) else value
        value = get(self.coordinator.data, "site", "grid_power_w")
        return float(value) if value is not None else None

    def wallboxes(self) -> list[Wallbox]:
        data = self.coordinator.data or {}
        settings = self._settings()
        result: list[Wallbox] = []
        for ski in connections(data):
            dev = device_data(data, ski)
            via = wallbox_control_path(dev)
            if via is None:
                continue
            ev = dev.get("ev") or {}
            phases = int(ev.get("phases_connected") or 3)
            min_w, max_w = settings.min_power_w, settings.max_power_w
            if via == VIA_OPEV:
                if ev.get("min_current_a"):
                    min_w = ev["min_current_a"] * NOMINAL_PHASE_VOLTAGE * phases
                if ev.get("max_current_a"):
                    max_w = ev["max_current_a"] * NOMINAL_PHASE_VOLTAGE * phases
                plugged: bool | None = bool(ev.get("connected"))
            else:
                nominal = get(dev, "control", "lpc_nominal_max_w")
                if nominal:
                    max_w = float(nominal)
                plugged = None
            measured = ev.get("power_w")
            if measured is None:
                measured = get(dev, "mpc", "power_w")
            result.append(
                Wallbox(
                    ski=ski,
                    mode=self.modes.get(ski, CHARGE_MODE_AUTO),
                    via=via,
                    min_w=min(min_w, max_w),
                    max_w=max_w,
                    phases=phases,
                    plugged=plugged,
                    measured_w=measured,
                    manual_w=self.manual_w.get(ski),
                )
            )
        return result

    # --- cycle ---

    async def _async_tick(self, _now: Any = None) -> None:
        wallboxes = self.wallboxes()
        grid = self.grid_power_w()
        pv_boxes = [w for w in wallboxes if w.mode in (CHARGE_MODE_PV, CHARGE_MODE_MIN_PV)]
        self.surplus_w = available_surplus(
            pv_boxes or wallboxes, self.runtime, grid, self._settings().reserve_w
        )
        commands = compute_commands(wallboxes, grid, self._settings(), self.runtime, time.monotonic())
        by_ski = {w.ski: w for w in wallboxes}
        for ski, cmd in commands.items():
            if not self._changed(ski, cmd):
                continue
            try:
                await self._send(by_ski[ski], cmd)
            except EebusApiError as err:
                _LOGGER.warning("charge control for %s failed: %s", ski, err)
                continue
            self.last_sent[ski] = cmd
        for cb in list(self._listeners):
            cb()

    def _changed(self, ski: str, cmd: Command) -> bool:
        last = self.last_sent.get(ski)
        forced = ski in self.force
        self.force.discard(ski)
        if last is None or last.kind == CMD_RELEASE:
            # Never "release" a wallbox we do not control (not in this run, or
            # already released).
            return cmd.kind != CMD_RELEASE
        if forced or last.kind != cmd.kind:
            return True
        return cmd.kind == CMD_POWER and abs(last.value_w - cmd.value_w) >= MIN_CHANGE_W

    async def _send(self, wb: Wallbox, cmd: Command) -> None:
        client = self.coordinator.client
        _LOGGER.debug("charge control %s (%s, %s): %s", wb.ski, wb.mode, wb.via, cmd)
        if wb.via == VIA_OPEV:
            if cmd.kind == CMD_RELEASE:
                await client.set_ev_current(wb.ski, None)
            else:
                await client.set_ev_current(wb.ski, power_to_current(cmd.value_w, wb.phases))
            return
        # LPC: a power limit; "full" and "release" both lift the limit.
        if cmd.kind in (CMD_RELEASE, CMD_FULL):
            await client.set_device_limit(wb.ski, active=False)
        else:
            await client.set_device_limit(wb.ski, target_w=cmd.value_w, active=True, duration_s=0)
