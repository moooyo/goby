package server

import (
	"strconv"

	"github.com/moooyo/goby/internal/library"
)

func addMusicCatalogFields(dto map[string]any, item library.Item) {
	if item.Type != "MusicAlbum" && item.Type != "Audio" {
		return
	}
	// Only persisted music associations provide navigable identities. Generic
	// People credits and raw tag strings never manufacture a NameIdPair ID.
	artists, artistItems := musicEntityDTOs(item.Entities.Artists)
	dto["Artists"], dto["ArtistItems"] = artists, artistItems
	albumArtists := item.Entities.AlbumArtists
	if item.Type == "Audio" && item.Album != nil {
		dto["AlbumId"], dto["Album"] = item.Album.ID, item.Album.Name
		// Raw album_artist tags are not extracted in this increment. A track
		// inherits the accepted relationship of its physical album, so a track
		// artist in a mixed album cannot become a fabricated album artist.
		albumArtists = item.Album.AlbumArtists
	}
	albumArtistNames, albumArtistItems := musicEntityDTOs(albumArtists)
	dto["AlbumArtists"] = albumArtistItems
	if len(albumArtistNames) == 1 {
		dto["AlbumArtist"] = albumArtistNames[0]
	}
	// Composer tags and associations are not indexed by this increment.
	dto["Composers"] = []map[string]any{}
	if item.Type == "MusicAlbum" && item.IsFolder && item.ChildCount != nil {
		dto["ChildCount"] = *item.ChildCount
	}
}

func musicEntityDTOs(entities []library.EntityRef) ([]string, []map[string]any) {
	names := make([]string, 0, len(entities))
	items := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		if entity.ID <= 0 || entity.Name == "" {
			continue
		}
		names = append(names, entity.Name)
		items = append(items, map[string]any{"Name": entity.Name, "Id": strconv.FormatInt(entity.ID, 10)})
	}
	return names, items
}
