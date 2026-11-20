package eebus

import (
	"errors"
	"testing"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"
	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/store"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	spineapi "github.com/enbility/spine-go/api"
)

type fakeFlex struct {
	info  *ucapi.OptionalPowerConsumptionInfo
	state ucapi.CompressorPowerConsumptionStateType
}

func (f fakeFlex) OptionalPowerConsumption(spineapi.EntityRemoteInterface) (*ucapi.OptionalPowerConsumptionInfo, error) {
	if f.info == nil {
		return nil, errors.New("no data")
	}
	return f.info, nil
}
func (f fakeFlex) PowerConsumptionProcessState(spineapi.EntityRemoteInterface) (ucapi.CompressorPowerConsumptionStateType, error) {
	return f.state, nil
}
func (f fakeFlex) PowerConsumptionMinimalRunDuration(spineapi.EntityRemoteInterface) (time.Duration, error) {
	return 15 * time.Minute, nil
}
func (f fakeFlex) PowerConsumptionMinimalPauseDuration(spineapi.EntityRemoteInterface) (time.Duration, error) {
	return 0, errors.New("not reported")
}

func TestReadFlex(t *testing.T) {
	if st := readFlex(fakeFlex{}, nil); !st.Supported || st.State != "" || st.Available {
		t.Fatalf("no offer: %+v", st)
	}
	st := readFlex(fakeFlex{
		info:  &ucapi.OptionalPowerConsumptionInfo{Power: ptr(1800.0), MaxPower: ptr(3000.0), IsPausable: true},
		state: ucapi.CompressorPowerConsumptionStateAvailable,
	}, nil)
	if st.State != "available" || !st.Available || *st.PowerW != 1800 || *st.MaxPowerW != 3000 ||
		!st.Pausable || st.Stoppable || st.MinRunS == nil || *st.MinRunS != 900 || st.MinPauseS != nil {
		t.Fatalf("offer: %+v", st)
	}
	running := readFlex(fakeFlex{info: &ucapi.OptionalPowerConsumptionInfo{}, state: ucapi.CompressorPowerConsumptionStateRunning}, nil)
	if running.Available {
		t.Fatal("a running process is not available for a start")
	}
}

func TestCheckFlexAction(t *testing.T) {
	cases := []struct {
		st     store.FlexState
		action string
		ok     bool
	}{
		{store.FlexState{State: "available"}, FlexStart, true},
		{store.FlexState{State: "scheduled"}, FlexStart, true}, // reschedule
		{store.FlexState{State: "running"}, FlexStart, false},
		{store.FlexState{State: "running", Pausable: true}, FlexPause, true},
		{store.FlexState{State: "running"}, FlexPause, false}, // not pausable
		{store.FlexState{State: "paused"}, FlexResume, true},
		{store.FlexState{State: "running"}, FlexResume, false},
		{store.FlexState{State: "running", Stoppable: true}, FlexStop, true},
		{store.FlexState{State: "available", Stoppable: true}, FlexStop, false},
		{store.FlexState{State: "running", Stoppable: false}, FlexStop, false},
		{store.FlexState{State: "available"}, "boost", false},
	}
	for _, c := range cases {
		if err := checkFlexAction(c.st, c.action); (err == nil) != c.ok {
			t.Errorf("%s in %+v: err=%v, want ok=%v", c.action, c.st, err, c.ok)
		}
	}
}

func TestSetFlexWithoutOffer(t *testing.T) {
	m := newTestManager(t, config.Config{})
	ski := "0123456789abcdef0123456789abcdef01234567"
	if err := m.SetFlex(ski, FlexStart, 0); err == nil {
		t.Fatal("expected an error without OHPCF")
	}
	if err := m.SetFlex("nope", FlexStart, 0); err == nil {
		t.Fatal("expected an error for an invalid SKI")
	}
}

func TestClassifyHeatPumpByOHPCF(t *testing.T) {
	// Bosch CS5800i with K40RF (diagnostics from issue #4)
	got := classify("Generic", []string{"HeatPumpAppliance", "Compressor"}, []string{ucEgLPC, ucMPC, ucOHPCF})
	if got != store.CategoryHeatPump {
		t.Fatalf("classify = %q", got)
	}
}
