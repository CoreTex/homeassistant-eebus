"""Diagnostics download for the EEBUS integration.

Contains the bridge state and the raw SPINE data the connected devices
reported (entities, features, measurement descriptions and values), so a
device's behaviour can be analysed without a trace log.

The file is meant to be shared (e.g. in a GitHub issue), so it is
anonymised: SKIs are replaced by stable placeholders ("device_1", ...) that
keep the relations between devices readable, and names, serial numbers,
SPINE addresses (they contain serial numbers), the SHIP ID, QR code,
certificate fingerprint and vehicle identifications are removed.
"""

from __future__ import annotations

import re
from typing import Any

from homeassistant.components.diagnostics import REDACTED, async_redact_data
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .api import EebusApiError
from .const import CONF_HOST, CONF_TOKEN, DOMAIN

TO_REDACT = {CONF_TOKEN, CONF_HOST, "api_token"}

# Keys whose values identify a device, person or installation.
IDENTIFYING_KEYS = {
    "fingerprint",
    "qr_code",
    "ship_id",
    "name",
    "serial",
    "device_serial",
    "serialNumber",
    "deviceName",
    "deviceCode",
    "identifications",
    "identificationValue",
    "host",
    "token",
}

_SKI = re.compile(r"\b[0-9a-fA-F]{40}\b")


def anonymize(data: Any) -> Any:
    """Replace SKIs by placeholders and remove identifying values."""
    names: dict[str, str] = {}
    if bridge_ski := _get_bridge_ski(data):
        names[bridge_ski.lower()] = "bridge"

    def alias(ski: str) -> str:
        key = ski.lower()
        if key not in names:
            names[key] = f"device_{sum(v != 'bridge' for v in names.values()) + 1}"
        return names[key]

    def text(value: str) -> str:
        return _SKI.sub(lambda m: alias(m.group(0)), value)

    def walk(obj: Any) -> Any:
        if isinstance(obj, dict):
            out: dict[str, Any] = {}
            for key, value in obj.items():
                new_key = text(key) if isinstance(key, str) else key
                if key in IDENTIFYING_KEYS and value not in (None, "", [], {}):
                    out[new_key] = REDACTED
                elif key == "address" and isinstance(value, str):
                    # SPINE device address, e.g. "d:_i:<vendor>_<model>-<serial>"
                    out[new_key] = REDACTED
                else:
                    out[new_key] = walk(value)
            return out
        if isinstance(obj, list):
            return [walk(item) for item in obj]
        if isinstance(obj, str):
            return text(obj)
        return obj

    return walk(data)


def _get_bridge_ski(data: Any) -> str | None:
    try:
        ski = data["bridge"]["state"]["service"]["ski"]
    except (KeyError, TypeError):
        return None
    return ski if isinstance(ski, str) and _SKI.fullmatch(ski) else None


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
    return anonymize(
        {
            "entry": {
                "data": async_redact_data(dict(entry.data), TO_REDACT),
                "options": dict(entry.options),
            },
            "integration": {
                "charge_modes": dict(coordinator.charge_controller.modes),
                "pv_surplus_w": coordinator.charge_controller.surplus_w,
                "heat_pump_pv_surplus": dict(coordinator.flex_controller.enabled)
                if coordinator.flex_controller
                else {},
            },
            "bridge": bridge,
        }
    )
