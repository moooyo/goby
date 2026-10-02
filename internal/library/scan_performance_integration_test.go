//go:build linux

package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type scanPerformanceObserverContextKey struct{}
type scanPerformanceWriteContextKey struct{}
type scanPerformanceTimingContextKey struct{}
type scanPerformanceStatementContextKey struct{}

type scanPerformanceQueryTiming struct {
	started  time.Time
	category string
}

type scanPerformanceAuthorityTransaction struct {
	eligible       bool
	statements     [6]string
	statementCount int
}

var scanPerformanceAuthorityStatements = func() []string {
	statements := []string{
		"SELECT COALESCE(task_child_id, '') FROM scan_jobs WHERE id = $1",
		"SELECT run_id FROM task_run_children WHERE id = $1",
		"SELECT state, stop_reason, task_key FROM task_runs WHERE id = $1 FOR UPDATE",
		`SELECT library_id, state, COALESCE(scan_job_id, ''), scanned, added, updated,
			error_code, error_message, started_at, finished_at FROM task_run_children
			WHERE id = $1 AND run_id = $2 FOR UPDATE`,
		"SELECT " + jobColumns + " FROM scan_jobs WHERE id = $1 FOR UPDATE",
		`SELECT ` + rootBindingMetadataColumns + `, r.storage_binding IS NOT NULL,
			CASE WHEN octet_length(r.storage_binding::text) <= $3 THEN r.storage_binding::text END,
			r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
			FROM library_roots r WHERE r.library_id = $1 AND r.id = $2 FOR UPDATE OF r`,
	}
	for index, statement := range statements {
		statements[index] = scanPerformanceNormalizeSQL(statement)
	}
	return statements
}()

func scanPerformanceNormalizeSQL(statement string) string {
	return strings.Join(strings.Fields(strings.ToLower(statement)), " ")
}

func (transaction *scanPerformanceAuthorityTransaction) isAuthority() bool {
	if !transaction.eligible {
		return false
	}
	if transaction.statementCount == 3 {
		return transaction.statements[0] == scanPerformanceAuthorityStatements[0] &&
			transaction.statements[1] == scanPerformanceAuthorityStatements[4] &&
			transaction.statements[2] == scanPerformanceAuthorityStatements[5]
	}
	if transaction.statementCount != len(scanPerformanceAuthorityStatements) {
		return false
	}
	for index, statement := range scanPerformanceAuthorityStatements {
		if transaction.statements[index] != statement {
			return false
		}
	}
	return true
}

type scanPerformanceSQLTracer struct {
	queries, begins, commits, rollbacks                   atomic.Int64
	authorityBegins, authorityCommits, authorityRollbacks atomic.Int64
	itemRows, metadataRows, scanRows, childRows           atomic.Int64
	cachedCompletionChecks                                atomic.Int64
	relationshipQueries, relationshipNanos                atomic.Int64
	completionNanos                                       atomic.Int64
	timingEnabled                                         bool
	authorityMu                                           sync.Mutex
	authorityTransactions                                 map[*pgx.Conn]*scanPerformanceAuthorityTransaction
}

func (trace *scanPerformanceSQLTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = context.WithValue(ctx, scanPerformanceStatementContextKey{}, scanPerformanceNormalizeSQL(data.SQL))
	if ignored, _ := ctx.Value(scanPerformanceObserverContextKey{}).(bool); ignored {
		return ctx
	}
	trace.queries.Add(1)
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(statement, "with progress_run as materialized") {
		trace.cachedCompletionChecks.Add(1)
	}
	switch {
	case strings.HasPrefix(statement, "begin"):
		trace.begins.Add(1)
	case statement == "commit":
		trace.commits.Add(1)
	case statement == "rollback":
		trace.rollbacks.Add(1)
	}
	var category string
	for _, table := range []string{"items", "item_metadata_state", "scan_jobs", "task_run_children"} {
		if strings.HasPrefix(statement, "update "+table+" ") || strings.HasPrefix(statement, "insert into "+table+" ") ||
			strings.HasPrefix(statement, "insert into "+table+"\n") || strings.HasPrefix(statement, "insert into "+table+"(") {
			category = table
			break
		}
	}
	ctx = context.WithValue(ctx, scanPerformanceWriteContextKey{}, category)
	if trace.timingEnabled {
		// Opt-in timings include the client round trip and result consumption.
		// They do not include waiting for ownership.mu before Query starts.
		timingCategory := ""
		if strings.HasPrefix(statement, "with progress_run as materialized") {
			timingCategory = "completion"
		} else if strings.HasPrefix(statement, "select coalesce(task_child_id, '') from scan_jobs where id = $1") ||
			strings.HasPrefix(statement, "select run_id from task_run_children where id = $1") ||
			strings.HasPrefix(statement, "select state, stop_reason, task_key from task_runs where id = $1 for update") ||
			(strings.HasPrefix(statement, "select library_id, state, coalesce(scan_job_id, '')") && strings.Contains(statement, "from task_run_children")) ||
			strings.Contains(statement, " from scan_jobs where id = $1 for update") {
			timingCategory = "relationship"
		}
		if timingCategory != "" {
			ctx = context.WithValue(ctx, scanPerformanceTimingContextKey{}, scanPerformanceQueryTiming{started: time.Now(), category: timingCategory})
		}
	}
	return ctx
}

func (trace *scanPerformanceSQLTracer) TraceQueryEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryEndData) {
	ignored, _ := ctx.Value(scanPerformanceObserverContextKey{}).(bool)
	statement, _ := ctx.Value(scanPerformanceStatementContextKey{}).(string)
	trace.recordAuthorityStatement(connection, statement, ignored, data)
	if ignored || data.Err != nil {
		return
	}
	if timing, ok := ctx.Value(scanPerformanceTimingContextKey{}).(scanPerformanceQueryTiming); ok {
		nanoseconds := time.Since(timing.started).Nanoseconds()
		if timing.category == "completion" {
			trace.completionNanos.Add(nanoseconds)
		} else {
			trace.relationshipQueries.Add(1)
			trace.relationshipNanos.Add(nanoseconds)
		}
	}
	category, _ := ctx.Value(scanPerformanceWriteContextKey{}).(string)
	rows := data.CommandTag.RowsAffected()
	switch category {
	case "items":
		trace.itemRows.Add(rows)
	case "item_metadata_state":
		trace.metadataRows.Add(rows)
	case "scan_jobs":
		trace.scanRows.Add(rows)
	case "task_run_children":
		trace.childRows.Add(rows)
	}
}

func (trace *scanPerformanceSQLTracer) recordAuthorityStatement(connection *pgx.Conn, statement string, ignored bool, data pgx.TraceQueryEndData) {
	trace.authorityMu.Lock()
	defer trace.authorityMu.Unlock()
	transaction := trace.authorityTransactions[connection]
	boundary := strings.HasPrefix(statement, "begin") || statement == "commit" || statement == "rollback"
	if ignored || data.Err != nil {
		if boundary {
			delete(trace.authorityTransactions, connection)
		} else if transaction != nil {
			transaction.eligible = false
		}
		return
	}
	if strings.HasPrefix(statement, "begin") {
		if trace.authorityTransactions == nil {
			trace.authorityTransactions = make(map[*pgx.Conn]*scanPerformanceAuthorityTransaction)
		}
		// An unobserved boundary or an unfamiliar BEGIN form cannot be deducted.
		trace.authorityTransactions[connection] = &scanPerformanceAuthorityTransaction{
			eligible: transaction == nil && statement == "begin" && data.CommandTag.String() == "BEGIN",
		}
		return
	}
	if statement == "commit" || statement == "rollback" {
		// pgx reports a server ROLLBACK tag before Commit returns its error.
		if transaction != nil && transaction.isAuthority() && data.CommandTag.String() == strings.ToUpper(statement) {
			trace.authorityBegins.Add(1)
			if statement == "commit" {
				trace.authorityCommits.Add(1)
			} else {
				trace.authorityRollbacks.Add(1)
			}
		}
		delete(trace.authorityTransactions, connection)
		return
	}
	if transaction == nil || !transaction.eligible {
		return
	}
	// These complete read-authority transactions still acquire FOR UPDATE locks.
	// Unknown statements, DML, repeats and incomplete sequences remain in totals.
	allowed := false
	for _, expected := range scanPerformanceAuthorityStatements {
		if statement == expected {
			allowed = true
			break
		}
	}
	// QueryRow can report ErrNoRows after TraceQueryEnd; each authority lookup
	// must have produced its single unique row before it can be attributed.
	if !allowed || data.CommandTag.RowsAffected() != 1 || transaction.statementCount == len(transaction.statements) {
		transaction.eligible = false
		return
	}
	transaction.statements[transaction.statementCount] = statement
	transaction.statementCount++
}

func (trace *scanPerformanceSQLTracer) reset() {
	trace.queries.Store(0)
	trace.begins.Store(0)
	trace.commits.Store(0)
	trace.rollbacks.Store(0)
	trace.authorityMu.Lock()
	trace.authorityTransactions = nil
	trace.authorityBegins.Store(0)
	trace.authorityCommits.Store(0)
	trace.authorityRollbacks.Store(0)
	trace.authorityMu.Unlock()
	trace.itemRows.Store(0)
	trace.metadataRows.Store(0)
	trace.scanRows.Store(0)
	trace.childRows.Store(0)
	trace.cachedCompletionChecks.Store(0)
	trace.relationshipQueries.Store(0)
	trace.relationshipNanos.Store(0)
	trace.completionNanos.Store(0)
}

type scanPerformanceCorpus struct {
	label, collectionType, root, addedRelative string
	library                                    Library
	paths                                      []string
	wantCounts                                 map[string]int
}

type scanPerformanceRecord struct {
	id, libraryID, parentID, kind, path, relative, name, media string
	folder                                                     bool
	index, parentIndex                                         int
}

func (record scanPerformanceRecord) change(kind CatalogChangeKind) CatalogChange {
	return CatalogChange{Kind: kind, ItemID: record.id, LibraryID: record.libraryID,
		ParentID: record.parentID, IsFolder: record.folder, IsCollectionFolder: record.kind == "CollectionFolder"}
}

// This opt-in profile uses the public scan lifecycle and a cheap descriptor
// prober to isolate catalog, filesystem and transaction overhead. It can be
// copied unchanged into the pre-optimization checkout for paired measurements.
// Timings are observations, not acceptance thresholds. Linux supplies the stable
// file identity and storage approval needed by the rename/deletion assertions.
func TestScanPerformanceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_SCAN_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_SCAN_PERFORMANCE=1 enables the media scan performance profile")
	}
	focus := os.Getenv("GOBY_SCAN_PERFORMANCE_FOCUS") == "1"
	var probes atomic.Int64
	prober := scanProberFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		probes.Add(1)
		stat, err := file.Stat()
		if err != nil {
			return media.Info{}, err
		}
		info := libraryMediaFixture([]byte("video:scan-performance"))
		info.Size, info.ProbeVersion = stat.Size(), media.CurrentProbeVersion
		return info, nil
	})
	ctx, fixturePool, fixtureStore, root, userID := libraryIntegrationStoreWithTimeout(t, prober, 8*time.Minute)
	if err := fixtureStore.Close(ctx); err != nil {
		t.Fatalf("release fixture catalog ownership: %v", err)
	}
	trace := &scanPerformanceSQLTracer{timingEnabled: os.Getenv("GOBY_SCAN_PERFORMANCE_SQL_TIMING") == "1"}
	configuration := fixturePool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create traced scan pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	// Reopen the entire Store so its reserved ownership session and ordinary
	// pooled reads share the tracer. Replacing Store.pool alone misses owned SQL.
	store, err := New(tracedPool, prober, []string{root})
	if err != nil {
		t.Fatalf("reopen traced scan store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close traced scan store: %v", err)
		}
	})
	corpora := []scanPerformanceCorpus{
		{label: "flat_movies", collectionType: "movies", root: "flat-movies",
			addedRelative: "flat-movies/Movie0161.mp4", wantCounts: map[string]int{"CollectionFolder": 1, "Movie": 160}},
		{label: "flat_episodes", collectionType: "tvshows", root: "flat-tv",
			addedRelative: "flat-tv/Flat Show01.S01E17.mp4", wantCounts: map[string]int{"CollectionFolder": 1, "Series": 6, "Season": 12, "Episode": 192}},
		{label: "directory_episodes", collectionType: "tvshows", root: "directory-tv",
			addedRelative: "directory-tv/Directory Show01/Season01/Directory Show01.S01E07.mp4", wantCounts: map[string]int{"CollectionFolder": 1, "Series": 4, "Season": 8, "Episode": 48}},
	}
	for movie := 1; movie <= 160; movie++ {
		corpora[0].paths = append(corpora[0].paths, libraryIntegrationFile(t, root,
			fmt.Sprintf("flat-movies/Movie%04d.mp4", movie), fmt.Sprintf("video:movie-%04d", movie)))
	}
	for show := 1; show <= 6; show++ {
		for season := 1; season <= 2; season++ {
			for episode := 1; episode <= 16; episode++ {
				relative := fmt.Sprintf("flat-tv/Flat Show%02d.S%02dE%02d.mp4", show, season, episode)
				corpora[1].paths = append(corpora[1].paths, libraryIntegrationFile(t, root, relative, "video:"+relative))
			}
		}
	}
	for show := 1; show <= 4; show++ {
		for season := 1; season <= 2; season++ {
			for episode := 1; episode <= 6; episode++ {
				relative := fmt.Sprintf("directory-tv/Directory Show%02d/Season%02d/Directory Show%02d.S%02dE%02d.mp4", show, season, show, season, episode)
				corpora[2].paths = append(corpora[2].paths, libraryIntegrationFile(t, root, relative, "video:"+relative))
			}
		}
	}
	for index := range corpora {
		corpus := &corpora[index]
		corpus.library = libraryIntegrationCreate(t, ctx, store, corpus.label, corpus.collectionType, filepath.Join(root, corpus.root))
	}
	notifications := make(chan CatalogNotification, 2048)
	var notificationOverflow atomic.Bool
	store.SetCatalogChangeListener(func(notification CatalogNotification) {
		select {
		case notifications <- notification:
		default:
			notificationOverflow.Store(true)
		}
	})
	t.Cleanup(func() { store.SetCatalogChangeListener(nil) })
	observerCtx := context.WithValue(ctx, scanPerformanceObserverContextKey{}, true)
	scanPerformanceLogConnectionPolicy(t, observerCtx, store, tracedPool.Config(), "initial", focus, trace.timingEnabled)
	scan := func(phase string, corpus scanPerformanceCorpus, force bool, wantAdded, wantUpdated int, wantProbes int64) {
		t.Helper()
		childID := ""
		if strings.HasPrefix(phase, "task_owned_") {
			_, children := taskScanFixture(t, observerCtx, fixturePool, corpus.library)
			childID = children[0]
		}
		walBefore := scanPerformanceWALPosition(t, observerCtx, fixturePool)
		trace.reset()
		probesBefore, started := probes.Load(), time.Now()
		var job Job
		var err error
		if childID == "" {
			job, err = store.StartScanWithOptions(ctx, corpus.library.ID, ScanOptions{ForceProbe: force})
		} else {
			var admission ScanAdmission
			admission, err = store.AdmitTaskScan(ctx, childID)
			job = admission.Job
			if err == nil && admission.Kind != ScanAdmitted {
				t.Fatalf("performance task scan was not admitted: %+v", admission)
			}
		}
		if err != nil {
			t.Fatalf("start %s/%s scan: %v", phase, corpus.label, err)
		}
		// Observer polling is excluded from SQL counts and uses the capacity
		// helper's 250 ms interval rather than adding 100 reads per second.
		job = libraryIntegrationWaitJobWithTimeout(t, observerCtx, store, job.ID, "Completed", 3*time.Minute)
		elapsed, probeCalls := time.Since(started), probes.Load()-probesBefore
		walAfter := scanPerformanceWALPosition(t, observerCtx, fixturePool)
		var walBytes int64
		if err := fixturePool.QueryRow(observerCtx, `SELECT pg_wal_lsn_diff($1::pg_lsn,$2::pg_lsn)::bigint`, walAfter, walBefore).Scan(&walBytes); err != nil {
			t.Fatalf("measure scan WAL delta: %v", err)
		}
		if job.Error != "" || job.Scanned != len(corpus.paths) || job.Added != wantAdded || job.Updated != wantUpdated ||
			job.ForceProbe != force || job.CancelRequested || job.StartedAt == nil || job.FinishedAt == nil || probeCalls != wantProbes {
			t.Fatalf("%s/%s scan facts differ: job=%+v probe_calls=%d want_probes=%d", phase, corpus.label, job, probeCalls, wantProbes)
		}
		if trace.queries.Load() == 0 || trace.begins.Load() == 0 || trace.commits.Load() == 0 {
			t.Fatal("scan SQL tracer did not observe the owned transaction session")
		}
		if childID != "" {
			taskScanAssertChild(t, observerCtx, fixturePool, childID, job)
		}
		// Direct row counts cover client UPDATE/INSERT statements, excluding
		// trigger/function writes and DELETE. WAL positions are cluster-wide
		// observations, so paired measurements need an otherwise quiet database.
		t.Logf("scan_performance phase=%s library=%s elapsed=%s job_elapsed=%s sql=%d begin=%d commit=%d rollback=%d probe_calls=%d scanned=%d added=%d updated=%d direct_items_write_rows=%d direct_metadata_write_rows=%d direct_scan_write_rows=%d direct_child_write_rows=%d wal_bytes=%d cached_completion_checks=%d relationship_queries=%d relationship_query_ns=%d completion_query_ns=%d",
			phase, corpus.label, elapsed, job.FinishedAt.Sub(*job.StartedAt), trace.queries.Load(), trace.begins.Load(),
			trace.commits.Load(), trace.rollbacks.Load(), probeCalls, job.Scanned, job.Added, job.Updated,
			trace.itemRows.Load(), trace.metadataRows.Load(), trace.scanRows.Load(), trace.childRows.Load(), walBytes, trace.cachedCompletionChecks.Load(),
			trace.relationshipQueries.Load(), trace.relationshipNanos.Load(), trace.completionNanos.Load())
	}
	for _, corpus := range corpora {
		scan("cold", corpus, false, len(corpus.paths), 0, int64(len(corpus.paths)))
	}
	initial := scanPerformanceReadCatalog(t, ctx, fixturePool)
	scanPerformanceAssertCounts(t, initial, corpora)
	wantChanges := make(map[string]CatalogChange)
	for _, record := range initial {
		if record.kind != "CollectionFolder" {
			scanPerformanceAddChange(t, wantChanges, record.change(CatalogAdded))
		}
	}
	scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), wantChanges)
	seedIDs := make([]string, 0, 5)
	for _, corpus := range corpora {
		seedIDs = append(seedIDs, scanPerformanceFindPath(t, initial, corpus.paths[0]).id)
	}
	seedIDs = append(seedIDs,
		scanPerformanceFindRelative(t, initial, corpora[1].library.ID, "//series/flat show01").id,
		scanPerformanceFindRelative(t, initial, corpora[2].library.ID, "Directory Show01/Season01").id)
	playedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for index, id := range seedIDs {
		userDataSeed(t, ctx, fixturePool, userID, UserData{ItemID: id, IsFavorite: true,
			Played: index%2 == 0, PlayCount: index + 2, PlaybackPositionTicks: int64(index+1) * media.TicksPerSecond, LastPlayedDate: &playedAt})
	}
	userDataBefore := scanPerformanceUserData(t, ctx, fixturePool, len(seedIDs))
	assertUserData := func() {
		t.Helper()
		if scanPerformanceUserData(t, ctx, fixturePool, len(seedIDs)) != userDataBefore {
			t.Fatal("scan rewrote saved UserData values, timestamps or row versions")
		}
	}
	for visit := 1; visit <= 2; visit++ {
		for _, corpus := range corpora {
			scan(fmt.Sprintf("cached_%d", visit), corpus, false, 0, 0, 0)
		}
		if current := scanPerformanceReadCatalog(t, ctx, fixturePool); !reflect.DeepEqual(current, initial) {
			t.Fatal("cached scan changed catalog identities, hierarchy or accepted media")
		}
		scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), nil)
		assertUserData()
	}
	for _, corpus := range corpora {
		scan("task_owned_cached", corpus, false, 0, 0, 0)
	}
	if current := scanPerformanceReadCatalog(t, observerCtx, fixturePool); !reflect.DeepEqual(current, initial) {
		t.Fatal("task-owned cached scan changed catalog identities, hierarchy or accepted media")
	}
	scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), nil)
	assertUserData()
	if focus {
		// Keep identical cold priming and both independent cached controls before
		// task-owned measurements. Unrelated incremental/forced work is omitted.
		if probes.Load() != 400 {
			t.Fatalf("focused profile probe calls=%d, want 400 priming probes", probes.Load())
		}
		scanPerformanceLogConnectionPolicy(t, observerCtx, store, tracedPool.Config(), "focus_complete", focus, trace.timingEnabled)
		return
	}
	// Each layout receives one add, rename and deletion. Existing folders remain
	// populated, so expected item counts stay constant and every old survivor ID
	// can be compared independently of the scan's progress counters.
	wantChanges = make(map[string]CatalogChange)
	removedIDs := make(map[string]bool)
	renamedPaths := make(map[string]string)
	for index := range corpora {
		corpus := &corpora[index]
		oldPath := corpus.paths[0]
		renamed := scanPerformanceFindPath(t, initial, oldPath)
		newPath := strings.TrimSuffix(oldPath, filepath.Ext(oldPath)) + ".Renamed.mp4"
		if err := os.Rename(oldPath, newPath); err != nil {
			t.Fatalf("rename performance fixture: %v", err)
		}
		renamedPaths[renamed.id] = newPath
		removed := scanPerformanceFindPath(t, initial, corpus.paths[1])
		if err := os.Remove(corpus.paths[1]); err != nil {
			t.Fatalf("remove performance fixture: %v", err)
		}
		removedIDs[removed.id] = true
		scanPerformanceAddChange(t, wantChanges, renamed.change(CatalogUpdated))
		scanPerformanceAddChange(t, wantChanges, removed.change(CatalogRemoved))
		corpus.paths[0] = newPath
		corpus.paths[1] = libraryIntegrationFile(t, root, corpus.addedRelative, "video:incremental-"+corpus.label)
	}
	for _, corpus := range corpora {
		scan("incremental", corpus, false, 1, 1, 1)
	}
	incremental := scanPerformanceReadCatalog(t, ctx, fixturePool)
	scanPerformanceAssertCounts(t, incremental, corpora)
	for id, before := range initial {
		after, exists := incremental[id]
		if removedIDs[id] {
			if exists {
				t.Fatal("complete scan retained a deleted media ID")
			}
			continue
		}
		if !exists {
			t.Fatalf("incremental scan lost survivor ID %s", id)
		}
		if path, renamed := renamedPaths[id]; renamed {
			if after.path != path || after.parentID != before.parentID || after.kind != before.kind || after.media != before.media ||
				after.index != before.index || after.parentIndex != before.parentIndex {
				t.Fatal("rename changed media identity, numbering, hierarchy or cached facts")
			}
		} else if after != before {
			t.Fatal("incremental scan changed an unrelated catalog survivor")
		}
	}
	for _, corpus := range corpora {
		added := scanPerformanceFindPath(t, incremental, corpus.paths[1])
		if _, exists := initial[added.id]; exists {
			t.Fatal("new media reused an existing or deleted catalog ID")
		}
		scanPerformanceAddChange(t, wantChanges, added.change(CatalogAdded))
	}
	scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), wantChanges)
	assertUserData()
	for _, corpus := range corpora {
		scan("cached_after_incremental", corpus, false, 0, 0, 0)
	}
	scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), nil)
	assertUserData()
	for _, corpus := range corpora {
		scan("force_probe", corpus, true, 0, len(corpus.paths), int64(len(corpus.paths)))
	}
	forced := scanPerformanceReadCatalog(t, ctx, fixturePool)
	if !reflect.DeepEqual(forced, incremental) {
		t.Fatal("forced refresh changed catalog IDs, hierarchy or identical accepted media")
	}
	wantChanges = make(map[string]CatalogChange)
	for _, record := range forced {
		if !record.folder {
			scanPerformanceAddChange(t, wantChanges, record.change(CatalogUpdated))
		}
	}
	scanPerformanceAssertChanges(t, notifications, notificationOverflow.Load(), wantChanges)
	assertUserData()
	if probes.Load() != 803 {
		t.Fatalf("profile probe calls=%d, want 400 cold + 3 added + 400 forced", probes.Load())
	}
	for _, corpus := range corpora {
		kind := "Episode"
		if corpus.collectionType == "movies" {
			kind = "Movie"
		}
		items := libraryIntegrationQuery(t, observerCtx, store, Query{UserID: userID, ParentID: corpus.library.ID,
			Recursive: true, IncludeItemTypes: []string{kind}, Limit: len(corpus.paths) + 1})
		if items.TotalRecordCount != len(corpus.paths) || len(items.Items) != len(corpus.paths) {
			t.Fatalf("public catalog count differs for %s: total=%d returned=%d", corpus.label, items.TotalRecordCount, len(items.Items))
		}
		for _, item := range items.Items {
			record, exists := forced[item.ID]
			if !exists || item.Path != record.path || item.ParentID != record.parentID || item.Media == nil {
				t.Fatal("public projection lost accepted media or hierarchy")
			}
		}
	}
	scanPerformanceLogConnectionPolicy(t, observerCtx, store, tracedPool.Config(), "complete", focus, trace.timingEnabled)
}

func scanPerformanceLogConnectionPolicy(t *testing.T, ctx context.Context, store *Store, config *pgxpool.Config, phase string, focus, timing bool) {
	t.Helper()
	policy := struct {
		Phase              string `json:"phase"`
		QueryExecMode      string `json:"query_exec_mode"`
		QueryExecModeValue int    `json:"query_exec_mode_value"`
		JIT                string `json:"jit"`
		PlanCacheMode      string `json:"plan_cache_mode"`
		PreparedStatements int64  `json:"prepared_statement_count"`
		GenericPlans       int64  `json:"generic_plan_count"`
		CustomPlans        int64  `json:"custom_plan_count"`
		Focus              bool   `json:"focus"`
		SQLTiming          bool   `json:"sql_timing"`
	}{Phase: phase, QueryExecMode: fmt.Sprint(config.ConnConfig.DefaultQueryExecMode),
		QueryExecModeValue: int(config.ConnConfig.DefaultQueryExecMode), Focus: focus, SQLTiming: timing}
	if err := store.lockOwnedSession(ctx); err != nil {
		t.Fatal(err)
	}
	err := func() error {
		defer store.ownership.mu.Unlock()
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		err := store.ownership.conn.QueryRow(readCtx, `SELECT current_setting('jit'),current_setting('plan_cache_mode'),
			count(*),COALESCE(sum(generic_plans),0)::bigint,COALESCE(sum(custom_plans),0)::bigint
			FROM pg_prepared_statements`).Scan(&policy.JIT, &policy.PlanCacheMode, &policy.PreparedStatements,
			&policy.GenericPlans, &policy.CustomPlans)
		return store.ownershipErrorLocked(err)
	}()
	if err != nil {
		t.Fatalf("observe scan owner connection policy: %v", err)
	}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	// This observation uses the ignored context and executes outside workload
	// timing/reset boundaries. Plan counts cover the exact owner's whole session,
	// including the policy query itself, rather than a specific checkpoint SQL.
	t.Logf("scan_performance_connection=%s", data)
}

func scanPerformanceWALPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var position string
	if err := pool.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&position); err != nil {
		t.Fatalf("read scan WAL position: %v", err)
	}
	return position
}

func scanPerformanceReadCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]scanPerformanceRecord {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id, library_id, COALESCE(parent_id,''), type, path, relative_path,
		name, COALESCE(media::text,''), is_folder, index_number, parent_index_number FROM items`)
	if err != nil {
		t.Fatalf("read performance catalog: %v", err)
	}
	defer rows.Close()
	result := make(map[string]scanPerformanceRecord)
	for rows.Next() {
		var record scanPerformanceRecord
		if err := rows.Scan(&record.id, &record.libraryID, &record.parentID, &record.kind, &record.path, &record.relative,
			&record.name, &record.media, &record.folder, &record.index, &record.parentIndex); err != nil {
			t.Fatal(err)
		}
		result[record.id] = record
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func scanPerformanceAssertCounts(t *testing.T, records map[string]scanPerformanceRecord, corpora []scanPerformanceCorpus) {
	t.Helper()
	counts := make(map[string]map[string]int)
	for _, record := range records {
		if counts[record.libraryID] == nil {
			counts[record.libraryID] = make(map[string]int)
		}
		counts[record.libraryID][record.kind]++
		if record.kind != "CollectionFolder" {
			parent, exists := records[record.parentID]
			if !exists || !parent.folder || parent.libraryID != record.libraryID {
				t.Fatal("performance catalog has a missing or foreign parent")
			}
			if record.kind == "Episode" && parent.kind != "Season" || record.kind == "Season" && parent.kind != "Series" {
				t.Fatal("performance catalog lost its television hierarchy")
			}
		}
	}
	if len(counts) != len(corpora) {
		t.Fatal("performance catalog contains unexpected libraries")
	}
	for _, corpus := range corpora {
		if !reflect.DeepEqual(counts[corpus.library.ID], corpus.wantCounts) {
			t.Fatalf("%s catalog counts=%v want=%v", corpus.label, counts[corpus.library.ID], corpus.wantCounts)
		}
		for _, path := range corpus.paths {
			if scanPerformanceFindPath(t, records, path).libraryID != corpus.library.ID {
				t.Fatal("a corpus pathname belongs to another library")
			}
		}
	}
}

func scanPerformanceFindPath(t *testing.T, records map[string]scanPerformanceRecord, path string) scanPerformanceRecord {
	t.Helper()
	for _, record := range records {
		if record.path == path {
			return record
		}
	}
	t.Fatalf("performance catalog omitted path %q", path)
	return scanPerformanceRecord{}
}

func scanPerformanceFindRelative(t *testing.T, records map[string]scanPerformanceRecord, libraryID, relative string) scanPerformanceRecord {
	t.Helper()
	for _, record := range records {
		if record.libraryID == libraryID && record.relative == relative {
			return record
		}
	}
	t.Fatalf("performance catalog omitted relative path %q", relative)
	return scanPerformanceRecord{}
}

func scanPerformanceAddChange(t *testing.T, changes map[string]CatalogChange, change CatalogChange) {
	t.Helper()
	key := fmt.Sprintf("%d:%s", change.Kind, change.ItemID)
	if _, exists := changes[key]; exists {
		t.Fatalf("duplicate catalog fact for %s", key)
	}
	changes[key] = change
}

func scanPerformanceAssertChanges(t *testing.T, notifications <-chan CatalogNotification, overflow bool, want map[string]CatalogChange) {
	t.Helper()
	if overflow {
		t.Fatal("performance notification queue exceeded its bound")
	}
	got := make(map[string]CatalogChange)
	for {
		select {
		case notification := <-notifications:
			if notification.Resync {
				t.Fatal("bounded performance corpus unexpectedly required catalog resynchronization")
			}
			for _, change := range notification.Changes {
				scanPerformanceAddChange(t, got, change)
			}
		default:
			if len(got) != len(want) {
				t.Fatalf("performance catalog events=%d want=%d", len(got), len(want))
			}
			for key, change := range got {
				if expected, exists := want[key]; !exists || change != expected {
					t.Fatalf("performance catalog event=%+v want=%+v", change, expected)
				}
			}
			return
		}
	}
}

func scanPerformanceUserData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, wantCount int) string {
	t.Helper()
	var count int
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(jsonb_agg(to_jsonb(d) ||
		jsonb_build_object('rowVersion', d.xmin::text) ORDER BY d.user_id,d.item_id),'[]'::jsonb)::text
		FROM user_item_data d`).Scan(&count, &snapshot); err != nil || count != wantCount {
		t.Fatalf("read performance UserData: count=%d want=%d error=%v", count, wantCount, err)
	}
	return snapshot
}
