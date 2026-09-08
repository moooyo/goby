package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// These literal expectations come from the Emby 4.9.5.0 reference captures.
// Tests do not load captures at runtime, so changing a capture cannot hide drift.
const referenceEmbyAuthorizationAttributes = `Client="Goby Reference Recorder", Device="Linux Test", DeviceId="goby-reference-recorder", Version="0.1.0"`

func referenceEmbySeparateHeaders() http.Header {
	return http.Header{
		"X-Emby-Client":         {"Goby Reference Recorder"},
		"X-Emby-Client-Version": {"0.1.0"},
		"X-Emby-Device-Id":      {"goby-reference-recorder"},
		"X-Emby-Device-Name":    {"Linux Test"},
	}
}

func TestHTTPReferenceEmbyLoginClientTransports(t *testing.T) {
	consistent := referenceEmbySeparateHeaders()
	for header, values := range consistent {
		consistent[header] = []string{values[0], values[0]}
	}
	consistent.Set("Authorization", "Emby "+referenceEmbyAuthorizationAttributes)
	consistent.Set("X-Emby-Authorization", "MediaBrowser "+referenceEmbyAuthorizationAttributes)
	tests := []struct {
		name    string
		headers http.Header
	}{
		{name: "authorization_emby", headers: http.Header{"Authorization": {"Emby " + referenceEmbyAuthorizationAttributes}}},
		{name: "x_emby_authorization", headers: http.Header{"X-Emby-Authorization": {"Emby " + referenceEmbyAuthorizationAttributes}}},
		{name: "legacy_mediabrowser", headers: http.Header{"Authorization": {"MediaBrowser " + referenceEmbyAuthorizationAttributes}}},
		{name: "mixed_case_mediabrowser", headers: http.Header{"Authorization": {"mEdIaBrOwSeR " + referenceEmbyAuthorizationAttributes}}},
		{name: "mixed_case_emby", headers: http.Header{"Authorization": {"eMbY " + referenceEmbyAuthorizationAttributes}}},
		{name: "four_separate_headers", headers: referenceEmbySeparateHeaders()},
		{name: "matching_repeated_headers_and_attributes", headers: consistent},
		{
			name: "empty_separate_headers_do_not_override_attributes",
			headers: http.Header{
				"Authorization":         {"Emby " + referenceEmbyAuthorizationAttributes},
				"X-Emby-Client":         {""},
				"X-Emby-Client-Version": {""},
				"X-Emby-Device-Id":      {""},
				"X-Emby-Device-Name":    {""},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newServerFixture(t)
			adminID := f.bootstrap(t)
			response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
				"Username": "Administrator", "Pw": "administrator-password",
			}, test.headers)
			expectStatus(t, response, http.StatusOK)
			login := jsonObject(t, response)
			token := stringValue(t, login, "AccessToken")
			serverID := stringValue(t, login, "ServerId")
			user := objectValue(t, login, "User")
			if user["Id"] != adminID || user["Name"] != "Administrator" || user["ServerId"] != serverID {
				t.Fatalf("login returned the wrong user identity: %#v", user)
			}
			session := objectValue(t, login, "SessionInfo")
			if session["UserId"] != adminID || session["UserName"] != "Administrator" || session["ServerId"] != serverID ||
				session["Client"] != "Goby Reference Recorder" || session["DeviceId"] != "goby-reference-recorder" ||
				session["DeviceName"] != "Linux Test" || session["ApplicationVersion"] != "0.1.0" {
				t.Fatalf("login session does not preserve reference client metadata: %#v", session)
			}
			sessionID := stringValue(t, session, "Id")
			principal, err := f.users.Resolve(f.ctx, token, "emby")
			if err != nil {
				t.Fatalf("resolve issued reference client token: %v", err)
			}
			if principal.SessionID != sessionID || principal.User.ID != adminID || principal.Client.Name != "Goby Reference Recorder" ||
				principal.Client.DeviceID != "goby-reference-recorder" || principal.Client.Device != "Linux Test" || principal.Client.Version != "0.1.0" {
				t.Errorf("persisted session differs from the login response: %+v", principal)
			}
			protected := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, http.Header{"X-Emby-Token": {token}})
			expectStatus(t, protected, http.StatusOK)
			if jsonObject(t, protected)["Id"] != adminID {
				t.Error("issued token did not authorize the authenticated user's route")
			}
		})
	}
}

func TestHTTPReferenceEmbyRejectsConflictingClientMetadata(t *testing.T) {
	fields := []struct {
		name, header, value string
	}{
		{name: "client", header: "X-Emby-Client", value: "Goby Reference Recorder"},
		{name: "version", header: "X-Emby-Client-Version", value: "0.1.0"},
		{name: "device_id", header: "X-Emby-Device-Id", value: "goby-reference-recorder"},
		{name: "device_name", header: "X-Emby-Device-Name", value: "Linux Test"},
	}
	for _, field := range fields {
		for _, source := range []string{"authorization_attribute", "repeated_header"} {
			t.Run(field.name+"/"+source, func(t *testing.T) {
				f := newServerFixture(t)
				f.bootstrap(t)
				headers := referenceEmbySeparateHeaders()
				if source == "authorization_attribute" {
					headers.Set("Authorization", "Emby "+referenceEmbyAuthorizationAttributes)
					headers.Set(field.header, "Conflicting metadata")
				} else {
					headers[field.header] = []string{field.value, "Conflicting metadata"}
				}
				response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
					"Username": "Administrator", "Pw": "administrator-password",
				}, headers)
				expectAPIError(t, response, http.StatusBadRequest, "invalid_client", true)
				if _, exists := jsonObject(t, response)["AccessToken"]; exists {
					t.Error("conflicting client metadata produced an access token")
				}
				var sessions int
				if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM sessions").Scan(&sessions); err != nil {
					t.Fatalf("count sessions after rejected login: %v", err)
				}
				if sessions != 0 {
					t.Errorf("rejected login persisted %d sessions, want 0", sessions)
				}
			})
		}
	}
}

func TestHTTPReferenceEmbyLoginTextErrors(t *testing.T) {
	tests := []struct {
		name, username, password, message string
		headers                           http.Header
		status                            int
	}{
		{
			name: "missing_client_metadata", username: "Administrator", password: "administrator-password",
			status: http.StatusBadRequest, message: "Value cannot be null. (Parameter 'appName')",
		},
		{
			name: "device_without_client", username: "Administrator", password: "administrator-password",
			headers: http.Header{"X-Emby-Device-Id": {"goby-reference-recorder"}},
			status:  http.StatusBadRequest, message: "Value cannot be null. (Parameter 'appName')",
		},
		{
			name: "authorization_client_without_device", username: "Administrator", password: "administrator-password",
			headers: http.Header{"Authorization": {`Emby Client="Goby Reference Recorder", Device="Linux Test", Version="0.1.0"`}},
			status:  http.StatusBadRequest, message: "Value cannot be null. (Parameter 'reportedDeviceId')",
		},
		{
			name: "separate_client_without_device", username: "Administrator", password: "administrator-password",
			headers: http.Header{"X-Emby-Client": {"Goby Reference Recorder"}},
			status:  http.StatusBadRequest, message: "Value cannot be null. (Parameter 'reportedDeviceId')",
		},
		{
			name: "wrong_password", username: "Administrator", password: "wrong-password",
			headers: referenceEmbySeparateHeaders(),
			status:  http.StatusUnauthorized, message: "Invalid username or password. Please try again.",
		},
		{
			name: "unknown_user", username: "missing-reference-user", password: "administrator-password",
			headers: referenceEmbySeparateHeaders(),
			status:  http.StatusUnauthorized, message: "Invalid username or password. Please try again.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newServerFixture(t)
			f.bootstrap(t)
			response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
				"Username": test.username, "Pw": test.password,
			}, test.headers)
			expectEmbyTextError(t, response, test.status, test.message)
			var sessions int
			if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM sessions").Scan(&sessions); err != nil {
				t.Fatalf("count sessions after failed authentication: %v", err)
			}
			if sessions != 0 {
				t.Errorf("failed authentication persisted %d sessions, want 0", sessions)
			}
		})
	}
}

func TestHTTPReferenceEmbyProtectedTokenTextErrors(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	for _, test := range []struct {
		name    string
		headers http.Header
	}{
		{name: "missing_token"},
		{name: "malformed_token", headers: http.Header{"X-Emby-Token": {"not-a-token"}}},
		{name: "unknown_well_formed_token", headers: http.Header{"X-Emby-Token": {strings.Repeat("A", 43)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, test.headers)
			expectEmbyTextError(t, response, http.StatusUnauthorized, "Access token is invalid or expired.")
		})
	}
	login := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, login, "AccessToken")
	for _, scheme := range []string{"MediaBrowser", "mEdIaBrOwSeR"} {
		response := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, http.Header{
			"Authorization": {scheme + ` Token="` + token + `"`},
		})
		expectStatus(t, response, http.StatusOK)
	}
	logout := f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, http.Header{"X-Emby-Token": {token}})
	expectStatus(t, logout, http.StatusOK)
	revoked := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, http.Header{"X-Emby-Token": {token}})
	expectEmbyTextError(t, revoked, http.StatusUnauthorized, "Access token is invalid or expired.")
}

func TestHTTPReferenceEmbyIdentityHeadersCannotGrantAdministratorAccess(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Reference Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create non-administrator reference user: %v", err)
	}
	claims := referenceEmbySeparateHeaders()
	claims.Set("UserId", adminID)
	claims.Set("IsAdministrator", "true")
	claims.Set("Authorization", `MediaBrowser UserId="`+adminID+`", IsAdministrator="true"`)
	metadataOnly := f.request(t, http.MethodGet, "/emby/Users/Query", nil, claims)
	expectEmbyTextError(t, metadataOnly, http.StatusUnauthorized, "Access token is invalid or expired.")
	response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
		"Username": viewer.Name, "Pw": "viewer-password",
	}, claims)
	expectStatus(t, response, http.StatusOK)
	login := jsonObject(t, response)
	user := objectValue(t, login, "User")
	if user["Id"] != viewer.ID || objectValue(t, user, "Policy")["IsAdministrator"] != false {
		t.Fatalf("untrusted identity claims changed the authenticated user: %#v", user)
	}
	if objectValue(t, login, "SessionInfo")["UserId"] != viewer.ID {
		t.Error("untrusted UserId claim changed the session user")
	}
	claims.Set("X-Emby-Token", stringValue(t, login, "AccessToken"))
	ownUser := f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, claims)
	expectStatus(t, ownUser, http.StatusOK)
	if jsonObject(t, ownUser)["Id"] != viewer.ID {
		t.Error("identity claims changed the protected route's authenticated user")
	}
	otherUser := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, claims)
	expectAPIError(t, otherUser, http.StatusForbidden, "access_denied", true)
	administratorRoute := f.request(t, http.MethodGet, "/emby/Users/Query", nil, claims)
	expectAPIError(t, administratorRoute, http.StatusForbidden, "administrator_required", true)
}

func TestHTTPReferenceMediaBrowserTokenAliasConsistency(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, login, "AccessToken")
	for _, test := range []struct {
		name    string
		headers http.Header
		query   string
	}{
		{
			name:    "legacy_token_header_only",
			headers: http.Header{"X-MediaBrowser-Token": {token}},
		},
		{
			name: "matching_legacy_and_emby_token_headers",
			headers: http.Header{
				"X-MediaBrowser-Token": {token},
				"X-Emby-Token":         {token},
			},
		},
		{
			name: "matching_legacy_token_all_sources",
			headers: http.Header{
				"X-MediaBrowser-Token": {token, token},
				"X-Emby-Token":         {token},
				"Authorization":        {`MediaBrowser Token="` + token + `"`},
			},
			query: "?api_key=" + url.QueryEscape(token),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, "/emby/Users/"+adminID+test.query, nil, test.headers)
			expectStatus(t, response, http.StatusOK)
			if jsonObject(t, response)["Id"] != adminID {
				t.Error("legacy token header resolved a different user")
			}
		})
	}
	for _, test := range []struct {
		name    string
		headers http.Header
		query   string
	}{
		{
			name: "legacy_and_emby_header_conflict",
			headers: http.Header{
				"X-MediaBrowser-Token": {token},
				"X-Emby-Token":         {"conflicting-token"},
			},
		},
		{
			name: "legacy_and_authorization_attribute_conflict",
			headers: http.Header{
				"X-MediaBrowser-Token": {"conflicting-token"},
				"Authorization":        {`Emby Token="` + token + `"`},
			},
		},
		{
			name:    "repeated_legacy_header_conflict",
			headers: http.Header{"X-MediaBrowser-Token": {token, "conflicting-token"}},
		},
		{
			name:    "legacy_header_and_query_conflict",
			headers: http.Header{"X-MediaBrowser-Token": {token}},
			query:   "?api_key=conflicting-token",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, "/emby/Users/"+adminID+test.query, nil, test.headers)
			expectEmbyTextError(t, response, http.StatusUnauthorized, "Access token is invalid or expired.")
		})
	}
	logout := f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, http.Header{"X-MediaBrowser-Token": {token}})
	expectStatus(t, logout, http.StatusOK)
	revoked := f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, http.Header{"X-MediaBrowser-Token": {token}})
	expectEmbyTextError(t, revoked, http.StatusUnauthorized, "Access token is invalid or expired.")
}
