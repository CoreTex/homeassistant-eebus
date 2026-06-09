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
        key="lpc_heartbeat",
        translation_key="lpc_heartbeat",
        device_class=BinarySensorDeviceClass.CONNECTIVITY,
        entity_category=EntityCategory.DIAGNOSTIC,
        value_fn=lambda d: bool(_get(d, "lpc", "heartbeat_ok")),
        available_fn=lambda d: bool(_get(d, "lpc", "supported")),
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
