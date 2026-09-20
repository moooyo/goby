package backuppg

import (
	"context"
	"errors"
	"testing"
)

func TestCompatibilityMetadataFactsPreserveUnknownAndRejectInvalidRetainedValues(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"Status":null,"EndDate":null,"AirsBeforeSeasonNumber":null}`,
		`{"Status":"Ended","EndDate":"2025-01-02T03:04:05Z","AirsBeforeSeasonNumber":1,"AirsAfterSeasonNumber":0,"AirsBeforeEpisodeNumber":2147483647}`,
		`{"Status":"Continuing","EndDate":"0001-01-01T00:00:00Z"}`,
	} {
		if !validCompatibilityMetadataFacts([]byte(raw)) {
			t.Fatalf("rejected canonical or absent retained metadata: %s", raw)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{"Status":"ended"}`, `{"Status":"Unknown"}`, `{"Status":true}`,
		`{"EndDate":"2025-02-30T00:00:00Z"}`, `{"EndDate":"0000-01-01T00:00:00Z"}`,
		`{"EndDate":"2025-01-01T00:00:00+01:00"}`, `{"EndDate":1}`,
		`{"AirsBeforeSeasonNumber":-1}`, `{"AirsBeforeEpisodeNumber":2147483648}`,
		`{"AirsAfterSeasonNumber":1.5}`, `{"AirsBeforeEpisodeNumber":"2"}`,
		`{"status":"Ended"}`, `{"Status":"Ended","status":"Unknown"}`,
	} {
		if validCompatibilityMetadataFacts([]byte(raw)) {
			t.Fatalf("accepted invalid retained metadata: %s", raw)
		}
	}
}

func TestCompatibilityLongTailStateVersionAndCancellationGuards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, version := range []int64{48, 49} {
		if err := validateCompatibilityLongTailState(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled state validation lost its context error")
		}
	}
	if err := validateCompatibilityLongTailState(context.Background(), nil, 48); err != nil {
		t.Fatal("historical schema gained a new metadata requirement")
	}
	if err := validateCompatibilityLongTailState(context.Background(), nil, 49); !errors.Is(err, ErrDatabase) {
		t.Fatal("current state validation did not require its real transaction")
	}
}
