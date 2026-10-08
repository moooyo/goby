//go:build linux

package library

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

var preparedPlanMemoryFamilies = [...]string{"media", "images", "lookup", "bitmap", "source"}

type preparedPlanMemoryTemplate struct {
	family, sql string
	args        []any
}

type preparedPlanMemoryInput struct {
	configuration     *pgx.ConnConfig
	templates         [5]preparedPlanMemoryTemplate
	itemID, libraryID string
}

type preparedPlanMemoryCaptureTrace struct {
	mu        sync.Mutex
	templates [5]preparedPlanMemoryTemplate
}

func preparedPlanMemoryCloneArgs(args []any) []any {
	if len(args) > 0 {
		if _, option := args[0].(pgx.QueryExecMode); option {
			args = args[1:]
		}
	}
	copyArgs := append([]any(nil), args...)
	for index, value := range copyArgs {
		switch values := value.(type) {
		case []string:
			copyArgs[index] = slices.Clone(values)
		case []int:
			copyArgs[index] = slices.Clone(values)
		case []int64:
			copyArgs[index] = slices.Clone(values)
		case []time.Time:
			copyArgs[index] = slices.Clone(values)
		case []byte:
			copyArgs[index] = slices.Clone(values)
		}
	}
	return copyArgs
}

func (trace *preparedPlanMemoryCaptureTrace) capture(sql string, args []any) {
	family := scanQueryPlanFamily(sql)
	if strings.HasPrefix(strings.TrimSpace(sql), "/* image_catalog_unchanged */") {
		family = "images"
	} else if family == "images" {
		family = "other"
	} else if strings.HasPrefix(sql, "SELECT i.id, i.library_id, i.type, i.path, i.media,") {
		family = "source"
	}
	for index, name := range preparedPlanMemoryFamilies {
		if family == name {
			trace.mu.Lock()
			if trace.templates[index].sql == "" {
				trace.templates[index] = preparedPlanMemoryTemplate{family: family, sql: sql, args: preparedPlanMemoryCloneArgs(args)}
			}
			trace.mu.Unlock()
			return
		}
	}
}

func (trace *preparedPlanMemoryCaptureTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.capture(data.SQL, data.Args)
	return ctx
}

func (*preparedPlanMemoryCaptureTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *preparedPlanMemoryCaptureTrace) TraceBatchStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, query := range data.Batch.QueuedQueries {
		trace.capture(query.SQL, query.Arguments)
	}
	return ctx
}

func (*preparedPlanMemoryCaptureTrace) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {
}
func (*preparedPlanMemoryCaptureTrace) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
}

// Capture five real read-only call sites once, before measuring any target
// backend. No target connection or growing policy-SQL history is retained here.
func preparedPlanMemoryCapture(t *testing.T) preparedPlanMemoryInput {
	t.Helper()
	base := mediaSourceTestCatalog(t, nil)
	imageScanTestWrite(t, filepath.Join(filepath.Dir(base.path), "Feature-poster.png"), color.White)
	libraryIntegrationScan(t, base.ctx, base.store, base.library.ID, "Completed")
	if err := base.store.Close(base.ctx); err != nil {
		t.Fatal(err)
	}
	trace := &preparedPlanMemoryCaptureTrace{}
	configuration := base.pool.Config().Copy()
	configuration.ConnConfig.Tracer = trace
	configuration.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	configuration.ConnConfig.StatementCacheCapacity = 0
	configuration.ConnConfig.DescriptionCacheCapacity = 512
	pool, err := pgxpool.NewWithConfig(base.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, base.store.prober, []string{base.allowedRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("close the plan-template capture store: %v", err)
		}
	})
	state := imageScanTestState(t, base.ctx, base.pool, store, base.library, "Nested")
	walk := primaryScanRoutingRetainWalk(t, state)
	primary := scanClaimLookupInfo(t, base.path)
	if unchanged, err := state.cachedSubtitleScanEmpty(base.item.ID, "Nested/Feature.mkv", primary); err != nil || !unchanged {
		t.Fatalf("capture the real owner media template: unchanged=%t error=%v", unchanged, err)
	}
	if err := state.scanImages(base.item.ID, "Movie", "Nested/Feature.mkv", false); err != nil || state.warnings != 0 {
		t.Fatalf("capture the real unchanged image template: warnings=%d error=%v", state.warnings, err)
	}
	entries, err := os.ReadDir(filepath.Dir(base.path))
	if err != nil {
		t.Fatal(err)
	}
	state.subtitleDirectories = map[string]*subtitleDirectoryIndex{"Nested": newSubtitleDirectoryIndex(entries, base.library.CollectionType, state.directoryIdentities["Nested"])}
	if err := state.scanBitmapSubtitlesAttempt(base.item.ID, "Nested/Feature.mkv", base.item.Media); err != nil || state.warnings != 0 {
		t.Fatalf("capture the real pooled bitmap template: warnings=%d error=%v", state.warnings, err)
	}
	if stored, err := state.findStoredFileForRole("Nested/Feature.mkv", primary, scannedRoleOrdinary); err != nil || stored.id != base.item.ID {
		t.Fatalf("capture the real ordinary lookup template: id=%q error=%v", stored.id, err)
	}
	tx, err := pool.Begin(base.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := readIndexedPlaybackMediaBatch(base.ctx, tx, unrestrictedLibraryAccess(), base.item.ID, media.SourceID(base.item.ID), false)
	endErr := tx.Rollback(base.ctx)
	if readErr != nil || endErr != nil {
		t.Fatalf("capture the real playback source template: read=%v rollback=%v", readErr, endErr)
	}
	trace.mu.Lock()
	templates := trace.templates
	trace.mu.Unlock()
	wantArgs := [...]int{4, 17, 2, 1, 3}
	for index, template := range templates {
		if template.family != preparedPlanMemoryFamilies[index] || template.sql == "" || len(template.args) != wantArgs[index] {
			t.Fatalf("actual plan-template capture is incomplete for %s", preparedPlanMemoryFamilies[index])
		}
	}
	if templates[4].sql != indexedPlaybackMediaSQL(unrestrictedLibraryAccess()) {
		t.Fatal("the captured source query differs from the production SQL builder")
	}
	imageScanTestRetireTask(t, base.pool, store, state.task)
	if err := walk.Close(); err != nil {
		t.Fatal(err)
	}
	state.walkIO = nil
	if err := state.opened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(base.ctx); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	input := preparedPlanMemoryInput{configuration: base.pool.Config().ConnConfig.Copy(), templates: templates,
		itemID: base.item.ID, libraryID: base.library.ID}
	input.configuration.Tracer = nil
	input.configuration.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	input.configuration.DescriptionCacheCapacity = 512
	input.configuration.RuntimeParams["jit"] = "off"
	return input
}

// Each additional shape is generated by the production policy parser and SQL
// builder. Only the current shape exists outside the driver's bounded cache.
func (input *preparedPlanMemoryInput) sourceShape(index int) (preparedPlanMemoryTemplate, error) {
	if index == 0 {
		return input.templates[4], nil
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false,
		"EnabledFolders": []string{input.libraryID, fmt.Sprintf("memory-unused-scope-%03d", index)}})
	if err != nil {
		return preparedPlanMemoryTemplate{}, err
	}
	access, err := parseLibraryPolicy(policy)
	if err != nil {
		return preparedPlanMemoryTemplate{}, err
	}
	access.userID = fmt.Sprintf("memory-scope-user-%03d", index)
	return preparedPlanMemoryTemplate{family: "source", sql: indexedPlaybackMediaSQL(access),
		args: []any{input.itemID, access.all, access.folders}}, nil
}
