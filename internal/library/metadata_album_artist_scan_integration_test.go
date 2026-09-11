package library

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestStoreAlbumArtistTagsPublishMixedArtistsWithSharedPersistentCredits(t *testing.T) {
	prober := &metadataMusicScanProber{}
	prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "First track", Album: "Shared album", Artist: "Artist A", AlbumArtist: "Artist A"}, false)
	prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
		Title: "Second track", Album: "Shared album", Artist: "Artist B", AlbumArtist: "Artist A"}, false)
	ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
	firstPath := libraryIntegrationFile(t, allowedRoot, "music/Album/01 First.flac", "audio:album-artist-first")
	secondPath := libraryIntegrationFile(t, allowedRoot, "music/Album/02 Second.flac", "audio:album-artist-second")
	library := libraryIntegrationCreate(t, ctx, store, "Explicit mixed album artists", "music", filepath.Join(allowedRoot, "music"))
	if job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
		t.Fatalf("scan mixed track artists with a uniform explicit album artist: %+v", job)
	}
	first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
	second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
	album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
	wantArtists := []string{"Artist A", "Artist B"}
	if first.IndexNumber != 1 || second.IndexNumber != 2 {
		t.Fatal("ordered album fixture lost its physical track numbering")
	}
	metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
		Version: 1, Name: "Shared album", Album: "Shared album", Artists: wantArtists, AlbumArtists: []string{"Artist A"},
	})
	for _, track := range []struct {
		item   Item
		title  string
		artist string
	}{{first, "First track", "Artist A"}, {second, "Second track", "Artist B"}} {
		metadataMusicScanAssertSource(t, ctx, pool, track.item.ID, musicMetadataSource{
			Version: 1, Name: track.title, Album: "Shared album", Artists: []string{track.artist}, AlbumArtists: []string{"Artist A"},
		})
		if track.item.ParentID != album.ID || track.item.Album == nil || track.item.Album.ID != album.ID ||
			track.item.Media == nil || track.item.Media.ProbeVersion != 6 || track.item.Media.EmbeddedMusic == nil ||
			track.item.Media.EmbeddedMusic.Version != media.CurrentMusicMetadataVersion || track.item.Media.EmbeddedMusic.AlbumArtist != "Artist A" {
			t.Fatalf("explicit album artist changed the physical album or technical cache: %+v", track.item)
		}
		if len(track.item.Entities.Artists) != 1 || len(track.item.Entities.AlbumArtists) != 1 ||
			track.item.Entities.AlbumArtists[0].Name != "Artist A" {
			t.Fatalf("track did not retain distinct persisted artist credit roles: %+v", track.item.Entities)
		}
	}
	if len(album.Entities.Artists) != 2 || len(album.Entities.AlbumArtists) != 1 ||
		first.Entities.Artists[0].ID <= 0 || first.Entities.Artists[0].ID != first.Entities.AlbumArtists[0].ID ||
		first.Entities.AlbumArtists[0].ID != second.Entities.AlbumArtists[0].ID ||
		first.Entities.AlbumArtists[0].ID != album.Entities.AlbumArtists[0].ID {
		t.Fatalf("mixed album and tracks did not share the same real album artist identity: first=%+v second=%+v album=%+v",
			first.Entities, second.Entities, album.Entities)
	}
	for index, name := range wantArtists {
		if album.Entities.Artists[index].Name != name {
			t.Fatalf("album artist union lost deterministic member ordering: got=%+v want=%v", album.Entities.Artists, wantArtists)
		}
	}
	var samePositionRoles bool
	if err := pool.QueryRow(ctx, `SELECT count(*)=2 AND count(DISTINCT credit_group)=2 AND
		bool_and(position=1 AND ((credit_group=1 AND credit_type='Artist') OR (credit_group=2 AND credit_type='AlbumArtist')))
		FROM item_entities WHERE item_id=$1 AND entity_id=$2`, first.ID, first.Entities.Artists[0].ID).Scan(&samePositionRoles); err != nil || !samePositionRoles {
		t.Fatalf("same artist lost its two independent position-one roles: retained=%v error=%v", samePositionRoles, err)
	}
	before := make(map[string]string)
	for _, item := range []Item{first, second, album} {
		before[item.ID] = metadataMusicScanStateSnapshot(t, ctx, pool, item.ID)
	}
	calls := len(prober.calls())
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if job.Error != "" || job.Added != 0 || job.Updated != 0 || len(prober.calls()) != calls {
		t.Fatalf("cached explicit album artist caused a redundant probe or update: %+v", job)
	}
	for _, item := range []Item{first, second, album} {
		if metadataMusicScanStateSnapshot(t, ctx, pool, item.ID) != before[item.ID] {
			t.Errorf("cached explicit album artist rewrote source, metadata, or association row versions for %s", item.ID)
		}
	}
}

func TestStoreAlbumArtistPublicationDoesNotHidePartialOrConflictingTags(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		first, second    string
		secondArtist     string
		wantAlbumArtists []string
	}{
		{name: "PartialExplicit", first: "Explicit Artist", wantAlbumArtists: []string{}},
		{name: "PartialBlank", first: "Explicit Artist", second: "   ", wantAlbumArtists: []string{}},
		{name: "ConflictingExplicit", first: "Album Artist A", second: "Album Artist B", wantAlbumArtists: []string{}},
		{name: "DifferentCaseIsNotExactConsensus", first: "Album Artist", second: "album artist", wantAlbumArtists: []string{}},
		{name: "DifferentWhitespaceIsNotExactConsensus", first: " Album Artist ", second: "Album Artist", wantAlbumArtists: []string{}},
		{name: "AllMissingUsesUniformTrackArtist", wantAlbumArtists: []string{"Uniform Track Artist"}},
		{name: "AllBlankUsesUniformTrackArtist", first: " ", second: "   ", wantAlbumArtists: []string{"Uniform Track Artist"}},
		{name: "AllMissingMixedTrackArtists", secondArtist: "Other Track Artist", wantAlbumArtists: []string{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prober := &metadataMusicScanProber{}
			for _, name := range []string{"01 First.flac", "02 Second.flac"} {
				prober.set(name, &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
					Album: "Policy album", Artist: "Uniform Track Artist", AlbumArtist: "Previously Published Album Artist"}, false)
			}
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
			firstPath := libraryIntegrationFile(t, allowedRoot, "music/Album/01 First.flac", "audio:album-policy-first")
			secondPath := libraryIntegrationFile(t, allowedRoot, "music/Album/02 Second.flac", "audio:album-policy-second")
			library := libraryIntegrationCreate(t, ctx, store, "Album artist consensus", "music", filepath.Join(allowedRoot, "music"))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
			metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
				Version: 1, Name: "Policy album", Album: "Policy album", Artists: []string{"Uniform Track Artist"},
				AlbumArtists: []string{"Previously Published Album Artist"},
			})
			secondArtist := scenario.secondArtist
			if secondArtist == "" {
				secondArtist = "Uniform Track Artist"
			}
			prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Album: "Policy album", Artist: "Uniform Track Artist", AlbumArtist: scenario.first}, false)
			prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Album: "Policy album", Artist: secondArtist, AlbumArtist: scenario.second}, false)
			if job := forceProbeTestScan(t, ctx, store, library.ID, "Completed"); job.Error != "" {
				t.Fatalf("accepted absent or conflicting album tags were treated as unread: %+v", job)
			}
			first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
			second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
			wantArtists := []string{"Uniform Track Artist"}
			if secondArtist != wantArtists[0] {
				wantArtists = append(wantArtists, secondArtist)
			}
			metadataMusicScanAssertSource(t, ctx, pool, album.ID, musicMetadataSource{
				Version: 1, Name: "Policy album", Album: "Policy album", Artists: wantArtists, AlbumArtists: scenario.wantAlbumArtists,
			})
			current, err := store.GetItem(ctx, userID, album.ID)
			if err != nil || len(current.Entities.AlbumArtists) != len(scenario.wantAlbumArtists) {
				t.Fatalf("album retained stale or invented consensus artist associations: %+v error=%v", current.Entities, err)
			}
			for index, name := range scenario.wantAlbumArtists {
				if current.Entities.AlbumArtists[index].Name != name || current.Entities.AlbumArtists[index].ID <= 0 {
					t.Errorf("album fallback did not use a real persisted track artist: %+v", current.Entities.AlbumArtists)
				}
			}
			for _, track := range []struct {
				item        Item
				albumArtist string
				artist      string
			}{{first, scenario.first, "Uniform Track Artist"}, {second, scenario.second, secondArtist}} {
				own := []string{}
				if strings.TrimSpace(track.albumArtist) != "" {
					own = append(own, track.albumArtist)
				}
				metadataMusicScanAssertSource(t, ctx, pool, track.item.ID, musicMetadataSource{
					Version: 1, Album: "Policy album", Artists: []string{track.artist}, AlbumArtists: own,
				})
			}
		})
	}
}

func TestStoreAlbumArtistIncompleteMembersRetainPublishedAlbumAndFreshTrackSource(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		version     int
		fail        bool
		albumArtist string
	}{
		{name: "FailedMember", version: media.CurrentMusicMetadataVersion, fail: true},
		{name: "OldVersionOneMember", version: 1},
		{name: "InvalidTypedUTF8", version: media.CurrentMusicMetadataVersion, albumArtist: string([]byte{0xff})},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prober := &metadataMusicScanProber{}
			prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "First original", Album: "Original album", Artist: "Original A", AlbumArtist: "Original Ensemble"}, false)
			prober.set("02 Second.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "Second original", Album: "Original album", Artist: "Original B", AlbumArtist: "Original Ensemble"}, false)
			ctx, pool, store, allowedRoot, userID := libraryIntegrationStore(t, prober)
			firstPath := libraryIntegrationFile(t, allowedRoot, "music/Album/01 First.flac", "audio:incomplete-album-first")
			secondPath := libraryIntegrationFile(t, allowedRoot, "music/Album/02 Second.flac", "audio:incomplete-album-second")
			library := libraryIntegrationCreate(t, ctx, store, "Incomplete explicit album artists", "music", filepath.Join(allowedRoot, "music"))
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			first := nfoCatalogItem(t, ctx, store, userID, library.ID, firstPath)
			second := nfoCatalogItem(t, ctx, store, userID, library.ID, secondPath)
			album := nfoCatalogItem(t, ctx, store, userID, library.ID, filepath.Dir(firstPath))
			albumSource := metadataMusicScanSource(t, ctx, pool, album.ID)
			albumState := metadataMusicScanStateSnapshot(t, ctx, pool, album.ID)
			secondSource := metadataMusicScanSource(t, ctx, pool, second.ID)
			secondState := metadataEditTestSnapshot(t, ctx, pool, second.ID)
			firstEntities := first.Entities
			prober.set("01 First.flac", &media.MusicMetadata{Version: media.CurrentMusicMetadataVersion,
				Title: "Fresh first", Album: "Fresh album", Artist: "Fresh Artist", AlbumArtist: "Fresh Ensemble"}, false)
			albumArtist := scenario.albumArtist
			if albumArtist == "" {
				albumArtist = "Unaccepted Ensemble"
			}
			prober.set("02 Second.flac", &media.MusicMetadata{Version: scenario.version,
				Title: "Unaccepted second", Album: "Unaccepted album", Artist: "Unaccepted Artist", AlbumArtist: albumArtist}, scenario.fail)
			job := forceProbeTestScan(t, ctx, store, library.ID, "Completed")
			if job.Error == "" {
				t.Fatal("incomplete album member refresh did not report a warning")
			}
			if metadataMusicScanSource(t, ctx, pool, album.ID) != albumSource ||
				metadataMusicScanStateSnapshot(t, ctx, pool, album.ID) != albumState {
				t.Fatal("an incomplete root replaced the published album source or relationship row versions")
			}
			metadataMusicScanAssertSource(t, ctx, pool, first.ID, musicMetadataSource{
				Version: 1, Name: "Fresh first", Album: "Fresh album", Artists: []string{"Fresh Artist"}, AlbumArtists: []string{"Fresh Ensemble"},
			})
			fresh, err := store.GetItem(ctx, userID, first.ID)
			if err != nil || fresh.ID != first.ID || fresh.Path != first.Path || fresh.ParentID != first.ParentID ||
				len(fresh.Entities.AlbumArtists) != 1 || fresh.Entities.AlbumArtists[0].Name != "Fresh Ensemble" ||
				reflect.DeepEqual(fresh.Entities, firstEntities) {
				t.Fatalf("the earlier successful track lost its independent new album artist publication: %+v error=%v", fresh, err)
			}
			if metadataMusicScanSource(t, ctx, pool, second.ID) != secondSource {
				t.Error("an unaccepted member replaced its previous accepted music source")
			}
			if (scenario.fail || scenario.albumArtist != "") && metadataEditTestSnapshot(t, ctx, pool, second.ID) != secondState {
				t.Error("a failed probe changed the previous track's technical or metadata state")
			}
		})
	}
}
