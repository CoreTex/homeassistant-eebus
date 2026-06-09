"""Sensor platform for EEBUS."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from homeassistant.components.sensor import (
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
    UnitOfTime,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity


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


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS sensors."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    descriptions = list(SENSORS) + _build_phase_sensors()
    async_add_entities(
        EebusSensor(coordinator, description) for description in descriptions
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
        return (
            super().available
            and self.entity_description.available_fn(self.data)
        )
