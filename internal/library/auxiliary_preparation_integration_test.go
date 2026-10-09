//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type auxiliaryPreparationTraceKey struct{}

type auxiliaryPreparationTrace struct {
	mu          sync.Mutex
	sizes       []int
	scalar      int
	files       []*os.File
	err         error
	cancelQuery bool
	afterBatch  func(context.Context) error
}

func (trace *auxiliaryPreparationTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if strings.HasPrefix(statement, "select goby_generated_sort_name($1,sort_remove_words,true)") {
		trace.scalar++
	}
	if !strings.HasPrefix(statement, "select names.ordinal, goby_generated_sort_name(") {
		return ctx
	}
	if len(data.Args) != 1 {
		trace.err = errors.New("auxiliary sorting lost its single typed name input")
	} else if names, ok := data.Args[0].([]string); !ok {
		trace.err = errors.New("auxiliary sorting did not use a text-array input")
	} else {
		trace.sizes = append(trace.sizes, len(names))
	}
	for _, file := range trace.files {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			trace.err = errors.New("an auxiliary input survived into batched sorting")
		}
	}
	if trace.cancelQuery {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	return context.WithValue(ctx, auxiliaryPreparationTraceKey{}, trace)
}

func (trace *auxiliaryPreparationTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(auxiliaryPreparationTraceKey{}) != trace || data.Err != nil {
		return
	}
	trace.mu.Lock()
	after := trace.afterBatch
	trace.afterBatch = nil
	trace.mu.Unlock()
	if after != nil {
		if err := after(ctx); err != nil {
			trace.mu.Lock()
			trace.err = errors.Join(trace.err, err)
			trace.mu.Unlock()
		}
	}
}

func (trace *auxiliaryPreparationTrace) reset() {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.sizes, trace.scalar, trace.err = nil, 0, nil
}

func (trace *auxiliaryPreparationTrace) assertQueries(t *testing.T, want []int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if trace.err != nil || trace.scalar != 0 || !reflect.DeepEqual(trace.sizes, want) {
		t.Fatalf("auxiliary sort queries sizes=%v scalar=%d error=%v; want sizes=%v", trace.sizes, trace.scalar, trace.err, want)
	}
}

type auxiliaryPreparationProber struct {
	libraryFixtureProber
	trace      *auxiliaryPreparationTrace
	beforeLast func(context.Context) error
}

func (prober *auxiliaryPreparationProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := prober.libraryFixtureProber.ProbeFile(ctx, file)
	if err != nil {
		return info, err
	}
	directory, name := filepath.Base(filepath.Dir(file.Name())), filepath.Base(file.Name())
	if directory != "theme-music" && directory != "featurettes" {
		return info, nil
	}
	prober.trace.mu.Lock()
	prober.trace.files = append(prober.trace.files, file)
	prober.trace.mu.Unlock()
	if directory == "theme-music" && (name == "The 000.mp3" || name == "The 001.mp3") {
		info.EmbeddedMusic = &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "The Duplicate Title"}
	}
	if strings.HasPrefix(name, "The 002.") && prober.beforeLast != nil {
		err = prober.beforeLast(ctx)
	}
	return info, err
}

func auxiliaryPreparationStore(t *testing.T, trace *auxiliaryPreparationTrace, prober Prober) (context.Context, *pgxpool.Pool, *Store, string, string) {
	t.Helper()
	ctx, pool, initial, root, userID := libraryIntegrationStoreWithTimeout(t, prober, 3*time.Minute)
	if err := initial.Close(ctx); err != nil {
		t.Fatal(err)
	}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	store, err := New(traced, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return ctx, pool, store, root, userID
}

func TestAuxiliaryPreparationBatchesSortingAfterInputRetirement(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			trace := &auxiliaryPreparationTrace{}
			prober := &auxiliaryPreparationProber{trace: trace}
			ctx, pool, store, root, _ := auxiliaryPreparationStore(t, trace, prober)
			libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:owner")
			lib := libraryIntegrationCreate(t, ctx, store, "Auxiliary batched sorting", "movies", filepath.Join(root, "movies"))
			directory, extension, content := "theme-music", "mp3", "audio:auxiliary"
			resources, snapshot := themeScanTestResources, themeScanTestSnapshot
			if role == "extra" {
				directory, extension, content = "featurettes", "mp4", "video:auxiliary"
				resources, snapshot = extraScanTestResources, extraScanTestSnapshot
			}
			previous := 0
			for _, count := range []int{0, 3, 256} {
				for index := previous; index < count; index++ {
					libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/%s/The %03d.%s", directory, index, extension), content)
				}
				trace.reset()
				job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
				if job.Error != "" || len(resources(t, ctx, pool, lib.ID)) != count {
					t.Fatalf("complete auxiliary batch lost members: count=%d job=%+v", count, job)
				}
				var want []int
				if count != 0 {
					want = []int{count}
				}
				trace.assertQueries(t, want)
				previous = count
			}
			before := snapshot(t, ctx, pool, lib.ID)
			trace.reset()
			if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" || job.Added != 0 || job.Updated != 0 || snapshot(t, ctx, pool, lib.ID) != before {
				t.Fatalf("cached batched sorting changed the accepted population: %+v", job)
			}
			trace.assertQueries(t, []int{256})
		})
	}
}

func TestAuxiliaryPreparationSortingUsesPublicationSettings(t *testing.T) {
	for _, changedAt := range []string{"last-probe", "after-batch"} {
		t.Run(changedAt, func(t *testing.T) {
			trace := &auxiliaryPreparationTrace{}
			prober := &auxiliaryPreparationProber{trace: trace}
			ctx, pool, store, root, _ := auxiliaryPreparationStore(t, trace, prober)
			libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:owner")
			for index := 0; index < 3; index++ {
				libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/theme-music/The %03d.mp3", index), "audio:auxiliary")
			}
			lib := libraryIntegrationCreate(t, ctx, store, "Auxiliary live sorting", "movies", filepath.Join(root, "movies"))
			update := func(ctx context.Context) error {
				_, err := pool.Exec(ctx, `UPDATE managed_settings SET sort_remove_words=ARRAY['The'] WHERE id=1`)
				return err
			}
			if changedAt == "last-probe" {
				prober.beforeLast = update
			} else {
				trace.afterBatch = update
			}
			trace.reset()
			if job := libraryIntegrationScan(t, ctx, store, lib.ID, "Completed"); job.Error != "" {
				t.Fatalf("sorting policy transition warned: %+v", job)
			}
			trace.assertQueries(t, []int{3})
			rows, err := pool.Query(ctx, `SELECT i.name,i.sort_name,ms.automatic->>'Name',ms.automatic->>'SortName'
				FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id
				JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.library_id=$1 ORDER BY i.relative_path`, lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for rows.Next() {
				var name, sortName, automaticName, automaticSort string
				if err := rows.Scan(&name, &sortName, &automaticName, &automaticSort); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				if !strings.HasPrefix(name, "The ") || sortName != strings.TrimPrefix(name, "The ") || automaticName != name || automaticSort != sortName {
					rows.Close()
					t.Fatalf("publication reused stale prepared sorting: name=%q sort=%q automatic=%q/%q", name, sortName, automaticName, automaticSort)
				}
				count++
			}
			rows.Close()
			if err := rows.Err(); err != nil || count != 3 {
				t.Fatalf("sorting query lost duplicate embedded or filename titles: count=%d err=%v", count, err)
			}
		})
	}
}

func TestAuxiliaryPreparationQueryFailureRetainsPublishedPopulation(t *testing.T) {
	for _, role := range []string{"theme", "extra"} {
		t.Run(role, func(t *testing.T) {
			trace := &auxiliaryPreparationTrace{}
			prober := &auxiliaryPreparationProber{trace: trace}
			ctx, pool, store, root, _ := auxiliaryPreparationStore(t, trace, prober)
			libraryIntegrationFile(t, root, "movies/Film/Main.mp4", "video:owner")
			directory, extension, content := "theme-music", "mp3", "audio:auxiliary"
			snapshot := themeScanTestSnapshot
			if role == "extra" {
				directory, extension, content = "featurettes", "mp4", "video:auxiliary"
				snapshot = extraScanTestSnapshot
			}
			for index := 0; index < 3; index++ {
				libraryIntegrationFile(t, root, fmt.Sprintf("movies/Film/%s/The %03d.%s", directory, index, extension), content)
			}
			lib := libraryIntegrationCreate(t, ctx, store, "Auxiliary sort failure", "movies", filepath.Join(root, "movies"))
			libraryIntegrationScan(t, ctx, store, lib.ID, "Completed")
			before := snapshot(t, ctx, pool, lib.ID)
			owners, io := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			trace.reset()
			trace.mu.Lock()
			trace.cancelQuery = true
			trace.mu.Unlock()
			job, err := store.StartScanWithOptions(ctx, lib.ID, ScanOptions{ForceProbe: true})
			if err != nil {
				t.Fatal(err)
			}
			job = themeScanTestWaitStopped(t, ctx, store, job.ID)
			trace.assertQueries(t, []int{3})
			if snapshot(t, ctx, pool, lib.ID) != before || originalMediaReadOwners.Stats().RegisteredOwners != owners || originalMediaReadGovernor.Stats() != io {
				t.Fatalf("failed sort query changed publication or retained source ownership: %+v", job)
			}
		})
	}
}
