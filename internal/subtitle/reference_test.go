package subtitle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// These captures exercise SRT input only. Native WebVTT parsing, metadata, and
// inline timestamp behavior are Goby correctness tests in subtitle_test.go.
func TestObservedSRTDeliveryBodies(t *testing.T) {
	source, _ := readReferenceBody(t, "videos-no-offset-srt")
	doc, err := Parse([]byte(source), FormatSRT)
	if err != nil {
		t.Fatal(err)
	}
	end := int64(20) * TicksPerSecond
	reversedEnd := int64(10) * TicksPerSecond
	emptyEnd := int64(50) * TicksPerSecond
	cases := []struct {
		name    string
		options Options
	}{
		{"videos-no-offset-srt", Options{Format: FormatSRT, PreserveSource: true}},
		{"vtt-get", Options{Format: FormatWebVTT}},
		{"srt-query-copy-true", Options{Format: FormatSRT, StartTicks: 10 * TicksPerSecond, EndTicks: &end, CopyTimestamps: true}},
		{"srt-query-copy-omitted", Options{Format: FormatSRT, StartTicks: 10 * TicksPerSecond, EndTicks: &end}},
		{"srt-query-copy-false", Options{Format: FormatSRT, StartTicks: 10 * TicksPerSecond, EndTicks: &end}},
		{"vtt-query-copy-true", Options{Format: FormatWebVTT, StartTicks: 10 * TicksPerSecond, EndTicks: &end, CopyTimestamps: true}},
		{"vtt-query-copy-omitted", Options{Format: FormatWebVTT, StartTicks: 10 * TicksPerSecond, EndTicks: &end}},
		{"vtt-query-copy-false", Options{Format: FormatWebVTT, StartTicks: 10 * TicksPerSecond, EndTicks: &end}},
		{"srt-path-copy-true", Options{Format: FormatSRT, StartTicks: 10 * TicksPerSecond, EndTicks: &end, CopyTimestamps: true}},
		{"vtt-path-copy-false", Options{Format: FormatWebVTT, StartTicks: 10 * TicksPerSecond, EndTicks: &end}},
		{"srt-start-only", Options{Format: FormatSRT, StartTicks: 10 * TicksPerSecond, CopyTimestamps: true}},
		{"vtt-start-only", Options{Format: FormatWebVTT, StartTicks: 10 * TicksPerSecond}},
		{"srt-path-query-conflict", Options{Format: FormatSRT, EndTicks: &end, CopyTimestamps: true}},
		{"vtt-path-query-conflict", Options{Format: FormatWebVTT, EndTicks: &end, CopyTimestamps: true}},
		{"reversed-window", Options{Format: FormatSRT, StartTicks: 20 * TicksPerSecond, EndTicks: &reversedEnd}},
		{"empty-window", Options{Format: FormatSRT, StartTicks: 40 * TicksPerSecond, EndTicks: &emptyEnd}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want, wantType := readReferenceBody(t, test.name)
			result, err := Render(doc, test.options)
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Data) != want {
				t.Fatalf("body = %q\nwant = %q", result.Data, want)
			}
			if result.ContentType != wantType {
				t.Fatalf("ContentType = %q, want %q", result.ContentType, wantType)
			}
		})
	}
}

func readReferenceBody(t *testing.T, name string) (string, string) {
	t.Helper()
	path := filepath.Join("..", "..", "tests", "compatibility", "fixtures", "reference", "emby-4.9.5.0", "subtitle-m3c-"+name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Response struct {
			Status  int        `json:"status"`
			Headers [][]string `json:"headers"`
			Body    string     `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Response.Status != 200 {
		t.Fatalf("reference %s has status %d", name, capture.Response.Status)
	}
	for _, header := range capture.Response.Headers {
		if len(header) == 2 && header[0] == "Content-Type" {
			return capture.Response.Body, header[1]
		}
	}
	t.Fatalf("reference %s has no content type", name)
	return "", ""
}
