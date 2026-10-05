package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestDecodeCreditsEditRequiresExactPoint(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{}`, `true`,
		`{"Revision":"0","SourceRevision":"source","Provenance":"Manual"}`,
		`{"Revision":null,"SourceRevision":"source","StartTicks":0,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":null,"StartTicks":0,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":null,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":null}`,
		`{"Revision":0,"SourceRevision":"source","StartTicks":0,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":"0","Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0.5,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":9223372036854775808,"Provenance":"Manual"}`,
		`{"Revision":"0","Revision":"1","SourceRevision":"source","StartTicks":0,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"Start\u0054icks":1,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":"Manual","Extra":true}`,
		`{"revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":"Manual"}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":"Manual"} {}`,
		`{"Revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":"Manual"`,
	} {
		if _, err := decodeCreditsEdit([]byte(raw), false); err == nil {
			t.Fatalf("accepted invalid credits edit: %s", raw)
		}
	}
	for _, provenance := range []string{"Manual", "Import"} {
		raw := `{"Revision":"0","SourceRevision":"source","StartTicks":0,"Provenance":"` + provenance + `"}`
		edit, err := decodeCreditsEdit([]byte(raw), false)
		if err != nil || edit.Revision != "0" || edit.SourceRevision != "source" || edit.StartTicks != 0 || edit.Provenance != provenance {
			t.Fatalf("explicit zero start: %+v, %v", edit, err)
		}
	}
	if _, err := decodeCreditsEdit([]byte(`{"Revision":"1","SourceRevision":"source"}`), true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"Revision":"1","SourceRevision":"source","StartTicks":0}`,
		`{"Revision":"1","SourceRevision":"source","Provenance":"Manual"}`,
		`{"Revision":"1","SourceRevision":null}`,
		`{"Revision":"1","Revision":"1","SourceRevision":"source"}`,
	} {
		if _, err := decodeCreditsEdit([]byte(raw), true); err == nil {
			t.Fatalf("accepted invalid reset: %s", raw)
		}
	}
}

func TestItemChaptersDTOUsesCreditsStartAlongsideIntro(t *testing.T) {
	item := library.Item{
		Type:    "Episode",
		Media:   &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 0, Title: "Opening"}, {StartTicks: 95, Title: "End"}}},
		Intro:   &library.IntroInterval{StartTicks: 5, EndTicks: 20, Provenance: "Manual"},
		Credits: &library.CreditsPoint{StartTicks: 90, Provenance: "Import"},
	}
	want := []map[string]any{
		{"StartPositionTicks": int64(0), "Name": "Opening", "MarkerType": "Chapter", "ChapterIndex": 0},
		{"StartPositionTicks": int64(5), "Name": "Intro Start", "MarkerType": "IntroStart", "ChapterIndex": 1},
		{"StartPositionTicks": int64(20), "Name": "Intro End", "MarkerType": "IntroEnd", "ChapterIndex": 2},
		{"StartPositionTicks": int64(90), "Name": "Credits Start", "MarkerType": "CreditsStart", "ChapterIndex": 3},
		{"StartPositionTicks": int64(95), "Name": "End", "MarkerType": "Chapter", "ChapterIndex": 4},
	}
	if got := itemChaptersDTO(item); !reflect.DeepEqual(got, want) {
		t.Fatalf("chapters = %#v, want %#v", got, want)
	}
	item.Credits = nil
	for _, chapter := range itemChaptersDTO(item) {
		if chapter["MarkerType"] == "CreditsStart" {
			t.Fatal("ordinary End chapter inferred credits")
		}
	}
}
