"""Config flow for the EEBUS integration."""

from __future__ import annotations

import logging
from typing import Any

import voluptuous as vol
from homeassistant.config_entries import ConfigFlow, ConfigFlowResult
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import EebusApiClient, EebusApiError
from .const import CONF_HOST, CONF_PORT, CONF_TOKEN, DEFAULT_PORT, DOMAIN

_LOGGER = logging.getLogger(__name__)


class EebusConfigFlow(ConfigFlow, domain=DOMAIN):
    """Handle a config flow for EEBUS."""

    VERSION = 1

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
            step_id="user", data_schema=schema, errors=errors
        )
