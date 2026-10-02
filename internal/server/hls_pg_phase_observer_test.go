//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const hlsPGObserverInterval = 10 * time.Millisecond
const hlsPGObserverQueryDeadline = 50 * time.Millisecond
const hlsPGObserverRowLimit = 100000
const hlsPGObserverApplicationPIDLimit = 16

type hlsPGObserverSpanContextKey struct{}

type hlsPGCommitSpan struct {
	ID                int64     `json:"span_id"`
	PID               uint32    `json:"backend_pid"`
	Group             string    `json:"request_group"`
	Ordinal           int64     `json:"request_ordinal"`
	Role              string    `json:"transaction_role"`
	StartedAt         time.Time `json:"commit_started_at"`
	EndedAt           time.Time `json:"commit_ended_at,omitempty"`
	ValidSamples      int64     `json:"valid_samples"`
	ConsistentSamples int64     `json:"time_consistent_client_samples"`
	observer          *hlsPGPhaseObserver
	requestStarted    time.Time
}

type hlsPGObserverTransaction struct {
	requestStarted time.Time
	role           string
}

type hlsPGObserverPoint struct {
	SpanID               int64     `json:"span_id"`
	PID                  uint32    `json:"backend_pid"`
	Group                string    `json:"request_group"`
	Ordinal              int64     `json:"request_ordinal"`
	Role                 string    `json:"transaction_role"`
	ObserverStarted      time.Time `json:"observer_query_started_at"`
	ObserverEnded        time.Time `json:"observer_query_ended_at"`
	ServerObserved       time.Time `json:"server_observed_at"`
	BackendStarted       time.Time `json:"backend_started_at"`
	QueryStarted         time.Time `json:"backend_query_started_at,omitempty"`
	CommandClass         string    `json:"backend_command_class"`
	State                string    `json:"state"`
	WaitType             string    `json:"wait_event_type,omitempty"`
	WaitEvent            string    `json:"wait_event,omitempty"`
	BlockingPIDs         []int32   `json:"blocking_pids,omitempty"`
	ValidQuerySpan       bool      `json:"valid_query_span"`
	ClientSpanConsistent bool      `json:"time_consistent_client_span"`
	InvalidReason        string    `json:"invalid_reason,omitempty"`
}

type hlsPGObserverSnapshot struct {
	ObservedAt time.Time              `json:"observed_at"`
	View       string                 `json:"view"`
	Reset      json.RawMessage        `json:"stats_reset"`
	Counters   map[string]json.Number `json:"counters"`
	Available  bool                   `json:"available"`
}

type hlsPGPhaseObserver struct {
	mu           sync.Mutex
	sequence     atomic.Int64
	connection   *pgx.Conn
	ctx          context.Context
	trace        *hlsPriorityProfileTracer
	stop         chan struct{}
	done         chan struct{}
	finishOnce   sync.Once
	stopping     bool
	database     string
	searchPath   string
	username     string
	clockMin     time.Duration
	clockMax     time.Duration
	active       map[uint32]*hlsPGCommitSpan
	transactions map[uint32]hlsPGObserverTransaction
	generations  map[uint32]time.Time
	known        map[uint32]bool
	counts       map[string]int64
	roleCounts   map[string]map[string]int64
	waits        map[string]int64
	points       []hlsPGObserverPoint
	metadata     map[string]any
	before       []hlsPGObserverSnapshot
	exportFile   string
	dimensions   map[string]any
}

func hlsPGObserverFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hlsPGObserverErrorKind(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		return "postgres_error"
	}
	return "connection_or_query_error"
}

func hlsPGObserverSafeLabel(value string) string {
	if len(value) == 0 || len(value) > 64 {
		return "other"
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == ' ') {
			return "other"
		}
	}
	return value
}

func hlsPGObserverValidRunID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func hlsStartPGPhaseObserver(t *testing.T, ctx context.Context, pool *pgxpool.Pool, trace *hlsPriorityProfileTracer, dimensions map[string]any) *hlsPGPhaseObserver {
	t.Helper()
	if os.Getenv("GOBY_HLS_PG_WAIT_SAMPLING") != "1" {
		return nil
	}
	if !hlsPhaseTimingEnabled() {
		t.Fatal("PostgreSQL wait sampling requires GOBY_HLS_PHASE_TIMING=1")
	}
	exportRoot, runID := os.Getenv("GOBY_HLS_PG_OBSERVER_EXPORT_ROOT"), os.Getenv("GOBY_HLS_PG_OBSERVER_RUN_ID")
	info, err := os.Lstat(exportRoot)
	if err != nil || !filepath.IsAbs(exportRoot) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		t.Fatal("the PostgreSQL observer export root must be an existing private absolute directory")
	}
	if !hlsPGObserverValidRunID(runID) {
		t.Fatal("the PostgreSQL observer run ID must contain bounded ASCII letters, digits, hyphens or underscores")
	}
	file, err := os.OpenFile(filepath.Join(exportRoot, "pg_observer_"+runID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("reserve a new PostgreSQL observer evidence file")
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal("close the reserved PostgreSQL observer evidence file")
	}
	configuration := pool.Config().ConnConfig.Copy()
	configuration.Tracer = nil
	configuration.RuntimeParams["application_name"] = "goby_hls_phase_observer"
	searchPath := configuration.RuntimeParams["search_path"]
	if searchPath == "" || searchPath == "public" {
		t.Fatal("the PostgreSQL observer requires the isolated fixture search path")
	}
	openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	openedAt := time.Now()
	connection, err := pgx.ConnectConfig(openCtx, configuration)
	if err != nil {
		t.Fatal("open the single independent PostgreSQL observer connection")
	}
	observer := &hlsPGPhaseObserver{connection: connection, ctx: ctx, trace: trace, stop: make(chan struct{}), done: make(chan struct{}),
		database: configuration.Database, username: configuration.User, searchPath: searchPath,
		active: make(map[uint32]*hlsPGCommitSpan), transactions: make(map[uint32]hlsPGObserverTransaction),
		generations: make(map[uint32]time.Time), known: make(map[uint32]bool), counts: make(map[string]int64),
		roleCounts: make(map[string]map[string]int64), waits: make(map[string]int64), metadata: make(map[string]any),
		exportFile: path, dimensions: dimensions}
	t.Cleanup(func() { observer.finish(t) })
	observer.counts["observer_connections_opened"] = 1
	observer.counts["connection_open_elapsed_ns"] = time.Since(openedAt).Nanoseconds()
	var schema, observedPath, database string
	var observedAt time.Time
	var version int
	var walTiming, ioTiming pgtype.Text
	var synchronousCommit string
	started := time.Now()
	metadataCtx, metadataCancel := context.WithTimeout(ctx, hlsPGObserverQueryDeadline)
	err = connection.QueryRow(metadataCtx, `SELECT current_schema(),current_setting('search_path'),current_database(),clock_timestamp(),
		current_setting('server_version_num')::integer,current_setting('track_wal_io_timing',true),current_setting('track_io_timing',true),
		current_setting('synchronous_commit')`).Scan(&schema, &observedPath, &database, &observedAt, &version, &walTiming, &ioTiming, &synchronousCommit)
	ended := time.Now()
	metadataCancel()
	observer.counts["metadata_queries"]++
	observer.counts["metadata_query_elapsed_ns"] += ended.Sub(started).Nanoseconds()
	if err != nil || schema == "public" || schema == "" || observedPath != searchPath || database != configuration.Database {
		observer.metadata["initialization_failed"] = true
		observer.finish(t)
		t.Fatal("the independent PostgreSQL observer did not bind the exact fixture schema and database")
	}
	observer.clockMin, observer.clockMax = observedAt.Sub(ended), observedAt.Sub(started)
	observer.metadata["schema_fingerprint"] = hlsPGObserverFingerprint(searchPath)
	observer.metadata["database_fingerprint"] = hlsPGObserverFingerprint(database)
	observer.metadata["server_version_num"] = version
	observer.metadata["track_wal_io_timing"] = hlsPGObserverSafeLabel(walTiming.String)
	observer.metadata["track_io_timing"] = hlsPGObserverSafeLabel(ioTiming.String)
	observer.metadata["synchronous_commit"] = hlsPGObserverSafeLabel(synchronousCommit)
	observer.metadata["clock_offset_min_ns"] = observer.clockMin.Nanoseconds()
	observer.metadata["clock_offset_max_ns"] = observer.clockMax.Nanoseconds()
	observer.metadata["observer_backend_pid"] = connection.PgConn().PID()
	observer.metadata["observer_connection_budget"] = 1
	observer.metadata["application_pool_budget_unchanged"] = 16
	observer.metadata["lease_configuration_unchanged"] = true
	observer.metadata["schema_filter_contract"] = "Application connection database/user/search_path configuration matches the isolated fixture; the observer verifies its own current_schema/current_setting. Other backends' current_schema is not inferred from pg_stat_activity."
	observer.before = observer.snapshots()
	trace.pgObserver.Store(observer)
	go observer.run()
	return observer
}

func (observer *hlsPGPhaseObserver) queryStart(ctx context.Context, connection *pgx.Conn, sql string) context.Context {
	request := hlsPhaseTimingFor(ctx)
	if request == nil {
		return ctx
	}
	pid := connection.PgConn().PID()
	category := hlsPhaseTimingCategory(sql)
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.stopping {
		return ctx
	}
	valid, known := observer.known[pid]
	if !known {
		if len(observer.known) >= hlsPGObserverApplicationPIDLimit {
			observer.counts["rejected_schema_or_pid_budget"]++
			return ctx
		}
		configuration := connection.Config()
		valid = configuration.Database == observer.database && configuration.User == observer.username && configuration.RuntimeParams["search_path"] == observer.searchPath
		observer.known[pid] = valid
	}
	if !valid || len(observer.known) > hlsPGObserverApplicationPIDLimit {
		observer.counts["rejected_schema_or_pid_budget"]++
		return ctx
	}
	transaction := observer.transactions[pid]
	if category == "transaction_begin" || transaction.requestStarted != request.started {
		transaction = hlsPGObserverTransaction{requestStarted: request.started, role: "other_transaction"}
	}
	switch category {
	case "playback_context_restore":
		transaction.role = "context_restore_transaction"
	case "client_activity_predicates":
		transaction.role = "steady_client_touch_transaction"
	case "client_write", "credential_write", "key_write", "device_write":
		transaction.role = "legacy_or_due_activity_transaction"
	case "source_projection":
		if transaction.role == "other_transaction" {
			transaction.role = "legacy_source_transaction"
		}
	case "play_final_clock":
		if transaction.role == "other_transaction" {
			transaction.role = "legacy_play_validation_transaction"
		}
	}
	observer.transactions[pid] = transaction
	if category == "transaction_rollback" {
		delete(observer.transactions, pid)
	}
	if category != "transaction_commit" {
		return ctx
	}
	span := &hlsPGCommitSpan{ID: observer.sequence.Add(1), PID: pid, Group: request.group, Ordinal: request.summary.Ordinal,
		Role: transaction.role, StartedAt: time.Now(), observer: observer, requestStarted: request.started}
	observer.active[pid] = span
	observer.counts["commit_spans_started"]++
	if int64(len(observer.active)) > observer.counts["maximum_active_commit_spans"] {
		observer.counts["maximum_active_commit_spans"] = int64(len(observer.active))
	}
	return context.WithValue(ctx, hlsPGObserverSpanContextKey{}, span)
}

func (observer *hlsPGPhaseObserver) batchStart(ctx context.Context, connection *pgx.Conn, batch *pgx.Batch) {
	request := hlsPhaseTimingFor(ctx)
	if request == nil || len(batch.QueuedQueries) == 0 {
		return
	}
	category := hlsPhaseTimingCategory(batch.QueuedQueries[0].SQL)
	if category != "account_shared_lock" && category != "credential_shared_lock" {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	pid := connection.PgConn().PID()
	if observer.stopping {
		return
	}
	// BEGIN's QueryTracer validates this connection before an authority batch.
	// A rejected/rebuilt PID must not bypass the same budget through BatchTracer.
	if !observer.known[pid] {
		observer.counts["rejected_batch_pid"]++
		return
	}
	observer.transactions[pid] = hlsPGObserverTransaction{requestStarted: request.started, role: "fresh_authority_transaction"}
}

func hlsPGObserverQueryEnd(ctx context.Context, failed bool) {
	span, _ := ctx.Value(hlsPGObserverSpanContextKey{}).(*hlsPGCommitSpan)
	if span == nil {
		return
	}
	observer := span.observer
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if !span.EndedAt.IsZero() {
		return
	}
	span.EndedAt = time.Now()
	if observer.active[span.PID] == span {
		delete(observer.active, span.PID)
		delete(observer.transactions, span.PID)
	}
	observer.counts["commit_spans_ended"]++
	if failed {
		observer.counts["commit_span_errors"]++
	}
	role := observer.roleCounts[span.Role]
	if role == nil {
		role = make(map[string]int64)
		observer.roleCounts[span.Role] = role
	}
	elapsed := span.EndedAt.Sub(span.StartedAt).Nanoseconds()
	role["count"]++
	role["elapsed_ns"] += elapsed
	if elapsed > role["maximum_ns"] {
		role["maximum_ns"] = elapsed
	}
	if span.ValidSamples > 0 {
		role["spans_with_valid_samples"]++
	}
	if span.ConsistentSamples > 0 {
		role["spans_with_time_consistent_client_samples"]++
	}
	if elapsed >= hlsPGObserverInterval.Nanoseconds() {
		role["spans_at_least_interval"]++
		if span.ValidSamples > 0 {
			role["long_spans_with_valid_samples"]++
		}
		if span.ConsistentSamples > 0 {
			role["long_spans_with_time_consistent_client_samples"]++
		}
	}
}

func (observer *hlsPGPhaseObserver) run() {
	defer close(observer.done)
	ticker := time.NewTicker(hlsPGObserverInterval)
	defer ticker.Stop()
	var previous time.Time
	for {
		select {
		case <-observer.stop:
			return
		case <-observer.ctx.Done():
			return
		case <-ticker.C:
			started := time.Now()
			observer.mu.Lock()
			if !previous.IsZero() && started.Sub(previous).Nanoseconds() > observer.counts["maximum_tick_interval_ns"] {
				observer.counts["maximum_tick_interval_ns"] = started.Sub(previous).Nanoseconds()
			}
			previous = started
			observer.counts["ticks"]++
			spans := make(map[uint32]*hlsPGCommitSpan, len(observer.active))
			pids := make([]int32, 0, len(observer.active))
			for pid, span := range observer.active {
				spans[pid], pids = span, append(pids, int32(pid))
			}
			if len(pids) == 0 {
				observer.counts["ticks_without_active_commit"]++
			}
			observer.mu.Unlock()
			if len(pids) > 0 {
				observer.sample(started, spans, pids)
			}
			if observer.connection.IsClosed() {
				observer.mu.Lock()
				observer.counts["connection_closed_during_sampling"]++
				observer.mu.Unlock()
				return
			}
		}
	}
}

func (observer *hlsPGPhaseObserver) sample(started time.Time, spans map[uint32]*hlsPGCommitSpan, pids []int32) {
	queryCtx, cancel := context.WithTimeout(observer.ctx, hlsPGObserverQueryDeadline)
	defer cancel()
	rows, err := observer.connection.Query(queryCtx, `SELECT pid,state,wait_event_type,wait_event,backend_start,query_start,clock_timestamp(),
		CASE WHEN query IS NULL THEN 'not_visible' WHEN lower(btrim(query,E' ;\t\r\n'))='commit' THEN 'commit' ELSE 'other' END,
		CASE WHEN wait_event_type='Lock' THEN pg_blocking_pids(pid) ELSE ARRAY[]::integer[] END
		FROM pg_catalog.pg_stat_activity WHERE pid=ANY($1::integer[]) AND datname=$2
		AND usesysid=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=current_user) AND backend_type='client backend'`, pids, observer.database)
	observer.mu.Lock()
	observer.counts["sampling_queries"]++
	observer.mu.Unlock()
	if err != nil {
		observer.mu.Lock()
		observer.counts["sampling_error_"+hlsPGObserverErrorKind(err)]++
		elapsed := time.Since(started).Nanoseconds()
		observer.counts["sampling_query_elapsed_ns"] += elapsed
		if elapsed > observer.counts["maximum_sampling_query_elapsed_ns"] {
			observer.counts["maximum_sampling_query_elapsed_ns"] = elapsed
		}
		observer.mu.Unlock()
		return
	}
	for rows.Next() {
		var pid uint32
		var state, commandClass string
		var waitType, waitEvent pgtype.Text
		var backendStarted, serverObserved time.Time
		var queryStarted pgtype.Timestamptz
		var blockers []int32
		if err = rows.Scan(&pid, &state, &waitType, &waitEvent, &backendStarted, &queryStarted, &serverObserved, &commandClass, &blockers); err != nil {
			break
		}
		span := spans[pid]
		if span == nil {
			continue
		}
		point := hlsPGObserverPoint{SpanID: span.ID, PID: pid, Group: span.Group, Ordinal: span.Ordinal, Role: span.Role,
			ObserverStarted: started.UTC(), ServerObserved: serverObserved.UTC(), BackendStarted: backendStarted.UTC(),
			State: hlsPGObserverSafeLabel(state), CommandClass: commandClass, BlockingPIDs: blockers}
		if waitType.Valid {
			point.WaitType = hlsPGObserverSafeLabel(waitType.String)
		}
		if waitEvent.Valid {
			point.WaitEvent = hlsPGObserverSafeLabel(waitEvent.String)
		}
		if queryStarted.Valid {
			point.QueryStarted = queryStarted.Time.UTC()
		}
		observer.mu.Lock()
		observer.counts["activity_rows"]++
		generation, known := observer.generations[pid]
		if !known {
			observer.generations[pid] = backendStarted
		}
		switch {
		case known && !generation.Equal(backendStarted):
			point.InvalidReason = "backend_generation_changed"
		case commandClass != "commit":
			point.InvalidReason = "backend_command_not_commit"
		case observer.active[pid] != span:
			point.InvalidReason = "span_ended_or_replaced_before_row_consumption"
		case !queryStarted.Valid:
			point.InvalidReason = "missing_query_start"
		case queryStarted.Time.Add(-observer.clockMin).Before(span.StartedAt.Add(-time.Millisecond)):
			point.InvalidReason = "query_start_before_commit_span"
		case queryStarted.Time.Add(-observer.clockMax).After(time.Now().Add(time.Millisecond)):
			point.InvalidReason = "query_start_after_observation"
		default:
			point.ClientSpanConsistent = true
			span.ConsistentSamples++
			observer.counts["time_consistent_client_span_rows"]++
			bucket := "client_span_backend_not_active"
			if state == "active" {
				point.ValidQuerySpan = true
				span.ValidSamples++
				observer.counts["valid_query_span_rows"]++
				bucket = "active_server_commit"
			} else {
				point.InvalidReason = "backend_not_active"
				observer.counts["client_commit_span_backend_not_active_rows"]++
			}
			wait := bucket + ":" + point.State + ":" + point.WaitType + ":" + point.WaitEvent
			if _, exists := observer.waits[wait]; !exists && len(observer.waits) >= 127 {
				wait = "other_wait_bucket"
			}
			observer.waits[wait]++
		}
		if !point.ValidQuerySpan {
			observer.counts["invalid_"+point.InvalidReason]++
		}
		point.ObserverEnded = time.Now().UTC()
		if len(observer.points) < hlsPGObserverRowLimit {
			observer.points = append(observer.points, point)
		} else {
			observer.counts["dropped_activity_rows"]++
		}
		observer.mu.Unlock()
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	observer.mu.Lock()
	observer.counts["sampling_query_elapsed_ns"] += time.Since(started).Nanoseconds()
	if elapsed := time.Since(started).Nanoseconds(); elapsed > observer.counts["maximum_sampling_query_elapsed_ns"] {
		observer.counts["maximum_sampling_query_elapsed_ns"] = elapsed
	}
	if err != nil {
		observer.counts["sampling_error_"+hlsPGObserverErrorKind(err)]++
	} else {
		observer.counts["successful_sampling_queries"]++
	}
	observer.mu.Unlock()
}

func (observer *hlsPGPhaseObserver) snapshots() []hlsPGObserverSnapshot {
	var snapshots []hlsPGObserverSnapshot
	for _, view := range []string{"pg_stat_wal", "pg_stat_checkpointer", "pg_stat_bgwriter"} {
		started := time.Now()
		snapshot := hlsPGObserverSnapshot{ObservedAt: time.Now().UTC(), View: view, Counters: make(map[string]json.Number)}
		ctx, cancel := context.WithTimeout(observer.ctx, hlsPGObserverQueryDeadline)
		var available bool
		err := observer.connection.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", "pg_catalog."+view).Scan(&available)
		observer.mu.Lock()
		observer.counts["metadata_queries"]++
		observer.mu.Unlock()
		if err == nil && available {
			var raw []byte
			err = observer.connection.QueryRow(ctx, "SELECT to_jsonb(s) FROM pg_catalog."+view+" s").Scan(&raw)
			observer.mu.Lock()
			observer.counts["metadata_queries"]++
			observer.mu.Unlock()
			if err == nil {
				fields := make(map[string]any)
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				if err = decoder.Decode(&fields); err == nil {
					snapshot.Available = true
					for name, value := range fields {
						if name == "stats_reset" {
							snapshot.Reset, _ = json.Marshal(value)
						} else if number, ok := value.(json.Number); ok {
							snapshot.Counters[name] = number
						}
					}
				}
			}
		}
		cancel()
		observer.mu.Lock()
		observer.counts["metadata_query_elapsed_ns"] += time.Since(started).Nanoseconds()
		if err != nil {
			observer.counts["metadata_error_"+hlsPGObserverErrorKind(err)]++
		}
		observer.mu.Unlock()
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func (observer *hlsPGPhaseObserver) finish(t *testing.T) {
	t.Helper()
	if observer == nil {
		return
	}
	observer.finishOnce.Do(func() {
		observer.trace.pgObserver.CompareAndSwap(observer, nil)
		observer.mu.Lock()
		observer.stopping = true
		observer.counts["active_commit_spans_at_stop"] = int64(len(observer.active))
		observer.mu.Unlock()
		// Initialization failure has no sampling goroutine to join.
		if observer.metadata["initialization_failed"] != true {
			close(observer.stop)
			<-observer.done
		}
		after := observer.snapshots()
		deltas := make(map[string]any)
		for index, before := range observer.before {
			current := after[index]
			valid := before.Available && current.Available && len(before.Reset) > 0 && bytes.Equal(before.Reset, current.Reset)
			counters := make(map[string]float64)
			for name, beforeValue := range before.Counters {
				a, errA := strconv.ParseFloat(beforeValue.String(), 64)
				b, errB := strconv.ParseFloat(current.Counters[name].String(), 64)
				if errA != nil || errB != nil || b < a {
					valid = false
				} else {
					counters[name] = b - a
				}
			}
			deltas[before.View] = map[string]any{"valid_reset_and_monotonic_counters": valid, "server_global_counter_deltas": counters}
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		closeStarted := time.Now()
		closeErr := observer.connection.Close(closeCtx)
		cancel()
		observer.mu.Lock()
		if observer.connection.IsClosed() {
			observer.counts["observer_connections_closed"] = 1
		}
		observer.counts["connection_close_elapsed_ns"] = time.Since(closeStarted).Nanoseconds()
		if closeErr != nil {
			observer.counts["connection_close_errors"]++
		}
		valid := observer.metadata["initialization_failed"] != true && observer.counts["observer_connections_opened"] == 1 && observer.counts["observer_connections_closed"] == 1 &&
			observer.counts["rejected_schema_or_pid_budget"] == 0 && observer.counts["rejected_batch_pid"] == 0 && observer.counts["invalid_backend_generation_changed"] == 0 &&
			observer.counts["active_commit_spans_at_stop"] == 0 && observer.counts["commit_span_errors"] == 0 &&
			observer.counts["commit_spans_started"] == observer.counts["commit_spans_ended"] && observer.counts["connection_closed_during_sampling"] == 0 &&
			observer.counts["sampling_error_deadline"] == 0 && observer.counts["sampling_error_canceled"] == 0 && observer.counts["sampling_error_postgres_error"] == 0 && observer.counts["sampling_error_connection_or_query_error"] == 0 && closeErr == nil
		encoded, err := json.Marshal(map[string]any{
			"schema_version": 1, "measurement_kind": "pg_commit_wait_diagnostic_only", "dimensions": observer.dimensions,
			"metadata": observer.metadata, "interval_ns": hlsPGObserverInterval.Nanoseconds(), "query_deadline_ns": hlsPGObserverQueryDeadline.Nanoseconds(),
			"counts": observer.counts, "commit_roles": observer.roleCounts, "valid_query_span_waits": observer.waits, "observations": observer.points,
			"validity": map[string]any{"complete_span_and_connection_accounting": valid, "has_valid_query_span_observations": observer.counts["valid_query_span_rows"] > 0,
				"has_time_consistent_client_span_observations": observer.counts["time_consistent_client_span_rows"] > 0,
				"no_observation_row_truncation":                observer.counts["dropped_activity_rows"] == 0},
			"before_server_global_statistics": observer.before, "after_server_global_statistics": after, "server_global_deltas": deltas,
			"interpretation": "One observer connection is evidence overhead and never borrows application or lease capacity. Schema/database/user-bound application PIDs and backend generations fence observations. The server returns only a commit/other/not_visible command enum; SQL text is not returned. Client spans and time consistency are checked at row consumption; short or completed spans can be rejected. valid_query_span additionally requires an active server COMMIT. Time-consistent client spans with a non-active backend are retained separately, never counted as active server COMMIT waits. Timing includes observation cost. Null waits and active/idle states do not independently prove CPU, scheduler, WAL or row-lock causes. Server-global deltas require unchanged reset markers and monotonic counters and are not per-request attribution.",
		})
		observer.mu.Unlock()
		if err != nil || os.WriteFile(observer.exportFile, append(encoded, '\n'), 0600) != nil {
			t.Error("write bounded PostgreSQL observer evidence")
		}
		t.Logf("hls_pg_phase_observer_file=%s", filepath.Base(observer.exportFile))
	})
}
