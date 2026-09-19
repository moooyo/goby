package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionsSubscriptionLimitsAndReplacement(t *testing.T) {
	for _, test := range []struct {
		kind, raw       string
		active          bool
		delay, interval time.Duration
	}{
		{"SessionsStart", "", true, 0, time.Second},
		{"SessionsStart", `""`, true, 0, time.Second},
		{"SessionsStart", `"250,2000"`, true, 250 * time.Millisecond, 2 * time.Second},
		{"SessionsStart", `"60000,60000"`, true, time.Minute, time.Minute},
		{"SessionsStop", "", false, 0, time.Second},
	} {
		state, err := parseSessionSubscription(test.kind, json.RawMessage(test.raw))
		if err != nil || state.active != test.active || state.delay != test.delay || state.interval != test.interval {
			t.Fatalf("subscription timing = %+v, %v", state, err)
		}
	}
	for _, raw := range []string{`null`, `1`, `{}`, `[]`, `"0"`, `"0,0"`, `"0,999"`, `"60001,1000"`, `"0,60001"`, `"-1,1000"`, `"0,1000,1000"`} {
		if _, err := parseSessionSubscription("SessionsStart", json.RawMessage(raw)); err == nil {
			t.Fatalf("unbounded subscription accepted: %s", raw)
		}
	}
	if _, err := parseSessionSubscription("SessionsStop", json.RawMessage(`"0,1000"`)); err == nil {
		t.Fatal("stop accepted a timing declaration")
	}
	first, second := newSessionSubscription(), newSessionSubscription()
	if err := first.handle("SessionsStart", json.RawMessage(`"0,1000"`)); err != nil {
		t.Fatal(err)
	}
	if err := first.handle("SessionsStop", nil); err != nil {
		t.Fatal(err)
	}
	if err := first.handle("SessionsStart", json.RawMessage(`"100,60000"`)); err != nil {
		t.Fatal(err)
	}
	state := first.snapshot()
	if state.revision != 3 || !state.active || state.delay != 100*time.Millisecond || len(first.wake) != 1 || second.snapshot().active {
		t.Fatal("replacement did not coalesce locally to the latest subscription")
	}
	if err := first.handle("UnknownOperation", nil); err != nil || first.snapshot() != state {
		t.Fatal("an inert operation changed the subscription")
	}
}
