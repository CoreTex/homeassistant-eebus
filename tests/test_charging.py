"""Charge controller: pure logic + end-to-end with a Kostal + KATHREIN-like setup."""

import copy
from unittest.mock import AsyncMock, patch

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

import custom_components.eebus.api  # noqa: F401  (load before patching)
from custom_components.eebus.charging import (
    CMD_FULL,
    CMD_POWER,
    CMD_RELEASE,
    Runtime,
    Settings,
    Wallbox,
    compute_commands,
    power_to_current,
)

S = Settings(reserve_w=100, min_power_w=4140, max_power_w=11000, start_delay_s=60, stop_delay_s=300)


def wb(mode, **kw):
    base = dict(ski="w1", mode=mode, via="lpc", min_w=4140, max_w=11000)
    base.update(kw)
    return Wallbox(**base)


# ---------------- pure logic ----------------

def test_modes_basic():
    rt = {}
    assert compute_commands([wb("auto")], 0, S, rt, 0)["w1"].kind == CMD_RELEASE
    assert compute_commands([wb("off")], 0, S, rt, 0)["w1"] == compute_commands([wb("off")], 0, S, {}, 0)["w1"]
    assert compute_commands([wb("off")], 0, S, {}, 0)["w1"].value_w == 0
    assert compute_commands([wb("fast")], 0, S, {}, 0)["w1"].kind == CMD_FULL
    cmd = compute_commands([wb("manual", manual_w=7000)], 0, S, {}, 0)["w1"]
    assert (cmd.kind, cmd.value_w) == (CMD_POWER, 7000)
    # manual is clamped to the wallbox maximum
    assert compute_commands([wb("manual", manual_w=50000)], 0, S, {}, 0)["w1"].value_w == 11000


def test_pv_start_delay_and_stop_delay():
    rt = {}
    # 6 kW export, nothing charging yet -> surplus 5900 W, but start delay first
    assert compute_commands([wb("pv")], -6000, S, rt, 0)["w1"].value_w == 0
    assert compute_commands([wb("pv")], -6000, S, rt, 30)["w1"].value_w == 0
    cmd = compute_commands([wb("pv")], -6000, S, rt, 61)["w1"]
    assert cmd.value_w == 5900
    # now charging at 5900 (estimate), grid balanced at +50 W import
    cmd = compute_commands([wb("pv")], 50, S, rt, 90)["w1"]
    assert cmd.value_w == 5750  # 5900 - 50 - 100
    # cloud: surplus drops below minimum -> ride through at minimum
    cmd = compute_commands([wb("pv")], 3000, S, rt, 120)["w1"]
    assert cmd.value_w == 4140
    cmd = compute_commands([wb("pv")], 3000, S, rt, 300)["w1"]
    assert cmd.value_w == 4140
    # stop delay (300 s since 120) elapsed -> stop
    cmd = compute_commands([wb("pv")], 3000, S, rt, 421)["w1"]
    assert cmd.value_w == 0


def test_pv_without_grid_measurement_does_not_charge():
    rt = {}
    assert compute_commands([wb("pv")], None, S, rt, 1000)["w1"].value_w == 0


def test_min_pv_charges_at_least_minimum():
    # never below the minimum, even after rounding to 50 W steps
    assert compute_commands([wb("min_pv")], 2000, S, {}, 0)["w1"].value_w == 4140
    assert compute_commands([wb("min_pv")], -8000, S, {}, 0)["w1"].value_w == 7900


def test_measured_power_beats_estimate():
    rt = {"w1": Runtime(commanded_w=11000, charging=True)}
    # wallbox reports it only draws 3 kW (car nearly full); 1 kW export
    cmd = compute_commands([wb("min_pv", measured_w=3000)], -1000, S, rt, 0)["w1"]
    assert cmd.value_w == 4140  # 3000 + 1000 - 100 = 3900 -> raised to minimum


def test_surplus_split_between_two_pv_wallboxes():
    rt = {}
    boxes = [wb("min_pv", ski="a"), wb("min_pv", ski="b")]
    cmds = compute_commands(boxes, -12100, S, rt, 0)
    assert cmds["a"].value_w == cmds["b"].value_w == 6000


def test_unplugged_opev_parks_at_zero():
    cmd = compute_commands([wb("min_pv", via="opev", plugged=False)], -8000, S, {}, 0)["w1"]
    assert cmd.value_w == 0


def test_power_to_current():
    assert power_to_current(4140, 3) == 6
    assert power_to_current(11000, 3) == 15
    assert power_to_current(3680, 1) == 16
    assert power_to_current(-1, 3) == 0


# ---------------- end-to-end ----------------

BRIDGE = "3ec9b9ff8c080b14356f2ae285c234d1c213bd1f"
KOSTAL = "051474aad919b16eb7a6589e672facff0a8cf38b"
KATHREIN = "4ff37d1e374a006d0de80260256b82de3d745fa4"

STATE = {
    "service": {"ski": BRIDGE, "version": "0.3.0-dev"},
    "connected": True,
    "primary_ski": KOSTAL,
    "connections": [
        {"ski": KOSTAL, "brand": "KOSTAL Solar Electric GmbH", "model": "PLENTICORE-M-G3",
         "connected": True, "trusted": True, "primary": True, "energy_guard_target": True},
        {"ski": KATHREIN, "brand": "KATHREIN", "model": "AC40",
         "connected": True, "trusted": True},
    ],
    "devices": {
        KOSTAL: {
            "category": "inverter", "device_type": "Generic",
            "entity_types": ["Battery", "CEM", "Inverter", "PVESHybrid"],
            "use_cases": ["eg_lpc", "eg_lpp", "mpc"],
            "mpc": {"supported": True, "power_w": 2361.8, "energy_consumed_wh": 152253.3},
            "control": {"lpc_supported": True, "lpp_supported": True, "lpc_target_w": 0,
                        "lpp_target_w": 0, "duration_s": 0},
        },
        KATHREIN: {
            "category": "wallbox", "device_type": "ChargingStation", "entity_types": ["EVSE"],
            "use_cases": ["eg_lpc"],
            "control": {"lpc_supported": True, "lpc_target_w": 0, "duration_s": 0,
                        "lpc_failsafe_w": 4200, "lpc_failsafe_duration_s": 7200,
                        "lpc_nominal_max_w": 11000},
        },
    },
    "site": {"leading_inverter_ski": KOSTAL, "inverters": 1, "wallboxes": 1, "heat_pumps": 0},
    "distribution": {"enabled": False},
    "lpc": {"supported": True, "limit": {}}, "lpp": {"supported": True, "limit": {}},
    "mpc": {"supported": True, "power_w": 2361.8},
    "battery": {}, "inverter": {}, "inverter_control": {"supported": True},
}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations):
    yield


async def _never(_self, _cb, stop):
    await stop.wait()


async def test_kathrein_pv_charging(hass: HomeAssistant) -> None:
    hass.states.async_set("sensor.grid_power", "-6000", {"unit_of_measurement": "W"})
    entry = MockConfigEntry(
        domain="eebus", unique_id=BRIDGE,
        data={"host": "127.0.0.1", "port": 7050, "token": None},
        options={"grid_power_entity": "sensor.grid_power", "start_delay_s": 0},
    )
    entry.add_to_hass(hass)
    state = copy.deepcopy(STATE)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
        patch("custom_components.eebus.api.EebusApiClient.set_device_limit", AsyncMock()) as limit,
        patch("custom_components.eebus.api.EebusApiClient.set_device_failsafe", AsyncMock()) as failsafe,
        patch("custom_components.eebus.api.EebusApiClient.set_ev_current", AsyncMock()) as current,
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        ent = er.async_get(hass)

        def eid(domain, ski, key):
            return ent.async_get_entity_id(domain, "eebus", f"{BRIDGE}_{ski}_{key}")

        # Kathrein: LPC-only wallbox -> charge mode, manual power, LPC number/switch, failsafe
        mode = eid("select", KATHREIN, "charge_mode")
        assert mode, "charge mode select missing for the Kathrein"
        assert hass.states.get(mode).state == "auto"
        for key in ("lpc_target", "lpc_failsafe_power", "lpc_failsafe_duration", "charge_power_manual"):
            assert eid("number", KATHREIN, key), key
        assert eid("switch", KATHREIN, "lpc_active")
        assert hass.states.get(eid("number", KATHREIN, "lpc_failsafe_power")).state == "4200.0"
        # no EV/measurement entities for a box that does not report them
        assert eid("sensor", KATHREIN, "remote_power") is None
        assert eid("binary_sensor", KATHREIN, "ev_plugged") is None
        # Kostal: inverter gets no charge mode, but LPC + LPP controls and MPC sensors
        assert eid("select", KOSTAL, "charge_mode") is None
        assert eid("switch", KOSTAL, "lpp_active") and eid("number", KOSTAL, "lpp_target")
        assert hass.states.get(eid("sensor", KOSTAL, "remote_power")).state == "2361.8"

        # auto mode must not touch the wallbox
        coord = hass.data["eebus"][entry.entry_id]
        await coord.charge_controller._async_tick()
        limit.assert_not_awaited()

        # PV mode: 6 kW export -> 5900 W limit via LPC
        await hass.services.async_call("select", "select_option",
                                       {"entity_id": mode, "option": "pv"}, blocking=True)
        await hass.async_block_till_done()
        # start_delay 0: charging starts in the cycle run by the mode change
        limit.assert_awaited_with(KATHREIN, target_w=5900.0, active=True, duration_s=0)
        assert hass.states.get("sensor.eebus_bridge_pv_surplus").state == "5900"
        # the car now draws 5.9 kW, grid balanced at 100 W export -> unchanged, nothing sent
        limit.reset_mock()
        hass.states.async_set("sensor.grid_power", "-100", {"unit_of_measurement": "W"})
        await coord.charge_controller._async_tick()
        limit.assert_not_awaited()
        # more sun: 2 kW extra export -> limit follows (integrating estimate)
        hass.states.async_set("sensor.grid_power", "-2100", {"unit_of_measurement": "W"})
        await coord.charge_controller._async_tick()
        limit.assert_awaited_with(KATHREIN, target_w=7900.0, active=True, duration_s=0)
        # kW sensors are converted
        hass.states.async_set("sensor.grid_power", "0.1", {"unit_of_measurement": "kW"})
        assert coord.charge_controller.grid_power_w() == 100.0

        # fast -> lift the limit
        await hass.services.async_call("select", "select_option",
                                       {"entity_id": mode, "option": "fast"}, blocking=True)
        limit.assert_awaited_with(KATHREIN, active=False)

        # off -> 0 W
        await hass.services.async_call("select", "select_option",
                                       {"entity_id": mode, "option": "off"}, blocking=True)
        limit.assert_awaited_with(KATHREIN, target_w=0.0, active=True, duration_s=0)

        # manual 7 kW
        await hass.services.async_call("number", "set_value",
                                       {"entity_id": eid("number", KATHREIN, "charge_power_manual"), "value": 7000},
                                       blocking=True)
        await hass.services.async_call("select", "select_option",
                                       {"entity_id": mode, "option": "manual"}, blocking=True)
        limit.assert_awaited_with(KATHREIN, target_w=7000.0, active=True, duration_s=0)

        # back to auto releases once
        limit.reset_mock()
        await hass.services.async_call("select", "select_option",
                                       {"entity_id": mode, "option": "auto"}, blocking=True)
        limit.assert_awaited_once_with(KATHREIN, active=False)
        limit.reset_mock()
        await coord.charge_controller._async_tick()
        limit.assert_not_awaited()  # nothing more to say in auto

        # failsafe number writes through
        await hass.services.async_call("number", "set_value",
                                       {"entity_id": eid("number", KATHREIN, "lpc_failsafe_power"), "value": 11000},
                                       blocking=True)
        failsafe.assert_awaited_with(KATHREIN, power_w=11000.0)

        # set_device_limit service by device id
        from homeassistant.helpers import device_registry as dr
        dev = dr.async_get(hass).async_get_device_by_identifier(("eebus", f"remote_{KATHREIN}"), entry.entry_id)
        await hass.services.async_call("eebus", "set_device_limit",
                                       {"device_id": dev.id, "value_w": 4200, "active": True, "duration_s": 7200},
                                       blocking=True)
        limit.assert_awaited_with(KATHREIN, production=False, target_w=4200.0, active=True, duration_s=7200.0)

        current.assert_not_awaited()  # LPC box: never OPEV
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_mode_restored_after_restart(hass: HomeAssistant) -> None:
    from homeassistant.core import State
    from pytest_homeassistant_custom_component.common import mock_restore_cache
    mock_restore_cache(hass, [State("select.kathrein_ac40_4ff37d1e_charge_mode", "min_pv")])
    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": None})
    entry.add_to_hass(hass)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(STATE))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        ent = er.async_get(hass)
        mode = ent.async_get_entity_id("select", "eebus", f"{BRIDGE}_{KATHREIN}_charge_mode")
        assert mode == "select.kathrein_ac40_4ff37d1e_charge_mode", mode
        assert hass.states.get(mode).state == "min_pv"
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_eg_control_disabled_repair(hass: HomeAssistant) -> None:
    """KATHREIN without enable_eg_control: classified, repair issue, no charge mode."""
    from homeassistant.helpers import issue_registry as ir

    state = copy.deepcopy(STATE)
    state["service"]["features"] = {"eg_control": False, "ev": True}
    # without Energy Guard the bridge only knows the device from discovery
    state["devices"][KATHREIN] = {
        "category": "wallbox", "device_type": "ChargingStation", "entity_types": ["EVSE"],
    }
    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": None})
    entry.add_to_hass(hass)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        ent = er.async_get(hass)
        issues = ir.async_get(hass)
        issue_id = f"eg_control_disabled_{entry.entry_id}"

        category = ent.async_get_entity_id("sensor", "eebus", f"{BRIDGE}_{KATHREIN}_remote_category")
        assert hass.states.get(category).state == "wallbox"
        assert hass.states.get(category).attributes["device_type"] == "ChargingStation"
        assert ent.async_get_entity_id("select", "eebus", f"{BRIDGE}_{KATHREIN}_charge_mode") is None
        issue = issues.async_get_issue("eebus", issue_id)
        assert issue is not None
        assert "KATHREIN AC40" in issue.translation_placeholders["devices"]

        # user enables enable_eg_control -> bridge reports LPC support
        state["service"]["features"]["eg_control"] = True
        state["devices"][KATHREIN] = copy.deepcopy(STATE["devices"][KATHREIN])
        await hass.data["eebus"][entry.entry_id].async_refresh()
        await hass.async_block_till_done()
        assert issues.async_get_issue("eebus", issue_id) is None
        assert ent.async_get_entity_id("select", "eebus", f"{BRIDGE}_{KATHREIN}_charge_mode")
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_simulate_grid_limit_service(hass: HomeAssistant) -> None:
    from homeassistant.exceptions import HomeAssistantError

    from custom_components.eebus.api import EebusApiError

    state = copy.deepcopy(STATE)
    state["lpc"] = {"supported": True, "simulated": True, "limit": {"active": True, "value_w": 4200}}
    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": None})
    entry.add_to_hass(hass)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
        patch("custom_components.eebus.api.EebusApiClient.simulate_grid_limit", AsyncMock()) as sim,
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()

        await hass.services.async_call("eebus", "simulate_grid_limit", {}, blocking=True)
        sim.assert_awaited_with(4200.0, True, 900.0)  # defaults: 4.2 kW for 15 min
        await hass.services.async_call("eebus", "simulate_grid_limit",
                                       {"value_w": 6000, "duration_s": 60}, blocking=True)
        sim.assert_awaited_with(6000.0, True, 60.0)
        await hass.services.async_call("eebus", "simulate_grid_limit", {"active": False}, blocking=True)
        sim.assert_awaited_with(4200.0, False, 900.0)

        simulated = er.async_get(hass).async_get_entity_id("binary_sensor", "eebus", f"{BRIDGE}_lpc_simulated")
        assert simulated, "simulation sensor missing"
        assert hass.states.get(simulated).state == "on"
        limit = er.async_get(hass).async_get_entity_id("binary_sensor", "eebus", f"{BRIDGE}_consumption_limit_active")
        assert hass.states.get(limit).state == "on"

        # a refusal of the bridge surfaces as an error
        sim.side_effect = EebusApiError("a real control box is connected; simulation refused")
        with pytest.raises(HomeAssistantError, match="simulation refused"):
            await hass.services.async_call("eebus", "simulate_grid_limit", {}, blocking=True)
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_ui_cleanup(hass: HomeAssistant) -> None:
    """MOI inverters get no MPC duplicates; limit status is a plain on/off with the value."""
    state = copy.deepcopy(STATE)
    kostal = state["devices"][KOSTAL]
    kostal["use_cases"] = ["eg_lpc", "eg_lpp", "mpc", "moi", "mob"]
    kostal["inverter_ac"] = {"supported": True, "power_w": 0.0, "yield_total_wh": 28005680.0}
    kostal["control"]["lpc_applied_w"] = 500.0
    kostal["battery"] = {"supported": True, "state_of_charge_pct": 69.0, "state_of_energy_wh": 3047.04,
                         "usable_capacity_wh": 4416.0, "cycle_count": 1357.0, "voltage_v": 263.7,
                         "current_a": 0.0, "temperature_c": 27.5}
    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": None})
    entry.add_to_hass(hass)
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(state))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        ent = er.async_get(hass)

        def eid(domain, key):
            return ent.async_get_entity_id(domain, "eebus", f"{BRIDGE}_{KOSTAL}_{key}")

        # MPC copies are not created for an inverter that reports MOI
        assert eid("sensor", "remote_power") is None
        assert eid("sensor", "remote_energy_produced") is None
        assert hass.states.get(eid("sensor", "remote_inverter_yield_total")).state == "28005680.0"

        # MOB battery values of the KOSTAL
        assert hass.states.get(eid("sensor", "remote_battery_energy_stored")).state == "3047.04"
        assert hass.states.get(eid("sensor", "remote_battery_capacity")).state == "4416.0"
        assert hass.states.get(eid("sensor", "remote_battery_cycles")).state == "1357.0"
        assert hass.states.get(eid("sensor", "remote_battery_voltage")).state == "263.7"

        limited = hass.states.get(eid("binary_sensor", "lpc_limited"))
        assert limited.state == "on" and limited.attributes["limit_w"] == 500.0
        assert "device_class" not in limited.attributes  # plain on/off, no "running"
        feed_in = hass.states.get(eid("binary_sensor", "lpp_limited"))
        assert feed_in.state == "off"
        # the numeric "applied" sensors are disabled by default
        assert ent.async_get(eid("sensor", "lpc_applied")).disabled_by is er.RegistryEntryDisabler.INTEGRATION
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_diagnostics(hass: HomeAssistant) -> None:
    from custom_components.eebus.api import EebusApiError
    from custom_components.eebus.diagnostics import async_get_config_entry_diagnostics

    entry = MockConfigEntry(domain="eebus", unique_id=BRIDGE,
                            data={"host": "127.0.0.1", "port": 7050, "token": "secret"})
    entry.add_to_hass(hass)
    dump = {"version": "0.4.1-dev", "state": {}, "spine": [{"ski": KOSTAL, "entities": []}]}
    with (
        patch("custom_components.eebus.api.EebusApiClient.get_state",
              AsyncMock(side_effect=lambda: copy.deepcopy(STATE))),
        patch("custom_components.eebus.api.EebusApiClient.listen", _never),
        patch("custom_components.eebus.api.EebusApiClient.get_diagnostics", AsyncMock(return_value=dump)) as diag,
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()

        result = await async_get_config_entry_diagnostics(hass, entry)
        assert result["entry"]["data"]["token"] == "**REDACTED**"
        assert result["bridge"]["spine"][0]["ski"] == KOSTAL

        # older bridge without the endpoint: state only, no failure
        diag.side_effect = EebusApiError("HTTP 404")
        result = await async_get_config_entry_diagnostics(hass, entry)
        assert "HTTP 404" in result["bridge"]["error"]
        assert result["bridge"]["state"]["service"]["ski"] == BRIDGE
        assert await hass.config_entries.async_unload(entry.entry_id)


async def test_cards_registered_once(hass: HomeAssistant) -> None:
    from unittest.mock import MagicMock

    from custom_components.eebus.cards import CARDS_URL, async_register_cards

    # without the http component nothing is registered (and nothing fails)
    hass.http = None
    await async_register_cards(hass)

    hass.http = MagicMock()
    hass.http.async_register_static_paths = AsyncMock()
    with patch("homeassistant.components.frontend.add_extra_js_url") as add_js:
        await async_register_cards(hass)
        await async_register_cards(hass)  # second entry / reload: no duplicate
    hass.http.async_register_static_paths.assert_awaited_once()
    config = hass.http.async_register_static_paths.await_args.args[0][0]
    assert config.url_path == CARDS_URL and config.path.endswith("frontend/eebus-cards.js")
    import os
    assert os.path.isfile(config.path)
    add_js.assert_called_once()
    assert add_js.call_args.args[1].startswith(f"{CARDS_URL}?v=")


async def test_config_flow_explains_missing_bridge(hass: HomeAssistant) -> None:
    from homeassistant import config_entries
    from homeassistant.data_entry_flow import FlowResultType

    from custom_components.eebus.api import EebusApiError

    result = await hass.config_entries.flow.async_init("eebus", context={"source": config_entries.SOURCE_USER})
    assert result["type"] is FlowResultType.FORM
    with patch("custom_components.eebus.api.EebusApiClient.health",
               AsyncMock(side_effect=EebusApiError("connection refused"))):
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {"host": "192.0.2.10", "port": 7050, "token": ""})
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "cannot_connect"}

    with (
        patch("custom_components.eebus.api.EebusApiClient.health", AsyncMock(return_value={"ski": BRIDGE})),
        patch("custom_components.eebus.async_setup_entry", AsyncMock(return_value=True)),
    ):
        result = await hass.config_entries.flow.async_configure(
            result["flow_id"], {"host": "192.0.2.10", "port": 7050, "token": ""})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["result"].unique_id == BRIDGE


def test_setup_texts_mention_the_addon() -> None:
    import json
    from pathlib import Path

    base = Path(__file__).resolve().parents[1] / "custom_components" / "eebus"
    for name in ("strings.json", "translations/en.json", "translations/de.json"):
        data = json.loads((base / name).read_text())
        assert "EEBUS Bridge" in data["config"]["error"]["cannot_connect"], name
        assert "HACS" in data["config"]["step"]["user"]["description"], name
