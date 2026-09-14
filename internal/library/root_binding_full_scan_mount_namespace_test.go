//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/storagebinding"
	"golang.org/x/sys/unix"
)

// This test never creates a namespace or changes propagation. The reviewed
// launcher supplies a private mount namespace, an owned PostgreSQL fixture,
// an ext4 GOTMPDIR, and pinned small video and ffprobe inputs. Ordinary runs
// skip explicitly; only the scenario and cleanup markers establish execution.
//
// GOBY_ROOT_BINDING_FULL_SCAN_MOUNT_HELPER=1
// GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE=mnt:[the-original-host-inode]
// GOBY_TEST_DATABASE_URL=the-reviewed-owned-test-database
// GOTMPDIR=an-existing-owned-ext4-directory
// GOBY_ROOT_BINDING_FULL_SCAN_MEDIA=an-absolute-small-self-contained-video
// GOBY_ROOT_BINDING_FULL_SCAN_MEDIA_SHA256=its-lowercase-sha256
// GOBY_ROOT_BINDING_FULL_SCAN_FFPROBE=an-absolute-real-ffprobe-executable
// GOBY_ROOT_BINDING_FULL_SCAN_FFPROBE_SHA256=its-lowercase-sha256
func TestRootBindingFullScanMountNamespaceHelper(t *testing.T) {
	if os.Getenv("GOBY_ROOT_BINDING_FULL_SCAN_MOUNT_HELPER") != "1" {
		t.Skip("the full-scan mount scenario requires its explicit reviewed opt-in scope")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getuid() != 0 || os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("the full-scan helper requires root and its owned PostgreSQL fixture")
	}
	host := os.Getenv("GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE")
	if !strings.HasPrefix(host, "mnt:[") || !validRootMountFilesystemRoot(host, "nsfs") {
		t.Fatal("the exact original host mount namespace witness is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	namespace, _, err := rootTopologyHelperNamespace(ctx, host, "")
	if err != nil {
		t.Fatal(err)
	}
	base := os.Getenv("GOTMPDIR")
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil || !filepath.IsAbs(base) || filepath.Clean(base) != base || canonical != base {
		t.Fatal("the reviewed GOTMPDIR must be an existing canonical absolute directory")
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(base, &filesystem); err != nil || filesystem.Type != unix.EXT4_SUPER_MAGIC {
		t.Fatal("the full-scan media and bind mounts require ext4, not tmpfs")
	}
	_, mediaBytes := rootBindingFullScanInput(t, "MEDIA", 1<<20, false)
	ffprobePath, _ := rootBindingFullScanInput(t, "FFPROBE", 256<<20, true)
	directory, err := os.MkdirTemp(base, "goby-root-binding-full-scan-mount-")
	if err != nil {
		t.Fatal("create the exclusive full-scan mount fixture")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || filepath.Dir(directory) != base {
		t.Fatal("the exclusive full-scan fixture could not be identified")
	}
	helper := &rootTopologyMountHelper{ctx: ctx, host: host, namespace: namespace, directory: directory, identity: info,
		sources: make(map[string]RootStorageIdentity), targets: make(map[string]bool)}
	closedStores := make(map[*Store]bool)
	closeStore := func(store *Store) error {
		if closedStores[store] {
			return nil
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer closeCancel()
		if err := store.Close(closeCtx); err != nil {
			return err
		}
		closedStores[store] = true
		return nil
	}
	// Register this before libraryIntegrationStore. Its Store, pool and random
	// schema cleanups run first. The mount fixture is an independent MkdirTemp,
	// never inside a testing.TempDir that could remove mounted source contents.
	t.Cleanup(func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for _, closed := range closedStores {
			if !closed {
				t.Errorf("a library store did not close; the owned mount fixture is retained at %s", directory)
				return
			}
		}
		if err := helper.cleanup(); err != nil {
			t.Errorf("ordinary mount cleanup failed; fixture retained at %s: %v", directory, err)
			return
		}
		t.Log("m2_full_scan_mount_cleanup_completed=true stores_closed=true owned_mounts_remaining=0 fixture_removed=true")
	})
	registerStore := func(store *Store) {
		closedStores[store] = false
		t.Cleanup(func() {
			if err := closeStore(store); err != nil {
				t.Errorf("close full-scan library store: %v", err)
			}
		})
	}
	approved := filepath.Join(directory, "approved")
	registered := filepath.Join(approved, "registered")
	nested := filepath.Join(registered, "nested")
	one, replacement := filepath.Join(directory, "source-original"), filepath.Join(directory, "source-empty")
	for _, path := range []string{nested, one, replacement} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	missingPath := libraryIntegrationFile(t, registered, "missing.mp4", string(mediaBytes))
	parentPath := libraryIntegrationFile(t, registered, "parent-survivor.mp4", string(mediaBytes))
	libraryIntegrationFile(t, one, "nested-survivor.mp4", string(mediaBytes))
	nestedPath := filepath.Join(nested, "nested-survivor.mp4")
	for _, source := range []string{one, replacement} {
		observation, err := rootTopologyHelperDirectory(source)
		if err != nil {
			t.Fatalf("observe the owned bind source: %v", err)
		}
		helper.sources[source] = observation.Identity
	}
	if helper.sources[one].Equal(helper.sources[replacement]) {
		t.Fatal("the original and empty replacement need distinct directory identities")
	}
	helper.targets[nested] = true
	if err := helper.mount(one, nested); err != nil {
		t.Fatal(err)
	}
	prober := &rootBindingFullScanRealProber{Prober: media.Prober{FFprobePath: ffprobePath, Timeout: 10 * time.Second}}
	databaseCtx, pool, store, _, userID := libraryIntegrationStore(t, prober)
	registerStore(store)
	store.mu.Lock()
	store.roots = append(store.roots, approvedRoot{path: approved})
	store.mu.Unlock()
	library := libraryIntegrationCreate(t, databaseCtx, store, "Nested mount full-scan protection", "movies", registered)
	scanReconciliationAssertBound(t, databaseCtx, pool, library.ID)
	var bindingDocument, bindingBefore string
	if err := pool.QueryRow(databaseCtx, `SELECT storage_binding::text, to_jsonb(r)::text
		FROM library_roots r WHERE library_id=$1`, library.ID).Scan(&bindingDocument, &bindingBefore); err != nil {
		t.Fatal(err)
	}
	snapshot, err := storagebinding.DecodeSnapshot([]byte(bindingDocument))
	if err != nil || snapshot.Mapping.ApprovedPath != approved || snapshot.Mapping.RegisteredPath != registered ||
		len(snapshot.Boundaries) != 1 || snapshot.Boundaries[0].RelativePath != "nested" ||
		!snapshot.Boundaries[0].Identity.Equal(helper.sources[one]) {
		t.Fatal("the persisted approval does not bind the actual nested source")
	}
	libraryIntegrationScan(t, databaseCtx, store, library.ID, "Completed")
	missing := nfoCatalogItem(t, databaseCtx, store, userID, library.ID, missingPath)
	parent := nfoCatalogItem(t, databaseCtx, store, userID, library.ID, parentPath)
	inside := nfoCatalogItem(t, databaseCtx, store, userID, library.ID, nestedPath)
	for _, item := range []Item{missing, parent, inside} {
		video := false
		if item.Media != nil {
			for _, stream := range item.Media.Streams {
				video = video || stream.CodecType == "video" && stream.Codec != "" && stream.Width > 0 && stream.Height > 0
			}
		}
		if item.Type != "Movie" || !video || item.Media.ProbeVersion != media.CurrentProbeVersion ||
			item.Media.DurationTicks <= 0 || item.Media.Size != int64(len(mediaBytes)) {
			t.Fatal("the initial scan did not persist real ffprobe video facts")
		}
	}
	if prober.calls.Load() != 3 {
		t.Fatal("the initial scan did not probe exactly the three declared media copies")
	}
	userData := make(map[string]string)
	for index, item := range []Item{missing, parent, inside} {
		userDataSeed(t, databaseCtx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true,
			PlayCount: index + 3, PlaybackPositionTicks: int64(index+1) * 100000})
		userData[item.ID] = forceProbeUserDataSnapshot(t, databaseCtx, pool, item.ID)
	}
	initialIDs := rootBindingFullScanIDs(t, databaseCtx, pool, library.ID)
	notifications := catalogChangesTestListener(t, store)
	t.Logf("m2_full_scan_mount_phase=initial_scan real_ffprobe=true media_items=3 library_items=%d userdata_rows=3", len(initialIDs))
	assertApproval := func() {
		t.Helper()
		var current string
		if err := pool.QueryRow(databaseCtx, `SELECT to_jsonb(r)::text FROM library_roots r WHERE library_id=$1`, library.ID).Scan(&current); err != nil || current != bindingBefore {
			t.Fatal("scanning changed the original persisted root approval")
		}
		var rebinds int
		if err := pool.QueryRow(databaseCtx, `SELECT count(*) FROM activity_entries WHERE action='library.root_binding.updated'`).Scan(&rebinds); err != nil || rebinds != 0 {
			t.Fatal("scanning manufactured an explicit storage rebind")
		}
	}
	assertUserData := func(ids ...string) {
		t.Helper()
		for _, id := range ids {
			if forceProbeUserDataSnapshot(t, databaseCtx, pool, id) != userData[id] {
				t.Fatal("an unchanged item lost its exact seeded user history")
			}
		}
	}
	preserveScan := func(phase string) {
		t.Helper()
		libraryIntegrationScan(t, databaseCtx, store, library.ID, "Completed")
		if !reflect.DeepEqual(rootBindingFullScanIDs(t, databaseCtx, pool, library.ID), initialIDs) {
			t.Fatal("incomplete nested storage evidence changed the complete library item set")
		}
		assertUserData(missing.ID, parent.ID, inside.ID)
		assertApproval()
		if removed := scanReconciliationRemoved(t, notifications, library.ID); len(removed) != 0 {
			t.Fatal("an unavailable or replacement nested mount emitted Removed facts")
		}
		t.Logf("m2_full_scan_mount_phase=%s library_items_preserved=true userdata_exact=true removed_items=0 approval_unchanged=true", phase)
	}
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}
	// No test-owned descriptor is retained inside nested. A terminal full scan
	// has closed its topology/evidence witnesses; a busy ordinary unmount fails.
	if err := helper.unmount(nested, 0); err != nil {
		t.Fatal(err)
	}
	preserveScan("mount_absent_first")
	preserveScan("mount_absent_repeat")
	if err := helper.mount(replacement, nested); err != nil {
		t.Fatal(err)
	}
	preserveScan("empty_replacement")
	if err := closeStore(store); err != nil {
		t.Fatal(err)
	}
	store, err = New(pool, prober, []string{approved})
	if err != nil {
		t.Fatalf("restart the store while the empty replacement is mounted: %v", err)
	}
	registerStore(store)
	notifications = catalogChangesTestListener(t, store)
	preserveScan("empty_replacement_after_restart")
	if err := helper.unmount(nested, 0); err != nil {
		t.Fatal(err)
	}
	if err := helper.mount(one, nested); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, databaseCtx, store, library.ID, "Completed")
	want := make([]string, 0, len(initialIDs)-1)
	for _, id := range initialIDs {
		if id != missing.ID {
			want = append(want, id)
		}
	}
	if !reflect.DeepEqual(rootBindingFullScanIDs(t, databaseCtx, pool, library.ID), want) ||
		!reflect.DeepEqual(scanReconciliationRemoved(t, notifications, library.ID), []string{missing.ID}) {
		t.Fatal("restoring the original mount did not remove only the genuinely absent parent item")
	}
	if forceProbeUserDataSnapshot(t, databaseCtx, pool, missing.ID) != "[]" {
		t.Fatal("the proven missing item retained orphan user state after deletion")
	}
	assertUserData(parent.ID, inside.ID)
	assertApproval()
	for _, item := range []Item{parent, inside} {
		if current := nfoCatalogItem(t, databaseCtx, store, userID, library.ID, item.Path); current.ID != item.ID {
			t.Fatal("an original survivor changed its stable catalog identity")
		}
	}
	t.Log("m2_full_scan_mount_phase=original_remounted_scan removed_items=1 survivor_ids_stable=true survivor_userdata_exact=true approval_unchanged=true")
	libraryIntegrationScan(t, databaseCtx, store, library.ID, "Completed")
	if !reflect.DeepEqual(rootBindingFullScanIDs(t, databaseCtx, pool, library.ID), want) ||
		len(scanReconciliationRemoved(t, notifications, library.ID)) != 0 {
		t.Fatal("the stable restored rescan changed membership or repeated a removal")
	}
	assertUserData(parent.ID, inside.ID)
	assertApproval()
	for _, path := range []string{parentPath, filepath.Join(one, "nested-survivor.mp4")} {
		raw, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(raw) != sha256.Sum256(mediaBytes) {
			t.Fatal("scanning changed an original surviving media copy")
		}
	}
	t.Log("m2_full_scan_mount_phase=restored_rescan removed_items=0 survivor_ids_stable=true survivor_userdata_exact=true")
	if _, _, err := rootTopologyHelperNamespace(ctx, host, namespace); err != nil {
		t.Fatal(err)
	}
	if err := closeStore(store); err != nil {
		t.Fatal(err)
	}
	t.Log("m2_full_scan_mount_scenario_passed=true full_scans=7 real_ffprobe=true ordinary_unmount_only=true")
}

type rootBindingFullScanRealProber struct {
	media.Prober
	calls atomic.Int32
}

func (prober *rootBindingFullScanRealProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	prober.calls.Add(1)
	return prober.Prober.ProbeFile(ctx, file)
}

func rootBindingFullScanInput(t *testing.T, name string, maximum int64, executable bool) (string, []byte) {
	t.Helper()
	path := os.Getenv("GOBY_ROOT_BINDING_FULL_SCAN_" + name)
	expected := os.Getenv("GOBY_ROOT_BINDING_FULL_SCAN_" + name + "_SHA256")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(expected) != expected {
		t.Fatal("a fixed input requires its complete lowercase SHA256")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || canonical != path {
		t.Fatal("a fixed input must have a canonical absolute nonsymlink path")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximum ||
		before.Mode().Perm()&0o022 != 0 || executable && before.Mode().Perm()&0o111 == 0 {
		t.Fatal("a fixed input has an invalid type, size or mode")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("open the fixed input")
	}
	digest := sha256.New()
	var raw []byte
	var count int64
	var readErr error
	if executable {
		// Hash the pinned tool without retaining another binary-sized allocation.
		count, readErr = io.Copy(digest, io.LimitReader(file, maximum+1))
	} else {
		raw, readErr = io.ReadAll(io.LimitReader(file, maximum+1))
		count = int64(len(raw))
		_, _ = digest.Write(raw)
	}
	opened, statErr := file.Stat()
	closeErr := file.Close()
	after, pathErr := os.Lstat(path)
	if readErr != nil || statErr != nil || closeErr != nil || pathErr != nil || count != before.Size() ||
		!os.SameFile(before, opened) || !os.SameFile(before, after) || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) != media.FileChangeTime(after) {
		t.Fatal("a fixed input changed during its bounded read")
	}
	if hex.EncodeToString(digest.Sum(nil)) != expected {
		t.Fatal("a fixed input differs from its approved SHA256")
	}
	return path, raw
}

func rootBindingFullScanIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id FROM items WHERE library_id=$1 ORDER BY id LIMIT 17`, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) < 3 || len(ids) > 16 || !sort.StringsAreSorted(ids) {
		t.Fatal("the complete library item set is invalid or exceeds the tiny fixture")
	}
	return ids
}
