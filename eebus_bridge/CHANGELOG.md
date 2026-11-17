# Changelog

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
