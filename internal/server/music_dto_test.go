package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/metadata"
)

func assertEmptyMusicCollections(t *testing.T, dto map[string]any) {
	t.Helper()
	for _, field := range []string{"Artists", "ArtistItems", "AlbumArtists", "Composers"} {
		value, present := dto[field]
		array, typed := value.([]any)
		if !present || !typed || array == nil || len(array) != 0 {
			t.Errorf("unindexed music relationship %s must be an empty JSON array", field)
		}
	}
}

func TestMusicDTOCollectionsReflectOnlyIndexedRelationships(t *testing.T) {
	for _, kind := range []string{"MusicAlbum", "Audio"} {
		for _, mode := range []struct {
			name   string
			detail bool
			fields []string
		}{
			{name: "list"}, {name: "detail", detail: true}, {name: "requested", fields: []string{"Artists", "AlbumArtists"}},
		} {
			t.Run(kind+"/"+mode.name, func(t *testing.T) {
				count := 7
				item := library.Item{ID: "music-id", LibraryID: "music-library", ParentID: "real-parent", Name: "Raw Name",
					SortName: "raw name", Path: "/owned/Music/Raw Name", Type: kind, IsFolder: kind == "MusicAlbum", ChildCount: &count,
					Metadata: &metadata.Metadata{People: []metadata.Person{{Name: "Unlinked Composer", Type: "Composer"}}},
					Entities: library.ItemEntities{People: []library.PersonRef{{ID: "123", Name: "Generic Credit", Type: "Artist"}}}}
				dto, _ := metadataDTOJSON(t, (&Server{serverID: "server-id"}).itemDTO(item, mode.fields, mode.detail))
				assertEmptyMusicCollections(t, dto)
				if dto["Id"] != item.ID || dto["Name"] != item.Name || dto["ParentId"] != item.ParentID || dto["Type"] != kind {
					t.Fatal("music collections changed catalog identity or hierarchy")
				}
				if mode.detail && dto["Path"] != item.Path {
					t.Fatal("music collections changed the catalog path")
				}
				if kind == "MusicAlbum" {
					if dto["ChildCount"] != float64(count) {
						t.Fatal("album projection lost its authoritative direct-child count")
					}
				} else if _, present := dto["ChildCount"]; present {
					t.Fatal("audio leaves must not acquire an album child count")
				}
				for _, field := range []string{"AlbumArtist", "Album", "AlbumId"} {
					if _, present := dto[field]; present {
						t.Errorf("unindexed music scalar %s was invented", field)
					}
				}
			})
		}
	}
}

func TestMusicDTODoesNotInventCountsOrChangeOtherItemKinds(t *testing.T) {
	for _, kind := range []string{"Movie", "Episode", "Folder", "MusicArtist"} {
		dto := map[string]any{"Existing": "preserved"}
		addMusicCatalogFields(dto, library.Item{Type: kind})
		if !reflect.DeepEqual(dto, map[string]any{"Existing": "preserved"}) {
			t.Errorf("music collection projection changed unrelated kind %s", kind)
		}
	}
	for _, item := range []library.Item{{Type: "MusicAlbum", IsFolder: true}, {Type: "MusicAlbum"}} {
		dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
		assertEmptyMusicCollections(t, dto)
		if _, present := dto["ChildCount"]; present {
			t.Fatal("an unread child count must not become a fabricated zero")
		}
	}
}

func TestMusicDTOUsesPersistedArtistsAndThePhysicalAlbumRelationship(t *testing.T) {
	item := library.Item{ID: "track", Type: "Audio", ParentID: "disc", Name: "Accepted Title",
		Path:     "/owned/Music/disc/track.mp3",
		UserData: &library.UserData{ItemID: "track", PlaybackPositionTicks: 34567, PlayCount: 3, IsFavorite: true, Played: true},
		Entities: library.ItemEntities{
			Artists:      []library.EntityRef{{ID: 20, Name: "Second Artist"}, {ID: 10, Name: "First Artist"}},
			AlbumArtists: []library.EntityRef{{ID: 30, Name: "Track-only Relation"}},
		},
		Album: &library.AlbumRef{ID: "physical-album", Name: "Effective Album Title",
			AlbumArtists: []library.EntityRef{{ID: 10, Name: "First Artist"}}}}
	dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if !reflect.DeepEqual(dto["Artists"], []any{"Second Artist", "First Artist"}) ||
		!reflect.DeepEqual(dto["ArtistItems"], []any{map[string]any{"Id": "20", "Name": "Second Artist"}, map[string]any{"Id": "10", "Name": "First Artist"}}) ||
		!reflect.DeepEqual(dto["AlbumArtists"], []any{map[string]any{"Id": "30", "Name": "Track-only Relation"}}) {
		t.Fatal("music DTO lost persisted association identity, role, or ordering")
	}
	if dto["AlbumArtist"] != "Track-only Relation" || dto["AlbumId"] != "physical-album" || dto["Album"] != "Effective Album Title" ||
		dto["ParentId"] != "disc" || dto["Name"] != "Accepted Title" || dto["Path"] != item.Path {
		t.Fatal("music DTO confused the physical album, direct parent, or accepted display names")
	}
	if !reflect.DeepEqual(dto["UserData"], map[string]any{"PlaybackPositionTicks": float64(34567), "PlayCount": float64(3),
		"IsFavorite": true, "Played": true}) {
		t.Fatal("album-artist precedence changed the track's user data")
	}
	if composers, ok := dto["Composers"].([]any); !ok || composers == nil || len(composers) != 0 {
		t.Fatal("unimplemented composer extraction invented a relationship")
	}
	item.Album.AlbumArtists = nil
	dto, _ = metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if !reflect.DeepEqual(dto["AlbumArtists"], []any{map[string]any{"Id": "30", "Name": "Track-only Relation"}}) ||
		dto["AlbumArtist"] != "Track-only Relation" {
		t.Fatal("a mixed physical album discarded the track's own persisted album artist")
	}
	item.Album = nil
	dto, _ = metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if !reflect.DeepEqual(dto["AlbumArtists"], []any{map[string]any{"Id": "30", "Name": "Track-only Relation"}}) {
		t.Fatal("a standalone track lost its own persisted album artist")
	}
	for _, field := range []string{"Album", "AlbumId"} {
		if _, present := dto[field]; present {
			t.Fatalf("a standalone track invented a physical %s relationship", field)
		}
	}
}

func TestMusicDTOFallsBackToPhysicalAlbumOnlyWithoutOwnAlbumArtistCredits(t *testing.T) {
	for _, own := range [][]library.EntityRef{nil, {}} {
		item := library.Item{ID: "track", Type: "Audio", Entities: library.ItemEntities{AlbumArtists: own},
			Album: &library.AlbumRef{ID: "physical-album", Name: "Physical Album",
				AlbumArtists: []library.EntityRef{{ID: 10, Name: "Album Artist"}}}}
		dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
		if !reflect.DeepEqual(dto["AlbumArtists"], []any{map[string]any{"Id": "10", "Name": "Album Artist"}}) ||
			dto["AlbumArtist"] != "Album Artist" || dto["AlbumId"] != "physical-album" || dto["Album"] != "Physical Album" {
			t.Fatal("an absent track credit failed to inherit its actual physical album artist")
		}
		item.Album.AlbumArtists = nil
		dto, _ = metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
		assertEmptyMusicCollections(t, dto)
		if _, present := dto["AlbumArtist"]; present {
			t.Fatal("an absent own and physical album artist produced a scalar")
		}
	}
}

func TestMusicDTOOwnAlbumArtistCreditsKeepOrderingAndRejectInvalidReferences(t *testing.T) {
	item := library.Item{Type: "Audio", Entities: library.ItemEntities{
		AlbumArtists: []library.EntityRef{{Name: "Raw Album Tag"}, {ID: -1, Name: "Invalid ID"}, {ID: 9},
			{ID: 30, Name: "Second Album Artist"}, {ID: 20, Name: "First Album Artist"}},
		People: []library.PersonRef{{ID: "77", Name: "Person Artist", Type: "AlbumArtist"}},
	}, Album: &library.AlbumRef{ID: "physical-album", Name: "Physical Album",
		AlbumArtists: []library.EntityRef{{ID: 10, Name: "Physical Fallback"}}}}
	dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if !reflect.DeepEqual(dto["AlbumArtists"], []any{map[string]any{"Id": "30", "Name": "Second Album Artist"},
		map[string]any{"Id": "20", "Name": "First Album Artist"}}) {
		t.Fatal("own album-artist credits lost ordering or leaked invalid, generic, or fallback identities")
	}
	if _, present := dto["AlbumArtist"]; present {
		t.Fatal("multiple own album artists were collapsed into an arbitrary scalar")
	}
	item.Entities.AlbumArtists = item.Entities.AlbumArtists[:3]
	dto, _ = metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	assertEmptyMusicCollections(t, dto)
	if _, present := dto["AlbumArtist"]; present {
		t.Fatal("invalid own album-artist references invented a navigable scalar")
	}
}

func TestMusicDTOOnlyEmitsNavigableMusicEntityIDs(t *testing.T) {
	item := library.Item{Type: "MusicAlbum", IsFolder: true, Entities: library.ItemEntities{
		Artists:      []library.EntityRef{{Name: "Raw Tag Without ID"}, {ID: -1, Name: "Invalid ID"}, {ID: 9}},
		AlbumArtists: []library.EntityRef{{ID: 10, Name: "One"}, {ID: 11, Name: "Two"}},
	}}
	dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if artists, ok := dto["Artists"].([]any); !ok || artists == nil || len(artists) != 0 {
		t.Fatal("raw or invalid entity data invented a navigable artist")
	}
	if artists, ok := dto["ArtistItems"].([]any); !ok || artists == nil || len(artists) != 0 {
		t.Fatal("invalid persistent entity IDs leaked into artist items")
	}
	if _, present := dto["AlbumArtist"]; present {
		t.Fatal("multiple album artists were collapsed into an arbitrary scalar")
	}
}
