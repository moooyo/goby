//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type auxiliaryOriginalAnchorTrace struct {
	table   string
	probed  atomic.Pointer[os.File]
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func (trace *auxiliaryOriginalAnchorTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	if !strings.Contains(statement, "from "+trace.table+" r join items i") || !strings.Contains(statement, "limit $2) active") {
		return ctx
	}
	file := trace.probed.Load()
	if file == nil {
		return ctx
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		return ctx
	}
	trace.once.Do(func() {
		// This planning query follows the last retired probe and precedes
		// prepareAuxiliaryPublicationWitness. No publication proof exists yet.
		close(trace.reached)
		select {
		case <-trace.release:
		case <-ctx.Done():
		}
	})
	return ctx
}

func (*auxiliaryOriginalAnchorTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestAuxiliaryPublicationRetainsOriginalAnchorBeforeWitnessCreation(t *testing.T) {
	for _, scenario := range []struct {
		role   string
		legacy bool
	}{{"theme", false}, {"extra", false}, {"collection", false}, {"theme", true}, {"extra", true}, {"collection", true}} {
		t.Run(fmt.Sprintf("%s/legacy_%t", scenario.role, scenario.legacy), func(t *testing.T) {
			directory, name, contents := "theme-music", "Theme.mp3", "audio:accepted-theme"
			table := "item_theme_resources"
			resources, snapshot := themeScanTestResources, themeScanTestSnapshot
			if scenario.role == "extra" {
				directory, name, contents = "featurettes", "Clip.mp4", "video:accepted-extra"
				table = "item_extra_resources"
				resources, snapshot = extraScanTestResources, extraScanTestSnapshot
			}
			fixture := &libraryFixtureProber{}
			ctx, pool, initial, approved, _ := libraryIntegrationStore(t, fixture)
			registered := filepath.Join(approved, "bridge", "registered")
			ownerDirectory := "Film"
			if scenario.role == "collection" {
				ownerDirectory = ""
			} else {
				libraryIntegrationFile(t, approved, "bridge/registered/Film/Main.mp4", "video:main")
			}
			sourcePath := libraryIntegrationFile(t, approved, filepath.Join("bridge", "registered", ownerDirectory, directory, name), contents)
			library := libraryIntegrationCreate(t, ctx, initial, "Auxiliary original ancestor proof", "movies", registered)
			if job := libraryIntegrationScan(t, ctx, initial, library.ID, "Completed"); job.Error != "" {
				t.Fatalf("prepare accepted auxiliary population: %+v", job)
			}
			accepted := resources(t, ctx, pool, library.ID)
			if len(accepted) != 1 || !accepted[0].active {
				t.Fatalf("accepted auxiliary population has an unexpected shape: %+v", accepted)
			}
			before := snapshot(t, ctx, pool, library.ID)
			if scenario.legacy {
				if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE library_id=$1`, library.ID); err != nil {
					t.Fatal("prepare a legacy root without a verified startup capture", err)
				}
			}
			if err := initial.Close(ctx); err != nil {
				t.Fatal("close initial catalog owner", err)
			}
			trace := &auxiliaryOriginalAnchorTrace{table: table, reached: make(chan struct{}), release: make(chan struct{})}
			configuration := pool.Config()
			configuration.ConnConfig.Tracer = trace
			tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, tracedPool)
			prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
				info, err := fixture.ProbeFile(ctx, file)
				if err == nil && filepath.Base(file.Name()) == name {
					trace.probed.Store(file)
				}
				return info, err
			})
			store, err := New(tracedPool, prober, []string{approved})
			if err != nil {
				t.Fatal(err)
			}
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(trace.release) }) }
			t.Cleanup(func() {
				unblock()
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := store.Close(cleanup); err != nil {
					t.Error("close original-anchor fixture", err)
				}
			})
			beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			notifications := catalogChangesTestListener(t, store)
			job := themeScanTestForce(t, ctx, store, library.ID)
			select {
			case <-trace.reached:
			case <-time.After(15 * time.Second):
				current, err := store.GetJob(ctx, job.ID)
				t.Fatalf("scan did not reach planning after its final probe retired: job=%+v error=%v", current, err)
			}
			if file := trace.probed.Load(); file == nil {
				t.Fatal("the final source probe was not observed")
			} else if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("the source descriptor survived into publication planning: %v", err)
			}
			rootBefore, err := os.Stat(registered)
			if err != nil {
				t.Fatal(err)
			}
			fileBefore, err := os.Stat(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			anchorBefore, err := os.Stat(approved)
			if err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(t.TempDir(), "original-anchor")
			if err := os.Rename(approved, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(approved, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(moved, "bridge"), filepath.Join(approved, "bridge")); err != nil {
				t.Fatal(err)
			}
			rootAfter, err := os.Stat(registered)
			if err != nil || !themeSnapshotEqual(rootBefore, rootAfter) {
				t.Fatalf("moving the bridge changed the registered root's source facts: %v", err)
			}
			fileAfter, err := os.Stat(sourcePath)
			if err != nil || !themeSnapshotEqual(fileBefore, fileAfter) {
				t.Fatalf("moving the bridge changed the resource's source facts: %v", err)
			}
			anchorAfter, err := os.Stat(approved)
			if err != nil || os.SameFile(anchorBefore, anchorAfter) {
				t.Fatalf("the configured name still identifies its original ancestor: %v", err)
			}
			unblock()
			finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
			if finished.Error == "" || finished.Status == "Cancelled" {
				t.Fatalf("publication learned a replacement ancestor after retiring its probe: %+v", finished)
			}
			if after := snapshot(t, ctx, pool, library.ID); after != before {
				t.Fatal("replacement ancestor published changed auxiliary resources or metadata")
			}
			assertAuxiliaryCatalogKinds(t, auxiliaryCatalogBatches(t, notifications), accepted[0].id)
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
				t.Fatalf("rejected original-anchor proof retained owners: before=%d after=%d", beforeOwners, owners)
			}
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
				t.Fatalf("rejected original-anchor proof retained actual I/O: before=%+v after=%+v", beforeIO, stats)
			}
		})
	}
}

func TestAuxiliaryLegacyCollectionSharesOneRetainedOwnerAcrossRoots(t *testing.T) {
	prober := &auxiliaryCapacityProber{}
	ctx, pool, store, approved, _ := libraryIntegrationStoreWithTimeout(t, prober, 3*time.Minute)
	// Stay inside CreateLibrary's 32-directory contract while the complete
	// shared collection still exceeds the 64 retained-owner capacity.
	const resourceRoots, resourcesPerRoot = 31, 3
	paths := make([]string, resourceRoots+1)
	for index := range paths {
		paths[index] = filepath.Join(approved, fmt.Sprintf("root-%02d", index))
		if index < resourceRoots {
			for resource := range resourcesPerRoot {
				libraryIntegrationFile(t, paths[index], fmt.Sprintf("theme-music/Theme-%d.mp3", resource), "audio:complete-shared-theme")
			}
		} else if err := os.MkdirAll(paths[index], 0700); err != nil {
			t.Fatal("prepare an empty completed collection root", err)
		}
	}
	library, err := store.CreateLibrary(ctx, "Legacy multi-root collection", "mixed", paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE library_id=$1`, library.ID); err != nil {
		t.Fatal(err)
	}
	beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
	job := auxiliaryFailureScan(t, ctx, store, library.ID, false, "Completed")
	if job.Error != "" {
		t.Fatalf("legal multi-root collection did not publish completely: %+v", job)
	}
	resources := themeScanTestResources(t, ctx, pool, library.ID)
	if len(resources) != resourceRoots*resourcesPerRoot {
		t.Fatalf("collection published %d resources across %d roots, want %d", len(resources), len(paths), resourceRoots*resourcesPerRoot)
	}
	for _, resource := range resources {
		if !resource.active || resource.ownerID != library.ID {
			t.Fatalf("collection lost an active shared-owner member: %+v", resource)
		}
	}
	if maximum := prober.maximum.Load(); maximum > int64(beforeOwners+8) {
		t.Fatalf("collection retained one owner per root: maximum=%d baseline=%d", maximum, beforeOwners)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
		t.Fatalf("completed collection retained group ownership: before=%d after=%d", beforeOwners, owners)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("completed collection retained actual I/O: before=%+v after=%+v", beforeIO, stats)
	}
}
