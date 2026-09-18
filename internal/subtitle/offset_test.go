package subtitle

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestSignedOffsetAppliesToEverySubtitleFormat(t *testing.T) {
	for _, format := range []Format{FormatSRT, FormatWebVTT, FormatASS, FormatSSA} {
		doc := Document{Format: format, Cues: []Cue{
			{StartTicks: TicksPerSecond, EndTicks: 2 * TicksPerSecond, Text: "First"},
			{StartTicks: 3 * TicksPerSecond, EndTicks: 5 * TicksPerSecond, Text: "Second"},
		}}
		for _, offset := range []int64{-4 * TicksPerSecond, -2 * TicksPerSecond, TicksPerSecond} {
			result, err := Render(doc, Options{OffsetTicks: offset})
			if err != nil {
				t.Fatal(err)
			}
			again, err := Parse(result.Data, format)
			if err != nil {
				t.Fatal(err)
			}
			if offset < 0 {
				if len(again.Cues) != 1 || again.Cues[0].Text != "Second" {
					t.Fatalf("%s offset %d: %#v", format, offset, again.Cues)
				}
				wantStart := int64(3)*TicksPerSecond + offset
				if wantStart < 0 {
					wantStart = 0
				}
				if again.Cues[0].StartTicks != wantStart || again.Cues[0].EndTicks != 5*TicksPerSecond+offset {
					t.Fatalf("%s clipped offset %d: %#v", format, offset, again.Cues)
				}
			} else if len(again.Cues) != 2 || again.Cues[0].StartTicks != 2*TicksPerSecond || again.Cues[1].EndTicks != 6*TicksPerSecond {
				t.Fatalf("%s delayed cues: %#v", format, again.Cues)
			}
		}
	}
}

func TestSignedOffsetRetainsLegacySelectionOrder(t *testing.T) {
	doc := Document{Format: FormatSRT, Cues: []Cue{
		{StartTicks: 5 * TicksPerSecond, EndTicks: 8 * TicksPerSecond, Text: "Before selection"},
		{StartTicks: 12 * TicksPerSecond, EndTicks: 15 * TicksPerSecond, Text: "Selected"},
		{StartTicks: 13 * TicksPerSecond, EndTicks: 16 * TicksPerSecond, Text: "At exclusive end"},
	}}
	end := int64(4) * TicksPerSecond
	result, err := Render(doc, Options{StartTicks: 10 * TicksPerSecond, EndTicks: &end, OffsetTicks: TicksPerSecond})
	want := "1\n00:00:03,000 --> 00:00:06,000\nSelected\n\n"
	if err != nil || string(result.Data) != want {
		t.Fatalf("legacy selection with offset = %q, %v; want %q", result.Data, err, want)
	}
	result, err = Render(doc, Options{StartTicks: 10 * TicksPerSecond, CopyTimestamps: true, OffsetTicks: -TicksPerSecond})
	if err != nil || !strings.Contains(string(result.Data), "00:00:11,000 --> 00:00:14,000") || strings.Contains(string(result.Data), "Before selection") {
		t.Fatalf("copied timestamps with offset = %q, %v", result.Data, err)
	}
}

func TestSignedOffsetMovesWebVTTInlineTimestamps(t *testing.T) {
	doc, err := Parse([]byte("WEBVTT\n\n00:01.000 --> 00:04.000\nA<00:02.500>B<00:03.500>C"), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Render(doc, Options{OffsetTicks: -2 * TicksPerSecond, PreserveSource: true})
	want := "WEBVTT\n\n00:00.000 --> 00:02.000\nA<00:00.500>B<00:01.500>C\n"
	if err != nil || string(result.Data) != want {
		t.Fatalf("inline negative offset = %q, %v; want %q", result.Data, err, want)
	}
	result, err = Render(doc, Options{OffsetTicks: TicksPerSecond})
	if err != nil || !strings.Contains(string(result.Data), "A<00:03.500>B<00:04.500>C") {
		t.Fatalf("inline positive offset = %q, %v", result.Data, err)
	}
}

func TestSignedOffsetBoundsAndOverflow(t *testing.T) {
	doc := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: TicksPerSecond, EndTicks: 2 * TicksPerSecond, Text: "Text"}}}
	for _, offset := range []int64{-MaxOffsetTicks - 1, MaxOffsetTicks + 1, math.MinInt64, math.MaxInt64} {
		if _, err := Render(doc, Options{OffsetTicks: offset}); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("offset %d error = %v, want ErrInvalidRange", offset, err)
		}
	}
	for _, offset := range []int64{-MaxOffsetTicks, MaxOffsetTicks} {
		if _, err := Render(doc, Options{OffsetTicks: offset}); err != nil {
			t.Errorf("bounded offset %d error = %v", offset, err)
		}
	}
	doc.Cues[0].StartTicks = math.MaxInt64 - TicksPerSecond
	doc.Cues[0].EndTicks = math.MaxInt64
	if _, err := Render(doc, Options{OffsetTicks: 1}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("overflow error = %v, want ErrInvalidRange", err)
	}
}
