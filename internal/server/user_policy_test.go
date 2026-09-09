package server

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

const userPolicySecretMarker = "policy-secret-marker-not-public"

func projectedUserPolicyForTest(t *testing.T, user identity.User) map[string]any {
	t.Helper()
	encoded, err := json.Marshal((&Server{serverID: "server-id"}).userDTO(user))
	if err != nil {
		t.Fatalf("marshal projected user policy: %v", err)
	}
	var dto map[string]any
	if err := json.Unmarshal(encoded, &dto); err != nil {
		t.Fatalf("decode projected user policy: %v", err)
	}
	return objectValue(t, dto, "Policy")
}

func assertUserPolicyWhitelist(t *testing.T, policy map[string]any) {
	t.Helper()
	allowed := map[string]bool{
		"IsAdministrator": true, "IsDisabled": true, "EnableMediaPlayback": true,
		"EnableAllFolders": true, "EnabledFolders": true, "EnablePlaybackRemuxing": true,
		"EnableAudioPlaybackTranscoding": true, "EnableVideoPlaybackTranscoding": true, "EnableContentDeletion": true,
	}
	if len(policy) != len(allowed) {
		t.Errorf("projected policy has %d fields, want the %d supported fields: %#v", len(policy), len(allowed), policy)
	}
	for key, value := range policy {
		if !allowed[key] {
			t.Errorf("projected policy exposes unsupported stored field %s", key)
		}
		if key == "EnabledFolders" {
			if _, ok := value.([]any); !ok {
				t.Errorf("EnabledFolders must be a non-null JSON array: %#v", value)
			}
		} else if _, ok := value.(bool); !ok {
			t.Errorf("policy field %s must be a JSON boolean: %#v", key, value)
		}
	}
	for _, field := range []string{"EnablePlaybackRemuxing", "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding", "EnableContentDeletion"} {
		if value, ok := policy[field].(bool); !ok || value {
			t.Errorf("unsupported capability %s must remain false: %#v", field, policy[field])
		}
	}
}

func assertNoStoredPolicyExposure(t *testing.T, value any, forbiddenKeys ...string) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			for _, forbidden := range forbiddenKeys {
				if strings.EqualFold(key, forbidden) {
					t.Errorf("response exposes private policy field %s", key)
				}
			}
			assertNoStoredPolicyExposure(t, child, forbiddenKeys...)
		}
	case []any:
		for _, child := range value {
			assertNoStoredPolicyExposure(t, child, forbiddenKeys...)
		}
	case string:
		if strings.Contains(value, userPolicySecretMarker) {
			t.Error("response exposes the stored policy secret marker")
		}
	}
}

func TestUserDTOPolicyPlaybackIsExplicitAndFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name, raw       string
		admin, disabled bool
		playback        bool
	}{
		{name: "nil_policy"},
		{name: "empty_object_defaults_to_playback", raw: `{}`, playback: true},
		{name: "explicit_true", raw: `{"EnableMediaPlayback":true}`, playback: true},
		{name: "explicit_false", raw: `{"EnableMediaPlayback":false}`},
		{name: "explicit_null", raw: `{"EnableMediaPlayback":null}`},
		{name: "string_true_is_not_boolean", raw: `{"EnableMediaPlayback":"true"}`},
		{name: "string_false_is_not_boolean", raw: `{"EnableMediaPlayback":"false"}`},
		{name: "numeric_one_is_not_boolean", raw: `{"EnableMediaPlayback":1}`},
		{name: "numeric_zero_is_not_boolean", raw: `{"EnableMediaPlayback":0}`},
		{name: "array_is_not_boolean", raw: `{"EnableMediaPlayback":[]}`},
		{name: "object_is_not_boolean", raw: `{"EnableMediaPlayback":{}}`},
		{name: "whole_policy_null", raw: `null`},
		{name: "whole_policy_true", raw: `true`},
		{name: "whole_policy_false", raw: `false`},
		{name: "whole_policy_string", raw: `"policy"`},
		{name: "whole_policy_array", raw: `[]`},
		{name: "malformed_policy", raw: `{`},
		{name: "whitespace_policy", raw: " "},
		{name: "stored_role_claims_do_not_override_columns", raw: `{"IsAdministrator":true,"IsDisabled":true}`, playback: true},
		{name: "disabled_user_cannot_enable_playback", raw: `{"EnableMediaPlayback":true,"IsDisabled":false}`, disabled: true},
		{name: "administrator_playback_can_be_denied", raw: `{"EnableMediaPlayback":false}`, admin: true},
		{name: "administrator_bad_policy_fails_closed", raw: `true`, admin: true},
		{name: "administrator_column_wins", raw: `{"IsAdministrator":false,"EnableMediaPlayback":true}`, admin: true, playback: true},
		{name: "disabled_administrator_cannot_enable_playback", raw: `{"EnableMediaPlayback":true}`, admin: true, disabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var raw json.RawMessage
			if test.raw != "" {
				raw = json.RawMessage(test.raw)
			}
			user := identity.User{ID: "user-id", Name: "Policy User", IsAdministrator: test.admin, IsDisabled: test.disabled, Policy: raw}
			policy := projectedUserPolicyForTest(t, user)
			assertUserPolicyWhitelist(t, policy)
			if policy["EnableMediaPlayback"] != test.playback || policy["IsAdministrator"] != test.admin || policy["IsDisabled"] != test.disabled {
				t.Errorf("projected playback or identity does not match trusted state: %#v", policy)
			}
		})
	}
}

func TestUserDTOPolicyFolderAccessUsesSafeDefaultsAndColumnRoles(t *testing.T) {
	for _, test := range []struct {
		name, raw       string
		admin, disabled bool
		all             bool
		folders         []string
	}{
		{name: "nil_policy"},
		{name: "empty_object_grants_all", raw: `{}`, all: true},
		{name: "missing_flag_ignores_folder_list", raw: `{"EnabledFolders":["ignored"]}`, all: true},
		{name: "true_flag_ignores_wrong_folder_type", raw: `{"EnableAllFolders":true,"EnabledFolders":42}`, all: true},
		{name: "true_flag_ignores_folder_list", raw: `{"EnableAllFolders":true,"EnabledFolders":["ignored"]}`, all: true},
		{name: "false_flag_preserves_explicit_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":["library-a","library-b"]}`, folders: []string{"library-a", "library-b"}},
		{name: "false_flag_without_folders", raw: `{"EnableAllFolders":false}`},
		{name: "false_flag_with_null_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":null}`},
		{name: "false_flag_with_empty_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":[]}`},
		{name: "false_flag_with_string_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":"library-a"}`},
		{name: "false_flag_with_object_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":{}}`},
		{name: "false_flag_with_boolean_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":true}`},
		{name: "mixed_folder_types_discard_partial_results", raw: `{"EnableAllFolders":false,"EnabledFolders":["library-a",42]}`},
		{name: "null_flag_fails_closed", raw: `{"EnableAllFolders":null,"EnabledFolders":["library-a"]}`},
		{name: "string_flag_fails_closed", raw: `{"EnableAllFolders":"false","EnabledFolders":["library-a"]}`},
		{name: "numeric_flag_fails_closed", raw: `{"EnableAllFolders":1,"EnabledFolders":["library-a"]}`},
		{name: "array_flag_fails_closed", raw: `{"EnableAllFolders":[],"EnabledFolders":["library-a"]}`},
		{name: "whole_policy_null", raw: `null`},
		{name: "whole_policy_array", raw: `[]`},
		{name: "whole_policy_boolean", raw: `true`},
		{name: "malformed_policy", raw: `{`},
		{name: "administrator_bypasses_restricted_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":["ignored"]}`, admin: true, all: true},
		{name: "administrator_bypasses_invalid_folder_policy", raw: `true`, admin: true, all: true},
		{name: "administrator_bypasses_missing_policy", admin: true, all: true},
		{name: "disabled_user_denied_all_folders", raw: `{"EnableAllFolders":true}`, disabled: true},
		{name: "disabled_user_hides_explicit_folders", raw: `{"EnableAllFolders":false,"EnabledFolders":["library-a"]}`, disabled: true},
		{name: "disabled_administrator_denied_all_folders", raw: `{}`, admin: true, disabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := identity.User{ID: "user-id", IsAdministrator: test.admin, IsDisabled: test.disabled, Policy: json.RawMessage(test.raw)}
			policy := projectedUserPolicyForTest(t, user)
			assertUserPolicyWhitelist(t, policy)
			wantedFolders := make([]any, len(test.folders))
			for index, folder := range test.folders {
				wantedFolders[index] = folder
			}
			if policy["EnableAllFolders"] != test.all || !reflect.DeepEqual(policy["EnabledFolders"], wantedFolders) {
				t.Errorf("projected folder access = %#v/%#v, want %v/%#v", policy["EnableAllFolders"], policy["EnabledFolders"], test.all, wantedFolders)
			}
		})
	}
}

func TestUserPolicyProjectionDoesNotMutateOrExposeRawPolicy(t *testing.T) {
	user := identity.User{
		ID: "user-id", Name: "Policy User",
		Policy: json.RawMessage(`{"SensitiveMarker":"` + userPolicySecretMarker + `","IsAdministrator":true,"IsDisabled":true,` +
			`"EnableMediaPlayback":true,"EnablePlaybackRemuxing":true,"EnableAudioPlaybackTranscoding":true,` +
			`"EnableVideoPlaybackTranscoding":true,"EnableContentDeletion":true,"EnableAllFolders":false,"EnabledFolders":["library-a"]}`),
	}
	before := append([]byte(nil), user.Policy...)
	policy := projectedUserPolicyForTest(t, user)
	assertUserPolicyWhitelist(t, policy)
	assertNoStoredPolicyExposure(t, policy, "SensitiveMarker")
	if policy["IsAdministrator"] != false || policy["IsDisabled"] != false || policy["EnableMediaPlayback"] != true {
		t.Errorf("raw policy changed trusted identity columns or playback projection: %#v", policy)
	}
	if !bytes.Equal(before, user.Policy) {
		t.Error("user DTO projection modified the original raw policy")
	}
	for _, value := range []any{nativeUser(user), user} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal native user without private policy: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		assertNoStoredPolicyExposure(t, decoded, "Policy", "EnabledFolders", "SensitiveMarker")
	}
}
