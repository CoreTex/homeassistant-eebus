# EEBUS Bridge

This add-on runs an EEBUS (SHIP/SPINE) service based on
[`enbility/eebus-go`](https://github.com/enbility/eebus-go) and exposes its state
and controls over a small local REST + WebSocket API. The companion
**EEBUS** custom integration turns that API into Home Assistant entities.

It is fully generic: nothing about your network, vendor or devices is hard-coded.
Everything is supplied through the options below and at runtime via pairing.

## What it does

The bridge presents itself on the LAN as a single EEBUS **HEMS / CEM** device and
supports these use cases simultaneously:

| Use case | EEBUS role | What you get |
| --- | --- | --- |
| **LPC** – Limitation of Power Consumption | Controllable System | Receives consumption limits (e.g. §14a EnWG grid box) and *holds* them while active |
| **LPP** – Limitation of Power Production | Controllable System | Receives production limits and holds them |
| **LPC / LPP** | Energy Guard *(optional)* | Sends limits to your inverter to curtail it |
| **MPC** – Monitoring of Power Consumption | Monitoring Appliance | Power, energy, current, voltage, frequency |
| **MOB** – Monitoring of Battery* | CEM (VABD) | Battery power, state of charge, charged/discharged energy |
| **MOI** – Monitoring of Inverter* | CEM (VAPD) | PV power, total yield, nominal peak power |

\* **Note on MOI/MOB:** `eebus-go` does not (yet) ship the dedicated *Monitoring
of Inverter* / *Monitoring of Battery* use cases. This add-on implements the
equivalent, library-supported **VAPD** (Visualization of Aggregated PV Data) and
**VABD** (Visualization of Aggregated Battery Data) use cases. Whether your
inverter exposes them depends on the device. LPC, LPP and MPC are first-class.

## Installation

1. Install and start this add-on.
2. Open the add-on **Log**. On first start it generates a certificate and prints
   the local **SKI** (Secure Key Identifier), e.g.
   `Local SKI : 047f4944...`. It is also written to
   `/addon_config` (or `/data/local_ski.txt`).
3. **Trust this SKI on the other EEBUS device** (your grid box / inverter). Most
   devices have an "EEBUS pairing" page where you enter or accept the partner SKI.
4. Copy the *other* device's SKI into the `trusted_skis` (and/or `inverter_ski`)
   option, or enable `auto_accept_trust` temporarily to pair, then disable it.
5. Install the **EEBUS** custom integration (see the repository README) and point
   it at this add-on's API.

## Pairing two ways

- **You add the partner**: put its SKI into `trusted_skis` / `inverter_ski`.
- **The partner adds you**: enable `auto_accept_trust` so incoming pairing
  requests are accepted, or use the `eebus.trust_device` service / the
  integration's pairing controls to trust a discovered SKI on demand.

The bridge aborts the SHIP handshake with any device whose SKI it does not
trust, so the partner reports "pairing failed" until you trust it here.

SKIs trusted at runtime (the integration's **Trusted** switch per device, the
`eebus.trust_device` service, or `auto_accept_trust`) are stored in
`/data/trusted_skis.json` and survive restarts. `eebus.forget_device` removes
them again.

## Device classes

The bridge is the home's energy manager (CEM). It classifies every paired
device from its SPINE device type, entity types and announced use cases, and
offers whatever that device supports:

| Class | Detected by | What the bridge can do |
| --- | --- | --- |
| **Inverter** (PV/hybrid) | device type `Inverter`, entities `Inverter`/`PVSystem`/`PVESHybrid`/`Battery…`, VAPD/VABD | MPC/PV/battery data; LPC + LPP limits (`enable_eg_control`) |
| **Wallbox** | device type `ChargingStation`, entities `EVSE`/`EV`, EV use cases | §14a power limit via LPC; with EVSECC/EVCC/EVCEM/EVSOC/OPEV also vehicle state, charging power, SoC and charging current |
| **Heat pump** | entities `HeatPumpAppliance`/`Compressor`/heating circuits | MPC data; §14a dimming via LPC |
| **Grid meter** | entity `GridConnectionPointOfPremises`, MGCP | grid power (positive = import), feed-in/consumption energy, feed-in limitation |
| **Control box** | device type `ElectricitySupplySystem`, sends LPC/LPP to us | the grid operator's limits arrive at the bridge (Controllable System) |

Not available (missing in `eebus-go`): heat-pump specific use cases such as
*Optimization of Self-Consumption by Heat Pump Compressor Flexibility*, hot
water or room temperature control, and the inverter/battery use cases MOI, MOB
and Control of Battery. Heat pumps can therefore be **dimmed** (LPC) and
**monitored** (MPC), but not boosted.

Real-world example: a **KATHREIN AC40** announces only `limitationOfPowerConsumption`
(Controllable System) on one EVSE entity. It reports no measurements, but the
bridge can limit its charging power in watts - enable `enable_eg_control`.

### Leading inverter (several inverters)

With more than one inverter, set `inverter_ski` to the **leading** one:

- it feeds the bridge's top-level power/battery/PV sensors and the
  bridge-level *Inverter … limit* controls (without `inverter_ski` the first
  inverter is used; wallboxes, heat pumps and meters are never chosen);
- every inverter keeps its own per-device sensors and limit controls;
- the *site* sensors add up all inverters (total PV, total battery power).

### Limits per device

With `enable_eg_control`, every device that accepts limits gets a
*Consumption limit* / *Production limit* number (staged value) and a matching
switch (apply / remove). The bridge always writes the **lower** of the user
limit and an active §14a share.

**Failsafe:** once a device received a limit from the bridge it falls back to
its *failsafe power* for the *failsafe duration* when the bridge's heartbeat
stops (e.g. while the add-on restarts). For a wallbox, set the failsafe power
to what it may draw during such an outage.

### §14a distribution (`distribute_lpc`)

When enabled and the grid operator's control box sends an active consumption
limit to the bridge, the bridge shares it **equally** among the connected
wallboxes and heat pumps: via LPC where supported, otherwise by capping the
wallbox charging current (OPEV). Inverters are not included. The share is
removed when the incoming limit ends. This runs inside the add-on, so it keeps
working while Home Assistant restarts.

## Options

| Option | Default | Description |
| --- | --- | --- |
| `log_level` | `info` | `error`, `info`, `debug`, `trace` |
| `api_port` | `7050` | Port of the local REST/WebSocket API |
| `api_token` | _(empty)_ | Optional bearer token protecting the API |
| `eebus_port` | `4715` | TCP port of the SHIP server |
| `device_brand` / `device_model` / `device_serial` | HomeAssistant / EEBUS-Bridge / _(host)_ | EEBUS identity advertised on the LAN |
| `enable_lpc` | `true` | Receive consumption limits (Controllable System) |
| `enable_lpp` | `true` | Receive production limits (Controllable System) |
| `enable_mpc` | `true` | Monitoring of Power Consumption |
| `enable_battery` | `true` | Battery monitoring (VABD ~ MOB) |
| `enable_inverter` | `true` | Inverter/PV monitoring (VAPD ~ MOI) |
| `enable_eg_control` | `false` | Act as Energy Guard: send LPC/LPP limits to devices (inverters, wallboxes, heat pumps) |
| `enable_mgcp` | `true` | Monitoring of the grid connection point (smart meter) |
| `enable_ev` | `true` | Wallbox/vehicle use cases (EVSECC, EVCC, EVCEM, EVSOC, OPEV) |
| `distribute_lpc` | `false` | Share an incoming §14a consumption limit among wallboxes and heat pumps |
| `lpc_nominal_max_w` | `0` | Seed the consumption nominal maximum (W); `0` = leave unset. Relevant for §14a. |
| `lpp_nominal_max_w` | `0` | Seed the production nominal maximum (W); `0` = leave unset. |
| `auto_accept_trust` | `false` | Auto-trust any remote that tries to pair |
| `auto_approve_limits` | `true` | Auto-accept incoming LPC/LPP write limits |
| `trusted_skis` | `[]` | Pre-trusted remote SKIs (40 hex chars each) |
| `inverter_ski` | _(empty)_ | SKI of the **leading** inverter (top-level sensors, bridge-level inverter limits) |

## API (for advanced users / the integration)

- `GET /api/health` – `{status, ski, version}` (no auth)
- `GET /api/state` – full state snapshot
- `GET /api/ws` – WebSocket, pushes the full snapshot on every change
- `POST /api/lpc/limit` / `POST /api/lpp/limit` – `{value_w, active, duration_s}` (Energy Guard → inverter, one-shot)
- `POST /api/lpc/target` / `POST /api/lpp/target` – `{value_w, duration_s?}` (stage a limit without applying it)
- `POST /api/lpc/activate` / `POST /api/lpp/activate` – `{active}` (apply / remove the staged target)
- `POST /api/lpc/failsafe` / `POST /api/lpp/failsafe` – `{power_w?, duration_s?}`
- `POST /api/lpc/nominal_max` / `POST /api/lpp/nominal_max` – `{value_w}`
- `GET /api/pairing`, `POST /api/pairing/trust` `{ski}`, `POST /api/pairing/forget` `{ski}`
- `POST /api/devices/{ski}/lpc` / `.../lpp` – `{target_w?, active?, duration_s?}` (stage and/or apply a device limit)
- `POST /api/devices/{ski}/failsafe` – `{power_w?, duration_s?}` (device failsafe)
- `POST /api/devices/{ski}/ev_current` – `{current_a}` (A per phase, 0 = pause) or `{release: true}`

`/api/state` contains `devices` (per SKI: `category`, `use_cases`, `mpc`,
`inverter`, `battery`, `grid`, `ev`, `control`), `site` (aggregates) and
`distribution` (§14a share-out).

If `api_token` is set, send `Authorization: Bearer <token>` (or `?token=` for the
WebSocket).

## Try it without hardware

You can drive a test LPC limit into the bridge using the `controlbox` example
from `eebus-go` (acting as a simulated Energy Guard / grid box). Trust each
other's SKIs, then have the control box send a consumption limit; the
`binary_sensor.*_consumption_limit_active` entity will turn on and
`sensor.*_consumption_limit` will show the value.

## Networking

The add-on uses **host network** so EEBUS mDNS discovery and direct TLS
connections work on your LAN. Because of this, point the integration at your
**Home Assistant host IP** (not the add-on slug hostname) on `api_port`.
