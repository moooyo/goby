//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func holdThemePublicationGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION theme_publication_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(hashtextextended(current_schema() || ':theme-publication-gate', 0));
			RETURN NEW;
		END; $$;
		CREATE TRIGGER theme_publication_gate BEFORE INSERT OR UPDATE ON item_theme_resources
		FOR EACH ROW EXECUTE FUNCTION theme_publication_gate()`); err != nil {
		t.Fatalf("install the schema-local theme publication gate (%T)", err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin the theme publication gate")
	}
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':theme-publication-gate', 0))"); err != nil {
		rollback(gate)
		t.Fatal("hold the theme publication gate")
	}
	return gate
}

func waitThemePublicationCandidate(t *testing.T, ctx context.Context, store *Store, jobID string, ready <-chan struct{}) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ready:
			return
		case <-ticker.C:
			job, err := store.GetJob(ctx, jobID)
			if err != nil {
				t.Fatalf("read the theme publication preparation job: %v", err)
			}
			if job.Status != "Queued" && job.Status != "Running" {
				t.Fatalf("the scan stopped before its final theme candidate was prepared: %+v", job)
			}
		case <-ctx.Done():
			t.Fatal("the theme publication fixture ended before its final candidate was prepared")
		}
	}
}

func TestThemePublicationRejectsSourceChangesAfterTransactionStarts(t *testing.T) {
	for _, owner := range []string{"movie", "collection"} {
		for _, mutation := range []string{"registered root", "file pathname", "file contents", "directory contents"} {
			t.Run(owner+"/"+mutation, func(t *testing.T) {
				finalCandidate := "Theme.mp3"
				if owner == "collection" {
					finalCandidate = "Second.mp3"
				}
				fixture := &libraryFixtureProber{}
				ready := make(chan struct{}, 1)
				var armed atomic.Bool
				prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
					info, err := fixture.ProbeFile(ctx, file)
					if err == nil && armed.Load() && filepath.Base(file.Name()) == finalCandidate {
						select {
						case ready <- struct{}{}:
						default:
						}
					}
					return info, err
				})
				ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
				registered := filepath.Join(root, "registered")
				relative := "registered/theme-music/Theme.mp3"
				paths := []string{registered}
				if owner == "movie" {
					libraryIntegrationFile(t, root, "registered/Film/Main.mp4", "video:main")
					relative = "registered/Film/theme-music/Theme.mp3"
				} else {
					libraryIntegrationFile(t, root, "second/theme-music/Second.mp3", "audio:second-theme")
					paths = append(paths, filepath.Join(root, "second"))
				}
				theme := libraryIntegrationFile(t, root, relative, "audio:accepted-theme")
				lib, err := store.CreateLibrary(ctx, "Theme publication source witnesses", "movies", paths)
				if err != nil {
					t.Fatal(err)
				}
				if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
					t.Fatalf("prepare the accepted theme population: %+v", job)
				}
				resources := themeScanTestResources(t, ctx, pool, lib.ID)
				if len(resources) != len(paths) {
					t.Fatal("the source fixture did not publish its complete theme population")
				}
				for _, resource := range resources {
					if (resource.ownerID == lib.ID) != (owner == "collection") {
						t.Fatal("the source fixture selected the wrong semantic publication owner")
					}
				}
				before := themeScanTestSnapshot(t, ctx, pool, lib.ID)
				notifications := catalogChangesTestListener(t, store)
				gate := holdThemePublicationGate(t, ctx, pool)
				defer rollback(gate)
				armed.Store(true)
				job := themeScanTestForce(t, ctx, store, lib.ID)
				// Collection publication follows both complete root walks. Start
				// the database-lock deadline only after its final source probe;
				// that deadline must not also bound unrelated scan preparation.
				waitThemePublicationCandidate(t, ctx, store, job.ID, ready)
				taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
				switch mutation {
				case "registered root":
					if err := os.Rename(registered, registered+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(registered, 0o700); err != nil {
						t.Fatal(err)
					}
				case "file pathname":
					if err := os.Rename(theme, theme+".original"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(theme, []byte("audio:replacement-theme"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "file contents":
					// Parent directory snapshots remain unchanged by an in-place write.
					if err := os.WriteFile(theme, []byte("audio:changed-theme-with-a-new-size"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "directory contents":
					if err := os.WriteFile(filepath.Join(filepath.Dir(theme), "Additional.mp3"), []byte("audio:new-theme"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := gate.Commit(ctx); err != nil {
					t.Fatal("release the source replacement theme publication gate")
				}
				finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
				if finished.Error == "" || finished.Status == "Cancelled" {
					t.Fatalf("theme publication accepted its changed source: %+v", finished)
				}
				if after := themeScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
					t.Fatal("a source change during the transaction replaced accepted theme state")
				}
				batches := auxiliaryCatalogBatches(t, notifications)
				for _, resource := range resources {
					assertAuxiliaryCatalogKinds(t, batches, resource.id)
				}
			})
		}
	}
}

func TestThemePublicationRetirementRechecksSourcesAfterTransactionStarts(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationFile(t, root, "registered/Film/Main.mp4", "video:main")
	theme := libraryIntegrationFile(t, root, "registered/Film/theme-music/Theme.mp3", "audio:accepted-theme")
	lib := libraryIntegrationCreate(t, ctx, store, "Theme retirement source witnesses", "movies", filepath.Join(root, "registered"))
	if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
		t.Fatalf("prepare the accepted theme population: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, lib.ID)
	if len(resources) != 1 || !resources[0].active {
		t.Fatal("the retirement fixture lacks its active resource")
	}
	before := themeScanTestSnapshot(t, ctx, pool, lib.ID)
	if err := os.Remove(theme); err != nil {
		t.Fatal(err)
	}
	notifications := catalogChangesTestListener(t, store)
	gate := holdThemePublicationGate(t, ctx, pool)
	defer rollback(gate)
	job := themeScanTestForce(t, ctx, store, lib.ID)
	taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
	if err := os.WriteFile(theme, []byte("audio:returned-theme"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal("release the theme retirement publication gate")
	}
	finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
	if finished.Error == "" || finished.Status == "Cancelled" {
		t.Fatalf("theme retirement accepted an outdated absence observation: %+v", finished)
	}
	if after := themeScanTestSnapshot(t, ctx, pool, lib.ID); after != before {
		t.Fatal("a theme that reappeared during the transaction was retired")
	}
	assertAuxiliaryCatalogKinds(t, auxiliaryCatalogBatches(t, notifications), resources[0].id)
}

func TestAuxiliaryPublicationWitnessRetainsIndependentFilesAfterObservationTimeout(t *testing.T) {
	ctx, pool, store, root, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	theme := libraryIntegrationFile(t, root, "registered/theme-music/Theme.mp3", "audio:accepted-theme")
	lib := libraryIntegrationCreate(t, ctx, store, "Auxiliary observation lifetime", "movies", filepath.Join(root, "registered"))
	var record libraryRoot
	if err := pool.QueryRow(ctx, `SELECT id,library_id,path,allowed_path,relative_path FROM library_roots WHERE library_id=$1`, lib.ID).
		Scan(&record.id, &record.libraryID, &record.path, &record.allowedPath, &record.relativePath); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(theme)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	task := &scanTask{ctx: ctx}
	state := &scanState{store: store, task: task, library: lib, root: record, themes: &themeScan{directories: make(map[string]os.FileInfo)}}
	for _, relative := range []string{".", "theme-music"} {
		observed, err := os.Stat(filepath.Join(record.path, relative))
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		state.themes.directories[relative] = observed
	}
	files := []*preparedThemeFile{{state: state, role: scannedRoleTheme,
		candidate: themeCandidate{relative: "theme-music/Theme.mp3", kind: themePathKindSong, layout: themePathLayoutMusic},
		input:     &scannedMediaInput{file: file, info: info}}}
	defer closeThemeFiles(files)
	witness, err := store.prepareAuxiliaryPublicationWitness(task, lib, files, map[string]bool{record.id: true},
		map[string]*scanState{record.id: state}, themePublicationPlan{})
	if err != nil {
		t.Fatal(err)
	}
	defer witness.Close()
	held := witness.files[0].input.file
	if held == nil || held == file {
		t.Fatal("the observation witness borrowed the caller's media descriptor")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	observed := make(chan error, 1)
	finished := make(chan error, 1)
	slots := make(chan struct{}, 1)
	go func() {
		finished <- runStorageObservationWithLimit(ctx, slots, 25*time.Millisecond,
			[]*storageObservationLifetime{&witness.observation}, func(context.Context) error {
				close(entered)
				<-release
				_, err := held.Stat()
				observed <- err
				return err
			})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("the bounded observation worker did not enter")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, errStorageObservationUnavailable) {
			t.Fatalf("the blocked observation did not time out: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the observation caller did not return before its worker")
	}
	closeThemeFiles(files)
	if err := witness.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := held.Stat(); err != nil || len(slots) != 1 {
		t.Fatal("retirement closed the active worker's independent descriptor or released its slot")
	}
	close(release)
	select {
	case err := <-observed:
		if err != nil {
			t.Fatalf("the timed-out worker lost its retained descriptor: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the released observation worker did not finish")
	}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for len(slots) != 0 {
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("the finished observation worker did not release its slot")
		}
	}
	if _, err := held.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("the finished worker did not close its retired descriptor: %v", err)
	}
}
