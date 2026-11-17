"""Binary sensor platform for EEBUS."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
    BinarySensorEntityDescription,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN
from .coordinator import EebusCoordinator
from .entity import EebusEntity
from .remote import EebusRemoteEntity, async_track_remotes
from .sensor import _get


@dataclass(frozen=True, kw_only=True)
class EebusBinarySensorEntityDescription(BinarySensorEntityDescription):
    """Binary sensor description with a value extractor and availability gate."""

    value_fn: Callable[[dict[str, Any]], bool]
    available_fn: Callable[[dict[str, Any]], bool] = lambda _data: True


BINARY_SENSORS: tuple[EebusBinarySensorEntityDescription, ...] = (
    EebusBinarySensorEntityDescription(
        key="consumption_limit_active",
        translation_key="consumption_limit_active",
        device_class=BinarySensorDeviceClass.RUNNING,
        value_fn=lambda d: bool(_get(d, "lpc", "limit", "active")),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
    EebusBinarySensorEntityDescription(
        key="production_limit_active",
        translation_key="production_limit_active",
        device_class=BinarySensorDeviceClass.RUNNING,
        value_fn=lambda d: bool(_get(d, "lpp", "limit", "active")),
        available_fn=lambda d: bool(_get(d, "lpp", "supported")),
    ),
    EebusBinarySensorEntityDescription(
        key="connected",
        translation_key="connected",
        device_class=BinarySensorDeviceClass.CONNECTIVITY,
        value_fn=lambda d: bool(d.get("connected")),
    ),
    EebusBinarySensorEntityDescription(
        key="lpc_distribution_active",
        translation_key="lpc_distribution_active",
        device_class=BinarySensorDeviceClass.RUNNING,
        value_fn=lambda d: bool(_get(d, "distribution", "active")),
        available_fn=lambda d: bool(_get(d, "distribution", "enabled")),
    ),
    EebusBinarySensorEntityDescription(
        key="lpc_heartbeat",
        translation_key="lpc_heartbeat",
        device_class=BinarySensorDeviceClass.CONNECTIVITY,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: bool(_get(d, "lpc", "heartbeat_ok")),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
    ),
)


def _remote_factory(coord: EebusCoordinator, ski: str, dev: dict[str, Any]) -> list:
    entities: list = [EebusRemoteConnectedSensor(coord, ski)]
    for description in REMOTE_BINARY_SENSORS:
        if description.available_fn(dev):
            entities.append(EebusRemoteBinarySensor(coord, ski, description))
    return entities


# Per-device binary sensors, created by capability.
REMOTE_BINARY_SENSORS: tuple[EebusBinarySensorEntityDescription, ...] = (
    EebusBinarySensorEntityDescription(
        key="ev_plugged",
        translation_key="ev_plugged",
        device_class=BinarySensorDeviceClass.PLUG,
        value_fn=lambda d: bool(_get(d, "ev", "connected")),
        available_fn=lambda d: bool(_get(d, "ev", "charge_state")) or "evcc" in (d.get("use_cases") or []),
    ),
    EebusBinarySensorEntityDescription(
        key="ev_sleep",
        translation_key="ev_sleep",
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: bool(_get(d, "ev", "sleep_mode")),
        available_fn=lambda d: _get(d, "ev", "sleep_mode") is not None,
    ),
    EebusBinarySensorEntityDescription(
        key="lpc_limited",
        translation_key="lpc_limited",
        device_class=BinarySensorDeviceClass.RUNNING,
        value_fn=lambda d: _get(d, "control", "lpc_applied_w") is not None,
        available_fn=lambda d: bool(_get(d, "control", "lpc_supported")),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up EEBUS binary sensors."""
    coordinator: EebusCoordinator = hass.data[DOMAIN][entry.entry_id]
    async_add_entities(
        EebusBinarySensor(coordinator, description)
        for description in BINARY_SENSORS
    )
    async_track_remotes(
        coordinator,
        async_add_entities,
        _remote_factory,
    )


class EebusBinarySensor(EebusEntity, BinarySensorEntity):
    """A single EEBUS binary sensor."""

    entity_description: EebusBinarySensorEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        description: EebusBinarySensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, description.key)
        self.entity_description = description

    @property
    def is_on(self) -> bool:
        return self.entity_description.value_fn(self.data)

    @property
    def available(self) -> bool:
        return (
            super().available
            and self.entity_description.available_fn(self.data)
        )


class EebusRemoteConnectedSensor(EebusRemoteEntity, BinarySensorEntity):
    """Whether a remote EEBUS device currently has a SHIP connection."""

    _attr_translation_key = "remote_connected"
    _attr_device_class = BinarySensorDeviceClass.CONNECTIVITY

    def __init__(self, coordinator: EebusCoordinator, ski: str) -> None:
        super().__init__(coordinator, ski, "connected")

    @property
    def is_on(self) -> bool:
        return bool(self.conn.get("connected"))

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        conn = self.conn
        return {
            "ski": self._ski,
            "trusted": bool(conn.get("trusted")),
            "primary": bool(conn.get("primary")),
            "energy_guard_target": bool(conn.get("energy_guard_target")),
        }


class EebusRemoteBinarySensor(EebusRemoteEntity, BinarySensorEntity):
    """A per-device binary sensor of a remote EEBUS device."""

    entity_description: EebusBinarySensorEntityDescription

    def __init__(
        self,
        coordinator: EebusCoordinator,
        ski: str,
        description: EebusBinarySensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, ski, description.key)
        self.entity_description = description

    @property
    def is_on(self) -> bool:
        return self.entity_description.value_fn(self.device_data)

    @property
    def available(self) -> bool:
        return super().available and self.entity_description.available_fn(self.device_data)
