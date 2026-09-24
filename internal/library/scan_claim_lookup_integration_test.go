package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scanClaimLookupQuery struct {
	SQL  string
	Args []any
}

type scanClaimLookupTracer struct {
	mu      sync.Mutex
	queries []scanClaimLookupQuery
}

func (trace *scanClaimLookupTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "FROM items") {
		trace.mu.Lock()
		trace.queries = append(trace.queries, scanClaimLookupQuery{SQL: data.SQL, Args: append([]any(nil), data.Args...)})
		trace.mu.Unlock()
	}
	return ctx
}

func (*scanClaimLookupTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *scanClaimLookupTracer) take() []scanClaimLookupQuery {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	queries := trace.queries
	trace.queries = nil
	return queries
}

func scanClaimLookupState(t *testing.T, ctx context.Context, store *Store, library Library, rootPath string) *scanState {
	t.Helper()
	var root libraryRoot
	if err := store.pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id=$1 AND path=$2`, library.ID, rootPath).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatal(err)
	}
	opened, err := store.openLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := opened.Close(); err != nil {
			t.Errorf("close lookup test root: %v", err)
		}
	})
	return &scanState{store: store, task: &scanTask{ctx: ctx}, library: library, root: root, opened: opened,
		themes: &themeScan{claimed: make(map[string]string)}}
}

func scanClaimLookupInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("read regular lookup fixture: %v", err)
	}
	return info
}

func TestScanClaimLookupExactPathQueryDoesNotGrowWithClaims(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:exact-claim-query")
	library := libraryIntegrationCreate(t, ctx, store, "Bounded exact lookup", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	info := scanClaimLookupInfo(t, path)
	trace := &scanClaimLookupTracer{}
	configuration := pool.Config()
	configuration.MaxConns, configuration.MinConns = 1, 0
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	// Only read-only path lookup uses this pool; the real Store keeps ownership.
	state.store = &Store{pool: tracedPool}
	for _, role := range []scannedMediaRole{scannedRoleOrdinary, scannedRoleTheme, scannedRoleExtra} {
		t.Run(string(role), func(t *testing.T) {
			var baseline []byte
			for _, count := range []int{0, 1, 10000} {
				state.themes.claimed = make(map[string]string, count)
				for index := 0; index < count; index++ {
					state.themes.claimed[fmt.Sprintf("unrelated-%06d", index)] = "ordinary:another-root:another-path"
				}
				trace.take()
				stored, err := state.findStoredFileForRole("Film.mp4", info, role)
				if err != nil || stored.id != item.ID {
					t.Fatalf("lookup with %d unrelated claims returned %q: %v", count, stored.id, err)
				}
				queries := trace.take()
				if len(queries) != 2 {
					t.Fatalf("exact lookup issued %d item queries, want conflict and exact lookup only", len(queries))
				}
				for _, query := range queries {
					if !reflect.DeepEqual(query.Args, []any{state.root.id, "Film.mp4"}) {
						t.Fatalf("exact lookup serialized non-scalar or unrelated claim arguments: %#v", query.Args)
					}
				}
				encoded, err := json.Marshal(queries)
				if err != nil {
					t.Fatal(err)
				}
				if baseline == nil {
					baseline = encoded
				} else if string(encoded) != string(baseline) {
					t.Fatalf("actual exact lookup SQL/arguments changed with claim count: %d versus %d bytes", len(encoded), len(baseline))
				}
				if len(state.themes.claimed) != count {
					t.Fatal("read-only lookup registered a claim before media inspection")
				}
			}
		})
	}
}

func TestScanClaimLookupExactPathHonorsClaimIdentityBeforeDecoding(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, root, "movies/Film.mp4", "video:claim-identity")
	library := libraryIntegrationCreate(t, ctx, store, "Claim identity", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(path))
	info := scanClaimLookupInfo(t, path)
	key := string(scannedRoleOrdinary) + ":" + state.root.id + ":Film.mp4"
	for _, shared := range []bool{false, true} {
		for _, claim := range []struct {
			name, key string
			wantID    string
		}{
			{"same_key", key, item.ID},
			{"different_path", string(scannedRoleOrdinary) + ":" + state.root.id + ":Other.mp4", ""},
			{"different_root", "ordinary:another-root:Film.mp4", ""},
			{"different_role", string(scannedRoleTheme) + ":" + state.root.id + ":Film.mp4", ""},
		} {
			t.Run(fmt.Sprintf("shared_%t/%s", shared, claim.name), func(t *testing.T) {
				claims := map[string]string{item.ID: claim.key}
				state.themeLibrary = nil
				state.themes.claimed = claims
				if shared {
					// The root-local map must not override the library-wide owner.
					local := key
					if claim.key == key {
						local = "ordinary:stale-root:stale-path"
					}
					state.themes.claimed = map[string]string{item.ID: local}
					state.themeLibrary = &themeLibraryScan{claimed: claims}
				}
				stored, err := state.findStoredFileForRole("Film.mp4", info, scannedRoleOrdinary)
				if err != nil || stored.id != claim.wantID {
					t.Fatalf("claimed lookup returned %q, want %q: %v", stored.id, claim.wantID, err)
				}
				if len(claims) != 1 || claims[item.ID] != claim.key {
					t.Fatal("lookup changed the accepted inspection claim")
				}
			})
		}
	}
	// SQL used to exclude a claimed row before its JSON reached the decoder.
	if _, err := pool.Exec(ctx, `UPDATE items SET media='"invalid media object"'::jsonb WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	state.themeLibrary = nil
	state.themes.claimed = map[string]string{item.ID: "ordinary:another-root:Other.mp4"}
	if stored, err := state.findStoredFileForRole("Film.mp4", info, scannedRoleOrdinary); err != nil || stored.id != "" {
		t.Fatalf("claimed malformed metadata was decoded or reused: id=%q error=%v", stored.id, err)
	}
	state.themes.claimed[item.ID] = key
	if _, err := state.findStoredFileForRole("Film.mp4", info, scannedRoleOrdinary); err == nil {
		t.Fatal("available malformed metadata unexpectedly bypassed decoding")
	}
}

func TestScanClaimLookupPreservesRootAndPermanentRoleBoundaries(t *testing.T) {
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	first := libraryIntegrationFile(t, root, "first/Film.mp4", "video:first-root")
	second := libraryIntegrationFile(t, root, "second/Film.mp4", "video:second-root")
	themePath := libraryIntegrationFile(t, root, "first/RetainedTheme.mp4", "video:retained-theme")
	extraPath := libraryIntegrationFile(t, root, "first/RetainedExtra.mp4", "video:retained-extra")
	library, err := store.CreateLibrary(ctx, "Lookup boundaries", "movies", []string{filepath.Dir(first), filepath.Dir(second)})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	firstItem := nfoCatalogItem(t, ctx, store, userID, library.ID, first)
	secondItem := nfoCatalogItem(t, ctx, store, userID, library.ID, second)
	themeItem := nfoCatalogItem(t, ctx, store, userID, library.ID, themePath)
	extraItem := nfoCatalogItem(t, ctx, store, userID, library.ID, extraPath)
	firstState := scanClaimLookupState(t, ctx, store, library, filepath.Dir(first))
	secondState := scanClaimLookupState(t, ctx, store, library, filepath.Dir(second))
	if firstItem.ID == secondItem.ID {
		t.Fatal("two registered roots did not receive distinct item identities")
	}
	for _, fixture := range []struct {
		state    *scanState
		path, id string
	}{{firstState, first, firstItem.ID}, {secondState, second, secondItem.ID}} {
		stored, err := fixture.state.findStoredFileForRole("Film.mp4", scanClaimLookupInfo(t, fixture.path), scannedRoleOrdinary)
		if err != nil || stored.id != fixture.id || stored.rootID != fixture.state.root.id {
			t.Fatalf("exact lookup crossed a registered root: %+v, %v", stored, err)
		}
	}
	for _, fixture := range []struct {
		path, id, relative, table, kind string
		allowed, opposite               scannedMediaRole
	}{
		{themePath, themeItem.ID, "RetainedTheme.mp4", "item_theme_resources", "video", scannedRoleTheme, scannedRoleExtra},
		{extraPath, extraItem.ID, "RetainedExtra.mp4", "item_extra_resources", "clip", scannedRoleExtra, scannedRoleTheme},
	} {
		t.Run(string(fixture.allowed), func(t *testing.T) {
			if _, err := pool.Exec(ctx, "INSERT INTO "+fixture.table+"(resource_item_id,owner_item_id,kind,active) VALUES($1,$2,$3,false)",
				fixture.id, firstItem.ID, fixture.kind); err != nil {
				t.Fatal(err)
			}
			info := scanClaimLookupInfo(t, fixture.path)
			stored, err := firstState.findStoredFileForRole(fixture.relative, info, fixture.allowed)
			if err != nil || stored.id != fixture.id {
				t.Fatalf("same permanent role lost its identity: %q, %v", stored.id, err)
			}
			for _, role := range []scannedMediaRole{scannedRoleOrdinary, fixture.opposite} {
				if stored, err := firstState.findStoredFileForRole(fixture.relative, info, role); !errors.Is(err, errScannedMediaRoleConflict) || stored.id != "" {
					t.Fatalf("%s reused or replaced an inactive %s identity: %q, %v", role, fixture.allowed, stored.id, err)
				}
			}
			if runtime.GOOS == "linux" {
				moved := "Moved" + fixture.relative
				if err := os.Rename(fixture.path, filepath.Join(firstState.root.path, moved)); err != nil {
					t.Fatal(err)
				}
				for _, role := range []scannedMediaRole{scannedRoleOrdinary, fixture.opposite} {
					stored, err := firstState.findStoredFileForRole(moved, info, role)
					if err != nil || stored.id != "" {
						t.Fatalf("rename fallback stole an inactive %s identity for %s: %q, %v", fixture.allowed, role, stored.id, err)
					}
				}
				stored, err := firstState.findStoredFileForRole(moved, info, fixture.allowed)
				if err != nil || stored.id != fixture.id {
					t.Fatalf("same-role rename lost its candidate: %q, %v", stored.id, err)
				}
			}
		})
	}
	if _, err := pool.Exec(ctx, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory) VALUES($1,'Film.mp4',false)`, firstState.root.id); err != nil {
		t.Fatal(err)
	}
	if _, err := firstState.findStoredFileForRole("Film.mp4", scanClaimLookupInfo(t, first), scannedRoleOrdinary); !errors.Is(err, errScannedMediaRoleConflict) {
		t.Fatalf("ordinary exact lookup ignored a reserved path: %v", err)
	}
	if stored, err := secondState.findStoredFileForRole("Film.mp4", scanClaimLookupInfo(t, second), scannedRoleOrdinary); err != nil || stored.id != secondItem.ID {
		t.Fatalf("reservation leaked into another root: %q, %v", stored.id, err)
	}
}

func TestScanClaimLookupRenameExcludesClaimsBeforeCandidateLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable hardlink and rename identity requires Linux")
	}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	first := libraryIntegrationFile(t, root, "movies/Old00.mp4", "video:ninth-rename-candidate")
	paths := []string{first}
	for index := 1; index < 9; index++ {
		path := filepath.Join(filepath.Dir(first), fmt.Sprintf("Old%02d.mp4", index))
		if err := os.Link(first, path); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Filtered rename candidates", "movies", filepath.Dir(first))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	state := scanClaimLookupState(t, ctx, store, library, filepath.Dir(first))
	state.themeLibrary = &themeLibraryScan{claimed: make(map[string]string)}
	ids := make([]string, 9)
	seen := make(map[string]bool, 9)
	for index, path := range paths {
		item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
		if seen[item.ID] {
			t.Fatal("initial hardlink scan reused an item ID")
		}
		seen[item.ID], ids[index] = true, item.ID
		if _, err := pool.Exec(ctx, `UPDATE items SET created_at=$2 WHERE id=$1`, item.ID,
			time.Date(2020, 1, 1, 0, 0, index, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		if index < 8 {
			state.themeLibrary.claimed[item.ID] = fmt.Sprintf("ordinary:%s:Claimed%02d.mp4", state.root.id, index)
		}
	}
	destination := filepath.Join(filepath.Dir(first), "Renamed.mp4")
	if err := os.Rename(paths[8], destination); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths[:8] {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	info := scanClaimLookupInfo(t, destination)
	stored, err := state.findStoredFileForRole("Renamed.mp4", info, scannedRoleOrdinary)
	if err != nil || stored.id != ids[8] {
		t.Fatalf("eight claimed candidates hid the ninth unclaimed rename: %q, want %q, error=%v", stored.id, ids[8], err)
	}
	state.themeLibrary.claimed[ids[8]] = "ordinary:" + state.root.id + ":Renamed.mp4"
	if stored, err := state.findStoredFileForRole("Renamed.mp4", info, scannedRoleOrdinary); err != nil || stored.id != ids[8] {
		t.Fatalf("same-key rename lookup was not reentrant: %q, %v", stored.id, err)
	}
	state.themeLibrary.claimed[ids[8]] = "ordinary:another-root:Another.mp4"
	if stored, err := state.findStoredFileForRole("Renamed.mp4", info, scannedRoleOrdinary); err != nil || stored.id != "" {
		t.Fatalf("already claimed final rename candidate was reused: %q, %v", stored.id, err)
	}
}

func TestScanClaimLookupFullScanPreservesRenameAndHardlinkIdentities(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cross-root stable file identity requires Linux")
	}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	original := libraryIntegrationFile(t, root, "first/Original.mp4", "video:cross-root-claims")
	secondRoot := filepath.Join(root, "second")
	if err := os.Mkdir(secondRoot, 0700); err != nil {
		t.Fatal(err)
	}
	library, err := store.CreateLibrary(ctx, "Claimed cross-root identity", "movies", []string{filepath.Dir(original), secondRoot})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, original)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 3, PlaybackPositionTicks: 120})
	beforeUserData := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	destination := filepath.Join(secondRoot, "Renamed.mp4")
	before := scanClaimLookupInfo(t, original)
	if err := os.Rename(original, destination); err != nil {
		t.Fatal(err)
	}
	after := scanClaimLookupInfo(t, destination)
	if !os.SameFile(before, after) || before.Size() != after.Size() || !catalogModifiedTime(before).Equal(catalogModifiedTime(after)) {
		t.Fatal("cross-root fixture did not preserve rename identity")
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("cross-root rename scan failed: %+v", job)
	}
	moved := nfoCatalogItem(t, ctx, store, userID, library.ID, destination)
	if moved.ID != item.ID || forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
		t.Fatal("same-library cross-root rename lost identity or saved user state")
	}
	alias := filepath.Join(secondRoot, "A-Alias.mp4")
	if err := os.Link(destination, alias); err != nil {
		t.Fatal(err)
	}
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("hardlink scan failed: %+v", job)
	}
	linked := nfoCatalogItem(t, ctx, store, userID, library.ID, alias)
	moved = nfoCatalogItem(t, ctx, store, userID, library.ID, destination)
	if linked.ID == moved.ID || moved.ID != item.ID {
		t.Fatal("an earlier-walked hardlink alias stole the live original identity")
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if nfoCatalogItem(t, ctx, store, userID, library.ID, alias).ID != linked.ID ||
		nfoCatalogItem(t, ctx, store, userID, library.ID, destination).ID != item.ID ||
		forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != beforeUserData {
		t.Fatal("unchanged rescan merged hardlink identities or changed saved state")
	}
}
