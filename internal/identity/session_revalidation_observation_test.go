package identity

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSessionObservationKeepsPolicyPrivateAndRechecksTime(t *testing.T) {
	start := time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)
	principal := Principal{
		Kind: "emby", Client: Client{DeviceID: "tv"}, PeerIP: "127.0.0.1", ExpiresAt: start.Add(2 * time.Hour),
		User: User{Policy: json.RawMessage(`{
			"EnableRemoteAccess":false,"EnableAllDevices":false,"EnabledDevices":["tv"],
			"EnableAllFolders":false,"EnabledFolders":["library"],"ExcludedSubFolders":["hidden"],
			"BlockedTags":["blocked"],"IncludeTags":["included"],"MaxParentalRating":5,
			"BlockUnratedItems":["Movie"],"RestrictedFeatures":["goby_playlists"],
			"EnableContentDeletionFromFolders":["deletable"],"LockedOutDate":0,
			"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":9,"EndHour":10}]
		}`)},
	}
	observation, err := observeRevalidatedSessionAt(principal, start)
	if err != nil {
		t.Fatal(err)
	}
	expected := observation.Policy()
	copy := observation.Policy()
	for _, values := range [][]string{copy.BlockedTags, copy.IncludeTags, copy.BlockUnratedItems,
		copy.RestrictedFeatures, copy.EnableContentDeletionFromFolders, copy.EnabledFolders,
		copy.ExcludedSubFolders, copy.EnabledDevices} {
		values[0] = "changed"
	}
	copy.AccessSchedules[0].EndHour = 24
	*copy.MaxParentalRating = 100
	principal.User.Policy[0] = '['
	principal.ExpiresAt = start.Add(24 * time.Hour)
	if !reflect.DeepEqual(observation.Policy(), expected) {
		t.Fatal("a policy projection changed the retained observation")
	}
	if err := observation.ValidateAt(start.Add(30 * time.Minute)); err != nil {
		t.Fatalf("fixed parsed facts were reparsed from changed principal JSON: %v", err)
	}
	if err := observation.ValidateAt(start.Add(time.Hour)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("the final clock check retained an ended schedule: %v", err)
	}
	if err := ValidateRevalidatedSessionAt(principal, start); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("the existing validation API ignored its current policy input: %v", err)
	}
}

func TestSessionObservationRetainsTheObservedExpiration(t *testing.T) {
	start := time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)
	principal := Principal{Kind: "emby", User: User{Policy: json.RawMessage(`{}`)}, ExpiresAt: start.Add(time.Hour)}
	observation, err := observeRevalidatedSessionAt(principal, start)
	if err != nil {
		t.Fatal(err)
	}
	principal.ExpiresAt = start.Add(24 * time.Hour)
	if err := observation.ValidateAt(start.Add(time.Hour - time.Nanosecond)); err != nil {
		t.Fatalf("a still-live observation was rejected: %v", err)
	}
	if err := observation.ValidateAt(start.Add(time.Hour)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("the final clock check extended the observed expiration: %v", err)
	}
	if err := (SessionObservation{}).ValidateAt(start); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("an empty observation authorized a session: %v", err)
	}
}

func TestSessionObservationPreservesLoginPolicyRejections(t *testing.T) {
	start := time.Date(2026, 10, 6, 9, 0, 0, 0, time.Local)
	for _, raw := range []string{
		`{"LockedOutDate":1}`, `{"LockedOutDate":"0"}`, `{"EnableRemoteAccess":null}`,
		`{"EnableRemoteAccess":false}`, `{"EnableAllDevices":false,"EnabledDevices":["other"]}`,
		`{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":10,"EndHour":11}]}`,
	} {
		principal := Principal{Kind: "emby", User: User{Policy: json.RawMessage(raw)},
			Client: Client{DeviceID: "tv"}, PeerIP: "192.0.2.10", ExpiresAt: start.Add(2 * time.Hour)}
		observation, err := observeRevalidatedSessionAt(principal, start)
		if !errors.Is(err, ErrUnauthorized) || !errors.Is(observation.ValidateAt(start), ErrUnauthorized) {
			t.Fatalf("rejected policy produced a usable observation: %v", err)
		}
	}
	principal := Principal{Kind: "emby", User: User{Policy: json.RawMessage(`{"EnableMediaPlayback":"invalid"}`)},
		ExpiresAt: start.Add(time.Hour)}
	observation, err := observeRevalidatedSessionAt(principal, start)
	if err != nil || observation.Policy().EnableMediaPlayback {
		t.Fatalf("playback-only fallback changed ordinary login authority: %v", err)
	}
}
