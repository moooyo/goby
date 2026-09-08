package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/metadata"
)

func metadataDTOFixture(t *testing.T) library.Item {
	t.Helper()
	const sidecar = `<?xml version="1.0" encoding="UTF-8"?>
<episodedetails>
  <title>Sidecar Title</title>
  <sorttitle>Sidecar Sort Title</sorttitle>
  <plot>Sidecar overview that must not override the catalog.</plot>
  <season>9</season>
  <episode>42</episode>
  <originaltitle>Original Episode Title</originaltitle>
  <year>2024</year>
  <premiered>2024-05-06T01:02:03+02:00</premiered>
  <rating>8.75</rating>
  <mpaa>TV-PG</mpaa>
  <uniqueid type="imdb">tt1234567</uniqueid>
  <tmdbid>4321</tmdbid>
  <genre>Adventure</genre>
  <genre>Drama</genre>
  <tag>Local Collection</tag>
  <studio>Example Studio</studio>
  <studio>Second Studio</studio>
  <actor><name>Unordered First</name><role>Guide</role></actor>
  <actor><name>Ordered Two First</name><role>Captain</role><order>2</order></actor>
  <actor><name>Ordered Zero</name><role>Navigator</role><order>0</order><thumb>/secret/sidecar/actor.jpg</thumb></actor>
  <actor><name>Ordered Two Second</name><order>2</order></actor>
  <actor><name>Ordered One</name><order>1</order></actor>
  <director>Example Director</director>
  <writer>Example Writer</writer>
  <actor><name>Unordered Last</name></actor>
  <path>/secret/sidecar/episode.nfo</path>
  <art><poster>/secret/sidecar/poster.jpg</poster></art>
  <thumb>https://untrusted.example/cover.jpg</thumb>
</episodedetails>`
	parsed, err := metadata.ParseNFO(strings.NewReader(sidecar))
	if err != nil {
		t.Fatalf("parse metadata projection fixture: %v", err)
	}
	return library.Item{
		ID: "episode-id", LibraryID: "library-id", ParentID: "season-id", Type: "Episode",
		Name: "Catalog Episode", SortName: "catalog episode", Overview: "Catalog overview.",
		Path: "/media/series/catalog-episode.mp4", IndexNumber: 7, ParentIndexNumber: 3,
		CreatedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Metadata: &parsed,
	}
}

func metadataDTOJSON(t *testing.T, dto map[string]any) (map[string]any, []byte) {
	t.Helper()
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal metadata item DTO: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode metadata item DTO: %v", err)
	}
	return decoded, encoded
}

func TestMetadataDTOCoreFieldsPreserveCatalogIdentityAndJSONTypes(t *testing.T) {
	item := metadataDTOFixture(t)
	api := &Server{serverID: "server-id"}
	for _, mode := range []struct {
		name   string
		fields []string
		detail bool
	}{
		{name: "default_projection"},
		{name: "requested_overview", fields: []string{"oVeRvIeW"}},
		{name: "detail_projection", detail: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dto, encoded := metadataDTOJSON(t, api.itemDTO(item, mode.fields, mode.detail))
			for key, expected := range map[string]any{
				"Id": "episode-id", "Name": "Catalog Episode", "SortName": "catalog episode",
				"Type": "Episode", "ParentId": "season-id", "ServerId": "server-id",
				"IndexNumber": float64(7), "ParentIndexNumber": float64(3),
			} {
				if dto[key] != expected {
					t.Errorf("JSON field %s = %#v, want %#v", key, dto[key], expected)
				}
			}
			for key, expected := range map[string]any{
				"ProductionYear": float64(2024), "PremiereDate": "2024-05-05T23:02:03Z",
				"OriginalTitle": "Original Episode Title", "CommunityRating": float64(8.75), "OfficialRating": "TV-PG",
			} {
				actual, exists := dto[key]
				if mode.detail {
					if !exists || actual != expected {
						t.Errorf("detail scalar %s = %#v, want %#v", key, actual, expected)
					}
				} else if exists {
					t.Errorf("unrequested scalar %s must be omitted from item lists", key)
				}
			}
			if mode.detail || len(mode.fields) > 0 {
				if dto["Overview"] != "Catalog overview." {
					t.Errorf("overview must come from the catalog item: %#v", dto["Overview"])
				}
			} else if _, exists := dto["Overview"]; exists {
				t.Error("the default item projection must not include Overview")
			}
			// Typed decoding rejects strings, nulls, and nonintegral production years.
			var wire struct {
				ProductionYear  *int32
				CommunityRating *float32
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatalf("decode official numeric field types: %v", err)
			}
			if mode.detail {
				if wire.ProductionYear == nil || *wire.ProductionYear != 2024 || wire.CommunityRating == nil || *wire.CommunityRating != 8.75 {
					t.Errorf("metadata numeric fields did not survive typed JSON decoding: %+v", wire)
				}
			} else if wire.ProductionYear != nil || wire.CommunityRating != nil {
				t.Errorf("unrequested numeric scalars were projected: %+v", wire)
			}
			for _, forbidden := range []string{"Metadata", "NfoPath", "SidecarPath", "MetadataPath", "Art", "Thumb", "PrimaryImagePath"} {
				if _, exists := dto[forbidden]; exists {
					t.Errorf("DTO must not expose sidecar field %s", forbidden)
				}
			}
			for _, privateValue := range []string{"/secret/sidecar", "untrusted.example", "Sidecar overview"} {
				if bytes.Contains(encoded, []byte(privateValue)) {
					t.Errorf("DTO exposes ignored sidecar data %q", privateValue)
				}
			}
		})
	}
}

func TestMetadataDTOScalarFieldsAreIndependentlyProjected(t *testing.T) {
	item := metadataDTOFixture(t)
	api := &Server{serverID: "server-id"}
	expected := map[string]any{
		"ProductionYear": float64(2024), "PremiereDate": "2024-05-05T23:02:03Z",
		"OriginalTitle": "Original Episode Title", "CommunityRating": float64(8.75), "OfficialRating": "TV-PG",
	}
	for _, requested := range []string{"OriginalTitle", "ProductionYear", "PremiereDate", "CommunityRating", "OfficialRating"} {
		t.Run(requested, func(t *testing.T) {
			dto, encoded := metadataDTOJSON(t, api.itemDTO(item, []string{strings.ToLower(requested)}, false))
			for field, value := range expected {
				actual, exists := dto[field]
				if field == requested {
					if !exists || actual != value {
						t.Errorf("requested scalar %s = %#v, want %#v", field, actual, value)
					}
				} else if exists {
					t.Errorf("requesting %s unexpectedly projected scalar %s", requested, field)
				}
			}
			for _, unrequested := range []string{"Overview", "ProviderIds", "Genres", "GenreItems", "Tags", "TagItems", "Studios", "People", "Path"} {
				if _, exists := dto[unrequested]; exists {
					t.Errorf("requesting scalar %s unexpectedly projected %s", requested, unrequested)
				}
			}
			var wire struct {
				ProductionYear  *int32
				CommunityRating *float32
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatalf("decode projected official numeric field types: %v", err)
			}
			if requested == "ProductionYear" && (wire.ProductionYear == nil || *wire.ProductionYear != 2024) {
				t.Error("requested ProductionYear must be a JSON integer compatible with int32")
			}
			if requested == "CommunityRating" && (wire.CommunityRating == nil || *wire.CommunityRating != 8.75) {
				t.Error("requested CommunityRating must be a JSON number compatible with float32")
			}
		})
	}
}

func TestMetadataDTOCollectionProjectionAndWireShapes(t *testing.T) {
	item := metadataDTOFixture(t)
	api := &Server{serverID: "server-id"}
	expected := map[string]any{
		"ProviderIds": map[string]any{"Imdb": "tt1234567", "Tmdb": "4321"},
		"Genres":      []any{"Adventure", "Drama"},
		"GenreItems":  []any{map[string]any{"Name": "Adventure"}, map[string]any{"Name": "Drama"}},
		"TagItems":    []any{map[string]any{"Name": "Local Collection"}},
		// Entity projection is partial until persistent IDs exist; none are invented.
		"Studios": []any{map[string]any{"Name": "Example Studio"}, map[string]any{"Name": "Second Studio"}},
		"People": []any{
			map[string]any{"Name": "Ordered Zero", "Type": "Actor", "Role": "Navigator"},
			map[string]any{"Name": "Ordered One", "Type": "Actor"},
			map[string]any{"Name": "Ordered Two First", "Type": "Actor", "Role": "Captain"},
			map[string]any{"Name": "Ordered Two Second", "Type": "Actor"},
			map[string]any{"Name": "Unordered First", "Type": "Actor", "Role": "Guide"},
			map[string]any{"Name": "Example Director", "Type": "Director"},
			map[string]any{"Name": "Example Writer", "Type": "Writer"},
			map[string]any{"Name": "Unordered Last", "Type": "Actor"},
		},
	}
	before, err := json.Marshal(item.Metadata)
	if err != nil {
		t.Fatalf("snapshot metadata before projection: %v", err)
	}
	modes := []struct {
		name   string
		fields []string
		detail bool
	}{
		{name: "default_omits_collections"},
		{name: "detail_includes_collections", detail: true},
	}
	for _, field := range []string{"ProviderIds", "Genres", "Tags", "Studios", "People"} {
		modes = append(modes, struct {
			name   string
			fields []string
			detail bool
		}{name: "requested_" + field, fields: []string{strings.ToLower(field)}})
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			dto, _ := metadataDTOJSON(t, api.itemDTO(item, mode.fields, mode.detail))
			if _, exists := dto["Tags"]; exists {
				t.Error("tag projection must use TagItems and never emit a Tags string array")
			}
			for field, value := range expected {
				requestField := field
				if field == "GenreItems" {
					requestField = "Genres"
				} else if field == "TagItems" {
					requestField = "Tags"
				}
				requested := mode.detail || len(mode.fields) == 1 && strings.EqualFold(mode.fields[0], requestField)
				actual, exists := dto[field]
				if !requested {
					if exists {
						t.Errorf("unrequested metadata collection %s was projected", field)
					}
					continue
				}
				if !exists || !reflect.DeepEqual(actual, value) {
					t.Errorf("JSON collection %s = %#v, want %#v", field, actual, value)
				}
			}
		})
	}
	after, err := json.Marshal(item.Metadata)
	if err != nil {
		t.Fatalf("snapshot metadata after projection: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("DTO projection changed source metadata or reordered its People slice")
	}
}

func TestMetadataDTOEmptyMetadataUsesRequestedEmptyCollections(t *testing.T) {
	api := &Server{serverID: "server-id"}
	collections := []string{"ProviderIds", "Genres", "GenreItems", "TagItems", "Studios", "People"}
	for _, source := range []struct {
		name     string
		metadata *metadata.Metadata
	}{
		{name: "missing_metadata"},
		{name: "empty_metadata", metadata: &metadata.Metadata{}},
	} {
		t.Run(source.name, func(t *testing.T) {
			item := library.Item{ID: "item-id", Name: "Catalog Name", SortName: "catalog name", Type: "Movie", Metadata: source.metadata}
			for _, mode := range []struct {
				name   string
				fields []string
				detail bool
			}{
				{name: "default"},
				{name: "detail", detail: true},
				{name: "requested", fields: []string{"providerids", "genres", "tags", "studios", "people", "originaltitle", "productionyear", "premieredate", "communityrating", "officialrating"}},
			} {
				t.Run(mode.name, func(t *testing.T) {
					dto, _ := metadataDTOJSON(t, api.itemDTO(item, mode.fields, mode.detail))
					if _, exists := dto["Tags"]; exists {
						t.Error("empty tag projection must use TagItems, not Tags")
					}
					for _, field := range []string{"ProductionYear", "PremiereDate", "OriginalTitle", "CommunityRating", "OfficialRating"} {
						if value, exists := dto[field]; exists {
							t.Errorf("missing scalar %s must be omitted, got %#v", field, value)
						}
					}
					for _, field := range collections {
						value, exists := dto[field]
						if !mode.detail && len(mode.fields) == 0 {
							if exists {
								t.Errorf("unrequested empty collection %s must be omitted", field)
							}
							continue
						}
						if !exists || value == nil {
							t.Errorf("requested empty collection %s must be present and non-null", field)
							continue
						}
						if field == "ProviderIds" {
							object, ok := value.(map[string]any)
							if !ok || len(object) != 0 {
								t.Errorf("empty ProviderIds must be an object, got %#v", value)
							}
						} else {
							array, ok := value.([]any)
							if !ok || len(array) != 0 {
								t.Errorf("empty %s must be an array, got %#v", field, value)
							}
						}
					}
				})
			}
		})
	}
}

func TestMetadataDTOPreservesZeroRatingAndNormalizesPremiereDate(t *testing.T) {
	zero := 0.0
	localDate := time.Date(2024, 6, 7, 1, 2, 3, 0, time.FixedZone("Fixture offset", 8*60*60))
	item := library.Item{ID: "item-id", Type: "Movie", Metadata: &metadata.Metadata{CommunityRating: &zero, PremiereDate: &localDate}}
	dto, encoded := metadataDTOJSON(t, (&Server{}).itemDTO(item, nil, true))
	if rating, exists := dto["CommunityRating"]; !exists || rating != float64(0) {
		t.Errorf("explicit zero CommunityRating must remain a JSON number, got %#v", rating)
	}
	if dto["PremiereDate"] != "2024-06-06T17:02:03Z" {
		t.Errorf("PremiereDate must be UTC RFC3339, got %#v", dto["PremiereDate"])
	}
	var wire struct{ CommunityRating *float32 }
	if err := json.Unmarshal(encoded, &wire); err != nil || wire.CommunityRating == nil || *wire.CommunityRating != 0 {
		t.Errorf("zero rating did not survive typed JSON decoding: %+v, error = %v", wire, err)
	}
}

func TestMetadataDTOTagsSortByNameWithoutChangingSource(t *testing.T) {
	source := &metadata.Metadata{Tags: []string{"reference", "local-artwork", "beta", "Alpha", "alpha", "Beta"}}
	before := append([]string(nil), source.Tags...)
	item := library.Item{ID: "item-id", Type: "Movie", Metadata: source}
	expected := []any{
		map[string]any{"Name": "Alpha"}, map[string]any{"Name": "alpha"},
		map[string]any{"Name": "Beta"}, map[string]any{"Name": "beta"},
		map[string]any{"Name": "local-artwork"}, map[string]any{"Name": "reference"},
	}
	for _, mode := range []struct {
		name   string
		fields []string
		detail bool
	}{
		{name: "requested_tags", fields: []string{"Tags"}},
		{name: "detail", detail: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dto, _ := metadataDTOJSON(t, (&Server{}).itemDTO(item, mode.fields, mode.detail))
			if !reflect.DeepEqual(dto["TagItems"], expected) {
				t.Errorf("TagItems = %#v, want names sorted with deterministic case ties", dto["TagItems"])
			}
			if !reflect.DeepEqual(source.Tags, before) {
				t.Errorf("tag projection changed source order: %#v", source.Tags)
			}
		})
	}
}

func TestMetadataDTORespectsPathProjectionAndImageSwitches(t *testing.T) {
	item := metadataDTOFixture(t)
	api := &Server{serverID: "server-id"}
	for _, mode := range []struct {
		name     string
		fields   []string
		detail   bool
		wantPath bool
	}{
		{name: "default"},
		{name: "metadata_fields_do_not_request_path", fields: []string{"ProviderIds", "People"}},
		{name: "requested_path", fields: []string{"pAtH"}, wantPath: true},
		{name: "detail_path", detail: true, wantPath: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dto, _ := metadataDTOJSON(t, api.itemDTO(item, mode.fields, mode.detail))
			path, exists := dto["Path"]
			if mode.wantPath {
				if !exists || path != item.Path {
					t.Errorf("projected Path = %#v, want the catalog media path", path)
				}
			} else if exists {
				t.Error("unrequested catalog Path was projected")
			}
		})
	}
	for _, enabled := range []string{"true", "false"} {
		t.Run("images_"+enabled, func(t *testing.T) {
			dto := api.itemDTO(item, nil, true)
			request := httptest.NewRequest(http.MethodGet, "/emby/Items?EnableImages="+enabled, nil)
			applyItemSwitches(dto, request)
			wire, encoded := metadataDTOJSON(t, dto)
			for _, field := range []string{"ImageTags", "BackdropImageTags"} {
				_, exists := wire[field]
				if exists != (enabled == "true") {
					t.Errorf("%s presence = %v with EnableImages=%s", field, exists, enabled)
				}
			}
			if wire["Path"] != item.Path || wire["OriginalTitle"] != "Original Episode Title" {
				t.Error("image switching changed path or descriptive metadata projection")
			}
			if bytes.Contains(encoded, []byte("/secret/sidecar")) || bytes.Contains(encoded, []byte("untrusted.example")) {
				t.Error("image switching exposed ignored sidecar artwork paths")
			}
		})
	}
}
