package library

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestExplicitChapterIntro(t *testing.T) {
	for _, test := range []struct {
		name     string
		chapters []media.Chapter
		want     *IntroInterval
	}{
		{"absent", nil, nil},
		{"ordinary opening", []media.Chapter{{StartTicks: 0, EndTicks: 20, Title: "Opening"}}, nil},
		{"ordinary intro title", []media.Chapter{{StartTicks: 0, EndTicks: 20, Title: "Intro"}}, nil},
		{"explicit", []media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 20, Title: "IntroEnd"}}, &IntroInterval{0, 20, "Chapter"}},
		{"missing end", []media.Chapter{{StartTicks: 0, Title: "IntroStart"}}, nil},
		{"reversed", []media.Chapter{{StartTicks: 20, Title: "IntroStart"}, {StartTicks: 10, Title: "IntroEnd"}}, nil},
		{"empty", []media.Chapter{{StartTicks: 20, Title: "IntroStart"}, {StartTicks: 20, Title: "IntroEnd"}}, nil},
		{"negative", []media.Chapter{{StartTicks: -1, Title: "IntroStart"}, {StartTicks: 20, Title: "IntroEnd"}}, nil},
		{"past duration", []media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 101, Title: "IntroEnd"}}, nil},
		{"overlapping pairs", []media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 10, Title: "IntroStart"}, {StartTicks: 20, Title: "IntroEnd"}, {StartTicks: 30, Title: "IntroEnd"}}, nil},
		{"multiple pairs", []media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 10, Title: "IntroEnd"}, {StartTicks: 20, Title: "IntroStart"}, {StartTicks: 30, Title: "IntroEnd"}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ExplicitChapterIntro(&media.Info{DurationTicks: 100, Chapters: test.chapters}); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("intro = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestProjectItemIntroRequiresMovieOrEpisodeAndValidOverride(t *testing.T) {
	for _, kind := range []string{"Movie", "Episode", "Audio", "Video", "Series"} {
		item := Item{Type: kind, Media: &media.Info{DurationTicks: 100, Chapters: []media.Chapter{{StartTicks: 0, Title: "IntroStart"}, {StartTicks: 20, Title: "IntroEnd"}}}}
		if err := projectItemIntro(&item, []byte(`{"StartTicks":10,"EndTicks":30,"Provenance":"Manual"}`)); err != nil {
			t.Fatal(err)
		}
		if kind == "Movie" || kind == "Episode" {
			if item.Intro == nil || item.Intro.StartTicks != 10 || item.Intro.Provenance != "Manual" {
				t.Fatalf("manual override missing for %s", kind)
			}
			if err := projectItemIntro(&item, []byte(`{"StartTicks":10,"EndTicks":130,"Provenance":"Manual"}`)); err != nil {
				t.Fatal(err)
			}
			if item.Intro == nil || item.Intro.Provenance != "Chapter" {
				t.Fatal("invalid override displaced valid source facts")
			}
		} else if item.Intro != nil {
			t.Fatalf("intro emitted for %s", kind)
		}
	}
}
