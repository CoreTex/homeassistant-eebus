package store

import "slices"

// Device categories, derived from the SPINE device type, entity types and the
// use cases a remote announces (see eebus.classify).
const (
	CategoryInverter   = "inverter"
	CategoryWallbox    = "wallbox"
	CategoryHeatPump   = "heat_pump"
	CategoryGridMeter  = "grid_meter"
	CategoryControlBox = "control_box"
	CategoryGeneric    = "generic"
)

// DeviceData holds everything known about one remote device (keyed by SKI in
// State.Devices). The optional blocks only appear once the device reports the
// matching use case, so consumers can create entities by capability.
type DeviceData struct {
	Category    string   `json:"category"`
	DeviceType  string   `json:"device_type,omitempty"`
	EntityTypes []string `json:"entity_types,omitempty"`
	// UseCases lists the remote's capabilities as seen by the bridge, e.g.
	// "mpc", "mgcp", "vapd", "vabd", "eg_lpc", "eg_lpp", "evsecc", "evcc",
	// "evcem", "evsoc", "opev", "lpc_source", "lpp_source".
	UseCases []string `json:"use_cases,omitempty"`

	MPC      *MPCState      `json:"mpc,omitempty"`
	Battery  *BatteryState  `json:"battery,omitempty"`
	Inverter *InverterState `json:"inverter,omitempty"`
	Grid     *GridState     `json:"grid,omitempty"`
	EV       *EVState       `json:"ev,omitempty"`
	Control  *DeviceControl `json:"control,omitempty"`
}

// HasUseCase reports whether the device announced the given capability.
func (d DeviceData) HasUseCase(uc string) bool { return slices.Contains(d.UseCases, uc) }

// GridState is MGCP ("Monitoring of Grid Connection Point") data. PowerW is
// positive when importing from the grid and negative when feeding in.
type GridState struct {
	Supported bool     `json:"supported"`
	PowerW    *float64 `json:"power_w,omitempty"`
	// PowerLimitationFactor is the feed-in limitation in percent of the PV peak
	// power (e.g. 70 for the "70 % rule"), if the meter reports one.
	PowerLimitationFactor *float64  `json:"power_limitation_factor,omitempty"`
	EnergyFeedInWh        *float64  `json:"energy_feed_in_wh,omitempty"`
	EnergyConsumedWh      *float64  `json:"energy_consumed_wh,omitempty"`
	CurrentPerPhaseA      []float64 `json:"current_per_phase_a,omitempty"`
	VoltagePerPhaseV      []float64 `json:"voltage_per_phase_v,omitempty"`
	FrequencyHz           *float64  `json:"frequency_hz,omitempty"`
}

// EVState combines the wallbox (EVSE) and vehicle (EV) use cases.
type EVState struct {
	// EVSECC
	EVSEOperatingState string `json:"evse_operating_state,omitempty"`
	EVSEErrorMessage   string `json:"evse_error_message,omitempty"`

	// EVCC
	Connected             bool     `json:"connected"`
	ChargeState           string   `json:"charge_state,omitempty"`
	CommunicationStandard string   `json:"communication_standard,omitempty"`
	AsymmetricCharging    *bool    `json:"asymmetric_charging,omitempty"`
	SleepMode             *bool    `json:"sleep_mode,omitempty"`
	Identifications       []string `json:"identifications,omitempty"`
	// Per-phase current limits of the vehicle (lowest common value over phases).
	MinCurrentA     *float64 `json:"min_current_a,omitempty"`
	MaxCurrentA     *float64 `json:"max_current_a,omitempty"`
	DefaultCurrentA *float64 `json:"default_current_a,omitempty"`

	// EVCEM
	PhasesConnected  *uint     `json:"phases_connected,omitempty"`
	PowerW           *float64  `json:"power_w,omitempty"`
	PowerPerPhaseW   []float64 `json:"power_per_phase_w,omitempty"`
	CurrentPerPhaseA []float64 `json:"current_per_phase_a,omitempty"`
	EnergyChargedWh  *float64  `json:"energy_charged_wh,omitempty"`

	// EVSOC
	StateOfChargePct *float64 `json:"state_of_charge_pct,omitempty"`

	// OPEV control. RequestedCurrentA is what Home Assistant asked for, CapCurrentA
	// the §14a distribution cap and AppliedCurrentA what was last written to the
	// wallbox (min of both). LimitCurrentA is the limit the wallbox reports back.
	ControlSupported  bool     `json:"control_supported"`
	RequestedCurrentA *float64 `json:"requested_current_a,omitempty"`
	CapCurrentA       *float64 `json:"cap_current_a,omitempty"`
	AppliedCurrentA   *float64 `json:"applied_current_a,omitempty"`
	LimitCurrentA     *float64 `json:"limit_current_a,omitempty"`
}

// DeviceControl is the bridge acting as Energy Guard towards this device
// (LPC: consumption limit, e.g. §14a dimming of a heat pump or wallbox; LPP:
// production limit of an inverter).
//
// TargetW/Active are what the user asked for (staged value + on/off),
// AppliedW is the limit actually commanded (nil = no limit), which may be lower
// than the target when a §14a share (DistributedLimitW) is in force.
// ReportedW/ReportedActive is the device's own readback.
type DeviceControl struct {
	LPCSupported      bool     `json:"lpc_supported"`
	LPCTargetW        float64  `json:"lpc_target_w"`
	LPCActive         bool     `json:"lpc_active"`
	LPCAppliedW       *float64 `json:"lpc_applied_w,omitempty"`
	LPCReportedW      *float64 `json:"lpc_reported_w,omitempty"`
	LPCReportedActive bool     `json:"lpc_reported_active"`
	LPCNominalMaxW    *float64 `json:"lpc_nominal_max_w,omitempty"`
	// Failsafe values the device falls back to when the bridge's heartbeat is
	// lost (e.g. bridge restart) after it has received a limit.
	LPCFailsafeW         *float64 `json:"lpc_failsafe_w,omitempty"`
	LPCFailsafeDurationS *float64 `json:"lpc_failsafe_duration_s,omitempty"`
	LPPSupported         bool     `json:"lpp_supported"`
	LPPTargetW           float64  `json:"lpp_target_w"`
	LPPActive            bool     `json:"lpp_active"`
	LPPAppliedW          *float64 `json:"lpp_applied_w,omitempty"`
	LPPReportedW         *float64 `json:"lpp_reported_w,omitempty"`
	LPPReportedActive    bool     `json:"lpp_reported_active"`
	DurationS            float64  `json:"duration_s"`
	DistributedLimitW    *float64 `json:"distributed_limit_w,omitempty"`
}

// SiteState aggregates all devices. Values are nil when no device reports them.
type SiteState struct {
	LeadingInverterSKI string   `json:"leading_inverter_ski"`
	GridSKI            string   `json:"grid_ski,omitempty"`
	PVPowerW           *float64 `json:"pv_power_w,omitempty"`
	BatteryPowerW      *float64 `json:"battery_power_w,omitempty"`
	GridPowerW         *float64 `json:"grid_power_w,omitempty"`
	EVChargingPowerW   *float64 `json:"ev_charging_power_w,omitempty"`
	HeatPumpPowerW     *float64 `json:"heat_pump_power_w,omitempty"`
	Inverters          int      `json:"inverters"`
	Wallboxes          int      `json:"wallboxes"`
	HeatPumps          int      `json:"heat_pumps"`
}

// DistributionState reports the §14a share-out of the incoming LPC limit.
type DistributionState struct {
	Enabled bool `json:"enabled"`
	Active  bool `json:"active"`
	// LimitW is the incoming total limit, ShareW the per-device share.
	LimitW  float64  `json:"limit_w"`
	ShareW  float64  `json:"share_w"`
	Devices []string `json:"devices,omitempty"`
}

// UpdateDevice applies mutate to a copy of the device entry for ski, stores it
// in a fresh Devices map and refreshes the site aggregates. It must be called
// from inside an Update mutator.
//
// Copy-on-write rule: snapshots handed to readers share the old structs, so
// writers must only replace fields (never mutate slices in place). All nested
// structs are shallow-copied here so plain field assignment is safe.
func (st *State) UpdateDevice(ski string, mutate func(*DeviceData)) {
	next := make(map[string]DeviceData, len(st.Devices)+1)
	for k, v := range st.Devices {
		next[k] = v
	}
	d := next[ski]
	d.MPC = clonePtr(d.MPC)
	d.Battery = clonePtr(d.Battery)
	d.Inverter = clonePtr(d.Inverter)
	d.Grid = clonePtr(d.Grid)
	d.EV = clonePtr(d.EV)
	d.Control = clonePtr(d.Control)
	mutate(&d)
	next[ski] = d
	st.Devices = next
	st.RecomputeSite()
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// RecomputeSite derives the whole-home aggregates from all devices.
func (st *State) RecomputeSite() {
	site := SiteState{LeadingInverterSKI: st.PrimarySKI}
	var pv, bat, ev, hp *float64
	add := func(acc **float64, v *float64) {
		if v == nil {
			return
		}
		if *acc == nil {
			z := 0.0
			*acc = &z
		}
		**acc += *v
	}

	skis := make([]string, 0, len(st.Devices))
	for ski := range st.Devices {
		skis = append(skis, ski)
	}
	slices.Sort(skis) // deterministic choice of the grid meter

	for _, ski := range skis {
		d := st.Devices[ski]
		switch d.Category {
		case CategoryInverter:
			site.Inverters++
		case CategoryWallbox:
			site.Wallboxes++
		case CategoryHeatPump:
			site.HeatPumps++
			if d.MPC != nil {
				add(&hp, d.MPC.PowerW)
			}
		}
		if d.Inverter != nil {
			add(&pv, d.Inverter.PowerW)
		}
		if d.Battery != nil {
			add(&bat, d.Battery.PowerW)
		}
		if d.EV != nil {
			add(&ev, d.EV.PowerW)
		}
		// Prefer a dedicated grid meter, otherwise the first device with MGCP.
		if d.Grid != nil && d.Grid.PowerW != nil {
			if site.GridSKI == "" || (d.Category == CategoryGridMeter && st.Devices[site.GridSKI].Category != CategoryGridMeter) {
				site.GridSKI = ski
				v := *d.Grid.PowerW
				site.GridPowerW = &v
			}
		}
	}
	site.PVPowerW, site.BatteryPowerW, site.EVChargingPowerW, site.HeatPumpPowerW = pv, bat, ev, hp
	st.Site = site
}
