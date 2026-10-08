//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

var errAuxiliaryFinalProbe = errors.New("the last auxiliary probe failed")

type auxiliaryFailureProber struct {
	libraryFixtureProber
	directory string
	mu        sync.Mutex
	calls     int
	first     *os.File
	previous  *os.File
	onLast    func(context.Context) error
	reached   bool
	retired   bool
	injected  error
}

func (prober *auxiliaryFailureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	if filepath.Base(filepath.Dir(file.Name())) != prober.directory {
		return prober.libraryFixtureProber.ProbeFile(ctx, file)
	}
	prober.mu.Lock()
	if prober.previous != nil {
		if _, err := prober.previous.Stat(); !errors.Is(err, os.ErrClosed) {
			prober.mu.Unlock()
			return media.Info{}, errors.New("the previous auxiliary descriptor survived its probe")
		}
	}
	prober.calls++
	if prober.calls == 1 {
		prober.first = file
	}
	prober.previous = file
	index, first, onLast := prober.calls, prober.first, prober.onLast
	prober.mu.Unlock()
	if index == 65 && onLast != nil {
		_, firstErr := first.Stat()
		prober.mu.Lock()
		prober.reached = true
		prober.retired = errors.Is(firstErr, os.ErrClosed)
		prober.mu.Unlock()
		if !errors.Is(firstErr, os.ErrClosed) {
			return media.Info{}, errors.New("the first auxiliary descriptor was not retired before the final probe")
		}
		err := onLast(ctx)
		prober.mu.Lock()
		prober.injected = err
		prober.mu.Unlock()
		if err != nil {
			return media.Info{}, err
		}
	}
	return prober.libraryFixtureProber.ProbeFile(ctx, file)
}

func auxiliaryFailureScan(t *testing.T, ctx context.Context, store *Store, libraryID string, force bool, status string) Job {
	t.Helper()
	job, err := store.StartScanWithOptions(ctx, libraryID, ScanOptions{ForceProbe: force})
	if err != nil {
		t.Fatal(err)
	}
	return libraryIntegrationWaitJobWithTimeout(t, ctx, store, job.ID, status, 90*time.Second)
}

func TestAuxiliaryGroupLateFailurePreservesCompletePopulation(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		for _, failure := range []string{"probe_failure", "source_change", "task_cancel"} {
			t.Run(role+"/"+failure, func(t *testing.T) {
				directory, extension, contents := "theme-music", "mp3", "audio:accepted-resource"
				if role == "extra" {
					directory, extension, contents = "featurettes", "mp4", "video:accepted-resource"
				}
				prober := &auxiliaryFailureProber{directory: directory}
				ctx, pool, store, root, _ := libraryIntegrationStoreWithTimeout(t, prober, 3*time.Minute)
				libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
				for index := 0; index < 65; index++ {
					libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/%s/%03d.%s", directory, index, extension), contents)
				}
				library := libraryIntegrationCreate(t, ctx, store, "Atomic auxiliary failure", "movies", filepath.Join(root, "movies"))
				if job := auxiliaryFailureScan(t, ctx, store, library.ID, false, "Completed"); job.Error != "" {
					t.Fatalf("initial complete population failed: %+v", job)
				}
				resources, snapshot := themeScanTestResources, themeScanTestSnapshot
				if role == "extra" {
					resources, snapshot = extraScanTestResources, extraScanTestSnapshot
				}
				accepted := resources(t, ctx, pool, library.ID)
				if len(accepted) != 65 {
					t.Fatalf("initial population has %d members, want 65", len(accepted))
				}
				for _, resource := range accepted {
					if !resource.active {
						t.Fatal("initial population contains an inactive member")
					}
				}
				before := snapshot(t, ctx, pool, library.ID)
				beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
				prober.mu.Lock()
				prober.calls, prober.first, prober.previous = 0, nil, nil
				prober.onLast = func(context.Context) error {
					switch failure {
					case "probe_failure":
						return errAuxiliaryFinalProbe
					case "source_change":
						// The first descriptor is already closed. Final group proof must
						// still reject a change to that earlier retained source fact.
						prober.mu.Lock()
						firstPath := prober.first.Name()
						prober.mu.Unlock()
						return os.WriteFile(firstPath, []byte(contents+":changed-after-retirement"), 0600)
					case "task_cancel":
						store.mu.Lock()
						var jobID string
						for _, task := range store.active {
							if task.job.LibraryID == library.ID {
								jobID = task.job.ID
								break
							}
						}
						store.mu.Unlock()
						if jobID == "" {
							return errors.New("the auxiliary scan was not active at its final probe")
						}
						return store.CancelJob(ctx, jobID)
					}
					return errors.New("unknown auxiliary failure scenario")
				}
				prober.mu.Unlock()
				status := "Completed"
				if failure == "task_cancel" {
					status = "Cancelled"
				}
				job := auxiliaryFailureScan(t, ctx, store, library.ID, true, status)
				prober.mu.Lock()
				calls, reached, retired, injected, last := prober.calls, prober.reached, prober.retired, prober.injected, prober.previous
				prober.mu.Unlock()
				if calls != 65 || !reached || !retired {
					t.Fatalf("failure did not follow 64 retired inputs: calls=%d reached=%v first_retired=%v", calls, reached, retired)
				}
				if failure == "probe_failure" {
					if !errors.Is(injected, errAuxiliaryFinalProbe) {
						t.Fatalf("the expected probe failure was not injected: %v", injected)
					}
				} else if injected != nil {
					t.Fatalf("the source mutation or real cancellation failed: %v", injected)
				}
				if failure != "task_cancel" && job.Error == "" {
					t.Fatal("the incomplete auxiliary group was accepted without a warning")
				}
				if after := snapshot(t, ctx, pool, library.ID); after != before {
					t.Fatal("a late failure published a partial or changed auxiliary population")
				}
				if _, err := last.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("the last auxiliary descriptor survived scan completion: %v", err)
				}
				if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
					t.Fatalf("a rejected group retained owners: before=%d after=%d", beforeOwners, owners)
				}
				if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
					t.Fatalf("a rejected group retained actual I/O charges: before=%+v after=%+v", beforeIO, stats)
				}
			})
		}
	}
}

func TestAuxiliaryGroupWarmCompletePopulationDoesNotReprobe(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			directory, extension, contents := "theme-music", "mp3", "audio:complete-resource"
			if role == "extra" {
				directory, extension, contents = "featurettes", "mp4", "video:complete-resource"
			}
			prober := &auxiliaryFailureProber{directory: directory}
			ctx, pool, store, root, _ := libraryIntegrationStoreWithTimeout(t, prober, 3*time.Minute)
			libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:main")
			for index := 0; index < 256; index++ {
				libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/%s/%03d.%s", directory, index, extension), contents)
			}
			library := libraryIntegrationCreate(t, ctx, store, "Complete warm auxiliary group", "movies", filepath.Join(root, "movies"))
			if job := auxiliaryFailureScan(t, ctx, store, library.ID, false, "Completed"); job.Error != "" {
				t.Fatalf("initial complete population failed: %+v", job)
			}
			snapshot := themeScanTestSnapshot
			if role == "extra" {
				snapshot = extraScanTestSnapshot
			}
			before := snapshot(t, ctx, pool, library.ID)
			beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			prober.mu.Lock()
			beforeCalls := prober.calls
			prober.mu.Unlock()
			if beforeCalls != 256 {
				t.Fatalf("initial population probed %d auxiliary inputs, want 256", beforeCalls)
			}
			job := auxiliaryFailureScan(t, ctx, store, library.ID, false, "Completed")
			prober.mu.Lock()
			calls := prober.calls
			prober.mu.Unlock()
			if job.Error != "" || job.Added != 0 || job.Updated != 0 || calls != beforeCalls {
				t.Fatalf("warm complete population was reprobed or rewritten: job=%+v calls=%d -> %d", job, beforeCalls, calls)
			}
			if after := snapshot(t, ctx, pool, library.ID); after != before {
				t.Fatal("warm complete population changed identity, metadata or roles")
			}
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
				t.Fatalf("warm population retained owners: before=%d after=%d", beforeOwners, owners)
			}
			if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
				t.Fatalf("warm population retained actual I/O charges: before=%+v after=%+v", beforeIO, stats)
			}
		})
	}
}
