package identity

import "testing"

func TestInstalledFeatureRestrictionsAreDenyOnlyAndDiscoverable(t *testing.T) {
	policy := DefaultManagedPolicy()
	seen := make(map[string]bool)
	for _, feature := range UserFeatures() {
		if feature.Name == "" || feature.FeatureType != "User" || seen[feature.ID] || !policy.AllowsFeature(feature.ID) {
			t.Fatalf("invalid installed feature descriptor: %+v", feature)
		}
		seen[feature.ID] = true
	}
	policy.RestrictedFeatures = []string{FeaturePlaylists, "retained_uninstalled_feature"}
	if policy.AllowsFeature(FeaturePlaylists) || !policy.AllowsFeature(FeatureCollections) || policy.AllowsFeature("unknown") {
		t.Fatal("feature restrictions changed an unrelated or unknown grant")
	}
	copy := UserFeatures()
	copy[0].ID = "changed"
	if UserFeatures()[0].ID == "changed" {
		t.Fatal("discovery response mutated the authorization registry")
	}
}
