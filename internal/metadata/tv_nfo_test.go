package metadata

import (
	"strings"
	"testing"
)

func TestParseNFOTelevisionStatusDatesAndPlacement(t *testing.T) {
	series, err := ParseNFO(strings.NewReader(`<tvshow><status>ended</status><enddate>2025-03-04T01:30:00+02:00</enddate></tvshow>`))
	if err != nil || series.Status == nil || *series.Status != "Ended" || series.EndDate == nil || series.EndDate.Format("2006-01-02T15:04:05Z") != "2025-03-03T23:30:00Z" {
		t.Fatalf("series facts were not parsed and normalized: %#v, %v", series, err)
	}
	for _, source := range []string{
		`<airsbefore_season>2</airsbefore_season><airsbefore_episode>3</airsbefore_episode><airsafter_season>0</airsafter_season>`,
		`<displayseason>2</displayseason><displayepisode>3</displayepisode><displayafterseason>0</displayafterseason>`,
	} {
		episode, err := ParseNFO(strings.NewReader(`<episodedetails><season>0</season><episode>1</episode>` + source + `</episodedetails>`))
		if err != nil || episode.ParentIndexNumber == nil || *episode.ParentIndexNumber != 0 || episode.AirsBeforeSeasonNumber == nil || *episode.AirsBeforeSeasonNumber != 2 || episode.AirsBeforeEpisodeNumber == nil || *episode.AirsBeforeEpisodeNumber != 3 || episode.AirsAfterSeasonNumber == nil || *episode.AirsAfterSeasonNumber != 0 {
			t.Fatalf("placement changed physical numbering or lost a source fact: %#v, %v", episode, err)
		}
	}
	unknown, err := ParseNFO(strings.NewReader(`<tvshow><title>Unknown status</title></tvshow>`))
	if err != nil || unknown.Status != nil || unknown.EndDate != nil {
		t.Fatal("absent series facts must remain unknown")
	}
	regular, err := ParseNFO(strings.NewReader(`<movie><status>Ended</status><airsbefore_season>2</airsbefore_season></movie>`))
	if err != nil || regular.Status != nil || regular.AirsBeforeSeasonNumber != nil {
		t.Fatal("TV-only facts leaked to an unrelated media type")
	}
}

func TestParseNFOTelevisionRejectsConflictsAndMalformedFacts(t *testing.T) {
	for _, source := range []string{
		`<tvshow><status>Unreleased</status></tvshow>`,
		`<tvshow><status>0</status></tvshow>`,
		`<tvshow><enddate>2025-02-30</enddate></tvshow>`,
		`<tvshow><enddate>0000-01-01</enddate></tvshow>`,
		`<episodedetails><airsbefore_season>-1</airsbefore_season></episodedetails>`,
		`<episodedetails><airsbefore_episode>2147483648</airsbefore_episode></episodedetails>`,
		`<episodedetails><airsafter_season>1.5</airsafter_season></episodedetails>`,
		`<episodedetails><airsbefore_season>1</airsbefore_season><displayseason>2</displayseason></episodedetails>`,
	} {
		if _, err := ParseNFO(strings.NewReader(source)); err == nil {
			t.Fatalf("accepted malformed TV metadata: %s", source)
		}
	}
}
