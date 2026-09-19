package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestAdminMusicEntityQueryIsBoundedAndRoleExplicit(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/music/artists?Role=AlbumArtist&SearchTerm=Ensemble&StartIndex=2&Limit=25&IsFavorite=false", nil)
	query, family, err := parseAdminMusicEntityQuery(r, "artists")
	if err != nil || family != "albumartists" || query.SearchTerm != "Ensemble" || query.StartIndex != 2 || query.Limit != 25 || query.IsFavorite == nil || *query.IsFavorite {
		t.Fatalf("native music query changed: %+v %s %v", query, family, err)
	}
	for _, raw := range []string{"Limit=0", "Limit=201", "StartIndex=-1", "Role=Composer", "Role=Artist&Role=AlbumArtist", "UserId=foreign", "IsFavorite=unknown"} {
		if _, _, err := parseAdminMusicEntityQuery(httptest.NewRequest(http.MethodGet, "/admin/v1/music/artists?"+raw, nil), "artists"); err == nil {
			t.Fatal("native music query accepted invalid input", raw)
		}
	}
}

func TestMusicDTOComposersRequirePersistedPeopleAndRetainTrackDisc(t *testing.T) {
	item := library.Item{Type: "Audio", IndexNumber: 3, ParentIndexNumber: 2,
		Entities: library.ItemEntities{People: []library.PersonRef{{ID: "11", Name: "Composer", Type: "Composer"},
			{ID: "11", Name: "Composer", Type: "Composer", Role: "arrangement"}, {Name: "Unindexed", Type: "Composer"},
			{ID: "12", Name: "Director", Type: "Director"}}}}
	dto := make(map[string]any)
	addMusicCatalogFields(dto, item)
	composers, ok := dto["Composers"].([]map[string]any)
	if !ok || len(composers) != 1 || composers[0]["Id"] != "11" || dto["IndexNumber"] != 3 || dto["ParentIndexNumber"] != 2 {
		t.Fatalf("music DTO invented or lost a relationship: %#v", dto)
	}
}
