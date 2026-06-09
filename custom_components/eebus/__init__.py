"""The EEBUS integration."""

from __future__ import annotations

import logging

import voluptuous as vol
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import EebusApiClient, EebusApiError
from .const import (
    CONF_HOST,
    CONF_PORT,
    CONF_TOKEN,
    DOMAIN,
    PLATFORMS,
    SERVICE_SET_CONSUMPTION_LIMIT,
    SERVICE_SET_PRODUCTION_LIMIT,
    SERVICE_TRUST_DEVICE,
)
from .coordinator import EebusCoordinator

_LOGGER = logging.getLogger(__name__)


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up EEBUS from a config entry."""
    session = async_get_clientsession(hass)
    client = EebusApiClient(
        session,
        entry.data[CONF_HOST],
        entry.data[CONF_PORT],
        entry.data.get(CONF_TOKEN),
    )

    coordinator = EebusCoordinator(hass, entry, client)
    await coordinator.async_config_entry_first_refresh()
    await coordinator.async_start_websocket()

    hass.data.setdefault(DOMAIN, {})[entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    _async_register_services(hass)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    unload_ok = await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
    coordinator: EebusCoordinator | None = hass.data.get(DOMAIN, {}).pop(
        entry.entry_id, None
    )
    if coordinator is not None:
        await coordinator.async_stop()

    if not hass.data.get(DOMAIN):
        for service in (
            SERVICE_SET_CONSUMPTION_LIMIT,
            SERVICE_SET_PRODUCTION_LIMIT,
            SERVICE_TRUST_DEVICE,
        ):
            hass.services.async_remove(DOMAIN, service)
    return unload_ok


def _coordinators(hass: HomeAssistant) -> list[EebusCoordinator]:
    return list(hass.data.get(DOMAIN, {}).values())


def _async_register_services(hass: HomeAssistant) -> None:
    """Register integration services once."""
    if hass.services.has_service(DOMAIN, SERVICE_SET_CONSUMPTION_LIMIT):
        return

    limit_schema = vol.Schema(
        {
            vol.Required("value_w"): vol.Coerce(float),
            vol.Optional("active", default=True): cv.boolean,
            vol.Optional("duration_s", default=0): vol.Coerce(float),
        }
    )
    trust_schema = vol.Schema({vol.Required("ski"): cv.string})

    async def _handle_consumption(call: ServiceCall) -> None:
        for coord in _coordinators(hass):
            try:
                await coord.client.set_consumption_limit(
                    call.data["value_w"],
                    call.data["active"],
                    call.data["duration_s"],
                )
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err

    async def _handle_production(call: ServiceCall) -> None:
        for coord in _coordinators(hass):
            try:
                await coord.client.set_production_limit(
                    call.data["value_w"],
                    call.data["active"],
                    call.data["duration_s"],
                )
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err

    async def _handle_trust(call: ServiceCall) -> None:
        for coord in _coordinators(hass):
            try:
                await coord.client.trust(call.data["ski"])
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err

    hass.services.async_register(
        DOMAIN, SERVICE_SET_CONSUMPTION_LIMIT, _handle_consumption, schema=limit_schema
    )
    hass.services.async_register(
        DOMAIN, SERVICE_SET_PRODUCTION_LIMIT, _handle_production, schema=limit_schema
    )
    hass.services.async_register(
        DOMAIN, SERVICE_TRUST_DEVICE, _handle_trust, schema=trust_schema
    )
