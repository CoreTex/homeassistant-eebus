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
| **MOI** – Monitoring of Inverter | `cem/vapd`* | Reads PV power, yield, nominal peak |
| **MOB** – Monitoring of Battery | `cem/vabd`* | Reads battery power, state of charge, energy |

\* **MOI/MOB caveat:** `eebus-go` does not yet provide dedicated *Monitoring of
Inverter*/*Battery* use cases. This project uses the library's **VAPD** and
**VABD** use cases, which expose the same underlying inverter/battery data.
**LPC, LPP and MPC are first-class.**

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

## Using it in automations

The core "hold the limit" feature surfaces as:

- `binary_sensor.eebus_bridge_consumption_limit_active` – on while a limit applies
- `sensor.eebus_bridge_consumption_limit` – the limit value in watts

Example: shed a load while a consumption limit is active.

```yaml
automation:
  - alias: Curtail wallbox during EEBUS consumption limit
    trigger:
      - platform: state
        entity_id: binary_sensor.eebus_bridge_consumption_limit_active
        to: "on"
    action:
      - service: number.set_value
        target:
          entity_id: number.wallbox_charging_limit
        data:
          value: "{{ states('sensor.eebus_bridge_consumption_limit') | float(0) }}"
```

Services: `eebus.set_consumption_limit`, `eebus.set_production_limit`,
`eebus.trust_device`.

## Entities (overview)

- **Sensors:** consumption/production limit (+ duration, nominal max), active
  power, per-phase power/current/voltage, energy consumed/produced, frequency,
  inverter power/PV yield/nominal peak, battery power/SoC/energy.
- **Binary sensors:** consumption/production limit active, EEBUS connected, LPC
  heartbeat.
- **Numbers:** failsafe consumption/production power & duration, nominal max,
  and (when Energy-Guard control is enabled) inverter consumption/production
  limits.
- **Switches:** activate/deactivate the Energy-Guard inverter limits.

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
python -m py_compile custom_components/eebus/*.py
```

## License

Apache-2.0. See [LICENSE](LICENSE). Built on the Apache-2.0 licensed
`enbility/eebus-go`, `ship-go` and `spine-go` libraries.
