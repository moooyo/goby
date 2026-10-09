package server

import "github.com/moooyo/goby/internal/timeshift"

// dynamicSnapshotArtifact models committed-snapshot lookup for package tests.
// Production artifact reads use the timeshift store's ResolveArtifact and
// OpenArtifact methods.
func dynamicSnapshotArtifact(snapshot timeshift.WindowSnapshot, name string) (timeshift.Artifact, string, bool) {
	for _, variant := range snapshot.Variants {
		for _, segment := range snapshot.Segments {
			for _, artifact := range segment.Artifacts {
				if artifact.VariantID == variant.ID && name == dynamicArtifactName(artifact.ID, variant.Format, false) {
					return artifact, variant.Kind, true
				}
			}
		}
		for _, epoch := range snapshot.Epochs {
			for _, artifact := range epoch.Initializations {
				if artifact.VariantID == variant.ID && name == dynamicArtifactName(artifact.ID, variant.Format, true) {
					return artifact, variant.Kind, true
				}
			}
		}
	}
	return timeshift.Artifact{}, "", false
}
