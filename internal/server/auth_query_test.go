package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func embyQueryClientValues() url.Values {
	return url.Values{
		"X-Emby-Client": {"Emby Web"}, "X-Emby-Client-Version": {"4.9.5.0"},
		"X-Emby-Device-Id": {"query-browser-device"}, "X-Emby-Device-Name": {"Chrome Linux"},
		"X-Emby-Language": {"en-US"},
	}
}

func TestParseEmbyQueryCredentialsPreservesExplicitCarriers(t *testing.T) {
	query := embyQueryClientValues()
	query.Set("X-Emby-Token", "query-token")
	query.Set("api_key", "query-token")
	query.Set("UserId", "untrusted-administrator")
	query.Set("IsAdministrator", "true")
	query.Set("Username", "not-a-login-body")
	query.Set("Pw", "not-a-login-body-password")
	query.Set("Authorization", "Bearer not-an-allowed-query-header")
	want := identity.Client{Name: "Emby Web", Version: "4.9.5.0", DeviceID: "query-browser-device", Device: "Chrome Linux"}
	for _, headers := range []map[string][]string{
		nil,
		{"X-Emby-Client": {"Emby Web"}, "X-Emby-Token": {"query-token"}},
		{"Authorization": {`MediaBrowser Client="Emby Web", DeviceId="query-browser-device", Token="query-token"`}},
	} {
		token, client, err := parseEmbyCredentials(embyCredentialRequest(headers, query.Encode()))
		if err != nil || token != "query-token" || client != want {
			t.Fatal("explicit query carriers lost their token or client metadata")
		}
	}
	query.Add("x-emby-client", "Emby Web")
	query.Add("x-EMBY-token", "query-token")
	query.Add("API_KEY", "query-token")
	token, client, err := parseEmbyCredentials(embyCredentialRequest(nil, query.Encode()))
	if err != nil || token != "query-token" || client != want {
		t.Fatal("consistent repeated query carriers did not preserve the existing matching-value policy")
	}
	claims := "Username=administrator&Pw=password&UserId=administrator&IsAdministrator=true&Authorization=Bearer+token&Token=token"
	token, client, err = parseEmbyCredentials(embyCredentialRequest(nil, claims))
	if err != nil || token != "" || client != (identity.Client{}) {
		t.Fatal("arbitrary query fields established credentials or client headers")
	}
}

func TestParseEmbyQueryCredentialsRejectsAllConflictingCarrierValues(t *testing.T) {
	for _, field := range []struct{ name, attribute string }{
		{"X-Emby-Client", "Client"}, {"X-Emby-Client-Version", "Version"},
		{"X-Emby-Device-Id", "DeviceId"}, {"X-Emby-Device-Name", "Device"},
	} {
		for _, test := range []struct {
			headers map[string][]string
			query   string
		}{
			{map[string][]string{field.name: {"header-value"}}, field.name + "=query-value"},
			{map[string][]string{"Authorization": {`Emby ` + field.attribute + `="header-value"`}}, field.name + "=query-value"},
			{nil, field.name + "=first&" + field.name + "=second"},
			{nil, field.name + "=first&" + strings.ToLower(field.name) + "=second"},
		} {
			token, client, err := parseEmbyCredentials(embyCredentialRequest(test.headers, test.query))
			if err == nil || token != "" || client != (identity.Client{}) {
				t.Fatal("conflicting query client metadata returned usable credentials")
			}
		}
	}
	for _, test := range []struct {
		headers map[string][]string
		query   string
	}{
		{nil, "X-Emby-Token=first&X-Emby-Token=second"},
		{nil, "X-Emby-Token=first&x-emby-token=second"},
		{nil, "X-Emby-Token=first&%58-Emby-Token=second"},
		{nil, "X-Emby-Token=first&api_key=second"},
		{nil, "api_key=first&API_KEY=second"},
		{map[string][]string{"X-Emby-Token": {"first"}}, "X-Emby-Token=second"},
		{map[string][]string{"X-MediaBrowser-Token": {"first"}}, "x-emby-token=second"},
		{map[string][]string{"Authorization": {`Emby Token="first"`}}, "X-Emby-Token=second"},
	} {
		token, client, err := parseEmbyCredentials(embyCredentialRequest(test.headers, test.query))
		if err == nil || token != "" || client != (identity.Client{}) {
			t.Fatal("conflicting query token carriers returned a usable principal")
		}
	}
}

func TestParseEmbyQueryCredentialsRejectsMalformedQueryWithoutPartialAuthentication(t *testing.T) {
	for _, query := range []string{
		"X-Emby-Token=private-query-marker&X-Emby-Token=%GG",
		"api_key=private-query-marker&unrelated=%",
		"X-Emby-Client=%FF&X-Emby-Token=private-query-marker",
		"X-Emby-Token=private-query-marker%00",
		"X-Emby-Device-Name=Chrome;X-Emby-Token=private-query-marker",
	} {
		token, client, err := parseEmbyCredentials(embyCredentialRequest(map[string][]string{"X-Emby-Token": {"private-query-marker"}}, query))
		if err == nil || token != "" || client != (identity.Client{}) {
			t.Fatal("malformed query authenticated from a partially parsed token set")
		}
		if strings.Contains(err.Error(), "private-query-marker") || strings.Contains(err.Error(), query) {
			t.Fatal("authorization parsing error retained credential-bearing query content")
		}
	}
}

func TestEmbyQueryCredentialDiagnosticsNeverRecordQueryTokens(t *testing.T) {
	for _, query := range []string{"X-Emby-Token=private-query-marker", "x-emby-token=private-query-marker&api_key=other-private-marker",
		"X-Emby-Token=private-query-marker&X-Emby-Token=%GG"} {
		server, output := requestLoggingServer()
		mux := http.NewServeMux()
		mux.HandleFunc("GET /emby/Users/{Id}", func(w http.ResponseWriter, r *http.Request) {
			if _, _, err := parseEmbyCredentials(r); err != nil {
				embyTextError(w, r, http.StatusUnauthorized, embyInvalidTokenMessage)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		response := httptest.NewRecorder()
		server.middleware(mux).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/emby/Users/query-user?"+query, nil))
		requestLoggingCompletion(t, output)
		for _, secret := range []string{"private-query-marker", "other-private-marker", query} {
			if strings.Contains(output.String(), secret) {
				t.Fatal("request diagnostics exposed the newly supported query token carrier")
			}
		}
	}
}
