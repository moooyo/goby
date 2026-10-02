//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type scanShortProofOwnerTransactionSpan struct {
	Ordinal int64
	Elapsed time.Duration
	Finish  string
	Err     error
}

type scanShortProofSQLSnapshot struct {
	OwnerTransactions     int64
	OwnerTransactionTime  time.Duration
	OwnerTransactionSpans []scanShortProofOwnerTransactionSpan
	HookErr               error
}

type scanShortProofQueryContextKey struct{}

type scanShortProofQueryTrace struct {
	statement string
	ordinal   int64
}

// The reserved owner is identified by its exact physical backend PID. Ordinary
// pool transactions and setup SQL cannot inflate the owner's measured spans.
type scanShortProofSQLTracer struct {
	Capture  *rootBindingScanCapture
	ownerPID atomic.Uint32
	mu       sync.Mutex
	onBegin  func(int64) error
	snapshot scanShortProofSQLSnapshot
	started  time.Time
	ordinal  int64
}

func (trace *scanShortProofSQLTracer) Reset() {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.snapshot = scanShortProofSQLSnapshot{}
	trace.started = time.Time{}
	trace.ordinal = 0
}

// Hooks run before BEGIN is sent. They may mutate through an ordinary pool
// connection, but must not call an owned Store operation while ownership.mu is
// held. Hook work is excluded from the BEGIN-to-COMMIT/ROLLBACK measurement.
func (trace *scanShortProofSQLTracer) SetOnBegin(hook func(int64) error) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.onBegin = hook
}

func (trace *scanShortProofSQLTracer) Snapshot() scanShortProofSQLSnapshot {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	snapshot := trace.snapshot
	snapshot.OwnerTransactionSpans = append([]scanShortProofOwnerTransactionSpan(nil), snapshot.OwnerTransactionSpans...)
	return snapshot
}

func (trace *scanShortProofSQLTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if conn.PgConn().PID() != trace.ownerPID.Load() {
		return ctx
	}
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if !strings.HasPrefix(statement, "begin") && statement != "commit" && statement != "rollback" {
		return ctx
	}
	trace.mu.Lock()
	ordinal := trace.ordinal
	var hook func(int64) error
	if strings.HasPrefix(statement, "begin") {
		trace.snapshot.OwnerTransactions++
		ordinal = trace.snapshot.OwnerTransactions
		trace.ordinal = ordinal
		hook = trace.onBegin
	}
	trace.mu.Unlock()
	if hook != nil {
		if err := hook(ordinal); err != nil {
			trace.mu.Lock()
			trace.snapshot.HookErr = errors.Join(trace.snapshot.HookErr, err)
			trace.mu.Unlock()
		}
	}
	if strings.HasPrefix(statement, "begin") {
		trace.mu.Lock()
		trace.started = time.Now()
		trace.mu.Unlock()
	}
	return context.WithValue(ctx, scanShortProofQueryContextKey{}, scanShortProofQueryTrace{statement, ordinal})
}

func (trace *scanShortProofSQLTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	query, ok := ctx.Value(scanShortProofQueryContextKey{}).(scanShortProofQueryTrace)
	if !ok || conn.PgConn().PID() != trace.ownerPID.Load() {
		return
	}
	if strings.HasPrefix(query.statement, "begin") && data.Err == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.started.IsZero() || query.ordinal != trace.ordinal {
		return
	}
	finish := query.statement
	if strings.HasPrefix(finish, "begin") {
		finish = "begin_error"
	}
	elapsed := time.Since(trace.started)
	trace.snapshot.OwnerTransactionTime += elapsed
	trace.snapshot.OwnerTransactionSpans = append(trace.snapshot.OwnerTransactionSpans,
		scanShortProofOwnerTransactionSpan{query.ordinal, elapsed, finish, data.Err})
	trace.started = time.Time{}
}

func scanShortProofAllowedCloseError(err error, allowed []error) bool {
	if err == nil {
		return true
	}
	for _, candidate := range allowed {
		if errors.Is(err, candidate) {
			return true
		}
	}
	return false
}

func scanShortProofTestFixture(t *testing.T, allowedCloseErrors ...error) (rootBindingScanFixture, *rootBindingScanTestCapture,
	*scanReconciliationEvidence, *scanReconciliationStaging, *scanShortProofSQLTracer) {
	t.Helper()
	return scanShortProofTestFixtureWithPrepare(t, nil, allowedCloseErrors...)
}

// prepare must finish all catalog and filesystem setup before evidence records
// raw directory membership. Adding files after this factory returns invalidates
// the observation instead of creating a larger equivalent profile dataset.
func scanShortProofTestFixtureWithPrepare(t *testing.T, prepare func(rootBindingScanFixture), allowedCloseErrors ...error) (
	rootBindingScanFixture, *rootBindingScanTestCapture, *scanReconciliationEvidence, *scanReconciliationStaging, *scanShortProofSQLTracer) {
	t.Helper()
	ctx, initialPool, initialStore, directory, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := initialStore.Close(ctx); err != nil {
		t.Fatalf("release initial short-proof owner: %v", err)
	}
	trace := &scanShortProofSQLTracer{}
	configuration := initialPool.Config()
	configuration.ConnConfig.Tracer = trace
	// Mirror the production connection strategy on both paired revisions.
	configuration.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	if configuration.ConnConfig.DescriptionCacheCapacity <= 0 {
		configuration.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
	}
	configuration.ConnConfig.RuntimeParams["jit"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create traced short-proof pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, &libraryFixtureProber{}, []string{directory})
	if err != nil {
		t.Fatalf("acquire traced short-proof owner: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); !scanShortProofAllowedCloseError(err, allowedCloseErrors) {
			t.Errorf("close traced short-proof store: %v", err)
		}
	})
	store.ownership.mu.Lock()
	trace.ownerPID.Store(store.ownership.conn.Conn().PgConn().PID())
	store.ownership.mu.Unlock()
	actor := metadataEditTestActor(t, ctx, pool, "short-proof-reader")
	library := libraryIntegrationCreate(t, ctx, store, "Short proof profile", "movies", directory)
	roots, err := store.ListRegisteredRoots(ctx, actor, library.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("discover short-proof root: roots = %d, error = %v", len(roots), err)
	}
	storageIdentity := RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: "0102030405060708090a0b0c0d0e0f10", HandleType: 1, Handle: []byte("private-directory-handle")}
	snapshot := RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: RootTopologyMapping{ApprovedPath: roots[0].AllowedPath, RegisteredPath: roots[0].Path},
		Anchor:  storageIdentity, RegisteredRoot: storageIdentity, Boundaries: []RootTopologyBoundary{}}
	read := rootBindingReadFixture{ctx, pool, store, actor, library, roots[0], snapshot}
	read.bind(t, 13)
	root := libraryRoot{id: read.root.RootID, libraryID: library.ID, path: read.root.Path,
		allowedPath: read.root.AllowedPath, relativePath: read.root.RelativePath}
	fixture := rootBindingScanFixture{read, root, rootBindingScanOwnedTask(t, ctx, pool, store, library)}
	scanReconciliationCommitInsertItem(t, fixture, "short-proof-missing", "Missing.mkv", "Movie", library.ID, false)
	if prepare != nil {
		prepare(fixture)
	}
	adapter := &rootBindingScanTestCapture{snapshot: snapshot}
	trace.Capture = scanReconciliationCommitCapture(t, fixture, adapter)
	evidence := scanReconciliationCommitEvidence(t, trace.Capture)
	staging, err := store.beginScanReconciliationStaging(ctx, fixture.task.job.ID, library.ID)
	if err != nil {
		t.Fatalf("begin short-proof Seen staging: %v", err)
	}
	t.Cleanup(func() {
		if err := staging.Close(); !scanShortProofAllowedCloseError(err, allowedCloseErrors) {
			t.Errorf("close short-proof Seen staging: %v", err)
		}
	})
	if err := staging.Seal(ctx); err != nil {
		t.Fatalf("seal empty short-proof Seen staging: %v", err)
	}
	trace.Reset()
	return fixture, adapter, evidence, staging, trace
}

type scanShortProofRootRevalidation struct {
	Ordinal   int64 `json:"ordinal"`
	OwnerHeld bool  `json:"owner_held"`
}

type scanShortProofProfile struct {
	TotalElapsedMS                float64                          `json:"total_elapsed_ms"`
	OwnerTransactionMS            float64                          `json:"owner_transaction_ms"`
	OwnerTransactions             int64                            `json:"owner_transactions"`
	RootRevalidations             int64                            `json:"root_revalidations"`
	RootRevalidationsWithOwner    int64                            `json:"root_revalidations_with_owner"`
	RootRevalidationsOutsideOwner int64                            `json:"root_revalidations_outside_owner"`
	RootRevalidationStages        []scanShortProofRootRevalidation `json:"root_revalidation_stages"`
	OwnerTransactionSpansMS       []float64                        `json:"owner_transaction_spans_ms"`
	MembersRemoved                int                              `json:"members_removed"`
	PresentItems                  int                              `json:"present_items"`
	IgnoredFiles                  int                              `json:"ignored_files"`
	Evidence                      string                           `json:"evidence"`
	QueryExecMode                 string                           `json:"query_exec_mode"`
	JIT                           string                           `json:"jit"`
	ProofDelayMS                  int64                            `json:"proof_delay_ms"`
}

// This file can be copied unchanged into the preceding checkout. It calls only
// the existing reconciliation entry point, not candidate preflight helpers.
// Root topology uses the existing controlled capture adapter; held directory
// handles, complete raw membership, file identities and absence proofs are real.
// Report every full revalidation so moving one out of ownership is observable
// without hiding the final precommit and postdelete filesystem proofs. Timings
// are observations, never acceptance thresholds or claimed throughput gains.
func TestScanReconciliationShortProofPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_SCAN_RECONCILIATION_SHORT_PROOF") != "1" {
		t.Skip("GOBY_TEST_SCAN_RECONCILIATION_SHORT_PROOF=1 enables the short-proof profile")
	}
	const missingItems, presentItems, ignoredFiles = 32, 32, 4096
	var proofDelay time.Duration
	if raw := os.Getenv("GOBY_SCAN_RECONCILIATION_SHORT_PROOF_DELAY_MS"); raw != "" {
		milliseconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || milliseconds < 0 || milliseconds > 1000 {
			t.Fatal("GOBY_SCAN_RECONCILIATION_SHORT_PROOF_DELAY_MS must be between 0 and 1000")
		}
		proofDelay = time.Duration(milliseconds) * time.Millisecond
	}
	fixture, adapter, evidence, staging, trace := scanShortProofTestFixtureWithPrepare(t, func(fixture rootBindingScanFixture) {
		for index := 1; index < missingItems; index++ {
			scanReconciliationCommitInsertItem(t, fixture, fmt.Sprintf("short-proof-missing-%03d", index),
				fmt.Sprintf("Missing-%03d.mkv", index), "Movie", fixture.library.ID, false)
		}
		for index := 0; index < presentItems; index++ {
			id, relative := fmt.Sprintf("short-proof-present-%03d", index), fmt.Sprintf("Present-%03d.mkv", index)
			contents := "video:preserved-short-proof-source"
			path := libraryIntegrationFile(t, fixture.scanRoot.path, relative, contents)
			scanReconciliationCommitInsertItem(t, fixture, id, relative, "Movie", fixture.library.ID, false)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			probe := libraryMediaFixture([]byte(contents))
			probe.ProbeVersion, probe.FileChangeTimeNs = media.CurrentProbeVersion, media.FileChangeTime(info)
			source, err := json.Marshal(probe)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=$1::jsonb, file_size=$2,
				modified_at=$3, file_identity=$4 WHERE id=$5`, string(source), info.Size(), info.ModTime(),
				fileIdentity(info), id); err != nil {
				t.Fatal(err)
			}
			userDataSeed(t, fixture.ctx, fixture.pool, fixture.actor.User.ID, UserData{
				ItemID: id, IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 91,
			})
		}
		for index := 0; index < ignoredFiles; index++ {
			libraryIntegrationFile(t, fixture.scanRoot.path, fmt.Sprintf("ignored-%02d/Note-%04d.txt", index%16, index),
				"unsupported media extension retained in raw directory evidence")
		}
	})
	before := scanShortProofPreservedSnapshot(t, fixture)
	var recordMu sync.Mutex
	var stages []scanShortProofRootRevalidation
	adapter.revalidate = func(ctx context.Context, _ int) error {
		held := !fixture.store.ownership.mu.TryLock()
		if !held {
			fixture.store.ownership.mu.Unlock()
		}
		recordMu.Lock()
		stages = append(stages, scanShortProofRootRevalidation{int64(len(stages) + 1), held})
		recordMu.Unlock()
		if proofDelay > 0 {
			// An optional synthetic delay demonstrates owner occupancy only. It
			// must not be used as measured filesystem cost or a speedup claim.
			timer := time.NewTimer(proofDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return ctx.Err()
	}
	trace.Reset()
	started := time.Now()
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{trace.Capture}, evidence, nil, staging)
	elapsed := time.Since(started)
	measurement := trace.Snapshot()
	if err != nil || len(albums) != 0 || measurement.HookErr != nil {
		t.Fatalf("reconcile short-proof profile: albums = %v, error = %v, hook error = %v", albums, err, measurement.HookErr)
	}
	if measurement.OwnerTransactions == 0 || int64(len(measurement.OwnerTransactionSpans)) != measurement.OwnerTransactions {
		t.Fatalf("profile did not observe every reserved owner transaction: %+v", measurement)
	}
	var removed, retainedPresent, roots int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM items WHERE library_id=$1 AND id LIKE 'short-proof-missing%'),
		(SELECT count(*) FROM items WHERE library_id=$1 AND id LIKE 'short-proof-present-%'),
		(SELECT count(*) FROM library_roots WHERE id=$2 AND library_id=$1)`, fixture.library.ID, fixture.scanRoot.id).
		Scan(&removed, &retainedPresent, &roots); err != nil {
		t.Fatal(err)
	}
	if removed != 0 || retainedPresent != presentItems || roots != 1 {
		t.Fatalf("profile changed deletion scope: retained missing = %d, present = %d, roots = %d", removed, retainedPresent, roots)
	}
	scanReconciliationCommitAssertItem(t, fixture, fixture.library.ID, true)
	if after := scanShortProofPreservedSnapshot(t, fixture); after != before {
		t.Fatal("reconciliation changed present items, source facts, or UserData")
	}
	recordMu.Lock()
	observed := append([]scanShortProofRootRevalidation(nil), stages...)
	recordMu.Unlock()
	// A sealed pass keeps exactly three complete proofs on both revisions.
	// Only the first changes from owner-held to outside ownership; the final
	// precommit and postdelete proofs remain inside the owner transaction.
	if len(observed) != 3 {
		t.Fatalf("reconciliation changed the complete filesystem proof sequence: %+v", observed)
	}
	for _, stage := range observed[1:] {
		if !stage.OwnerHeld {
			t.Fatalf("reconciliation released ownership during a final filesystem proof: %+v", observed)
		}
	}
	profile := scanShortProofProfile{TotalElapsedMS: float64(elapsed) / float64(time.Millisecond),
		OwnerTransactionMS: float64(measurement.OwnerTransactionTime) / float64(time.Millisecond),
		OwnerTransactions:  measurement.OwnerTransactions, RootRevalidations: int64(len(observed)),
		RootRevalidationStages: observed, MembersRemoved: missingItems, PresentItems: presentItems, IgnoredFiles: ignoredFiles,
		Evidence: "memory", QueryExecMode: fmt.Sprint(fixture.pool.Config().ConnConfig.DefaultQueryExecMode),
		ProofDelayMS: proofDelay.Milliseconds()}
	for _, stage := range observed {
		if stage.OwnerHeld {
			profile.RootRevalidationsWithOwner++
		} else {
			profile.RootRevalidationsOutsideOwner++
		}
	}
	for _, span := range measurement.OwnerTransactionSpans {
		if span.Finish != "commit" || span.Err != nil {
			t.Fatalf("successful profile did not commit its owner transaction: %+v", span)
		}
		profile.OwnerTransactionSpansMS = append(profile.OwnerTransactionSpansMS, float64(span.Elapsed)/float64(time.Millisecond))
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT current_setting('jit')`).Scan(&profile.JIT); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SCAN_RECONCILIATION_SHORT_PROOF_PROFILE %s", raw)
}

func scanShortProofPreservedSnapshot(t *testing.T, fixture rootBindingScanFixture) string {
	t.Helper()
	var snapshot string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'items', (SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY i.id),'[]') FROM items i
			WHERE i.library_id=$1 AND i.id LIKE 'short-proof-present-%'),
		'user_data', (SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY u.user_id,u.item_id),'[]') FROM user_item_data u
			WHERE u.item_id LIKE 'short-proof-present-%'))::text`, fixture.library.ID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot preserved short-proof sources and UserData: %v", err)
	}
	return snapshot
}
