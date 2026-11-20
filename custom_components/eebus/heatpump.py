"""Heat pump flexibility (OHPCF): use PV surplus for optional consumption.

A heat pump compressor that supports OHPCF announces an *optional*
consumption ("I could heat or make hot water now, about X W"). The bridge
reports the offer and executes start/pause/resume/stop; this module decides
when, if "Use PV surplus" is switched on for the heat pump:

* **start** once the surplus covers the requested power for the start delay,
* **pause** (if the heat pump allows it) once less than half of its
  consumption is covered by surplus for the stop delay, but not before the
  minimum run time,
* **resume** once the surplus is back for the start delay, but not before the
  minimum pause time.

It never stops a process on its own: stopping ends the offer.

The surplus uses the same grid sensor, reserve and delays as PV-surplus
charging. ``decide`` is pure and unit-tested; ``FlexController`` wires it to
Home Assistant.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import timedelta
from typing import Any

from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.event import async_track_time_interval

from .api import EebusApiError
from .const import CONF_CONTROL_INTERVAL_S, DEFAULT_CONTROL_INTERVAL_S
from .remote import connections, device_data, get

_LOGGER = logging.getLogger(__name__)

START, PAUSE, RESUME = "start", "pause", "resume"

# Used when the heat pump does not say how much it wants.
DEFAULT_REQUEST_W = 1000.0


@dataclass
class FlexRuntime:
    """Hysteresis timers of one heat pump (monotonic seconds)."""

    above_since: float | None = None
    below_since: float | None = None
    running_since: float | None = None
    paused_since: float | None = None
    last_state: str | None = None


@dataclass(frozen=True)
class FlexInput:
    """What the controller knows about one heat pump."""

    state: str  # available, scheduled, running, paused, completed, stopped, ""
    requested_w: float
    measured_w: float | None
    pausable: bool
    min_run_s: float
    min_pause_s: float


def requested_power(flex: dict[str, Any]) -> float:
    """Expected consumption of the offer (W)."""
    for key in ("power_w", "max_power_w"):
        value = flex.get(key)
        if value:
            return float(value)
    return DEFAULT_REQUEST_W


def decide(
    hp: FlexInput,
    grid_power_w: float | None,
    reserve_w: float,
    start_delay_s: float,
    stop_delay_s: float,
    rt: FlexRuntime,
    now: float,
) -> str | None:
    """Return START, PAUSE, RESUME or None and update ``rt``."""
    if hp.state != rt.last_state:
        # track transitions also when somebody else (app, heat pump) acts
        if hp.state == "running":
            rt.running_since = now
        elif hp.state == "paused":
            rt.paused_since = now
        rt.above_since = rt.below_since = None
        rt.last_state = hp.state
    if grid_power_w is None:
        return None

    # Surplus without the heat pump's own draw (it is part of the grid power).
    own = max(0.0, hp.measured_w or 0.0) if hp.state == "running" else 0.0
    surplus = own - grid_power_w - reserve_w

    if hp.state in ("available", "paused"):
        rt.below_since = None
        if surplus < hp.requested_w:
            rt.above_since = None
            return None
        if rt.above_since is None:
            rt.above_since = now
        if now - rt.above_since < start_delay_s:
            return None
        if hp.state == "paused":
            if rt.paused_since is not None and now - rt.paused_since < hp.min_pause_s:
                return None
            return RESUME
        return START

    if hp.state == "running" and hp.pausable:
        rt.above_since = None
        # keep running while surplus covers at least half of its consumption
        if surplus >= 0.5 * own:
            rt.below_since = None
            return None
        if rt.below_since is None:
            rt.below_since = now
        if now - rt.below_since < stop_delay_s:
            return None
        if rt.running_since is not None and now - rt.running_since < hp.min_run_s:
            return None
        return PAUSE
    return None


@dataclass
class FlexController:
    """Runs "Use PV surplus" for all heat pumps of one bridge."""

    hass: HomeAssistant
    coordinator: Any
    enabled: dict[str, bool] = field(default_factory=dict)
    runtime: dict[str, FlexRuntime] = field(default_factory=dict)
    _unsub: Callable[[], None] | None = None

    @callback
    def async_start(self) -> None:
        interval = self.coordinator.entry.options.get(
            CONF_CONTROL_INTERVAL_S, DEFAULT_CONTROL_INTERVAL_S
        )
        self._unsub = async_track_time_interval(
            self.hass, self._async_tick, timedelta(seconds=max(10, interval))
        )

    @callback
    def async_stop(self) -> None:
        if self._unsub:
            self._unsub()
            self._unsub = None

    async def async_set_enabled(self, ski: str, on: bool, *, run: bool = True) -> None:
        self.enabled[ski] = on
        self.runtime.pop(ski, None)
        if run and on:
            await self._async_tick()

    def heat_pumps(self) -> dict[str, FlexInput]:
        data = self.coordinator.data or {}
        result: dict[str, FlexInput] = {}
        for ski in connections(data):
            dev = device_data(data, ski)
            flex = dev.get("flex")
            if not flex or not flex.get("supported"):
                continue
            result[ski] = FlexInput(
                state=flex.get("state") or "",
                requested_w=requested_power(flex),
                measured_w=get(dev, "mpc", "power_w"),
                pausable=bool(flex.get("pausable")),
                min_run_s=float(flex.get("min_run_s") or 0),
                min_pause_s=float(flex.get("min_pause_s") or 0),
            )
        return result

    async def _async_tick(self, _now: Any = None) -> None:
        charge = self.coordinator.charge_controller
        if charge is None:
            return
        settings = charge._settings()  # noqa: SLF001 - shared tuning
        grid = charge.grid_power_w()
        now = time.monotonic()
        for ski, hp in self.heat_pumps().items():
            if not self.enabled.get(ski):
                continue
            rt = self.runtime.setdefault(ski, FlexRuntime())
            action = decide(
                hp, grid, settings.reserve_w, settings.start_delay_s,
                settings.stop_delay_s, rt, now,
            )
            if action is None:
                continue
            _LOGGER.debug("heat pump %s: %s (grid %s W)", ski, action, grid)
            try:
                await self.coordinator.client.set_flex(ski, action)
            except EebusApiError as err:
                _LOGGER.warning("heat pump %s: %s failed: %s", ski, action, err)
                continue
            # wait for the heat pump to confirm before acting again
            rt.above_since = rt.below_since = None
