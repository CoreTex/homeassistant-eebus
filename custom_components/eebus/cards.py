"""Serve and register the EEBUS dashboard cards (frontend/eebus-cards.js).

The cards are loaded automatically on every dashboard once the integration is
set up - no manual "resource" is needed. The version query string makes
browsers fetch the new file after an update.
"""

from __future__ import annotations

import logging
from pathlib import Path

from homeassistant.core import HomeAssistant
from homeassistant.loader import async_get_integration

from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

CARDS_URL = "/eebus_static/eebus-cards.js"
_REGISTERED = f"{DOMAIN}_cards_registered"


async def async_register_cards(hass: HomeAssistant) -> None:
    """Register the cards once per Home Assistant run."""
    if hass.data.get(_REGISTERED) or hass.http is None:
        return
    # imported here: http/frontend are only after_dependencies
    from homeassistant.components.frontend import add_extra_js_url
    from homeassistant.components.http import StaticPathConfig

    path = Path(__file__).parent / "frontend" / "eebus-cards.js"
    integration = await async_get_integration(hass, DOMAIN)
    await hass.http.async_register_static_paths(
        [StaticPathConfig(CARDS_URL, str(path), cache_headers=False)]
    )
    add_extra_js_url(hass, f"{CARDS_URL}?v={integration.version}")
    hass.data[_REGISTERED] = True
    _LOGGER.debug("EEBUS cards registered at %s", CARDS_URL)
