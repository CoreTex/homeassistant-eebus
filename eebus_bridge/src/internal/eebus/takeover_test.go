package eebus

import (
	"testing"
	"time"

	"github.com/CoreTex/homeassistant-eebus/eebus_bridge/internal/config"

	ucapi "github.com/enbility/eebus-go/usecases/api"
	ucmocks "github.com/enbility/eebus-go/usecases/mocks"
	spineapi "github.com/enbility/spine-go/api"
	spinemocks "github.com/enbility/spine-go/mocks"
	"github.com/enbility/spine-go/model"
	"github.com/stretchr/testify/mock"
)

// A rejected first limit is followed by the fallback limit (#7).
func TestTakeoverSendsFallbackWhenRejected(t *testing.T) {
	m := newTestManager(t, config.Config{})
	lpc := ucmocks.NewEgLPCInterface(t)
	m.egLPC = lpc
	ski := "0123456789abcdef0123456789abcdef01234567"

	written := make(chan ucapi.LoadLimit, 1)
	lpc.EXPECT().WriteConsumptionLimit(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ spineapi.EntityRemoteInterface, l ucapi.LoadLimit, _ func(model.ResultDataType, model.MsgCounterType)) (*model.MsgCounterType, error) {
			written <- l
			return nil, nil
		}).Once()

	cb := m.takeoverResultCB(ski, spinemocks.NewEntityRemoteInterface(t), &ucapi.LoadLimit{Value: 16200, IsChangeable: true})
	cb(model.ResultDataType{ErrorNumber: ptr(model.ErrorNumberTypeNoError)}, 0) // accepted: nothing
	select {
	case <-written:
		t.Fatal("an accepted limit must not be re-sent")
	case <-time.After(50 * time.Millisecond):
	}

	cb(model.ResultDataType{ErrorNumber: ptr(model.ErrorNumberType(7))}, 0)
	select {
	case l := <-written:
		if l.IsActive || l.Value != 16200 {
			t.Fatalf("fallback limit: %+v", l)
		}
	case <-time.After(time.Second):
		t.Fatal("no fallback limit written")
	}
}

// Bosch heat pumps only leave failsafe (error 1240) on an active limit, so the
// takeover sends "no limit" as an active limit at the nominal maximum.
func TestTakeoverSendsActiveNominalFirst(t *testing.T) {
	m := newTestManager(t, config.Config{})
	lpc := ucmocks.NewEgLPCInterface(t)
	m.egLPC = lpc
	ski := "0123456789abcdef0123456789abcdef01234567"
	entity := spinemocks.NewEntityRemoteInterface(t)
	m.egEntities[KindLPC][ski] = entity
	m.flexEntities[ski] = entity

	lpc.EXPECT().ConsumptionNominalMax(mock.Anything).Return(16200, nil)
	var got ucapi.LoadLimit
	lpc.EXPECT().WriteConsumptionLimit(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ spineapi.EntityRemoteInterface, l ucapi.LoadLimit, _ func(model.ResultDataType, model.MsgCounterType)) (*model.MsgCounterType, error) {
			got = l
			return nil, nil
		}).Once()
	m.takeLPCControl(ski)
	if !got.IsActive || got.Value != 16200 {
		t.Fatalf("takeover limit: %+v", got)
	}
}

// A heat pump that rejects every write right after connecting gets the
// takeover again later (#7, 24 Nov log: both writes rejected with code 7).
func TestTakeoverRetriesWhenEverythingIsRejected(t *testing.T) {
	old := takeoverDelay
	takeoverDelay = func(int) time.Duration { return 10 * time.Millisecond }
	t.Cleanup(func() { takeoverDelay = old })

	m := newTestManager(t, config.Config{})
	lpc := ucmocks.NewEgLPCInterface(t)
	m.egLPC = lpc
	ski := "0123456789abcdef0123456789abcdef01234567"
	entity := spinemocks.NewEntityRemoteInterface(t)
	m.egEntities[KindLPC][ski] = entity
	m.flexEntities[ski] = entity

	lpc.EXPECT().ConsumptionNominalMax(mock.Anything).Return(16200, nil).Maybe()
	writes := make(chan ucapi.LoadLimit, 100)
	lpc.EXPECT().WriteConsumptionLimit(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ spineapi.EntityRemoteInterface, l ucapi.LoadLimit, cb func(model.ResultDataType, model.MsgCounterType)) (*model.MsgCounterType, error) {
			writes <- l
			go cb(model.ResultDataType{ErrorNumber: ptr(model.ErrorNumberType(7))}, 0)
			return nil, nil
		}).Maybe()

	m.takeLPCControl(ski)
	deadline := time.After(3 * time.Second)
	var got []ucapi.LoadLimit
	for len(got) < 2*(maxTakeoverRetries+1) {
		select {
		case l := <-writes:
			got = append(got, l)
		case <-deadline:
			t.Fatalf("only %d writes: %+v", len(got), got)
		}
	}
	if !got[0].IsActive || got[0].Value != 16200 || got[1].IsActive || !got[2].IsActive {
		t.Fatalf("expected active nominal, deactivated, active nominal...: %+v", got[:3])
	}
	// gives up after the last retry
	select {
	case l := <-writes:
		time.Sleep(50 * time.Millisecond)
		if len(writes) > 0 {
			t.Fatalf("still retrying: %+v", l)
		}
	case <-time.After(100 * time.Millisecond):
	}
}
