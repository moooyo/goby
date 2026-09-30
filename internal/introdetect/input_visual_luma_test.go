package introdetect

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestVisualLumaFeatureBudgetIncludesDescriptorAndAlignment(t *testing.T) {
	episode := testEpisode(1, 10*TicksPerSecond)
	cohort := Cohort{Key: "feature-budget", Episodes: []Episode{episode}}
	options := DefaultOptions()
	audioBytes := int64(reflect.TypeOf(AudioSample{}).Size())
	visualBytes := int64(reflect.TypeOf(VisualSample{}).Size())
	if audioBytes != 24 || visualBytes != 88 {
		t.Fatalf("feature accounting must track the in-memory layout: audio=%d visual=%d", audioBytes, visualBytes)
	}
	identityBytes := len(episode.EpisodeKey) + len(episode.SourceKey) + len(episode.ContentIdentity) + len(episode.AlgorithmProfile)
	options.MaxFeatureBytes = len(episode.Audio)*int(audioBytes) + len(episode.Visual)*int(visualBytes) + identityBytes
	if _, err := validateInput(context.Background(), cohort, options); err != nil {
		t.Fatalf("exact feature budget was rejected: %v", err)
	}
	options.MaxFeatureBytes--
	if _, err := validateInput(context.Background(), cohort, options); !errors.Is(err, ErrLimit) {
		t.Fatalf("descriptor bytes escaped the feature budget: %v", err)
	}
}
