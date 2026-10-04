//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scanOperationScopeTraceKey struct{}

type scanOperationScopeTraceResult struct {
	requests, commits, approvalReads, legacyReads, mutations int
	err                                                      error
}

// The hook runs after the startup transaction has committed on its exact
// connection. It uses a separate, untraced pool and cannot contend with the
// startup row locks or recurse through this tracer.
type scanOperationScopeTrace struct {
	mu          sync.Mutex
	pending     map[*pgx.Conn]bool
	afterCommit func() error
	result      scanOperationScopeTraceResult
}

func (trace *scanOperationScopeTrace) arm(hook func() error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.pending = make(map[*pgx.Conn]bool)
	trace.afterCommit = hook
	trace.result = scanOperationScopeTraceResult{}
}

func (trace *scanOperationScopeTrace) snapshot() scanOperationScopeTraceResult {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.result
}

func (trace *scanOperationScopeTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	kind := ""
	trace.mu.Lock()
	if strings.Contains(data.SQL, "/* scan_operation_authority */") {
		trace.result.requests++
		kind = "startup"
	}
	if strings.Contains(data.SQL, "storage_binding") && strings.Contains(data.SQL, "FROM library_roots") {
		trace.result.approvalReads++
	}
	if strings.Contains(data.SQL, "/* primary_scan_authority */") {
		trace.result.legacyReads++
	}
	trace.mu.Unlock()
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") {
		kind = "commit"
	} else if strings.EqualFold(strings.TrimSpace(data.SQL), "rollback") {
		kind = "rollback"
	}
	if kind == "" {
		return ctx
	}
	return context.WithValue(ctx, scanOperationScopeTraceKey{}, kind)
}

func (trace *scanOperationScopeTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	kind, _ := ctx.Value(scanOperationScopeTraceKey{}).(string)
	trace.mu.Lock()
	if kind == "startup" {
		if data.Err == nil {
			trace.pending[conn] = true
		}
		trace.mu.Unlock()
		return
	}
	if (kind != "commit" && kind != "rollback") || !trace.pending[conn] {
		trace.mu.Unlock()
		return
	}
	delete(trace.pending, conn)
	if kind != "commit" || data.Err != nil || data.CommandTag.String() != "COMMIT" {
		trace.mu.Unlock()
		return
	}
	trace.result.commits++
	if conn.PgConn().TxStatus() != 'I' {
		trace.result.err = errors.Join(trace.result.err, errors.New("startup commit retained its transaction"))
		trace.mu.Unlock()
		return
	}
	hook := trace.afterCommit
	trace.afterCommit = nil
	trace.mu.Unlock()
	if hook != nil {
		err := hook()
		trace.mu.Lock()
		trace.result.mutations++
		trace.result.err = errors.Join(trace.result.err, err)
		trace.mu.Unlock()
	}
}

func TestScanOperationAuthorityCoversColdAndWarmMainThemeAndSidecars(t *testing.T) {
	prober := &libraryFixtureProber{}
	ctx, pool, previous, approved, userID := libraryIntegrationStore(t, prober)
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanOperationScopeTrace{pending: make(map[*pgx.Conn]bool)}
	config := pool.Config()
	config.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, prober, []string{approved})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close operation scope store: %v", err)
		}
	})
	moviePath := libraryIntegrationFile(t, approved, "operation-scope/Film/Feature.mp4", "video:operation-scope-film")
	songPath := libraryIntegrationFile(t, approved, "operation-scope/Film/theme.mp3", "audio:operation-scope-theme")
	libraryIntegrationFile(t, approved, "operation-scope/Film/Feature.en.srt", subtitleTestSRT)
	poster := imageStoreTestPNG(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(moviePath), "poster.png"), poster, 0600); err != nil {
		t.Fatal(err)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Operation scope", "mixed", filepath.Dir(moviePath))
	var rootID string
	var initialRevision int64
	var stored bool
	if err := pool.QueryRow(ctx, `SELECT id,binding_revision,storage_binding IS NOT NULL FROM library_roots WHERE library_id=$1`, library.ID).
		Scan(&rootID, &initialRevision, &stored); err != nil {
		t.Fatal(err)
	}
	if !stored {
		t.Fatal("operation scope fixture requires a stored root approval")
	}
	posterHash := fmt.Sprintf("%x", sha256.Sum256(poster))
	subtitleHash := fmt.Sprintf("%x", sha256.Sum256([]byte(subtitleTestSRT)))
	assertCatalog := func() [2]string {
		t.Helper()
		ordinary := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
		if len(ordinary.Items) != 1 {
			t.Fatalf("operation lost its primary movie: %+v", ordinary.Items)
		}
		movie := libraryIntegrationItemByPath(t, ordinary.Items, moviePath)
		if movie.Media == nil || movie.Media.Size <= 0 || len(movie.Media.Streams) == 0 {
			t.Fatalf("operation lost accepted primary probe facts: %+v", movie)
		}
		themes := themeScanTestResources(t, ctx, pool, library.ID)
		if len(themes) != 1 || themes[0].ownerID != movie.ID || themes[0].path != songPath || !themes[0].active ||
			themes[0].kind != "song" || themes[0].itemType != "Audio" || themes[0].probe.Size <= 0 || len(themes[0].probe.Streams) == 0 {
			t.Fatalf("operation lost its accepted theme song: %+v", themes)
		}
		var images, exactImages, subtitles, exactSubtitles int
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM item_images WHERE root_id=$1),
			(SELECT count(*) FROM item_images WHERE item_id=$2 AND image_type='Primary' AND relative_path='poster.png'
				AND source_hash=$3 AND width>0 AND height>0),
			(SELECT count(*) FROM item_subtitles WHERE root_id=$1 AND active),
			(SELECT count(*) FROM item_subtitles WHERE item_id=$2 AND active AND relative_path='Feature.en.srt'
				AND source_hash=$4 AND codec='srt')`, rootID, movie.ID, posterHash, subtitleHash).
			Scan(&images, &exactImages, &subtitles, &exactSubtitles); err != nil {
			t.Fatal(err)
		}
		if images != 1 || exactImages != 1 || subtitles != 1 || exactSubtitles != 1 {
			t.Fatalf("operation lost sidecar ownership or exact payloads: images=%d/%d subtitles=%d/%d", images, exactImages, subtitles, exactSubtitles)
		}
		return [2]string{movie.ID, themes[0].id}
	}
	assertAuthority := func(phase string, mutations int) {
		t.Helper()
		result := trace.snapshot()
		if result.requests != 1 || result.commits != 1 || result.approvalReads != 1 || result.legacyReads != 0 ||
			result.mutations != mutations || result.err != nil {
			t.Fatalf("%s scan did not retain one complete startup authority transaction: %+v", phase, result)
		}
	}
	var identities [2]string
	for index, phase := range []string{"cold", "warm"} {
		beforeProbes := len(prober.calls())
		trace.arm(func() error {
			if len(prober.calls()) != beforeProbes {
				return errors.New("source probing started before the startup authority commit hook")
			}
			change, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			updated, err := pool.Exec(change, `UPDATE library_roots SET binding_revision=binding_revision+1,
				bound_at=clock_timestamp(),bound_by=$2 WHERE id=$1`, rootID, "operation-scope-"+phase)
			if err != nil {
				return err
			}
			if updated.RowsAffected() != 1 {
				return fmt.Errorf("startup approval mutation affected %d roots", updated.RowsAffected())
			}
			return nil
		})
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		assertAuthority(phase, 1)
		if job.Error != "" {
			t.Fatalf("%s scan warned after an admitted approval edit: %+v", phase, job)
		}
		current := assertCatalog()
		if phase == "cold" {
			identities = current
			if len(prober.calls()) != 2 {
				t.Fatalf("cold scan did not probe exactly the primary movie and theme song: %q", prober.calls())
			}
		} else if current != identities || len(prober.calls()) != beforeProbes || job.Added != 0 || job.Updated != 0 {
			t.Fatalf("warm scan changed accepted identities, repeated probes, or rewrote primary facts: identities=%v/%v probes=%d/%d job=%+v",
				current, identities, len(prober.calls()), beforeProbes, job)
		}
		var revision int64
		var boundBy string
		if err := pool.QueryRow(ctx, `SELECT binding_revision,bound_by FROM library_roots WHERE id=$1`, rootID).Scan(&revision, &boundBy); err != nil {
			t.Fatal(err)
		}
		if revision != initialRevision+int64(index)+1 || boundBy != "operation-scope-"+phase {
			t.Fatalf("%s scan overwrote the approval edit made after startup: revision=%d bound_by=%q", phase, revision, boundBy)
		}
	}

	// A new operation must read the current legacy unbound configuration even
	// though every primary, theme, and sidecar source is already cached.
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL,
		binding_revision=binding_revision+1 WHERE id=$1`, rootID); err != nil {
		t.Fatal(err)
	}
	beforeProbes := len(prober.calls())
	trace.arm(nil)
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	assertAuthority("legacy unbound configuration", 0)
	if job.Error != "" || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != beforeProbes || assertCatalog() != identities {
		t.Fatalf("new unbound scan repeated probes or changed the accepted catalog: probes=%d/%d job=%+v", len(prober.calls()), beforeProbes, job)
	}
	var stillUnbound bool
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT storage_binding IS NULL AND bound_at IS NULL AND bound_by IS NULL,binding_revision
		FROM library_roots WHERE id=$1`, rootID).Scan(&stillUnbound, &revision); err != nil {
		t.Fatal(err)
	}
	if !stillUnbound || revision != initialRevision+3 {
		t.Fatalf("new scan rewrote its unbound configuration: unbound=%v revision=%d", stillUnbound, revision)
	}
}
