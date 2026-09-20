package library

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestAnalysisConfigurationCASIsIndependentAndNoopStable(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "analysis-config-editor")
	initial, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil || initial.Revision != "1" || initial.Profile != DefaultAnalysisProfile() || initial.Defaults != DefaultAnalysisProfile() {
		t.Fatalf("read initial analysis profile: %+v: %v", initial, err)
	}
	var serverRevisionBefore, serverRevisionAfter int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM managed_settings WHERE id=1`).Scan(&serverRevisionBefore); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: initial.Revision, Profile: initial.Profile})
	if err != nil || !reflect.DeepEqual(unchanged, initial) {
		t.Fatalf("no-op changed analysis revision or timestamp: %+v: %v", unchanged, err)
	}
	profile := initial.Profile
	profile.PreviewQuality++
	changed, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: initial.Revision, Profile: profile})
	if err != nil || changed.Revision != "2" || changed.Profile != profile || changed.Defaults != initial.Defaults {
		t.Fatalf("analysis CAS failed: %+v: %v", changed, err)
	}
	if result, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: initial.Revision, Profile: initial.Profile}); !errors.Is(err, ErrAnalysisConflict) || result != (AnalysisConfiguration{}) {
		t.Fatalf("stale analysis CAS returned a usable result: %+v: %v", result, err)
	}
	if err := pool.QueryRow(ctx, `SELECT revision FROM managed_settings WHERE id=1`).Scan(&serverRevisionAfter); err != nil || serverRevisionBefore != serverRevisionAfter {
		t.Fatalf("analysis configuration changed the unrelated server revision: %v", err)
	}
	read, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil || !reflect.DeepEqual(read, changed) {
		t.Fatalf("committed analysis profile was not durable: %+v: %v", read, err)
	}
}

func TestAnalysisConfigurationConcurrentCASHasOneWinner(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "analysis-concurrent-editor")
	initial, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, quality := range []int{81, 82} {
		workers.Add(1)
		go func(quality int) {
			defer workers.Done()
			<-start
			profile := initial.Profile
			profile.PreviewQuality = quality
			_, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: initial.Revision, Profile: profile})
			results <- err
		}(quality)
	}
	close(start)
	workers.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrAnalysisConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent analysis CAS: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("analysis CAS had %d winners and %d conflicts", winners, conflicts)
	}
}

func TestAnalysisConfigurationFinalAuthorityFailureRollsBack(t *testing.T) {
	ctx, pool, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "analysis-final-authority-editor")
	initial, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	// Revocation inside the owned write exercises the final fresh authority check.
	// Rollback must undo the profile and the trigger's temporary credential edit.
	if _, err := pool.Exec(ctx, `CREATE FUNCTION revoke_analysis_configuration_actor() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id='analysis-final-authority-editor'; RETURN NEW; END $$;
	CREATE TRIGGER revoke_analysis_configuration_actor AFTER UPDATE ON analysis_settings
	FOR EACH ROW EXECUTE FUNCTION revoke_analysis_configuration_actor()`); err != nil {
		t.Fatal(err)
	}
	profile := initial.Profile
	profile.AutoPublishIntros = false
	if result, err := store.UpdateAnalysisConfiguration(ctx, actor, AnalysisConfigurationUpdate{Revision: initial.Revision, Profile: profile}); !errors.Is(err, ErrForbidden) || result != (AnalysisConfiguration{}) {
		t.Fatalf("revoked authority committed an analysis profile: %+v: %v", result, err)
	}
	read, err := store.GetAnalysisConfiguration(ctx, actor)
	if err != nil || !reflect.DeepEqual(read, initial) {
		t.Fatalf("failed final authority check did not roll back every write: %+v: %v", read, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if result, err := store.GetAnalysisConfiguration(ctx, actor); !errors.Is(err, ErrForbidden) || result != (AnalysisConfiguration{}) {
		t.Fatalf("revoked credential read analysis settings: %+v: %v", result, err)
	}
}
