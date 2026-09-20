package media

import (
	"errors"
	"testing"
)

// A writer-derived duration estimate never exempts the original stream tag
// from the final metadata proof. This regression is independent of repair.
func TestSubtitleRemovalMetadataRequiresExactStoredDuration(t *testing.T) {
	source := mediaEditTestDocument(t)
	source.Streams[0]["tags"].(map[string]any)["DURATION"] = "00:00:12.000000000"
	if _, _, err := compareMediaEditDocuments(source, mediaEditTestCandidate(t, source), "mkv", 7); err != nil {
		t.Fatalf("unchanged duration tag was rejected: %v", err)
	}
	for _, test := range []struct {
		name    string
		value   string
		missing bool
	}{
		{name: "observed 24 FPS millisecond loss", value: "00:00:11.999000000"},
		{name: "one nanosecond change", value: "00:00:12.000000001"},
		{name: "missing original tag", missing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := mediaEditTestCandidate(t, source)
			tags := candidate.Streams[0]["tags"].(map[string]any)
			if test.missing {
				delete(tags, "DURATION")
			} else {
				tags["DURATION"] = test.value
			}
			if _, _, err := compareMediaEditDocuments(source, candidate, "mkv", 7); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("changed duration metadata was accepted: %v", err)
			}
		})
	}
}
