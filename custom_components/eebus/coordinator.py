"""DataUpdateCoordinator for EEBUS: polls REST and receives WebSocket pushes."""

from __future__ import annotations

import asyncio
import logging
from datetime import timedelta
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import EebusApiClient, EebusApiError
from .const import DOMAIN, UPDATE_INTERVAL_SECONDS

_LOGGER = logging.getLogger(__name__)


class EebusCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Keeps the latest EEBUS state, refreshed by polling and WebSocket pushes."""

    def __init__(
        self,
        hass: HomeAssistant,
        entry: ConfigEntry,
        client: EebusApiClient,
    ) -> None:
        super().__init__(
            hass,
            _LOGGER,
            name=DOMAIN,
            update_interval=timedelta(seconds=UPDATE_INTERVAL_SECONDS),
        )
        self.entry = entry
        self.client = client
        self._stop_event = asyncio.Event()
        self._ws_task: asyncio.Task | None = None

    async def _async_update_data(self) -> dict[str, Any]:
        try:
            return await self.client.get_state()
        except EebusApiError as err:
            raise UpdateFailed(str(err)) from err

    async def async_start_websocket(self) -> None:
        """Start the background WebSocket listener."""
        if self._ws_task is None:
            self._ws_task = self.entry.async_create_background_task(
                self.hass,
                self.client.listen(self._on_ws_state, self._stop_event),
                name=f"{DOMAIN}_ws",
            )

    @callback
    def _on_ws_state(self, data: dict[str, Any]) -> None:
        """Handle a state snapshot pushed over the WebSocket."""
        self.async_set_updated_data(data)

    async def async_stop(self) -> None:
        """Stop the WebSocket listener."""
        self._stop_event.set()
        if self._ws_task is not None:
            self._ws_task.cancel()
            self._ws_task = None
