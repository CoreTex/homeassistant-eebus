# Changelog

## 0.3.3-dev

- **Fix:** a repeated identical limit (a control box re-sending its limit, or
  the simulation started again) now restarts the duration; before, it expired
  at the time of the first write.
- `service.features` also reports `distribute_lpc_inverters` and
  `lpc_simulation`; the log names inverters left out of the §14a share-out
  because `distribute_lpc_inverters` is off.

## 0.3.2-dev

- `distribute_lpc_inverters`: inverters with LPC (battery charging from the
  grid) take part in the §14a share-out, for storage registered under §14a.
- The §14a limit is now shared **by demand** instead of equally: measured
  consumers get demand × 1.25 + 500 W, the rest goes to devices without a
  measurement; re-evaluated on new measurements (at most every 10 s), with
  100 W steps and 200 W hysteresis. `distribution.shares` lists the share
  per device.

## 0.3.1-dev

- Devices are classified right after SPINE discovery, independent of the
  enabled use cases (a §14a-only wallbox was not recognised while
  `enable_eg_control` was off).
- The state reports the enabled add-on features (`service.features`); the
  integration raises a repair issue when wallboxes/heat pumps are found but
  `enable_eg_control` is off, and shows a per-device *Device type* sensor.
- **Fix:** an incoming consumption limit with a duration now ends when the
  duration is over (eebus-go keeps it active), so a §14a share no longer
  stays in force after the grid operator's limit ended.
- **§14a simulation** (`enable_lpc_simulation`, off by default): service
  `eebus.simulate_grid_limit` injects an incoming limit to test the whole
  chain; refused while a real control box is connected or a real limit is
  active.

## 0.3.0-dev

- **Device classes:** remotes are classified as inverter, wallbox, heat pump,
  grid meter or control box (from SPINE device type, entity types and use
  cases) and expose their capabilities (`devices.<ski>.category/use_cases`).
- **Wallboxes:** EVSECC, EVCC, EVCEM, EVSOC and OPEV (vehicle state, charging
  power/energy, SoC, current limits, charging current control). §14a-only
  wallboxes (e.g. KATHREIN AC40) are controlled via LPC.
- **Grid meter:** MGCP (grid power, feed-in/consumption energy, limitation
  factor). New option `enable_mgcp`, `enable_ev`.
- **Energy Guard per device:** LPC/LPP limits and failsafe values for every
  device that accepts them (`/api/devices/{ski}/lpc|lpp|failsafe`). The
  bridge-level inverter controls now act on the leading inverter
  (`inverter_ski`, else the first inverter - never a wallbox).
- **§14a distribution** (`distribute_lpc`, opt-in): an incoming consumption
  limit is shared equally among the connected wallboxes and heat pumps.
- **Site aggregates:** total PV, battery, grid, charging and heat-pump power.
- **MOI / MOB:** Monitoring of Inverter (AC power, energy, yield, current,
  voltage, frequency) and Monitoring of Battery (power, SoC, SoH, energy,
  temperature), e.g. for KOSTAL PLENTICORE G3. Built against the fork
  `CoreTex/eebus-go` (upstream `dev` + MOI/MOB), which also brings the newer
  upstream fixes; the result callbacks gained a message counter.

## 0.2.0-dev

- Multiple paired devices: measurements are kept per device (`devices` in
  `/api/state`). Only the primary device (`inverter_ski`, otherwise the first
  reporter) feeds the top-level `mpc`/`battery`/`inverter` blocks, so a wallbox
  reporting MPC no longer overwrites the inverter's values.
- Energy Guard: the control target is pinned to `inverter_ski` (or the first
  device offering LPC/LPP) and can no longer silently switch to a wallbox.
  Limit readbacks from other devices are ignored, and the target is cleared
  when its device disconnects.
- SKIs trusted at runtime (`trust_device`, auto-accept) are persisted in
  `/data/trusted_skis.json` and survive restarts; `forget` removes them.
- `connections` entries gain `primary` and `energy_guard_target` flags and are
  sorted by SKI; new top-level `primary_ski`.

## 0.1.1

- Add `lpc_nominal_max_w` / `lpp_nominal_max_w` options to seed the Controllable
  System nominal maximum power (relevant for §14a) persistently across restarts.
- Add-on store icon.

## 0.1.0

Initial release.

- EEBUS (SHIP/SPINE) service based on `enbility/eebus-go`.
- Controllable System: receive and hold **LPC** and **LPP** limits.
- Energy Guard (optional): send LPC/LPP limits to an inverter.
- Monitoring: **MPC** (power consumption), **VABD** (battery ~ MOB),
  **VAPD** (inverter/PV ~ MOI).
- Local REST + WebSocket API for the `eebus` Home Assistant integration.
- Certificate auto-generation and persistence in `/data`; SKI-based trust with
  optional auto-accept pairing.
