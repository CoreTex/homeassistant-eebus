"""Constants for the EEBUS integration."""

from __future__ import annotations

from homeassistant.const import Platform

DOMAIN = "eebus"

PLATFORMS: list[Platform] = [
    Platform.SENSOR,
    Platform.BINARY_SENSOR,
    Platform.NUMBER,
    Platform.SWITCH,
    Platform.SELECT,
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
SERVICE_FORGET_DEVICE = "forget_device"
SERVICE_SET_DEVICE_LIMIT = "set_device_limit"

# --- Options (charging controller) ---
# Grid power sensor used for PV-surplus charging. If unset, the bridge's grid
# connection point (MGCP) is used when a meter reports it.
CONF_GRID_POWER_ENTITY = "grid_power_entity"
# True if the sensor is positive when feeding in (most HA sensors are positive
# when importing, which is also the EEBUS convention).
CONF_GRID_POWER_INVERTED = "grid_power_inverted"
CONF_SURPLUS_RESERVE_W = "surplus_reserve_w"
CONF_MIN_CHARGE_POWER_W = "min_charge_power_w"
CONF_MAX_CHARGE_POWER_W = "max_charge_power_w"
CONF_START_DELAY_S = "start_delay_s"
CONF_STOP_DELAY_S = "stop_delay_s"
CONF_CONTROL_INTERVAL_S = "control_interval_s"

DEFAULT_SURPLUS_RESERVE_W = 100
# 6 A on three phases at 230 V - the IEC 61851 minimum for 3-phase charging.
DEFAULT_MIN_CHARGE_POWER_W = 4140
DEFAULT_MAX_CHARGE_POWER_W = 11000
DEFAULT_START_DELAY_S = 60
DEFAULT_STOP_DELAY_S = 300
DEFAULT_CONTROL_INTERVAL_S = 30

# Charge modes of a wallbox (select entity).
CHARGE_MODE_AUTO = "auto"  # bridge does not intervene (wallbox decides)
CHARGE_MODE_OFF = "off"  # do not charge
CHARGE_MODE_PV = "pv"  # charge from PV surplus only
CHARGE_MODE_MIN_PV = "min_pv"  # minimum power, more with surplus
CHARGE_MODE_FAST = "fast"  # full power
CHARGE_MODE_MANUAL = "manual"  # fixed power/current set by the user
CHARGE_MODES = [
    CHARGE_MODE_AUTO,
    CHARGE_MODE_OFF,
    CHARGE_MODE_PV,
    CHARGE_MODE_MIN_PV,
    CHARGE_MODE_FAST,
    CHARGE_MODE_MANUAL,
]

NOMINAL_PHASE_VOLTAGE = 230.0
