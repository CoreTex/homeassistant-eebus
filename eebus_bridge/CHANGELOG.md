# Changelog

## 0.6.1-dev

- Heat pumps offering OHPCF get the current (normally deactivated) LPC limit
  after connecting, so they accept the bridge as their energy manager (#7).

## 0.6.0-dev

- Heat pumps: optional consumption (OHPCF) – report the offer, start, pause,
  resume and stop it (`POST /api/devices/{ski}/flex`).

## 0.5.1-dev

- Documentation: full option reference and running the bridge in Docker
  without add-ons.

## 0.5.0-dev

- No add-on changes; version aligned with the integration (dashboard cards).

## 0.4.2-dev

- MOB: energy stored, usable capacity, load cycles, DC voltage and current;
  charged/discharged energy falls back to the DC counters (fork
  `CoreTex/eebus-go` updated).
- A stored energy above the usable capacity (KOSTAL: SoC% × capacity) is
  replaced by state of charge × capacity.
- Diagnostics: empty lists as `[]`.

## 0.4.1-dev

- **Diagnostics:** new `GET /api/diagnostics` returns the raw SPINE data the
  connected devices reported (entities, features, measurement descriptions
  and values, limits). The integration offers it via *Download diagnostics*
  (API token redacted) – no trace log needed to see what a device sends.
- The MOI yield is named *Inverter PV yield total* (verified against the
  KOSTAL yield counter).

## 0.4.0-dev

Clean-up release: settings, names and documentation.

- **Add-on settings** are grouped (devices & pairing, control, grid operator
  limits, monitoring, advanced) and every option has a name and help text
  (English, German). The newer options are no longer hidden as optional.
- **Integration:**
  - Clear names: *Grid operator limit …* for what the control box demands,
    *Consumption limit* / *Feed-in limit* for what you send to a device,
    *Leading inverter …* for bridge-level copies.
  - Status sensors show plain on/off (no "running" / "not running").
  - New *Consumption limited* / *Feed-in limited* per device (limit as
    attribute); the numeric *… (applied)* sensors are disabled by default.
  - Duplicates are disabled by default for new installations: bridge-level
    copies of the leading inverter, per-phase details, MPC power/energy on
    inverters that report MOI (identical values).
  - Options dialog with help texts.
- **Documentation** rewritten: features per device, quick start, settings,
  entities, charge modes, §14a, actions, automations, troubleshooting. The
  license note now correctly names the MIT-licensed eebus-go libraries.

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
