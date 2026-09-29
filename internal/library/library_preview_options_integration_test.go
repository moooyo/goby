package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func previewScanRequestSequence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var sequence int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='PreviewGenerationRequested'`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func TestLibraryPreviewPolicyPersistsAndRequestsOnlyNewOptIns(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "preview-policy-admin")
	enabled, disabled := true, false
	initialIntro := introScanRequestSequence(t, ctx, pool)
	for _, kind := range []string{"movies", "tvshows", "mixed"} {
		path := filepath.Dir(libraryIntegrationFile(t, approved, "preview-policy-"+kind+"/Video.mp4", "video:preview-policy"))
		before := previewScanRequestSequence(t, ctx, pool)
		created, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
			"Preview policy "+kind, kind, []string{path}, DefaultLibraryOptions())
		if err != nil || created.Options == nil || created.Options.EnablePreviewGeneration || previewScanRequestSequence(t, ctx, pool) != before {
			t.Fatalf("disabled library created preview work: %+v %v", created, err)
		}
		update := LibraryUpdate{Revision: created.Revision, LibraryOptions: &LibraryOptionsUpdate{EnablePreviewGeneration: &enabled}}
		selected, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
		if err != nil || !selected.Library.Options.EnablePreviewGeneration || selected.Library.Options.EnableIntroDetection || previewScanRequestSequence(t, ctx, pool) != before+1 {
			t.Fatalf("enabling failed to atomically publish preview policy and request: %+v %v", selected, err)
		}
		var explicit bool
		if err := pool.QueryRow(ctx, `SELECT options->'EnablePreviewGeneration'='true'::jsonb FROM libraries WHERE id=$1`, created.ID).Scan(&explicit); err != nil || !explicit {
			t.Fatalf("enabled preview policy was not persisted: %v", err)
		}
		update.Revision = selected.Library.Revision
		unchanged, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
		if err != nil || unchanged.Library.Revision != selected.Library.Revision || previewScanRequestSequence(t, ctx, pool) != before+1 {
			t.Fatalf("unchanged preview policy created another request: %+v %v", unchanged, err)
		}
		update.Revision = created.Revision
		update.LibraryOptions.EnablePreviewGeneration = &disabled
		if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update); !errors.Is(err, ErrLibraryConflict) || previewScanRequestSequence(t, ctx, pool) != before+1 {
			t.Fatalf("stale preview policy changed the request counter: %v", err)
		}
		update.Revision = selected.Library.Revision
		cleared, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, created.ID, update)
		if err != nil || cleared.Library.Options.EnablePreviewGeneration || previewScanRequestSequence(t, ctx, pool) != before+1 {
			t.Fatalf("disabling must persist without queuing preview work: %+v %v", cleared, err)
		}
		options := DefaultLibraryOptions()
		options.EnablePreviewGeneration = true
		other := filepath.Dir(libraryIntegrationFile(t, approved, "preview-enabled-"+kind+"/Video.mp4", "video:preview-enabled"))
		second, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
			"Enabled previews "+kind, kind, []string{other}, options)
		if err != nil || !second.Options.EnablePreviewGeneration || previewScanRequestSequence(t, ctx, pool) != before+2 {
			t.Fatalf("creation opt-in did not request preview work: %+v %v", second, err)
		}
	}
	if got := introScanRequestSequence(t, ctx, pool); got != initialIntro {
		t.Fatalf("preview policy changed the separate intro request counter: %d want %d", got, initialIntro)
	}
}

func TestPreviewGenerationRequestedAfterCompletedScans(t *testing.T) {
	for _, kind := range []string{"movies", "tvshows", "mixed"} {
		for _, enabled := range []bool{false, true} {
			for _, owned := range []bool{false, true} {
				name := kind + "/disabled"
				if enabled {
					name = kind + "/enabled"
				}
				if owned {
					name += "/task-owned"
				} else {
					name += "/independent"
				}
				t.Run(name, func(t *testing.T) {
					ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
					file := libraryIntegrationFile(t, root, "preview-scan/Series/Season 01/Series.S01E01.mp4", "video:preview-trigger")
					actor := metadataEditTestActor(t, ctx, pool, "preview-trigger-admin")
					options := DefaultLibraryOptions()
					options.EnablePreviewGeneration = enabled
					collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
						"Preview scans", kind, []string{filepath.Dir(filepath.Dir(filepath.Dir(file)))}, options)
					if err != nil {
						t.Fatal(err)
					}
					before := previewScanRequestSequence(t, ctx, pool)
					introBefore := introScanRequestSequence(t, ctx, pool)
					var finished Job
					if owned {
						_, children := taskScanFixture(t, ctx, pool, collection)
						admission, err := store.AdmitTaskScan(ctx, children[0])
						if err != nil || admission.Kind != ScanAdmitted {
							t.Fatalf("admit task-owned preview scan: %+v %v", admission, err)
						}
						finished = libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
					} else {
						finished = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
					}
					want := before
					if enabled {
						want++
					}
					if got := previewScanRequestSequence(t, ctx, pool); got != want {
						t.Fatalf("completed scan preview request=%d, want %d", got, want)
					}
					if err := store.finishTask(&scanTask{ctx: ctx, job: finished}, "Completed", ""); err != nil {
						t.Fatal(err)
					}
					if got := previewScanRequestSequence(t, ctx, pool); got != want {
						t.Fatalf("repeated finalization emitted another preview request: %d, want %d", got, want)
					}
					if got := introScanRequestSequence(t, ctx, pool); got != introBefore {
						t.Fatalf("preview request leaked into intro scheduling: %d want %d", got, introBefore)
					}
				})
			}
		}
	}
}

func TestCancelledScanDoesNotRequestPreviewGeneration(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	file := libraryIntegrationFile(t, root, "preview-cancel/Series/Season 01/Series.S01E01.mp4", "video:preview-cancel")
	actor := metadataEditTestActor(t, ctx, pool, "preview-cancel-admin")
	options := DefaultLibraryOptions()
	options.EnablePreviewGeneration = true
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"Preview cancellation", "tvshows", []string{filepath.Dir(filepath.Dir(filepath.Dir(file)))}, options)
	if err != nil {
		t.Fatal(err)
	}
	before := previewScanRequestSequence(t, ctx, pool)
	_, children := taskScanFixture(t, ctx, pool, collection)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit task-owned preview scan: %+v %v", admission, err)
	}
	taskScanAwaitSignal(t, ctx, prober.entered, "scan did not enter the prober")
	if err := store.CancelTaskScan(ctx, children[0]); err != nil {
		t.Fatal(err)
	}
	taskScanAwaitSignal(t, ctx, prober.cancelled, "scan cancellation did not reach the prober")
	libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Cancelled")
	if got := previewScanRequestSequence(t, ctx, pool); got != before {
		t.Fatalf("cancelled scan emitted a preview request: %d, want %d", got, before)
	}
}
