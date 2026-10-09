//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type musicAlbumDiscoveryCommitKey struct{}

type musicAlbumDiscoveryTrace struct {
	mu                sync.Mutex
	batches           [][]string
	commits           int
	cancelAtDiscovery bool
	cancelAfterCommit int
	cancel            context.CancelFunc
}

func (trace *musicAlbumDiscoveryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "WITH RECURSIVE music_album_ancestors AS") {
		trace.mu.Lock()
		parents, _ := data.Args[0].([]string)
		trace.batches = append(trace.batches, append([]string(nil), parents...))
		var cancel context.CancelFunc
		if trace.cancelAtDiscovery {
			cancel, trace.cancel = trace.cancel, nil
		}
		trace.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") {
		return context.WithValue(ctx, musicAlbumDiscoveryCommitKey{}, true)
	}
	return ctx
}

func (trace *musicAlbumDiscoveryTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if commit, _ := ctx.Value(musicAlbumDiscoveryCommitKey{}).(bool); !commit || data.Err != nil {
		return
	}
	trace.mu.Lock()
	trace.commits++
	var cancel context.CancelFunc
	if trace.cancelAfterCommit > 0 && trace.commits == trace.cancelAfterCommit {
		cancel, trace.cancel = trace.cancel, nil
	}
	trace.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (trace *musicAlbumDiscoveryTrace) reset(cancel context.CancelFunc, atDiscovery bool, afterCommit int) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.batches, trace.commits = nil, 0
	trace.cancel, trace.cancelAtDiscovery, trace.cancelAfterCommit = cancel, atDiscovery, afterCommit
}

func (trace *musicAlbumDiscoveryTrace) assertBatches(t *testing.T, parents int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	want := (parents + acceptedMusicAlbumParentBatch - 1) / acceptedMusicAlbumParentBatch
	if len(trace.batches) != want {
		t.Fatalf("album discovery used %d statements for %d parents, want %d", len(trace.batches), parents, want)
	}
	total := 0
	for _, batch := range trace.batches {
		if len(batch) == 0 || len(batch) > acceptedMusicAlbumParentBatch {
			t.Fatalf("album discovery retained an invalid input batch of %d parents", len(batch))
		}
		total += len(batch)
	}
	if total != parents {
		t.Fatalf("album discovery supplied %d parent IDs, want %d", total, parents)
	}
}

func musicAlbumDiscoveryFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Store, *musicAlbumDiscoveryTrace) {
	t.Helper()
	trace := &musicAlbumDiscoveryTrace{}
	ctx, pool, store, _, _ := catalogBatchTestStoreWithTracer(t, trace)
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('music-discovery','Music discovery','music'),('music-discovery-other','Other music','music');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('music-discovery-complete','music-discovery','/music-discovery/complete','/music-discovery','complete'),
		('music-discovery-incomplete','music-discovery','/music-discovery/incomplete','/music-discovery','incomplete'),
		('music-discovery-foreign','music-discovery-other','/music-discovery/other','/music-discovery','other');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES
		('foreign-album','music-discovery-other','music-discovery-foreign','Foreign','foreign','MusicAlbum',true,'foreign-album');
		WITH nodes(id,parent_id,type,is_folder,root_id,relative_path) AS (VALUES
		('outer-album',NULL::text,'MusicAlbum',true,'music-discovery-complete','outer-album'),
		('inner-album','outer-album','MusicAlbum',true,'music-discovery-complete','inner-album'),
		('inner-leaf-a','inner-album','Folder',true,'music-discovery-complete','inner-leaf-a'),
		('inner-leaf-b','inner-album','Folder',true,'music-discovery-complete','inner-leaf-b'),
		('shared-leaf','inner-leaf-a','Folder',true,'music-discovery-complete','shared-leaf'),
		('not-folder-album','outer-album','MusicAlbum',false,'music-discovery-complete','not-folder-album'),
		('not-folder-leaf','not-folder-album','Folder',true,'music-discovery-complete','not-folder-leaf'),
		('cycle-a','cycle-b','Folder',true,'music-discovery-complete','cycle-a'),
		('cycle-b','cycle-a','Folder',true,'music-discovery-complete','cycle-b'),
		('cross-library-leaf','foreign-album','Folder',true,'music-discovery-complete','cross-library-leaf'),
		('incomplete-album',NULL::text,'MusicAlbum',true,'music-discovery-incomplete','incomplete-album'),
		('incomplete-leaf','incomplete-album','Folder',true,'music-discovery-incomplete','incomplete-leaf'),
		('reserved-album',NULL::text,'MusicAlbum',true,'music-discovery-complete','reserved'),
		('reserved-leaf','reserved-album','Folder',true,'music-discovery-complete','reserved/leaf'),
		('reserved-ancestor-leaf','reserved-album','Folder',true,'music-discovery-complete','reserved-ancestor-leaf'))
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path)
		SELECT id,'music-discovery',root_id,parent_id,id,id,type,is_folder,relative_path FROM nodes;
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('music-discovery-complete','reserved',true);
		UPDATE item_metadata_state SET music_source='{"Version":1,"Name":"Prior accepted album"}'::jsonb
		WHERE item_id IN ('outer-album','inner-album','foreign-album','incomplete-album','reserved-album')`); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, store, trace
}

func TestMusicAlbumDiscoveryBatchesParentQueries(t *testing.T) {
	ctx, pool, store, trace := musicAlbumDiscoveryFixture(t)
	const maximum = 2*acceptedMusicAlbumParentBatch + 1
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path)
		SELECT 'batch-parent-'||n,'music-discovery','music-discovery-complete','inner-album',
		'Parent','parent','Folder',true,'batch-parent-'||n FROM generate_series(1,$1::integer) n`, maximum); err != nil {
		t.Fatal(err)
	}
	before := metadataMusicScanStateSnapshot(t, ctx, pool, "inner-album")
	for _, count := range []int{0, 1, acceptedMusicAlbumParentBatch - 1, acceptedMusicAlbumParentBatch, acceptedMusicAlbumParentBatch + 1, maximum} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			parents := make(map[string]bool, count)
			for number := 1; number <= count; number++ {
				parents[fmt.Sprintf("batch-parent-%d", number)] = true
			}
			trace.reset(nil, false, 0)
			warnings, err := store.refreshScannedMusicAlbums(ctx, "music-discovery", parents, nil)
			if err != nil || warnings != 0 {
				t.Fatalf("bounded album discovery failed: warnings=%d, error=%v", warnings, err)
			}
			trace.assertBatches(t, count)
			if metadataMusicScanStateSnapshot(t, ctx, pool, "inner-album") != before {
				t.Fatal("discovery published an album from an incomplete root")
			}
		})
	}
	parents := make(map[string]bool, maximum)
	for number := 1; number <= maximum; number++ {
		parents[fmt.Sprintf("batch-parent-%d", number)] = true
	}
	trace.reset(nil, false, 0)
	warnings, err := store.refreshScannedMusicAlbums(ctx, "music-discovery", parents, map[string]bool{"music-discovery-complete": true})
	if err != nil || warnings != 0 {
		t.Fatalf("shared album publication failed: warnings=%d, error=%v", warnings, err)
	}
	trace.assertBatches(t, maximum)
	trace.mu.Lock()
	commits := trace.commits
	trace.mu.Unlock()
	if commits != 2 || metadataMusicScanStateSnapshot(t, ctx, pool, "inner-album") == before {
		t.Fatalf("shared album across batches was not published exactly once: commits=%d", commits)
	}
}

func TestMusicAlbumDiscoveryKeepsIndependentNearestPaths(t *testing.T) {
	ctx, pool, store, trace := musicAlbumDiscoveryFixture(t)
	albums := []string{"inner-album", "outer-album", "foreign-album", "incomplete-album", "reserved-album"}
	before := make(map[string]string, len(albums))
	for _, id := range albums {
		before[id] = metadataMusicScanStateSnapshot(t, ctx, pool, id)
	}
	parents := map[string]bool{"inner-leaf-a": true, "inner-leaf-b": true, "shared-leaf": true,
		"cycle-a": true, "cycle-b": true, "cross-library-leaf": true, "incomplete-leaf": true,
		"reserved-leaf": true, "reserved-ancestor-leaf": true, "missing-parent": true}
	trace.reset(nil, false, 0)
	warnings, err := store.refreshScannedMusicAlbums(ctx, "music-discovery", parents, map[string]bool{"music-discovery-complete": true})
	if err != nil || warnings != 0 {
		t.Fatalf("independent nearest-album discovery failed: warnings=%d, error=%v", warnings, err)
	}
	trace.assertBatches(t, len(parents))
	for _, id := range albums {
		changed := metadataMusicScanStateSnapshot(t, ctx, pool, id) != before[id]
		if changed != (id == "inner-album") {
			t.Fatalf("album %s changed=%t; shared origins, nearest boundaries or root visibility were lost", id, changed)
		}
	}
	// A nonfolder MusicAlbum is not an album boundary for its descendants.
	warnings, err = store.refreshScannedMusicAlbums(ctx, "music-discovery", map[string]bool{"not-folder-leaf": true}, map[string]bool{"music-discovery-complete": true})
	if err != nil || warnings != 0 || metadataMusicScanStateSnapshot(t, ctx, pool, "outer-album") == before["outer-album"] {
		t.Fatalf("nonfolder album stopped discovery: warnings=%d, error=%v", warnings, err)
	}
	// A cached move retains both the old album and the moved parent as origins.
	if _, err := pool.Exec(ctx, `UPDATE items SET parent_id='outer-album' WHERE id='inner-leaf-a';
		UPDATE item_metadata_state SET music_source='{"Version":1,"Name":"Before cached move"}'::jsonb
		WHERE item_id IN ('inner-album','outer-album')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range albums[:2] {
		before[id] = metadataMusicScanStateSnapshot(t, ctx, pool, id)
	}
	parents = map[string]bool{"inner-album": true, "inner-leaf-a": true}
	warnings, err = store.refreshScannedMusicAlbums(ctx, "music-discovery", parents, map[string]bool{"music-discovery-complete": true})
	if err != nil || warnings != 0 {
		t.Fatalf("cached parent move discovery failed: warnings=%d, error=%v", warnings, err)
	}
	for _, id := range albums[:2] {
		if metadataMusicScanStateSnapshot(t, ctx, pool, id) == before[id] {
			t.Fatalf("cached parent move did not refresh album %s", id)
		}
	}
}

func TestMusicAlbumDiscoveryCancellationPreservesOwnerAndCommittedAlbums(t *testing.T) {
	ctx, pool, store, trace := musicAlbumDiscoveryFixture(t)
	parents := map[string]bool{"inner-album": true, "outer-album": true}
	before := []string{metadataMusicScanStateSnapshot(t, ctx, pool, "inner-album"), metadataMusicScanStateSnapshot(t, ctx, pool, "outer-album")}
	for _, test := range []struct {
		name               string
		atDiscovery        bool
		afterCommit        int
		wantInnerPublished bool
	}{
		{name: "discovery cancellation keeps owner", atDiscovery: true},
		{name: "later cancellation retains first publication", afterCommit: 2, wantInnerPublished: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			work, cancel := context.WithCancel(ctx)
			defer cancel()
			trace.reset(cancel, test.atDiscovery, test.afterCommit)
			warnings, err := store.refreshScannedMusicAlbums(work, "music-discovery", parents, map[string]bool{"music-discovery-complete": true})
			if !errors.Is(err, context.Canceled) || warnings != 0 {
				t.Fatalf("cancelled discovery result: warnings=%d, error=%v", warnings, err)
			}
			trace.assertBatches(t, len(parents))
			after := []string{metadataMusicScanStateSnapshot(t, ctx, pool, "inner-album"), metadataMusicScanStateSnapshot(t, ctx, pool, "outer-album")}
			if (after[0] != before[0]) != test.wantInnerPublished || after[1] != before[1] {
				t.Fatal("cancellation changed an unadmitted album or lost an independently committed album")
			}
			if err := store.CheckOwnership(ctx); err != nil {
				t.Fatalf("caller cancellation retired the catalog owner: %v", err)
			}
		})
	}
}
