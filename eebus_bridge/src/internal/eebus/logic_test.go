package eebus

import (
	"math"
	"testing"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	spineapi "github.com/enbility/spine-go/api"
)

// Classification inputs taken from real devices (bridge debug log).
func TestClassifyRealDevices(t *testing.T) {
	cases := []struct {
		name       string
		deviceType string
		entities   []string
		useCases   []string
		want       string
	}{
		// KOSTAL PLENTICORE G3: deviceType Generic, PVESHybrid/Inverter/Battery
		{"kostal", "Generic", []string{"Battery", "CEM", "Inverter", "PVESHybrid"}, []string{ucMPC, ucEgLPC, ucEgLPP}, store.CategoryInverter},
		// KATHREIN AC40: ChargingStation with one EVSE entity, LPC only
		{"kathrein", "ChargingStation", []string{"EVSE"}, []string{ucEgLPC}, store.CategoryWallbox},
		{"wallbox by use case", "Generic", nil, []string{ucOPEV}, store.CategoryWallbox},
		{"heat pump", "Generic", []string{"HeatPumpAppliance", "Compressor"}, []string{ucMPC, ucEgLPC}, store.CategoryHeatPump},
		{"smart meter", "Generic", []string{"GridConnectionPointOfPremises"}, []string{ucMGCP}, store.CategoryGridMeter},
		{"control box", "ElectricitySupplySystem", []string{"CEM"}, []string{ucLPCSource}, store.CategoryControlBox},
		// An inverter that also hosts the grid meter stays an inverter.
		{"inverter with meter", "Inverter", []string{"Inverter", "GridConnectionPointOfPremises"}, []string{ucMGCP}, store.CategoryInverter},
		// A wallbox reporting MPC is still a wallbox.
		{"wallbox with mpc", "ChargingStation", []string{"EVSE"}, []string{ucMPC}, store.CategoryWallbox},
		{"inverter by MOI only", "Generic", nil, []string{ucMOI}, store.CategoryInverter},
		{"inverter by MOB only", "Generic", nil, []string{ucMOB}, store.CategoryInverter},
		{"unknown", "", nil, nil, store.CategoryGeneric},
	}
	for _, c := range cases {
		if got := classify(c.deviceType, c.entities, c.useCases); got != c.want {
			t.Errorf("%s: classify = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPrimaryEligible(t *testing.T) {
	for cat, want := range map[string]bool{
		store.CategoryInverter: true, store.CategoryGeneric: true, "": true,
		store.CategoryWallbox: false, store.CategoryHeatPump: false,
		store.CategoryGridMeter: false, store.CategoryControlBox: false,
	} {
		if got := primaryEligible(cat); got != want {
			t.Errorf("primaryEligible(%q) = %v, want %v", cat, got, want)
		}
	}
}

func TestWallboxNeverBecomesLeadingInverter(t *testing.T) {
	m := newTestManager(t, config.Config{})
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(skiWallbox1, func(d *store.DeviceData) { d.Category = store.CategoryWallbox })
		s.UpdateDevice(skiInverter, func(d *store.DeviceData) { d.Category = store.CategoryInverter })
	})
	if m.isPrimary(skiWallbox1) {
		t.Fatal("wallbox must not become leading inverter")
	}
	if !m.isPrimary(skiInverter) {
		t.Fatal("inverter should become leading inverter")
	}
}

func TestEffectiveLimit(t *testing.T) {
	share := 3000.0
	cases := []struct {
		userActive bool
		userW      float64
		share      *float64
		wantW      float64
		wantActive bool
	}{
		{false, 5000, nil, 0, false},
		{true, 5000, nil, 5000, true},
		{false, 5000, &share, 3000, true},
		{true, 5000, &share, 3000, true}, // lower wins
		{true, 2000, &share, 2000, true},
	}
	for i, c := range cases {
		w, a := effectiveLimit(c.userActive, c.userW, c.share)
		if w != c.wantW || a != c.wantActive {
			t.Errorf("case %d: got (%v,%v), want (%v,%v)", i, w, a, c.wantW, c.wantActive)
		}
	}
}

func TestEffectiveCurrent(t *testing.T) {
	ten, six := 10.0, 6.0
	if v, a := effectiveCurrent(nil, nil); v != nil || a {
		t.Error("nothing requested must mean no write")
	}
	if v, a := effectiveCurrent(&ten, &six); !a || *v != 6 {
		t.Errorf("cap must win, got %v", v)
	}
	if v, a := effectiveCurrent(&six, &ten); !a || *v != 6 {
		t.Errorf("request below cap, got %v", v)
	}
	if v, a := effectiveCurrent(nil, &six); !a || *v != 6 {
		t.Errorf("cap alone, got %v", v)
	}
}

func TestShareToCurrent(t *testing.T) {
	if got := shareToCurrent(4200, 3); got != 6 {
		t.Errorf("4200 W / 3 phases = %v A, want 6", got)
	}
	if got := shareToCurrent(4200, 1); got != 18 {
		t.Errorf("4200 W / 1 phase = %v A, want 18", got)
	}
	if got := shareToCurrent(4200, 0); got != 6 {
		t.Errorf("unknown phases default to 3, got %v", got)
	}
	if got := shareToCurrent(-5, 3); got != 0 {
		t.Errorf("negative share must clamp to 0, got %v", got)
	}
}

func TestPlanDistribution(t *testing.T) {
	hp := "4444444444444444444444444444444444444444"
	one := uint(1)
	s := store.State{
		LPC: store.LPCState{Limit: store.LimitState{Active: true, ValueW: 8400}},
		Connections: []store.Connection{
			{SKI: skiInverter, Connected: true},
			{SKI: skiWallbox1, Connected: true},
			{SKI: skiWallbox2, Connected: true},
			{SKI: hp, Connected: false},
		},
		Devices: map[string]store.DeviceData{
			skiInverter: {Category: store.CategoryInverter},
			skiWallbox1: {Category: store.CategoryWallbox},
			skiWallbox2: {Category: store.CategoryWallbox, EV: &store.EVState{PhasesConnected: &one}},
			hp:          {Category: store.CategoryHeatPump},
		},
	}
	lpc := map[string]bool{skiInverter: true, skiWallbox1: true, hp: true}
	opev := map[string]bool{skiWallbox2: true}

	consumers := planDistribution(s, lpc, opev, false)
	if len(consumers) != 2 {
		t.Fatalf("want 2 consumers (inverter excluded, offline heat pump skipped), got %+v", consumers)
	}
	if consumers[0].SKI != skiWallbox1 || !consumers[0].ViaLPC || consumers[0].ShareW != 4200 {
		t.Errorf("wallbox 1 should get 4200 W via LPC: %+v", consumers[0])
	}
	if consumers[1].SKI != skiWallbox2 || consumers[1].ViaLPC || consumers[1].Phases != 1 || consumers[1].ShareW != 4200 {
		t.Errorf("wallbox 2 should get 4200 W via OPEV on 1 phase: %+v", consumers[1])
	}

	// with distribute_lpc_inverters the inverter takes part (via LPC)
	consumers = planDistribution(s, lpc, opev, true)
	if len(consumers) != 3 || consumers[0].SKI != skiInverter || !consumers[0].ViaLPC {
		t.Fatalf("inverter should be included: %+v", consumers)
	}
	// an inverter without LPC is never capped via OPEV
	if c := planDistribution(s, map[string]bool{}, map[string]bool{skiInverter: true}, true); len(c) != 0 {
		t.Errorf("inverter without LPC must not take part: %+v", c)
	}

	s.LPC.Limit.Active = false
	if c := planDistribution(s, lpc, opev, true); c != nil {
		t.Error("no distribution without an active incoming limit")
	}
}

func total(cs []distributionConsumer) float64 {
	sum := 0.0
	for _, c := range cs {
		sum += c.ShareW
	}
	return sum
}

func TestAllocateSharesByDemand(t *testing.T) {
	idle, charging := 0.0, 3000.0

	// Kostal idle (measured 0 W), Kathrein unknown: the battery keeps a ramp
	// reserve, the wallbox gets the rest
	cs := []distributionConsumer{{SKI: "kostal", DemandW: &idle}, {SKI: "kathrein"}}
	allocateShares(4200, cs)
	if cs[0].ShareW != 500 || cs[1].ShareW != 3700 {
		t.Errorf("idle battery: got kostal=%v kathrein=%v, want 500/3700", cs[0].ShareW, cs[1].ShareW)
	}

	// battery charges from the grid with 3 kW: its cap (3000*1.25+500=4250)
	// exceeds the fair half, so both get an equal share
	cs = []distributionConsumer{{SKI: "kostal", DemandW: &charging}, {SKI: "kathrein"}}
	allocateShares(4200, cs)
	if cs[0].ShareW != 2100 || cs[1].ShareW != 2100 {
		t.Errorf("busy battery: got %v/%v, want 2100/2100", cs[0].ShareW, cs[1].ShareW)
	}

	// all measured and idle: the leftover is spread, nothing wasted beyond rounding
	a, b := 0.0, 0.0
	cs = []distributionConsumer{{SKI: "a", DemandW: &a}, {SKI: "b", DemandW: &b}}
	allocateShares(8400, cs)
	if cs[0].ShareW != 4200 || cs[1].ShareW != 4200 {
		t.Errorf("all idle: got %v/%v, want 4200/4200", cs[0].ShareW, cs[1].ShareW)
	}

	// never above the limit, rounded down to 100 W
	odd := 1234.0
	cs = []distributionConsumer{{SKI: "a", DemandW: &odd}, {SKI: "b"}, {SKI: "c"}}
	allocateShares(9999, cs)
	if total(cs) > 9999 {
		t.Errorf("shares %v exceed the limit", total(cs))
	}
	for _, c := range cs {
		if math.Mod(c.ShareW, 100) != 0 {
			t.Errorf("share %v not rounded to 100 W", c.ShareW)
		}
	}

	allocateShares(1000, nil) // no consumers: no panic
}

func TestConsumerDemand(t *testing.T) {
	ev, mpc, ac := 7000.0, -300.0, -2500.0
	if d := consumerDemand(store.DeviceData{EV: &store.EVState{PowerW: &ev}}); d == nil || *d != 7000 {
		t.Errorf("EV power: %v", d)
	}
	if d := consumerDemand(store.DeviceData{MPC: &store.MPCState{PowerW: &mpc}}); d == nil || *d != 0 {
		t.Errorf("producing device has no demand: %v", d)
	}
	// MOI: negative AC power = drawing from the grid (charging the battery)
	if d := consumerDemand(store.DeviceData{InverterAC: &store.InverterACState{PowerW: &ac}}); d == nil || *d != 2500 {
		t.Errorf("inverter drawing 2.5 kW: %v", d)
	}
	if consumerDemand(store.DeviceData{}) != nil {
		t.Error("no measurement: unknown demand")
	}
}

func TestKeepShare(t *testing.T) {
	old := 3000.0
	if !keepShare(&old, 3150) || keepShare(&old, 3300) || keepShare(nil, 3000) {
		t.Error("hysteresis wrong")
	}
}

func TestCommonCurrentLimits(t *testing.T) {
	minA, maxA, defA := commonCurrentLimits([]float64{6, 6, 8}, []float64{16, 32, 16}, []float64{0, 0, 0})
	if *minA != 8 || *maxA != 16 || *defA != 0 {
		t.Errorf("got min=%v max=%v def=%v", *minA, *maxA, *defA)
	}
	if a, b, c := commonCurrentLimits(nil, nil, nil); a != nil || b != nil || c != nil {
		t.Error("empty input must yield nils")
	}
}

func TestLowestActiveLimit(t *testing.T) {
	got := lowestActiveLimit([]ucapi.LoadLimitsPhase{
		{IsActive: true, Value: 16}, {IsActive: false, Value: 2}, {IsActive: true, Value: 10},
	})
	if got == nil || *got != 10 {
		t.Errorf("got %v, want 10", got)
	}
	if lowestActiveLimit(nil) != nil {
		t.Error("no limits must yield nil")
	}
}

func TestLeadingEGSKI(t *testing.T) {
	m := newTestManager(t, config.Config{})
	m.egEntities = map[LimitKind]map[string]spineEntity{KindLPC: {}, KindLPP: {}}
	m.store.Update(func(s *store.State) {
		s.UpdateDevice(skiWallbox1, func(d *store.DeviceData) { d.Category = store.CategoryWallbox })
		s.UpdateDevice(skiInverter, func(d *store.DeviceData) { d.Category = store.CategoryInverter })
	})
	m.egEntities[KindLPC][skiWallbox1] = nil
	if got := m.leadingEGSKI(KindLPC); got != "" {
		t.Errorf("only a wallbox accepts LPC: leading must be empty, got %q", got)
	}
	m.egEntities[KindLPC][skiInverter] = nil
	if got := m.leadingEGSKI(KindLPC); got != skiInverter {
		t.Errorf("leading = %q, want inverter", got)
	}

	pinned := newTestManager(t, config.Config{InverterSKI: skiWallbox2})
	pinned.egEntities = map[LimitKind]map[string]spineEntity{KindLPC: {}, KindLPP: {}}
	if got := pinned.leadingEGSKI(KindLPC); got != skiWallbox2 {
		t.Errorf("inverter_ski must win even while offline, got %q", got)
	}
}

func TestRecomputeSite(t *testing.T) {
	pv1, pv2, grid, ev := 3000.0, 1500.0, -800.0, 7000.0
	var s store.State
	s.PrimarySKI = skiInverter
	s.UpdateDevice(skiInverter, func(d *store.DeviceData) {
		d.Category = store.CategoryInverter
		d.Inverter = &store.InverterState{PowerW: &pv1}
	})
	s.UpdateDevice(skiWallbox2, func(d *store.DeviceData) {
		d.Category = store.CategoryInverter
		d.Inverter = &store.InverterState{PowerW: &pv2}
		d.Grid = &store.GridState{PowerW: &grid}
	})
	s.UpdateDevice(skiWallbox1, func(d *store.DeviceData) {
		d.Category = store.CategoryWallbox
		d.EV = &store.EVState{PowerW: &ev}
	})
	site := s.Site
	if site.PVPowerW == nil || *site.PVPowerW != 4500 {
		t.Errorf("pv sum = %v, want 4500", site.PVPowerW)
	}
	if site.GridPowerW == nil || *site.GridPowerW != -800 || site.GridSKI != skiWallbox2 {
		t.Errorf("grid = %v from %s", site.GridPowerW, site.GridSKI)
	}
	if site.EVChargingPowerW == nil || *site.EVChargingPowerW != 7000 {
		t.Errorf("ev = %v", site.EVChargingPowerW)
	}
	if site.Inverters != 2 || site.Wallboxes != 1 || site.LeadingInverterSKI != skiInverter {
		t.Errorf("counts/leading wrong: %+v", site)
	}
	if site.BatteryPowerW != nil {
		t.Error("no battery reported: must stay nil")
	}

	ac1, ac2 := 2000.0, -500.0
	s.UpdateDevice(skiInverter, func(d *store.DeviceData) { d.InverterAC = &store.InverterACState{PowerW: &ac1} })
	s.UpdateDevice(skiWallbox2, func(d *store.DeviceData) { d.InverterAC = &store.InverterACState{PowerW: &ac2} })
	if s.Site.InverterACPowerW == nil || *s.Site.InverterACPowerW != 1500 {
		t.Errorf("inverter AC sum = %v, want 1500", s.Site.InverterACPowerW)
	}
}

func TestHandleEventIgnoresIrrelevantEvents(t *testing.T) {
	m := newTestManager(t, config.Config{})
	// no device / not an "add": must not panic or touch the store
	m.HandleEvent(spineapi.EventPayload{})
	m.HandleEvent(spineapi.EventPayload{EventType: spineapi.EventTypeDataChange, ChangeType: spineapi.ElementChangeAdd})
	if len(m.store.Snapshot().Devices) != 0 {
		t.Error("irrelevant events must not create devices")
	}
}
