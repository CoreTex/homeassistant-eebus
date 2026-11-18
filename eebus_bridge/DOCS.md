# EEBUS Bridge

The EEBUS Bridge makes Home Assistant the **energy manager** of your EEBUS
devices. It speaks EEBUS (SHIP/SPINE) on your network and is used together
with the **EEBUS integration** (HACS), which creates the devices, entities and
actions in Home Assistant.

What it does:

- **Pairs and classifies** inverters, batteries, wallboxes, heat pumps, smart
  meters and the grid operator's control box.
- **Monitors** power, energy, PV, battery, inverter AC, grid meter, wallbox and
  vehicle data – whatever a device reports.
- **Receives grid operator limits** (§14a consumption, feed-in) and ends them
  when their duration is over.
- **Controls devices** (option *Control devices*): consumption and feed-in
  limits, failsafe values, wallbox charging, and **shares a grid operator
  limit by demand** among your controllable devices.

Safety-relevant logic (holding and sharing limits, expiry, failsafe) runs in
this add-on and keeps working while Home Assistant restarts.

## Setup

1. Start the add-on and open the **Log**. Note the **Local SKI**.
2. **Pair each device:** enter the bridge's SKI in the EEBUS settings of the
   device, and add the device's SKI to *Trusted devices* below (or switch on
   *Accept every pairing request* while pairing, then off again). The bridge
   refuses devices it does not trust – the device then reports "pairing
   failed".
3. Install the **EEBUS integration** via HACS and add it with your **Home
   Assistant host IP** and the API port (default `7050`). The add-on uses the
   host network, so the add-on hostname does not work.
4. For control features switch on **Control devices (Energy Guard)** and set
   the **Leading inverter**.

Devices trusted in Home Assistant (*Trusted* switch, action
`eebus.trust_device`) are stored in `/data/trusted_skis.json` and survive
restarts.

## Options

Every option has a name and help text on the configuration page. Overview:

| Group | Options |
| --- | --- |
| **Devices & pairing** | Leading inverter, trusted devices, accept every pairing request |
| **Control** | Control devices (Energy Guard), share grid operator limit (§14a), include storage in §14a sharing, allow §14a simulation |
| **Grid operator limits** | Receive consumption / feed-in limits, accept automatically, nominal consumption / feed-in power |
| **Monitoring** | Power (MPC), inverter (MOI, VAPD), battery (MOB, VABD), grid meter (MGCP), wallbox and vehicle |
| **Advanced** | Name, model and serial on the EEBUS network, ports, API token, log level |

## Device classes

The bridge classifies every device from its EEBUS device type, entity types
and use cases, right after pairing:

| Class | What the bridge can do |
| --- | --- |
| **Inverter** | Monitoring (MPC, MOI, VAPD) and battery (MOB, VABD); consumption and feed-in limits |
| **Wallbox** | §14a power limit (LPC); with EVSECC/EVCC/EVCEM/EVSOC/OPEV also vehicle state, charging power, state of charge and charging current |
| **Heat pump** | Monitoring (MPC); §14a dimming (LPC) |
| **Grid meter** | Grid power (positive = import), feed-in/consumption energy, feed-in limitation |
| **Control box** | Sends the grid operator's limits to the bridge |

**Several inverters:** the *Leading inverter* feeds the bridge-level inverter
values and the bridge-level inverter limits; without it the first inverter is
used (never a wallbox, heat pump or meter). Every inverter keeps its own
values and controls, and the site totals add up all inverters.

**Limits per device:** with *Control devices*, every device that accepts limits
can get a consumption and/or feed-in limit. The bridge always applies the
lower of your limit and its §14a share.

**Failsafe:** a device that received a limit from the bridge falls back to its
failsafe power for the failsafe duration when the bridge's heartbeat stops
(e.g. while the add-on restarts). Set it per device – for a wallbox, the power
it may draw during such an outage.

## §14a: sharing a grid operator limit

With **Share grid operator limit**, an active consumption limit from the
control box is shared among the connected wallboxes and heat pumps – and,
with **Include storage**, among inverters whose battery is registered under
§14a (the limit then restricts charging the battery from the grid).

The limit is shared **by demand**: a device with a measured consumption gets
its demand × 1.25 + 500 W, devices without a measurement (e.g. a §14a-only
wallbox) share the rest. Shares are re-evaluated on new measurements (at most
every 10 s, 100 W steps); the sum never exceeds the limit. Example with
4200 W: idle storage 500 W, wallbox 3700 W; storage charging with 3 kW and the
wallbox 2100 W each. The shares are removed when the limit ends.

**Testing without a control box:** with **Allow §14a simulation** the action
*Simulate grid operator limit* (`eebus.simulate_grid_limit`) injects a limit
as if the control box had sent it and runs through the whole chain. It is
refused while a real control box is connected or a real limit is active, so
it can never override or lift a genuine limit. A simulated or self-set limit
is no substitute for §14a control by the grid operator.

## Supported EEBUS use cases

LPC / LPP (as controllable system and as Energy Guard), MPC, MOI, MOB, VAPD,
VABD, MGCP, EVSECC, EVCC, EVCEM, EVSOC, OPEV. MOI and MOB come from the
[`CoreTex/eebus-go`](https://github.com/CoreTex/eebus-go/tree/feature/uc-ma-moi-mob)
fork, which is intended to be contributed upstream.

## Troubleshooting

- **"Pairing failed" on a device:** trust its SKI here or with the *Trusted*
  switch, and enter the bridge's SKI on the device.
- **Controls missing in Home Assistant:** switch on *Control devices*; update
  the integration in HACS and restart Home Assistant. *Settings → Repairs*
  explains missing settings.
- **Log lines `operation is not supported on function electricalConnection…`**
  are harmless: a device does not offer an optional read.
- For details set the log level to `debug`.

## Local API

Used by the integration; documented for advanced users. If an API token is
set, send `Authorization: Bearer <token>` (or `?token=` for the WebSocket).

| Endpoint | Purpose |
| --- | --- |
| `GET /api/health` | `{status, ski, version}` (no token needed) |
| `GET /api/state` | Full state: `service` (incl. enabled `features`), `connections`, `devices` (per SKI: `category`, `use_cases`, `mpc`, `inverter`, `inverter_ac`, `battery`, `grid`, `ev`, `control`), `site` totals, `distribution` (§14a shares), received `lpc`/`lpp` limits |
| `GET /api/ws` | WebSocket, pushes the full state on every change |
| `POST /api/devices/{ski}/lpc` · `/lpp` | `{target_w?, active?, duration_s?}` – stage and/or apply a device limit |
| `POST /api/devices/{ski}/failsafe` | `{power_w?, duration_s?}` – device failsafe |
| `POST /api/devices/{ski}/ev_current` | `{current_a}` (A per phase, 0 = pause) or `{release: true}` |
| `POST /api/lpc/simulate` | `{value_w, active, duration_s}` – simulated grid operator limit |
| `POST /api/lpc/failsafe` · `/api/lpp/failsafe` | `{power_w?, duration_s?}` – failsafe reported to the grid operator |
| `POST /api/lpc/nominal_max` · `/api/lpp/nominal_max` | `{value_w}` – nominal power reported to the grid operator |
| `POST /api/lpc/limit` · `/api/lpp/limit` | `{value_w, active, duration_s}` – limit the leading inverter |
| `POST /api/lpc/target` · `/api/lpp/target` | `{value_w, duration_s?}` – stage a leading-inverter limit |
| `POST /api/lpc/activate` · `/api/lpp/activate` | `{active}` – apply / remove the staged leading-inverter limit |
| `GET /api/pairing`, `POST /api/pairing/trust` · `/forget` | `{ski}` – pairing |
