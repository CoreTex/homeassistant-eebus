"""Base entity for the EEBUS integration."""

from __future__ import annotations

from typing import Any

from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN, MANUFACTURER, MODEL
from .coordinator import EebusCoordinator


class EebusEntity(CoordinatorEntity[EebusCoordinator]):
    """Common base wiring entities to the coordinator and the bridge device."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: EebusCoordinator, key: str) -> None:
        super().__init__(coordinator)
        self._key = key
        ski = coordinator.entry.unique_id or "eebus"
        self._attr_unique_id = f"{ski}_{key}"
        version = self._service().get("version")
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, ski)},
            name="EEBUS Bridge",
            manufacturer=MANUFACTURER,
            model=MODEL,
            sw_version=version,
        )

    @property
    def data(self) -> dict[str, Any]:
        """Return the latest full state snapshot."""
        return self.coordinator.data or {}

    def _service(self) -> dict[str, Any]:
        return self.data.get("service", {}) if self.coordinator.data else {}
