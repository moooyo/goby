package identity

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagedPolicyParsesRestrictionsWithoutExposingPrivateFields(t *testing.T) {
	policy, err := ParseStoredManagedPolicy(json.RawMessage(`{
		"PrivateExtension":{"Secret":"not-public"},"MaxParentalRating":13,
		"EnableAllDevices":false,"EnabledDevices":["tv-b","tv-a","tv-a"],
		"AccessSchedules":[{"DayOfWeek":"Weekday","StartHour":9.5,"EndHour":18}],
		"BlockedTags":["restricted"],"BlockUnratedItems":["Movie","Series"],
		"RemoteClientBitrateLimit":1000000,"SimultaneousStreamLimit":2,
		"EnableRemoteAccess":false,"EnableUserPreferenceAccess":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxParentalRating == nil || *policy.MaxParentalRating != 13 || policy.EnableRemoteAccess || policy.EnableUserPreferenceAccess ||
		policy.RemoteClientBitrateLimit != 1000000 || policy.SimultaneousStreamLimit != 2 {
		t.Fatalf("restrictions were not parsed: %+v", policy)
	}
	if !reflect.DeepEqual(policy.EnabledDevices, []string{"tv-a", "tv-b"}) || !policy.AllowsDevice("tv-a") || policy.AllowsDevice("") || policy.AllowsDevice("other") {
		t.Fatal("device allowlist did not retain canonical exact identifiers")
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "not-public") || strings.Contains(string(encoded), "PrivateExtension") {
		t.Fatal("public policy projection exposed unknown stored fields")
	}
}

func TestManagedPolicyRejectsAmbiguousOrMalformedRestrictions(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`,
		`{"EnableAllDevices":true,"EnableAllDevices":false}`,
		`{"EnableAllDevices":true,"enablealldevices":false}`,
		`{"enableremoteaccess":true}`,
		`{"EnableRemoteAccess":null}`, `{"EnableRemoteAccess":"false"}`,
		`{"EnabledDevices":null}`, `{"EnabledDevices":[null]}`, `{"EnabledDevices":[""]}`,
		`{"MaxParentalRating":-1}`, `{"MaxParentalRating":2147483648}`, `{"MaxParentalRating":13.5}`,
		`{"SimultaneousStreamLimit":-1}`, `{"RemoteClientBitrateLimit":2147483648}`,
		`{"BlockUnratedItems":["Audio"]}`,
		`{"AccessSchedules":[null]}`,
		`{"AccessSchedules":[{"DayOfWeek":"Monday","StartHour":0}]}`,
		`{"AccessSchedules":[{"DayOfWeek":"monday","StartHour":0,"EndHour":24}]}`,
		`{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":23,"EndHour":2}]}`,
		`{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":0,"EndHour":25}]}`,
		`{"AccessSchedules":[{"DayOfWeek":"Everyday","StartHour":0,"starthour":1,"EndHour":24}]}`,
		`{"EnableAllFolders":true} {"EnableAllDevices":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseManagedPolicy(json.RawMessage(raw)); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid policy accepted: %v", err)
			}
			projected := ProjectManagedPolicy(json.RawMessage(raw))
			if projected.EnableAllFolders || projected.EnableAllDevices || projected.EnableMediaPlayback || projected.EnableRemoteAccess || projected.AllowsAccessAt(time.Now()) {
				t.Fatal("invalid policy projected a permission")
			}
		})
	}
	for _, raw := range []json.RawMessage{json.RawMessage(strings.Repeat(" ", MaxManagedPolicyBytes+1)), {0xff}} {
		if _, err := ParseManagedPolicy(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid policy encoding accepted")
		}
	}
}

func TestManagedPolicySchedulesUseDayUnionAndExclusiveEnd(t *testing.T) {
	policy := DefaultManagedPolicy()
	policy.AccessSchedules = []AccessSchedule{{DayOfWeek: "Weekday", StartHour: 9.5, EndHour: 18}, {DayOfWeek: "Weekend", StartHour: 12, EndHour: 14}}
	for _, test := range []struct {
		instant time.Time
		allowed bool
	}{
		{time.Date(2026, 9, 21, 9, 29, 59, 0, time.Local), false},
		{time.Date(2026, 9, 21, 9, 30, 0, 0, time.Local), true},
		{time.Date(2026, 9, 21, 17, 59, 59, 0, time.Local), true},
		{time.Date(2026, 9, 21, 18, 0, 0, 0, time.Local), false},
		{time.Date(2026, 9, 20, 9, 30, 0, 0, time.Local), false},
		{time.Date(2026, 9, 20, 12, 0, 0, 0, time.Local), true},
		{time.Date(2026, 9, 20, 14, 0, 0, 0, time.Local), false},
	} {
		if got := policy.AllowsAccessAt(test.instant); got != test.allowed {
			t.Errorf("access at %v = %v", test.instant, got)
		}
	}
	policy.AccessSchedules = []AccessSchedule{{DayOfWeek: "Everyday", StartHour: math.NaN(), EndHour: 24}}
	if policy.AllowsAccessAt(time.Now()) {
		t.Fatal("invalid direct schedule granted access")
	}
}

func TestManagedPolicyCanonicalizationCopiesAllMutableValues(t *testing.T) {
	rating := 13
	input := DefaultManagedPolicy()
	input.MaxParentalRating = &rating
	input.EnabledDevices = []string{"b", "a", "b"}
	input.AccessSchedules = []AccessSchedule{{DayOfWeek: "Sunday", StartHour: 0, EndHour: 24}}
	fields := make(map[string]string)
	result := canonicalManagedPolicy(input, fields)
	if len(fields) != 0 {
		t.Fatal(fields)
	}
	*result.MaxParentalRating = 18
	result.EnabledDevices[0] = "changed"
	result.AccessSchedules[0].EndHour = 1
	if rating != 13 || input.EnabledDevices[0] != "b" || input.AccessSchedules[0].EndHour != 24 {
		t.Fatal("validation retained caller-owned mutable values")
	}
}

func TestLoginPolicyHonorsLockedOutStateWithoutInventingThresholds(t *testing.T) {
	for _, raw := range []string{`{"LockedOutDate":1}`, `{"LockedOutDate":-1}`, `{"LockedOutDate":"1"}`} {
		if loginPolicyAllows(json.RawMessage(raw), "", time.Now()) {
			t.Fatal("locked account granted login access")
		}
	}
	if !loginPolicyAllows(json.RawMessage(`{"InvalidLoginAttemptCount":123}`), "", time.Now()) {
		t.Fatal("an undocumented failure threshold was applied")
	}
}

func TestLocalPeerClassificationUsesOnlyTrustedIPAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "10.0.0.4", "192.168.1.2", "172.16.0.1", "169.254.1.1", "fc00::1", "::ffff:192.168.1.1"} {
		if !IsLocalPeer(address) {
			t.Errorf("local peer rejected: %s", address)
		}
	}
	for _, address := range []string{"", "8.8.8.8", "2001:4860:4860::8888", "127.0.0.1:80", "127.0.0.1, 8.8.8.8", "localhost", "0.0.0.0"} {
		if IsLocalPeer(address) {
			t.Errorf("nonlocal or untrusted peer accepted: %s", address)
		}
	}
}

func TestClientSessionControlRespectsOwnerSharedAndCrossUserPolicy(t *testing.T) {
	user := User{ID: "viewer", Policy: json.RawMessage(`{}`)}
	own := ClientSession{Kind: "emby", UserID: user.ID}
	other := ClientSession{Kind: "emby", UserID: "another-viewer"}
	shared := ClientSession{Kind: ApplicationKeyKind}
	if !CanControlClientSession(user, own) || CanControlClientSession(user, other) || CanControlClientSession(user, shared) {
		t.Fatal("missing policy changed the existing ownership boundary")
	}
	user.Policy = json.RawMessage(`{"EnableRemoteControlOfOtherUsers":true}`)
	if !CanControlClientSession(user, other) || CanControlClientSession(user, shared) {
		t.Fatal("cross-user flag granted unrelated shared-device authority")
	}
	user.Policy = json.RawMessage(`{"EnableSharedDeviceControl":true}`)
	if CanControlClientSession(user, other) || !CanControlClientSession(user, shared) {
		t.Fatal("shared-device flag granted unrelated cross-user authority")
	}
	user.IsAdministrator = true
	if !CanControlClientSession(user, other) || !CanControlClientSession(user, shared) {
		t.Fatal("administrator could not manage sessions")
	}
	user.IsDisabled = true
	if CanControlClientSession(user, own) || CanControlClientSession(user, shared) {
		t.Fatal("disabled administrator retained control")
	}
	user.IsDisabled = false
	user.Policy = json.RawMessage(`{"EnableRemoteControlOfOtherUsers":null}`)
	if CanControlClientSession(user, other) {
		t.Fatal("malformed policy retained control")
	}
}
