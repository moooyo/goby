//go:build linux

package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
)

func TestAnalysisPreviewGenerationReusesOneWidthOfScratchCapacity(t *testing.T) {
	_, configuration, work, source, _, plan := previewGenerationFixture(t)
	options := configuration.CacheOptions()
	options.Root = filepath.Join(t.TempDir(), "single-width-cache")
	options.MaxTemporaryFiles = plan.frames
	store, err := analysiscache.Open(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close single-width cache: %v", err)
		}
	})
	publication, values, err := generateAnalysisPreview(context.Background(), store, work, source, plan, previewGenerationExtract(work, source, ""))
	if publication != nil {
		defer func() {
			if err := publication.Discard(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil || publication == nil || len(values) != 3 || len(publication.Entry.Artifacts) != 4 {
		t.Fatalf("three variants did not fit one reusable scratch batch: values=%d publication=%v error=%v", len(values), publication, err)
	}
	stats := store.Stats()
	if stats.BuildingEntries != 0 || stats.ReservedBytes != 0 || stats.PendingPublications != 1 || stats.ReadyBytes != publication.Entry.Bytes {
		t.Fatalf("completed batches did not transfer exactly one publication: %+v", stats)
	}
}
