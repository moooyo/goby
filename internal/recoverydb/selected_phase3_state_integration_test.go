//go:build linux

package recoverydb

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/lifecycle"
)

// Reuse the exclusive recovery pair. Every mutation preserves roster authority
// and normalized import content, so a stale reset proof must fail on its exact
// durable fingerprint rather than on an invalid episode roster.
func assertRecoverySelectedPhase3ResetProtection(t *testing.T, f *recoveryStoreFixture, original Retained) {
	t.Helper()
	const seriesID = "reset-phase3-series"
	imports := []struct {
		revision  int64
		payload   []byte
		active    bool
		createdAt string
		edit      library.EpisodeRosterEdit
		digest    string
	}{
		{
			revision:  9007199254740993,
			payload:   []byte(`{"ParserVersion":1,"Source":{"Key":"reset-local-source","Label":"Reset roster witness","Revision":"edition-1"},"Entries":[{"Key":"retired-entry","SeasonNumber":1,"EpisodeNumber":1,"Name":"Retained retired episode","PremiereDate":"2025-01-01"}]}`),
			active:    false,
			createdAt: "2025-01-12T00:00:00.123456Z",
		},
		{
			revision:  9007199254740994,
			payload:   []byte(`{"ParserVersion":1,"Source":{"Key":"reset-local-source","Label":"Reset roster witness","Revision":"edition-2"},"Entries":[{"Key":"active-entry","SeasonNumber":1,"EpisodeNumber":3,"Name":"Retained active episode"}]}`),
			active:    true,
			createdAt: "2025-01-13T00:00:00.234567Z",
		},
	}
	for index := range imports {
		edit, digest, err := library.ParseEpisodeRosterPayload(imports[index].payload)
		if err != nil || len(edit.Entries) != 1 {
			t.Fatal("prepare the exact normalized episode roster reset witness")
		}
		imports[index].edit, imports[index].digest = edit, digest
	}
	current := imports[len(imports)-1]
	f.exec(t, f.source, `INSERT INTO libraries(id,name,collection_type)
		VALUES('reset-phase3-library','Episode roster reset witness','tvshows');
		INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES('reset-phase3-series','reset-phase3-library','Episode roster reset witness','episode roster reset witness','Series',true)`)
	f.exec(t, f.source, `INSERT INTO series_episode_rosters(series_id,revision,state,source_key,source_label,source_revision,
		parser_version,payload_sha256,last_edited_by,created_at,updated_at)
		VALUES($1,$2,'active',$3,$4,$5,$6,$7,'recovery-admin',$8::timestamptz,$9::timestamptz)`,
		seriesID, current.revision, current.edit.Source.Key, current.edit.Source.Label, current.edit.Source.Revision,
		library.EpisodeRosterParserVersion, current.digest, imports[0].createdAt, current.createdAt)
	for _, imported := range imports {
		f.exec(t, f.source, `INSERT INTO episode_roster_imports(series_id,revision,action,source_key,source_label,source_revision,
			parser_version,payload,payload_sha256,actor_id,created_at)
			VALUES($1,$2,'replace',$3,$4,$5,$6,$7,$8,'recovery-admin',$9::timestamptz)`,
			seriesID, imported.revision, imported.edit.Source.Key, imported.edit.Source.Label, imported.edit.Source.Revision,
			library.EpisodeRosterParserVersion, imported.payload, imported.digest, imported.createdAt)
		entry := imported.edit.Entries[0]
		var retiredAt any
		if !imported.active {
			retiredAt = current.createdAt
		}
		f.exec(t, f.source, `INSERT INTO expected_episodes(id,series_id,source_key,entry_key,season_number,episode_number,
			name,premiere_date,active,import_revision,created_at,updated_at,retired_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,$9,$10,$11::timestamptz,$12::timestamptz,$13::timestamptz)`,
			library.ExpectedEpisodeID(seriesID, imported.edit.Source.Key, entry.Key), seriesID, imported.edit.Source.Key, entry.Key,
			entry.SeasonNumber, entry.EpisodeNumber, entry.Name, entry.PremiereDate, imported.active, imported.revision,
			imported.createdAt, current.createdAt, retiredAt)
	}

	observe := func(name, table, mutation, undo string) {
		t.Helper()
		if !t.Run(name, func(t *testing.T) {
			before := f.capture(t, f.source)
			f.exec(t, f.source, mutation)
			after := f.capture(t, f.source)
			if before.RawMarker != after.RawMarker || before.Marker != after.Marker || len(before.Facts.Tables) != len(after.Facts.Tables) {
				t.Fatal("an expected episode mutation changed the local generation or catalog inventory")
			}
			changed := false
			for index, previous := range before.Facts.Tables {
				current := after.Facts.Tables[index]
				if reflect.DeepEqual(previous, current) {
					continue
				}
				if previous.Name != table || current.Name != table || current.Rows != previous.Rows || changed {
					t.Fatal("an expected episode reset witness changed an unexpected durable table or row count")
				}
				changed = true
			}
			if !changed {
				t.Fatalf("the retained reset proof omitted a microsecond change in %s", table)
			}
			if err := f.source.store.ResetOwnedTarget(f.ctx,
				recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), before); !errors.Is(err, ErrConflict) {
				t.Fatalf("a stale expected episode fingerprint authorized an owned target reset: %v", err)
			}
			f.assertFacts(t, f.source, after.Facts)
			f.assertNamespace(t, f.source, false)
			f.assertEmpty(t, f.target)
			f.exec(t, f.source, undo)
			f.assertFacts(t, f.source, before.Facts)
		}) {
			t.Fatal("stop the exclusive recovery sequence after an inexact expected episode reset result")
		}
	}
	observe("roster_timestamp_microsecond", "series_episode_rosters",
		`UPDATE series_episode_rosters SET updated_at=updated_at+interval '1 microsecond' WHERE series_id='reset-phase3-series'`,
		`UPDATE series_episode_rosters SET updated_at=updated_at-interval '1 microsecond' WHERE series_id='reset-phase3-series'`)
	observe("historical_import_timestamp_microsecond", "episode_roster_imports",
		`UPDATE episode_roster_imports SET created_at=created_at+interval '1 microsecond' WHERE series_id='reset-phase3-series' AND revision=9007199254740993`,
		`UPDATE episode_roster_imports SET created_at=created_at-interval '1 microsecond' WHERE series_id='reset-phase3-series' AND revision=9007199254740993`)
	observe("active_fact_timestamp_microsecond", "expected_episodes",
		`UPDATE expected_episodes SET updated_at=updated_at+interval '1 microsecond' WHERE series_id='reset-phase3-series' AND active`,
		`UPDATE expected_episodes SET updated_at=updated_at-interval '1 microsecond' WHERE series_id='reset-phase3-series' AND active`)
	observe("retired_fact_timestamp_microsecond", "expected_episodes",
		`UPDATE expected_episodes SET updated_at=updated_at+interval '1 microsecond' WHERE series_id='reset-phase3-series' AND NOT active`,
		`UPDATE expected_episodes SET updated_at=updated_at-interval '1 microsecond' WHERE series_id='reset-phase3-series' AND NOT active`)
	f.exec(t, f.source, `DELETE FROM libraries WHERE id='reset-phase3-library'`)
	f.assertFacts(t, f.source, original.Facts)
	if f.read(t, f.source) != original.RawMarker {
		t.Fatal("expected episode reset protection changed the retained local generation")
	}
}
