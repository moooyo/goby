//go:build linux

package server

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisPreviewRevalidationPreservesMissingLeaseAfterPublication(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, false)
	f := fixture.stream.f
	missing := fixture.open(t)
	before := missing.Metadata()
	if before.Ready || missing.Reader() != nil {
		t.Fatal("missing preview fixture unexpectedly acquired a ready derivative")
	}
	fixture.seedReady(t)
	if err := missing.Revalidate(f.ctx); err != nil || missing.Metadata() != before || missing.Reader() != nil {
		t.Fatalf("source-only revalidation changed a missing lease after publication: metadata=%+v error=%v", missing.Metadata(), err)
	}
	ready := fixture.open(t)
	if !ready.Metadata().Ready || ready.Reader() == nil || fixture.cache.Stats().Readers != 2 {
		t.Fatal("a new lease did not acquire the physically published preview")
	}
	if err := ready.Revalidate(f.ctx); err != nil {
		t.Fatalf("current ready preview failed revalidation: %v", err)
	}
	// Withdraw only the durable references. The original source and pinned
	// derivative remain readable, so only the ready lease loses its grant.
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM analysis_previews WHERE item_id=$1`, fixture.source.ItemID); err != nil {
		t.Fatal(err)
	}
	if err := ready.Revalidate(f.ctx); !errors.Is(err, library.ErrAnalysisSourceChanged) {
		t.Fatalf("ready lease retained a withdrawn preview reference: %v", err)
	}
	if err := missing.Revalidate(f.ctx); err != nil || missing.Metadata() != before || missing.Reader() != nil {
		t.Fatalf("missing lease acquired a reference dependency: metadata=%+v error=%v", missing.Metadata(), err)
	}
	if err := ready.Close(); err != nil {
		t.Fatal(err)
	}
	if err := missing.Close(); err != nil {
		t.Fatal(err)
	}
	assertAnalysisProviderIdle(t, fixture)
}
