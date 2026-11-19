"""Buttons for EEBUS: start/pause/resume/stop a heat pump's optional consumption."""

from __future__ import annotations

from typing import Any

from homeassistant.components.button import ButtonEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .api import EebusApiError
from .const import DOMAIN
from .coordinator import EebusCoordinator
from .remote import EebusRemoteEntity, async_track_remotes

FLEX_ACTIONS = ("start", "pause", "resume", "stop")


def flex_action_allowed(flex: dict[str, Any], action: str) -> bool:
    """Whether the heat pump's current offer accepts ``action`` (as the bridge checks)."""
    state = flex.get("state") or ""
    if action == "start":
        return state in ("available", "scheduled")
    if action == "pause":
        return bool(flex.get("pausable")) and state == "running"
    if action == "resume":
        return state == "paused"
    if action == "stop":
        return bool(flex.get("stoppable")) and state in ("running", "paused", "scheduled")
    return False


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS buttons."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_track_remotes(
        coordinator,
        async_add_entities,
        lambda coord, ski, dev: [
            EebusFlexButton(coord, ski, action) for action in FLEX_ACTIONS
        ]
        if (dev.get("flex") or {}).get("supported")
        else [],
    )


class EebusFlexButton(EebusRemoteEntity, ButtonEntity):
    """Run one command on the heat pump's optional consumption (OHPCF)."""

    def __init__(self, coordinator: EebusCoordinator, ski: str, action: str) -> None:
        super().__init__(coordinator, ski, f"flex_{action}")
        self._action = action
        self._attr_translation_key = f"flex_{action}"

    @property
    def available(self) -> bool:
        return super().available and flex_action_allowed(
            self.device_data.get("flex") or {}, self._action
        )

    async def async_press(self) -> None:
        try:
            await self.coordinator.client.set_flex(self._ski, self._action)
        except EebusApiError as err:
            raise HomeAssistantError(str(err)) from err
        await self.coordinator.async_request_refresh()
