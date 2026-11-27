"""The EEBUS integration."""

from __future__ import annotations

import logging

import voluptuous as vol
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import EebusApiClient, EebusApiError
from .cards import async_register_cards
from .charging import ChargeController
from .const import (
    CONF_HOST,
    CONF_PORT,
    CONF_TOKEN,
    DOMAIN,
    MANUFACTURER,
    MODEL,
    PLATFORMS,
    SERVICE_FORGET_DEVICE,
    SERVICE_SET_CONSUMPTION_LIMIT,
    SERVICE_SET_DEVICE_LIMIT,
    SERVICE_SET_PRODUCTION_LIMIT,
    SERVICE_SIMULATE_GRID_LIMIT,
    SERVICE_TRUST_DEVICE,
)
from .coordinator import EebusCoordinator
from .heatpump import FlexController
from .remote import connections
from .repairs import async_track_issues

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

    # Register the bridge device up front so remote devices (inverter,
    # wallboxes) can reference it via its device id.
    bridge_device = dr.async_get(hass).async_get_or_create(
        config_entry_id=entry.entry_id,
        identifiers={(DOMAIN, entry.unique_id or "eebus")},
        name="EEBUS Bridge",
        manufacturer=MANUFACTURER,
        model=MODEL,
        sw_version=(coordinator.data or {}).get("service", {}).get("version"),
    )
    coordinator.bridge_device_id = bridge_device.id
    coordinator.charge_controller = ChargeController(hass, coordinator)
    coordinator.flex_controller = FlexController(hass, coordinator)
    await coordinator.async_start_websocket()

    hass.data.setdefault(DOMAIN, {})[entry.entry_id] = coordinator

    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    coordinator.charge_controller.async_start()
    coordinator.flex_controller.async_start()
    await async_register_cards(hass)
    async_track_issues(coordinator)
    entry.async_on_unload(coordinator.charge_controller.async_stop)
    entry.async_on_unload(coordinator.flex_controller.async_stop)
    entry.async_on_unload(entry.add_update_listener(_async_options_updated))
    _async_register_services(hass)
    return True


async def _async_options_updated(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Reload so the charge controller picks up new options."""
    await hass.config_entries.async_reload(entry.entry_id)


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
            SERVICE_FORGET_DEVICE,
            SERVICE_SET_DEVICE_LIMIT,
            SERVICE_SIMULATE_GRID_LIMIT,
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
    async def _handle_forget(call: ServiceCall) -> None:
        for coord in _coordinators(hass):
            try:
                await coord.client.forget(call.data["ski"])
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err

    hass.services.async_register(
        DOMAIN, SERVICE_TRUST_DEVICE, _handle_trust, schema=trust_schema
    )
    hass.services.async_register(
        DOMAIN, SERVICE_FORGET_DEVICE, _handle_forget, schema=trust_schema
    )

    device_limit_schema = vol.Schema(
        {
            vol.Exclusive("device_id", "target"): cv.string,
            vol.Exclusive("ski", "target"): cv.string,
            vol.Optional("kind", default="consumption"): vol.In(["consumption", "production"]),
            vol.Optional("value_w"): vol.All(vol.Coerce(float), vol.Range(min=0)),
            vol.Optional("active"): cv.boolean,
            vol.Optional("duration_s"): vol.All(vol.Coerce(float), vol.Range(min=0)),
        }
    )

    async def _handle_device_limit(call: ServiceCall) -> None:
        ski = call.data.get("ski") or _ski_from_device(hass, call.data.get("device_id"))
        if not ski:
            raise HomeAssistantError("Pass an EEBUS device (device_id) or its SKI")
        coords = [c for c in _coordinators(hass) if ski in connections(c.data)]
        if not coords:
            raise HomeAssistantError(f"No EEBUS bridge knows the device {ski}")
        for coord in coords:
            try:
                await coord.client.set_device_limit(
                    ski,
                    production=call.data["kind"] == "production",
                    target_w=call.data.get("value_w"),
                    active=call.data.get("active"),
                    duration_s=call.data.get("duration_s"),
                )
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err
            await coord.async_request_refresh()

    hass.services.async_register(
        DOMAIN, SERVICE_SET_DEVICE_LIMIT, _handle_device_limit, schema=device_limit_schema
    )

    simulate_schema = vol.Schema(
        {
            vol.Optional("value_w", default=4200): vol.All(vol.Coerce(float), vol.Range(min=0)),
            vol.Optional("active", default=True): cv.boolean,
            vol.Optional("duration_s", default=900): vol.All(vol.Coerce(float), vol.Range(min=0)),
        }
    )

    async def _handle_simulate(call: ServiceCall) -> None:
        for coord in _coordinators(hass):
            try:
                await coord.client.simulate_grid_limit(
                    call.data["value_w"], call.data["active"], call.data["duration_s"]
                )
            except EebusApiError as err:
                raise HomeAssistantError(str(err)) from err
            await coord.async_request_refresh()

    hass.services.async_register(
        DOMAIN, SERVICE_SIMULATE_GRID_LIMIT, _handle_simulate, schema=simulate_schema
    )


def _ski_from_device(hass: HomeAssistant, device_id: str | None) -> str | None:
    """Map a device-registry id of a remote EEBUS device to its SKI."""
    if not device_id:
        return None
    device = dr.async_get(hass).async_get(device_id)
    if device is None:
        return None
    for domain, identifier in device.identifiers:
        if domain == DOMAIN and identifier.startswith("remote_"):
            return identifier.removeprefix("remote_")
    return None
