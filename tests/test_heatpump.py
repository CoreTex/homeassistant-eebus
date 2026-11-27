"""Heat pump support: OHPCF (PV surplus), calculated energy, empty sensors, diagnostics.

Device data modelled on a Bosch CS5800i with K40RF (diagnostics from issue #4):
power only, energy/phase values announced but empty, LPC, compressor entity.
"""

import copy
from unittest.mock import AsyncMock, patch

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

import custom_components.eebus.api  # noqa: F401  (load before patching)
from custom_components.eebus.button import flex_action_allowed
from custom_components.eebus.diagnostics import anonymize
from custom_components.eebus.heatpump import (
    PAUSE,
    RESUME,
    START,
    FlexInput,
    FlexRuntime,
    decide,
    requested_power,
)
from custom_components.eebus.sensor import integrate_energy, needs_calculated_energy

BRIDGE = "b" * 40
HP = "4" * 40

HP_DEVICE = {
    "category": "heat_pump",
    "device_type": "Generic",
    "entity_types": ["Compressor", "HeatPumpAppliance"],
    "use_cases": ["eg_lpc", "mpc", "ohpcf"],
    "mpc": {"supported": True, "power_w": 839},
    "control": {"lpc_supported": True, "lpc_target_w": 0, "lpc_active": False},
    "flex": {"supported": True, "state": "available", "available": True,
             "power_w": 1800, "pausable": True, "stoppable": True, "min_run_s": 600},
}

STATE = {
    "service": {"ski": BRIDGE, "version": "0.6.0-dev"},
    "connected": True,
    "primary_ski": "",
    "connections": [
        {"ski": HP, "name": "Bosch\\ Heating\\ System-102248200", "brand": "Bosch",
         "model": "K40RF", "connected": True, "trusted": True, "primary": False,
         "energy_guard_target": False},
    ],
    "devices": {HP: HP_DEVICE},
    "lpc": {"supported": True, "limit": {}},
    "lpp": {"supported": True, "limit": {}},
    "mpc": {"supported": True},
    "battery": {}, "inverter": {}, "inverter_control": {},
}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    yield


async def _never(_self, _cb, stop):
    await stop.wait()


# --- pure logic ---


def _hp(state="available", measured=None, pausable=True, min_run=0.0, min_pause=0.0):
    return FlexInput(state=state, requested_w=1500, measured_w=measured,
                     pausable=pausable, min_run_s=min_run, min_pause_s=min_pause)


def test_start_needs_surplus_for_the_start_delay() -> None:
    rt = FlexRuntime()
    # 1000 W surplus is not enough for 1500 W
    assert decide(_hp(), -1200, 200, 60, 300, rt, 0) is None
    assert rt.last_surplus_w == 1000  # shown in the debug log
    assert decide(_hp(), -2000, 200, 60, 300, rt, 10) is None  # enough, delay starts
    assert decide(_hp(), -2000, 200, 60, 300, rt, 50) is None
    assert decide(_hp(), -2000, 200, 60, 300, rt, 75) == START
    # a cloud resets the delay
    rt = FlexRuntime()
    decide(_hp(), -2000, 200, 60, 300, rt, 0)
    decide(_hp(), 100, 200, 60, 300, rt, 30)
    assert decide(_hp(), -2000, 200, 60, 300, rt, 70) is None


def test_no_grid_value_no_action() -> None:
    assert decide(_hp(), None, 200, 0, 0, FlexRuntime(), 0) is None


def test_pause_when_running_mostly_on_grid() -> None:
    rt = FlexRuntime()
    run = _hp("running", measured=2000, min_run=600)
    # 2000 W draw, 500 W export: fully covered -> keep running
    assert decide(run, -500, 200, 60, 120, rt, 0) is None
    # importing 1500 W: only 300 W covered (< half) -> pause after delay and min run
    assert decide(run, 1500, 200, 60, 120, rt, 100) is None
    assert decide(run, 1500, 200, 60, 120, rt, 300) is None  # min run 600 s
    assert decide(run, 1500, 200, 60, 120, rt, 700) == PAUSE


def test_not_pausable_keeps_running() -> None:
    rt = FlexRuntime()
    run = _hp("running", measured=2000, pausable=False)
    for t in (0, 1000, 5000):
        assert decide(run, 3000, 200, 0, 0, rt, t) is None


def test_resume_after_min_pause() -> None:
    rt = FlexRuntime()
    paused = _hp("paused", min_pause=300)
    assert decide(paused, -2000, 200, 0, 0, rt, 0) is None  # just paused
    assert decide(paused, -2000, 200, 0, 0, rt, 100) is None
    assert decide(paused, -2000, 200, 0, 0, rt, 400) == RESUME


def test_completed_or_stopped_is_left_alone() -> None:
    for state in ("completed", "stopped", "scheduled", ""):
        assert decide(_hp(state), -5000, 0, 0, 0, FlexRuntime(), 100) is None


def test_requested_power() -> None:
    assert requested_power({"power_w": 1800, "max_power_w": 3000}) == 1800
    assert requested_power({"max_power_w": 3000}) == 3000
    assert requested_power({}) == 1000


def test_flex_action_allowed() -> None:
    assert flex_action_allowed({"state": "available"}, "start")
    assert not flex_action_allowed({"state": "running"}, "start")
    assert flex_action_allowed({"state": "running", "pausable": True}, "pause")
    assert not flex_action_allowed({"state": "running"}, "pause")
    assert flex_action_allowed({"state": "paused"}, "resume")
    assert flex_action_allowed({"state": "paused", "stoppable": True}, "stop")
    assert not flex_action_allowed({"state": "available", "stoppable": True}, "stop")


def test_integrate_energy() -> None:
    assert integrate_energy(0, None, 0, 1000) == 0
    # 1 kW for one hour in 6 steps of 10 minutes
    total, last = 0.0, (0.0, 1000.0)
    for i in range(1, 7):
        total = integrate_energy(total, last, i * 600.0, 1000)
        last = (i * 600.0, 1000.0)
    assert total == pytest.approx(1.0)
    # trapezoid 0 -> 2000 W over 10 minutes = 1000 W avg = 0.1667 kWh
    assert integrate_energy(0, (0, 0), 600, 2000) == pytest.approx(1 / 6)
    # gaps longer than ten minutes are skipped, negative power is not counted
    assert integrate_energy(5, (0, 1000), 3600, 1000) == 5
    assert integrate_energy(5, (0, -500), 60, -500) == 5


def test_needs_calculated_energy() -> None:
    assert needs_calculated_energy(HP_DEVICE)
    with_counter = copy.deepcopy(HP_DEVICE)
    with_counter["mpc"]["energy_consumed_wh"] = 1
    assert not needs_calculated_energy(with_counter)
    assert not needs_calculated_energy({**HP_DEVICE, "category": "inverter"})


def test_anonymize_diagnostics() -> None:
    other = "A" * 40
    data = {
        "entry": {"data": {"host": "**REDACTED**"}},
        "bridge": {
            "state": {
                "service": {"ski": BRIDGE, "ship_id": "HomeAssistant-EEBUS-Bridge-ha-omv",
                            "fingerprint": "ab:cd", "qr_code": f"SHIP;SKI:{BRIDGE};ENDSHIP;"},
                "connections": [{"ski": HP, "name": "Bosch Heating System-102248200", "brand": "Bosch"},
                                {"ski": other, "name": ""}],
                "primary_ski": "",
                "devices": {HP: {"category": "heat_pump"}, other.lower(): {}},
                "ev": {"identifications": ["00:11:22:33:44:55"]},
            },
            "spine": [{"ski": HP, "address": "d:_i:12345_K40RF-102248200",
                       "entities": [{"address": [1, 1], "type": "Compressor"}]}],
        },
    }
    out = anonymize(data)
    text = repr(out)
    for secret in (BRIDGE, HP, other, other.lower(), "102248200", "ha-omv", "ab:cd", "00:11:22"):
        assert secret not in text, secret
    st = out["bridge"]["state"]
    assert st["service"]["ski"] == "bridge"
    assert st["connections"][0]["ski"] == "device_1"
    assert st["connections"][1]["ski"] == "device_2"
    assert set(st["devices"]) == {"device_1", "device_2"}  # same SKI, any case -> same alias
    assert out["bridge"]["spine"][0]["ski"] == "device_1"
    assert st["connections"][0]["brand"] == "Bosch"  # brand/model stay
    assert out["bridge"]["spine"][0]["entities"][0]["address"] == [1, 1]  # entity address stays
    assert st["primary_ski"] == ""


# --- in Home Assistant ---


async def test_heat_pump_entities(hass: HomeAssistant) -> None:
    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": None})
    entry.add_to_hass(hass)
    state = copy.deepcopy(STATE)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
        patch("custom_components.eebus.api.EebusApiClient.set_flex", AsyncMock()) as set_flex,
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        ent = er.async_get(hass)

        def eid(domain, key):
            return ent.async_get_entity_id(domain, "eebus", f"{BRIDGE}_{HP}_{key}")

        # power yes; energy counters announced but empty -> not created
        assert eid("sensor", "remote_power")
        assert eid("sensor", "remote_energy_consumed") is None
        assert eid("sensor", "remote_energy_produced") is None
        calc = eid("sensor", "energy_calculated")
        assert calc and hass.states.get(calc).state == "0.0"

        flex = eid("sensor", "flex_state")
        assert hass.states.get(flex).state == "available"
        assert hass.states.get(flex).attributes["power_w"] == 1800
        assert hass.states.get(eid("sensor", "flex_power")).state == "1800"

        start, pause = eid("button", "flex_start"), eid("button", "flex_pause")
        assert hass.states.get(start).state != "unavailable"
        assert hass.states.get(pause).state == "unavailable"  # not running
        await hass.services.async_call("button", "press", {"entity_id": start}, blocking=True)
        set_flex.assert_awaited_with(HP, "start")

        surplus = eid("switch", "flex_pv_surplus")
        assert hass.states.get(surplus).state == "off"
        await hass.services.async_call("switch", "turn_on", {"entity_id": surplus}, blocking=True)
        assert hass.states.get(surplus).state == "on"
        coordinator = hass.data["eebus"][entry.entry_id]
        assert coordinator.flex_controller.enabled[HP] is True

        # an energy counter that arrives later creates its sensor on the fly
        state["devices"][HP]["mpc"]["energy_consumed_wh"] = 4200.0
        await coordinator.async_refresh()
        await hass.async_block_till_done()
        assert eid("sensor", "remote_energy_consumed")

        assert await hass.config_entries.async_unload(entry.entry_id)
