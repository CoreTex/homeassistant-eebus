package store

import (
	"testing"
	"time"
)

func TestUpdateAndSnapshot(t *testing.T) {
	s := New()
	if !s.Snapshot().UpdatedAt.IsZero() {
		t.Errorf("expected zero UpdatedAt before any update")
	}
	s.Update(func(st *State) { st.Connected = true })
	snap := s.Snapshot()
	if !snap.Connected {
		t.Errorf("expected Connected true after update")
	}
	if snap.UpdatedAt.IsZero() {
		t.Errorf("expected UpdatedAt to be set after update")
	}
}

func TestSubscribeSeedsAndReceivesUpdates(t *testing.T) {
	s := New()
	s.Update(func(st *State) { st.LPC.Supported = true })

	ch, unsub := s.Subscribe()
	defer unsub()

	// A new subscriber is immediately seeded with the current snapshot.
	select {
	case seed := <-ch:
		if !seed.LPC.Supported {
			t.Errorf("seed did not reflect current state")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive seed snapshot")
	}

	s.Update(func(st *State) { st.LPC.Limit.Active = true })
	select {
	case got := <-ch:
		if !got.LPC.Limit.Active {
			t.Errorf("update not delivered to subscriber")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive update")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	s := New()
	ch, unsub := s.Subscribe()
	<-ch // drain seed
	unsub()
	// channel should now be closed
	if _, ok := <-ch; ok {
		t.Errorf("expected channel to be closed after unsubscribe")
	}
}

func TestUpdateCoalescesWhenSubscriberSlow(t *testing.T) {
	s := New()
	ch, unsub := s.Subscribe()
	defer unsub()
	<-ch // drain seed, leave buffer empty

	// Two rapid updates without reading in between: the buffer (size 1) must
	// not block and the subscriber must end up with the latest value.
	s.Update(func(st *State) { st.MPC.Supported = true })
	s.Update(func(st *State) { st.Battery.Supported = true })

	got := <-ch
	if !got.Battery.Supported {
		t.Errorf("expected latest snapshot to win, got %+v", got.Battery)
	}
}

func TestUpdateDeviceSnapshotIsolation(t *testing.T) {
	s := New()
	v1, v2 := 100.0, 200.0
	s.Update(func(st *State) {
		st.UpdateDevice("a", func(d *DeviceData) { d.MPC = &MPCState{PowerW: &v1} })
	})
	before := s.Snapshot()

	s.Update(func(st *State) {
		st.UpdateDevice("a", func(d *DeviceData) { d.MPC.PowerW = &v2 })
		st.UpdateDevice("b", func(d *DeviceData) { d.Battery = &BatteryState{Supported: true} })
	})
	after := s.Snapshot()

	if got := *before.Devices["a"].MPC.PowerW; got != 100 {
		t.Errorf("old snapshot was mutated: power = %v", got)
	}
	if _, ok := before.Devices["b"]; ok {
		t.Error("old snapshot gained a device")
	}
	if got := *after.Devices["a"].MPC.PowerW; got != 200 {
		t.Errorf("new snapshot power = %v, want 200", got)
	}
	if !after.Devices["b"].Battery.Supported {
		t.Error("device b missing in new snapshot")
	}
}
