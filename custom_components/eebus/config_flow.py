"""Config flow for the EEBUS integration."""

from __future__ import annotations

import logging
from typing import Any

import voluptuous as vol
from homeassistant.config_entries import (
    ConfigEntry,
    ConfigFlow,
    ConfigFlowResult,
    OptionsFlow,
)
from homeassistant.core import callback
from homeassistant.helpers import selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import EebusApiClient, EebusApiError
from .const import (
    CONF_CONTROL_INTERVAL_S,
    CONF_GRID_POWER_ENTITY,
    CONF_GRID_POWER_INVERTED,
    CONF_HOST,
    CONF_MAX_CHARGE_POWER_W,
    CONF_MIN_CHARGE_POWER_W,
    CONF_PORT,
    CONF_START_DELAY_S,
    CONF_STOP_DELAY_S,
    CONF_SURPLUS_RESERVE_W,
    CONF_TOKEN,
    DEFAULT_CONTROL_INTERVAL_S,
    DEFAULT_MAX_CHARGE_POWER_W,
    DEFAULT_MIN_CHARGE_POWER_W,
    DEFAULT_PORT,
    DEFAULT_START_DELAY_S,
    DEFAULT_STOP_DELAY_S,
    DEFAULT_SURPLUS_RESERVE_W,
    DOMAIN,
)

_LOGGER = logging.getLogger(__name__)


ADDON_REPOSITORY_URL = "https://github.com/CoreTex/homeassistant-eebus"


class EebusConfigFlow(ConfigFlow, domain=DOMAIN):
    """Handle a config flow for EEBUS."""

    VERSION = 1

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> OptionsFlow:
        """Charge-controller options."""
        return EebusOptionsFlow()

    async def async_step_user(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        """Handle the initial step."""
        errors: dict[str, str] = {}

        if user_input is not None:
            session = async_get_clientsession(self.hass)
            client = EebusApiClient(
                session,
                user_input[CONF_HOST],
                user_input[CONF_PORT],
                user_input.get(CONF_TOKEN),
            )
            try:
                health = await client.health()
            except EebusApiError:
                errors["base"] = "cannot_connect"
            else:
                ski = health.get("ski") or ""
                if not ski:
                    errors["base"] = "no_ski"
                else:
                    await self.async_set_unique_id(ski)
                    self._abort_if_unique_id_configured()
                    return self.async_create_entry(
                        title=f"EEBUS Bridge ({ski[:8]})",
                        data=user_input,
                    )

        schema = vol.Schema(
            {
                vol.Required(CONF_HOST): str,
                vol.Required(CONF_PORT, default=DEFAULT_PORT): int,
                vol.Optional(CONF_TOKEN, default=""): str,
            }
        )
        return self.async_show_form(
            step_id="user",
            data_schema=schema,
            errors=errors,
            description_placeholders={"repository_url": ADDON_REPOSITORY_URL},
        )


def _number(minimum: float, maximum: float, step: float, unit: str) -> selector.NumberSelector:
    return selector.NumberSelector(
        selector.NumberSelectorConfig(
            min=minimum,
            max=maximum,
            step=step,
            unit_of_measurement=unit,
            mode=selector.NumberSelectorMode.BOX,
        )
    )


class EebusOptionsFlow(OptionsFlow):
    """Options for PV-surplus charging."""

    async def async_step_init(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        if user_input is not None:
            # An empty entity selector means "use the bridge's grid meter".
            if not user_input.get(CONF_GRID_POWER_ENTITY):
                user_input.pop(CONF_GRID_POWER_ENTITY, None)
            return self.async_create_entry(data=user_input)

        opts = self.config_entry.options
        grid_entity = opts.get(CONF_GRID_POWER_ENTITY)
        schema = vol.Schema(
            {
                vol.Optional(
                    CONF_GRID_POWER_ENTITY,
                    description={"suggested_value": grid_entity} if grid_entity else None,
                ): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain="sensor", device_class="power")
                ),
                vol.Required(
                    CONF_GRID_POWER_INVERTED,
                    default=opts.get(CONF_GRID_POWER_INVERTED, False),
                ): selector.BooleanSelector(),
                vol.Required(
                    CONF_SURPLUS_RESERVE_W,
                    default=opts.get(CONF_SURPLUS_RESERVE_W, DEFAULT_SURPLUS_RESERVE_W),
                ): _number(0, 5000, 50, "W"),
                vol.Required(
                    CONF_MIN_CHARGE_POWER_W,
                    default=opts.get(CONF_MIN_CHARGE_POWER_W, DEFAULT_MIN_CHARGE_POWER_W),
                ): _number(1000, 22000, 10, "W"),
                vol.Required(
                    CONF_MAX_CHARGE_POWER_W,
                    default=opts.get(CONF_MAX_CHARGE_POWER_W, DEFAULT_MAX_CHARGE_POWER_W),
                ): _number(1000, 44000, 100, "W"),
                vol.Required(
                    CONF_START_DELAY_S,
                    default=opts.get(CONF_START_DELAY_S, DEFAULT_START_DELAY_S),
                ): _number(0, 3600, 10, "s"),
                vol.Required(
                    CONF_STOP_DELAY_S,
                    default=opts.get(CONF_STOP_DELAY_S, DEFAULT_STOP_DELAY_S),
                ): _number(0, 3600, 10, "s"),
                vol.Required(
                    CONF_CONTROL_INTERVAL_S,
                    default=opts.get(CONF_CONTROL_INTERVAL_S, DEFAULT_CONTROL_INTERVAL_S),
                ): _number(10, 300, 5, "s"),
            }
        )
        return self.async_show_form(step_id="init", data_schema=schema)
