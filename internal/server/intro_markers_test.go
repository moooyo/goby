package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestDecodeIntroEditRequiresExactSingleInterval(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"Revision":"0","SourceRevision":"source","EndTicks":20,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":null,"EndTicks":20,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0.5,"EndTicks":20,"Provenance":"Manual"}`,
		`{"Revision":"0","Revision":"1","SourceRevision":"source","StartTicks":0,"EndTicks":20,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"EndTicks":20,"Provenance":"Manual","Extra":true}`,
	} {
		if _, err := decodeIntroEdit([]byte(raw), false); err == nil {
			t.Fatalf("accepted invalid edit: %s", raw)
		}
	}
	edit, err := decodeIntroEdit([]byte(`{"Revision":"0","SourceRevision":"source","StartTicks":0,"EndTicks":20,"Provenance":"Import"}`), false)
	if err != nil || edit.StartTicks != 0 || edit.EndTicks != 20 || edit.Provenance != "Import" {
		t.Fatalf("valid import: %+v, %v", edit, err)
	}
	if _, err := decodeIntroEdit([]byte(`{"Revision":"1","SourceRevision":"source"}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeIntroEdit([]byte(`{"Revision":"1","SourceRevision":"source","StartTicks":0}`), true); err == nil {
		t.Fatal("reset accepted interval fields")
	}
}

func TestItemChaptersDTOUsesExplicitSDKMarkerPair(t *testing.T) {
	item := library.Item{Type: "Episode", Media: &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 0, Title: "Opening"}, {StartTicks: 40, Title: "Part 2"}}}}
	chapters := itemChaptersDTO(item)
	if len(chapters) != 2 || chapters[0]["MarkerType"] != "Chapter" {
		t.Fatal("ordinary chapters became intro markers")
	}
	item.Intro = &library.IntroInterval{StartTicks: 5, EndTicks: 20, Provenance: "Manual"}
	chapters = itemChaptersDTO(item)
	want := []map[string]any{
		{"StartPositionTicks": int64(0), "Name": "Opening", "MarkerType": "Chapter", "ChapterIndex": 0},
		{"StartPositionTicks": int64(5), "Name": "Intro Start", "MarkerType": "IntroStart", "ChapterIndex": 1},
		{"StartPositionTicks": int64(20), "Name": "Intro End", "MarkerType": "IntroEnd", "ChapterIndex": 2},
		{"StartPositionTicks": int64(40), "Name": "Part 2", "MarkerType": "Chapter", "ChapterIndex": 3},
	}
	if !reflect.DeepEqual(chapters, want) {
		t.Fatalf("chapters: %#v", chapters)
	}
}
