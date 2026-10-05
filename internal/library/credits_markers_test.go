package library

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestExplicitChapterCredits(t *testing.T) {
	for _, test := range []struct {
		name string
		info *media.Info
		want *CreditsPoint
	}{
		{"absent media", nil, nil},
		{"absent chapters", &media.Info{DurationTicks: 100}, nil},
		{"ordinary end", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "End"}}}, nil},
		{"ordinary credits", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "Credits"}}}, nil},
		{"case sensitive", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "creditsstart"}}}, nil},
		{"explicit", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "CreditsStart"}}}, &CreditsPoint{90, "Chapter"}},
		{"zero", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 0, Title: "CreditsStart"}}}, &CreditsPoint{0, "Chapter"}},
		{"trimmed title", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: " CreditsStart "}}}, &CreditsPoint{90, "Chapter"}},
		{"negative", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: -1, Title: "CreditsStart"}}}, nil},
		{"at end", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 100, Title: "CreditsStart"}}}, nil},
		{"past end", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 101, Title: "CreditsStart"}}}, nil},
		{"no duration", &media.Info{Chapters: []media.Chapter{{StartTicks: 0, Title: "CreditsStart"}}}, nil},
		{"duplicates", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "CreditsStart"}, {StartTicks: 90, Title: "CreditsStart"}}}, nil},
		{"multiple candidates", &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 80, Title: "CreditsStart"}, {StartTicks: 90, Title: "CreditsStart"}}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ExplicitChapterCredits(test.info); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("credits = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestProjectItemCreditsRequiresPlayableItemAndValidOverride(t *testing.T) {
	for _, kind := range []string{"Movie", "Episode", "Audio", "Video", "Series"} {
		t.Run(kind, func(t *testing.T) {
			item := Item{Type: kind, Media: &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 90, Title: "CreditsStart"}}}}
			if err := projectItemCredits(&item, []byte(`{"StartTicks":80,"Provenance":"Manual"}`)); err != nil {
				t.Fatal(err)
			}
			if kind != "Movie" && kind != "Episode" {
				if item.Credits != nil {
					t.Fatalf("credits emitted for %s", kind)
				}
				return
			}
			if item.Credits == nil || item.Credits.StartTicks != 80 || item.Credits.Provenance != "Manual" {
				t.Fatalf("manual override missing: %+v", item.Credits)
			}
			for _, raw := range []string{`null`, `{"StartTicks":100,"Provenance":"Manual"}`, `{"StartTicks":-1,"Provenance":"Import"}`} {
				if err := projectItemCredits(&item, []byte(raw)); err != nil {
					t.Fatal(err)
				}
				if item.Credits == nil || item.Credits.StartTicks != 90 || item.Credits.Provenance != "Chapter" {
					t.Fatalf("invalid override displaced source facts: %+v", item.Credits)
				}
			}
			if err := projectItemCredits(&item, []byte(`{"StartTicks":"invalid"}`)); err == nil {
				t.Fatal("invalid persisted JSON was silently projected")
			}
		})
	}
	for _, item := range []*Item{nil, {Type: "Movie"}, {Type: "Movie", IsFolder: true, Media: &media.Info{DurationTicks: 100}}} {
		if err := projectItemCredits(item, []byte(`{"StartTicks":80,"Provenance":"Manual"}`)); err != nil {
			t.Fatal(err)
		}
		if item != nil && item.Credits != nil {
			t.Fatal("credits emitted for a folder or missing media")
		}
	}
}
