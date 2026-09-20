package server

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

func TestSearchHintsParserPreservesPresenceAndRejectsAmbiguousControls(t *testing.T) {
	values, err := url.ParseQuery("SearchTerm=%E4%B8%AD&IncludeMedia=false&IsMovie=false&Limit=0&StartIndex=3&IncludeItemTypes=Audio,MusicAlbum&IncludeItemTypes=MusicArtist&ExcludeItemTypes=Person")
	if err != nil {
		t.Fatal(err)
	}
	query, err := parseSearchHintsQuery(values)
	if err != nil || query.SearchTerm != "\u4e2d" || query.IncludeMedia == nil || *query.IncludeMedia ||
		query.IsMovie == nil || *query.IsMovie || query.IncludeArtists != nil || query.Limit != 0 || query.StartIndex != 3 ||
		!reflect.DeepEqual(query.IncludeItemTypes, []string{"Audio", "MusicAlbum", "MusicArtist"}) {
		t.Fatalf("search controls lost their explicit presence: %+v, %v", query, err)
	}
	defaults, err := parseSearchHintsQuery(url.Values{"SearchTerm": {""}})
	if err != nil || defaults.Limit != 20 || defaults.SearchTerm != "" {
		t.Fatal("empty required term or omitted pagination changed", defaults, err)
	}
	for _, raw := range []string{
		"", "SearchTerm=x&SearchTerm=x", "SearchTerm=x&Limit=", "SearchTerm=x&Limit=1001",
		"SearchTerm=x&Limit=-1", "SearchTerm=x&StartIndex=2147483648", "SearchTerm=x&Limit=1&Limit=1",
		"SearchTerm=x&IncludePeople=", "SearchTerm=x&IncludeArtists=maybe", "SearchTerm=x&IsMovie=true&IsMovie=false",
		"SearchTerm=x&IncludeItemTypes=,", "SearchTerm=x&MediaTypes=", "SearchTerm=x&UserId=",
		"SearchTerm=x&UserId=a&UserId=b", "SearchTerm=x&EnableImages=", "SearchTerm=x&EnableImages=maybe",
		"SearchTerm=x&IsNews=false", "SearchTerm=x&IsKids=true", "SearchTerm=x&IsSports=false",
	} {
		t.Run(raw, func(t *testing.T) {
			values, err := url.ParseQuery(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseSearchHintsQuery(values); err == nil {
				t.Fatal("ambiguous or unsupported search controls were accepted")
			}
		})
	}
}

func TestSearchHintDTOUsesTypedOpaqueReferencesAndAuthorizedRelationships(t *testing.T) {
	year := 2026
	item := &library.Item{ID: "opaque/id?\u4fdd\u6301", Name: "Audio Name", Type: "Audio", IndexNumber: 3, ParentIndexNumber: 2,
		Metadata: &metadata.Metadata{ProductionYear: &year},
		Media:    &media.Info{DurationTicks: 120000000},
		Album:    &library.AlbumRef{ID: "album:opaque", Name: "Album Name", AlbumArtists: []library.EntityRef{{ID: 8, Name: "Inherited Ensemble"}}},
		Entities: library.ItemEntities{Artists: []library.EntityRef{{ID: 7, Name: "Artist"}}},
	}
	hint := library.SearchHint{Reference: library.SearchHintReference{Kind: "Item", ID: item.ID}, Item: item,
		LegacyImageReference: true, Images: []library.Image{{ImageType: "Primary", Tag: "item-tag", Width: 40, Height: 80}}}
	dto := (&Server{}).searchHintDTO(hint, "user &1", true)
	reference, ok := dto["GobyReference"].(map[string]string)
	if !ok || reference["Kind"] != "Item" || reference["Id"] != item.ID || dto["Id"] != item.ID || dto["ItemId"] != item.ID ||
		dto["AlbumId"] != "album:opaque" || dto["AlbumArtist"] != "Inherited Ensemble" ||
		dto["PrimaryImageAspectRatio"] != 0.5 || dto["PrimaryImageTag"] != "item-tag" ||
		dto["MediaType"] != "Audio" || dto["RunTimeTicks"] != int64(120000000) || dto["ProductionYear"] != year {
		t.Fatalf("hint changed source identities or metadata: %#v", dto)
	}
	want := "/emby/Items/opaque%2Fid%3F%E4%BF%9D%E6%8C%81?UserId=user+%261"
	if dto["GobyNavigationUrl"] != want || dto["PrimaryImageUrl"] != strings.Replace(want, "?UserId=", "/Images/Primary/0?UserId=", 1)+"&tag=item-tag" {
		t.Fatalf("opaque path escaping or scope changed: %#v", dto)
	}
	for _, key := range []string{"Path", "MediaSources", "UserData", "EpisodeCount", "SongCount", "StartDate", "EndDate"} {
		if _, exists := dto[key]; exists {
			t.Fatalf("hint invented an unrelated or private field %s", key)
		}
	}
}

func TestSearchHintEntityImagesCannotBorrowCollidingPhysicalOwner(t *testing.T) {
	hint := library.SearchHint{Reference: library.SearchHintReference{Kind: "Entity", ID: "42"},
		Entity: &library.Entity{ID: 42, Name: "AlbumArtists/\u540d\u5b57", Type: "MusicArtist"},
		Images: []library.Image{
			{ImageType: "Primary", Tag: "entity-primary", Width: 80, Height: 40},
			{ImageType: "Thumb", Tag: "entity-thumb"},
			{ImageType: "Backdrop", Tag: "other-index", ImageIndex: 1},
			{ImageType: "Backdrop", Tag: "entity-backdrop"},
		},
	}
	dto := (&Server{}).searchHintDTO(hint, "", true)
	if dto["GobyNavigationUrl"] != "/emby/Search/Entities/42" ||
		dto["PrimaryImageUrl"] != "/emby/Artists/AlbumArtists%2F%E5%90%8D%E5%AD%97/Images/Primary/0?tag=entity-primary" ||
		dto["BackdropImageUrl"] != "/emby/Artists/AlbumArtists%2F%E5%90%8D%E5%AD%97/Images/Backdrop/0?tag=entity-backdrop" {
		t.Fatalf("typed entity references were not retained: %#v", dto)
	}
	for _, key := range []string{"PrimaryImageTag", "ThumbImageTag", "ThumbImageItemId", "BackdropImageTag", "BackdropImageItemId"} {
		if _, exists := dto[key]; exists {
			t.Fatalf("ambiguous implicit image reference survived: %s", key)
		}
	}
	withoutImages := (&Server{}).searchHintDTO(hint, "", false)
	for key := range withoutImages {
		if strings.Contains(key, "Image") {
			t.Fatalf("disabled image projection survived: %s", key)
		}
	}
}
