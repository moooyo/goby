package library

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func introScanRequestSequence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var sequence int64
	if err := pool.QueryRow(ctx, `SELECT sequence FROM task_system_events WHERE name='IntroAnalysisRequested'`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	return sequence
}

func TestIntroDetectionRequestedAfterCompletedScans(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			name := "disabled"
			if enabled {
				name = "enabled"
			}
			if owned {
				name += "/task-owned"
			} else {
				name += "/independent"
			}
			t.Run(name, func(t *testing.T) {
				ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
				file := libraryIntegrationFile(t, root, "Shows/Series/Season 01/Series.S01E01.mp4", "video:intro-trigger")
				actor := metadataEditTestActor(t, ctx, pool, "intro-trigger-admin")
				options := DefaultLibraryOptions()
				options.EnableIntroDetection = enabled
				collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
					"Shows", "tvshows", []string{filepath.Dir(filepath.Dir(filepath.Dir(file)))}, options)
				if err != nil {
					t.Fatal(err)
				}
				before := introScanRequestSequence(t, ctx, pool)
				var finished Job
				if owned {
					_, children := taskScanFixture(t, ctx, pool, collection)
					admission, err := store.AdmitTaskScan(ctx, children[0])
					if err != nil || admission.Kind != ScanAdmitted {
						t.Fatalf("admit task-owned scan: %+v %v", admission, err)
					}
					finished = libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Completed")
				} else {
					finished = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
				}
				want := before
				if enabled {
					want++
				}
				if got := introScanRequestSequence(t, ctx, pool); got != want {
					t.Fatalf("completed scan signal=%d, want %d", got, want)
				}
				// An uncertain commit can retry finalization. Its terminal state
				// must prevent a second event and duplicate background analysis.
				if err := store.finishTask(&scanTask{ctx: ctx, job: finished}, "Completed", ""); err != nil {
					t.Fatal(err)
				}
				if got := introScanRequestSequence(t, ctx, pool); got != want {
					t.Fatalf("repeated finalization emitted another signal: %d, want %d", got, want)
				}
			})
		}
	}
}

func TestCancelledScanDoesNotRequestIntroDetection(t *testing.T) {
	prober := taskScanBlockingProber()
	ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
	file := libraryIntegrationFile(t, root, "Shows/Series/Season 01/Series.S01E01.mp4", "video:intro-cancel")
	actor := metadataEditTestActor(t, ctx, pool, "intro-cancel-admin")
	options := DefaultLibraryOptions()
	options.EnableIntroDetection = true
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"Shows", "tvshows", []string{filepath.Dir(filepath.Dir(filepath.Dir(file)))}, options)
	if err != nil {
		t.Fatal(err)
	}
	before := introScanRequestSequence(t, ctx, pool)
	_, children := taskScanFixture(t, ctx, pool, collection)
	admission, err := store.AdmitTaskScan(ctx, children[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("admit task-owned scan: %+v %v", admission, err)
	}
	taskScanAwaitSignal(t, ctx, prober.entered, "scan did not enter the prober")
	if err := store.CancelTaskScan(ctx, children[0]); err != nil {
		t.Fatal(err)
	}
	taskScanAwaitSignal(t, ctx, prober.cancelled, "scan cancellation did not reach the prober")
	libraryIntegrationWaitJob(t, ctx, store, admission.Job.ID, "Cancelled")
	if got := introScanRequestSequence(t, ctx, pool); got != before {
		t.Fatalf("cancelled scan emitted an analysis signal: %d, want %d", got, before)
	}
}
