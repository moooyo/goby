// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"reflect"
	"testing"
)

func TestPinnedDefaultChapterExpression(t *testing.T) {
	// In "Credits : Ending", the first keyword fails the negative lookahead,
	// but Regex.IsMatch finds a new valid match at the later "Ending" keyword.
	for _, name := range []string{"End Credits", "Ending", "Credit start", "Closing Credits", "Credits", "Credits:", "ED", "Outro", "[SponsorBlock]: endcards/credits", "\u3000Ending\u3000", "Credits : Ending"} {
		got := FindChapterCandidates([]Chapter{{"Cold Open", 0}, {"Introduction", 60}, {"Main Episode", 90}, {name, 1890}}, 2000, false, DefaultOptions())
		if !reflect.DeepEqual(got, []Segment{{1890, 2000, ChapterSource}}) {
			t.Fatalf("name=%q: %+v", name, got)
		}
	}
	for _, name := range []string{"Ending End", "Credits: End", "Credits : End", "Friend", "bED", "Endings", "CreditsEnd", "[SponsorBlock]: preview/recap"} {
		if DefaultChapterMatches(name, true) {
			t.Fatalf("unexpected chapter match %q", name)
		}
	}
}
func TestPinnedChapterReverseOrderAdjacentPolicyAndMovieLimit(t *testing.T) {
	got := FindChapterCandidates([]Chapter{{"Content", 0}, {"Ending", 100}, {"Credits", 150}, {"Preview", 200}}, 240, false, DefaultOptions())
	if !reflect.DeepEqual(got, []Segment{{100, 150, ChapterSource}}) {
		t.Fatalf("adjacent chapter ambiguity: %+v", got)
	}
	chapters := []Chapter{{"Content", 0}, {"Credits", 1000}}
	if got := FindChapterCandidates(chapters, 1700, false, DefaultOptions()); len(got) != 0 {
		t.Fatalf("episode exceeded 450 seconds: %+v", got)
	}
	if got := FindChapterCandidates(chapters, 1700, true, DefaultOptions()); !reflect.DeepEqual(got, []Segment{{1000, 1700, ChapterSource}}) {
		t.Fatalf("movie did not use 900 seconds: %+v", got)
	}
	if got := Window(1200, false); got != (Range{750, 1200}) {
		t.Fatal(got)
	}
	if got := Window(1200, true); got != (Range{300, 1200}) {
		t.Fatal(got)
	}
	if got := Window(120, true); got != (Range{0, 120}) {
		t.Fatal(got)
	}
}
