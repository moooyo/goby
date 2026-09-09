package transcode_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/transcode"
)

// Each test owns exactly one random schema. No production schema is selected or
// modified, and all fixture authentication hashes are synthetic test values.
func encodingRepositoryFixture(t *testing.T) (context.Context, *pgxpool.Pool, *transcode.PGRepository, transcode.Scope, transcode.Scope) {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "goby_encoding_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create owned test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned test schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated integration database pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	createScope := func(name, device string) transcode.Scope {
		scope := transcode.Scope{UserID: name, AuthSessionID: "auth_" + name, DeviceID: device,
			PlaySessionID: "play_" + name, ItemID: "item_" + name, SourceID: "mediasource_item_" + name}
		if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,normalized_name,password_hash,has_password)
			VALUES ($1,$1,$1,'fixture-only-no-authentication',false)`, scope.UserID); err != nil {
			t.Fatalf("insert fixture account: %v", err)
		}
		tokenHash := sha256.Sum256([]byte("encoding-repository-fixture-" + name))
		if _, err := pool.Exec(ctx, `INSERT INTO sessions (id,user_id,token_hash,kind,device_id,created_at,expires_at)
			VALUES ($1,$2,$3,'emby',$4,clock_timestamp() - interval '31 days',clock_timestamp() + interval '1 day')`,
			scope.AuthSessionID, scope.UserID, tokenHash[:], scope.DeviceID); err != nil {
			t.Fatalf("insert fixture authentication: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO libraries (id,name,collection_type) VALUES ($1,$1,'movies')`, "library_"+name); err != nil {
			t.Fatalf("insert fixture library: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO items (id,library_id,name,sort_name,type,path)
			VALUES ($1,$2,$1,$1,'Movie','/synthetic/private/source-path')`, scope.ItemID, "library_"+name); err != nil {
			t.Fatalf("insert fixture item: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
			(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,'Prepared',6000000000,clock_timestamp() + interval '30 minutes')`,
			scope.PlaySessionID, scope.UserID, scope.AuthSessionID, scope.DeviceID, scope.ItemID, scope.SourceID); err != nil {
			t.Fatalf("insert fixture playback: %v", err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_item_data (user_id,item_id,is_favorite,play_count)
			VALUES ($1,$2,true,7)`, scope.UserID, scope.ItemID); err != nil {
			t.Fatalf("insert fixture user data: %v", err)
		}
		return scope
	}
	first := createScope("first-viewer", "")
	second := createScope("second-viewer", "second-device")
	return ctx, pool, transcode.NewRepository(pool), first, second
}

func encodingFixtureRecord(t *testing.T, scope transcode.Scope) transcode.Record {
	t.Helper()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	return transcode.Record{ID: hex.EncodeToString(random[:]), Spec: transcode.Spec{Scope: scope,
		SourceStamp: strings.Repeat("a", 64), Plan: transcode.Plan{Container: "ts", VideoCodec: "h264", AudioCodec: "aac",
			VideoStreamIndex: 0, AudioStreamIndex: 1, DurationTicks: 6_000_000_000,
			VideoBitrate: 2_000_000, AudioBitrate: 128_000, AudioChannels: 2, SegmentSeconds: 3}},
		State: "queued", CreatedAt: now, UpdatedAt: now, LastAccessAt: now}
}

func encodingStoredRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) transcode.Record {
	t.Helper()
	var record transcode.Record
	var plan []byte
	if err := pool.QueryRow(ctx, `SELECT id,user_id,auth_session_id,device_id,play_session_id,item_id,
		media_source_id,source_stamp,plan,state,output_bytes,error_code,created_at,updated_at,last_access_at
		FROM encoding_jobs WHERE id = $1`, id).Scan(&record.ID, &record.Spec.Scope.UserID,
		&record.Spec.Scope.AuthSessionID, &record.Spec.Scope.DeviceID, &record.Spec.Scope.PlaySessionID,
		&record.Spec.Scope.ItemID, &record.Spec.Scope.SourceID, &record.Spec.SourceStamp, &plan,
		&record.State, &record.OutputBytes, &record.ErrorCode, &record.CreatedAt, &record.UpdatedAt, &record.LastAccessAt); err != nil {
		t.Fatalf("read persisted encoding: %v", err)
	}
	if err := json.Unmarshal(plan, &record.Spec.Plan); err != nil {
		t.Fatalf("decode persisted encoding plan: %v", err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.LastAccessAt = record.LastAccessAt.UTC()
	return record
}

func TestEncodingRepositoryPersistsImmutableScopeAndMonotonicStatus(t *testing.T) {
	ctx, pool, repository, first, second := encodingRepositoryFixture(t)
	record := encodingFixtureRecord(t, first)
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("create prepared playback encoding with empty device ID: %v", err)
	}
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, record) {
		t.Fatalf("stored encoding differs from immutable input: %+v", stored)
	}
	if err := repository.Create(ctx, record); !errors.Is(err, transcode.ErrRecordConflict) {
		t.Fatalf("duplicate encoding ID = %v", err)
	}
	record.State, record.OutputBytes = "running", 4096
	record.UpdatedAt, record.LastAccessAt = record.CreatedAt.Add(2*time.Second), record.CreatedAt.Add(time.Second)
	if err := repository.Update(ctx, record); err != nil {
		t.Fatalf("start encoding: %v", err)
	}
	stale := record
	stale.OutputBytes = 1
	stale.UpdatedAt, stale.LastAccessAt = stale.CreatedAt, stale.CreatedAt
	if err := repository.Update(ctx, stale); err != nil {
		t.Fatalf("repeat older progress safely: %v", err)
	}
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, record) {
		t.Fatalf("older progress regressed stored status: %+v", stored)
	}
	for _, test := range []struct {
		name string
		edit func(*transcode.Record)
		want error
	}{
		{"user", func(r *transcode.Record) { r.Spec.Scope.UserID = second.UserID }, transcode.ErrRecordNotFound},
		{"authentication", func(r *transcode.Record) { r.Spec.Scope.AuthSessionID = second.AuthSessionID }, transcode.ErrRecordNotFound},
		{"device", func(r *transcode.Record) { r.Spec.Scope.DeviceID = second.DeviceID }, transcode.ErrRecordNotFound},
		{"playback", func(r *transcode.Record) { r.Spec.Scope.PlaySessionID = second.PlaySessionID }, transcode.ErrRecordNotFound},
		{"item", func(r *transcode.Record) { r.Spec.Scope.ItemID = second.ItemID }, transcode.ErrRecordNotFound},
		{"source", func(r *transcode.Record) { r.Spec.Scope.SourceID = second.SourceID }, transcode.ErrRecordNotFound},
		{"stamp", func(r *transcode.Record) { r.Spec.SourceStamp = strings.Repeat("b", 64) }, transcode.ErrRecordConflict},
		{"plan", func(r *transcode.Record) { r.Spec.Plan.VideoBitrate++ }, transcode.ErrRecordConflict},
		{"creation time", func(r *transcode.Record) { r.CreatedAt = r.CreatedAt.Add(-time.Second) }, transcode.ErrRecordConflict},
		{"return to queue", func(r *transcode.Record) { r.State = "queued" }, transcode.ErrRecordConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := record
			test.edit(&changed)
			if err := repository.Update(ctx, changed); !errors.Is(err, test.want) {
				t.Errorf("immutable or ownership change returned %v, want %v", err, test.want)
			}
		})
	}
	// Cancellation and process completion must still be recorded after logout.
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", first.AuthSessionID); err != nil {
		t.Fatal(err)
	}
	record.State, record.OutputBytes = "completed", 8192
	record.UpdatedAt = record.UpdatedAt.Add(time.Second)
	if err := repository.Update(ctx, record); err != nil {
		t.Fatalf("persist a terminal status after logout: %v", err)
	}
	for _, next := range []string{"queued", "running", "failed", "cancelled", "interrupted"} {
		changed := record
		changed.State = next
		if err := repository.Update(ctx, changed); !errors.Is(err, transcode.ErrRecordConflict) {
			t.Errorf("completed encoding changed to %s: %v", next, err)
		}
	}
	repeat := record
	repeat.OutputBytes, repeat.ErrorCode = 9999, "late_error"
	repeat.LastAccessAt = repeat.LastAccessAt.Add(time.Minute)
	if err := repository.Update(ctx, repeat); err != nil {
		t.Fatalf("touch a completed encoding: %v", err)
	}
	expected := record
	expected.LastAccessAt = repeat.LastAccessAt
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, expected) {
		t.Errorf("terminal result changed on repeated update: %+v", stored)
	}
}

func TestEncodingRepositoryRechecksCurrentAuthenticationAndPlayback(t *testing.T) {
	ctx, pool, repository, first, second := encodingRepositoryFixture(t)
	for _, test := range []struct {
		name      string
		statement string
		argument  string
		edit      func(*transcode.Record)
	}{
		{name: "disabled account", statement: "UPDATE users SET is_disabled = true WHERE id = $1", argument: first.UserID},
		{name: "playback disabled", statement: `UPDATE users SET policy = '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, argument: first.UserID},
		{name: "invalid policy", statement: "UPDATE users SET policy = '[]'::jsonb WHERE id = $1", argument: first.UserID},
		{name: "revoked authentication", statement: "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", argument: first.AuthSessionID},
		{name: "expired authentication", statement: "UPDATE sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", argument: first.AuthSessionID},
		{name: "admin cookie", statement: "UPDATE sessions SET kind = 'admin' WHERE id = $1", argument: first.AuthSessionID},
		{name: "stopped playback", statement: "UPDATE play_sessions SET state = 'Stopped' WHERE id = $1", argument: first.PlaySessionID},
		{name: "expired playback", statement: "UPDATE play_sessions SET expires_at = clock_timestamp() - interval '1 second' WHERE id = $1", argument: first.PlaySessionID},
		{name: "wrong owner", edit: func(r *transcode.Record) { r.Spec.Scope.UserID = second.UserID }},
		{name: "wrong authentication", edit: func(r *transcode.Record) { r.Spec.Scope.AuthSessionID = second.AuthSessionID }},
		{name: "wrong device", edit: func(r *transcode.Record) { r.Spec.Scope.DeviceID = "wrong-device" }},
		{name: "wrong playback", edit: func(r *transcode.Record) { r.Spec.Scope.PlaySessionID = second.PlaySessionID }},
		{name: "wrong item", edit: func(r *transcode.Record) { r.Spec.Scope.ItemID = second.ItemID }},
		{name: "wrong source", edit: func(r *transcode.Record) { r.Spec.Scope.SourceID = second.SourceID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, reset := range []struct{ query, id string }{
				{"UPDATE users SET is_disabled = false, policy = '{}'::jsonb WHERE id = $1", first.UserID},
				{"UPDATE sessions SET kind = 'emby', revoked_at = NULL, expires_at = clock_timestamp() + interval '1 day' WHERE id = $1", first.AuthSessionID},
				{"UPDATE play_sessions SET state = 'Prepared', expires_at = clock_timestamp() + interval '30 minutes' WHERE id = $1", first.PlaySessionID},
			} {
				if _, err := pool.Exec(ctx, reset.query, reset.id); err != nil {
					t.Fatalf("restore owned fixture state: %v", err)
				}
			}
			if test.statement != "" {
				if _, err := pool.Exec(ctx, test.statement, test.argument); err != nil {
					t.Fatal(err)
				}
			}
			record := encodingFixtureRecord(t, first)
			if test.edit != nil {
				test.edit(&record)
			}
			if err := repository.Create(ctx, record); !errors.Is(err, transcode.ErrRecordUnauthorized) {
				t.Errorf("unauthorized encoding returned %v", err)
			}
		})
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected requests persisted %d jobs: %v", count, err)
	}
}

func TestEncodingRepositoryRejectsUnsafeAndUninitializedRecords(t *testing.T) {
	ctx, pool, repository, first, _ := encodingRepositoryFixture(t)
	for _, test := range []struct {
		name string
		edit func(*transcode.Record)
	}{
		{"invalid ID", func(r *transcode.Record) { r.ID = "../other-job" }},
		{"empty stamp", func(r *transcode.Record) { r.Spec.SourceStamp = "" }},
		{"oversized identity", func(r *transcode.Record) { r.Spec.Scope.ItemID = strings.Repeat("x", 257) }},
		{"control character", func(r *transcode.Record) { r.Spec.Scope.DeviceID = "bad\x00device" }},
		{"negative size", func(r *transcode.Record) { r.OutputBytes = -1 }},
		{"raw error detail", func(r *transcode.Record) { r.ErrorCode = "open /private/media: failed" }},
		{"oversized error code", func(r *transcode.Record) { r.ErrorCode = strings.Repeat("x", 65) }},
		{"invalid plan", func(r *transcode.Record) { r.Spec.Plan.Container = "../../playlist" }},
		{"zero timestamp", func(r *transcode.Record) { r.CreatedAt = time.Time{} }},
		{"backdated update", func(r *transcode.Record) { r.UpdatedAt = r.CreatedAt.Add(-time.Second) }},
		{"backdated access", func(r *transcode.Record) { r.LastAccessAt = r.CreatedAt.Add(-time.Second) }},
		{"unknown status", func(r *transcode.Record) { r.State = "successful" }},
		{"already running", func(r *transcode.Record) { r.State = "running" }},
		{"already completed", func(r *transcode.Record) { r.State = "completed" }},
		{"nonempty output", func(r *transcode.Record) { r.OutputBytes = 1 }},
		{"initial error", func(r *transcode.Record) { r.ErrorCode = "process_failed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := encodingFixtureRecord(t, first)
			test.edit(&record)
			if err := repository.Create(ctx, record); !errors.Is(err, transcode.ErrInvalidRecord) {
				t.Errorf("invalid record returned %v", err)
			}
		})
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 0 {
		t.Errorf("invalid records persisted %d rows: %v", count, err)
	}
}

func encodingUnrelatedSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'users', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
		'auth', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM sessions t),
		'items', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM items t),
		'play', (SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM play_sessions t),
		'userdata', (SELECT jsonb_agg(to_jsonb(t) ORDER BY user_id,item_id) FROM user_item_data t))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot unrelated state: %v", err)
	}
	return snapshot
}

func TestEncodingRepositoryRecoveryOnlyInterruptsAbandonedWork(t *testing.T) {
	ctx, pool, repository, first, second := encodingRepositoryFixture(t)
	records := make(map[string]transcode.Record)
	for index, state := range []string{"queued", "running", "completed", "failed", "cancelled", "interrupted"} {
		scope := first
		if index%2 == 1 {
			scope = second
		}
		record := encodingFixtureRecord(t, scope)
		if err := repository.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
		if state != "queued" {
			record.State = "running"
			record.OutputBytes = int64(index + 1)
			if err := repository.Update(ctx, record); err != nil {
				t.Fatal(err)
			}
			if state != "running" {
				record.State = state
				if state != "completed" {
					record.ErrorCode = "original_terminal"
				}
				if err := repository.Update(ctx, record); err != nil {
					t.Fatal(err)
				}
			}
		}
		records[state] = encodingStoredRecord(t, ctx, pool, record.ID)
	}
	unrelated := encodingUnrelatedSnapshot(t, ctx, pool)
	if err := repository.Recover(ctx); err != nil {
		t.Fatalf("recover abandoned conversions: %v", err)
	}
	for state, before := range records {
		after := encodingStoredRecord(t, ctx, pool, before.ID)
		if state == "queued" || state == "running" {
			if after.State != "interrupted" || after.ErrorCode != "server_restart" || after.UpdatedAt.Before(before.UpdatedAt) {
				t.Errorf("abandoned %s encoding did not become interrupted: %+v", state, after)
			}
			before.State, before.ErrorCode, before.UpdatedAt = after.State, after.ErrorCode, after.UpdatedAt
		}
		if !reflect.DeepEqual(after, before) {
			t.Errorf("recovery changed unrelated encoding fields for %s: %+v", state, after)
		}
		records[state] = after
	}
	if err := repository.Recover(ctx); err != nil {
		t.Fatalf("repeat completed recovery: %v", err)
	}
	for state, before := range records {
		if after := encodingStoredRecord(t, ctx, pool, before.ID); !reflect.DeepEqual(after, before) {
			t.Errorf("repeated recovery changed terminal %s encoding", state)
		}
	}
	if after := encodingUnrelatedSnapshot(t, ctx, pool); after != unrelated {
		t.Error("encoding recovery changed users, authentication, catalog, playback, or user data")
	}
}

func TestEncodingRepositoryConcurrentTerminalWritersCannotReviveJobs(t *testing.T) {
	ctx, pool, repository, first, _ := encodingRepositoryFixture(t)
	record := encodingFixtureRecord(t, first)
	if err := repository.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.State = "running"
	if err := repository.Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type outcome struct {
		record transcode.Record
		err    error
	}
	results := make(chan outcome, 2)
	for index, state := range []string{"completed", "failed"} {
		candidate := record
		candidate.State, candidate.OutputBytes = state, int64(100+index)
		if state == "failed" {
			candidate.ErrorCode = "process_failed"
		}
		go func() {
			<-start
			results <- outcome{candidate, repository.Update(ctx, candidate)}
		}()
	}
	close(start)
	wins, conflicts := 0, 0
	var winner transcode.Record
	for range 2 {
		result := <-results
		if result.err == nil {
			wins++
			winner = result.record
		} else if errors.Is(result.err, transcode.ErrRecordConflict) {
			conflicts++
		} else {
			t.Errorf("concurrent terminal write: %v", result.err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("terminal writes had %d winners and %d conflicts", wins, conflicts)
	}
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, winner) {
		t.Errorf("terminal result did not match the first serialized writer: %+v", stored)
	}
}

func TestEncodingRepositoryRejectsAuthenticationExpiryWhileWaitingForPlayback(t *testing.T) {
	fixtureCtx, pool, repository, first, _ := encodingRepositoryFixture(t)
	ctx, cancel := context.WithTimeout(fixtureCtx, 15*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = blocker.Rollback(cleanupCtx)
	}()
	var blockerPID int32
	if err := blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM play_sessions WHERE id = $1 FOR UPDATE", first.PlaySessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET expires_at = clock_timestamp() + interval '2 seconds' WHERE id = $1", first.AuthSessionID); err != nil {
		t.Fatal(err)
	}
	record := encodingFixtureRecord(t, first)
	result := make(chan error, 1)
	go func() { result <- repository.Create(ctx, record) }()
	waitFor := func(label, query string, argument any) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var ready bool
			if err := pool.QueryRow(ctx, query, argument).Scan(&ready); err != nil {
				t.Fatalf("observe %s: %v", label, err)
			}
			if ready {
				return
			}
			select {
			case err := <-result:
				t.Fatalf("creation ended before %s: %v", label, err)
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", label)
			case <-ticker.C:
			}
		}
	}
	waitFor("the owned playback row lock", `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
		WHERE $1::integer = ANY(pg_blocking_pids(pid)))`, blockerPID)
	waitFor("authentication expiry", "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", first.AuthSessionID)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, transcode.ErrRecordUnauthorized) {
			t.Errorf("authentication expired while creation waited, returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("encoding creation did not finish after releasing the owned row lock")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 0 {
		t.Errorf("expired authentication persisted %d encodings: %v", count, err)
	}
}

func TestEncodingRepositoryParentDeletionHonorsExistingRetention(t *testing.T) {
	ctx, pool, repository, first, second := encodingRepositoryFixture(t)
	firstRecord, secondRecord := encodingFixtureRecord(t, first), encodingFixtureRecord(t, second)
	for _, record := range []transcode.Record{firstRecord, secondRecord} {
		if err := repository.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "DELETE FROM play_sessions WHERE id = $1", first.PlaySessionID); err != nil {
		t.Fatalf("delete an owned playback history entry: %v", err)
	}
	if err := repository.Update(ctx, firstRecord); !errors.Is(err, transcode.ErrRecordNotFound) {
		t.Errorf("deleted playback retained an orphaned encoding: %v", err)
	}
	if after := encodingStoredRecord(t, ctx, pool, secondRecord.ID); !reflect.DeepEqual(after, secondRecord) {
		t.Error("deleting one playback changed another owner's encoding")
	}
	for _, table := range []string{"users", "sessions", "items", "user_item_data"} {
		var count int
		if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", pgx.Identifier{table}.Sanitize())).Scan(&count); err != nil || count != 2 {
			t.Errorf("playback history deletion changed %s count = %d: %v", table, count, err)
		}
	}
}

func encodingVODCutTimes(start int64, count int) string {
	var cuts strings.Builder
	for index := 1; index <= count; index++ {
		if index > 1 {
			cuts.WriteByte(',')
		}
		cuts.WriteString(strconv.FormatInt(start+int64(index)*30_000_000, 10))
	}
	return cuts.String()
}

func TestEncodingRepositoryPersistsLargeImmutableVODPlans(t *testing.T) {
	ctx, pool, repository, first, _ := encodingRepositoryFixture(t)
	record := encodingFixtureRecord(t, first)
	plan := &record.Spec.Plan
	plan.SegmentMode, plan.SegmentStartNumber = "vod", 6
	plan.StartTicks, plan.EndTicks, plan.DurationTicks = 180_000_000, 45_210_000_000, 60_000_000_000
	plan.SegmentTimes = encodingVODCutTimes(plan.StartTicks, 1500)
	encoded, err := json.Marshal(*plan)
	if err != nil || len(encoded) <= 8192 || len(encoded) > transcode.MaxPlanBytes || len(plan.SegmentTimes) > 64*1024 {
		t.Fatalf("large VOD fixture has unexpected encoded size %d: %v", len(encoded), err)
	}
	if _, err := pool.Exec(ctx, "UPDATE play_sessions SET duration_ticks = $2 WHERE id = $1", first.PlaySessionID, plan.DurationTicks); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("persist a legal VOD plan beyond the previous 8 KiB limit: %v", err)
	}
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, record) {
		t.Fatal("large VOD cut points or immutable source fields changed on persistence")
	}
	record.State, record.OutputBytes = "running", 4096
	if err := repository.Update(ctx, record); err != nil {
		t.Fatalf("update a large VOD encoding: %v", err)
	}
	for _, test := range []struct {
		name string
		edit func(*transcode.Plan)
	}{
		{"global segment number", func(p *transcode.Plan) { p.SegmentStartNumber++ }},
		{"source start", func(p *transcode.Plan) { p.StartTicks++ }},
		{"window end", func(p *transcode.Plan) { p.EndTicks++ }},
		{"source cut points", func(p *transcode.Plan) { _, p.SegmentTimes, _ = strings.Cut(p.SegmentTimes, ",") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := record
			test.edit(&changed.Spec.Plan)
			if err := repository.Update(ctx, changed); !errors.Is(err, transcode.ErrRecordConflict) {
				t.Errorf("immutable VOD window change returned %v", err)
			}
		})
	}
	if stored := encodingStoredRecord(t, ctx, pool, record.ID); !reflect.DeepEqual(stored, record) {
		t.Fatal("rejected VOD window changes altered the persisted plan")
	}
}

func TestEncodingRepositoryRejectsVODPlansBeyondPersistedSizeLimit(t *testing.T) {
	ctx, pool, repository, first, _ := encodingRepositoryFixture(t)
	record := encodingFixtureRecord(t, first)
	record.Spec.Plan.SegmentMode = "vod"
	record.Spec.Plan.DurationTicks = 1_000_000_000_000
	record.Spec.Plan.EndTicks = 900_000_000_000
	record.Spec.Plan.SegmentTimes = encodingVODCutTimes(0, 15000)
	encoded, err := json.Marshal(record.Spec.Plan)
	if err != nil || len(encoded) <= transcode.MaxPlanBytes {
		t.Fatalf("oversized VOD fixture has unexpected encoded size %d: %v", len(encoded), err)
	}
	// The cut-list bound is deliberately narrower than MaxPlanBytes, so a plan
	// exceeding the storage budget must already be rejected by semantic checks.
	if err := repository.Create(ctx, record); !errors.Is(err, transcode.ErrInvalidRecord) {
		t.Errorf("oversized VOD plan returned %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 0 {
		t.Errorf("oversized VOD plan persisted %d encodings: %v", count, err)
	}
}
