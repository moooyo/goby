//go:build linux

package library

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const preparedPlanMemoryOutputLimit = 64 << 20

const preparedPlanMemoryBackendExistsSQL = `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
	WHERE pid=$1::integer AND backend_start=$2::timestamptz)`

type preparedPlanMemoryOutput struct {
	file    *os.File
	bytes   int64
	group   string
	ordinal int
	encoder *json.Encoder
}

func (output *preparedPlanMemoryOutput) Write(data []byte) (int, error) {
	if output.bytes+int64(len(data)) > preparedPlanMemoryOutputLimit {
		return 0, errors.New("plan-memory evidence exceeded its fixed byte limit")
	}
	n, err := output.file.Write(data)
	output.bytes += int64(n)
	return n, err
}

func (output *preparedPlanMemoryOutput) emit(kind, phase string, record map[string]any) error {
	output.ordinal++
	record["schema_version"], record["group"] = 1, output.group
	record["kind"], record["phase"], record["record_ordinal"] = kind, phase, output.ordinal
	return output.encoder.Encode(record)
}

func (output *preparedPlanMemoryOutput) client(phase string) error {
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return output.emit("client_memory", phase, map[string]any{
		"heap_alloc": memory.HeapAlloc, "heap_objects": memory.HeapObjects,
		"heap_inuse": memory.HeapInuse, "heap_sys": memory.HeapSys, "heap_released": memory.HeapReleased,
		"total_alloc": memory.TotalAlloc, "mallocs": memory.Mallocs, "frees": memory.Frees,
		"num_gc": memory.NumGC, "pause_total_ns": memory.PauseTotalNs,
	})
}

type preparedPlanMemoryIdentity struct {
	role         string
	index        int
	pid          uint32
	backendStart string
}

type preparedPlanMemoryBackend struct {
	preparedPlanMemoryIdentity
	conn  *pgx.Conn
	lease *pgxpool.Conn
}

type preparedPlanMemoryCohort struct {
	backends []preparedPlanMemoryBackend
	data     *pgxpool.Pool
	control  *pgxpool.Pool
	closed   bool
}

// Stream one simple-query result without retaining historical rows or calling
// pgx's cache-draining Query/Exec wrappers. The observer replaces unnamed SQL.
func preparedPlanMemoryRawRows(ctx context.Context, conn *pgx.Conn, sql string, columns, limit int, consume func([][]byte) error) error {
	if conn.IsClosed() || conn.PgConn().IsBusy() || conn.PgConn().TxStatus() != 'I' {
		return errors.New("raw plan-memory observer requires an idle live backend")
	}
	results := conn.PgConn().Exec(ctx, sql)
	defer results.Close()
	sets, rows := 0, 0
	for results.NextResult() {
		sets++
		result := results.ResultReader()
		for result.NextRow() {
			rows++
			values := result.Values()
			if sets != 1 || rows > limit || len(values) != columns {
				return errors.New("raw plan-memory observer exceeded its fixed result contract")
			}
			if err := consume(values); err != nil {
				return err
			}
		}
		if _, err := result.Close(); err != nil {
			return err
		}
	}
	if err := results.Close(); err != nil {
		return err
	}
	if sets != 1 || conn.PgConn().TxStatus() != 'I' {
		return errors.New("raw plan-memory observer did not complete one idle result")
	}
	return nil
}

func preparedPlanMemoryIdentify(ctx context.Context, backend *preparedPlanMemoryBackend) error {
	rows := 0
	err := preparedPlanMemoryRawRows(ctx, backend.conn, `SELECT pg_backend_pid()::text,backend_start::text
		FROM pg_stat_activity WHERE pid=pg_backend_pid()`, 2, 1, func(values [][]byte) error {
		pid, err := strconv.ParseUint(string(values[0]), 10, 32)
		if err != nil {
			return err
		}
		backend.pid, backend.backendStart = uint32(pid), string(values[1])
		rows++
		return nil
	})
	if err == nil && (rows != 1 || backend.pid != backend.conn.PgConn().PID() || backend.backendStart == "") {
		return errors.New("target backend identity was not observed exactly once")
	}
	return err
}

func (cohort *preparedPlanMemoryCohort) close(ctx context.Context) error {
	if cohort.closed {
		return nil
	}
	var result error
	for index := range cohort.backends {
		backend := &cohort.backends[index]
		if backend.lease != nil {
			backend.lease.Release()
			backend.lease = nil
		} else if cohort.data == nil {
			result = errors.Join(result, backend.conn.Close(ctx))
		}
	}
	for _, pool := range []*pgxpool.Pool{cohort.control, cohort.data} {
		if pool == nil {
			continue
		}
		done := make(chan struct{})
		go func() { pool.Close(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
	}
	for _, backend := range cohort.backends {
		select {
		case <-backend.conn.PgConn().CleanupDone():
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
		if !backend.conn.IsClosed() {
			return errors.Join(result, errors.New("target connection remained live after Close"))
		}
	}
	cohort.closed = true
	return result
}

func preparedPlanMemoryOpen(ctx context.Context, input *preparedPlanMemoryInput, group string, capacity int) (*preparedPlanMemoryCohort, error) {
	cohort := &preparedPlanMemoryCohort{}
	configuration := input.configuration.Copy()
	configuration.StatementCacheCapacity = capacity
	configuration.DescriptionCacheCapacity = 512
	configuration.Tracer = nil
	configuration.RuntimeParams["application_name"] = "goby-plan-memory-" + group
	if group != "topology-16" {
		conn, err := pgx.ConnectConfig(ctx, configuration)
		if err != nil {
			return cohort, err
		}
		cohort.backends = append(cohort.backends, preparedPlanMemoryBackend{conn: conn,
			preparedPlanMemoryIdentity: preparedPlanMemoryIdentity{role: "single", index: 0}})
	} else {
		for _, role := range []struct {
			name  string
			count int32
		}{{"data", 12}, {"control", 4}} {
			config, err := pgxpool.ParseConfig(configuration.ConnString())
			if err != nil {
				return cohort, err
			}
			config.ConnConfig = configuration.Copy()
			config.ConnConfig.RuntimeParams["application_name"] += "-" + role.name
			config.MaxConns, config.MinConns, config.MinIdleConns = role.count, 0, 0
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				return cohort, err
			}
			if role.name == "data" {
				cohort.data = pool
			} else {
				cohort.control = pool
			}
			for index := range int(role.count) {
				lease, err := pool.Acquire(ctx)
				if err != nil {
					return cohort, err
				}
				name := role.name
				if role.name == "data" && index == 0 {
					name = "owner"
				}
				cohort.backends = append(cohort.backends, preparedPlanMemoryBackend{conn: lease.Conn(), lease: lease,
					preparedPlanMemoryIdentity: preparedPlanMemoryIdentity{role: name, index: index}})
			}
		}
	}
	for index := range cohort.backends {
		if err := preparedPlanMemoryIdentify(ctx, &cohort.backends[index]); err != nil {
			return cohort, err
		}
		for previous := range index {
			if cohort.backends[previous].pid == cohort.backends[index].pid {
				return cohort, errors.New("target roles did not receive distinct physical connections")
			}
		}
	}
	return cohort, nil
}

func preparedPlanMemoryHashRow(digest hash.Hash, values [][]byte) {
	var size [8]byte
	for _, value := range values {
		length := uint64(len(value))
		if value == nil {
			length = ^uint64(0)
		}
		binary.BigEndian.PutUint64(size[:], length)
		_, _ = digest.Write(size[:])
		_, _ = digest.Write(value)
	}
}

func preparedPlanMemoryReplay(ctx context.Context, conn *pgx.Conn, template preparedPlanMemoryTemplate, mode pgx.QueryExecMode) ([32]byte, int, error) {
	var result [32]byte
	args := make([]any, 1, len(template.args)+1)
	args[0] = mode
	args = append(args, template.args...)
	rows, err := conn.Query(ctx, template.sql, args...)
	if err != nil {
		return result, 0, err
	}
	defer rows.Close()
	digest, count := sha256.New(), 0
	for rows.Next() {
		count++
		if count > 1 {
			return result, count, errors.New("captured plan template returned more than one fixture row")
		}
		preparedPlanMemoryHashRow(digest, rows.RawValues())
	}
	rows.Close()
	copy(result[:], digest.Sum(nil))
	return result, count, rows.Err()
}

func preparedPlanMemoryFamily(input *preparedPlanMemoryInput, prefix string) string {
	if prefix == "" {
		return "unidentified"
	}
	for _, template := range input.templates {
		if strings.HasPrefix(template.sql, prefix) {
			return template.family
		}
	}
	return "other"
}

func preparedPlanMemoryObserve(ctx context.Context, output *preparedPlanMemoryOutput, input *preparedPlanMemoryInput, schema, phase string, backend *preparedPlanMemoryBackend) error {
	base := func() map[string]any {
		return map[string]any{"backend_pid": backend.pid, "backend_start": backend.backendStart,
			"role": backend.role, "connection_index": backend.index}
	}
	var total, used, cachedTotal, cachedUsed int64
	var contextRows, namedRows int
	contextSQL := `SELECT name,left(COALESCE(ident,''),32),octet_length(COALESCE(ident,''))::text,
		md5(COALESCE(ident,'')),COALESCE(parent,''),level::text,total_bytes::text,total_nblocks::text,
		free_bytes::text,free_chunks::text,used_bytes::text FROM ` + pgx.Identifier{schema, "contexts"}.Sanitize() + `()`
	if err := preparedPlanMemoryRawRows(ctx, backend.conn, contextSQL, 11, 16384, func(values [][]byte) error {
		numbers := [7]int64{}
		for index, column := range []int{2, 5, 6, 7, 8, 9, 10} {
			value, err := strconv.ParseInt(string(values[column]), 10, 64)
			if err != nil || value < 0 {
				return errors.New("memory-context observer returned an invalid nonnegative number")
			}
			numbers[index] = value
		}
		name, prefix := string(values[0]), string(values[1])
		if numbers[6] > numbers[2] {
			return errors.New("memory-context used bytes exceeded its total bytes")
		}
		record := base()
		record["name"], record["ident_prefix"], record["ident_view_bytes"], record["ident_view_md5"] = name, prefix, numbers[0], string(values[3])
		record["family_hint"], record["parent"], record["level"] = preparedPlanMemoryFamily(input, prefix), string(values[4]), numbers[1]
		record["total_bytes"], record["total_nblocks"], record["free_bytes"], record["free_chunks"], record["used_bytes"] = numbers[2], numbers[3], numbers[4], numbers[5], numbers[6]
		total, used = total+numbers[2], used+numbers[6]
		if strings.HasPrefix(name, "CachedPlan") {
			cachedTotal, cachedUsed = cachedTotal+numbers[2], cachedUsed+numbers[6]
		}
		contextRows++
		return output.emit("pg_memory_context", phase, record)
	}); err != nil {
		return err
	}
	if contextRows == 0 || total <= 0 || used > total {
		return errors.New("memory-context observation was empty or invalid")
	}
	if err := preparedPlanMemoryRawRows(ctx, backend.conn, `SELECT name,md5(statement),octet_length(statement)::text,
		left(statement,32),parameter_types::text,generic_plans::text,custom_plans::text,from_sql::text
		FROM pg_prepared_statements ORDER BY name`, 8, 2048, func(values [][]byte) error {
		sqlBytes, err := strconv.ParseInt(string(values[2]), 10, 64)
		if err != nil {
			return err
		}
		generic, err := strconv.ParseInt(string(values[5]), 10, 64)
		if err != nil {
			return err
		}
		custom, err := strconv.ParseInt(string(values[6]), 10, 64)
		if err != nil {
			return err
		}
		record := base()
		record["name"], record["sql_md5"], record["sql_bytes"] = string(values[0]), string(values[1]), sqlBytes
		record["family"], record["parameter_types"] = preparedPlanMemoryFamily(input, string(values[3])), string(values[4])
		record["generic_plans"], record["custom_plans"], record["from_sql"] = generic, custom, string(values[7])
		namedRows++
		return output.emit("prepared_statement", phase, record)
	}); err != nil {
		return err
	}
	record := base()
	record["context_rows"], record["named_statements"] = contextRows, namedRows
	record["backend_context_total_bytes"], record["backend_context_used_bytes"] = total, used
	record["cached_plan_context_total_bytes"], record["cached_plan_context_used_bytes"] = cachedTotal, cachedUsed
	wantNamed := preparedPlanMemoryExpectedNamed(output.group, phase, backend.role)
	record["expected_named_statements"], record["named_count_qualified"] = wantNamed, namedRows == wantNamed
	if err := output.emit("backend_summary", phase, record); err != nil {
		return err
	}
	if namedRows != wantNamed {
		return errors.New("prepared-statement inventory differs from the registered phase")
	}
	return nil
}

func preparedPlanMemoryExpectedNamed(group, phase, role string) int {
	if phase == "metadata_warm" || phase == "empty_baseline" || phase == "after_deallocate_all_both_caches" || group == "single-0" {
		return 0
	}
	if group == "topology-16" {
		switch role {
		case "owner":
			return 2
		case "data":
			return 6
		default:
			return 0
		}
	}
	if group == "single-1" {
		if phase == "representative_repeat" || phase == "after_normal_exec_drain" {
			return 1
		}
		return 2 // One resident entry and one pending protocol deallocation.
	}
	if phase == "pre_drain" {
		return 513 // The last registered probe leaves one pending deallocation.
	}
	if phase == "after_normal_exec_drain" {
		return 512
	}
	return 5
}

func preparedPlanMemoryAssignments(input *preparedPlanMemoryInput, role string, diverse bool, use func(preparedPlanMemoryTemplate) error) error {
	switch role {
	case "single":
		for _, template := range input.templates[:4] {
			if err := use(template); err != nil {
				return err
			}
		}
		count := 1
		if diverse {
			count = 509
		}
		for index := range count {
			template, err := input.sourceShape(index)
			if err != nil {
				return err
			}
			if err := use(template); err != nil {
				return err
			}
		}
	case "owner":
		for _, template := range input.templates[:2] {
			if err := use(template); err != nil {
				return err
			}
		}
	case "data":
		for _, template := range input.templates[2:4] {
			if err := use(template); err != nil {
				return err
			}
		}
		for index := range 4 {
			template, err := input.sourceShape(index)
			if err != nil {
				return err
			}
			if err := use(template); err != nil {
				return err
			}
		}
	case "control":
	default:
		return errors.New("unknown plan-memory role")
	}
	return nil
}

func preparedPlanMemorySafeError(err error) string {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		return "postgres:" + postgres.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return fmt.Sprintf("%T", err)
}

func preparedPlanMemoryRun(t *testing.T, ctx context.Context, output *preparedPlanMemoryOutput, input *preparedPlanMemoryInput, schema, group string, capacity, expectedQueries int) error {
	t.Helper()
	configuration := input.configuration.Copy()
	configuration.StatementCacheCapacity, configuration.DescriptionCacheCapacity = 0, 0
	configuration.DefaultQueryExecMode = pgx.QueryExecModeExec
	configuration.RuntimeParams["application_name"] = "goby-plan-memory-observer"
	observer, err := pgx.ConnectConfig(ctx, configuration)
	if err != nil {
		return err
	}
	// Capture the clearable variable, never a bound Close method or connection
	// value. This fixed observer is excluded from the target cohort population.
	t.Cleanup(func() {
		if observer != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = observer.Close(cleanup)
			observer = nil
		}
	})
	queries, err := preparedPlanMemoryRunCohort(t, ctx, output, input, schema, group, capacity, observer)
	if err != nil {
		return err
	}
	// RunCohort has returned: none of its stack slots can keep a raw target
	// connection, checkout or pool reachable at this final GC measurement.
	if err := output.client("target_references_dropped"); err != nil {
		return err
	}
	if queries != expectedQueries {
		return fmt.Errorf("registered target query count differs: got %d want %d", queries, expectedQueries)
	}
	return output.emit("completion", "complete", map[string]any{"target_queries": queries,
		"all_original_target_backends_exited": true, "replacement_backends_used_as_old_baseline": false})
}

func preparedPlanMemoryRunCohort(t *testing.T, ctx context.Context, output *preparedPlanMemoryOutput, input *preparedPlanMemoryInput, schema, group string, capacity int, observer *pgx.Conn) (int, error) {
	t.Helper()
	holder, err := preparedPlanMemoryOpen(ctx, input, group, capacity)
	// This variable is explicitly cleared before returning to the final GC.
	// Cleanup must not retain a copied cohort pointer after that point.
	t.Cleanup(func() {
		if holder != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := holder.close(cleanup); err != nil {
				t.Errorf("close the plan-memory target cohort: %s", preparedPlanMemorySafeError(err))
			}
			holder = nil
		}
	})
	if err != nil {
		return 0, err
	}
	identities := make([]preparedPlanMemoryIdentity, len(holder.backends))
	for index, backend := range holder.backends {
		identities[index] = backend.preparedPlanMemoryIdentity
		actual := backend.conn.Config()
		if actual.DefaultQueryExecMode != pgx.QueryExecModeCacheDescribe || actual.StatementCacheCapacity != capacity || actual.DescriptionCacheCapacity != 512 {
			return 0, errors.New("physical plan-memory configuration differs from selection")
		}
		if err := output.emit("backend_identity", "connected", map[string]any{"role": backend.role,
			"connection_index": backend.index, "backend_pid": backend.pid, "backend_start": backend.backendStart,
			"statement_cache_capacity": actual.StatementCacheCapacity, "description_cache_capacity": actual.DescriptionCacheCapacity,
			"default_mode": actual.DefaultQueryExecMode.String()}); err != nil {
			return 0, err
		}
		var exists bool
		if err := observer.QueryRow(ctx, preparedPlanMemoryBackendExistsSQL, pgx.QueryExecModeExec,
			int32(backend.pid), backend.backendStart).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, errors.New("an original target backend was absent before memory sampling")
		}
	}
	var expected [5][32]byte
	var known [5]bool
	queries := 0
	execute := func(backend *preparedPlanMemoryBackend, template preparedPlanMemoryTemplate, mode pgx.QueryExecMode, establish bool) error {
		digest, rows, err := preparedPlanMemoryReplay(ctx, backend.conn, template, mode)
		queries++
		if err != nil {
			_ = output.emit("workload_error", "query", map[string]any{"family": template.family,
				"query_ordinal": queries, "backend_pid": backend.pid, "error_code": preparedPlanMemorySafeError(err)})
			return err
		}
		family := -1
		for index, name := range preparedPlanMemoryFamilies {
			if name == template.family {
				family = index
				break
			}
		}
		if family < 0 || rows != 1 {
			return errors.New("captured plan-template result has an unexpected family or row count")
		}
		if establish && !known[family] {
			expected[family], known[family] = digest, true
		}
		if !known[family] || expected[family] != digest {
			_ = output.emit("workload_error", "result_hash", map[string]any{"family": template.family,
				"query_ordinal": queries, "backend_pid": backend.pid,
				"expected_sha256": hex.EncodeToString(expected[family][:]), "observed_sha256": hex.EncodeToString(digest[:])})
			return fmt.Errorf("plan-template result hash changed for %s", template.family)
		}
		return nil
	}
	phase := func(name string) error {
		if err := output.emit("phase", name, map[string]any{"target_queries_so_far": queries, "target_backends": len(holder.backends), "observed_at_utc": time.Now().UTC()}); err != nil {
			return err
		}
		for index := range holder.backends {
			if err := preparedPlanMemoryObserve(ctx, output, input, schema, name, &holder.backends[index]); err != nil {
				return err
			}
		}
		return output.client(name)
	}
	apply := func(operation func(*pgx.Conn) error) error {
		for index := range holder.backends {
			if err := operation(holder.backends[index].conn); err != nil {
				return err
			}
		}
		return nil
	}
	workload := func(diverse bool, repeats int, mode pgx.QueryExecMode, establish bool) error {
		for index := range holder.backends {
			backend := &holder.backends[index]
			if err := preparedPlanMemoryAssignments(input, backend.role, diverse, func(template preparedPlanMemoryTemplate) error {
				for range repeats {
					if err := execute(backend, template, mode, establish); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}
	// Warm the same catalog metadata and typed result machinery without named
	// or description-cache entries. Every source scope selects the same fixture.
	if err := workload(group != "topology-16", 1, pgx.QueryExecModeDescribeExec, true); err != nil {
		return queries, err
	}
	for _, found := range known {
		if !found {
			return queries, errors.New("metadata warm-up missed a real template family")
		}
	}
	// Warm the SECURITY DEFINER helper and both fixed simple-query observers
	// before the empty baseline, including backend-local function metadata.
	if err := phase("metadata_warm"); err != nil {
		return queries, err
	}
	if err := apply(func(conn *pgx.Conn) error { return conn.DeallocateAll(ctx) }); err != nil {
		return queries, err
	}
	if err := phase("empty_baseline"); err != nil {
		return queries, err
	}
	mode := pgx.QueryExecModeCacheStatement
	if capacity == 0 {
		mode = pgx.QueryExecModeCacheDescribe
	}
	if err := workload(false, 1, mode, false); err != nil {
		return queries, err
	}
	if err := phase("representative"); err != nil {
		return queries, err
	}
	if err := workload(false, 5, mode, false); err != nil {
		return queries, err
	}
	if err := phase("representative_repeat"); err != nil {
		return queries, err
	}
	if group != "topology-16" {
		if err := workload(true, 6, mode, false); err != nil {
			return queries, err
		}
		// The final fixed-template probe adds no 514th SQL shape. It makes
		// pending eviction observable after shape-major six-execution warming.
		if err := execute(&holder.backends[0], input.templates[0], mode, false); err != nil {
			return queries, err
		}
	}
	if err := phase("pre_drain"); err != nil {
		return queries, err
	}
	if err := apply(func(conn *pgx.Conn) error { _, err := conn.Exec(ctx, "SELECT 1"); return err }); err != nil {
		return queries, err
	}
	if err := phase("after_normal_exec_drain"); err != nil {
		return queries, err
	}
	if err := apply(func(conn *pgx.Conn) error { return conn.DeallocateAll(ctx) }); err != nil {
		return queries, err
	}
	if err := phase("after_deallocate_all_both_caches"); err != nil {
		return queries, err
	}
	if err := workload(false, 1, mode, false); err != nil {
		return queries, err
	}
	if err := phase("rebuilt_representative"); err != nil {
		return queries, err
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = holder.close(cleanup)
	cancel()
	if err != nil {
		return queries, err
	}
	for _, identity := range identities {
		wait, stop := context.WithTimeout(ctx, 5*time.Second)
		for {
			var exists bool
			err := observer.QueryRow(wait, preparedPlanMemoryBackendExistsSQL, pgx.QueryExecModeExec,
				int32(identity.pid), identity.backendStart).Scan(&exists)
			if err != nil {
				stop()
				return queries, err
			}
			if !exists {
				break
			}
			select {
			case <-wait.Done():
				stop()
				return queries, errors.New("an original target backend did not exit before its bounded close observation")
			case <-time.After(10 * time.Millisecond):
			}
		}
		stop()
		if err := output.emit("backend_exit", "closed_reachable", map[string]any{"backend_pid": identity.pid,
			"backend_start": identity.backendStart, "role": identity.role, "connection_index": identity.index, "original_backend_absent": true}); err != nil {
			return queries, err
		}
	}
	if err := output.client("closed_reachable"); err != nil {
		return queries, err
	}
	runtime.KeepAlive(holder)
	for index := range holder.backends {
		holder.backends[index].conn, holder.backends[index].lease = nil, nil
	}
	holder.backends, holder.data, holder.control = nil, nil, nil
	holder = nil
	return queries, nil
}

func TestPreparedPlanResidentMemory(t *testing.T) {
	if os.Getenv("GOBY_PREPARED_PLAN_MEMORY") != "1" {
		t.Skip("GOBY_PREPARED_PLAN_MEMORY=1 enables the fixed memory experiment")
	}
	group := os.Getenv("GOBY_PREPARED_PLAN_MEMORY_GROUP")
	capacities := map[string]int{"single-512": 512, "single-0": 0, "single-1": 1, "topology-16": 512}
	capacity, ok := capacities[group]
	if !ok {
		t.Fatal("select exactly one registered plan-memory group per native process")
	}
	schema := os.Getenv("GOBY_PREPARED_PLAN_MEMORY_SCHEMA")
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) || !strings.Contains(schema, "plan_memory") {
		t.Fatal("a bounded task-owned plan_memory schema is required")
	}
	directory := os.Getenv("GOBY_PREPARED_PLAN_MEMORY_OUTPUT")
	info, err := os.Lstat(directory)
	if !filepath.IsAbs(directory) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		t.Fatal("plan-memory output requires an existing private absolute directory")
	}
	file, err := os.OpenFile(filepath.Join(directory, group+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("create immutable plan-memory evidence")
	}
	output := &preparedPlanMemoryOutput{file: file, group: group}
	output.encoder = json.NewEncoder(output)
	defer func() {
		_ = file.Sync()
		_ = file.Close()
		record, _ := json.Marshal(map[string]any{"schema_version": 1, "group": group, "success": !t.Failed(), "records": output.ordinal, "bytes": output.bytes})
		t.Logf("prepared_plan_memory=%s", record)
	}()
	expectedQueries, representative := 3627, 5
	if group == "topology-16" {
		expectedQueries, representative = 544, 68
	}
	if err := output.emit("selection", "admission", map[string]any{
		"statement_cache_capacity": capacity, "description_cache_capacity": 512,
		"default_mode": "cache_describe", "target_queries": expectedQueries,
		"representative_templates": representative, "single_distinct_shapes": 513,
		"source_shapes": 509, "single_diversity_repeats": 6, "single_eviction_probe": 1,
		"representative_initial_repeats": 1, "representative_additional_repeats": 5,
		"topology_diversity_repeats": 0, "topology_eviction_probe": 0,
		"topology_min_connections": 0, "production_min_connections": 1,
		"rebuild_repeats": 1, "output_limit_bytes": preparedPlanMemoryOutputLimit,
		"context_row_limit_per_backend": 16384, "inventory_row_limit_per_backend": 2048,
		"normal_exec_barriers_per_backend": 1, "deallocate_all_calls_per_backend": 2,
		"go_version": runtime.Version(), "go_max_procs": runtime.GOMAXPROCS(0),
		"policy_shape_scope": "historical literal-policy calibration; current playback binds these values; not a production SQL-shape distribution",
		"scope":              "real captured typed SELECT templates replayed on dedicated backends; not complete HTTP, scan, Server or Store execution; deployment lease excluded",
		"observer_scope":     "raw simple SQL replaces unnamed statements; contexts include the fixed observer; ident is PostgreSQL view-truncated and only a 32-character prefix plus hash is exported; parent/name are not unique tree keys",
		"memory_scope":       "backend context totals are not per-query costs; Go heap is process-wide after GC and includes the fixed fixture, observer and recorder; TotalAlloc is cumulative allocation, not retained heap",
	}); err != nil {
		t.Fatal("write the registered memory selection")
	}
	input := preparedPlanMemoryCapture(t)
	for _, template := range input.templates {
		digest, short := sha256.Sum256([]byte(template.sql)), md5.Sum([]byte(template.sql))
		if err := output.emit("captured_template", "capture", map[string]any{"family": template.family,
			"sql_sha256": hex.EncodeToString(digest[:]), "sql_md5": hex.EncodeToString(short[:]),
			"sql_bytes": len(template.sql), "typed_arguments": len(template.args)}); err != nil {
			t.Fatal("write bounded template identities")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := preparedPlanMemoryRun(t, ctx, output, &input, schema, group, capacity, expectedQueries); err != nil {
		_ = output.emit("failure", "failed", map[string]any{"error_code": preparedPlanMemorySafeError(err)})
		t.Fatalf("plan-memory experiment failed: %s", preparedPlanMemorySafeError(err))
	}
}
