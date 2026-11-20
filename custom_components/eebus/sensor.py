"""Sensor platform for EEBUS."""

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass, replace
from typing import Any

from homeassistant.components.sensor import (
    RestoreSensor,
    SensorDeviceClass,
    SensorEntity,
    SensorEntityDescription,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import (
    PERCENTAGE,
    EntityCategory,
    UnitOfElectricCurrent,
    UnitOfElectricPotential,
    UnitOfEnergy,
    UnitOfFrequency,
    UnitOfPower,
    UnitOfTemperature,
    UnitOfTime,
)
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
from .remote import EebusRemoteEntity, async_track_remotes


def _get(data: dict[str, Any], *path: str) -> Any:
    cur: Any = data
    for key in path:
        if not isinstance(cur, dict):
            return None
        cur = cur.get(key)
    return cur


def _phase(path: tuple[str, ...], index: int) -> Callable[[dict[str, Any]], Any]:
    def _fn(data: dict[str, Any]) -> Any:
        value = _get(data, *path)
        if isinstance(value, list) and len(value) > index:
            return value[index]
        return None

    return _fn


@dataclass(frozen=True, kw_only=True)
class EebusSensorEntityDescription(SensorEntityDescription):
    """Sensor description with a value extractor and availability gate."""

    value_fn: Callable[[dict[str, Any]], Any]
    available_fn: Callable[[dict[str, Any]], bool] = lambda _data: True


SENSORS: tuple[EebusSensorEntityDescription, ...] = (
    # --- LPC (received consumption limit) ---
    EebusSensorEntityDescription(
        key="consumption_limit",
        translation_key="consumption_limit",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "lpc", "limit", "value_w"),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusSensorEntityDescription(
        key="consumption_limit_duration",
        translation_key="consumption_limit_duration",
        device_class=SensorDeviceClass.DURATION,
        native_unit_of_measurement=UnitOfTime.SECONDS,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "lpc", "limit", "duration_s"),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusSensorEntityDescription(
        key="consumption_nominal_max",
        translation_key="consumption_nominal_max",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "lpc", "nominal_max_w"),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    # --- LPP (received production limit) ---
    EebusSensorEntityDescription(
        key="production_limit",
        translation_key="production_limit",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "lpp", "limit", "value_w"),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    EebusSensorEntityDescription(
        key="production_limit_duration",
        translation_key="production_limit_duration",
        device_class=SensorDeviceClass.DURATION,
        native_unit_of_measurement=UnitOfTime.SECONDS,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "lpp", "limit", "duration_s"),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    # --- MPC ---
    EebusSensorEntityDescription(
        key="active_power",
        translation_key="active_power",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "mpc", "power_w"),
        available_fn=lambda d: bool(_get(d, "mpc", "supported")),
    ),
    EebusSensorEntityDescription(
        key="energy_consumed",
        translation_key="energy_consumed",
        device_class=SensorDeviceClass.ENERGY,
        native_unit_of_measurement=UnitOfEnergy.WATT_HOUR,
        state_class=SensorStateClass.TOTAL_INCREASING,
        value_fn=lambda d: _get(d, "mpc", "energy_consumed_wh"),
        available_fn=lambda d: bool(_get(d, "mpc", "supported")),
    ),
    EebusSensorEntityDescription(
        key="energy_produced",
        translation_key="energy_produced",
        device_class=SensorDeviceClass.ENERGY,
        native_unit_of_measurement=UnitOfEnergy.WATT_HOUR,
        state_class=SensorStateClass.TOTAL_INCREASING,
        value_fn=lambda d: _get(d, "mpc", "energy_produced_wh"),
        available_fn=lambda d: bool(_get(d, "mpc", "supported")),
    ),
    EebusSensorEntityDescription(
        key="frequency",
        translation_key="frequency",
        device_class=SensorDeviceClass.FREQUENCY,
        native_unit_of_measurement=UnitOfFrequency.HERTZ,
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "mpc", "frequency_hz"),
        available_fn=lambda d: bool(_get(d, "mpc", "supported")),
    ),
    # --- Inverter / PV (VAPD ~ MOI) ---
    EebusSensorEntityDescription(
        key="inverter_power",
        translation_key="inverter_power",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "inverter", "power_w"),
        available_fn=lambda d: bool(_get(d, "inverter", "supported")),
    ),
    EebusSensorEntityDescription(
        key="inverter_pv_yield_total",
        translation_key="inverter_pv_yield_total",
        device_class=SensorDeviceClass.ENERGY,
        native_unit_of_measurement=UnitOfEnergy.WATT_HOUR,
        state_class=SensorStateClass.TOTAL_INCREASING,
        value_fn=lambda d: _get(d, "inverter", "pv_yield_total_wh"),
        available_fn=lambda d: bool(_get(d, "inverter", "supported")),
    ),
    EebusSensorEntityDescription(
        key="inverter_nominal_peak",
        translation_key="inverter_nominal_peak",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "inverter", "power_nominal_peak_w"),
        available_fn=lambda d: bool(_get(d, "inverter", "supported")),
    ),
    # --- Battery (VABD ~ MOB) ---
    EebusSensorEntityDescription(
        key="battery_power",
        translation_key="battery_power",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "battery", "power_w"),
        available_fn=lambda d: bool(_get(d, "battery", "supported")),
    ),
    EebusSensorEntityDescription(
        key="battery_soc",
        translation_key="battery_soc",
        device_class=SensorDeviceClass.BATTERY,
        native_unit_of_measurement=PERCENTAGE,
        state_class=SensorStateClass.MEASUREMENT,
        value_fn=lambda d: _get(d, "battery", "state_of_charge_pct"),
        available_fn=lambda d: bool(_get(d, "battery", "supported")),
    ),
    EebusSensorEntityDescription(
        key="battery_energy_charged",
        translation_key="battery_energy_charged",
        device_class=SensorDeviceClass.ENERGY,
        native_unit_of_measurement=UnitOfEnergy.WATT_HOUR,
        state_class=SensorStateClass.TOTAL_INCREASING,
        value_fn=lambda d: _get(d, "battery", "energy_charged_wh"),
        available_fn=lambda d: bool(_get(d, "battery", "supported")),
    ),
    EebusSensorEntityDescription(
        key="battery_energy_discharged",
        translation_key="battery_energy_discharged",
        device_class=SensorDeviceClass.ENERGY,
        native_unit_of_measurement=UnitOfEnergy.WATT_HOUR,
        state_class=SensorStateClass.TOTAL_INCREASING,
        value_fn=lambda d: _get(d, "battery", "energy_discharged_wh"),
        available_fn=lambda d: bool(_get(d, "battery", "supported")),
    ),
)

# Per-phase sensors (current, voltage, power) generated for L1..L3.
_PHASE_DEFS = (
    ("power", "power_per_phase_w", SensorDeviceClass.POWER, UnitOfPower.WATT),
    ("current", "current_per_phase_a", SensorDeviceClass.CURRENT, UnitOfElectricCurrent.AMPERE),
    ("voltage", "voltage_per_phase_v", SensorDeviceClass.VOLTAGE, UnitOfElectricPotential.VOLT),
)


def _build_phase_sensors() -> list[EebusSensorEntityDescription]:
    sensors: list[EebusSensorEntityDescription] = []
    for prefix, field, device_class, unit in _PHASE_DEFS:
        for i in range(3):
            sensors.append(
                EebusSensorEntityDescription(
                    key=f"{prefix}_l{i + 1}",
                    translation_key=f"{prefix}_l{i + 1}",
                    device_class=device_class,
                    native_unit_of_measurement=unit,
                    state_class=SensorStateClass.MEASUREMENT,
                    entity_category=EntityCategory.DIAGNOSTIC,
                    value_fn=_phase(("mpc", field), i),
                    available_fn=lambda d: bool(_get(d, "mpc", "supported")),
                )
            )
    return sensors


# Bridge-level sensors that mirror the leading inverter. The same values are
# available on the inverter's own device, so they are disabled by default for
# new installations (kept for existing dashboards and automations).
LEADING_DUPLICATES = {
    "active_power",
    "energy_consumed",
    "energy_produced",
    "frequency",
    "inverter_power",
    "inverter_pv_yield_total",
    "inverter_nominal_peak",
    "battery_power",
    "battery_soc",
    "battery_energy_charged",
    "battery_energy_discharged",
    # same value as the "Nominal consumption power" number
    "consumption_nominal_max",
}
_PHASE_PREFIXES = ("power_l", "current_l", "voltage_l")


def _site(key: str, field: str, category: EntityCategory | None = None) -> EebusSensorEntityDescription:
    return EebusSensorEntityDescription(
        key=key,
        translation_key=key,
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=category,
        value_fn=lambda d: _get(d, "site", field),
    )


# Whole-home aggregates across all devices (sum of all inverters etc.).
SITE_SENSORS: tuple[EebusSensorEntityDescription, ...] = (
    _site("site_pv_power", "pv_power_w"),
    _site("site_battery_power", "battery_power_w"),
    _site("site_inverter_ac_power", "inverter_ac_power_w"),
    _site("site_grid_power", "grid_power_w"),
    _site("site_ev_charging_power", "ev_charging_power_w"),
    _site("site_heat_pump_power", "heat_pump_power_w"),
    EebusSensorEntityDescription(
        key="lpc_distribution_share",
        translation_key="lpc_distribution_share",
        device_class=SensorDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: _get(d, "distribution", "share_w") if _get(d, "distribution", "active") else None,
        available_fn=lambda d: bool(_get(d, "distribution", "enabled")),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS sensors."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    descriptions = [
        replace(d, entity_registry_enabled_default=False)
        if d.key in LEADING_DUPLICATES or d.key.startswith(_PHASE_PREFIXES)
        else d
        for d in list(SENSORS) + list(SITE_SENSORS) + _build_phase_sensors()
    ]
    async_add_entities(
        [EebusSensor(coordinator, description) for description in descriptions]
        + [EebusSurplusSensor(coordinator)]
    )
    async_track_remotes(
        coordinator,
        async_add_entities,
        lambda coord, ski, dev: [
            EebusRemoteSensor(coord, ski, description)
            for description in REMOTE_SENSORS
            if description.available_fn(dev)
        ]
        + ([EebusCalculatedEnergySensor(coord, ski)] if needs_calculated_energy(dev) else []),
    )


class EebusSensor(EebusEntity, SensorEntity):
    """A single EEBUS sensor."""

    entity_description: EebusSensorEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        description: EebusSensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> Any:
        return self.entity_description.value_fn(self.data)

    @property
    def available(self) -> bool:
        if not super().available:
            return False
        if not self.entity_description.available_fn(self.data):
            return False
        # Report sensors without a value as "unavailable" instead of "unknown".
        # This hides monitoring sensors for use cases the paired device does not
        # actually provide (e.g. a Kostal inverter that exposes MPC but not the
        # VABD/VAPD battery/PV data), while they appear automatically as soon as
        # real data arrives.
        return self.native_value is not None


def _ev_state(dev: dict[str, Any]) -> str | None:
    value = _get(dev, "ev", "charge_state")
    return value.lower() if isinstance(value, str) and value else None


FLEX_STATES = ["none", "available", "scheduled", "running", "paused", "completed", "stopped"]

EV_CHARGE_STATES = ["unknown", "unplugged", "error", "paused", "active", "finished"]


def _remote(
    key: str,
    block: str,
    field: str,
    *,
    device_class: SensorDeviceClass | None = None,
    unit: str | None = None,
    state_class: SensorStateClass | None = SensorStateClass.MEASUREMENT,
    category: EntityCategory | None = None,
    unless_use_case: str | None = None,
    when_block: bool = False,
) -> EebusSensorEntityDescription:
    """Per-device sensor, created once the device reports a value for it.

    Devices often announce measurements they never fill (e.g. a heat pump
    announcing phase values and energy counters, but only sending its
    power), so the entity is only created once a value arrives. With
    ``when_block`` it already exists once the device reports ``block``.

    ``unless_use_case`` suppresses it for devices announcing that use case
    (e.g. MPC power/energy duplicates the MOI values on inverters).
    """

    def _create(d: dict[str, Any]) -> bool:
        if d.get(block) is None:
            return False
        if unless_use_case is not None and unless_use_case in (d.get("use_cases") or []):
            return False
        return when_block or _get(d, block, field) is not None

    return EebusSensorEntityDescription(
        key=key,
        translation_key=key,
        device_class=device_class,
        native_unit_of_measurement=unit,
        state_class=state_class,
        entity_category=category,
        value_fn=lambda d: _get(d, block, field),
        available_fn=_create,
    )


_P, _E, _TI = SensorDeviceClass.POWER, SensorDeviceClass.ENERGY, SensorStateClass.TOTAL_INCREASING
_W, _WH = UnitOfPower.WATT, UnitOfEnergy.WATT_HOUR
_A = UnitOfElectricCurrent.AMPERE
_DIAG = EntityCategory.DIAGNOSTIC

# Per-device sensors, created by capability (available_fn = "device reports
# this block"). Each only shows a value once the device actually sends it.
REMOTE_SENSORS: tuple[EebusSensorEntityDescription, ...] = (
    # MPC - any monitored appliance (inverter AC side, heat pump, wallbox, ...)
    _remote("remote_power", "mpc", "power_w", device_class=_P, unit=_W, unless_use_case="moi"),
    _remote("remote_energy_consumed", "mpc", "energy_consumed_wh", device_class=_E, unit=_WH, state_class=_TI, unless_use_case="moi"),
    _remote("remote_energy_produced", "mpc", "energy_produced_wh", device_class=_E, unit=_WH, state_class=_TI, unless_use_case="moi"),
    # PV (VAPD)
    _remote("remote_pv_power", "inverter", "power_w", device_class=_P, unit=_W),
    _remote("remote_pv_yield_total", "inverter", "pv_yield_total_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_pv_nominal_peak", "inverter", "power_nominal_peak_w", device_class=_P, unit=_W, category=_DIAG),
    # Battery (VABD)
    _remote("remote_battery_power", "battery", "power_w", device_class=_P, unit=_W),
    _remote("remote_battery_soc", "battery", "state_of_charge_pct", device_class=SensorDeviceClass.BATTERY, unit=PERCENTAGE),
    _remote("remote_battery_energy_charged", "battery", "energy_charged_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_battery_energy_discharged", "battery", "energy_discharged_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_battery_soh", "battery", "state_of_health_pct", unit=PERCENTAGE, category=_DIAG),
    _remote("remote_battery_energy_stored", "battery", "state_of_energy_wh", device_class=SensorDeviceClass.ENERGY_STORAGE, unit=_WH),
    _remote("remote_battery_capacity", "battery", "usable_capacity_wh", device_class=SensorDeviceClass.ENERGY_STORAGE, unit=_WH, category=_DIAG),
    _remote("remote_battery_cycles", "battery", "cycle_count", state_class=SensorStateClass.TOTAL_INCREASING, category=_DIAG),
    _remote("remote_battery_voltage", "battery", "voltage_v", device_class=SensorDeviceClass.VOLTAGE, unit=UnitOfElectricPotential.VOLT, category=_DIAG),
    _remote("remote_battery_current", "battery", "current_a", device_class=SensorDeviceClass.CURRENT, unit=_A, category=_DIAG),
    _remote("remote_battery_temperature", "battery", "temperature_c", device_class=SensorDeviceClass.TEMPERATURE, unit=UnitOfTemperature.CELSIUS, category=_DIAG),
    # Inverter AC side (MOI): positive = feeding the AC side
    _remote("remote_inverter_ac_power", "inverter_ac", "power_w", device_class=_P, unit=_W),
    _remote("remote_inverter_energy_produced", "inverter_ac", "energy_produced_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_inverter_energy_consumed", "inverter_ac", "energy_consumed_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_inverter_yield_total", "inverter_ac", "yield_total_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_inverter_frequency", "inverter_ac", "frequency_hz", device_class=SensorDeviceClass.FREQUENCY, unit=UnitOfFrequency.HERTZ, category=_DIAG),
    # Grid connection point (MGCP): positive = import
    _remote("remote_grid_power", "grid", "power_w", device_class=_P, unit=_W),
    _remote("remote_grid_energy_feed_in", "grid", "energy_feed_in_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_grid_energy_consumed", "grid", "energy_consumed_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("remote_grid_limitation_factor", "grid", "power_limitation_factor", unit=PERCENTAGE, category=_DIAG),
    _remote("remote_grid_frequency", "grid", "frequency_hz", device_class=SensorDeviceClass.FREQUENCY, unit=UnitOfFrequency.HERTZ, category=_DIAG),
    # Wallbox / vehicle
    _remote("ev_charging_power", "ev", "power_w", device_class=_P, unit=_W),
    _remote("ev_energy_charged", "ev", "energy_charged_wh", device_class=_E, unit=_WH, state_class=_TI),
    _remote("ev_soc", "ev", "state_of_charge_pct", device_class=SensorDeviceClass.BATTERY, unit=PERCENTAGE),
    _remote("ev_applied_current", "ev", "applied_current_a", device_class=SensorDeviceClass.CURRENT, unit=_A),
    _remote("ev_min_current", "ev", "min_current_a", device_class=SensorDeviceClass.CURRENT, unit=_A, category=_DIAG),
    _remote("ev_max_current", "ev", "max_current_a", device_class=SensorDeviceClass.CURRENT, unit=_A, category=_DIAG),
    _remote("ev_communication_standard", "ev", "communication_standard", state_class=None, category=_DIAG),
    _remote("ev_evse_state", "ev", "evse_operating_state", state_class=None, category=_DIAG),
    EebusSensorEntityDescription(
        key="ev_charge_state",
        translation_key="ev_charge_state",
        device_class=SensorDeviceClass.ENUM,
        options=EV_CHARGE_STATES,
        value_fn=_ev_state,
        available_fn=lambda d: d.get("ev") is not None,
    ),
    # How the bridge classifies the device (diagnostics)
    EebusSensorEntityDescription(
        key="remote_category",
        translation_key="remote_category",
        device_class=SensorDeviceClass.ENUM,
        options=["inverter", "wallbox", "heat_pump", "grid_meter", "control_box", "generic"],
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: d.get("category") or None,
        available_fn=lambda d: bool(d.get("category")),
    ),
    # Limits the bridge applies to this device
    # values also on the "... limited" binary sensors (attribute limit_w)
    replace(_remote("lpc_applied", "control", "lpc_applied_w", device_class=_P, unit=_W, when_block=True), entity_registry_enabled_default=False),
    replace(_remote("lpp_applied", "control", "lpp_applied_w", device_class=_P, unit=_W, when_block=True), entity_registry_enabled_default=False),
    _remote("lpc_distributed", "control", "distributed_limit_w", device_class=_P, unit=_W, category=_DIAG, when_block=True),
    # Heat pump flexibility (OHPCF)
    EebusSensorEntityDescription(
        key="flex_state",
        translation_key="flex_state",
        device_class=SensorDeviceClass.ENUM,
        options=FLEX_STATES,
        value_fn=lambda d: _get(d, "flex", "state") or "none",
        available_fn=lambda d: bool(_get(d, "flex", "supported")),
    ),
    _remote("flex_power", "flex", "power_w", device_class=_P, unit=_W),
    _remote("flex_max_power", "flex", "max_power_w", device_class=_P, unit=_W, category=_DIAG),
)


class EebusRemoteSensor(EebusRemoteEntity, SensorEntity):
    """A measurement of one remote EEBUS device."""

    entity_description: EebusSensorEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        ski: str,
        description: EebusSensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, ski, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> Any:
        return self.entity_description.value_fn(self.device_data)

    @property
    def available(self) -> bool:
        return super().available and self.native_value is not None

    @property
    def extra_state_attributes(self) -> dict[str, Any] | None:
        if self.entity_description.key == "flex_state":
            flex = self.device_data.get("flex") or {}
            return {
                key: flex.get(key)
                for key in (
                    "power_w", "max_power_w", "pausable", "stoppable", "start_time",
                    "min_run_s", "min_pause_s", "last_command", "last_error",
                )
                if flex.get(key) is not None
            }
        if self.entity_description.key != "remote_category":
            return None
        dev = self.device_data
        return {
            "device_type": dev.get("device_type"),
            "entity_types": dev.get("entity_types") or [],
            "use_cases": dev.get("use_cases") or [],
        }


# Longer gaps between two power readings (e.g. Home Assistant or the bridge
# was offline) are not integrated: the consumption in between is unknown.
MAX_INTEGRATION_GAP_S = 600.0


def needs_calculated_energy(dev: dict[str, Any]) -> bool:
    """Heat pumps that report their power but no energy counter."""
    return (
        dev.get("category") == "heat_pump"
        and _get(dev, "mpc", "power_w") is not None
        and _get(dev, "mpc", "energy_consumed_wh") is None
    )


def integrate_energy(
    total_kwh: float,
    last: tuple[float, float] | None,
    now_s: float,
    power_w: float,
) -> float:
    """Add the energy between the last and this reading (trapezoidal rule)."""
    if last is None:
        return total_kwh
    last_s, last_w = last
    dt = now_s - last_s
    if dt <= 0 or dt > MAX_INTEGRATION_GAP_S:
        return total_kwh
    avg_w = (max(0.0, last_w) + max(0.0, power_w)) / 2
    return total_kwh + avg_w * dt / 3_600_000


class EebusCalculatedEnergySensor(EebusRemoteEntity, RestoreSensor):
    """Energy consumed, calculated from the power (device has no counter).

    For the energy dashboard. Only consumption is counted; gaps longer than
    ten minutes (bridge or Home Assistant offline) are skipped.
    """

    _attr_translation_key = "remote_energy_calculated"
    _attr_device_class = SensorDeviceClass.ENERGY
    _attr_native_unit_of_measurement = UnitOfEnergy.KILO_WATT_HOUR
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_suggested_display_precision = 2

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "energy_calculated")
        self._total = 0.0
        self._last: tuple[float, float] | None = None

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_sensor_data()) is not None:
            try:
                self._total = float(last.native_value or 0)
            except (TypeError, ValueError):
                self._total = 0.0
        self._sample()

    def _sample(self) -> None:
        power = _get(self.device_data, "mpc", "power_w")
        if power is None or not self.conn.get("connected", True):
            self._last = None
            return
        now = time.monotonic()
        self._total = integrate_energy(self._total, self._last, now, float(power))
        self._last = (now, float(power))

    @callback
    def _handle_coordinator_update(self) -> None:
        self._sample()
        super()._handle_coordinator_update()

    @property
    def native_value(self) -> float:
        return round(self._total, 4)


class EebusSurplusSensor(EebusEntity, SensorEntity):
    """Power available for PV-surplus charging, as seen by the controller."""

    _attr_translation_key = "pv_surplus"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_state_class = SensorStateClass.MEASUREMENT

    def __init__(self, coordinator: EebusCoordinator) -> None:
        super().__init__(coordinator, "pv_surplus")

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        self.async_on_remove(
            self.coordinator.charge_controller.async_add_listener(self.async_write_ha_state)
        )

    @property
    def native_value(self) -> float | None:
        value = self.coordinator.charge_controller.surplus_w
        return round(value) if value is not None else None

    @property
    def available(self) -> bool:
        return super().available and self.native_value is not None
