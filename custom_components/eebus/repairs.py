"""Repair issues that explain missing EEBUS controls."""

from __future__ import annotations

from homeassistant.core import callback
from homeassistant.helpers import issue_registry as ir

from .const import DOMAIN
from .coordinator import EebusCoordinator
from .remote import connections, device_data, device_name, get

# Device categories that can only be controlled with enable_eg_control.
CONTROLLABLE = {"wallbox": "wallbox", "heat_pump": "heat pump"}


def eg_control_missing(data: dict | None) -> list[str]:
    """Names of devices that need enable_eg_control to be controllable."""
    if get(data, "service", "features", "eg_control"):
        return []
    names = []
    for ski, conn in connections(data).items():
        dev = device_data(data, ski)
        if dev.get("category") not in CONTROLLABLE:
            continue
        # a wallbox the bridge can control via OPEV does not need EG
        if get(dev, "ev", "control_supported"):
            continue
        names.append(device_name(conn))
    return sorted(names)


@callback
def async_track_issues(coordinator: EebusCoordinator) -> None:
    """Keep the repair issues in sync with the bridge state."""
    issue_id = f"eg_control_disabled_{coordinator.entry.entry_id}"

    @callback
    def _refresh() -> None:
        names = eg_control_missing(coordinator.data)
        if names:
            ir.async_create_issue(
                coordinator.hass,
                DOMAIN,
                issue_id,
                is_fixable=False,
                severity=ir.IssueSeverity.WARNING,
                translation_key="eg_control_disabled",
                translation_placeholders={"devices": ", ".join(names)},
            )
        else:
            ir.async_delete_issue(coordinator.hass, DOMAIN, issue_id)

    _refresh()
    coordinator.entry.async_on_unload(coordinator.async_add_listener(_refresh))
    coordinator.entry.async_on_unload(
        lambda: ir.async_delete_issue(coordinator.hass, DOMAIN, issue_id)
    )
