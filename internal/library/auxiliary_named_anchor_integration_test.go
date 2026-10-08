//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestAuxiliaryPublicationRejectsReplacedConfiguredAncestor(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			directory, name, contents := "theme-music", "Theme.mp3", "audio:accepted-theme"
			resources, snapshot, holdGate := themeScanTestResources, themeScanTestSnapshot, holdThemePublicationGate
			if role == "extra" {
				directory, name, contents = "featurettes", "Clip.mp4", "video:accepted-extra"
				resources, snapshot, holdGate = extraScanTestResources, extraScanTestSnapshot, holdExtraPublicationGate
			}
			fixture := &libraryFixtureProber{}
			ready := make(chan struct{}, 1)
			var armed atomic.Bool
			var probed atomic.Pointer[os.File]
			prober := scanProberFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
				info, err := fixture.ProbeFile(ctx, file)
				if err == nil && armed.Load() && filepath.Base(file.Name()) == name {
					probed.Store(file)
					select {
					case ready <- struct{}{}:
					default:
					}
				}
				return info, err
			})
			ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, prober)
			registered := filepath.Join(allowedRoot, "registered")
			libraryIntegrationFile(t, allowedRoot, "registered/Film/Main.mp4", "video:main")
			libraryIntegrationFile(t, allowedRoot, filepath.Join("registered", "Film", directory, name), contents)
			library := libraryIntegrationCreate(t, ctx, store, "Auxiliary named ancestor proof", "movies", registered)
			if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
				t.Fatalf("prepare the accepted auxiliary population: %+v", job)
			}
			accepted := resources(t, ctx, pool, library.ID)
			if len(accepted) != 1 || !accepted[0].active {
				t.Fatalf("accepted auxiliary population has an unexpected shape: %+v", accepted)
			}
			var configured, relative string
			if err := pool.QueryRow(ctx, `SELECT allowed_path,relative_path FROM library_roots WHERE library_id=$1`, library.ID).
				Scan(&configured, &relative); err != nil {
				t.Fatal(err)
			}
			if configured != allowedRoot || relative != "registered" {
				t.Fatalf("fixture must register a child beneath its configured ancestor: configured=%q relative=%q", configured, relative)
			}
			before := snapshot(t, ctx, pool, library.ID)
			beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			notifications := catalogChangesTestListener(t, store)
			gate := holdGate(t, ctx, pool)
			defer rollback(gate)
			armed.Store(true)
			job := themeScanTestForce(t, ctx, store, library.ID)
			waitThemePublicationCandidate(t, ctx, store, job.ID, ready)
			// The source witness is prepared before the transaction reaches the
			// trigger. Replacing only its ancestor leaves every source snapshot
			// beneath the pinned old anchor unchanged.
			taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
			childBefore, err := os.Stat(registered)
			if err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(t.TempDir(), "original-anchor")
			if err := os.Rename(allowedRoot, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(registered, 0700); err != nil {
				t.Fatal(err)
			}
			childAfter, err := os.Stat(filepath.Join(moved, "registered"))
			if err != nil || !themeSnapshotEqual(childBefore, childAfter) {
				t.Fatalf("ancestor replacement changed the original child's inode or timestamps: %v", err)
			}
			replacement, err := os.Stat(registered)
			if err != nil || os.SameFile(childBefore, replacement) {
				t.Fatalf("the registered pathname did not select a replacement child: %v", err)
			}
			if source := probed.Load(); source == nil {
				t.Fatal("the expected auxiliary probe was not observed")
			} else if _, err := source.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("the group retained its probe descriptor through publication: %v", err)
			}
			if err := gate.Commit(ctx); err != nil {
				t.Fatal("release the configured-ancestor publication gate")
			}
			finished := themeScanTestWaitStopped(t, ctx, store, job.ID)
			if finished.Error == "" || finished.Status == "Cancelled" {
				t.Fatalf("the auxiliary publication accepted its replaced configured ancestor: %+v", finished)
			}
			if after := snapshot(t, ctx, pool, library.ID); after != before {
				t.Fatal("ancestor replacement published changed auxiliary resources or metadata")
			}
			assertAuxiliaryCatalogKinds(t, auxiliaryCatalogBatches(t, notifications), accepted[0].id)
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
				t.Fatalf("rejected named-anchor proof retained owners: before=%d after=%d", beforeOwners, owners)
			}
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
				t.Fatalf("rejected named-anchor proof retained actual I/O charges: before=%+v after=%+v", beforeIO, stats)
			}
		})
	}
}
