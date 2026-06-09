"""Switch platform for EEBUS (Energy-Guard limit activation)."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from typing import Any

from homeassistant.components.switch import (
    SwitchDeviceClass,
    SwitchEntity,
    SwitchEntityDescription,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .api import EebusApiClient, EebusApiError
from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
from .sensor import _get


@dataclass(frozen=True, kw_only=True)
class EebusSwitchEntityDescription(SwitchEntityDescription):
    """Switch description with state extractor, setter and availability gate."""

    is_on_fn: Callable[[dict[str, Any]], bool]
    set_fn: Callable[[EebusApiClient, dict[str, Any], bool], Awaitable[None]]
    available_fn: Callable[[dict[str, Any]], bool] = lambda _data: True


async def _set_consumption_active(
    client: EebusApiClient, data: dict[str, Any], active: bool
) -> None:
    await client.activate_eg_consumption(active)


async def _set_production_active(
    client: EebusApiClient, data: dict[str, Any], active: bool
) -> None:
    await client.activate_eg_production(active)


SWITCHES: tuple[EebusSwitchEntityDescription, ...] = (
    EebusSwitchEntityDescription(
        key="inverter_consumption_limit_active",
        translation_key="inverter_consumption_limit_active",
        device_class=SwitchDeviceClass.SWITCH,
        is_on_fn=lambda d: bool(_get(d, "inverter_control", "consumption_active")),
        set_fn=_set_consumption_active,
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
    EebusSwitchEntityDescription(
        key="inverter_production_limit_active",
        translation_key="inverter_production_limit_active",
        device_class=SwitchDeviceClass.SWITCH,
        is_on_fn=lambda d: bool(_get(d, "inverter_control", "production_active")),
        set_fn=_set_production_active,
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS switches."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        EebusSwitch(coordinator, description) for description in SWITCHES
    )


class EebusSwitch(EebusEntity, SwitchEntity):
    """A single EEBUS Energy-Guard activation switch."""

    entity_description: EebusSwitchEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        description: EebusSwitchEntityDescription,
    ) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    @property
    def is_on(self) -> bool:
        return self.entity_description.is_on_fn(self.data)

    @property
    def available(self) -> bool:
        return (
            super().available
            and self.entity_description.available_fn(self.data)
        )

    async def _apply(self, active: bool) -> None:
        try:
            await self.entity_description.set_fn(
                self.coordinator.client, self.data, active
            )
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._apply(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._apply(False)
