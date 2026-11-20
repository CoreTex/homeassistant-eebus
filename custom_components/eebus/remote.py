"""Remote EEBUS devices (inverter, wallboxes, ...) known to the bridge.

Every SKI the bridge lists under ``connections`` (seen via mDNS, configured as
trusted, or paired) becomes its own Home Assistant device, linked to the bridge
via ``via_device``. Entities for new SKIs are added dynamically as they appear.
"""

from __future__ import annotations

from collections.abc import Callable, Iterable
from typing import Any

from homeassistant.core import callback
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.entity import Entity
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN
from .coordinator import EebusCoordinator


def connections(data: dict[str, Any] | None) -> dict[str, dict[str, Any]]:
    """Return the bridge's known remotes keyed by SKI."""
    result: dict[str, dict[str, Any]] = {}
    for conn in (data or {}).get("connections") or []:
        if isinstance(conn, dict) and conn.get("ski"):
            result[conn["ski"]] = conn
    return result


def device_name(conn: dict[str, Any]) -> str:
    """Build a readable device name from the mDNS details of a remote."""
    label = " ".join(p for p in (conn.get("brand"), conn.get("model")) if p)
    label = label or conn.get("name") or "EEBUS device"
    return f"{label} ({conn['ski'][:8]})"


def device_identifier(ski: str) -> tuple[str, str]:
    """Device registry identifier of a remote device."""
    return (DOMAIN, f"remote_{ski}")


_SUPPORTS_VIA_DEVICE_ID = "via_device_id" in DeviceInfo.__annotations__


def _lookup_device(
    registry: dr.DeviceRegistry, identifier: tuple[str, str], entry_id: str
) -> dr.DeviceEntry | None:
    """Find our device by identifier across HA versions.

    Newer HA scopes identifiers per config entry and deprecates
    ``async_get_device``; older releases only have the latter.
    """
    by_identifier = getattr(registry, "async_get_device_by_identifier", None)
    if by_identifier is not None:
        return by_identifier(identifier, entry_id)
    return registry.async_get_device(identifiers={identifier})


class EebusRemoteEntity(CoordinatorEntity[EebusCoordinator]):
    """Base for entities that belong to one remote EEBUS device."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: EebusCoordinator, ski: str, key: str) -> None:
        super().__init__(coordinator)
        self._ski = ski
        bridge = coordinator.entry.unique_id or "eebus"
        self._attr_unique_id = f"{bridge}_{ski}_{key}"
        conn = connections(coordinator.data).get(ski, {"ski": ski})
        info = DeviceInfo(
            identifiers={device_identifier(ski)},
            name=device_name(conn),
            manufacturer=conn.get("brand") or None,
            model=conn.get("model") or None,
            serial_number=ski,
        )
        # HA 2026.10+ links devices by device id; `via_device` is deprecated
        # there (removed in 2027.8) but is the only option on older releases.
        if _SUPPORTS_VIA_DEVICE_ID and coordinator.bridge_device_id:
            info["via_device_id"] = coordinator.bridge_device_id
        else:
            info["via_device"] = (DOMAIN, bridge)
        self._attr_device_info = info

    @property
    def conn(self) -> dict[str, Any]:
        """The latest bridge view of this remote (empty if unknown)."""
        return connections(self.coordinator.data).get(self._ski, {})

    @property
    def device_data(self) -> dict[str, Any]:
        """Per-device capabilities and measurements reported by this remote."""
        return device_data(self.coordinator.data, self._ski)

    @property
    def available(self) -> bool:
        return super().available and bool(self.conn)


def device_data(data: dict[str, Any] | None, ski: str) -> dict[str, Any]:
    """Per-device block of the bridge state (capabilities and measurements)."""
    devices = (data or {}).get("devices") or {}
    return devices.get(ski) or {}


def get(data: dict[str, Any] | None, *path: str) -> Any:
    """Safe nested lookup."""
    cur: Any = data
    for key in path:
        if not isinstance(cur, dict):
            return None
        cur = cur.get(key)
    return cur


@callback
def async_track_remotes(
    coordinator: EebusCoordinator,
    add_entities: Callable[[Iterable[Entity]], None],
    factory: Callable[[EebusCoordinator, str, dict[str, Any]], Iterable[Entity]],
) -> None:
    """Add entities from ``factory`` for every remote device, now and later.

    ``factory`` receives the device's current data and returns the entities
    that apply to it (by capability). It is re-evaluated on every update, and
    entities whose unique_id is already known are skipped, so capabilities
    discovered later (e.g. a vehicle plugged in, Energy-Guard control enabled)
    add their entities on the fly.

    Also keeps the device-registry name/brand/model in sync once mDNS details
    arrive (trusted SKIs from the add-on config are known before mDNS sees
    them). A name the user set in the UI is stored separately and kept.
    """
    known: set[str] = set()
    entry = coordinator.entry

    @callback
    def _refresh() -> None:
        data = coordinator.data
        remotes = connections(data)
        new: list[Entity] = []
        for ski in remotes:
            for entity in factory(coordinator, ski, device_data(data, ski)):
                if entity.unique_id and entity.unique_id not in known:
                    known.add(entity.unique_id)
                    new.append(entity)
        if new:
            add_entities(new)

        registry = dr.async_get(coordinator.hass)
        for ski, conn in remotes.items():
            device = _lookup_device(registry, device_identifier(ski), entry.entry_id)
            if device is None:
                continue
            name = device_name(conn)
            brand = conn.get("brand") or None
            model = conn.get("model") or None
            if (device.name, device.manufacturer, device.model) != (name, brand, model):
                registry.async_update_device(
                    device.id, name=name, manufacturer=brand, model=model
                )

    _refresh()
    entry.async_on_unload(coordinator.async_add_listener(_refresh))
