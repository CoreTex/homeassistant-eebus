# Changelog

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
