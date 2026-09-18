package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestUserManagementObjectCanonicalizesCompatibilityFieldsWithoutAmbiguity(t *testing.T) {
	values, invalid := userManagementObject([]byte(`{"name":"Viewer","ID":"user-id"}`), []string{"Name", "Id"}, true, "")
	if invalid != nil || string(values["Name"]) != `"Viewer"` || string(values["Id"]) != `"user-id"` {
		t.Fatalf("compatibility names were not canonicalized: %#v, %#v", values, invalid)
	}
	for _, body := range []string{
		`{"Name":"a","name":"b"}`, `{"Name":"a","Na\u006de":"a"}`, `{"Unknown":true}`,
		`null`, `[]`, `{} {}`, `{"Name":"bad` + string([]byte{0xff}) + `"}`,
	} {
		if _, invalid := userManagementObject([]byte(body), []string{"Name", "Id"}, true, ""); len(invalid) == 0 {
			t.Errorf("ambiguous or malformed body accepted: %q", body)
		}
	}
	if _, invalid := userManagementObject([]byte(`{"name":"Viewer"}`), []string{"Name"}, false, ""); len(invalid) == 0 {
		t.Error("native field casing was weakened")
	}
}

func TestManagedUserLegacyUpdatePreservesExpandedPolicy(t *testing.T) {
	rating := 13
	base := identity.DefaultManagedPolicy()
	base.IsHidden = true
	base.MaxParentalRating = &rating
	base.BlockedTags = []string{"adult"}
	base.AccessSchedules = []identity.AccessSchedule{{DayOfWeek: "Weekday", StartHour: 8, EndHour: 20}}
	base.EnabledDevices = []string{"living-room"}
	base.EnableAllDevices = false
	base.RemoteClientBitrateLimit = 8000000
	response := httptest.NewRecorder()
	input, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(managedUserUpdateJSON, "application/json"), base)
	if !ok {
		t.Fatalf("legacy request rejected: %s", response.Body.String())
	}
	if !input.Policy.IsHidden || input.Policy.MaxParentalRating == nil || *input.Policy.MaxParentalRating != rating ||
		!reflect.DeepEqual(input.Policy.BlockedTags, base.BlockedTags) || !reflect.DeepEqual(input.Policy.AccessSchedules, base.AccessSchedules) ||
		!reflect.DeepEqual(input.Policy.EnabledDevices, base.EnabledDevices) || input.Policy.EnableAllDevices || input.Policy.RemoteClientBitrateLimit != 8000000 {
		t.Fatalf("legacy update replaced omitted policy fields: %#v", input.Policy)
	}
	if input.Policy.EnableAllFolders || !input.Policy.EnableMediaPlayback || input.Policy.EnablePlaybackRemuxing {
		t.Fatalf("legacy editable fields did not apply: %#v", input.Policy)
	}
	if !reflect.DeepEqual(base.BlockedTags, []string{"adult"}) || *base.MaxParentalRating != rating {
		t.Fatal("decoding changed the base policy")
	}
}

func TestManagedUserExpandedPolicyInputAndProjection(t *testing.T) {
	body := managedUserJSONForTest(t, managedUserUpdateJSON, func(object map[string]any) {
		policy := object["Policy"].(map[string]any)
		policy["IsHiddenRemotely"] = true
		policy["MaxParentalRating"] = 17
		policy["IncludeTags"] = []string{"family"}
		policy["EnableContentDeletion"] = true
		policy["SimultaneousStreamLimit"] = 2
		policy["AccessSchedules"] = []map[string]any{{"DayOfWeek": "Weekend", "StartHour": 9.5, "EndHour": 21}}
	})
	response := httptest.NewRecorder()
	input, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json"))
	if !ok {
		t.Fatalf("expanded request rejected: %s", response.Body.String())
	}
	if !input.Policy.IsHiddenRemotely || !input.Policy.EnableContentDeletion || input.Policy.SimultaneousStreamLimit != 2 ||
		input.Policy.MaxParentalRating == nil || *input.Policy.MaxParentalRating != 17 || !reflect.DeepEqual(input.Policy.IncludeTags, []string{"family"}) {
		t.Fatalf("expanded policy values were lost: %#v", input.Policy)
	}
	encoded, err := json.Marshal(nativeManagedUser(identity.ManagedUser{Policy: input.Policy}))
	if err != nil {
		t.Fatal(err)
	}
	var dto map[string]any
	if err := json.Unmarshal(encoded, &dto); err != nil {
		t.Fatal(err)
	}
	policy := objectValue(t, dto, "Policy")
	if policy["IsHiddenRemotely"] != true || policy["EnableContentDeletion"] != true || policy["MaxParentalRating"] != float64(17) || policy["SimultaneousStreamLimit"] != float64(2) {
		t.Fatalf("native expanded policy projection changed configured values: %#v", policy)
	}
	for _, field := range []string{"EnabledDevices", "ExcludedSubFolders", "BlockedTags", "BlockUnratedItems", "RestrictedFeatures", "EnableContentDeletionFromFolders"} {
		if _, ok := policy[field].([]any); !ok {
			t.Errorf("%s must be a non-null array: %#v", field, policy[field])
		}
	}
}

func TestManagedPolicyMergeRejectsMalformedExpandedValues(t *testing.T) {
	for _, test := range []struct{ field, raw string }{
		{"IsHidden", `"true"`}, {"BlockedTags", `[null]`}, {"BlockedTags", `null`},
		{"EnableAllDevices", `null`}, {"SimultaneousStreamLimit", `-1`},
		{"MaxParentalRating", `"13"`}, {"RemoteClientBitrateLimit", `2147483648`},
		{"AccessSchedules", `[{"DayOfWeek":"Everyday","StartHour":22,"EndHour":6}]`},
		{"AccessSchedules", `[{"DayOfWeek":"Everyday","StartHour":0,"EndHour":24,"EndHour":1}]`},
	} {
		if _, err := mergeManagedPolicy(identity.DefaultManagedPolicy(), map[string]json.RawMessage{test.field: json.RawMessage(test.raw)}); err == nil {
			t.Errorf("invalid %s accepted: %s", test.field, test.raw)
		}
	}
}

func TestPublicPolicyVisibilityHonorsNetworkDeviceAndHiddenRules(t *testing.T) {
	for _, test := range []struct {
		name, raw, device  string
		remote, used, want bool
	}{
		{name: "legacy_local", raw: `{}`, want: true},
		{name: "legacy_remote", raw: `{}`, remote: true, want: true},
		{name: "hidden", raw: `{"IsHidden":true}`},
		{name: "hidden_remote_local", raw: `{"IsHiddenRemotely":true}`, want: true},
		{name: "hidden_remote_external", raw: `{"IsHiddenRemotely":true}`, remote: true},
		{name: "remote_denied", raw: `{"EnableRemoteAccess":false}`, remote: true},
		{name: "unused_device", raw: `{"IsHiddenFromUnusedDevices":true}`, device: "tv"},
		{name: "used_device", raw: `{"IsHiddenFromUnusedDevices":true}`, device: "tv", used: true, want: true},
		{name: "device_allowlist_missing", raw: `{"EnableAllDevices":false,"EnabledDevices":["tv"]}`},
		{name: "device_allowlist_other", raw: `{"EnableAllDevices":false,"EnabledDevices":["tv"]}`, device: "phone"},
		{name: "device_allowlist_match", raw: `{"EnableAllDevices":false,"EnabledDevices":["tv"]}`, device: "tv", want: true},
		{name: "malformed_policy", raw: `{"IsHidden":"false"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := identity.User{Policy: json.RawMessage(test.raw)}
			if got := publicPolicyVisible(user, test.remote, test.device, test.used); got != test.want {
				t.Errorf("visible = %v, want %v", got, test.want)
			}
			user.IsDisabled = true
			if publicPolicyVisible(user, test.remote, test.device, test.used) {
				t.Error("disabled account is public")
			}
			user.IsDisabled = false
			user.IsAdministrator = true
			if publicPolicyVisible(user, test.remote, test.device, test.used) {
				t.Error("administrator account is public")
			}
		})
	}
}

func TestEmbyPolicyProjectionKeepsDeferredCapabilitiesInertAndStateHonest(t *testing.T) {
	user := identity.User{Policy: json.RawMessage(`{"EnableLiveTvAccess":true,"EnableSyncTranscoding":true,"EnableAllChannels":true,"LockedOutDate":638000000000000000,"InvalidLoginAttemptCount":7}`)}
	policy := embyUserPolicy(user)
	for _, field := range []string{"EnableLiveTvAccess", "EnableSyncTranscoding", "EnableAllChannels"} {
		if policy[field] != false {
			t.Errorf("deferred feature %s was advertised: %#v", field, policy[field])
		}
	}
	if policy["LockedOutDate"] != int64(638000000000000000) || policy["InvalidLoginAttemptCount"] != int64(7) {
		t.Fatalf("stored read-only state was fabricated: %#v", policy)
	}
}

func TestEmbyUserManagementBodyRejectsDuplicateAndUnknownInputs(t *testing.T) {
	for _, body := range []string{`{"Name":"a","name":"b"}`, `{"Name":"a","Password":"secret"}`, `null`, `{}`} {
		request := httptest.NewRequest(http.MethodPost, "/emby/Users/New", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		values, ok := embyUserManagementBody(response, request, []string{"Name"})
		if body == `{}` {
			if !ok || len(values) != 0 {
				t.Fatal("body parser must leave operation-specific required fields to its handler")
			}
		} else if ok || response.Code != http.StatusBadRequest {
			t.Errorf("invalid body accepted: %s", body)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/emby/Users/New?Name=override", bytes.NewBufferString(`{"Name":"a"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if _, ok := embyUserManagementBody(response, request, []string{"Name"}); ok || response.Code != http.StatusBadRequest {
		t.Fatal("query alias bypassed the JSON write contract")
	}
}
