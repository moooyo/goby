package subtitle

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestRenderHLSReplacesImportedMapsAndPreservesStyledCues(t *testing.T) {
	source := "WEBVTT authored captions\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:900000\n" +
		" x-timestamp-map=LOCAL:00:00:00.000,MPEGTS:180000\nKind: captions\n\n" +
		"STYLE\n::cue { color: lime; }\n\nREGION\nid:main\nwidth:80%\n\n" +
		"first\n00:01.000 --> 00:03.000 region:main\nText <00:02.000>continues\n"
	document, err := Parse([]byte(source), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	original, err := Parse([]byte(source), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderHLS(document, TicksPerSecond, 14*TicksPerSecond/10)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.Data)
	if result.ContentType != "text/vtt" || strings.Count(strings.ToUpper(text), "X-TIMESTAMP-MAP") != 1 ||
		!strings.Contains(text, "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:126000\n") {
		t.Fatalf("HLS output did not receive one measured clock: %q", text)
	}
	for _, preserved := range []string{"WEBVTT authored captions", "Kind: captions", "STYLE\n::cue { color: lime; }", "REGION\nid:main\nwidth:80%", "first\n00:02.000 --> 00:04.000 region:main", "Text <00:03.000>continues"} {
		if !strings.Contains(text, preserved) {
			t.Errorf("HLS conversion lost %q", preserved)
		}
	}
	if !reflect.DeepEqual(document, original) {
		t.Fatal("HLS rendering mutated the caller's parsed source")
	}
	native, err := Render(document, Options{Format: FormatWebVTT, PreserveSource: true})
	if err != nil || string(native.Data) != source {
		t.Fatal("HLS rendering changed the separate native subtitle response")
	}
}

func TestRenderHLSMapsSignedClockDifferencesWithoutWrapping(t *testing.T) {
	document := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: TicksPerSecond, EndTicks: 3 * TicksPerSecond, Text: "caption"}}}
	for _, test := range []struct {
		delta   int64
		mapping string
	}{
		{0, "LOCAL:00:00:00.000,MPEGTS:0"},
		{TicksPerSecond / 8, "LOCAL:00:00:00.000,MPEGTS:11250"},
		{1, "LOCAL:00:00:00.000,MPEGTS:0"},
		{-1, "LOCAL:00:00:00.001,MPEGTS:90"},
		{-TicksPerMillisecond, "LOCAL:00:00:00.001,MPEGTS:0"},
		{-TicksPerMillisecond - 1, "LOCAL:00:00:00.002,MPEGTS:90"},
		{-14_999, "LOCAL:00:00:00.002,MPEGTS:45"},
		{-TicksPerSecond - 4567, "LOCAL:00:00:01.001,MPEGTS:49"},
		{MaxOffsetTicks, "LOCAL:00:00:00.000,MPEGTS:7776000000"},
		{-MaxOffsetTicks, "LOCAL:24:00:00.000,MPEGTS:0"},
	} {
		result, err := RenderHLS(document, 0, test.delta)
		if err != nil || !strings.Contains(string(result.Data), "X-TIMESTAMP-MAP="+test.mapping+"\n") {
			t.Errorf("clock difference %d produced %q, %v", test.delta, result.Data, err)
		}
		parsed, err := Parse(result.Data, FormatWebVTT)
		if err != nil || len(parsed.Cues) != 1 || parsed.Cues[0].StartTicks != TicksPerSecond || parsed.Cues[0].EndTicks != 3*TicksPerSecond {
			t.Errorf("clock difference changed source-relative cues: %+v, %v", parsed.Cues, err)
		}
	}
}

func TestRenderHLSCaptionOffsetIsIndependentOfTransportClock(t *testing.T) {
	document := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: TicksPerSecond, EndTicks: 3 * TicksPerSecond, Text: "caption"}}}
	for _, test := range []struct {
		offset, start, end int64
		count              int
	}{
		{TicksPerSecond, 2 * TicksPerSecond, 4 * TicksPerSecond, 1},
		{-2 * TicksPerSecond, 0, TicksPerSecond, 1},
		{-4 * TicksPerSecond, 0, 0, 0},
	} {
		result, err := RenderHLS(document, test.offset, -TicksPerMillisecond)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(result.Data), "LOCAL:00:00:00.001,MPEGTS:0") {
			t.Fatal("caption delay leaked into the measured transport clock")
		}
		parsed, err := Parse(result.Data, FormatWebVTT)
		if err != nil || len(parsed.Cues) != test.count {
			t.Fatalf("offset result: %+v, %v", parsed.Cues, err)
		}
		if test.count == 1 && (parsed.Cues[0].StartTicks != test.start || parsed.Cues[0].EndTicks != test.end) {
			t.Errorf("caption delay produced %+v", parsed.Cues[0])
		}
	}
}

func TestRenderHLSTransportHeaderDoesNotRemoveCaptionText(t *testing.T) {
	document, err := Parse([]byte("WEBVTT\n\n00:01.000 --> 00:02.000\nX-TIMESTAMP-MAP is caption text\n"), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RenderHLS(document, 0, 0)
	if err != nil || !strings.Contains(string(result.Data), "\nX-TIMESTAMP-MAP is caption text\n") {
		t.Fatalf("caption text was mistaken for a transport header: %q, %v", result.Data, err)
	}
}

func TestRenderHLSRejectsUnboundedOffsetsAndHeaderGrowth(t *testing.T) {
	document := Document{Format: FormatWebVTT, Cues: []Cue{{StartTicks: TicksPerSecond, EndTicks: 2 * TicksPerSecond, Text: "caption"}}}
	for _, value := range []int64{MaxOffsetTicks + 1, -MaxOffsetTicks - 1, math.MinInt64, math.MaxInt64} {
		if _, err := RenderHLS(document, 0, value); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("unbounded clock accepted: %d, %v", value, err)
		}
		if _, err := RenderHLS(document, value, 0); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("unbounded caption delay accepted: %d, %v", value, err)
		}
	}
	// A document whose ordinary VTT fits must still reserve room for its new
	// transport header. Every caption line remains below the normal line cap.
	text := strings.Repeat("x", 65_500)
	document.Cues = make([]Cue, 257)
	for index := range document.Cues {
		document.Cues[index] = Cue{StartTicks: TicksPerSecond, EndTicks: 2 * TicksPerSecond, Text: text}
	}
	document.Cues[256].Text = "x"
	baseline, err := Render(document, Options{Format: FormatWebVTT, CopyTimestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	remainder := 1 + MaxOutputBytes - len(baseline.Data) - 2
	if remainder <= 0 || remainder > MaxLineBytes {
		t.Fatal("invalid output boundary fixture")
	}
	document.Cues[256].Text = strings.Repeat("x", remainder)
	if _, err := RenderHLS(document, 0, 0); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("mapped output bypassed its byte budget: %v", err)
	}
}
