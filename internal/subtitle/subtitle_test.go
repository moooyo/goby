package subtitle

import (
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestParseSRTPreservesMultilineOverlappingCues(t *testing.T) {
	source := "19\r\n00:00:01,125 --> 00:00:04,000\r\n<i>First</i>\r\nSecond line\r\n\r\n00:00:02,000 --> 00:00:03,250\r\nOverlap\r\n"
	doc, err := Parse([]byte(source), FormatSRT)
	if err != nil {
		t.Fatal(err)
	}
	want := []Cue{
		{StartTicks: 11_250_000, EndTicks: 40_000_000, Identifier: "19", Text: "<i>First</i>\nSecond line"},
		{StartTicks: 20_000_000, EndTicks: 32_500_000, Text: "Overlap"},
	}
	if !reflect.DeepEqual(doc.Cues, want) {
		t.Fatalf("cues = %#v, want %#v", doc.Cues, want)
	}
	result, err := Render(doc, Options{Format: FormatWebVTT})
	if err != nil {
		t.Fatal(err)
	}
	wantText := "WEBVTT\n\n00:01.125 --> 00:04.000\n<i>First</i>\nSecond line\n\n00:02.000 --> 00:03.250\nOverlap\n"
	if string(result.Data) != wantText || result.ContentType != "text/vtt" {
		t.Fatalf("result = %#v, want %q", result, wantText)
	}
}

func TestWebVTTMetadataIsNotCaptionText(t *testing.T) {
	source := "WEBVTT Example\nKind: captions\n\nSTYLE\n::cue { color: lime; }\n\nREGION\nid:bottom\nwidth:50%\n\nNOTE This is a comment\n00:00.000 --> 00:59.000\nDo not display this\n\nspeaker\n00:01.000 --> 00:03.500 align:start position:10%\n<v Ada><i>Hello</i>\nWorld\n\nNOTE trailing comment\n"
	doc, err := Parse([]byte(source), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 1 || doc.Cues[0].Identifier != "speaker" || doc.Cues[0].Settings != "align:start position:10%" || doc.Cues[0].Text != "<v Ada><i>Hello</i>\nWorld" {
		t.Fatalf("unexpected cues: %#v", doc.Cues)
	}
	srt, err := Render(doc, Options{Format: FormatSRT})
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n00:00:01,000 --> 00:00:03,500\n<v Ada><i>Hello</i>\nWorld\n\n"
	if string(srt.Data) != want {
		t.Fatalf("SRT = %q, want %q", srt.Data, want)
	}
	vtt, err := Render(doc, Options{Format: FormatWebVTT})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"WEBVTT Example\nKind: captions", "STYLE\n::cue", "REGION\nid:bottom", "NOTE This is a comment", "NOTE trailing comment"} {
		if !strings.Contains(string(vtt.Data), fragment) {
			t.Errorf("WebVTT lost metadata %q", fragment)
		}
	}
	again, err := Parse(vtt.Data, FormatWebVTT)
	if err != nil || !reflect.DeepEqual(again.Cues, doc.Cues) {
		t.Fatalf("round trip = %#v, %v", again.Cues, err)
	}
}

func TestWebVTTInlineTimestampsAreNotSRTCaptionText(t *testing.T) {
	doc, err := Parse([]byte("WEBVTT\n\n00:01.000 --> 00:04.000\n<i>First <00:02.500>Second</i>"), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	srt, err := Render(doc, Options{Format: FormatSRT})
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n00:00:01,000 --> 00:00:04,000\n<i>First Second</i>\n\n"
	if string(srt.Data) != want {
		t.Fatalf("SRT = %q, want %q", srt.Data, want)
	}
	vtt, err := Render(doc, Options{Format: FormatWebVTT})
	if err != nil || !strings.Contains(string(vtt.Data), "<i>First <00:02.500>Second</i>") {
		t.Fatalf("WebVTT = %q, %v", vtt.Data, err)
	}
}

func TestWebVTTInlineTimestampsFollowCueOffset(t *testing.T) {
	doc, err := Parse([]byte("WEBVTT\n\n00:12.000 --> 00:15.000\n<i>A<00:12.500>B<00:14.750>C</i>"), FormatWebVTT)
	if err != nil {
		t.Fatal(err)
	}
	end := int64(6) * TicksPerSecond
	result, err := Render(doc, Options{StartTicks: 10 * TicksPerSecond, EndTicks: &end})
	if err != nil {
		t.Fatal(err)
	}
	want := "WEBVTT\n\n00:02.000 --> 00:05.000\n<i>A<00:02.500>B<00:04.750>C</i>\n"
	if string(result.Data) != want {
		t.Fatalf("offset WebVTT = %q, want %q", result.Data, want)
	}
	result, err = Render(doc, Options{StartTicks: 10 * TicksPerSecond, CopyTimestamps: true})
	if err != nil || !strings.Contains(string(result.Data), "00:12.000 --> 00:15.000\n<i>A<00:12.500>B<00:14.750>C</i>") {
		t.Fatalf("copied WebVTT = %q, %v", result.Data, err)
	}
}

func TestStrictUnicodeDecoding(t *testing.T) {
	source := "1\r\n00:00:01,000 --> 00:00:02,000\r\n\u4f60\u597d \U0001f642\r\n"
	valid := map[string][]byte{
		"utf8":     []byte(source),
		"utf8_bom": append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...),
		"utf16_le": utf16Source(source, binary.LittleEndian),
		"utf16_be": utf16Source(source, binary.BigEndian),
	}
	for name, data := range valid {
		t.Run(name, func(t *testing.T) {
			doc, err := Parse(data, FormatSRT)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Cues) != 1 || doc.Cues[0].Text != "\u4f60\u597d \U0001f642" {
				t.Fatalf("cues = %#v", doc.Cues)
			}
			result, err := Render(doc, Options{PreserveSource: true})
			wantSource := source
			if name == "utf8_bom" {
				wantSource = "\ufeff" + source
			}
			if err != nil || !utf8.Valid(result.Data) || string(result.Data) != wantSource {
				t.Fatalf("UTF-8 preserved source = %q, %v", result.Data, err)
			}
		})
	}
	invalid := map[string][]byte{
		"legacy_without_bom":           []byte{'c', 'a', 'f', 0xe9},
		"truncated_utf8":               {0xe4, 0xbd},
		"odd_utf16":                    {0xff, 0xfe, 0x41},
		"high_surrogate_at_end":        {0xff, 0xfe, 0x00, 0xd8},
		"high_surrogate_before_letter": {0xfe, 0xff, 0xd8, 0x00, 0x00, 0x41},
		"low_surrogate":                {0xff, 0xfe, 0x00, 0xdc},
		"utf16_without_bom":            {0x31, 0, 0x0a, 0},
		"utf32_le":                     {0xff, 0xfe, 0, 0, 0x31, 0, 0, 0},
		"utf32_be":                     {0, 0, 0xfe, 0xff, 0, 0, 0, 0x31},
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(data, FormatSRT)
			if !errors.Is(err, ErrInvalidEncoding) {
				t.Fatalf("error = %v, want ErrInvalidEncoding", err)
			}
		})
	}
}

func TestMalformedDocuments(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		text   string
	}{
		{"missing_vtt_header", FormatWebVTT, "00:00.000 --> 00:01.000\nText"},
		{"invalid_vtt_header", FormatWebVTT, "WEBVTTinvalid\n\n00:00.000 --> 00:01.000\nText"},
		{"missing_header_separator", FormatWebVTT, "WEBVTT\n00:00.000 --> 00:01.000\nText"},
		{"markup_is_not_a_document", FormatWebVTT, "WEBVTT\n\n<script>arbitrary markup</script>"},
		{"style_after_cue", FormatWebVTT, "WEBVTT\n\n00:00.000 --> 00:01.000\nText\n\nSTYLE\n::cue { color: red; }"},
		{"timing_in_region", FormatWebVTT, "WEBVTT\n\nREGION\n00:00.000 --> 00:01.000"},
		{"invalid_setting", FormatWebVTT, "WEBVTT\n\n00:00.000 --> 00:01.000 unexpected\nText"},
		{"empty_setting_name", FormatWebVTT, "WEBVTT\n\n00:00.000 --> 00:01.000 :start\nText"},
		{"empty_setting_value", FormatWebVTT, "WEBVTT\n\n00:00.000 --> 00:01.000 align:\nText"},
		{"srt_nondecimal_index", FormatSRT, "caption-name\n00:00:00,000 --> 00:00:01,000\nText"},
		{"missing_timing", FormatSRT, "1\nText"},
		{"reversed_timing", FormatSRT, "00:00:02,000 --> 00:00:01,000\nText"},
		{"multiple_arrows", FormatSRT, "00:00:00,000 --> --> 00:00:01,000\nText"},
		{"srt_unknown_timing_suffix", FormatSRT, "00:00:00,000 --> 00:00:01,000 unsafe\nText"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.text), test.format)
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("error = %v, want ErrInvalidDocument", err)
			}
		})
	}
}

func TestTimestampBounds(t *testing.T) {
	invalid := []string{
		"-1:00:00,000", "00:60:00,000", "00:00:60,000", "00:0:00,000",
		"00:00:00,00", "00:00:00.000", "00:00,000", "00:00:00,0000",
		"999999999999999999999999:00:00,000", "256204778:48:05,478",
	}
	for _, stamp := range invalid {
		t.Run(stamp, func(t *testing.T) {
			_, err := Parse([]byte(stamp+" --> "+stamp+"\nText"), FormatSRT)
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("timestamp %q error = %v", stamp, err)
			}
		})
	}
	maximum := "256204778:48:05,477"
	doc, err := Parse([]byte(maximum+" --> "+maximum+"\nText"), FormatSRT)
	if err != nil || doc.Cues[0].StartTicks != math.MaxInt64/TicksPerMillisecond*TicksPerMillisecond {
		t.Fatalf("maximum supported timestamp = %#v, %v", doc.Cues, err)
	}
	result, err := Render(doc, Options{})
	if err != nil || !strings.Contains(string(result.Data), maximum+" --> "+maximum) {
		t.Fatalf("maximum timestamp render = %q, %v", result.Data, err)
	}
}

func TestParserResourceLimits(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"input", strings.Repeat(" ", MaxInputBytes+1)},
		{"line_count", strings.Repeat("\n", MaxLineCount)},
		{"line_length", "00:00:00,000 --> 00:00:01,000\n" + strings.Repeat("a", MaxLineBytes+1)},
		{"cue_text", "00:00:00,000 --> 00:00:01,000\n" + strings.Repeat(strings.Repeat("a", MaxLineBytes)+"\n", 5)},
		{"cue_count", strings.Repeat("00:00:00,000 --> 00:00:01,000\nA\n\n", MaxCueCount+1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.text), FormatSRT)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("error = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestPreserveSourceRequiresUnmodifiedSameFormatDocument(t *testing.T) {
	source := "27\r\n00:00:01,000  -->  00:00:02,000\r\nText"
	doc, err := Parse([]byte(source), FormatSRT)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Render(doc, Options{Format: FormatSRT, PreserveSource: true})
	if err != nil || string(result.Data) != source {
		t.Fatalf("preserved output = %q, %v", result.Data, err)
	}
	doc.Cues[0].Text = "Edited"
	result, err = Render(doc, Options{Format: FormatSRT, PreserveSource: true})
	if err != nil || strings.Contains(string(result.Data), "Text") || !strings.Contains(string(result.Data), "Edited") {
		t.Fatalf("edited output = %q, %v", result.Data, err)
	}
	result, err = Render(doc, Options{Format: FormatWebVTT, PreserveSource: true})
	if err != nil || !strings.HasPrefix(string(result.Data), "WEBVTT\n\n") || strings.ContainsRune(string(result.Data), '\r') {
		t.Fatalf("converted output = %q, %v", result.Data, err)
	}
}

func TestRenderRejectsInvalidRangesAndConstructedCues(t *testing.T) {
	doc := Document{Format: FormatSRT, Cues: []Cue{{StartTicks: 0, EndTicks: TicksPerSecond, Text: "Text"}}}
	negative := int64(-1)
	for _, options := range []Options{{StartTicks: -1}, {EndTicks: &negative}} {
		if _, err := Render(doc, options); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("options %#v: error = %v", options, err)
		}
	}
	for _, cue := range []Cue{
		{StartTicks: -1, EndTicks: 0, Text: "Text"},
		{StartTicks: 2, EndTicks: 1, Text: "Text"},
		{Text: "Text", Identifier: "id\n00:00.000 --> 00:01.000"},
		{Text: "Text", Identifier: "NOTE hidden"},
		{Text: "Text", Settings: "align:start\nInjected"},
		{Text: "First\n\nSecond"},
	} {
		if _, err := Render(Document{Format: FormatSRT, Cues: []Cue{cue}}, Options{}); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("cue %#v: error = %v", cue, err)
		}
	}
}

func TestSupportedFormatsAreExplicit(t *testing.T) {
	for name, want := range map[string]Format{"SRT": FormatSRT, " subrip ": FormatSRT, "WebVTT": FormatWebVTT, "vtt": FormatWebVTT} {
		got, err := NormalizeFormat(name)
		if err != nil || got != want {
			t.Errorf("NormalizeFormat(%q) = %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"ass", "ssa", "ttml", "", "sub"} {
		if _, err := Parse(nil, Format(name)); !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("format %q error = %v", name, err)
		}
	}
}

func utf16Source(source string, order binary.ByteOrder) []byte {
	units := utf16.Encode([]rune(source))
	data := make([]byte, 2+len(units)*2)
	order.PutUint16(data, 0xfeff)
	for index, unit := range units {
		order.PutUint16(data[2+index*2:], unit)
	}
	return data
}
