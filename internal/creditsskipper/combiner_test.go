// SPDX-FileCopyrightText: 2026 rlauuzo
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"reflect"
	"testing"
)

// These shapes are literal golden cases from the pinned
// TestCreditsCandidateCombiner, not values produced by the Go implementation.
func TestPinnedCombinerGoldenShapes(t *testing.T) {
	cases := []struct {
		name        string
		input, want []Segment
	}{
		{"audio contains black", []Segment{{351, 448, BlackFrameSource}, {227, 446, ChromaprintSource}}, []Segment{{227, 450, CombinedSource}}},
		{"content gap", []Segment{{401, 450, BlackFrameSource}, {90, 179, ChapterSource}}, []Segment{{90, 179, ChapterSource}, {401, 450, BlackFrameSource}}},
		{"hard authored end", []Segment{{350, 438, ChapterSource}, {400, 449.5, BlackFrameSource}, {300, 436, ChromaprintSource}}, []Segment{{300, 438, CombinedSource}}},
		{"chapter never shortened", []Segment{{320, 398, ChromaprintSource}, {309, 398, ChapterSource}, {399, 450, BlackFrameSource}}, []Segment{{309, 398, CombinedSource}, {399, 450, BlackFrameSource}}},
		{"cap below minimum", []Segment{{350, 438, ChapterSource}, {430, 450, BlackFrameSource}}, []Segment{{350, 438, ChapterSource}}},
		{"trailing roll", []Segment{{150, 210, ChapterSource}, {350, 449.5, BlackFrameSource}}, []Segment{{150, 210, ChapterSource}, {350, 450, BlackFrameSource}}},
		{"adjacent chapters", []Segment{{300, 350, ChapterSource}, {350, 450, ChapterSource}}, []Segment{{300, 450, CombinedSource}}},
		{"gap over limit", []Segment{{100, 200, ChromaprintSource}, {220.5, 450, BlackFrameSource}}, []Segment{{100, 200, ChromaprintSource}, {220.5, 450, BlackFrameSource}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Combine(tc.input, 450, 15); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
func TestPinnedCombinerStrictEndExtension(t *testing.T) {
	for _, tc := range []struct{ end, want float64 }{{443, 450}, {435.5, 450}, {435, 435}, {452, 452}} {
		got := Combine([]Segment{{326, tc.end, ChromaprintSource}}, 450, 15)
		if len(got) != 1 || got[0].End != tc.want {
			t.Fatalf("end=%v: %+v", tc.end, got)
		}
	}
	for _, gap := range []float64{3, 20} {
		got := Combine([]Segment{{340 + gap, 450, BlackFrameSource}, {200, 340, ChromaprintSource}}, 450, 15)
		if !reflect.DeepEqual(got, []Segment{{200, 450, CombinedSource}}) {
			t.Fatalf("gap=%v: %+v", gap, got)
		}
	}
}
