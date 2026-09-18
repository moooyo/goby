package subtitle

import (
	"errors"
	"strings"
	"testing"
)

func TestASSRetainsDocumentSectionsWhenTimingChanges(t *testing.T) {
	source := "\ufeff[Script Info]\r\nTitle: Styled subtitles\r\nScriptType: v4.00+\r\n" +
		"PlayResX: 1920\r\nPlayResY: 1080\r\n\r\n[V4+ Styles]\r\n" +
		"Format: Name, Fontname, Fontsize, PrimaryColour, Bold, Italic, Underline\r\n" +
		"Style: Default,Example Font,40,&H00FFFFFF,0,-1,0\r\n\r\n[Events]\r\n" +
		"Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\r\n" +
		"Comment: 0,0:00:00.00,0:00:10.00,Default,,0,0,0,,Keep comment\r\n" +
		"Dialogue: 2, 0:00:01.25 ,\t0:00:03.50 ,Default,Speaker,0010,0020,0030,Scroll up,{\\pos(50,60)}Hello, world\\NNext line\r\n" +
		"\r\n[Fonts]\r\nfontname: Embedded_0.ttf\r\n!!AABBCC\r\n\r\n[Graphics]\r\nfilename: graphic.bmp\r\n!!XXYYZZ\r\n"
	doc, err := Parse([]byte(source), FormatASS)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 1 || doc.Cues[0].StartTicks != 12_500_000 || doc.Cues[0].Text != `{\pos(50,60)}Hello, world\NNext line` {
		t.Fatalf("unexpected cues: %#v", doc.Cues)
	}
	result, err := Render(doc, Options{})
	if err != nil || string(result.Data) != source {
		t.Fatalf("unchanged ASS = %q, %v", result.Data, err)
	}
	result, err = Render(doc, Options{OffsetTicks: 15_000_000})
	want := strings.Replace(source, " 0:00:01.25 ,\t0:00:03.50 ", " 0:00:02.75 ,\t0:00:05.00 ", 1)
	if err != nil || string(result.Data) != want {
		t.Fatalf("shifted ASS = %q, %v; want %q", result.Data, err, want)
	}
}

func TestASSFormatColumnOrderAndCommaText(t *testing.T) {
	source := "[Script Info]\n[Events]\nFormat: Text, End, Layer, Start, Style\n" +
		"Dialogue: Hello, comma,0:00:05.00,4,0:00:02.00,Default\n" +
		"Format: End, Text, Start\nDialogue: 0:00:08.00,More, commas,0:00:06.00\n"
	doc, err := Parse([]byte(source), FormatASS)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cues) != 2 || doc.Cues[0].Text != " Hello, comma" || doc.Cues[1].Text != "More, commas" {
		t.Fatalf("unexpected cues: %#v", doc.Cues)
	}
	result, err := Render(doc, Options{OffsetTicks: TicksPerSecond})
	want := strings.NewReplacer("0:00:05.00", "0:00:06.00", "0:00:02.00", "0:00:03.00", "0:00:08.00", "0:00:09.00", "0:00:06.00", "0:00:07.00").Replace(source)
	if err != nil || string(result.Data) != want {
		t.Fatalf("reordered ASS = %q, %v; want %q", result.Data, err, want)
	}
}

func TestASSSelectionPreservesNonDialogueRecords(t *testing.T) {
	source := "[Script Info]\n[Events]\nFormat: Start, End, Text\n" +
		"Dialogue: 0:00:01.00,0:00:02.00,Early\n" +
		"; A comment between dialogue records\n" +
		"Dialogue: 0:00:04.00,0:00:07.00,Late\n" +
		"[Fonts]\nfontname: Example.ttf\n!!DATA\n"
	doc, err := Parse([]byte(source), FormatASS)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Render(doc, Options{StartTicks: 3 * TicksPerSecond})
	want := strings.Replace(source, "Dialogue: 0:00:01.00,0:00:02.00,Early\n", "", 1)
	want = strings.Replace(want, "0:00:04.00,0:00:07.00,Late", "0:00:01.00,0:00:04.00,Late", 1)
	if err != nil || string(result.Data) != want {
		t.Fatalf("filtered ASS = %q, %v; want %q", result.Data, err, want)
	}
}

func TestSSADialogueAndLegacyStyles(t *testing.T) {
	source := "[Script Info]\nScriptType: v4.00\n[V4 Styles]\n" +
		"Format: Name, Fontname, PrimaryColour, Bold, Italic\nStyle: Default,Arial,255,-1,0\n" +
		"[Events]\nFormat: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: Marked=0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Red and bold\n"
	doc, err := Parse([]byte(source), FormatSSA)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Render(doc, Options{PreserveSource: true})
	if err != nil || string(result.Data) != source {
		t.Fatalf("SSA preservation = %q, %v", result.Data, err)
	}
	result, err = Render(doc, Options{Format: FormatSRT})
	if err != nil || !strings.Contains(string(result.Data), `<font color="#FF0000"><b>Red and bold</b></font>`) {
		t.Fatalf("SSA style conversion = %q, %v", result.Data, err)
	}
}

func TestASSConvertsBasicOverridesAndOmitsDrawingCommands(t *testing.T) {
	source := "[Script Info]\n[V4+ Styles]\nFormat: Name, Bold, Italic, Underline\nStyle: Default,0,-1,0\n" +
		"[Events]\nFormat: Start, End, Style, Text\n" +
		"Dialogue: 0:00:01.00,0:00:05.00,Default,Styled{\\i0\\b1} bold{\\b0}\\NNew\\hline{\\p1}m 0 0 l 20 20{\\p0}End\n"
	doc, err := Parse([]byte(source), FormatASS)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []Format{FormatSRT, FormatWebVTT} {
		result, err := Render(doc, Options{Format: format})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(result.Data), "<i>Styled</i><b> bold</b>\nNew\u00a0lineEnd") || strings.Contains(string(result.Data), "m 0 0") {
			t.Fatalf("%s conversion = %q", format, result.Data)
		}
	}
}

func TestSRTAndWebVTTConvertToASSAndSSA(t *testing.T) {
	sources := map[Format]string{
		FormatSRT:    "1\n00:00:01,250 --> 00:00:03,500\n<i>Hello</i>\n<b>World</b>\n",
		FormatWebVTT: "WEBVTT\n\n00:01.250 --> 00:03.500\n<i>Hello</i>\n<b>World</b>\n",
	}
	for sourceFormat, source := range sources {
		for _, outputFormat := range []Format{FormatASS, FormatSSA} {
			doc, err := Parse([]byte(source), sourceFormat)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Render(doc, Options{Format: outputFormat})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(result.Data), `0:00:01.25,0:00:03.50,Default,,0,0,0,,{\i1}Hello{\i0}\N{\b1}World{\b0}`) {
				t.Fatalf("%s to %s = %q", sourceFormat, outputFormat, result.Data)
			}
			again, err := Parse(result.Data, outputFormat)
			if err != nil || len(again.Cues) != 1 || again.Cues[0].StartTicks != doc.Cues[0].StartTicks || again.Cues[0].EndTicks != doc.Cues[0].EndTicks {
				t.Fatalf("%s round trip = %#v, %v", outputFormat, again.Cues, err)
			}
		}
	}
}

func TestASSRejectsMalformedEvents(t *testing.T) {
	cases := []string{
		"[Events]\nFormat: Start, End, Text\n",
		"[Script Info]\n",
		"[Script Info]\n[Events]\nDialogue: 0:00:01.00,0:00:02.00,Missing format\n",
		"[Script Info]\n[Events]\nFormat: Start, Start, End, Text\n",
		"[Script Info]\n[Events]\nFormat: Start, End\n",
		"[Script Info]\n[Events]\nFormat: Start, End, Text\nDialogue: 0:00:01.00,Missing end\n",
		"[Script Info]\n[Events]\nFormat: Start, End, Text\nDialogue: 0:00:03.00,0:00:02.00,Reversed\n",
		"[Script Info]\n[Events]\nFormat: Start, End, Text\nDialogue: 0:60:00.00,1:00:02.00,Invalid minutes\n",
		"[Script Info]\n[Events]\nFormat: Start, End, Text\nDialogue: 0:00:01.000,0:00:02.00,Wrong precision\n",
	}
	for _, source := range cases {
		if _, err := Parse([]byte(source), FormatASS); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("Parse(%q) error = %v", source, err)
		}
	}
}
