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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type metadataMusicScanProber struct {
	libraryFixtureProber
	sourceMu sync.Mutex
	sources  map[string]*media.MusicMetadata
	failures map[string]bool
}

func (p *metadataMusicScanProber) MusicMetadataVersion() int {
	return media.CurrentMusicMetadataVersion
}

func (p *metadataMusicScanProber) set(name string, source *media.MusicMetadata, fail bool) {
	p.sourceMu.Lock()
	defer p.sourceMu.Unlock()
	if p.sources == nil {
		p.sources = make(map[string]*media.MusicMetadata)
		p.failures = make(map[string]bool)
	}
	if source == nil {
		p.sources[name] = nil
	} else {
		copied := *source
		p.sources[name] = &copied
	}
	p.failures[name] = fail
}

func (p *metadataMusicScanProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := p.libraryFixtureProber.ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	info.ProbeVersion = media.CurrentProbeVersion
	p.sourceMu.Lock()
	source, fail := p.sources[filepath.Base(file.Name())], p.failures[filepath.Base(file.Name())]
	if source != nil {
		copied := *source
		info.EmbeddedMusic = &copied
	}
	p.sourceMu.Unlock()
	if fail {
		return media.Info{}, fmt.Errorf("music fixture probe failed")
	}
	return info, nil
}

func TestStoreMusicMetadataRefreshesLegacyAudioWithoutReprobingVideo(t *testing.T) {
	prober := &metadataMusicScanProber{}
	firstFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "  First embedded title  ", Album: "  Shared album  ", Artist: "  Artist; One / Two  "}
	secondFacts := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Second embedded title", Album: firstFacts.Album, Artist: firstFacts.Artist}
	prober.set("01 First.flac", firstFacts, false)
	prober.set("02 Second.flac", secondFacts, false)
	prober.set("03 Empty.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion}, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "mixed/01 First.flac", "audio:music-first")
	secondPath := libraryIntegrationFile(t, allowedRoot, "mixed/02 Second.flac", "audio:music-second")
	emptyPath := libraryIntegrationFile(t, allowedRoot, "mixed/Empty/03 Empty.flac", "audio:music-empty")
	videoPath := libraryIntegrationFile(t, allowedRoot, "mixed/Film.mp4", "video:music-unaffected")
	library := libraryIntegrationCreate(t, ctx, store, "Versioned music", "mixed", filepath.Join(allowedRoot, "mixed"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	empty := nfoCatalogItem(t, ctx, store, userID, library.ID, emptyPath)
	video := nfoCatalogItem(t, ctx, store, userID, library.ID, videoPath)
	audioIDs := []string{first.ID, second.ID, empty.ID}
	if _, err := pool.Exec(ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,last_played_at,updated_at)
		VALUES($1,$2,9007199254740993,7,true,'2025-01-01T00:00:00Z','2025-01-02T00:00:00Z')`, userID, first.ID); err != nil {
		t.Fatalf("seed independent user data before music probe refresh: %v", err)
	}
	var userDataBefore string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2", userID, first.ID).Scan(&userDataBefore); err != nil {
		t.Fatalf("snapshot user playback state before music probe refresh: %v", err)
	}
	// Remove only the new music facts to model a valid legacy technical cache.
	if _, err := pool.Exec(ctx, "UPDATE items SET media = media - 'EmbeddedMusic' WHERE id = ANY($1::text[])", []string{first.ID, empty.ID}); err != nil {
		t.Fatalf("seed legacy audio probe snapshots: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=jsonb_set(media,'{EmbeddedMusic}',
		((media->'EmbeddedMusic') - 'AlbumArtist') || '{"Version":1}'::jsonb) WHERE id=$1`, second.ID); err != nil {
		t.Fatalf("seed the old version-one music cache without album_artist extraction: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE item_metadata_state SET music_source = '{}'::jsonb WHERE item_id = ANY($1::text[])", audioIDs); err != nil {
		t.Fatalf("seed unextracted accepted audio sources: %v", err)
	}
	firstFacts.AlbumArtist, secondFacts.AlbumArtist = "  Explicit album ensemble  ", "  Explicit album ensemble  "
	prober.set("01 First.flac", firstFacts, false)
	prober.set("02 Second.flac", secondFacts, false)
	videoBefore := metadataEditTestSnapshot(t, ctx, pool, video.ID)
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" {
		t.Fatalf("refresh legacy music facts: %+v", job)
	}
	first = nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	second = nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	empty = nfoCatalogItem(t, ctx, store, userID, library.ID, emptyPath)
	if first.ID != audioIDs[0] || second.ID != audioIDs[1] || empty.ID != audioIDs[2] {
		t.Fatal("music cache upgrade changed stable audio identifiers")
	}
	if first.Name != firstFacts.Title || second.Name != secondFacts.Title {
		t.Errorf("embedded titles were changed: first = %q, second = %q", first.Name, second.Name)
	}
	for _, item := range []Item{first, second, empty} {
		if item.Media == nil || item.Media.ProbeVersion != 6 || item.Media.EmbeddedMusic == nil || item.Media.EmbeddedMusic.Version != media.CurrentMusicMetadataVersion {
			t.Fatalf("audio %s retained an unversioned music cache", item.ID)
		}
	}
	metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
		Version: 1, Name: firstFacts.Title, Album: firstFacts.Album,
		Artists: []string{firstFacts.Artist}, AlbumArtists: []string{firstFacts.AlbumArtist},
	})
	metadataMusicScanAssertSource(t, ctx, pool, empty.ID, musicMetadataSource{
		Version: 1, Artists: []string{}, AlbumArtists: []string{},
	})
	album, err := store.GetItem(ctx, userID, first.ParentID)
	if err != nil || album.Type != "MusicAlbum" || !album.IsFolder || album.Media != nil {
		t.Fatalf("root music album projection = %+v, error = %v", album, err)
	}
	if album.ID != second.ParentID || album.Name != firstFacts.Album {
		t.Errorf("root album lost uniform member facts: %+v", album)
	}
	metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
		Version: 1, Name: firstFacts.Album, Album: firstFacts.Album,
		Artists: []string{firstFacts.Artist}, AlbumArtists: []string{firstFacts.AlbumArtist},
	})
	if len(first.Entities.Artists) != 1 || len(album.Entities.Artists) != 1 ||
		first.Entities.Artists[0].ID != album.Entities.Artists[0].ID ||
		len(first.Entities.AlbumArtists) != 1 || len(second.Entities.AlbumArtists) != 1 || len(album.Entities.AlbumArtists) != 1 ||
		album.Entities.AlbumArtists[0].ID != first.Entities.AlbumArtists[0].ID ||
		second.Entities.AlbumArtists[0].ID != first.Entities.AlbumArtists[0].ID ||
		album.Entities.AlbumArtists[0].ID == first.Entities.Artists[0].ID {
		t.Errorf("audio and album did not share the real artist entity: audio = %+v, album = %+v", first.Entities, album.Entities)
	}
	metadataMusicScanAssertProbeCount(t, prober, "audio:music-first", 2)
	metadataMusicScanAssertProbeCount(t, prober, "audio:music-second", 2)
	metadataMusicScanAssertProbeCount(t, prober, "audio:music-empty", 2)
	metadataMusicScanAssertProbeCount(t, prober, "video:music-unaffected", 1)
	foreign := nextUpLibrary(t, ctx, store, allowedRoot, "unscanned-foreign-music")
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, parent_id, name, sort_name, type, is_folder, media)
		VALUES ($1, $2, $3, 'Foreign unread audio', 'foreign unread audio', 'Audio', false, NULL)`,
		foreign.ID+"-cross-library-audio", foreign.ID, album.ID); err != nil {
		t.Fatalf("insert cross-library unread member fixture: %v", err)
	}
	firstBefore := metadataEditTestSnapshot(t, ctx, pool, first.ID)
	emptyBefore := metadataEditTestSnapshot(t, ctx, pool, empty.ID)
	callsBefore := len(prober.calls())
	cached := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if cached.Error != "" {
		t.Fatalf("foreign unread audio entered the local album aggregation: %+v", cached)
	}
	metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
		Version: 1, Name: firstFacts.Album, Album: firstFacts.Album,
		Artists: []string{firstFacts.Artist}, AlbumArtists: []string{firstFacts.AlbumArtist},
	})
	if len(prober.calls()) != callsBefore {
		t.Error("accepted empty or populated music facts were probed again")
	}
	if metadataEditTestSnapshot(t, ctx, pool, first.ID) != firstBefore ||
		metadataEditTestSnapshot(t, ctx, pool, empty.ID) != emptyBefore {
		t.Error("cached accepted music rewrote audio state")
	}
	if metadataEditTestSnapshot(t, ctx, pool, video.ID) != videoBefore {
		t.Error("independent music cache refresh changed a video item")
	}
	var userDataAfter string
	if err := pool.QueryRow(ctx, "SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2", userID, first.ID).Scan(&userDataAfter); err != nil || userDataAfter != userDataBefore {
		t.Fatalf("music probe refresh changed exact user playback state, favorite, or timestamps: %v", err)
	}
}

func TestStoreMusicMetadataPreservesControlsAndCachedRelations(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "First source", Album: "Original embedded album", Artist: "Original artist", AlbumArtist: "Original ensemble"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Second source", Album: "Original embedded album", Artist: "Original artist", AlbumArtist: "Original ensemble"}, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	actor := metadataEditTestActor(t, ctx, pool, "music-metadata-editor")
	firstPath := libraryIntegrationFile(t, allowedRoot, "music/Album/01 First.flac", "audio:controlled-first")
	libraryIntegrationFile(t, allowedRoot, "music/Album/02 Second.flac", "audio:controlled-second")
	libraryIntegrationFile(t, allowedRoot, "music/Album/album.nfo", `<album><title>Local album title</title><genre>Local genre</genre></album>`)
	library := libraryIntegrationCreate(t, ctx, store, "Controlled music", "music", filepath.Join(allowedRoot, "music"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
	if album.Name != "Local album title" {
		t.Fatalf("embedded album replaced the local NFO name: %q", album.Name)
	}
	if len(album.Entities.Genres) != 1 || album.Entities.Genres[0].Name != "Local genre" {
		t.Fatalf("initial album NFO did not create its genre association: %+v", album.Entities.Genres)
	}
	firstDetail := metadataEditTestDetail(t, ctx, store, actor, first.ID)
	albumDetail := metadataEditTestDetail(t, ctx, store, actor, album.ID)
	firstDetail = metadataEditTestUpdate(t, ctx, store, actor, firstDetail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual audio title"`)}, []string{"Name"})
	albumDetail = metadataEditTestUpdate(t, ctx, store, actor, albumDetail,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Manual album title"`)}, []string{"Name"})
	firstLocks, albumLocks := metadataEditTestCopy(firstDetail.LockedValues), metadataEditTestCopy(albumDetail.LockedValues)
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Changed audio title", Album: "Changed embedded album", Artist: "Changed artist", AlbumArtist: "Changed ensemble"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Changed second title", Album: "Changed embedded album", Artist: "Changed artist", AlbumArtist: "Changed ensemble"}, false)
	if job := forceProbeTestScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("refresh accepted music behind administrator controls: %+v", job)
	}
	firstDetail = metadataEditTestDetail(t, ctx, store, actor, first.ID)
	albumDetail = metadataEditTestDetail(t, ctx, store, actor, album.ID)
	if firstDetail.Automatic.Name != "Changed audio title" || albumDetail.Automatic.Name != "Local album title" ||
		firstDetail.Effective.Name != "Manual audio title" || albumDetail.Effective.Name != "Manual album title" {
		t.Errorf("music refresh changed name precedence: audio = %+v, album = %+v", firstDetail, albumDetail)
	}
	if !reflect.DeepEqual(firstDetail.LockedValues, firstLocks) || !reflect.DeepEqual(albumDetail.LockedValues, albumLocks) {
		t.Error("music refresh recaptured retained name locks")
	}
	for _, detail := range []ItemMetadataDetail{firstDetail, albumDetail} {
		sourceBefore := metadataMusicScanSource(t, ctx, pool, detail.ItemID)
		itemBefore, err := store.GetItem(ctx, userID, detail.ItemID)
		if err != nil {
			t.Fatalf("read music entities before native edit: %v", err)
		}
		values := metadataEditTestCopy(detail.Overrides)
		values["Overview"] = json.RawMessage(`"Native metadata edit"`)
		edited := metadataEditTestUpdate(t, ctx, store, actor, detail, values, detail.LockedFields)
		delete(values, "Name")
		locked := metadataEditTestUpdate(t, ctx, store, actor, edited, values, edited.LockedFields)
		if locked.Effective.Name != detail.Effective.Name || !reflect.DeepEqual(locked.LockedValues, detail.LockedValues) {
			t.Errorf("native edit or override removal lost the retained music name lock: %+v", locked)
		}
		if metadataMusicScanSource(t, ctx, pool, detail.ItemID) != sourceBefore {
			t.Error("native metadata editing changed the accepted music source")
		}
		itemAfter, err := store.GetItem(ctx, userID, detail.ItemID)
		if err != nil || !reflect.DeepEqual(itemAfter.Entities.Artists, itemBefore.Entities.Artists) ||
			!reflect.DeepEqual(itemAfter.Entities.AlbumArtists, itemBefore.Entities.AlbumArtists) {
			t.Errorf("native editing lost music artist references: before = %+v, after = %+v, error = %v", itemBefore.Entities, itemAfter.Entities, err)
		}
	}
	firstDetail = metadataEditTestDetail(t, ctx, store, actor, first.ID)
	albumDetail = metadataEditTestDetail(t, ctx, store, actor, album.ID)
	audioBefore := metadataEditTestSnapshot(t, ctx, pool, first.ID)
	albumBefore := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
	callsBefore := len(prober.calls())
	for visit := 0; visit < 2; visit++ {
		job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
		if job.Error != "" || job.Added != 0 || job.Updated != 0 {
			t.Fatalf("cached controlled music was treated as changed source: %+v", job)
		}
		if metadataEditTestSnapshot(t, ctx, pool, first.ID) != audioBefore {
			t.Error("cached audio changed persisted metadata or relationships")
		}
		if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumBefore {
			t.Error("cached album changed metadata state or relationship row versions")
		}
		if current := metadataEditTestDetail(t, ctx, store, actor, first.ID); current.Revision != firstDetail.Revision {
			t.Errorf("cached audio advanced metadata revision: %s, want %s", current.Revision, firstDetail.Revision)
		}
		if current := metadataEditTestDetail(t, ctx, store, actor, album.ID); current.Revision != albumDetail.Revision {
			t.Errorf("cached album advanced metadata revision: %s, want %s", current.Revision, albumDetail.Revision)
		}
	}
	if len(prober.calls()) != callsBefore {
		t.Error("native music controls invalidated the accepted probe cache")
	}
	metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
		Version: 1, Name: "Changed audio title", Album: "Changed embedded album",
		Artists: []string{"Changed artist"}, AlbumArtists: []string{"Changed ensemble"},
	})
	metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
		Version: 1, Name: "Changed embedded album", Album: "Changed embedded album",
		Artists: []string{"Changed artist"}, AlbumArtists: []string{"Changed ensemble"},
	})
}

func TestStoreMusicMetadataRequiresCompleteMembersBeforeAlbumPublication(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		second       *media.MusicMetadata
		probeFailure bool
		retainAlbum  bool
		wantArtists  []string
	}{
		{name: "MixedTags", second: &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Album: "Other album", Artist: "Other artist"}, wantArtists: []string{"Original artist", "Other artist"}},
		{name: "InspectedEmptyTags", second: &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion}, wantArtists: []string{"Original artist"}},
		{name: "UnextractedMember", retainAlbum: true},
		{name: "UnknownVersionMember", second: &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion + 1, Album: "Unknown album", Artist: "Unknown artist"}, retainAlbum: true},
		{name: "OversizedMember", second: &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Artist: strings.Repeat("x", 1025)}, retainAlbum: true},
		{name: "FailedMember", probeFailure: true, retainAlbum: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prober := &metadataMusicScanProber{}
			initialFirst := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "First title", Album: "Original album", Artist: "Original artist"}
			initialSecond := &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Second title", Album: "Original album", Artist: "Original artist"}
			prober.set("01 First.flac", initialFirst, false)
			prober.set("02 Second.flac", initialSecond, false)
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
			firstPath := libraryIntegrationFile(t, allowedRoot, "music/Album/01 First.flac", "audio:member-first")
			secondPath := libraryIntegrationFile(t, allowedRoot, "music/Album/02 Second.flac", "audio:member-second")
			library := libraryIntegrationCreate(t, ctx, store, "Complete music members", "music", filepath.Join(allowedRoot, "music"))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
			second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
			album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
			oldAlbum := musicMetadataSource{Version: 1, Name: "Original album", Album: "Original album",
				Artists: []string{"Original artist"}, AlbumArtists: []string{"Original artist"}}
			metadataMusicScanAssertSource(t, ctx, pool, album.ID, oldAlbum)
			albumBefore := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
			secondBefore := metadataEditTestSnapshot(t, ctx, pool, second.ID)
			if scenario.retainAlbum {
				prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Fresh first title", Album: "Fresh album", Artist: "Fresh artist"}, false)
			}
			prober.set("02 Second.flac", scenario.second, scenario.probeFailure)
			job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
			if scenario.retainAlbum {
				if job.Error == "" {
					t.Error("incomplete music members produced no scan warning")
				}
				metadataMusicScanAssertSource(t, ctx, pool, album.ID, oldAlbum)
				if metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumBefore {
					t.Error("incomplete root published a partial album source or changed its relationships")
				}
				metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
					Version: 1, Name: "Fresh first title", Album: "Fresh album",
					Artists: []string{"Fresh artist"}, AlbumArtists: []string{},
				})
				metadataMusicScanAssertSource(t, ctx, pool, second.ID, musicMetadataSource{
					Version: 1, Name: "Second title", Album: "Original album",
					Artists: []string{"Original artist"}, AlbumArtists: []string{},
				})
				if scenario.probeFailure && metadataEditTestSnapshot(t, ctx, pool, second.ID) != secondBefore {
					t.Error("failed member probing replaced the previous accepted audio snapshot")
				}
				return
			}
			if job.Error != "" {
				t.Fatalf("accepted mixed or empty tags were treated as unread: %+v", job)
			}
			metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
				Version: 1, Artists: scenario.wantArtists, AlbumArtists: []string{},
			})
			current, err := store.GetItem(ctx, userID, album.ID)
			if err != nil || current.Name != "Album" || current.Media != nil {
				t.Errorf("nonuniform members fabricated an album name or media source: %+v, error = %v", current, err)
			}
			if len(current.Entities.AlbumArtists) != 0 {
				t.Errorf("nonuniform members fabricated album artists: %+v", current.Entities.AlbumArtists)
			}
		})
	}
}

func metadataMusicScanSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var source string
	if err := pool.QueryRow(ctx, "SELECT music_source::text FROM item_metadata_state WHERE item_id = $1", itemID).Scan(&source); err != nil {
		t.Fatalf("read accepted music source: %v", err)
	}
	return source
}

func metadataMusicScanAssertSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, expected musicMetadataSource) {
	t.Helper()
	raw := metadataMusicScanSource(t, ctx, pool, itemID)
	var actual musicMetadataSource
	if err := json.Unmarshal([]byte(raw), &actual); err != nil {
		t.Fatalf("decode accepted music source: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("accepted music source for %s = %+v, want %+v", itemID, actual, expected)
	}
}

func metadataMusicScanAssertProbeCount(t *testing.T, prober *metadataMusicScanProber, content string, expected int) {
	t.Helper()
	actual := 0
	for _, call := range prober.calls() {
		if call == content {
			actual++
		}
	}
	if actual != expected {
		t.Errorf("probe count for %q = %d, want %d", content, actual, expected)
	}
}

func metadataMusicScanStateSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'state', (SELECT to_jsonb(ms) || jsonb_build_object('RowVersion', ms.xmin::text)
			FROM item_metadata_state ms WHERE item_id = $1),
		'entities', COALESCE((SELECT jsonb_agg(to_jsonb(edge) ORDER BY edge.entity_id, edge.credit_group, edge.position)
			FROM (SELECT e.*, e.xmin::text AS row_version FROM item_entities e WHERE item_id = $1) edge), '[]'::jsonb))::text`,
		itemID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot accepted music state and relationship versions: %v", err)
	}
	return snapshot
}

func TestStoreMusicMetadataKeepsPublishedAlbumWhenNextPublicationIsCancelled(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "First track", Album: "Original first album", Artist: "Original first artist"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Second track", Album: "Original second album", Artist: "Original second artist"}, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "music/First/01 First.flac", "audio:cancel-first")
	secondPath := libraryIntegrationFile(t, allowedRoot, "music/Second/02 Second.flac", "audio:cancel-second")
	library := libraryIntegrationCreate(t, ctx, store, "Independent music publication", "music", filepath.Join(allowedRoot, "music"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	albums := []struct {
		item, track Item
		facts       media.MusicMetadata
	}{
		{
			item:  nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath)),
			track: nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath),
			facts: media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Accepted first track", Album: "Accepted first album", Artist: "Accepted first artist"},
		},
		{
			item:  nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(secondPath)),
			track: nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath),
			facts: media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Accepted second track", Album: "Accepted second album", Artist: "Accepted second artist"},
		},
	}
	if albums[1].item.ID < albums[0].item.ID {
		albums[0], albums[1] = albums[1], albums[0]
	}
	secondSourceBefore := metadataMusicScanSource(t, ctx, pool, albums[1].item.ID)
	secondStateBefore := metadataMusicScanStateSnapshot(t, ctx, pool, albums[1].item.ID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin accepted member update fixture: %v", err)
	}
	defer rollback(tx)
	for _, album := range albums {
		if _, err := tx.Exec(ctx, "UPDATE items SET media = jsonb_set(media, '{EmbeddedMusic}', $2::jsonb) WHERE id = $1",
			album.track.ID, metadataEditTestRaw(t, album.facts)); err != nil {
			t.Fatalf("update accepted member media facts: %v", err)
		}
		source := musicMetadataSource{Version: 1, Name: album.facts.Title, Album: album.facts.Album,
			Artists: []string{album.facts.Artist}, AlbumArtists: []string{}}
		if _, err := tx.Exec(ctx, "UPDATE item_metadata_state SET music_source = $2::jsonb WHERE item_id = $1",
			album.track.ID, metadataEditTestRaw(t, source)); err != nil {
			t.Fatalf("update accepted member source fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit accepted member source fixture: %v", err)
	}
	ready, err := store.refreshOwnedMusicAlbum(ctx, library.ID, albums[0].item.ID)
	if err != nil || !ready {
		t.Fatalf("publish first accepted album: ready = %v, error = %v", ready, err)
	}
	metadataMusicScanAssertSource(t, ctx, pool, albums[0].item.ID, musicMetadataSource{
		Version: 1, Name: albums[0].facts.Album, Album: albums[0].facts.Album,
		Artists: []string{albums[0].facts.Artist}, AlbumArtists: []string{albums[0].facts.Artist},
	})
	firstStateAfter := metadataMusicScanStateSnapshot(t, ctx, pool, albums[0].item.ID)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	ready, err = store.refreshOwnedMusicAlbum(cancelled, library.ID, albums[1].item.ID)
	if !errors.Is(err, context.Canceled) || ready {
		t.Fatalf("cancelled album publication was accepted: ready = %v, error = %v", ready, err)
	}
	if metadataMusicScanStateSnapshot(t, ctx, pool, albums[0].item.ID) != firstStateAfter {
		t.Error("cancelling the next publication changed an already committed album")
	}
	if metadataMusicScanSource(t, ctx, pool, albums[1].item.ID) != secondSourceBefore ||
		metadataMusicScanStateSnapshot(t, ctx, pool, albums[1].item.ID) != secondStateBefore {
		t.Error("cancelled publication replaced the second album's previous accepted source")
	}
	first, err := store.GetItem(ctx, userID, albums[0].item.ID)
	if err != nil || first.Name != albums[0].facts.Album {
		t.Errorf("published album lost its effective name: %+v, error = %v", first, err)
	}
	second, err := store.GetItem(ctx, userID, albums[1].item.ID)
	if err != nil || second.Name != albums[1].item.Name {
		t.Errorf("cancelled album changed its effective name: %+v, error = %v", second, err)
	}
}

func TestStoreMusicMetadataMoveRefreshesBothAlbumsWithoutReprobing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stable filesystem identity is supported on Linux")
	}
	prober := &metadataMusicScanProber{}
	prober.set("01 Moving.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Moving title", Album: "Alpha", Artist: "Artist A"}, false)
	prober.set("03 Remain.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Remaining title", Album: "Gamma", Artist: "Artist C"}, false)
	prober.set("02 Target.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Target title", Album: "Beta", Artist: "Artist B"}, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	movingPath := libraryIntegrationFile(t, allowedRoot, "music/Old/01 Moving.flac", "audio:moving-track")
	libraryIntegrationFile(t, allowedRoot, "music/Old/03 Remain.flac", "audio:remaining-track")
	targetPath := libraryIntegrationFile(t, allowedRoot, "music/New/02 Target.flac", "audio:target-track")
	library := libraryIntegrationCreate(t, ctx, store, "Moving music", "music", filepath.Join(allowedRoot, "music"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	moving := nfoCatalogItem(t, ctx, store, userID, library.ID, movingPath)
	oldAlbum := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(movingPath))
	newAlbum := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(targetPath))
	if moving.ParentID != oldAlbum.ID || len(moving.Entities.Artists) != 1 {
		t.Fatalf("moving audio has no original album or real artist: %+v", moving)
	}
	movingArtistID := moving.Entities.Artists[0].ID
	movingSourceBefore := metadataMusicScanSource(t, ctx, pool, moving.ID)
	metadataMusicScanAssertSource(t, ctx, pool, oldAlbum.ID, musicMetadataSource{
		Version: 1, Artists: []string{"Artist A", "Artist C"}, AlbumArtists: []string{},
	})
	metadataMusicScanAssertSource(t, ctx, pool, newAlbum.ID, musicMetadataSource{
		Version: 1, Name: "Beta", Album: "Beta",
		Artists: []string{"Artist B"}, AlbumArtists: []string{"Artist B"},
	})
	callsBefore := len(prober.calls())
	destination := filepath.Join(filepath.Dir(targetPath), filepath.Base(movingPath))
	if err := os.Rename(movingPath, destination); err != nil {
		t.Fatalf("move audio between physical albums: %v", err)
	}
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" || job.Added != 0 || job.Updated != 1 {
		t.Fatalf("same-library audio move was not an accepted identity-preserving update: %+v", job)
	}
	moved := nfoCatalogItem(t, ctx, store, userID, library.ID, destination)
	if moved.ID != moving.ID || moved.LibraryID != library.ID || moved.ParentID != newAlbum.ID || moved.Path != destination {
		t.Errorf("audio move changed identity or lost its new parent: before = %+v, after = %+v", moving, moved)
	}
	audio := libraryIntegrationQuery(t, ctx, store, Query{
		UserID: userID, ParentID: library.ID, Recursive: true, IncludeItemTypes: []string{"Audio"},
	})
	if audio.TotalRecordCount != 3 || len(audio.Items) != 3 {
		t.Errorf("audio move left a duplicate catalog row: %+v", audio)
	}
	if metadataMusicScanSource(t, ctx, pool, moved.ID) != movingSourceBefore {
		t.Error("moving an unchanged file changed its accepted embedded facts")
	}
	if len(moved.Entities.Artists) != 1 || moved.Entities.Artists[0].ID != movingArtistID {
		t.Errorf("audio move replaced its real artist identity: %+v", moved.Entities.Artists)
	}
	metadataMusicScanAssertSource(t, ctx, pool, oldAlbum.ID, musicMetadataSource{
		Version: 1, Name: "Gamma", Album: "Gamma",
		Artists: []string{"Artist C"}, AlbumArtists: []string{"Artist C"},
	})
	metadataMusicScanAssertSource(t, ctx, pool, newAlbum.ID, musicMetadataSource{
		Version: 1, Artists: []string{"Artist A", "Artist B"}, AlbumArtists: []string{},
	})
	oldCurrent, err := store.GetItem(ctx, userID, oldAlbum.ID)
	if err != nil || oldCurrent.Type != "MusicAlbum" || oldCurrent.Name != "Gamma" || oldCurrent.Media != nil {
		t.Errorf("source album did not publish its remaining uniform member: %+v, error = %v", oldCurrent, err)
	}
	newCurrent, err := store.GetItem(ctx, userID, newAlbum.ID)
	if err != nil || newCurrent.Type != "MusicAlbum" || newCurrent.Name != "New" || newCurrent.Media != nil ||
		len(newCurrent.Entities.AlbumArtists) != 0 {
		t.Errorf("destination album retained fabricated uniform facts: %+v, error = %v", newCurrent, err)
	}
	foundMovingArtist := false
	for _, artist := range newCurrent.Entities.Artists {
		foundMovingArtist = foundMovingArtist || artist.ID == movingArtistID
	}
	if !foundMovingArtist {
		t.Errorf("destination album lost the moved track's artist identity: %+v", newCurrent.Entities.Artists)
	}
	if len(prober.calls()) != callsBefore {
		t.Error("same-library audio move repeated media probing")
	}
}
