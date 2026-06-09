package eebus

import (
	"testing"
	"time"

	ucapi "github.com/enbility/eebus-go/usecases/api"
)

func TestNormaliseSKI(t *testing.T) {
	if got := normaliseSKI(" AB:CD-EF "); got != "abcdef" {
		t.Errorf("normaliseSKI = %q, want abcdef", got)
	}
}

func TestValidSKI(t *testing.T) {
	if !validSKI("0123456789abcdef0123456789abcdef01234567") {
		t.Error("valid SKI should pass")
	}
	if validSKI("short") || validSKI("0123456789abcdef0123456789abcdef0123456X") {
		t.Error("invalid SKIs should fail")
	}
}

func TestToLimitStateActive(t *testing.T) {
	ls := toLimitState(ucapi.LoadLimit{
		IsActive:     true,
		IsChangeable: true,
		Value:        4200,
		Duration:     time.Hour,
	})
	if !ls.Active || ls.ValueW != 4200 || !ls.Changeable {
		t.Errorf("unexpected limit state: %+v", ls)
	}
	if ls.DurationS != 3600 {
		t.Errorf("DurationS = %v, want 3600", ls.DurationS)
	}
	if ls.ExpiresAt == nil {
		t.Errorf("expected ExpiresAt to be set for an active, time-bound limit")
	}
}

func TestToLimitStateInactiveHasNoExpiry(t *testing.T) {
	ls := toLimitState(ucapi.LoadLimit{IsActive: false, Duration: time.Hour})
	if ls.ExpiresAt != nil {
		t.Errorf("inactive limit should have no expiry")
	}
}
