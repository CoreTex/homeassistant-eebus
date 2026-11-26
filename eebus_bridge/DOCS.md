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

The add-on and the integration are two separate parts: HACS installs only the
integration, the add-on comes from the add-on store. Without add-on support
(Home Assistant Container/Core) see
[Running the bridge without add-ons](#running-the-bridge-without-add-ons-docker).
4. For control features switch on **Control devices (Energy Guard)** and set
   the **Leading inverter**.

Devices trusted in Home Assistant (*Trusted* switch, action
`eebus.trust_device`) are stored in `/data/trusted_skis.json` and survive
restarts.

## Options

Every option has a name and help text on the add-on's configuration page.
Full reference (the keys are also used in `options.json` when the bridge runs
in Docker, see below):

| Option | Default | Description |
| --- | --- | --- |
| **Devices & pairing** | | |
| `inverter_ski` | _(empty)_ | Leading inverter (SKI): SKI of your main inverter (40 hex characters). It feeds the bridge-level inverter values and is the target of the bridge-level inverter limits. Leave empty to use the first inverter found. |
| `trusted_skis` | `[]` | Trusted devices (SKIs): SKIs of EEBUS devices that may pair with the bridge (inverter, wallboxes, heat pumps, control box). Devices can also be trusted in Home Assistant with the "Trusted" switch of each device. |
| `auto_accept_trust` | `false` | Accept every pairing request: Trust any device that asks to pair. Switch on only while pairing a new device, then off again; accepted devices stay trusted. |
| **Control** | | |
| `enable_eg_control` | `false` | Control devices (Energy Guard): Lets the bridge send consumption/production limits to devices: dim wallboxes and heat pumps (§14a), charge modes of LPC-only wallboxes, inverter limits, and start heat pumps that offer optional consumption (OHPCF, e.g. on PV surplus). Required for most control features. |
| `distribute_lpc` | `false` | Share grid operator limit (§14a): When the grid operator's control box limits the home, share that limit by demand among the connected wallboxes and heat pumps. |
| `distribute_lpc_inverters` | `false` | Include storage in §14a sharing: Also give inverters with a battery a share (limits charging the battery from the grid). Enable if your storage is registered with the grid operator under §14a. |
| `enable_lpc_simulation` | `false` | Allow §14a simulation (testing): Allows the service "Simulate grid operator limit" to test the whole §14a chain without a control box. Refused while a real control box is connected or a real limit is active. |
| **Grid operator limits** | | |
| `enable_lpc` | `true` | Receive consumption limits (LPC): The bridge accepts consumption limits from the grid operator's control box (§14a) and shows them in Home Assistant. |
| `enable_lpp` | `true` | Receive feed-in limits (LPP): The bridge accepts feed-in (production) limits from the grid operator's control box. |
| `auto_approve_limits` | `true` | Accept limits automatically: Accept incoming grid operator limits without confirmation (recommended; §14a limits must be followed). |
| `lpc_nominal_max_w` | `0` | Nominal consumption power (W): Maximum consumption of the controllable devices reported to the grid operator (§14a). 0 = not set. |
| `lpp_nominal_max_w` | `0` | Nominal feed-in power (W): Maximum feed-in power reported to the grid operator. 0 = not set. |
| **Monitoring** | | |
| `enable_mpc` | `true` | Power monitoring (MPC): Read power, energy, current and voltage of devices. |
| `enable_inverter` | `true` | Inverter monitoring (MOI, VAPD): Read inverter AC values and PV data. |
| `enable_battery` | `true` | Battery monitoring (MOB, VABD): Read battery power, state of charge, health and energy. |
| `enable_mgcp` | `true` | Grid meter monitoring (MGCP): Read grid power and energy from a smart meter or gateway. |
| `enable_ev` | `true` | Wallbox and vehicle (EV use cases): Vehicle state, charging power, state of charge and charging-current control for wallboxes that support it. |
| **Advanced** | | |
| `device_brand` | `HomeAssistant` | Brand shown to other devices: Name the bridge uses on the EEBUS network. |
| `device_model` | `EEBUS-Bridge` | Model shown to other devices: Model the bridge uses on the EEBUS network. |
| `device_serial` | _(empty)_ | Serial number shown to other devices: Leave empty for an automatic value. Do not use the serial number of another device. |
| `eebus_port` | `4715` | EEBUS port: TCP port of the EEBUS (SHIP) server. Change only on conflicts. |
| `api_port` | `7050` | API port: Port of the local API used by the Home Assistant integration. |
| `api_token` | _(empty)_ | API token: Optional password for the local API. Enter the same token in the integration. |
| `log_level` | `info` | Log level: Use debug or trace only for troubleshooting. |

SKIs are 40 hexadecimal characters. Options that are not set keep their
default.

## Device classes

The bridge classifies every device from its EEBUS device type, entity types
and use cases, right after pairing:

| Class | What the bridge can do |
| --- | --- |
| **Inverter** | Monitoring (MPC, MOI, VAPD) and battery (MOB, VABD); consumption and feed-in limits |
| **Wallbox** | §14a power limit (LPC); with EVSECC/EVCC/EVCEM/EVSOC/OPEV also vehicle state, charging power, state of charge and charging current |
| **Heat pump** | Monitoring (MPC); §14a dimming (LPC); optional consumption (OHPCF): start, pause, resume, stop – e.g. on PV surplus |
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

## Running the bridge without add-ons (Docker)

Add-ons need Home Assistant OS or Supervised. With **Home Assistant Container
or Core**, run the bridge as its own Docker container on a machine in the same
network as your EEBUS devices – ideally the one that runs Home Assistant. The
image is the same one the add-on uses:

- `ghcr.io/coretex/amd64-addon-eebus_bridge:<version>` (Intel/AMD)
- `ghcr.io/coretex/aarch64-addon-eebus_bridge:<version>` (ARM 64-bit, e.g.
  Raspberry Pi 4/5)

Use a fixed version (e.g. `0.6.6-dev`) and update it together with the
integration; `latest` always points to the newest build, including
pre-releases.

1. Create a data folder with an `options.json` (all keys in the table above;
   missing keys use their default):

   ```json
   {
     "inverter_ski": "",
     "trusted_skis": [],
     "auto_accept_trust": false,
     "enable_eg_control": false,
     "distribute_lpc": false,
     "distribute_lpc_inverters": false,
     "enable_lpc_simulation": false,
     "enable_lpc": true,
     "enable_lpp": true,
     "auto_approve_limits": true,
     "lpc_nominal_max_w": 0,
     "lpp_nominal_max_w": 0,
     "enable_mpc": true,
     "enable_inverter": true,
     "enable_battery": true,
     "enable_mgcp": true,
     "enable_ev": true,
     "device_brand": "HomeAssistant",
     "device_model": "EEBUS-Bridge",
     "device_serial": "",
     "eebus_port": 4715,
     "api_port": 7050,
     "api_token": "",
     "log_level": "info"
   }
   ```

2. Start the container with **host networking** (EEBUS needs mDNS and direct
   connections) and the data folder mounted to `/data`, where the certificate
   is stored – keep this folder, otherwise the bridge gets a new SKI and all
   devices have to be paired again:

   ```bash
   docker run -d --name eebus-bridge --restart unless-stopped \
     --network host \
     -v /opt/eebus-bridge:/data \
     ghcr.io/coretex/amd64-addon-eebus_bridge:0.6.6-dev
   ```

   or with Docker Compose:

   ```yaml
   services:
     eebus-bridge:
       image: ghcr.io/coretex/amd64-addon-eebus_bridge:0.6.6-dev
       container_name: eebus-bridge
       network_mode: host
       restart: unless-stopped
       volumes:
         - /opt/eebus-bridge:/data
   ```

3. The log (`docker logs eebus-bridge`) shows the **Local SKI**. Pair your
   devices as described above.
4. Add the EEBUS integration with the IP of the Docker host and port `7050`.

After changing `options.json`, restart the container. Docker Desktop on
Windows and macOS does not support host networking the same way; use a Linux
host for the bridge.

## Supported EEBUS use cases

LPC / LPP (as controllable system and as Energy Guard), MPC, MOI, MOB, VAPD,
VABD, MGCP, EVSECC, EVCC, EVCEM, EVSOC, OPEV, OHPCF. MOI and MOB come from the
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
- **What does a device really send?** In Home Assistant: *EEBUS → ⋮ →
  Download diagnostics*. It contains the raw EEBUS data of every connected
  device (entities, features, measurement descriptions and values), which is
  easier than searching a trace log.
- For details set the log level to `debug`.

## Local API

Used by the integration; documented for advanced users. If an API token is
set, send `Authorization: Bearer <token>` (or `?token=` for the WebSocket).

| Endpoint | Purpose |
| --- | --- |
| `GET /api/health` | `{status, ski, version}` (no token needed) |
| `GET /api/state` | Full state: `service` (incl. enabled `features`), `connections`, `devices` (per SKI: `category`, `use_cases`, `mpc`, `inverter`, `inverter_ac`, `battery`, `grid`, `ev`, `control`), `site` totals, `distribution` (§14a shares), received `lpc`/`lpp` limits |
| `GET /api/ws` | WebSocket, pushes the full state on every change |
| `GET /api/diagnostics` | State plus the raw SPINE data of every connected device (entities, features, latest data per function) |
| `POST /api/devices/{ski}/lpc` · `/lpp` | `{target_w?, active?, duration_s?}` – stage and/or apply a device limit |
| `POST /api/devices/{ski}/failsafe` | `{power_w?, duration_s?}` – device failsafe |
| `POST /api/devices/{ski}/ev_current` | `{current_a}` (A per phase, 0 = pause) or `{release: true}` |
| `POST /api/devices/{ski}/flex` | `{action}` = `start` / `pause` / `resume` / `stop` for a heat pump's optional consumption (OHPCF); `start_in_s` delays a start |
| `POST /api/lpc/simulate` | `{value_w, active, duration_s}` – simulated grid operator limit |
| `POST /api/lpc/failsafe` · `/api/lpp/failsafe` | `{power_w?, duration_s?}` – failsafe reported to the grid operator |
| `POST /api/lpc/nominal_max` · `/api/lpp/nominal_max` | `{value_w}` – nominal power reported to the grid operator |
| `POST /api/lpc/limit` · `/api/lpp/limit` | `{value_w, active, duration_s}` – limit the leading inverter |
| `POST /api/lpc/target` · `/api/lpp/target` | `{value_w, duration_s?}` – stage a leading-inverter limit |
| `POST /api/lpc/activate` · `/api/lpp/activate` | `{active}` – apply / remove the staged leading-inverter limit |
| `GET /api/pairing`, `POST /api/pairing/trust` · `/forget` | `{ski}` – pairing |

## Changelog

The add-on's *Changelog* tab lists the add-on changes. For both add-on and
integration see
[CHANGELOG.md](https://github.com/CoreTex/homeassistant-eebus/blob/dev/CHANGELOG.md).
