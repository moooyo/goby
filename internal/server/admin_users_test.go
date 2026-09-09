package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

const managedUserUpdateJSON = `{"Revision":"1","Name":"Managed User","IsAdministrator":false,"IsDisabled":false,"Policy":{"EnableAllFolders":false,"EnabledFolders":["library-a","library-b"],"EnableMediaPlayback":true,"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":false}}`
const managedUserPasswordJSON = `{"Revision":"1","Password":"new-password"}`
const managedUserRequestID = "managed-user-request"

func managedUserRequestForTest(body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/users/user-id", strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, managedUserRequestID))
}

func managedUserJSONForTest(t *testing.T, body string, change func(map[string]any)) string {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(body), &object); err != nil {
		t.Fatalf("decode test input: %v", err)
	}
	change(object)
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("encode test input: %v", err)
	}
	return string(encoded)
}

func assertManagedUserDecodeError(t *testing.T, response *httptest.ResponseRecorder, status int, code, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("response status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode native error: %v", err)
	}
	if envelope.Error.Code != code {
		t.Errorf("native error code = %q, want %q", envelope.Error.Code, code)
	}
	if envelope.RequestID != managedUserRequestID {
		t.Errorf("native request ID = %q, want %q", envelope.RequestID, managedUserRequestID)
	}
	if status == http.StatusBadRequest && len(envelope.Error.Fields) == 0 {
		t.Error("invalid input must include field errors")
	}
	for key, message := range envelope.Error.Fields {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(message) == "" {
			t.Errorf("invalid field error %q: %q", key, message)
		}
	}
	if field != "" && strings.TrimSpace(envelope.Error.Fields[field]) == "" {
		t.Errorf("missing field error for %s: %#v", field, envelope.Error.Fields)
	}
}

func TestManagedUserIDAcceptsOpaqueIdentifiersWithinByteLimit(t *testing.T) {
	for _, test := range []struct {
		name, id string
	}{
		{name: "single_byte", id: "a"},
		{name: "opaque_nonhex", id: "account:nonhex-id.with_underscores"},
		{name: "internal_space", id: "account one"},
		{name: "unicode", id: "account-Caf\u00e9"},
		{name: "maximum_ascii_bytes", id: strings.Repeat("a", 256)},
		{name: "maximum_unicode_bytes", id: strings.Repeat("\u00e9", 128)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := managedUserRequestForTest("", "")
			request.SetPathValue("id", test.id)
			response := httptest.NewRecorder()
			id, ok := managedUserID(response, request)
			if !ok || id != test.id {
				t.Fatalf("valid path ID changed or rejected: got %q, valid = %v, response = %s", id, ok, response.Body.String())
			}
			if response.Body.Len() != 0 {
				t.Errorf("valid path ID wrote a response: %s", response.Body.String())
			}
		})
	}
}

func TestManagedUserIDRejectsInvalidPathValues(t *testing.T) {
	for _, test := range []struct {
		name, id string
	}{
		{name: "empty"},
		{name: "nul", id: "user\x00id"},
		{name: "newline", id: "user\nid"},
		{name: "tab", id: "user\tid"},
		{name: "leading_space", id: " user-id"},
		{name: "trailing_space", id: "user-id "},
		{name: "only_spaces", id: "   "},
		{name: "unicode_control", id: "user\u0085id"},
		{name: "leading_unicode_space", id: "\u00a0user-id"},
		{name: "trailing_unicode_space", id: "user-id\u2003"},
		{name: "invalid_utf8", id: "user\xffid"},
		{name: "too_many_ascii_bytes", id: strings.Repeat("a", 257)},
		{name: "too_many_unicode_bytes", id: strings.Repeat("\u00e9", 129)},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := managedUserRequestForTest("", "")
			request.SetPathValue("id", test.id)
			response := httptest.NewRecorder()
			if _, ok := managedUserID(response, request); ok {
				t.Fatalf("invalid path ID accepted: %q", test.id)
			}
			assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "Id")
		})
	}
}

func TestDecodeManagedUserUpdatePreservesSupportedConfiguration(t *testing.T) {
	for _, test := range []struct {
		name     string
		revision string
		want     int64
	}{
		{name: "initial_revision", revision: "1", want: 1},
		{name: "revision_above_javascript_precision", revision: "9007199254740993", want: 9007199254740993},
		{name: "maximum_revision", revision: "9223372036854775807", want: 9223372036854775807},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := managedUserJSONForTest(t, managedUserUpdateJSON, func(object map[string]any) {
				object["Revision"] = test.revision
				object["IsAdministrator"] = true
				object["IsDisabled"] = true
			})
			response := httptest.NewRecorder()
			input, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json; charset=utf-8"))
			if !ok {
				t.Fatalf("valid update rejected: %s", response.Body.String())
			}
			want := identity.ManagedUserUpdate{
				Revision: test.want, Name: "Managed User", IsAdministrator: true, IsDisabled: true,
				Policy: identity.ManagedPolicy{
					EnableAllFolders: false, EnabledFolders: []string{"library-a", "library-b"},
					EnableMediaPlayback: true, EnablePlaybackRemuxing: false,
					EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: false,
				},
			}
			if !reflect.DeepEqual(input, want) {
				t.Errorf("decoded update = %#v, want %#v", input, want)
			}
			if response.Body.Len() != 0 {
				t.Errorf("successful decoding wrote a response: %s", response.Body.String())
			}
		})
	}
}

func TestDecodeManagedUserUpdateRequiresEveryFieldWithExactTypes(t *testing.T) {
	for _, field := range []struct {
		path  string
		wrong any
	}{
		{path: "Revision", wrong: 1},
		{path: "Name", wrong: false},
		{path: "IsAdministrator", wrong: "false"},
		{path: "IsDisabled", wrong: 0},
		{path: "Policy", wrong: []any{}},
		{path: "Policy.EnableAllFolders", wrong: "true"},
		{path: "Policy.EnabledFolders", wrong: "library-a"},
		{path: "Policy.EnableMediaPlayback", wrong: 1},
		{path: "Policy.EnablePlaybackRemuxing", wrong: "false"},
		{path: "Policy.EnableAudioPlaybackTranscoding", wrong: []any{}},
		{path: "Policy.EnableVideoPlaybackTranscoding", wrong: map[string]any{}},
	} {
		for _, invalid := range []struct {
			name    string
			missing bool
			value   any
		}{
			{name: "missing", missing: true},
			{name: "null", value: nil},
			{name: "wrong_type", value: field.wrong},
		} {
			t.Run(field.path+"/"+invalid.name, func(t *testing.T) {
				body := managedUserJSONForTest(t, managedUserUpdateJSON, func(object map[string]any) {
					key := field.path
					if nested, ok := strings.CutPrefix(key, "Policy."); ok {
						object = object["Policy"].(map[string]any)
						key = nested
					}
					if invalid.missing {
						delete(object, key)
					} else {
						object[key] = invalid.value
					}
				})
				response := httptest.NewRecorder()
				if _, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json")); ok {
					t.Fatal("invalid update accepted")
				}
				assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", field.path)
			})
		}
	}
}

func TestDecodeManagedUserUpdateRejectsNonStringFolderElements(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "null"},
		{name: "number", value: 1},
		{name: "boolean", value: true},
		{name: "object", value: map[string]any{}},
		{name: "array", value: []any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := managedUserJSONForTest(t, managedUserUpdateJSON, func(object map[string]any) {
				object["Policy"].(map[string]any)["EnabledFolders"] = []any{"library-a", test.value}
			})
			response := httptest.NewRecorder()
			if _, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json")); ok {
				t.Fatal("invalid folder element accepted")
			}
			assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "Policy.EnabledFolders")
		})
	}
}

func TestDecodeManagedUserUpdateLeavesDomainValidationToIdentity(t *testing.T) {
	body := managedUserJSONForTest(t, managedUserUpdateJSON, func(object map[string]any) {
		object["Name"] = ""
		object["Policy"].(map[string]any)["EnabledFolders"] = []string{}
	})
	response := httptest.NewRecorder()
	input, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json"))
	if !ok || input.Name != "" || input.Policy.EnabledFolders == nil || len(input.Policy.EnabledFolders) != 0 {
		t.Fatalf("structurally valid empty values were changed or rejected: %#v, %s", input, response.Body.String())
	}
}

func TestDecodeManagedUserPasswordPreservesRevisionAndNonemptyPassword(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     string
		revision int64
		password string
	}{
		{name: "ordinary_password", body: managedUserPasswordJSON, revision: 1, password: "new-password"},
		{name: "domain_strength_check_is_deferred", body: `{"Revision":"1","Password":"x"}`, revision: 1, password: "x"},
		{name: "password_is_not_trimmed", body: `{"Revision":"2","Password":"  password  "}`, revision: 2, password: "  password  "},
		{name: "maximum_revision", body: `{"Revision":"9223372036854775807","Password":"new-password"}`, revision: 9223372036854775807, password: "new-password"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			revision, password, ok := decodeManagedUserPassword(response, managedUserRequestForTest(test.body, "application/json"))
			if !ok || revision != test.revision || password != test.password {
				t.Fatalf("password input changed or rejected: revision = %d, password = %q, valid = %v, response = %s", revision, password, ok, response.Body.String())
			}
			if response.Body.Len() != 0 {
				t.Errorf("successful decoding wrote a response: %s", response.Body.String())
			}
		})
	}
}

func TestDecodeManagedUserPasswordRequiresExactFieldsAndNonemptyPassword(t *testing.T) {
	for _, test := range []struct {
		name, body, field string
	}{
		{name: "missing_revision", body: `{"Password":"new-password"}`, field: "Revision"},
		{name: "null_revision", body: `{"Revision":null,"Password":"new-password"}`, field: "Revision"},
		{name: "numeric_revision", body: `{"Revision":1,"Password":"new-password"}`, field: "Revision"},
		{name: "missing_password", body: `{"Revision":"1"}`, field: "Password"},
		{name: "null_password", body: `{"Revision":"1","Password":null}`, field: "Password"},
		{name: "numeric_password", body: `{"Revision":"1","Password":1234}`, field: "Password"},
		{name: "boolean_password", body: `{"Revision":"1","Password":false}`, field: "Password"},
		{name: "array_password", body: `{"Revision":"1","Password":[]}`, field: "Password"},
		{name: "object_password", body: `{"Revision":"1","Password":{}}`, field: "Password"},
		{name: "empty_password", body: `{"Revision":"1","Password":""}`, field: "Password"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if _, _, ok := decodeManagedUserPassword(response, managedUserRequestForTest(test.body, "application/json")); ok {
				t.Fatal("invalid password input accepted")
			}
			assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", test.field)
		})
	}
}

func managedUserDecodersForTest() []struct {
	name   string
	body   string
	decode func(http.ResponseWriter, *http.Request) bool
} {
	return []struct {
		name   string
		body   string
		decode func(http.ResponseWriter, *http.Request) bool
	}{
		{name: "update", body: managedUserUpdateJSON, decode: func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeManagedUserUpdate(w, r)
			return ok
		}},
		{name: "password", body: managedUserPasswordJSON, decode: func(w http.ResponseWriter, r *http.Request) bool {
			_, _, ok := decodeManagedUserPassword(w, r)
			return ok
		}},
	}
}

func TestManagedUserDecodersRejectInvalidUTF8BeforeJSONReplacement(t *testing.T) {
	for _, decoder := range managedUserDecodersForTest() {
		original := `"new-password"`
		if decoder.name == "update" {
			original = `"Managed User"`
		}
		for _, test := range []struct {
			name, invalid string
		}{
			{name: "invalid_leading_byte", invalid: "invalid-\xff-value"},
			{name: "isolated_continuation_byte", invalid: "invalid-\x80-value"},
			{name: "truncated_sequence", invalid: "invalid-\xe2\x82"},
		} {
			t.Run(decoder.name+"/"+test.name, func(t *testing.T) {
				body := strings.Replace(decoder.body, original, `"`+test.invalid+`"`, 1)
				response := httptest.NewRecorder()
				if decoder.decode(response, managedUserRequestForTest(body, "application/json")) {
					t.Fatal("invalid UTF-8 string was accepted or silently replaced")
				}
				assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "Body")
			})
		}
	}
}

func TestManagedUserDecodersPreserveValidUTF8Strings(t *testing.T) {
	name := "Caf\u00e9 User"
	updateBody := strings.Replace(managedUserUpdateJSON, `"Managed User"`, `"`+name+`"`, 1)
	updateResponse := httptest.NewRecorder()
	input, ok := decodeManagedUserUpdate(updateResponse, managedUserRequestForTest(updateBody, "application/json"))
	if !ok || input.Name != name {
		t.Fatalf("valid UTF-8 name changed or rejected: %q, valid = %v, response = %s", input.Name, ok, updateResponse.Body.String())
	}
	password := "new-\u00e9-password"
	passwordBody := strings.Replace(managedUserPasswordJSON, `"new-password"`, `"`+password+`"`, 1)
	passwordResponse := httptest.NewRecorder()
	revision, decoded, ok := decodeManagedUserPassword(passwordResponse, managedUserRequestForTest(passwordBody, "application/json"))
	if !ok || revision != 1 || decoded != password {
		t.Fatalf("valid UTF-8 password changed or rejected: revision = %d, valid = %v, response = %s", revision, ok, passwordResponse.Body.String())
	}
}

func TestManagedUserDecodersRejectNoncanonicalRevisions(t *testing.T) {
	for _, decoder := range managedUserDecodersForTest() {
		for _, revision := range []string{"", "0", "-1", "+1", "01", "1.0", "1e3", " 1", "1 ", "9223372036854775808", "18446744073709551615", "\u0661"} {
			t.Run(decoder.name+"/"+revision, func(t *testing.T) {
				body := managedUserJSONForTest(t, decoder.body, func(object map[string]any) { object["Revision"] = revision })
				response := httptest.NewRecorder()
				if decoder.decode(response, managedUserRequestForTest(body, "application/json")) {
					t.Fatalf("noncanonical revision %q accepted", revision)
				}
				assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "Revision")
			})
		}
	}
}

func TestManagedUserDecodersRejectUnknownCaseVariantAndDuplicateFields(t *testing.T) {
	for _, decoder := range managedUserDecodersForTest() {
		for _, test := range []struct {
			name, body string
		}{
			{name: "unknown_field", body: strings.Replace(decoder.body, `{`, `{"Unexpected":false,`, 1)},
			{name: "case_variant", body: strings.Replace(decoder.body, `"Revision"`, `"revision"`, 1)},
			{name: "case_variant_alongside_exact_field", body: strings.Replace(decoder.body, `{`, `{"revision":"1",`, 1)},
			{name: "duplicate_same_value", body: strings.Replace(decoder.body, `{`, `{"Revision":"1",`, 1)},
			{name: "duplicate_different_value", body: strings.Replace(decoder.body, `{`, `{"Revision":"2",`, 1)},
			{name: "duplicate_escaped_key", body: strings.Replace(decoder.body, `{`, `{"Re\u0076ision":"1",`, 1)},
		} {
			t.Run(decoder.name+"/"+test.name, func(t *testing.T) {
				response := httptest.NewRecorder()
				if decoder.decode(response, managedUserRequestForTest(test.body, "application/json")) {
					t.Fatal("unsupported or duplicate field accepted")
				}
				assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "")
			})
		}
	}
	for _, test := range []struct {
		name, before, after string
	}{
		{name: "unknown_policy_field", before: `"Policy":{`, after: `"Policy":{"EnableContentDeletion":true,`},
		{name: "case_variant_policy", before: `"Policy"`, after: `"policy"`},
		{name: "case_variant_flag", before: `"EnableAllFolders"`, after: `"enableAllFolders"`},
		{name: "case_variant_folders", before: `"EnabledFolders"`, after: `"enabledFolders"`},
		{name: "duplicate_policy_flag", before: `"Policy":{`, after: `"Policy":{"EnableAllFolders":false,`},
		{name: "duplicate_policy_folders", before: `"Policy":{`, after: `"Policy":{"EnabledFolders":[],`},
		{name: "duplicate_escaped_policy_key", before: `"Policy":{`, after: `"Policy":{"Enabled\u0046olders":[],`},
		{name: "duplicate_name", before: `{`, after: `{"Name":"Other Name",`},
	} {
		t.Run("update/"+test.name, func(t *testing.T) {
			body := strings.Replace(managedUserUpdateJSON, test.before, test.after, 1)
			response := httptest.NewRecorder()
			if _, ok := decodeManagedUserUpdate(response, managedUserRequestForTest(body, "application/json")); ok {
				t.Fatal("unsupported or duplicate update field accepted")
			}
			assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "")
		})
	}
}

func TestManagedUserDecodersRequireOneCompleteJSONObject(t *testing.T) {
	for _, decoder := range managedUserDecodersForTest() {
		for _, test := range []struct {
			name, body string
		}{
			{name: "empty", body: ""},
			{name: "whitespace", body: " \r\n\t "},
			{name: "null", body: "null"},
			{name: "array", body: "[]"},
			{name: "boolean", body: "true"},
			{name: "number", body: "1"},
			{name: "string", body: `"value"`},
			{name: "truncated", body: decoder.body[:len(decoder.body)-1]},
			{name: "second_object", body: decoder.body + `{}`},
			{name: "second_null", body: decoder.body + ` null`},
			{name: "trailing_garbage", body: decoder.body + ` invalid`},
		} {
			t.Run(decoder.name+"/"+test.name, func(t *testing.T) {
				response := httptest.NewRecorder()
				if decoder.decode(response, managedUserRequestForTest(test.body, "application/json")) {
					t.Fatal("invalid JSON document accepted")
				}
				assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "")
			})
		}
	}
}

func TestManagedUserDecodersEnforceMediaTypeAndBodyLimit(t *testing.T) {
	for _, decoder := range managedUserDecodersForTest() {
		for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", "application/json; charset"} {
			t.Run(decoder.name+"/media_type/"+contentType, func(t *testing.T) {
				response := httptest.NewRecorder()
				if decoder.decode(response, managedUserRequestForTest(decoder.body, contentType)) {
					t.Fatal("unsupported content type accepted")
				}
				assertManagedUserDecodeError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type", "")
			})
		}
		for _, test := range []struct {
			name  string
			bytes int
			valid bool
		}{
			{name: "exactly_one_mebibyte", bytes: 1 << 20, valid: true},
			{name: "one_extra_whitespace_byte", bytes: (1 << 20) + 1},
		} {
			t.Run(decoder.name+"/"+test.name, func(t *testing.T) {
				body := decoder.body + strings.Repeat(" ", test.bytes-len(decoder.body))
				request := managedUserRequestForTest(body, "application/json")
				request.ContentLength = -1
				response := httptest.NewRecorder()
				if ok := decoder.decode(response, request); ok != test.valid {
					t.Fatalf("body of %d bytes: valid = %v, want %v: %s", test.bytes, ok, test.valid, response.Body.String())
				}
				if !test.valid {
					assertManagedUserDecodeError(t, response, http.StatusBadRequest, "invalid_input", "")
				}
			})
		}
	}
}

func TestNativeManagedUserExposesOnlyEditableProjectionAndDecimalRevision(t *testing.T) {
	created := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		folders []string
	}{
		{name: "nil_folders_are_non_null_json_array"},
		{name: "explicit_empty_folders", folders: []string{}},
		{name: "configured_folders", folders: []string{"library-a", "library-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			user := identity.ManagedUser{
				User: identity.User{
					ID: "user-id", Name: "Managed User", IsAdministrator: true, IsDisabled: true,
					HasPassword: true, CreatedAt: created,
					Policy: json.RawMessage(`{"Secret":"private-policy-marker","EnableAllFolders":true,"EnabledFolders":["private-folder"],"EnableContentDeletion":true}`),
				},
				Revision: 9223372036854775807,
				Policy: identity.ManagedPolicy{
					EnableAllFolders: false, EnabledFolders: test.folders, EnableMediaPlayback: true,
					EnablePlaybackRemuxing: false, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: false,
				},
			}
			rawBefore := append([]byte(nil), user.User.Policy...)
			foldersBefore := append([]string(nil), user.Policy.EnabledFolders...)
			encoded, err := json.Marshal(nativeManagedUser(user))
			if err != nil {
				t.Fatalf("marshal managed user: %v", err)
			}
			var dto map[string]any
			if err := json.Unmarshal(encoded, &dto); err != nil {
				t.Fatalf("decode managed user DTO: %v", err)
			}
			folders := make([]any, len(test.folders))
			for index, folder := range test.folders {
				folders[index] = folder
			}
			want := map[string]any{
				"Id": "user-id", "Name": "Managed User", "IsAdministrator": true, "IsDisabled": true,
				"HasPassword": true, "CreatedAt": created.Format(time.RFC3339), "Revision": "9223372036854775807",
				"Policy": map[string]any{
					"EnableAllFolders": false, "EnabledFolders": folders, "EnableMediaPlayback": true,
					"EnablePlaybackRemuxing": false, "EnableAudioPlaybackTranscoding": true, "EnableVideoPlaybackTranscoding": false,
				},
			}
			if !reflect.DeepEqual(dto, want) {
				t.Errorf("managed user DTO = %#v, want %#v", dto, want)
			}
			if bytes.Contains(encoded, []byte("private-")) || bytes.Contains(encoded, []byte("EnableContentDeletion")) {
				t.Error("managed user DTO exposed raw stored policy")
			}
			if !bytes.Equal(user.User.Policy, rawBefore) || !reflect.DeepEqual(append([]string(nil), user.Policy.EnabledFolders...), foldersBefore) {
				t.Error("managed user DTO modified its input")
			}
		})
	}
}
