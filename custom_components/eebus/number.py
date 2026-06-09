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
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import EntityCategory, UnitOfPower, UnitOfTime
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .api import EebusApiClient, EebusApiError
from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
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
