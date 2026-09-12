//go:build linux

package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

func TestScanReconciliationMusicIncompleteSurvivorRetainsWholeBatch(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "First original", Album: "Original album", Artist: "Original A", AlbumArtist: "Original Ensemble"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Second original", Album: "Original album", Artist: "Original B", AlbumArtist: "Original Ensemble"}, false)
	ctx, pool, store, root, userID := scanReconciliationScanStore(t, prober)
	lostPath := libraryIntegrationFile(t, root, "a-root/Gone/Lost.mp4", "video:music-gate-lost")
	otherPath := libraryIntegrationFile(t, root, "a-root/Other.mp4", "video:music-gate-other")
	firstPath := libraryIntegrationFile(t, root, "z-root/Album/01 First.flac", "audio:music-gate-first")
	secondPath := libraryIntegrationFile(t, root, "z-root/Album/02 Second.flac", "audio:music-gate-second")
	library, err := store.CreateLibrary(ctx, "Music completeness across roots", "mixed",
		[]string{filepath.Join(root, "a-root"), filepath.Join(root, "z-root")})
	if err != nil {
		t.Fatal(err)
	}
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("initial complete music fixture reported a warning: %+v", job)
	}
	lost := nfoCatalogItem(t, ctx, store, userID, library.ID, lostPath)
	other := nfoCatalogItem(t, ctx, store, userID, library.ID, otherPath)
	folder := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(lostPath))
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
	if album.Type != "MusicAlbum" || first.ParentID != album.ID || second.ParentID != album.ID || lost.ParentID == album.ID {
		t.Fatal("music gate fixture did not create an unrelated missing batch and a shared album")
	}
	albumSource := metadataMusicScanSource(t, ctx, pool, album.ID)
	albumState := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	secondSource := metadataMusicScanSource(t, ctx, pool, second.ID)
	userData := make(map[string]string)
	for _, item := range []Item{first, lost, other} {
		userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 4, PlaybackPositionTicks: 80})
		userData[item.ID] = forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	}
	for _, path := range []string{lostPath, filepath.Dir(lostPath), otherPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Fresh first", Album: "Fresh album", Artist: "Fresh Artist", AlbumArtist: "Fresh Ensemble"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: 1,
		Title: "Unaccepted second", Album: "Unaccepted album", Artist: "Unaccepted Artist", AlbumArtist: "Unaccepted Ensemble"}, false)
	notifications := catalogChangesTestListener(t, store)
	job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
	if job.Error == "" || !strings.Contains(job.Error, "missing catalog records were retained") {
		t.Fatalf("incomplete surviving music did not retain the final deletion batch: %+v", job)
	}
	currentSecond := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	if currentSecond.ID != second.ID || currentSecond.Media == nil || currentSecond.Media.EmbeddedMusic == nil ||
		currentSecond.Media.EmbeddedMusic.Version != 1 || currentSecond.Media.EmbeddedMusic.Title != "Unaccepted second" {
		t.Fatalf("old-version probe was not accepted into this scan's technical cache: %+v", currentSecond)
	}
	if metadataMusicScanSource(t, ctx, pool, second.ID) != secondSource {
		t.Error("the old-version member replaced its previously accepted music source")
	}
	if metadataMusicScanSource(t, ctx, pool, album.ID) != albumSource ||
		metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumState {
		t.Error("an incomplete surviving member changed the published album source or relationships")
	}
	metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
		Version: 1, Name: "Fresh first", Album: "Fresh album", Artists: []string{"Fresh Artist"}, AlbumArtists: []string{"Fresh Ensemble"},
	})
	scanReconciliationAssertItems(t, ctx, pool, true, lost.ID, other.ID, folder.ID, first.ID, second.ID, album.ID)
	for itemID, before := range userData {
		if forceProbeUserDataSnapshot(t, ctx, pool, itemID) != before {
			t.Errorf("music completeness rejection changed user data for %s", itemID)
		}
	}
	if got := scanReconciliationRemoved(t, notifications, library.ID); len(got) != 0 {
		t.Fatalf("a later music warning allowed unrelated committed removals: %v", got)
	}
	metadataMusicScanAssertProbeCount(t, prober, "audio:music-gate-first", 2)
	metadataMusicScanAssertProbeCount(t, prober, "audio:music-gate-second", 2)
}

func TestScanReconciliationMusicMissingInvalidMemberAllowsAlbumRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		invalidTyped bool
	}{
		{name: "OldVersionOneMember"},
		{name: "InvalidTypedMetadata", invalidTyped: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prober := &metadataMusicScanProber{}
			prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "First original", Album: "Original album", Artist: "Original A", AlbumArtist: "Original Ensemble"}, false)
			prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "Second original", Album: "Original album", Artist: "Original B", AlbumArtist: "Original Ensemble"}, false)
			ctx, pool, store, root, userID := scanReconciliationScanStore(t, prober)
			firstPath := libraryIntegrationFile(t, root, "music/Album/01 First.flac", "audio:music-recovery-first")
			secondPath := libraryIntegrationFile(t, root, "music/Album/02 Second.flac", "audio:music-recovery-second")
			library := libraryIntegrationCreate(t, ctx, store, "Missing incomplete music member", "music", filepath.Join(root, "music"))
			scanReconciliationAssertBound(t, ctx, pool, library.ID)
			if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
				t.Fatalf("initial complete album reported a warning: %+v", job)
			}
			first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
			second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
			album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
			if album.Type != "MusicAlbum" || first.ParentID != album.ID || second.ParentID != album.ID {
				t.Fatal("missing-member recovery fixture did not create a shared music album")
			}
			albumState := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
			userDataSeed(t, ctx, pool, userID, UserData{ItemID: first.ID, IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 90})
			userData := forceProbeUserDataSnapshot(t, ctx, pool, first.ID)
			prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "Fresh first", Album: "Fresh album", Artist: "Fresh Artist", AlbumArtist: "Fresh Ensemble"}, false)
			prober.set("02 Second.flac", &media.MusicMetadata{Version: 1,
				Title: "Unaccepted second", Album: "Unaccepted album", Artist: "Unaccepted Artist", AlbumArtist: "Unaccepted Ensemble"}, false)
			if job := forceProbeTestScan(t, ctx, store, library.ID, "Completed"); job.Error == "" {
				t.Fatal("the surviving old-version member did not block album publication")
			}
			currentSecond := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
			if currentSecond.Media == nil || currentSecond.Media.EmbeddedMusic == nil || currentSecond.Media.EmbeddedMusic.Version != 1 {
				t.Fatal("the recovery fixture did not persist its old-version member")
			}
			if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumState {
				t.Fatal("an incomplete member replaced the original album before deletion")
			}
			if scenario.invalidTyped {
				// Model malformed persisted facts on a member the next walk cannot inspect.
				if tag, err := pool.Exec(ctx, `UPDATE items SET media=jsonb_set(
					jsonb_set(media,'{EmbeddedMusic,Version}',to_jsonb($2::integer)),
					'{EmbeddedMusic,AlbumArtist}','7'::jsonb) WHERE id=$1`, second.ID, media.CurrentMusicMetadataVersion); err != nil || tag.RowsAffected() != 1 {
					t.Fatalf("seed a persisted member with an invalid typed music field: %v", err)
				}
			}
			if err := os.Remove(secondPath); err != nil {
				t.Fatal(err)
			}
			notifications := catalogChangesTestListener(t, store)
			job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			if job.Error != "" || job.Added != 0 {
				t.Fatalf("a proved-missing invalid member permanently blocked album recovery: %+v", job)
			}
			scanReconciliationAssertItems(t, ctx, pool, false, second.ID)
			scanReconciliationAssertItems(t, ctx, pool, true, first.ID, album.ID)
			metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
				Version: 1, Name: "Fresh album", Album: "Fresh album", Artists: []string{"Fresh Artist"}, AlbumArtists: []string{"Fresh Ensemble"},
			})
			metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
				Version: 1, Name: "Fresh first", Album: "Fresh album", Artists: []string{"Fresh Artist"}, AlbumArtists: []string{"Fresh Ensemble"},
			})
			if forceProbeUserDataSnapshot(t, ctx, pool, first.ID) != userData {
				t.Error("album recovery changed the surviving track's user data")
			}
			wantRemoved := []string{second.ID}
			if got := scanReconciliationRemoved(t, notifications, library.ID); !reflect.DeepEqual(got, wantRemoved) {
				t.Fatalf("successful post-deletion music proof published Removed=%v, want=%v", got, wantRemoved)
			}
			metadataMusicScanAssertProbeCount(t, prober, "audio:music-recovery-first", 2)
			metadataMusicScanAssertProbeCount(t, prober, "audio:music-recovery-second", 2)
		})
	}
}

func TestScanReconciliationMusicAggregationPagesNestedMembership(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("01 Seed.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Seed track", Album: "Paged album", Artist: "Seed artist", AlbumArtist: "Paged Ensemble"}, false)
	ctx, pool, store, root, userID := scanReconciliationScanStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/01 Seed.flac", "audio:paged-album-seed")
	library := libraryIntegrationCreate(t, ctx, store, "Paged music membership", "music", filepath.Join(root, "music"))
	scanReconciliationAssertBound(t, ctx, pool, library.ID)
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("initial paged album fixture reported a warning: %+v", job)
	}
	album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(path))
	if album.Type != "MusicAlbum" || !album.IsFolder {
		t.Fatal("paged membership fixture did not create a music album")
	}
	albumState := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	discID, nestedID, invalidID := album.ID+"-disc", album.ID+"-nested", album.ID+"-invalid"
	insert := func(want int64, query string, args ...any) {
		t.Helper()
		tag, err := pool.Exec(ctx, query, args...)
		if err != nil || tag.RowsAffected() != want {
			t.Fatalf("insert bounded music membership fixture: rows=%d want=%d error=%v", tag.RowsAffected(), want, err)
		}
	}
	// A catalog-only Disc remains an ordinary folder; rescanning a directory with
	// physical audio would turn it into a separate MusicAlbum boundary.
	insert(1, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT $1,library_id,root_id,id,'Disc 1','disc 1','Folder',true,
			path||'/Disc 1',relative_path||'/Disc 1' FROM items WHERE id=$2`, discID, album.ID)
	insert(1, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT $1,library_id,root_id,id,'Nested album','nested album','MusicAlbum',true,
			path||'/Nested',relative_path||'/Nested' FROM items WHERE id=$2`, nestedID, discID)
	// Reverse identifiers relative to track numbering so keyset pages and the
	// final album ordering exercise different orders with unique artist facts.
	insert(70, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,index_number,parent_index_number,media)
		SELECT album.id||'-direct-'||lpad((71-entry.n)::text,3,'0'),album.library_id,album.root_id,album.id,
			'Direct track '||entry.n,'direct track '||entry.n,'Audio',
			album.path||'/Direct '||entry.n||'.flac',album.relative_path||'/Direct '||entry.n||'.flac',entry.n,2,
			jsonb_build_object('EmbeddedMusic',jsonb_build_object('Version',$2::integer,
				'Album','Paged album','Artist','Direct artist '||lpad(entry.n::text,3,'0'),'AlbumArtist','Paged Ensemble'))
		FROM items album CROSS JOIN generate_series(1,70) entry(n) WHERE album.id=$1`, album.ID, media.CurrentMusicMetadataVersion)
	insert(3, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,index_number,parent_index_number,media)
		SELECT disc.id||'-track-'||lpad((4-entry.n)::text,3,'0'),disc.library_id,disc.root_id,disc.id,
			'Disc track '||entry.n,'disc track '||entry.n,'Audio',
			disc.path||'/Track '||entry.n||'.flac',disc.relative_path||'/Track '||entry.n||'.flac',entry.n,1,
			jsonb_build_object('EmbeddedMusic',jsonb_build_object('Version',$2::integer,
				'Album','Paged album','Artist','Disc artist '||lpad(entry.n::text,3,'0'),'AlbumArtist','Paged Ensemble'))
		FROM items disc CROSS JOIN generate_series(1,3) entry(n) WHERE disc.id=$1`, discID, media.CurrentMusicMetadataVersion)
	insert(1, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,media)
		SELECT $1,library_id,root_id,id,'Invalid outer member','invalid outer member','Audio',
			path||'/Invalid.flac',relative_path||'/Invalid.flac',
			jsonb_build_object('EmbeddedMusic',jsonb_build_object('Version',$3::integer,'AlbumArtist',7))
		FROM items WHERE id=$2`, invalidID, album.ID, media.CurrentMusicMetadataVersion)
	insert(1, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,media)
		SELECT id||'-unread',library_id,root_id,id,'Unread nested member','unread nested member','Audio',
			path||'/Unread.flac',relative_path||'/Unread.flac',NULL FROM items WHERE id=$1`, nestedID)
	want := musicMetadataSource{Version: 1, Name: "Paged album", Album: "Paged album",
		Artists: []string{"Seed artist"}, AlbumArtists: []string{"Paged Ensemble"}}
	for index := 1; index <= 3; index++ {
		want.Artists = append(want.Artists, fmt.Sprintf("Disc artist %03d", index))
	}
	for index := 1; index <= 70; index++ {
		want.Artists = append(want.Artists, fmt.Sprintf("Direct artist %03d", index))
	}
	notifications := catalogChangesTestListener(t, store)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if raw, ready, err := readAcceptedMusicAlbumSource(ctx, tx, library.ID, album.ID, nil, nil); err != nil || ready || len(raw) != 0 {
		t.Fatalf("unexcluded invalid music member was accepted: ready=%v raw=%s error=%v", ready, raw, err)
	}
	raw, ready, err := readAcceptedMusicAlbumSource(ctx, tx, library.ID, album.ID, []string{invalidID}, nil)
	if err != nil || !ready {
		t.Fatalf("excluding the outer invalid member did not recover paged album membership: ready=%v error=%v", ready, err)
	}
	var got musicMetadataSource
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode read-only paged music aggregation: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paged music aggregation lost ordered members or crossed a nested album boundary: got=%+v want=%+v", got, want)
	}
	if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumState {
		t.Error("read-only post-deletion aggregation published the album source or relationships")
	}
	assertNoCatalogTestNotification(t, notifications)
}
