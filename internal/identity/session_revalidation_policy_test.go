package identity_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestValidateRevalidatedSessionAtPreservesLoginRestrictions(t *testing.T) {
	start := time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)
	for _, test := range []struct {
		name    string
		policy  string
		peer    string
		device  string
		at      time.Time
		allowed bool
	}{
		{"default", `{}`, "192.0.2.10", "tv", start, true},
		{"invalid_policy", `{"EnableRemoteAccess":null}`, "127.0.0.1", "tv", start, false},
		{"remote_denied", `{"EnableRemoteAccess":false}`, "192.0.2.10", "tv", start, false},
		{"local_allowed", `{"EnableRemoteAccess":false}`, "127.0.0.1", "tv", start, true},
		{"unknown_peer_denied", `{"EnableRemoteAccess":false}`, "", "tv", start, false},
		{"device_allowed", `{"EnableAllDevices":false,"EnabledDevices":["tv"]}`, "127.0.0.1", "tv", start, true},
		{"device_denied", `{"EnableAllDevices":false,"EnabledDevices":["tv"]}`, "127.0.0.1", "other", start, false},
		{"schedule_start", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]}`, "127.0.0.1", "tv", start, true},
		{"schedule_end", `{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]}`, "127.0.0.1", "tv", start.Add(time.Hour), false},
		{"locked_out", `{"LockedOutDate":1}`, "127.0.0.1", "tv", start, false},
		{"cleared_lockout", `{"LockedOutDate":0}`, "127.0.0.1", "tv", start, true},
		{"null_lockout", `{"LockedOutDate":null}`, "127.0.0.1", "tv", start, true},
		{"invalid_lockout", `{"LockedOutDate":"0"}`, "127.0.0.1", "tv", start, false},
		{"expired", `{}`, "127.0.0.1", "tv", start.Add(2 * time.Hour), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			principal := identity.Principal{
				Kind: "emby", User: identity.User{Policy: json.RawMessage(test.policy)},
				Client: identity.Client{DeviceID: test.device}, PeerIP: test.peer,
				ExpiresAt: start.Add(2 * time.Hour),
			}
			err := identity.ValidateRevalidatedSessionAt(principal, test.at)
			if test.allowed && err != nil || !test.allowed && !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("allowed=%t, error=%v", test.allowed, err)
			}
		})
	}
}
