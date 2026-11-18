"""Diagnostics download for the EEBUS integration.

Contains the bridge state and the raw SPINE data the connected devices
reported (entities, features, measurement descriptions and values), so a
device's behaviour can be analysed without a trace log.
"""

from __future__ import annotations

from typing import Any

from homeassistant.components.diagnostics import async_redact_data
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .api import EebusApiError
from .const import CONF_TOKEN, DOMAIN

TO_REDACT = {CONF_TOKEN, "api_token"}


async def async_get_config_entry_diagnostics(
    hass: HomeAssistant, entry: ConfigEntry
) -> dict[str, Any]:
    """Return diagnostics for a config entry."""
    coordinator = hass.data[DOMAIN][entry.entry_id]
    try:
        bridge: dict[str, Any] = await coordinator.client.get_diagnostics()
    except EebusApiError as err:
        # older bridges have no diagnostics endpoint: fall back to the state
        bridge = {"error": str(err), "state": coordinator.data}
    return {
        "entry": {
            "data": async_redact_data(dict(entry.data), TO_REDACT),
            "options": dict(entry.options),
        },
        "integration": {
            "charge_modes": dict(coordinator.charge_controller.modes),
            "pv_surplus_w": coordinator.charge_controller.surplus_w,
        },
        "bridge": bridge,
    }
