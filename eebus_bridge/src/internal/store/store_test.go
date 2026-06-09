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
