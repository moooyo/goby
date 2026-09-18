//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPInstalledFeaturesRequireAdministratorAndMatchPolicyRegistry(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	cookie := accounts.cookie
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/features", nil, nil), http.StatusUnauthorized)
	response := f.request(t, http.MethodGet, "/admin/v1/features", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	var result struct{ Items []identity.FeatureInfo }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != len(identity.UserFeatures()) {
		t.Fatal("native discovery differs from installed policy features")
	}
	for _, feature := range result.Items {
		if !identity.IsUserFeature(feature.ID) || feature.FeatureType != "User" {
			t.Fatal("discovery advertised an uninstalled feature")
		}
	}
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "feature-admin")
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Features", nil, accounts.viewer.headers), http.StatusForbidden)
	emby := f.request(t, http.MethodGet, "/features?FeatureType=User", nil, admin.headers)
	expectStatus(t, emby, http.StatusOK)
	var features []identity.FeatureInfo
	if err := json.Unmarshal(emby.Body.Bytes(), &features); err != nil || len(features) != len(result.Items) {
		t.Fatalf("compatibility discovery shape: %v", err)
	}
	empty := f.request(t, http.MethodGet, "/emby/Features?FeatureType=System", nil, admin.headers)
	expectStatus(t, empty, http.StatusOK)
	if err := json.Unmarshal(empty.Body.Bytes(), &features); err != nil || len(features) != 0 {
		t.Fatal("uninstalled system features were advertised")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Features?FeatureType=unknown", nil, admin.headers), http.StatusBadRequest)
}
