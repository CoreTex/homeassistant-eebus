"""Select platform for EEBUS: wallbox charge mode."""

from __future__ import annotations

from typing import Any

from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.restore_state import RestoreEntity

from .charging import wallbox_control_path
from .const import CHARGE_MODE_AUTO, CHARGE_MODES, DOMAIN
from .coordinator import EebusCoordinator
from .remote import EebusRemoteEntity, async_track_remotes


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS selects."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]

    def _factory(coord: EebusCoordinator, ski: str, dev: dict[str, Any]):
        if wallbox_control_path(dev):
            return [EebusChargeModeSelect(coord, ski)]
        return []

    async_track_remotes(coordinator, async_add_entities, _factory)


class EebusChargeModeSelect(EebusRemoteEntity, SelectEntity, RestoreEntity):
    """Charge mode of a wallbox.

    auto   - the bridge does not intervene (wallbox / §14a only)
    off    - do not charge
    pv     - charge from PV surplus only (start/stop delays)
    min_pv - always charge at minimum power, more with surplus
    fast   - full power
    manual - fixed power from the "Charge power (manual)" number
    """

    _attr_translation_key = "charge_mode"
    _attr_options = CHARGE_MODES

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "charge_mode")

    @property
    def _controller(self):
        return self.coordinator.charge_controller

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        mode = CHARGE_MODE_AUTO
        if (last := await self.async_get_last_state()) and last.state in CHARGE_MODES:
            mode = last.state
        # Restore without running a cycle yet; the controller timer picks it up.
        await self._controller.async_set_mode(self._ski, mode, run=False)
        self.async_on_remove(self._controller.async_add_listener(self.async_write_ha_state))

    @property
    def current_option(self) -> str:
        return self._controller.modes.get(self._ski, CHARGE_MODE_AUTO)

    @property
    def available(self) -> bool:
        return super().available and wallbox_control_path(self.device_data) is not None

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        # "lpc": only a power limit is available (no EV current control, no
        # measurements); whether the wallbox pauses at low limits (off, PV
        # without surplus) depends on its own settings.
        return {"control": wallbox_control_path(self.device_data)}

    async def async_select_option(self, option: str) -> None:
        await self._controller.async_set_mode(self._ski, option)
        self.async_write_ha_state()
