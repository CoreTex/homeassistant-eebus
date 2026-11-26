# Changelog

All notable changes to the **EEBUS Bridge add-on** and the **EEBUS
integration**. Since 0.2.0 both parts share one version number; update both
and restart Home Assistant after updating the integration.

`-dev` versions are pre-releases from the `dev` branch (add-on repository
`https://github.com/CoreTex/homeassistant-eebus#dev`, integration via HACS with
beta versions enabled).

## 0.6.4-dev – 2026-11-26

### Fixed
- **Bosch heat pumps stay in error 1240 after a bridge restart** (#7). 1240
  means "communication to the energy manager lost" (failsafe); the heat pump
  only leaves it on an *active* limit. Heat pumps the bridge manages (LPC +
  OHPCF) now get "no limit" as an active limit at their nominal maximum –
  after connecting and when you switch your own limit off. This does not
  restrict the heat pump. The deactivated limit is only the fallback.

## 0.6.3-dev – 2026-11-25

### Fixed
- **Heat pumps that reject every limit right after connecting** (Bosch, #7):
  the bridge now keeps retrying the consumption limit with a growing delay
  (15 s, 30 s, 1, 2, 4, 8 min) until it is accepted or the heat pump
  disconnects, instead of giving up after the second attempt.

## 0.6.2-dev – 2026-11-23

### Fixed
- **Bosch heat pumps: error 1240 after a bridge restart** (#7). Right after
  connecting the bridge could not send the limit yet ("data not available")
  and did not retry reliably, and some heat pumps reject a deactivated limit
  at that moment. The bridge now retries as soon as the heat pump's limit
  data has arrived and, if the deactivated limit is rejected, sends an active
  limit at the heat pump's nominal maximum (which does not limit anything).
  You no longer need to set a consumption limit by hand.

### Changed
- *Use PV surplus* logs a warning when no grid power value is available
  (select your grid power sensor in the integration options).

## 0.6.1-dev – 2026-11-20

### Fixed
- **Heat pumps with OHPCF (e.g. Bosch CS5800i/CS6800i) now get a consumption
  limit from the bridge** – a deactivated one unless you set a limit – right
  after they connect. Without it the heat pump waited for an energy manager
  that controls it, showed a communication error (Bosch 1240) and ignored
  *Start optional consumption* (#7). Other devices are not touched, since
  another energy manager may control them.

## 0.6.0-dev – 2026-11-20

### Added
- **Heat pumps: use PV surplus (OHPCF).** Heat pumps that announce optional
  consumption (e.g. Bosch CS5800i with K 40 RF) get an *Optional consumption*
  sensor (state, requested power), buttons to start, pause, resume and stop
  it, and a *Use PV surplus* switch that starts it automatically on surplus
  (same grid sensor, reserve and delays as PV-surplus charging; minimum run
  and pause times are respected). The heat pump card shows it with buttons
  (#4). Add-on API: `POST /api/devices/{ski}/flex`.
- **Energy consumed (calculated)** for heat pumps that report their power but
  no energy counter, for the energy dashboard (#4).

### Changed
- Sensors for values a device announces but never sends are no longer
  created (they stayed "unavailable" forever). They appear automatically once
  a value arrives. Entities created by older versions can be removed in the
  entity settings.
- **Diagnostics are anonymised:** SKIs become `bridge`, `device_1`, ...;
  device names, serial numbers, SPINE addresses, SHIP ID, QR code, host,
  certificate fingerprint and vehicle identifications are removed, so the
  file can be attached to an issue.

## 0.5.1-dev – 2026-11-19

### Documentation
- **Running the bridge without add-ons** (Home Assistant Container/Core):
  Docker and Docker Compose examples, complete `options.json`, image names per
  architecture (#2).
- The **full option reference** (key, default, description) is back in the
  add-on documentation (#3).
- README: notice that add-on and integration are two separate parts, with
  one-click buttons to add the add-on repository and to open the integration
  in HACS.

### Changed
- Setting up the integration explains that the EEBUS Bridge is a separate
  add-on, also when no bridge answers at the given address.

## 0.5.0-dev – 2026-11-18

### New
- **Dashboard cards**, shipped with the integration (no resource to add) and
  available under *Add card → EEBUS*, each with a visual editor:
  energy flow, inverter, battery, wallbox (charge mode switcher, manual power
  slider), heat pump, grid meter, and an automatic device card. Variants
  *compact*, *standard* and *detailed*, optional history graph, light and
  dark theme, English and German.

## 0.4.2-dev – 2026-11-18

### New
- **More battery values (MOB)**, found with *Download diagnostics* on a
  KOSTAL PLENTICORE G3: energy stored, usable capacity, load cycles, DC voltage
  and DC current.

### Fixed
- Battery charged/discharged energy also read from the DC counters
  (`dcChargeEnergy` / `dcDischargeEnergy`) – they showed "unavailable" on the
  KOSTAL.
- The KOSTAL reports the stored energy 100 times too high (state of charge in
  percent × capacity). The bridge detects a stored energy above the capacity
  and uses state of charge × capacity instead.
- Diagnostics list empty feature lists as `[]` instead of `null`.

## 0.4.1-dev – 2026-11-18

### New
- **Download diagnostics** (*EEBUS → ⋮ → Download diagnostics*): contains the
  raw EEBUS data of every connected device – entities, features, measurement
  descriptions and values, limits. No trace log needed to see what a device
  sends. The API token is redacted.
- Add-on endpoint `GET /api/diagnostics`.

### Changed
- The MOI yield sensor is called *Inverter PV yield total* (verified against
  the KOSTAL's own yield counter).

## 0.4.0-dev – 2026-11-18

Clean-up of settings, names and documentation.

### Changed
- **Add-on settings** grouped (devices & pairing, control, grid operator
  limits, monitoring, advanced); every option has a name and help text in
  English and German; no options hidden as "optional" any more.
- **Clear entity names:** *Grid operator limit …* (what the control box
  demands), *Consumption / Feed-in limit* (what you send to a device),
  *Leading inverter …* (bridge-level copies).
- Status sensors show plain on/off instead of "running / not running".
- New per device: *Consumption limited* / *Feed-in limited* with the limit as
  attribute.
- **Disabled by default for new installations:** bridge-level copies of the
  leading inverter, per-phase details, the numeric *… (applied)* sensors;
  MPC power/energy is not created on inverters that report MOI (identical
  values).
- Help texts in the integration options dialog.
- README and add-on documentation rewritten.

### Fixed
- License note: `eebus-go`, `ship-go` and `spine-go` are MIT licensed.

## 0.3.3-dev – 2026-11-18

### Fixed
- A repeated identical limit (a control box re-sending its limit, or the
  simulation started again) restarts its duration. Before, it expired at the
  time of the first write.

### Changed
- The add-on log names inverters that are left out of the §14a sharing
  because *Include storage* is off.

## 0.3.2-dev – 2026-11-18

### New
- **Include storage in §14a sharing** (`distribute_lpc_inverters`): inverters
  whose battery is registered under §14a get a share of a grid operator limit
  (limits charging the battery from the grid).

### Changed
- A grid operator limit is shared **by demand** instead of equally: devices
  with a measured consumption get their demand × 1.25 + 500 W, the rest goes
  to devices without a measurement; the sum never exceeds the limit.

## 0.3.1-dev – 2026-11-17

### New
- **§14a simulation** (`enable_lpc_simulation`, off by default): action
  *Simulate grid operator limit* tests the whole chain without a control box;
  refused while a real control box is connected or a real limit is active.
- **Repair** when wallboxes or heat pumps are found but *Control devices* is
  off.
- *Device type* diagnostic sensor per device.

### Fixed
- Grid operator limits with a duration now end when the duration is over
  (they stayed active before).
- Devices are recognised right after pairing, independent of the enabled
  options (a §14a-only wallbox was not recognised without *Control devices*).

## 0.3.0-dev – 2026-11-17

### New
- **Device classes:** inverter, wallbox, heat pump, grid meter, control box –
  each EEBUS device is its own Home Assistant device with entities by
  capability.
- **Wallboxes:** charge modes (*auto, off, PV, minimum + PV, fast, manual*)
  with PV-surplus control; vehicle state, charging power and state of charge
  where the wallbox reports them (EVSECC, EVCC, EVCEM, EVSOC, OPEV); §14a-only
  wallboxes (e.g. KATHREIN AC40) via a power limit.
- **Limits and failsafe per device** (inverters, wallboxes, heat pumps),
  action `eebus.set_device_limit`.
- **§14a sharing** (`distribute_lpc`) of a grid operator limit among
  wallboxes and heat pumps.
- **Monitoring of Inverter and Battery (MOI, MOB)**, e.g. KOSTAL PLENTICORE
  G3, built on the fork `CoreTex/eebus-go`; **grid meter** (MGCP).
- **Site totals**, PV-surplus sensor, German translation, options dialog for
  PV charging.

## 0.2.0-dev – 2026-11-17

### New
- Several paired devices: each device has its own *Connected* and *Trusted*
  (pair / forget) entities; action `eebus.forget_device`.
- Trusted devices are stored and survive restarts.

### Fixed
- Pairing a second device (e.g. a wallbox) failed because the bridge refused
  devices it did not trust, without a way to trust them from Home Assistant.
- Data of a second device no longer overwrites the inverter's values; limits
  can no longer be sent to the wrong device.

## Integration 0.1.1 – 0.1.6 – 2026-06-09

- Compatibility with Home Assistant 2026 / Python 3.14.
- Sensors without a value show "unavailable" instead of "unknown".
- Own brand icon and logo; production nominal maximum number.
- Prepared for the HACS default repository.

## Add-on 0.1.1 – 2026-06-09

- Options to set the nominal consumption / feed-in power (§14a).
- Add-on icon; prebuilt images.

## 0.1.0 – 2026-06-09

First release: EEBUS bridge based on `enbility/eebus-go` (receive and hold
LPC/LPP limits, optional Energy Guard towards an inverter, MPC/VABD/VAPD
monitoring) and the Home Assistant integration.
