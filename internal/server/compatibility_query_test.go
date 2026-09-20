package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func compatibilityQueryCarrierCases() []string {
	return []string{
		"api_key=token", "X-Emby-Token=token", "API_KEY=token&x-emby-token=token",
		"api_key=token&api_key=token", "X-Emby-Token=token&x-emby-token=token",
		"X-Emby-Client=Browser&X-Emby-Client-Version=1.0&X-Emby-Device-Id=device&X-Emby-Device-Name=Linux",
		"X-Emby-Client=Browser&x-emby-client=Browser&X-Emby-Token=token",
		"X-Emby-Language=en-US", "x-emby-language=zh-CN&api_key=token",
	}
}

func TestCompatibilityQuerySeparatesOnlyDeclaredAuthenticationCarriers(t *testing.T) {
	for _, query := range compatibilityQueryCarrierCases() {
		t.Run(query, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/emby/System/Configuration?"+query, nil)
			original := request.URL.RawQuery
			w := httptest.NewRecorder()
			if !configurationQuery(w, request) {
				t.Fatal("configuration rejected a declared authentication carrier")
			}
			if _, err := parseEmbyDeviceQuery(request, true); err != nil {
				t.Fatalf("devices rejected a declared authentication carrier: %v", err)
			}
			if _, err := parseApplicationKeyQuery(request, false); err != nil {
				t.Fatalf("key listing rejected a declared authentication carrier: %v", err)
			}
			if _, err := parseEmbyTaskQuery(request, true); err != nil {
				t.Fatalf("task listing rejected a declared authentication carrier: %v", err)
			}
			if _, err := embyObservabilityQuery(request); err != nil {
				t.Fatalf("observability rejected a declared authentication carrier: %v", err)
			}
			if request.URL.RawQuery != original {
				t.Fatal("transport parsing modified the original request query")
			}
		})
	}
	for _, query := range []string{
		"X-Emby-Token=first&api_key=second", "api_key=first&API_KEY=second",
		"X-Emby-Client=first&x-emby-client=second", "api_key=token&broken=%gg",
		"X-Emby-Device-Id=%ff", "X-Emby-Device-Name=%00",
		"X-Emby-Client=" + url.QueryEscape(strings.Repeat("x", 257)),
		"X-Emby-Unknown=ignored", "X-Emby-UserId=administrator", "UserId=administrator",
		"X-Emby-Language=en-US&x-emby-language=en-US", "X-Emby-Language=%ff",
		"X-Emby-Language=%00", "X-Emby-Language=%0a", "X-Emby-Language=%20en-US",
		"X-Emby-Language=" + strings.Repeat("x", 257), "ReqFormat=json",
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/System/Configuration?"+query, nil)
		if configurationQuery(httptest.NewRecorder(), request) {
			t.Fatal("configuration accepted conflicting, malformed or undeclared query input")
		}
		if _, err := parseEmbyDeviceQuery(request, true); err == nil {
			t.Fatal("devices accepted conflicting, malformed or undeclared query input")
		}
		if _, err := parseApplicationKeyQuery(request, false); err == nil {
			t.Fatal("key listing accepted conflicting, malformed or undeclared query input")
		}
		if _, err := parseEmbyTaskQuery(request, true); err == nil {
			t.Fatal("task listing accepted conflicting, malformed or undeclared query input")
		}
		if _, err := embyObservabilityQuery(request); err == nil {
			t.Fatal("observability accepted conflicting, malformed or undeclared query input")
		}
	}
}

func TestCompatibilityQueryPreservesBusinessRulesAndHeaderConflicts(t *testing.T) {
	base := "X-Emby-Token=token&X-Emby-Client=Browser"
	request := func(query string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/emby/Devices?"+base+"&"+query, nil)
	}
	if id, err := parseEmbyDeviceQuery(request("Id=12"), false); err != nil || id != "12" {
		t.Fatalf("device lookup lost its business identifier: %v", err)
	}
	if _, err := parseEmbyDeviceQuery(request("Id=12&id=12"), false); err == nil {
		t.Fatal("device lookup accepted duplicate business aliases")
	}
	if filter, err := parseApplicationKeyQuery(request("StartIndex=2&Limit=3"), false); err != nil || filter.StartIndex != 2 || filter.Limit != 3 {
		t.Fatalf("key pagination changed while removing transport metadata: %v", err)
	}
	if _, err := parseApplicationKeyQuery(request("Limit=2&limit=2"), false); err == nil {
		t.Fatal("key pagination accepted duplicate business aliases")
	}
	if name, err := embyApplicationKeyName(request("App=Review+Key")); err != nil || name != "Review Key" {
		t.Fatalf("key creation lost its declared app name: %v", err)
	}
	for _, query := range []string{"", "App=one&app=one", "App=one&UserId=administrator"} {
		if _, err := embyApplicationKeyName(request(query)); err == nil {
			t.Fatal("key creation accepted missing, duplicate or undeclared business fields")
		}
	}
	if options, err := parseEmbyTaskQuery(request("IsHidden=false&IsEnabled=true"), true); err != nil || options.IsHidden == nil || *options.IsHidden || options.IsEnabled == nil || !*options.IsEnabled {
		t.Fatalf("task filters changed while removing transport metadata: %v", err)
	}
	if values, err := embyObservabilityQuery(request("Limit=3"), "Limit"); err != nil || values.Get("Limit") != "3" || len(values) != 1 {
		t.Fatalf("observability retained transport fields or lost business input: %v", err)
	}
	if _, err := embyObservabilityQuery(request("limit=3"), "Limit"); err == nil {
		t.Fatal("observability changed its case-sensitive business allowlist")
	}
	for _, field := range []string{"X-Emby-Token", "X-Emby-Client"} {
		r := request("")
		r.Header.Set(field, "conflicting-header")
		if _, err := embyBusinessQuery(r); err == nil {
			t.Fatal("transport filtering discarded a conflict with an authorization header")
		}
	}
	for _, query := range compatibilityQueryCarrierCases() {
		r := httptest.NewRequest(http.MethodGet, "/admin/v1/api-keys?"+query, nil)
		if _, err := parseApplicationKeyQuery(r, true); err == nil {
			t.Fatal("native key listing inherited compatibility transport exceptions")
		}
		if _, err := parseAdminDeviceQuery(r); err == nil {
			t.Fatal("native device listing inherited compatibility transport exceptions")
		}
	}
}
