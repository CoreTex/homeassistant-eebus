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
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.restore_state import RestoreEntity

from .api import EebusApiClient, EebusApiError
from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
from .remote import EebusRemoteEntity, async_track_remotes
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
        entity_registry_enabled_default=False,
        device_class=SwitchDeviceClass.SWITCH,
        is_on_fn=lambda d: bool(_get(d, "inverter_control", "consumption_active")),
        set_fn=_set_consumption_active,
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
    EebusSwitchEntityDescription(
        key="inverter_production_limit_active",
        translation_key="inverter_production_limit_active",
        entity_registry_enabled_default=False,
        device_class=SwitchDeviceClass.SWITCH,
        is_on_fn=lambda d: bool(_get(d, "inverter_control", "production_active")),
        set_fn=_set_production_active,
        available_fn=lambda d: bool(_get(d, "inverter_control", "supported")),
    ),
)


def _remote_factory(coord: EebusCoordinator, ski: str, dev: dict[str, Any]) -> list:
    entities: list = [EebusRemoteTrustSwitch(coord, ski)]
    if _get(dev, "control", "lpc_supported"):
        entities.append(EebusRemoteLimitSwitch(coord, ski, production=False))
    if _get(dev, "control", "lpp_supported"):
        entities.append(EebusRemoteLimitSwitch(coord, ski, production=True))
    if _get(dev, "flex", "supported"):
        entities.append(EebusFlexSurplusSwitch(coord, ski))
    return entities


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
    async_track_remotes(
        coordinator,
        async_add_entities,
        _remote_factory,
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


class EebusRemoteTrustSwitch(EebusRemoteEntity, SwitchEntity):
    """Trust (pair) or forget a remote EEBUS device.

    Turning it on lets the device complete SHIP pairing with the bridge; the
    trust is persisted by the add-on. The remote still has to trust the
    bridge's SKI on its side (e.g. in the wallbox web UI).
    """

    _attr_translation_key = "remote_trusted"
    _attr_entity_category = EntityCategory.CONFIG

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "trusted")

    @property
    def is_on(self) -> bool:
        return bool(self.conn.get("trusted"))

    async def _apply(self, trusted: bool) -> None:
        client = self.coordinator.client
        try:
            if trusted:
                await client.trust(self._ski)
            else:
                await client.forget(self._ski)
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._apply(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._apply(False)


class EebusRemoteLimitSwitch(EebusRemoteEntity, SwitchEntity):
    """Apply/remove the staged consumption (LPC) or production (LPP) limit.

    The limit value is the matching "... limit" number of the device. For
    wallboxes with a charge mode other than "auto" the charge controller
    drives this limit itself.
    """

    def __init__(self, coordinator: EebusCoordinator, ski: str, *, production: bool) -> None:
        key = "lpp_active" if production else "lpc_active"
        super().__init__(coordinator, ski, key)
        self._production = production
        self._attr_translation_key = key

    @property
    def is_on(self) -> bool:
        field = "lpp_active" if self._production else "lpc_active"
        return bool(_get(self.device_data, "control", field))

    async def _apply(self, active: bool) -> None:
        try:
            await self.coordinator.client.set_device_limit(
                self._ski, production=self._production, active=active
            )
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._apply(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._apply(False)


class EebusFlexSurplusSwitch(EebusRemoteEntity, SwitchEntity, RestoreEntity):
    """Start the heat pump's optional consumption on PV surplus (OHPCF).

    Uses the grid sensor, reserve and delays of PV-surplus charging. The
    setting is kept by Home Assistant (restored after a restart).
    """

    _attr_translation_key = "flex_pv_surplus"

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "flex_pv_surplus")
        self._on = False

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_state()) is not None:
            self._on = last.state == "on"
        await self.coordinator.flex_controller.async_set_enabled(self._ski, self._on, run=False)

    @property
    def is_on(self) -> bool:
        return self._on

    async def _apply(self, on: bool) -> None:
        self._on = on
        await self.coordinator.flex_controller.async_set_enabled(self._ski, on)
        self.async_write_ha_state()

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._apply(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._apply(False)
