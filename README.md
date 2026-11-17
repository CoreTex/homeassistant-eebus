# EEBUS for Home Assistant

Use **EEBUS** use cases in Home Assistant to monitor and control your energy
devices. This repository ships **two coupled parts**:

1. **`eebus_bridge`** – a Home Assistant **add-on** (Go service based on
   [`enbility/eebus-go`](https://github.com/enbility/eebus-go)) that speaks
   SHIP/SPINE on your LAN and exposes a local REST + WebSocket API.
2. **`custom_components/eebus`** – a **custom integration** (installable via
   HACS) that turns the add-on's data into native Home Assistant entities and
   services.

Everything is **generic**: no network-, vendor- or device-specific data is baked
in. You provide identities and trust at runtime, so the whole community can use it.

## Why two parts?

`eebus-go` is written in Go and EEBUS requires SHIP (mDNS discovery + TLS with
certificate/SKI pairing). Home Assistant entities are Python. The add-on runs the
Go stack in its own container; the integration creates entities and talks to it.

```
  Grid box / §14a controller ─┐                ┌─ Inverter / Battery
        (Energy Guard)        │   EEBUS/SHIP   │   (Controllable System,
                              ▼   (LAN, TLS)   ▼    Monitored Unit)
                     ┌───────────────────────────────┐
                     │   eebus_bridge  (Go add-on)    │
                     │   CS · EG · MA · VABD · VAPD   │
                     └───────────────┬───────────────┘
                                     │  REST + WebSocket (local API)
                                     ▼
                     ┌───────────────────────────────┐
                     │ custom_components/eebus (Python)│
                     │  sensors · binary_sensors ·    │
                     │  numbers · switches · services │
                     └───────────────────────────────┘
```

## Supported use cases

| User use case | EEBUS implementation | Role of Home Assistant |
| --- | --- | --- |
| **LPC** – Limitation of Power Consumption | `cs/lpc` (+ `eg/lpc`) | Receives & **holds** consumption limits; can also send them |
| **LPP** – Limitation of Power Production | `cs/lpp` (+ `eg/lpp`) | Receives & **holds** production limits; can also send them |
| **MPC** – Monitoring of Power Consumption | `ma/mpc` | Reads power, energy, current, voltage, frequency |
| **MOI** – Monitoring of Inverter | `ma/moi`* (+ `cem/vapd`) | Reads inverter AC power, energy, yield, current, voltage, frequency |
| **MOB** – Monitoring of Battery | `ma/mob`* (+ `cem/vabd`) | Reads battery power, state of charge/health, energy, temperature |

\* **MOI/MOB** are not in upstream `eebus-go` yet. The add-on builds against
the fork [`CoreTex/eebus-go`](https://github.com/CoreTex/eebus-go/tree/feature/uc-ma-moi-mob)
(upstream `dev` + MOI/MOB), which is intended to be contributed upstream.
The aggregated VAPD/VABD use cases are supported as well.

## Installation

### 1. Add-on (Home Assistant OS / Supervised)

1. **Settings → Add-ons → Add-on store → ⋮ → Repositories** and add:
   `https://github.com/CoreTex/homeassistant-eebus`
2. Install **EEBUS Bridge**, review the options, and **Start** it.
3. Open the **Log** and note the printed **Local SKI**. Trust this SKI on your
   grid box / inverter, and put the other device's SKI into `trusted_skis` /
   `inverter_ski` (or enable `auto_accept_trust` while pairing).

See [`eebus_bridge/DOCS.md`](eebus_bridge/DOCS.md) for all options and the
"try it without hardware" walkthrough.

### 2. Integration (HACS)

1. In **HACS → ⋮ → Custom repositories**, add this repo URL with category
   **Integration**, then install **EEBUS**.
2. **Settings → Devices & Services → Add Integration → EEBUS**.
3. Enter the add-on API address. Because the add-on uses host networking, use
   your **Home Assistant host IP** and the `api_port` (default `7050`); add the
   `api_token` if you set one.

## Operating concept

**One HA device per EEBUS device.** The bridge is the hub; every inverter,
wallbox, heat pump or meter it knows becomes its own device underneath it, with
entities created *by capability* - a device only gets the entities for data it
actually reports (see the device classes in the add-on docs).

| Level | Entities |
| --- | --- |
| **Bridge** | received §14a/LPP limits, leading-inverter values, **site** totals (PV, battery, grid, charging, heat pumps), **PV surplus**, §14a distribution |
| **Every device** | *Connected*, *Trusted* (pair / forget) |
| **Inverter** | power, energy, PV, battery; *Consumption/Production limit* + switches |
| **Wallbox** | **Charge mode**, *Charge power (manual)*, limit, failsafe; with vehicle data: plugged in, charge state, charging power, energy, SoC, current |
| **Heat pump** | power, energy; *Consumption limit* (§14a dimming) + failsafe |
| **Grid meter** | grid power, feed-in/consumption energy, feed-in limitation |

**Responsibilities are split on purpose:** safety-relevant limits (§14a share,
failsafe) are enforced by the add-on itself, so they keep working while Home
Assistant restarts. Comfort logic (charge modes, PV surplus) runs in Home
Assistant, where any grid sensor can be used.

### Charge modes (per wallbox)

| Mode | Behaviour |
| --- | --- |
| `auto` | default - the bridge does not intervene (wallbox / §14a only) |
| `off` | do not charge |
| `pv` | PV surplus only; starts after the *start delay* once the surplus reaches the minimum power, rides through dips at minimum power for the *stop delay* |
| `min_pv` | always at minimum power, more when there is surplus |
| `fast` | full power |
| `manual` | fixed power from *Charge power (manual)* |

Surplus = wallbox draw − grid power − reserve (grid power positive = import).
Configure the grid sensor under **Settings → Devices & Services → EEBUS →
Configure**; without one, a grid meter connected via EEBUS is used. Wallboxes
that report the vehicle are controlled by charging current (OPEV), §14a-only
wallboxes such as the KATHREIN AC40 by a power limit (requires
`enable_eg_control` in the add-on). Several wallboxes in a PV mode share the
surplus equally.

## Automations

**Cheap night charging, PV during the day** (dynamic tariff or fixed times):

```yaml
automation:
  - alias: Wallbox fast at night
    trigger:
      - platform: time
        at: "23:00:00"
    action:
      - service: select.select_option
        target:
          entity_id: select.kathrein_ac40_4ff37d1e_charge_mode
        data:
          option: fast
  - alias: Wallbox PV during the day
    trigger:
      - platform: time
        at: "06:00:00"
    action:
      - service: select.select_option
        target:
          entity_id: select.kathrein_ac40_4ff37d1e_charge_mode
        data:
          option: pv
```

**Dim the heat pump for two hours** (e.g. during a price peak):

```yaml
- service: eebus.set_device_limit
  data:
    device_id: <your heat pump device>
    value_w: 4200
    active: true
    duration_s: 7200
```

**Test the §14a chain without a control box** (add-on option
`enable_lpc_simulation`; with `distribute_lpc` the limit is shared among the
wallboxes and heat pumps and lifted after the duration):

```yaml
- service: eebus.simulate_grid_limit
  data:
    value_w: 4200
    duration_s: 900
```

**Notify when the grid operator limits the home (§14a):**

```yaml
automation:
  - alias: §14a limit active
    trigger:
      - platform: state
        entity_id: binary_sensor.eebus_bridge_consumption_limit_active
        to: "on"
    action:
      - service: notify.notify
        data:
          message: >-
            Grid operator limit: {{ states('sensor.eebus_bridge_consumption_limit') }} W
```

Services: `eebus.set_consumption_limit`, `eebus.set_production_limit` (leading
inverter), `eebus.set_device_limit` (any device), `eebus.trust_device`,
`eebus.forget_device`, `eebus.simulate_grid_limit` (testing).

## Development

Build & test the Go service locally:

```bash
cd eebus_bridge/src
go build ./...
go vet ./...
EEBUS_DATA_DIR=/tmp/eebus ./...   # see DOCS.md for stand-alone run
```

Check the integration:

```bash
python -m ruff check --config ruff.toml custom_components
pip install --prefer-binary -r tests/requirements.txt
python -m pytest -q -o asyncio_mode=auto tests
```

## License

Apache-2.0. See [LICENSE](LICENSE). Built on the Apache-2.0 licensed
`enbility/eebus-go`, `ship-go` and `spine-go` libraries.
