//go:build linux

package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
)

const selectedPhase3ArchiveRevision int64 = 9007199254740993

const selectedPhase3ArchiveStateSQL = `SELECT jsonb_build_object(
	'rosters',(SELECT jsonb_agg(to_jsonb(r) ORDER BY series_id) FROM series_episode_rosters r),
	'imports',(SELECT jsonb_agg(to_jsonb(i) ORDER BY series_id,revision) FROM episode_roster_imports i),
	'facts',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM expected_episodes e))::text`

func selectedPhase3ArchiveWitness(t *testing.T, ctx context.Context, source selectedPhase2TransactionSource, schema string) string {
	t.Helper()
	tx, err := source.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the exact episode roster witness: %v", err)
	}
	defer rollback(tx)
	if err := configureTransaction(ctx, tx, schema); err != nil {
		t.Fatalf("configure the exact episode roster witness: %v", err)
	}
	var state string
	if err := tx.QueryRow(ctx, selectedPhase3ArchiveStateSQL).Scan(&state); err != nil {
		t.Fatalf("read complete roster source history and facts: %v", err)
	}
	return state
}

func selectedPhase3ArchivePayload(t *testing.T, source library.EpisodeRosterSourceInput, entries []library.EpisodeRosterEntryInput) ([]byte, string) {
	t.Helper()
	payload, err := json.Marshal(struct {
		ParserVersion int                               `json:"ParserVersion"`
		Source        library.EpisodeRosterSourceInput  `json:"Source"`
		Entries       []library.EpisodeRosterEntryInput `json:"Entries"`
	}{1, source, entries})
	if err != nil {
		t.Fatal("encode the explicit episode roster witness")
	}
	_, digest, err := library.ParseEpisodeRosterPayload(payload)
	if err != nil {
		t.Fatal("the source witness must use the production canonical parser")
	}
	return payload, digest
}

func seedSelectedPhase3ArchiveWitness(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type)
		VALUES('roster-library','Explicit episode archive','tvshows');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES('roster-root','roster-library','/synthetic/rosters','/synthetic','rosters');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path)
		VALUES('roster-active','roster-library','roster-root','Active series','active series','Series',true,'Active'),
		('roster-withdrawn','roster-library','roster-root','Withdrawn series','withdrawn series','Series',true,'Withdrawn')`); err != nil {
		t.Fatalf("seed independent physical series identities: %v", err)
	}
	for _, series := range []struct {
		id        string
		withdrawn bool
	}{{"roster-active", false}, {"roster-withdrawn", true}} {
		oldSource := library.EpisodeRosterSourceInput{Key: "local-source", Label: "Retained source label", Revision: "edition-one"}
		oldEntries := []library.EpisodeRosterEntryInput{{Key: "retired", SeasonNumber: 1, EpisodeNumber: 1, Name: "Retained first episode", PremiereDate: "2020-01-01"}}
		oldPayload, oldHash := selectedPhase3ArchivePayload(t, oldSource, oldEntries)
		currentSource := oldSource
		currentEntries := oldEntries
		state, action := "withdrawn", "withdraw"
		if !series.withdrawn {
			state, action = "active", "replace"
			currentSource.Revision = "edition-two"
			currentEntries = []library.EpisodeRosterEntryInput{
				{Key: "unknown", SeasonNumber: 1, EpisodeNumber: 2, Name: ""},
				{Key: "future", SeasonNumber: 1, EpisodeNumber: 4, Name: "Declared future episode", PremiereDate: "9999-12-31"},
			}
		}
		currentPayload, currentHash := selectedPhase3ArchivePayload(t, currentSource, currentEntries)
		if _, err := pool.Exec(ctx, `INSERT INTO series_episode_rosters(series_id,revision,state,source_key,source_label,source_revision,
			parser_version,payload_sha256,last_edited_by,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,1,$7,'deleted-historical-admin','2020-01-01T00:00:00.123456Z','2020-01-03T00:00:00.234567Z')`,
			series.id, selectedPhase3ArchiveRevision+1, state, currentSource.Key, currentSource.Label, currentSource.Revision, currentHash); err != nil {
			t.Fatalf("seed the current roster pointer: %v", err)
		}
		for _, history := range []struct {
			revision int64
			action   string
			source   library.EpisodeRosterSourceInput
			payload  []byte
			hash     string
		}{
			{selectedPhase3ArchiveRevision, "replace", oldSource, oldPayload, oldHash},
			{selectedPhase3ArchiveRevision + 1, action, currentSource, currentPayload, currentHash},
		} {
			if _, err := pool.Exec(ctx, `INSERT INTO episode_roster_imports(series_id,revision,action,source_key,source_label,source_revision,
				parser_version,payload,payload_sha256,actor_id,created_at)
				VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,'deleted-historical-admin','2020-01-02T00:00:00.345678Z')`,
				series.id, history.revision, history.action, history.source.Key, history.source.Label, history.source.Revision, history.payload, history.hash); err != nil {
				t.Fatalf("seed immutable canonical source history: %v", err)
			}
		}
		insertFact := func(entry library.EpisodeRosterEntryInput, active bool, revision int64) {
			t.Helper()
			if _, err := pool.Exec(ctx, `INSERT INTO expected_episodes(id,series_id,source_key,entry_key,season_number,episode_number,name,
				premiere_date,active,import_revision,created_at,updated_at,retired_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,$9,$10,'2020-01-02T00:00:00.456789Z','2020-01-03T00:00:00.567890Z',
				CASE WHEN $9 THEN NULL ELSE '2020-01-03T00:00:00.567890Z'::timestamptz END)`,
				library.ExpectedEpisodeID(series.id, oldSource.Key, entry.Key), series.id, oldSource.Key, entry.Key,
				entry.SeasonNumber, entry.EpisodeNumber, entry.Name, entry.PremiereDate, active, revision); err != nil {
				t.Fatalf("seed source-bound active and retired facts: %v", err)
			}
		}
		insertFact(oldEntries[0], false, selectedPhase3ArchiveRevision)
		if !series.withdrawn {
			for _, entry := range currentEntries {
				insertFact(entry, true, selectedPhase3ArchiveRevision+1)
			}
		}
	}
}

func TestPostgreSQLSelectedPhase3ArchivePreservesHistoryAndRejectsInvalidFinalization(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	seedSelectedPhase3ArchiveWitness(t, ctx, source)
	want := selectedPhase3ArchiveWitness(t, ctx, source, options.Schema)
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	counts := make(map[string]int64)
	for _, table := range facts.Tables {
		counts[table.Name] = table.Rows
	}
	for table, count := range map[string]int64{"series_episode_rosters": 2, "episode_roster_imports": 4, "expected_episodes": 4} {
		if counts[table] != count {
			t.Fatalf("archive omitted durable roster table %s", table)
		}
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	called := false
	failed, err := RestoreOfflineFinalized(ctx, target, archive, facts, offline, func(ctx context.Context, tx pgx.Tx, raw RestoreResult) error {
		called = true
		if !equalJSON(raw.Tables, facts.Tables) || selectedPhase3ArchiveWitness(t, ctx, tx, options.Schema) != want {
			return errors.New("raw roster state changed before finalization")
		}
		_, err := tx.Exec(ctx, `UPDATE expected_episodes SET premiere_date='2020-01-01' WHERE series_id='roster-active' AND entry_key='unknown'`)
		return err
	})
	if !called || !errors.Is(err, ErrSchema) || failed.CurrentVersion != 0 {
		t.Fatalf("finalizer committed an inferred premiere date: %v", err)
	}
	assertThemeRestoreTargetEmpty(t, ctx, target, options.Schema)
	if _, err := archive.Seek(0, 0); err != nil {
		t.Fatal("rewind the exact rejected roster archive")
	}
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore the unchanged roster archive after rollback: %v", err)
	}
	if selectedPhase3ArchiveWitness(t, ctx, target, options.Schema) != want {
		t.Fatal("raw restore changed explicit facts, unknown dates, payload bytes, actor history or exact revisions")
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, restoredSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	if !equalJSON(after, facts) || !reflect.DeepEqual(restoredSequences, sequences) {
		t.Fatal("raw roster restoration changed complete fingerprints or sequence state")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLSelectedPhase3StateRejectsSQLValidInconsistency(t *testing.T) {
	ctx, source, _, options := recoveryFixture(t)
	seedSelectedPhase3ArchiveWitness(t, ctx, source)
	want := selectedPhase3ArchiveWitness(t, ctx, source, options.Schema)
	for _, fixture := range []struct{ name, mutation string }{
		{"payload_hash", `UPDATE episode_roster_imports SET payload_sha256=repeat('0',64) WHERE series_id='roster-active' AND revision=9007199254740993`},
		{"canonical_payload", `UPDATE episode_roster_imports SET payload=decode('20','hex')||payload,payload_sha256=encode(sha256(decode('20','hex')||payload),'hex') WHERE series_id='roster-active' AND revision=9007199254740993`},
		{"current_pointer", `UPDATE series_episode_rosters SET revision=9007199254740995 WHERE series_id='roster-active'`},
		{"source_identity", `UPDATE series_episode_rosters SET source_label='Changed declaration' WHERE series_id='roster-active'`},
		{"stable_id", `UPDATE expected_episodes SET id='missing-'||repeat('0',32) WHERE series_id='roster-active' AND entry_key='unknown'`},
		{"retired_payload", `UPDATE expected_episodes SET name='Invented historical title' WHERE series_id='roster-active' AND entry_key='retired'`},
		{"active_coverage", `DELETE FROM expected_episodes WHERE series_id='roster-active' AND entry_key='unknown'`},
		{"retired_coverage", `DELETE FROM expected_episodes WHERE series_id='roster-active' AND entry_key='retired'`},
		{"withdrawn_authority", `UPDATE expected_episodes SET active=true,retired_at=NULL WHERE series_id='roster-withdrawn'`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tx, err := source.Begin(ctx)
			if err != nil {
				t.Fatal("begin isolated invalid roster witness")
			}
			defer rollback(tx)
			if err := configureTransaction(ctx, tx, options.Schema); err != nil {
				t.Fatal(err)
			}
			if tag, err := tx.Exec(ctx, fixture.mutation); err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("apply a SQL-valid semantic corruption: %v", err)
			}
			if err := validateSelectedPhase3State(ctx, tx, 46); !errors.Is(err, ErrSchema) {
				t.Fatalf("admitted inconsistent roster state: %v", err)
			}
		})
	}
	if selectedPhase3ArchiveWitness(t, ctx, source, options.Schema) != want {
		t.Fatal("invalid-state admission checks changed the source archive witness")
	}
}

func TestPostgreSQLSelectedPhase3Schema45UpgradeDoesNotInferEpisodes(t *testing.T) {
	ctx, source, target, options := recoveryFixtureAtVersion(t, 45)
	seedThemeSnapshotWitness(t, ctx, source)
	if _, err := source.Exec(ctx, `INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path)
		SELECT 'historical-series',library_id,root_id,'Numbered gaps','numbered gaps','Series',true,'Numbered gaps' FROM items WHERE id='theme-owner';
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,index_number,parent_index_number,relative_path)
		SELECT 'historical-episode-'||number,library_id,root_id,id,'Episode '||number,'episode '||number,'Episode',number,1,'Numbered gaps/'||number||'.mkv'
		FROM items CROSS JOIN (VALUES(1),(3)) numbers(number) WHERE id='historical-series'`); err != nil {
		t.Fatalf("seed historical physical numbering gaps: %v", err)
	}
	archive, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	if facts.SchemaVersion != 45 || len(facts.Tables) != 53 {
		t.Fatal("the roster upgrade fixture is not the published schema45")
	}
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	result, err := RestoreOffline(ctx, target, archive, facts, offline)
	if err != nil || result.SourceVersion != 45 || result.CurrentVersion != currentRecoveryVersion(t) || !equalJSON(result.Tables, facts.Tables) {
		t.Fatalf("restore historical schema45 with numbering gaps: %v", err)
	}
	var empty bool
	if err := target.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM series_episode_rosters) AND NOT EXISTS(SELECT 1 FROM episode_roster_imports) AND NOT EXISTS(SELECT 1 FROM expected_episodes)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("historical restoration inferred missing episode authority: %v", err)
	}
	targetOptions := options
	targetOptions.SourceURL = target.Config().ConnString()
	after, targetSequences := unchangedSourceWitness(t, ctx, target, targetOptions)
	assertHistoricalRecoveryFacts(t, ctx, source, target, facts, after, sequences, targetSequences)
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
