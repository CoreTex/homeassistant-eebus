# Changelog

All notable changes to the **EEBUS Bridge add-on** and the **EEBUS
integration**. Since 0.2.0 both parts share one version number; update both
and restart Home Assistant after updating the integration.

`-dev` versions are pre-releases from the `dev` branch (add-on repository
`https://github.com/CoreTex/homeassistant-eebus#dev`, integration via HACS with
beta versions enabled).

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
