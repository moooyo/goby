package transcode_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/transcode"
)

func encodingApplicationKeyFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, item transcode.Scope, name string, sidecar bool) transcode.Scope {
	t.Helper()
	scope := transcode.Scope{ApplicationKey: true, AuthSessionID: "key_" + name, DeviceID: "key-device-" + name,
		PlaySessionID: "key-play-" + name, ItemID: item.ItemID, SourceID: item.SourceID}
	digest := sha256.Sum256([]byte("encoding-application-key-fixture-" + name))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, token_hash, kind, device_id)
		VALUES ($1, $2, 'application_key', $3)`, scope.AuthSessionID, digest[:], scope.DeviceID); err != nil {
		t.Fatal(err)
	}
	if sidecar {
		if _, err := pool.Exec(ctx, `INSERT INTO application_keys (credential_id, secret_ciphertext)
			VALUES ($1, $2)`, scope.AuthSessionID, []byte("fixture-ciphertext")); err != nil {
			t.Fatal(err)
		}
	}
	return encodingApplicationClientFixture(t, ctx, pool, scope, name)
}

func encodingApplicationClientFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scope transcode.Scope, name string) transcode.Scope {
	t.Helper()
	scope.ApplicationClientID, scope.PlaySessionID = "application-client-"+name, "key-play-"+name
	if _, err := pool.Exec(ctx, `INSERT INTO application_key_clients
		(id, credential_id, client_name, device_id, device_name, client_version)
		VALUES ($1, $2, $3, $4, 'Fixture device', '1')`, scope.ApplicationClientID, scope.AuthSessionID, name, scope.DeviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO play_sessions
		(id, user_id, auth_session_id, device_id, item_id, media_source_id, state, duration_ticks, expires_at, application_client_id)
		VALUES ($1, NULL, $2, $3, $4, $5, 'Prepared', 6000000000, clock_timestamp() + interval '30 minutes', $6)`,
		scope.PlaySessionID, scope.AuthSessionID, scope.DeviceID, scope.ItemID, scope.SourceID, scope.ApplicationClientID); err != nil {
		t.Fatal(err)
	}
	return scope
}

func encodingStoredApplicationRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) transcode.Record {
	t.Helper()
	var record transcode.Record
	var plan []byte
	if err := pool.QueryRow(ctx, `SELECT job.id, COALESCE(job.user_id, ''), job.auth_session_id, job.device_id,
		job.play_session_id, job.item_id, job.media_source_id, job.source_stamp, job.plan, job.state,
		job.output_bytes, job.error_code, job.created_at, job.updated_at, job.last_access_at,
		(authentication.kind = 'application_key' AND job.user_id IS NULL), client.id
		FROM encoding_jobs job JOIN sessions authentication ON authentication.id = job.auth_session_id
		AND authentication.user_id IS NOT DISTINCT FROM job.user_id
		JOIN application_key_clients client ON client.id = job.application_client_id
		AND client.credential_id = authentication.id AND client.device_id = job.device_id WHERE job.id = $1`, id).
		Scan(&record.ID, &record.Spec.Scope.UserID, &record.Spec.Scope.AuthSessionID, &record.Spec.Scope.DeviceID,
			&record.Spec.Scope.PlaySessionID, &record.Spec.Scope.ItemID, &record.Spec.Scope.SourceID,
			&record.Spec.SourceStamp, &plan, &record.State, &record.OutputBytes, &record.ErrorCode,
			&record.CreatedAt, &record.UpdatedAt, &record.LastAccessAt, &record.Spec.Scope.ApplicationKey, &record.Spec.Scope.ApplicationClientID); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plan, &record.Spec.Plan); err != nil {
		t.Fatal(err)
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	record.LastAccessAt = record.LastAccessAt.UTC()
	return record
}

func TestEncodingApplicationKeyPersistsOwnerSourceAndSurvivesRepositoryRestart(t *testing.T) {
	ctx, pool, repository, normal, _ := encodingRepositoryFixture(t)
	first := encodingApplicationKeyFixture(t, ctx, pool, normal, "first", true)
	second := encodingApplicationKeyFixture(t, ctx, pool, normal, "second", true)
	before := encodingUnrelatedSnapshot(t, ctx, pool)
	record := encodingFixtureRecord(t, first)
	if err := repository.Create(ctx, record); err != nil {
		t.Fatalf("create userless encoding: %v", err)
	}
	stored := encodingStoredApplicationRecord(t, ctx, pool, record.ID)
	if !reflect.DeepEqual(stored, record) || !stored.Spec.Scope.ApplicationKey || stored.Spec.Scope.UserID != "" {
		t.Fatalf("persisted encoding lost its real credential owner: %+v", stored)
	}
	for _, test := range []struct {
		name string
		edit func(*transcode.Record)
		want error
	}{
		{"credential", func(value *transcode.Record) { value.Spec.Scope.AuthSessionID = second.AuthSessionID }, transcode.ErrRecordNotFound},
		{"client", func(value *transcode.Record) { value.Spec.Scope.ApplicationClientID = second.ApplicationClientID }, transcode.ErrRecordNotFound},
		{"device", func(value *transcode.Record) { value.Spec.Scope.DeviceID = second.DeviceID }, transcode.ErrRecordNotFound},
		{"source", func(value *transcode.Record) { value.Spec.Scope.SourceID += "-other" }, transcode.ErrRecordNotFound},
		{"playback", func(value *transcode.Record) { value.Spec.Scope.PlaySessionID = second.PlaySessionID }, transcode.ErrRecordNotFound},
		{"user", func(value *transcode.Record) { value.Spec.Scope.UserID = normal.UserID }, transcode.ErrInvalidRecord},
		{"kind", func(value *transcode.Record) { value.Spec.Scope.ApplicationKey = false }, transcode.ErrInvalidRecord},
		{"source stamp", func(value *transcode.Record) { value.Spec.SourceStamp += "-other" }, transcode.ErrRecordConflict},
		{"plan", func(value *transcode.Record) { value.Spec.Plan.VideoBitrate++ }, transcode.ErrRecordConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := record
			test.edit(&changed)
			if err := repository.Update(ctx, changed); !errors.Is(err, test.want) {
				t.Fatalf("changed immutable key encoding returned %v, want %v", err, test.want)
			}
		})
	}
	reloadedRepository := transcode.NewRepository(pool)
	stored.State = "running"
	if err := reloadedRepository.Update(ctx, stored); err != nil {
		t.Fatalf("new repository could not update persisted key scope: %v", err)
	}
	if err := reloadedRepository.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recovered := encodingStoredApplicationRecord(t, ctx, pool, record.ID)
	if recovered.State != "interrupted" || recovered.ErrorCode != "server_restart" || recovered.Spec != record.Spec {
		t.Fatalf("restart lost ownership or revived key work: %+v", recovered)
	}
	if after := encodingUnrelatedSnapshot(t, ctx, pool); after != before {
		t.Fatal("key encoding changed users, login credentials, playback, catalog, or user data")
	}
}

func TestEncodingApplicationKeySeparatesClientBindingAndSharedRevocation(t *testing.T) {
	ctx, pool, repository, normal, _ := encodingRepositoryFixture(t)
	first := encodingApplicationKeyFixture(t, ctx, pool, normal, "client-first", true)
	second := encodingApplicationClientFixture(t, ctx, pool, first, "client-second")
	one, two := encodingFixtureRecord(t, first), encodingFixtureRecord(t, second)
	for _, record := range []transcode.Record{one, two} {
		if err := repository.Create(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	if first.AuthSessionID != second.AuthSessionID || first.DeviceID != second.DeviceID || first.ApplicationClientID == second.ApplicationClientID {
		t.Fatal("fixture did not isolate client identity within one credential and device")
	}
	foreign := encodingFixtureRecord(t, first)
	foreign.Spec.Scope.ApplicationClientID = second.ApplicationClientID
	if err := repository.Create(ctx, foreign); !errors.Is(err, transcode.ErrRecordUnauthorized) {
		t.Fatalf("client started encoding from its sibling's playback: %v", err)
	}
	unrelated := encodingApplicationKeyFixture(t, ctx, pool, normal, "unrelated-client", true)
	foreign.Spec.Scope.ApplicationClientID = unrelated.ApplicationClientID
	if err := repository.Create(ctx, foreign); !errors.Is(err, transcode.ErrRecordUnauthorized) {
		t.Fatalf("key accepted another credential's client binding: %v", err)
	}
	foreign = one
	foreign.Spec.Scope.ApplicationClientID = second.ApplicationClientID
	if err := repository.Update(ctx, foreign); !errors.Is(err, transcode.ErrRecordNotFound) {
		t.Fatalf("client changed its sibling's durable encoding: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", first.AuthSessionID); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []transcode.Scope{first, second} {
		if err := repository.Create(ctx, encodingFixtureRecord(t, scope)); !errors.Is(err, transcode.ErrRecordUnauthorized) {
			t.Fatalf("revoked parent credential left a client authorized: %v", err)
		}
	}
	for _, record := range []transcode.Record{one, two} {
		record.State = "cancelled"
		if err := repository.Update(ctx, record); err != nil {
			t.Fatalf("revocation cleanup failed for a client: %v", err)
		}
		stored := encodingStoredApplicationRecord(t, ctx, pool, record.ID)
		if stored.State != "cancelled" || stored.Spec != record.Spec {
			t.Fatalf("cleanup lost client ownership: %+v", stored)
		}
	}
}

func TestEncodingApplicationKeyRejectsMissingRevokedAndMismatchedCredentials(t *testing.T) {
	ctx, pool, repository, _, normal := encodingRepositoryFixture(t)
	valid := encodingApplicationKeyFixture(t, ctx, pool, normal, "valid", true)
	missing := encodingApplicationKeyFixture(t, ctx, pool, normal, "missing-sidecar", false)
	revoked := encodingApplicationKeyFixture(t, ctx, pool, normal, "revoked", true)
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", revoked.AuthSessionID); err != nil {
		t.Fatal(err)
	}
	wrongKind := valid
	wrongKind.AuthSessionID, wrongKind.DeviceID = normal.AuthSessionID, normal.DeviceID
	// Foreign keys alone permit this malformed nullable playback binding. The
	// repository must independently reject its actual normal credential kind.
	if _, err := pool.Exec(ctx, `UPDATE play_sessions SET auth_session_id = $2, device_id = $3 WHERE id = $1`,
		valid.PlaySessionID, normal.AuthSessionID, normal.DeviceID); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []transcode.Scope{missing, revoked, wrongKind} {
		if err := repository.Create(ctx, encodingFixtureRecord(t, scope)); !errors.Is(err, transcode.ErrRecordUnauthorized) {
			t.Fatalf("invalid actual key credential was admitted: %+v, %v", scope, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE play_sessions SET auth_session_id = $2, device_id = $3,
		expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, valid.PlaySessionID, valid.AuthSessionID, valid.DeviceID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(ctx, encodingFixtureRecord(t, valid)); !errors.Is(err, transcode.ErrRecordUnauthorized) {
		t.Fatalf("expired key playback started encoding: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM encoding_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected key encodings left rows: count=%d error=%v", count, err)
	}
}
