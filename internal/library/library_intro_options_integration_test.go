package library

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestLibraryIntroPolicyPersistsAndRequestsOnlyNewOptIns(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "intro-policy-admin")
	sequence := func() int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='IntroAnalysisRequested'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	initial := sequence()
	path := filepath.Dir(libraryIntegrationFile(t, approved, "intro-policy/Episode.mp4", "video:intro-policy"))
	created, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"TV intro policy", "tvshows", []string{path}, DefaultLibraryOptions())
	if err != nil || created.Options == nil || created.Options.EnableIntroDetection || sequence() != initial {
		t.Fatalf("disabled library created work: %+v %v", created, err)
	}
	enabled, disabled := true, false
	update := LibraryUpdate{Revision: created.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableIntroDetection: &enabled}}
	selected, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
	if err != nil || !selected.Library.Options.EnableIntroDetection || sequence() != initial+1 {
		t.Fatalf("enabling did not atomically publish its policy and request: %+v %v", selected, err)
	}
	var explicit bool
	if err := pool.QueryRow(ctx, `SELECT options->'EnableIntroDetection'='true'::jsonb FROM libraries WHERE id=$1`, created.ID).Scan(&explicit); err != nil || !explicit {
		t.Fatalf("enabled policy was not persisted: %v", err)
	}
	update.Revision = selected.Library.Revision
	unchanged, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
	if err != nil || unchanged.Library.Revision != selected.Library.Revision || sequence() != initial+1 {
		t.Fatalf("unchanged opt-in created another request: %+v %v", unchanged, err)
	}
	update.Revision = created.Revision
	update.LibraryOptions.EnableIntroDetection = &disabled
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update); !errors.Is(err, ErrLibraryConflict) || sequence() != initial+1 {
		t.Fatalf("stale policy edit changed the request counter: %v", err)
	}
	update.Revision = selected.Library.Revision
	cleared, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
	if err != nil || cleared.Library.Options.EnableIntroDetection || sequence() != initial+1 {
		t.Fatalf("disabling must persist without queuing intro work: %+v %v", cleared, err)
	}
	options := DefaultLibraryOptions()
	options.EnableIntroDetection = true
	other := filepath.Dir(libraryIntegrationFile(t, approved, "intro-policy-second/Episode.mp4", "video:intro-policy-second"))
	second, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"Enabled TV", "tvshows", []string{other}, options)
	if err != nil || !second.Options.EnableIntroDetection || sequence() != initial+2 {
		t.Fatalf("creation opt-in did not request automatic work: %+v %v", second, err)
	}
	for _, kind := range []string{"movies", "mixed"} {
		if _, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
			"Unsupported intro", kind, []string{path}, options); !errors.Is(err, ErrInvalidInput) || sequence() != initial+2 {
			t.Fatalf("unsupported library policy created work: %s %v", kind, err)
		}
	}
}
