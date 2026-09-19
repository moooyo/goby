package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFeatureLicensingDescribesOnlyServerLocalLicenseState(t *testing.T) {
	for _, feature := range []string{"playback", "themes", "intro", "dvr", "unimplemented-feature"} {
		t.Run(feature, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/emby/Registrations/"+feature, nil)
			request.SetPathValue("Feature", feature)
			response := httptest.NewRecorder()
			(&Server{}).embyFeatureRegistration(response, request)
			var info map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
				len(info) != 4 || info["Name"] != feature || info["IsRegistered"] != true || info["IsTrial"] != false {
				t.Fatalf("unexpected server-local license response: %s", response.Body.String())
			}
			expiration, err := time.Parse(time.RFC3339, info["ExpirationDate"].(string))
			if err != nil || expiration.Year() != 9999 {
				t.Fatal("a free feature must not have a trial expiration")
			}
		})
	}
}

func TestFeatureLicensingAliasesRetainAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerFeatureLicensingRoutes(mux)
	for _, path := range []string{"/emby/Registrations/AbC-Feature", "/registrations/AbC-Feature", "/EMBY/REGISTRATIONS/AbC-Feature"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		translated := compatibilityNamespace(request)
		if translated.URL.Path != "/emby/Registrations/AbC-Feature" {
			t.Fatalf("registration alias changed the feature identifier: %s", translated.URL.Path)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, translated)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("registration alias bypassed authentication: %s, %d", path, response.Code)
		}
	}
}
