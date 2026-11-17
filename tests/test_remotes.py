"""Smoke test: remote devices become HA devices with connected/trusted/power entities."""

import asyncio
import copy
from unittest.mock import AsyncMock, patch

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

import custom_components.eebus.api  # noqa: F401  (load before patching)

BRIDGE = "b" * 40
INV = "1" * 40
WB1 = "2" * 40
WB2 = "3" * 40

STATE = {
    "service": {"ski": BRIDGE, "version": "0.2.0"},
    "connected": True,
    "primary_ski": INV,
    "connections": [
        {"ski": INV, "name": "", "brand": "KOSTAL", "model": "PLENTICORE G3",
         "connected": True, "trusted": True, "primary": True,
         "energy_guard_target": False},
        {"ski": WB1, "name": "", "brand": "", "model": "",
         "connected": True, "trusted": True, "primary": False,
         "energy_guard_target": False},
    ],
    "devices": {
        INV: {"mpc": {"supported": True, "power_w": 500.0}},
        WB1: {"mpc": {"supported": True, "power_w": 11000.0,
                      "energy_consumed_wh": 1234.0}},
    },
    "lpc": {"supported": True, "limit": {}},
    "lpp": {"supported": True, "limit": {}},
    "mpc": {"supported": True, "power_w": 500.0},
    "battery": {}, "inverter": {}, "inverter_control": {},
}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    yield


async def _never(_self, _cb, stop):
    await stop.wait()


async def test_remotes(hass: HomeAssistant) -> None:
    entry = MockConfigEntry(
        domain="eebus", unique_id=BRIDGE,
        data={"host": "127.0.0.1", "port": 7050, "token": None},
    )
    entry.add_to_hass(hass)
    state = copy.deepcopy(STATE)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
        patch("custom_components.eebus.api.EebusApiClient.forget",
              AsyncMock()) as forget,
        patch("custom_components.eebus.api.EebusApiClient.trust",
              AsyncMock()) as trust,
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()

        ent = er.async_get(hass)
        dev = dr.async_get(hass)

        def eid(domain, ski, key):
            return ent.async_get_entity_id(domain, "eebus", f"{BRIDGE}_{ski}_{key}")

        # wallbox 1 entities
        conn = eid("binary_sensor", WB1, "connected")
        trusted = eid("switch", WB1, "trusted")
        power = eid("sensor", WB1, "remote_power")
        energy = eid("sensor", WB1, "remote_energy_consumed")
        assert all([conn, trusted, power, energy]), (conn, trusted, power, energy)
        assert hass.states.get(conn).state == "on"
        assert hass.states.get(conn).attributes["ski"] == WB1
        assert hass.states.get(trusted).state == "on"
        assert hass.states.get(power).state == "11000.0"
        assert hass.states.get(energy).state == "1234.0"
        # inverter: no energy reported -> unavailable, power present
        assert hass.states.get(eid("sensor", INV, "remote_power")).state == "500.0"
        assert hass.states.get(eid("sensor", INV, "remote_energy_consumed")).state == "unavailable"
        # bridge-level sensor still from primary only
        assert hass.states.get("sensor.eebus_bridge_active_power").state == "500.0"

        # devices: linked via the bridge, named from mDNS
        bridge_dev = dev.async_get_device_by_identifier(("eebus", BRIDGE), entry.entry_id)
        inv_dev = dev.async_get_device_by_identifier(("eebus", f"remote_{INV}"), entry.entry_id)
        wb_dev = dev.async_get_device_by_identifier(("eebus", f"remote_{WB1}"), entry.entry_id)
        assert inv_dev.via_device_id == bridge_dev.id
        assert inv_dev.name == "KOSTAL PLENTICORE G3 (11111111)", inv_dev.name
        assert wb_dev.name == "EEBUS device (22222222)", wb_dev.name

        # mDNS details arrive later + a second wallbox appears (untrusted)
        state["connections"][1]["brand"] = "Kathrein"
        state["connections"][1]["model"] = "KWB-AC"
        state["connections"].append(
            {"ski": WB2, "brand": "Kathrein", "model": "KWB-AC",
             "connected": False, "trusted": False})
        coord = hass.data["eebus"][entry.entry_id]
        await coord.async_refresh()
        await hass.async_block_till_done()

        wb_dev = dev.async_get_device_by_identifier(("eebus", f"remote_{WB1}"), entry.entry_id)
        assert wb_dev.name == "Kathrein KWB-AC (22222222)", wb_dev.name
        assert wb_dev.manufacturer == "Kathrein"
        wb2_switch = eid("switch", WB2, "trusted")
        assert wb2_switch and hass.states.get(wb2_switch).state == "off"
        assert hass.states.get(eid("binary_sensor", WB2, "connected")).state == "off"
        # no measurements reported -> no measurement entity at all
        assert eid("sensor", WB2, "remote_power") is None

        # pair WB2 / forget WB1 via the switches
        await hass.services.async_call("switch", "turn_on", {"entity_id": wb2_switch}, blocking=True)
        trust.assert_awaited_with(WB2)
        await hass.services.async_call("switch", "turn_off", {"entity_id": trusted}, blocking=True)
        forget.assert_awaited_with(WB1)

        # forget service
        await hass.services.async_call("eebus", "forget_device", {"ski": WB2}, blocking=True)
        forget.assert_awaited_with(WB2)

        # no duplicate entities after another refresh
        before = len(ent.entities)
        await coord.async_refresh()
        await hass.async_block_till_done()
        assert len(ent.entities) == before

        assert await hass.config_entries.async_unload(entry.entry_id)
        await asyncio.sleep(0)
