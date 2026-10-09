//go:build linux

package diagnostics

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestHandlerPrivateEncoderPersistsOnlyBoundedJSONLines(t *testing.T) {
	store := testStore(t, Config{})
	var fallback bytes.Buffer
	handler := NewHandler(store, slog.NewJSONHandler(&fallback, nil))
	calls := 0
	attrs := make([]slog.Attr, 1000)
	for index := range attrs {
		attrs[index] = slog.String("route", "GET /emby/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{SubtitleFileName}")
	}
	records := []slog.Record{
		diagnosticTestRecord("forged\n{}\r\x00\"record", slog.String("request_id", "forged\n{}")),
		diagnosticTestRecord("request completed", slog.String("route", "/path\n{}\r\x00"), slog.Int("status", 200)),
		diagnosticTestRecord("transcode failed", slog.Any("request_id", diagnosticPanicValue{&calls}),
			slog.Any("item_id", diagnosticPanicMarshaler{&calls}), slog.Any("error", diagnosticPanicError{&calls})),
		diagnosticTestRecord("request completed", attrs...),
	}
	for _, record := range records {
		fallback.Reset()
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		diagnosticJSON(t, fallback.Bytes())
	}
	if calls != 0 {
		t.Fatal("the trusted encoder invoked an arbitrary input method")
	}
	item := firstTestFile(t, store)
	lines, err := store.Lines(context.Background(), item.Name, LinesOptions{Limit: 100})
	if err != nil || len(lines.Items) != len(records) {
		t.Fatalf("private encoding did not persist exactly one line per input record: %v", err)
	}
	for _, line := range lines.Items {
		diagnosticJSON(t, append([]byte(line), '\n'))
		if strings.Contains(line, "forged") || strings.ContainsAny(line, "\r\x00") {
			t.Fatal("an external value escaped the private encoding boundary")
		}
	}
}
