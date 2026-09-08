package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func embyCredentialRequest(headers map[string][]string, query string) *http.Request {
	target := "http://goby.test/Users"
	if query != "" {
		target += "?" + query
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	return request
}

func TestParseEmbyCredentialsAcceptsConsistentSources(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string][]string
		query      string
		wantToken  string
		wantClient identity.Client
	}{
		{name: "no_credentials"},
		{
			name: "quoted_commas",
			headers: map[string][]string{
				"Authorization": {`Emby Client="Player, beta", DeviceId="device-1", Device="Living room, Linux", Version="1.2", Token="token-a"`},
			},
			wantToken:  "token-a",
			wantClient: identity.Client{Name: "Player, beta", DeviceID: "device-1", Device: "Living room, Linux", Version: "1.2"},
		},
		{
			name: "case_insensitive_scheme_and_attributes",
			headers: map[string][]string{
				"X-Emby-Authorization": {`eMbY cLiEnT="Player", dEvIcEiD="device-2", dEvIcE="Linux", vErSiOn="2.0", tOkEn="token-a"`},
			},
			wantToken:  "token-a",
			wantClient: identity.Client{Name: "Player", DeviceID: "device-2", Device: "Linux", Version: "2.0"},
		},
		{
			name: "consistent_attributes_across_authorization_headers",
			headers: map[string][]string{
				"Authorization":        {`Emby Client="Player", DeviceId="device-1", Token="token-a"`},
				"X-Emby-Authorization": {`Emby Client="Player", DeviceId="device-1", Device="Linux", Version="1.0", Token="token-a"`},
			},
			wantToken:  "token-a",
			wantClient: identity.Client{Name: "Player", DeviceID: "device-1", Device: "Linux", Version: "1.0"},
		},
		{
			name: "same_value_repeated_attributes",
			headers: map[string][]string{
				"Authorization": {`Emby Client="Player", cLiEnT="Player", Token="token-a", Token="token-a"`},
			},
			wantToken:  "token-a",
			wantClient: identity.Client{Name: "Player"},
		},
		{
			name: "matching_token_in_all_sources",
			headers: map[string][]string{
				"Authorization":        {`Emby Token="token-a"`},
				"X-Emby-Authorization": {`Emby Token="token-a"`},
				"X-Emby-Token":         {"token-a", "token-a"},
			},
			query:     "api_key=token-a&api_key=token-a",
			wantToken: "token-a",
		},
		{
			name:      "token_header_only",
			headers:   map[string][]string{"X-Emby-Token": {"token-a"}},
			wantToken: "token-a",
		},
		{
			name:      "query_token_only",
			query:     "api_key=token-a",
			wantToken: "token-a",
		},
		{
			name:      "matching_repeated_query_tokens",
			query:     "api_key=token-a&api_key=token-a",
			wantToken: "token-a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, client, err := parseEmbyCredentials(embyCredentialRequest(test.headers, test.query))
			if err != nil {
				t.Fatalf("parse consistent credentials: %v", err)
			}
			if token != test.wantToken {
				t.Errorf("token = %q, want %q", token, test.wantToken)
			}
			if client != test.wantClient {
				t.Errorf("client = %+v, want %+v", client, test.wantClient)
			}
		})
	}
}

func TestParseEmbyCredentialsRejectsConflicts(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
		query   string
	}{
		{
			name: "conflicting_repeated_client_attribute",
			headers: map[string][]string{
				"Authorization": {`Emby Client="First", Client="Second", Token="token-a"`},
			},
		},
		{
			name: "conflicting_repeated_token_attribute",
			headers: map[string][]string{
				"Authorization": {`Emby Token="token-a", tOkEn="token-b"`},
			},
		},
		{
			name: "client_attributes_across_authorization_headers",
			headers: map[string][]string{
				"Authorization":        {`Emby Client="First", Token="token-a"`},
				"X-Emby-Authorization": {`Emby Client="Second", Token="token-a"`},
			},
		},
		{
			name: "tokens_across_authorization_headers",
			headers: map[string][]string{
				"Authorization":        {`Emby Token="token-a"`},
				"X-Emby-Authorization": {`Emby Token="token-b"`},
			},
		},
		{
			name: "repeated_authorization_header_values",
			headers: map[string][]string{
				"Authorization": {`Emby Token="token-a"`, `Emby Token="token-b"`},
			},
		},
		{
			name: "authorization_and_token_header",
			headers: map[string][]string{
				"Authorization": {`Emby Token="token-a"`},
				"X-Emby-Token":  {"token-b"},
			},
		},
		{
			name:    "authorization_and_query",
			headers: map[string][]string{"Authorization": {`Emby Token="token-a"`}},
			query:   "api_key=token-b",
		},
		{
			name:    "token_header_and_query",
			headers: map[string][]string{"X-Emby-Token": {"token-a"}},
			query:   "api_key=token-b",
		},
		{
			name:    "conflicting_repeated_token_headers",
			headers: map[string][]string{"X-Emby-Token": {"token-a", "token-b"}},
		},
		{
			name:  "conflicting_repeated_query_tokens",
			query: "api_key=token-a&api_key=token-b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, _, err := parseEmbyCredentials(embyCredentialRequest(test.headers, test.query))
			if err == nil {
				t.Fatal("conflicting credentials must be rejected")
			}
			if token != "" {
				t.Error("rejected credentials must not return a usable token")
			}
		})
	}
}

func TestParseEmbyCredentialsRejectsMalformedAuthorization(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "bearer_scheme", value: "Bearer token-a"},
		{name: "basic_scheme", value: "Basic credentials"},
		{name: "missing_scheme", value: `Token="token-a"`},
		{name: "missing_scheme_separator", value: `EmbyToken="token-a"`},
		{name: "scheme_only", value: "Emby"},
		{name: "attribute_without_equals", value: "Emby Token"},
		{name: "unterminated_quote", value: `Emby Token="token-a`},
		{name: "missing_comma_after_quote", value: `Emby Token="token-a" Client="Player"`},
		{name: "semicolon_after_quote", value: `Emby Token="token-a"; Client="Player"`},
		{name: "junk_after_quote", value: `Emby Token="token-a"junk`},
	}
	for _, test := range tests {
		for _, header := range []string{"Authorization", "X-Emby-Authorization"} {
			t.Run(test.name+"/"+header, func(t *testing.T) {
				request := embyCredentialRequest(map[string][]string{header: {test.value}}, "")
				token, _, err := parseEmbyCredentials(request)
				if err == nil {
					t.Fatal("malformed authorization must be rejected")
				}
				if token != "" {
					t.Error("malformed authorization must not return a usable token")
				}
			})
		}
	}
}

func TestParseEmbyCredentialsDoesNotAuthenticateIdentityClaims(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string][]string
		query      string
		wantToken  string
		wantClient identity.Client
	}{
		{
			name: "client_metadata_is_not_a_token",
			headers: map[string][]string{
				"Authorization": {`Emby Client="token-a", DeviceId="token-b", Device="Linux", Version="1.0"`},
			},
			wantClient: identity.Client{Name: "token-a", DeviceID: "token-b", Device: "Linux", Version: "1.0"},
		},
		{
			name: "user_and_role_attributes_are_not_credentials",
			headers: map[string][]string{
				"Authorization": {`Emby Client="Player", DeviceId="device-1", UserId="administrator-id", IsAdministrator="true"`},
			},
			wantClient: identity.Client{Name: "Player", DeviceID: "device-1"},
		},
		{
			name: "claims_do_not_override_an_explicit_token",
			headers: map[string][]string{
				"Authorization": {`Emby UserId="administrator-id", IsAdministrator="true", Token="token-a"`},
			},
			wantToken: "token-a",
		},
		{
			name: "identity_headers_are_not_credentials",
			headers: map[string][]string{
				"UserId":          {"administrator-id"},
				"IsAdministrator": {"true"},
				"X-Emby-UserId":   {"administrator-id"},
				"Token":           {"token-a"},
			},
		},
		{
			name:  "identity_query_parameters_are_not_credentials",
			query: "UserId=administrator-id&IsAdministrator=true&Client=token-a&DeviceId=token-b&Token=token-c",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, client, err := parseEmbyCredentials(embyCredentialRequest(test.headers, test.query))
			if err != nil {
				t.Fatalf("parse client metadata: %v", err)
			}
			if token != test.wantToken {
				t.Errorf("identity claims changed the authentication token: got %q, want %q", token, test.wantToken)
			}
			if client != test.wantClient {
				t.Errorf("client metadata = %+v, want %+v", client, test.wantClient)
			}
		})
	}
}

func TestCSRFTokenIsStableAndSessionSpecific(t *testing.T) {
	first := csrfToken("session-a")
	if first == "" || first == "session-a" {
		t.Fatal("CSRF token must be nonempty and distinct from the session credential")
	}
	if again := csrfToken("session-a"); again != first {
		t.Error("the same session must produce a stable CSRF token")
	}
	if other := csrfToken("session-b"); other == first {
		t.Error("different sessions must not share a CSRF token")
	}
}
