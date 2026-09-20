package server

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/metadata"
)

func TestTelevisionMetadataDTOProjectsOnlyRequestedFacts(t *testing.T) {
	series, err := metadata.ParseNFO(strings.NewReader(`<tvshow><status>Ended</status><enddate>2025-04-05</enddate></tvshow>`))
	if err != nil {
		t.Fatal(err)
	}
	api := &Server{serverID: "tv-metadata"}
	item := library.Item{ID: "series", Type: "Series", IsFolder: true, Metadata: &series}
	plain := api.itemDTO(item, nil, false)
	if _, found := plain["Status"]; found {
		t.Fatal("default list exposed an unrequested series status")
	}
	if _, found := plain["EndDate"]; found {
		t.Fatal("default list exposed an unrequested end date")
	}
	dto, _ := metadataDTOJSON(t, api.itemDTO(item, []string{"sTaTuS", "EndDate"}, false))
	if dto["Status"] != "Ended" || dto["EndDate"] != "2025-04-05T00:00:00Z" {
		t.Fatalf("requested TV source facts were omitted: %#v", dto)
	}
	episode, err := metadata.ParseNFO(strings.NewReader(`<episodedetails><airsbefore_season>1</airsbefore_season><airsbefore_episode>2</airsbefore_episode></episodedetails>`))
	if err != nil {
		t.Fatal(err)
	}
	item = library.Item{ID: "special", Type: "Episode", ParentIndexNumber: 0, Metadata: &episode}
	dto, _ = metadataDTOJSON(t, api.itemDTO(item, []string{"IsStandaloneSpecial", "AirsBeforeSeasonNumber", "AirsBeforeEpisodeNumber"}, false))
	if dto["IsStandaloneSpecial"] != false || dto["AirsBeforeSeasonNumber"] != float64(1) || dto["AirsBeforeEpisodeNumber"] != float64(2) || dto["ParentIndexNumber"] != float64(0) {
		t.Fatalf("placement projection changed physical identity or classification: %#v", dto)
	}
	item.Metadata = nil
	dto = api.itemDTO(item, []string{"IsStandaloneSpecial", "Status", "EndDate"}, false)
	if dto["IsStandaloneSpecial"] != true {
		t.Fatal("unplaced season-zero episode was not standalone")
	}
	for _, field := range []string{"Status", "EndDate"} {
		if _, found := dto[field]; found {
			t.Fatalf("unknown %s was fabricated", field)
		}
	}
}
