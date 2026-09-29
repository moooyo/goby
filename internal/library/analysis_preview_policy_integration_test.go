//go:build linux

package library

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func (f analysisWorkFixture) setPreviewGeneration(t *testing.T, enabled bool) {
	t.Helper()
	current, err := f.store.GetLibraryEditing(f.ctx, f.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.UpdateLibraryAsAdministrator(f.ctx, f.actor, identity.AdministratorNative, f.library.ID,
		LibraryUpdate{Revision: fmt.Sprint(current.Library.Revision), LibraryOptions: &LibraryOptionsUpdate{EnablePreviewGeneration: &enabled}})
	if err != nil {
		t.Fatal(err)
	}
}

func (f analysisWorkFixture) makePreviewStartupSource(t *testing.T, run string) {
	t.Helper()
	// Library tests retain the existing durable-row fence fixture. Give this
	// unstarted run an exact system startup source and matching trigger scope;
	// the tasks package separately verifies its sealed process capability.
	trigger, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	err = f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind)
			SELECT $1,task_id,1,0,'startup' FROM task_runs WHERE id=$2`, trigger, run); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE task_runs SET source='startup',actor_kind='system',actor_user_id='',actor_session_id='',
			trigger_id=$2,trigger_revision=1,scheduled_for=clock_timestamp() WHERE id=$1`, run, trigger)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisPreviewOptOutStopsAutomaticPublicationAndRetainsReadableVariants(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		t.Run(fmt.Sprint("automatic=", automatic), func(t *testing.T) {
			f := newAnalysisWorkFixture(t, 1)
			f.setPreviewGeneration(t, true)
			run, child := f.admitPreviewSelection(t, AnalysisSelection{LibraryIDs: []string{f.library.ID}, ItemIDs: []string{f.ids[0]}})
			if automatic {
				f.makePreviewStartupSource(t, run)
			}
			f.claim(t, run, child)
			work, err := f.store.GetAnalysisWork(f.ctx, child, f.fence(child))
			if err != nil {
				t.Fatal(err)
			}
			values := analysisWorkPreviewVariants(t, work)
			if err := f.store.PublishAnalysisPreview(f.ctx, child, f.fence(child), values); err != nil {
				t.Fatal("publish initial enabled preview variants", err)
			}
			before, err := f.store.GetAnalysisPreviewsFor(f.ctx, Subject{UserID: f.viewer}, f.ids[0], "")
			if err != nil || len(before) != 3 {
				t.Fatal("initial preview references were not readable", err)
			}
			f.setPreviewGeneration(t, false)
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type,options)
				VALUES('other-preview-library','Other enabled library','movies',
				'{"EnableLocalMetadata":true,"EnableLocalImages":true,"EnableEmbeddedArtwork":true,"EnablePreviewGeneration":true}')`); err != nil {
				t.Fatal("seed an unrelated enabled library", err)
			}
			after, err := f.store.GetAnalysisPreviewsFor(f.ctx, Subject{UserID: f.viewer}, f.ids[0], "")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("opt-out removed or changed already-generated preview references", err)
			}
			for index := range values {
				values[index].CacheKey = fmt.Sprintf("%064x", 10000+index)
			}
			err = f.store.PublishAnalysisPreview(f.ctx, child, f.fence(child), values)
			if automatic && !errors.Is(err, ErrAnalysisPreviewDisabled) || !automatic && err != nil {
				t.Fatalf("publication did not respect its original automatic/manual source: %v", err)
			}
			after, err = f.store.GetAnalysisPreviewsFor(f.ctx, Subject{UserID: f.viewer}, f.ids[0], "")
			if err != nil || len(after) != 3 {
				t.Fatal("opt-out made retained previews unreadable", err)
			}
			if automatic && !reflect.DeepEqual(before, after) {
				t.Fatal("an already-admitted automatic run replaced a preview after opt-out")
			}
			if !automatic && after[0].CacheKey != values[0].CacheKey {
				t.Fatal("compatible manual generation was blocked by the automatic switch")
			}
		})
	}
}

func TestAnalysisConfigurationRequestsOnlyEnabledRebuildsOnCommittedChanges(t *testing.T) {
	for _, selection := range []string{"none", "intro", "preview", "both"} {
		t.Run(selection, func(t *testing.T) {
			f, child, _, _ := analysisWorkPreviewFixture(t)
			if selection == "intro" || selection == "both" {
				f.setIntroDetection(t, true)
			}
			if selection == "preview" || selection == "both" {
				f.setPreviewGeneration(t, true)
			}
			readEvents := func() map[string]int64 {
				t.Helper()
				rows, err := f.pool.Query(f.ctx, `SELECT name,sequence FROM task_system_events ORDER BY name`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				values := map[string]int64{}
				for rows.Next() {
					var name string
					var value int64
					if err := rows.Scan(&name, &value); err != nil {
						t.Fatal(err)
					}
					values[name] = value
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return values
			}
			before := readEvents()
			configuration, err := f.store.GetAnalysisConfiguration(f.ctx, f.actor)
			if err != nil {
				t.Fatal(err)
			}
			profile := configuration.Profile
			profile.PreviewQuality++
			changed, err := f.store.UpdateAnalysisConfiguration(f.ctx, f.actor, AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			after := readEvents()
			for name, previous := range before {
				want := previous
				if name == "LibraryChanged" || name == "IntroAnalysisRequested" && (selection == "intro" || selection == "both") ||
					name == "PreviewGenerationRequested" && (selection == "preview" || selection == "both") {
					want++
				}
				if after[name] != want {
					t.Fatalf("profile change changed %s counter to %d, want %d", name, after[name], want)
				}
			}
			var references int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_previews`).Scan(&references); err != nil || references != 0 {
				t.Fatal("profile change retained stale preview references", err)
			}
			if _, err := f.store.GetAnalysisWork(f.ctx, child, f.fence(child)); !errors.Is(err, ErrAnalysisConflict) {
				t.Fatal("profile change let old work retain publication authority", err)
			}
			if _, err := f.store.UpdateAnalysisConfiguration(f.ctx, f.actor, AnalysisConfigurationUpdate{Revision: changed.Revision, Profile: changed.Profile}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(readEvents(), after) {
				t.Fatal("a no-op profile write scheduled redundant rebuilds")
			}
		})
	}
}

func TestAnalysisConfigurationRebuildRequestFailureRollsBackProfileAndReferences(t *testing.T) {
	for _, mode := range []string{"event_failure", "final_authority"} {
		t.Run(mode, func(t *testing.T) {
			assertAnalysisRebuildRollback(t, mode)
		})
	}
}

func assertAnalysisRebuildRollback(t *testing.T, mode string) {
	t.Helper()
	f, _, _, _ := analysisWorkPreviewFixture(t)
	f.setIntroDetection(t, true)
	f.setPreviewGeneration(t, true)
	const witness = `SELECT jsonb_build_object(
		'settings',(SELECT to_jsonb(s) FROM analysis_settings s),
		'previews',(SELECT jsonb_agg(to_jsonb(p) ORDER BY item_id,width) FROM analysis_previews p),
		'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY name) FROM task_system_events e))::text`
	var before, after string
	if err := f.pool.QueryRow(f.ctx, witness).Scan(&before); err != nil {
		t.Fatal(err)
	}
	statement := `CREATE FUNCTION reject_preview_rebuild_request() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.name='PreviewGenerationRequested' THEN RAISE EXCEPTION 'fixture rejects rebuild request'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER reject_preview_rebuild_request BEFORE UPDATE ON task_system_events
		FOR EACH ROW EXECUTE FUNCTION reject_preview_rebuild_request()`
	if mode == "final_authority" {
		statement = `CREATE FUNCTION reject_preview_rebuild_request() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN IF NEW.name='PreviewGenerationRequested' THEN
			UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id='analysis-work-editor';
			END IF; RETURN NEW; END $$;
			CREATE TRIGGER reject_preview_rebuild_request AFTER UPDATE ON task_system_events
			FOR EACH ROW EXECUTE FUNCTION reject_preview_rebuild_request()`
	}
	if _, err := f.pool.Exec(f.ctx, statement); err != nil {
		t.Fatal(err)
	}
	configuration, err := f.store.GetAnalysisConfiguration(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	profile := configuration.Profile
	profile.PreviewQuality++
	_, err = f.store.UpdateAnalysisConfiguration(f.ctx, f.actor, AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: profile})
	if err == nil || mode == "final_authority" && !errors.Is(err, ErrForbidden) {
		t.Fatal("profile change ignored its failed rebuild signal or final authority", err)
	}
	if err := f.pool.QueryRow(f.ctx, witness).Scan(&after); err != nil || after != before {
		t.Fatal("failed rebuild signal committed profile, reference deletion, or another event", err)
	}
}
