package subtitle

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestRenderHLSWindowKeepsCompleteOverlappingCueIntervals(t *testing.T) {
	document := Document{Format: FormatSRT, Cues: []Cue{
		{StartTicks: 0, EndTicks: 2 * TicksPerSecond, Text: "ends at window"},
		{StartTicks: TicksPerSecond, EndTicks: 8 * TicksPerSecond, Text: "spans both boundaries"},
		{StartTicks: 2 * TicksPerSecond, EndTicks: 3 * TicksPerSecond, Text: "inside"},
		{StartTicks: 3 * TicksPerSecond, EndTicks: 3 * TicksPerSecond, Text: "empty interval"},
		{StartTicks: 4 * TicksPerSecond, EndTicks: 5 * TicksPerSecond, Text: "starts after window"},
	}}
	original := append([]Cue(nil), document.Cues...)
	for _, window := range [][2]int64{{2, 4}, {4, 6}} {
		result, err := RenderHLSWindow(document, window[0]*TicksPerSecond, window[1]*TicksPerSecond, 0, 14*TicksPerSecond/10)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(result.Data, FormatWebVTT)
		if err != nil || len(parsed.Cues) != 2 || parsed.Cues[0].StartTicks != TicksPerSecond || parsed.Cues[0].EndTicks != 8*TicksPerSecond ||
			!strings.Contains(string(result.Data), "MPEGTS:126000") {
			t.Fatalf("a crossing cue was clipped, omitted, or given the wrong transport clock: %+v, %v", parsed.Cues, err)
		}
	}
	if !reflect.DeepEqual(document.Cues, original) {
		t.Fatal("window generation mutated the shared source document")
	}
}

func TestRenderHLSWindowAppliesDelayBeforeSelectionAndRetainsMetadata(t *testing.T) {
	data := "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:999\n\nSTYLE\n::cue { color: lime; }\n\nREGION\nid:main\nwidth:80%\n\n" +
		"old\n00:00.000 --> 00:00.500\nOld\n\nNOTE Keep this metadata\n\n" +
		"crossing\n00:01.000 --> 00:04.000 region:main\nText <00:02.000>continues\n"
	document, err := Parse([]byte(data), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderHLSWindow(document, 3*TicksPerSecond, 4*TicksPerSecond, TicksPerSecond, -TicksPerMillisecond)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.Data)
	for _, want := range []string{"STYLE\n::cue { color: lime; }", "REGION\nid:main", "NOTE Keep this metadata", "crossing\n00:02.000 --> 00:05.000 region:main", "Text <00:03.000>continues", "LOCAL:00:00:00.001,MPEGTS:0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("a window lost caption timing or metadata %q: %s", want, text)
		}
	}
	if strings.Contains(text, "\nOld\n") || strings.Count(text, "X-TIMESTAMP-MAP") != 1 {
		t.Fatal("excluded cues or an imported transport clock leaked into the segment")
	}
	native, err := Render(document, Options{Format: FormatWebVTT, PreserveSource: true})
	if err != nil || string(native.Data) != data {
		t.Fatal("window generation changed the independently served source representation")
	}
}

func TestRenderHLSWindowEmitsEmptySegmentsAndClipsOnlyAtZero(t *testing.T) {
	document := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: TicksPerSecond, EndTicks: 4 * TicksPerSecond, Text: "caption"}}}
	for _, tc := range []struct {
		start, end, offset int64
		count              int
	}{
		{0, 1, 0, 0}, {4, 5, 0, 0}, {0, 1, -2, 1}, {10, 11, 2, 0},
	} {
		result, err := RenderHLSWindow(document, tc.start*TicksPerSecond, tc.end*TicksPerSecond, tc.offset*TicksPerSecond, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(result.Data, FormatWebVTT)
		if err != nil || len(parsed.Cues) != tc.count || !strings.HasPrefix(string(result.Data), "WEBVTT\nX-TIMESTAMP-MAP=") {
			t.Fatalf("an empty or clipped subtitle interval was not valid WebVTT: %q, %v", result.Data, err)
		}
		if tc.count == 1 && (parsed.Cues[0].StartTicks != 0 || parsed.Cues[0].EndTicks != 2*TicksPerSecond) {
			t.Fatal("window clipping changed the full shifted cue end")
		}
	}
}

func TestRenderHLSWindowRejectsOverflowAndInvalidIntervals(t *testing.T) {
	document := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: 1, EndTicks: 2, Text: "caption"}}}
	for _, values := range [][4]int64{{-1, 2, 0, 0}, {2, 2, 0, 0}, {3, 2, 0, 0}, {0, 2, MaxOffsetTicks + 1, 0}, {0, 2, 0, -MaxOffsetTicks - 1}} {
		if _, err := RenderHLSWindow(document, values[0], values[1], values[2], values[3]); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("invalid interval accepted: %v, %v", values, err)
		}
	}
	document.Cues[0].StartTicks, document.Cues[0].EndTicks = math.MaxInt64-20, math.MaxInt64-10
	for _, values := range [][2]int64{{21, 0}, {0, 21}, {5, 6}} {
		if _, err := RenderHLSWindow(document, math.MaxInt64-30, math.MaxInt64, values[0], values[1]); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("caption or mapped clock overflow accepted: %v, %v", values, err)
		}
	}
}
