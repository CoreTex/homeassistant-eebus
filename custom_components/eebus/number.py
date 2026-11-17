"""Number platform for EEBUS (writable configuration & Energy-Guard limits)."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any

from homeassistant.components.number import (
    NumberDeviceClass,
    NumberEntity,
    NumberEntityDescription,
    NumberMode,
    RestoreNumber,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import EntityCategory, UnitOfPower, UnitOfTime
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .api import EebusApiClient, EebusApiError
from .charging import wallbox_control_path
from .const import DEFAULT_MIN_CHARGE_POWER_W, DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
from .remote import EebusRemoteEntity, async_track_remotes
from .sensor import _get


@dataclass(frozen=True, kw_only=True)
class EebusNumberEntityDescription(NumberEntityDescription):
    """Number description with value extractor, setter and availability gate."""

    value_fn: Callable[[dict[str, Any]], Any]
    set_fn: Callable[[EebusApiClient, dict[str, Any], float], Awaitable[None]]
    available_fn: Callable[[dict[str, Any]], bool] = lambda _data: True


NUMBERS: tuple[EebusNumberEntityDescription, ...] = (
    # --- Controllable System failsafe / nominal config ---
    EebusNumberEntityDescription(
        key="failsafe_consumption_power",
        translation_key="failsafe_consumption_power",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpc", "failsafe_power_w"),
        set_fn=lambda c, d, v: c.set_lpc_failsafe(power_w=v),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusNumberEntityDescription(
        key="failsafe_consumption_duration",
        translation_key="failsafe_consumption_duration",
        device_class=NumberDeviceClass.DURATION,
        native_unit_of_measurement=UnitOfTime.SECONDS,
        native_min_value=7200,
        native_max_value=86400,
        native_step=3600,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpc", "failsafe_duration_s"),
        set_fn=lambda c, d, v: c.set_lpc_failsafe(duration_s=v),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusNumberEntityDescription(
        key="consumption_nominal_max",
        translation_key="consumption_nominal_max_cfg",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpc", "nominal_max_w"),
        set_fn=lambda c, d, v: c.set_lpc_nominal_max(v),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusNumberEntityDescription(
        key="failsafe_production_power",
        translation_key="failsafe_production_power",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpp", "failsafe_power_w"),
        set_fn=lambda c, d, v: c.set_lpp_failsafe(power_w=v),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    EebusNumberEntityDescription(
        key="failsafe_production_duration",
        translation_key="failsafe_production_duration",
        device_class=NumberDeviceClass.DURATION,
        native_unit_of_measurement=UnitOfTime.SECONDS,
        native_min_value=7200,
        native_max_value=86400,
        native_step=3600,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpp", "failsafe_duration_s"),
        set_fn=lambda c, d, v: c.set_lpp_failsafe(duration_s=v),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    EebusNumberEntityDescription(
        key="production_nominal_max",
        translation_key="production_nominal_max_cfg",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "lpp", "nominal_max_w"),
        set_fn=lambda c, d, v: c.set_lpp_nominal_max(v),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    # --- Energy Guard: stage the inverter limit target ---
    # Setting the target does not contact the inverter; flip the matching switch
    # to apply it. This means the target can be configured even when the inverter
    # is offline.
    EebusNumberEntityDescription(
        key="inverter_consumption_limit",
        translation_key="inverter_consumption_limit",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        value_fn=lambda d: _get(d, "inverter_control", "consumption_target_w"),
        set_fn=lambda c, d, v: c.set_eg_consumption_target(v),
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
    EebusNumberEntityDescription(
        key="inverter_production_limit",
        translation_key="inverter_production_limit",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        value_fn=lambda d: _get(d, "inverter_control", "production_target_w"),
        set_fn=lambda c, d, v: c.set_eg_production_target(v),
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS numbers."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        EebusNumber(coordinator, description) for description in NUMBERS
    )
    async_track_remotes(coordinator, async_add_entities, _remote_factory)


def _remote_factory(coord: EebusCoordinator, ski: str, dev: dict[str, Any]) -> list:
    entities: list = [
        EebusRemoteNumber(coord, ski, description)
        for description in REMOTE_NUMBERS
        if description.available_fn(dev)
    ]
    if wallbox_control_path(dev):
        entities.append(EebusManualChargePower(coord, ski))
    return entities


def _device_limit(production: bool):
    async def _set(client: EebusApiClient, ski: str, value: float) -> None:
        await client.set_device_limit(ski, production=production, target_w=value)

    return _set


# Per-device numbers (value_fn/available_fn work on the device's data block;
# set_fn receives the device SKI instead of the full state).
REMOTE_NUMBERS: tuple[EebusNumberEntityDescription, ...] = (
    EebusNumberEntityDescription(
        key="lpc_target",
        translation_key="lpc_target",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        value_fn=lambda d: _get(d, "control", "lpc_target_w"),
        set_fn=_device_limit(False),
        available_fn=lambda d: bool(_get(d, "control", "lpc_supported")),
    ),
    EebusNumberEntityDescription(
        key="lpp_target",
        translation_key="lpp_target",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        value_fn=lambda d: _get(d, "control", "lpp_target_w"),
        set_fn=_device_limit(True),
        available_fn=lambda d: bool(_get(d, "control", "lpp_supported")),
    ),
    EebusNumberEntityDescription(
        key="lpc_failsafe_power",
        translation_key="lpc_failsafe_power",
        device_class=NumberDeviceClass.POWER,
        native_unit_of_measurement=UnitOfPower.WATT,
        native_min_value=0,
        native_max_value=100000,
        native_step=100,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "control", "lpc_failsafe_w"),
        set_fn=lambda c, ski, v: c.set_device_failsafe(ski, power_w=v),
        available_fn=lambda d: bool(_get(d, "control", "lpc_supported")),
    ),
    EebusNumberEntityDescription(
        key="lpc_failsafe_duration",
        translation_key="lpc_failsafe_duration",
        device_class=NumberDeviceClass.DURATION,
        native_unit_of_measurement=UnitOfTime.SECONDS,
        native_min_value=7200,
        native_max_value=86400,
        native_step=3600,
        mode=NumberMode.BOX,
        entity_category=EntityCategory.CONFIG,
        value_fn=lambda d: _get(d, "control", "lpc_failsafe_duration_s"),
        set_fn=lambda c, ski, v: c.set_device_failsafe(ski, duration_s=v),
        available_fn=lambda d: bool(_get(d, "control", "lpc_supported")),
    ),
)


class EebusNumber(EebusEntity, NumberEntity):
    """A single EEBUS configuration/limit number."""

    entity_description: EebusNumberEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        description: EebusNumberEntityDescription,
    ) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> float | None:
        value = self.entity_description.value_fn(self.data)
        return float(value) if value is not None else None

    @property
    def available(self) -> bool:
        return (
            super().available
            and self.entity_description.available_fn(self.data)
        )

    async def async_set_native_value(self, value: float) -> None:
        try:
            await self.entity_description.set_fn(
                self.coordinator.client, self.data, value
            )
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()


class EebusRemoteNumber(EebusRemoteEntity, NumberEntity):
    """A per-device limit / failsafe number of a remote EEBUS device."""

    entity_description: EebusNumberEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        ski: str,
        description: EebusNumberEntityDescription,
    ) -> None:
        super().__init__(coordinator, ski, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> float | None:
        value = self.entity_description.value_fn(self.device_data)
        return float(value) if value is not None else None

    @property
    def available(self) -> bool:
        return super().available and self.entity_description.available_fn(self.device_data)

    async def async_set_native_value(self, value: float) -> None:
        try:
            await self.entity_description.set_fn(self.coordinator.client, self._ski, value)
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()


class EebusManualChargePower(EebusRemoteEntity, RestoreNumber):
    """Charging power used by the "manual" charge mode (converted to amperes
    for wallboxes controlled via OPEV)."""

    _attr_translation_key = "charge_power_manual"
    _attr_device_class = NumberDeviceClass.POWER
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_native_min_value = 0
    _attr_native_max_value = 44000
    _attr_native_step = 100
    _attr_mode = NumberMode.BOX

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "charge_power_manual")
        self._value: float = DEFAULT_MIN_CHARGE_POWER_W

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_number_data()) and last.native_value is not None:
            self._value = float(last.native_value)
        await self.coordinator.charge_controller.async_set_manual(self._ski, self._value, run=False)

    @property
    def native_value(self) -> float:
        return self._value

    @property
    def available(self) -> bool:
        return super().available and wallbox_control_path(self.device_data) is not None

    async def async_set_native_value(self, value: float) -> None:
        self._value = value
        await self.coordinator.charge_controller.async_set_manual(self._ski, value)
        self.async_write_ha_state()
