"""Constants for the EEBUS integration."""

from __future__ import annotations

from homeassistant.const import Platform

DOMAIN = "eebus"

PLATFORMS: list[Platform] = [
    Platform.SENSOR,
    Platform.BINARY_SENSOR,
    Platform.NUMBER,
    Platform.SWITCH,
]

CONF_HOST = "host"
CONF_PORT = "port"
CONF_TOKEN = "token"

DEFAULT_PORT = 7050

# How often we poll /api/state as a fallback. Live updates arrive via WebSocket.
UPDATE_INTERVAL_SECONDS = 30

MANUFACTURER = "EEBUS"
MODEL = "EEBUS Bridge"

# Service names
SERVICE_SET_CONSUMPTION_LIMIT = "set_consumption_limit"
SERVICE_SET_PRODUCTION_LIMIT = "set_production_limit"
SERVICE_TRUST_DEVICE = "trust_device"
