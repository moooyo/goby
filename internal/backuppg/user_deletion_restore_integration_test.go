//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
)

func TestPostgreSQLSchema29ArchivePreservesCommittedUserDeletion(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 29)
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin the owned deleted-user archive witness")
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `INSERT INTO users(id,name,normalized_name,password_hash,is_administrator,management_revision)
		VALUES('deleted-archive-user','Removed administrator','removed administrator','test-only-digest',true,9007199254740993);
		INSERT INTO sessions(id,user_id,token_hash,kind,created_at,expires_at)
		VALUES('deleted-archive-session','deleted-archive-user',decode(repeat('ab',32),'hex'),'admin','2020-01-01T00:00:00Z','2030-01-01T00:00:00Z');
		DELETE FROM users WHERE id='deleted-archive-user'`); err != nil {
		t.Fatal("prepare the committed deletion and credential cascade")
	}
	event := activity.Event{Action: activity.ActionUserDeleted, Source: activity.SourceNative,
		Actor:    activity.Actor{Kind: activity.ActorUser, ID: "deleted-archive-user", CredentialID: "deleted-archive-session"},
		Resource: activity.Resource{Kind: activity.ResourceUser, ID: "deleted-archive-user"}, Revision: 9007199254740993, Count: 1}
	if err := activity.Record(ctx, tx, event); err != nil {
		t.Fatalf("record the schema29 deletion witness in its transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit the deleted-user archive witness")
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 29 || len(facts.MigrationChecksums) != 29 || len(facts.Tables) != 35 ||
		facts.MigrationChecksums[28].Name != "0029_user_deletion_activity.sql" || !equalJSON(facts, before) {
		t.Fatal("the deletion archive did not use the actual schema29 catalog and migration prefix")
	}
	result, err := RestoreOffline(ctx, target, archive, facts, options)
	if err != nil || result.SourceVersion != 29 || result.CurrentVersion != 29 || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("round trip the actual schema29 deleted-user archive: %v", err)
	}
	var absent bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM users WHERE id='deleted-archive-user')
		AND NOT EXISTS(SELECT 1 FROM sessions WHERE id='deleted-archive-session')`).Scan(&absent); err != nil || !absent {
		t.Fatal("restoration resurrected the deleted actor or its credential")
	}
	page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row {
		return target.QueryRow(ctx, statement, args...)
	}, activity.QueryOptions{Action: activity.ActionUserDeleted, ActorID: event.Actor.ID, Limit: 10})
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 {
		t.Fatalf("query the restored deletion audit by its original action and actor: %v", err)
	}
	entry := page.Items[0]
	if entry.Actor != event.Actor || entry.Resource != event.Resource || entry.Revision != event.Revision || entry.Count != 1 ||
		entry.ActorName != "" || entry.Name != "User deleted" || len(entry.ChangedFields) != 0 {
		t.Fatal("restoration changed the historical deleted-user audit or invented its display name")
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, restoredSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, facts) || len(restoredSequences) != len(sequences) {
		t.Fatal("the schema29 round trip changed complete table fingerprints or the sequence inventory")
	}
	for name, expected := range sequences {
		if actual, exists := restoredSequences[name]; !exists || actual != expected {
			t.Fatalf("the schema29 round trip changed sequence %s", name)
		}
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLSchema28ArchiveRejectsLaterUserDeletedAction(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 28)
	if _, err := source.Exec(ctx, `INSERT INTO activity_entries
		(action,severity,source,actor_kind,actor_id,actor_credential_id,resource_kind,resource_id,revision,affected_count)
		VALUES('user.updated','Info','native','user','historical-audit-user','historical-audit-session','user','historical-audit-user',9007199254740993,1)`); err != nil {
		t.Fatal("seed a legal historical schema28 activity row")
	}
	archive, originalFacts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if originalFacts.SchemaVersion != 28 || len(originalFacts.MigrationChecksums) != 28 || !equalJSON(originalFacts, before) {
		t.Fatal("the adversarial test did not begin with a genuine schema28 archive")
	}
	// Establish that the unchanged archive reaches the latest target and its
	// finalizer before intentionally refusing commit. Later failure cannot be
	// attributed to an already-invalid original archive or missing target DDL.
	refused := errors.New("schema28 control finalizer refusal")
	controlCalled := false
	_, err := RestoreOfflineFinalized(ctx, target, archive, originalFacts, options,
		func(_ context.Context, _ pgx.Tx, result RestoreResult) error {
			controlCalled = true
			if result.SourceVersion != 28 || result.CurrentVersion != 29 || !equalJSON(result.Tables, originalFacts.Tables) {
				return errors.New("schema28 control changed the source or latest target")
			}
			return refused
		})
	if !controlCalled || !errors.Is(err, refused) {
		t.Fatalf("the genuine schema28 archive did not reach its current-schema finalizer: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the original schema28 archive")
	}
	var decoded []byte
	if err := decodeCommand(ctx, options, archive, func(stream io.Reader) error {
		var err error
		decoded, err = io.ReadAll(io.LimitReader(stream, options.MaxDumpBytes+1))
		if err == nil && int64(len(decoded)) > options.MaxDumpBytes {
			err = ErrLimit
		}
		return err
	}); err != nil {
		t.Fatalf("decode the complete real schema28 archive: %v", err)
	}
	if bytes.Count(decoded, []byte("\tuser.updated\t")) != 1 {
		t.Fatal("the decoded archive does not have exactly one selected action field")
	}
	modified := bytes.Replace(decoded, []byte("\tuser.updated\t"), []byte("\tuser.deleted\t"), 1)
	path := filepath.Join(t.TempDir(), "schema28-later-action.decoded")
	if err := os.WriteFile(path, modified, 0o600); err != nil {
		t.Fatal("retain only the test-modified decoded stream")
	}
	catalog, _, err := loadCatalog(28, options.Schema)
	if err != nil {
		t.Fatal("load the unchanged published schema28 catalog")
	}
	var activityTable TableSpec
	for _, table := range catalog.Tables {
		if table.Name == "activity_entries" {
			activityTable = table
		}
	}
	if activityTable.Name == "" {
		t.Fatal("the schema28 catalog omitted activity entries")
	}
	// Recompute the modified row's real PostgreSQL JSON fingerprint through a
	// rollback-only temporary view and the existing fingerprint reader. This
	// performs test-local DDL but no persistent source DDL/data change. Original
	// archive bytes and source facts are never overwritten or relabelled.
	projection, err := source.Begin(ctx)
	if err != nil {
		t.Fatal("begin the private projected-fingerprint fixture")
	}
	defer rollback(projection)
	if err := configureTransaction(ctx, projection, options.Schema); err != nil {
		t.Fatal("use the same PostgreSQL fingerprint formatting as restoration")
	}
	table := qualified(options.Schema, "activity_entries")
	if _, err := projection.Exec(ctx, `CREATE TEMP VIEW activity_entries AS
		SELECT (jsonb_populate_record(NULL::`+table+`,to_jsonb(original)||'{"action":"user.deleted"}'::jsonb)).*
		FROM `+table+` original`); err != nil {
		t.Fatal("project only the modified action without updating the historical source")
	}
	projected, err := fingerprints(ctx, projection, Catalog{Schema: "pg_temp", Tables: []TableSpec{activityTable}})
	if err != nil || len(projected) != 1 || projected[0].Rows != 1 {
		t.Fatalf("fingerprint the complete projected PostgreSQL row: %v", err)
	}
	if err := projection.Rollback(ctx); err != nil {
		t.Fatal("discard the temporary projection before restoring")
	}
	facts := cloneFacts(originalFacts)
	replaced := false
	for index, fact := range facts.Tables {
		if fact.Name == "activity_entries" {
			if fact.Rows != projected[0].Rows || fact.SHA256 == projected[0].SHA256 {
				t.Fatal("the modified action did not receive a distinct matching row fingerprint")
			}
			facts.Tables[index] = projected[0]
			replaced = true
		}
	}
	if !replaced || facts.SchemaVersion != 28 || !equalJSON(facts.MigrationChecksums, originalFacts.MigrationChecksums) || facts.SchemaSHA256 != originalFacts.SchemaSHA256 {
		t.Fatal("the modified data fixture changed its explicit historical source authority")
	}
	modifiedOptions := options
	modifiedOptions.PGRestore = commandFixture(t, fmt.Sprintf(`import os, sys
if sys.argv[1:] == ['--version']:
 os.execv(%s, [%s, '--version'])
else:
 sys.stdin.buffer.read()
 with open(%s, 'rb') as data:
  sys.stdout.buffer.write(data.read())
`, pythonString(options.PGRestore), pythonString(options.PGRestore), pythonString(path)))
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the original container for the modified decoder fixture")
	}
	if err := ValidateDump(ctx, archive, facts, modifiedOptions); err != nil {
		t.Fatalf("the modified data must pass framing and row-count preflight: %v", err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind the retained original container before restoration")
	}
	finalizerCalled := false
	result, err := RestoreOfflineFinalized(ctx, target, archive, facts, modifiedOptions,
		func(context.Context, pgx.Tx, RestoreResult) error { finalizerCalled = true; return nil })
	if !errors.Is(err, ErrArchive) || finalizerCalled || result.SourceVersion != 0 || result.CurrentVersion != 0 || len(result.Tables) != 0 {
		t.Fatalf("a schema28 COPY accepted the later user.deleted action or reached migration/finalization: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if !equalJSON(originalFacts, before) {
		t.Fatal("the rejection fixture overwrote the original archive descriptor")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
