//go:build linux

package library

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mediaRevalidationPerformanceContextKey struct{}

type mediaRevalidationPerformanceTrace struct {
	queries, sources, entities, subtitles atomic.Int64
	statement                             string
}

func (trace *mediaRevalidationPerformanceTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if enabled, _ := ctx.Value(mediaRevalidationPerformanceContextKey{}).(bool); !enabled {
		return ctx
	}
	trace.queries.Add(1)
	if strings.Contains(data.SQL, "AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')") {
		trace.sources.Add(1)
		trace.statement = data.SQL
		if strings.Contains(data.SQL, "jsonb_agg") && strings.Contains(data.SQL, "catalog_entities") {
			trace.entities.Add(1)
		}
	}
	if strings.Contains(data.SQL, " FROM item_subtitles ") || strings.Contains(data.SQL, " FROM item_owned_subtitles ") ||
		strings.Contains(data.SQL, " FROM item_bitmap_subtitles ") {
		trace.subtitles.Add(1)
	}
	return ctx
}

func (*mediaRevalidationPerformanceTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *mediaRevalidationPerformanceTrace) reset() {
	trace.queries.Store(0)
	trace.sources.Store(0)
	trace.entities.Store(0)
	trace.subtitles.Store(0)
	trace.statement = ""
}

// The optional interface allows this file to compile unchanged on the baseline
// checkout. That checkout measures its complete OpenMediaFor delivery lookup;
// the optimized checkout measures the dedicated revalidation lookup. Both open
// and close real rooted sources and include fresh policy/publication checks.
type mediaRevalidationPerformanceAPI interface {
	RevalidateMediaSourceFor(context.Context, Subject, string, string, bool) (*os.File, MediaFile, error)
}

func TestMediaRevalidationPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_MEDIA_REVALIDATION_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_MEDIA_REVALIDATION_PERFORMANCE=1 enables the media revalidation performance profile")
	}
	fixture := mediaSourceTestCatalog(t, nil)
	libraryIntegrationFile(t, fixture.allowedRoot, "movies/Nested/Feature.nfo",
		`<movie><title>Hot media authorization</title><plot>`+strings.Repeat("Catalog overview. ", 64)+`</plot><genre>Drama</genre><genre>Action</genre><tag>Tagged</tag><studio>Fixture studio</studio><actor><name>First Person</name><role>Lead</role></actor><actor><name>Second Person</name><role>Supporting</role></actor></movie>`)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	trace := &mediaRevalidationPerformanceTrace{}
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	fixture.store.pool = tracedPool
	t.Cleanup(func() { fixture.store.pool = fixture.pool })
	projected, usesProjection := any(fixture.store).(mediaRevalidationPerformanceAPI)
	key := seedCatalogApplicationKey(t, fixture.ctx, fixture.pool, "media-revalidation-performance-key", true)
	for _, subject := range []Subject{{UserID: fixture.userID}, key} {
		for _, includeSubtitles := range []bool{false, true} {
			name := fmt.Sprintf("application-%t/subtitles-%t", subject.ApplicationCredentialID != "", includeSubtitles)
			t.Run(name, func(t *testing.T) {
				ctx := context.WithValue(fixture.ctx, mediaRevalidationPerformanceContextKey{}, true)
				operation := func() {
					var file *os.File
					var source MediaFile
					var err error
					if usesProjection {
						file, source, err = projected.RevalidateMediaSourceFor(ctx, subject, fixture.item.ID, "", includeSubtitles)
					} else {
						file, source, err = fixture.store.OpenMediaFor(ctx, subject, fixture.item.ID, "")
					}
					if file != nil {
						_ = file.Close()
					}
					if err != nil || file == nil || source.Item.ID != fixture.item.ID || source.ETag == "" {
						t.Fatalf("hot media revalidation failed: %v", err)
					}
				}
				for range 5 {
					operation()
				}
				allocations := testing.AllocsPerRun(30, operation)
				trace.reset()
				const rounds = 100
				started := time.Now()
				for range rounds {
					operation()
				}
				elapsed := time.Since(started)
				if trace.sources.Load() != rounds {
					t.Fatal("the profile did not observe one current source projection per operation")
				}
				t.Logf("media_revalidation_performance projected=%t application_key=%t subtitles=%t operations=%d elapsed=%s ns_per_operation=%d allocations_per_operation=%.1f sql=%d entity_projections=%d subtitle_queries=%d",
					usesProjection, subject.ApplicationCredentialID != "", includeSubtitles, rounds, elapsed,
					elapsed.Nanoseconds()/rounds, allocations, trace.queries.Load(), trace.entities.Load(), trace.subtitles.Load())
			})
		}
	}
}
