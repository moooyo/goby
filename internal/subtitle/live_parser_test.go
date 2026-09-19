package subtitle

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func liveParserEvent(t *testing.T, events <-chan LiveBatch) LiveBatch {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("parser waited for EOF instead of emitting a complete block")
		return LiveBatch{}
	}
}

func TestLiveWebVTTEmitsSparseHeaderAndCueBeforeEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	events := make(chan LiveBatch, 3)
	done := make(chan error, 1)
	stop := errors.New("consumer has enough data")
	go func() {
		done <- ReadLiveWebVTT(reader, LiveParserOptions{}, func(batch LiveBatch) error {
			events <- batch
			if !batch.HeaderOnly {
				return stop
			}
			return nil
		})
	}()
	if _, err := io.WriteString(writer, "\ufeffWEBVTT\r\nX-TIMESTAMP-MAP=LOCAL:00:00:01.000,MPEGTS:8589934591\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	header := liveParserEvent(t, events)
	if !header.HeaderOnly || len(header.Document.Cues) != 0 || header.Header.TimestampMap == nil || header.Header.TimestampMap.MPEGTS != 1<<33-1 || header.Header.TimestampMap.LocalTicks != TicksPerSecond {
		t.Fatalf("sparse header or raw timestamp mapping lost: %+v", header)
	}
	header.Header.TimestampMap.MPEGTS = 1
	header.Header.Lines[0] = "caller change"
	if _, err := io.WriteString(writer, "id\r\n00:02.000 --> 00:05.000 align:start\r\nFirst line\r\nSecond line\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	batch := liveParserEvent(t, events)
	if batch.HeaderOnly || len(batch.Document.Cues) != 1 || batch.Document.Cues[0].Text != "First line\nSecond line" || batch.Header.TimestampMap.MPEGTS != 1<<33-1 || batch.Header.Lines[0] != "WEBVTT" {
		t.Fatalf("cue callback lost detached complete data: %+v", batch)
	}
	select {
	case err := <-done:
		if !errors.Is(err, stop) {
			t.Fatalf("callback cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback cancellation did not stop the reader")
	}
}

func TestLiveWebVTTBoundsUnitsInsteadOfStreamLifetime(t *testing.T) {
	var source strings.Builder
	source.WriteString("WEBVTT\n\n")
	for index := range 1000 {
		fmt.Fprintf(&source, "%02d:%02d.000 --> %02d:%02d.500\ntext\n\n", index/60, index%60, index/60, index%60)
	}
	cues, headers := 0, 0
	err := ReadLiveWebVTT(strings.NewReader(source.String()), LiveParserOptions{MaxHeaderBytes: 16, MaxBlockBytes: 64, MaxLineBytes: 48}, func(batch LiveBatch) error {
		if batch.Header.TimestampMap != nil {
			t.Fatal("missing timestamp map became an implicit zero mapping")
		}
		if batch.HeaderOnly {
			headers++
		} else {
			cues += len(batch.Document.Cues)
		}
		return nil
	})
	if err != nil || cues != 1000 || headers != 1 {
		t.Fatalf("long bounded stream = %d cues, %d headers, %v", cues, headers, err)
	}
}

func TestLiveWebVTTReportsOnlyTheCurrentNoteWithoutInterpretingIt(t *testing.T) {
	var source strings.Builder
	source.WriteString("WEBVTT\n\nSTYLE\n::cue { color: white; }\n\n")
	for index := range 1000 {
		fmt.Fprintf(&source, "NOTE item %d\nnot retained\n\n", index)
	}
	source.WriteString("NOTE GOBY-WATERMARK not-a-timestamp\n\n00:00.000 --> 00:01.000\nNOTE GOBY-WATERMARK is caption text\n\n")
	notes, cues := 0, 0
	err := ReadLiveWebVTT(strings.NewReader(source.String()), LiveParserOptions{MaxMetadataBytes: 64}, func(batch LiveBatch) error {
		if batch.Note != "" {
			notes++
			if !batch.HeaderOnly || len(batch.Document.Cues) != 0 || len(batch.Document.blocks) != 2 {
				t.Fatal("NOTE delivery accumulated unrelated history or became a cue")
			}
		}
		if len(batch.Document.Cues) != 0 {
			cues++
			if batch.Note != "" || len(batch.Document.blocks) != 1 || batch.Document.Cues[0].Text != "NOTE GOBY-WATERMARK is caption text" {
				t.Fatal("old notes escaped into a later cue or caption text became a barrier")
			}
		}
		return nil
	})
	if err != nil || notes != 1001 || cues != 1 {
		t.Fatalf("raw NOTE blocks were interpreted or retained: notes=%d cues=%d err=%v", notes, cues, err)
	}
}

func TestLiveWebVTTPreservesMetadataAndConcatenatedRepresentationHeaders(t *testing.T) {
	source := "WEBVTT\nKind: captions\n\nSTYLE\n::cue { color: lime; }\n\nREGION\nid:main\n\n00:00.000 --> 00:02.000 region:main\nfirst\n\nNOTE discarded by journal\n\n" +
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000\n\n00:02.000 --> 00:04.000\nsecond"
	var cues []LiveBatch
	headers := 0
	err := ReadLiveWebVTT(strings.NewReader(source), LiveParserOptions{}, func(batch LiveBatch) error {
		if batch.HeaderOnly {
			headers++
		} else {
			cues = append(cues, batch)
		}
		return nil
	})
	if err != nil || len(cues) != 2 || headers != 5 {
		t.Fatalf("complete representation blocks = %d cues, %d header events, %v", len(cues), headers, err)
	}
	if len(cues[0].Document.blocks) != 2 || cues[0].Header.TimestampMap != nil || len(cues[1].Document.blocks) != 0 || cues[1].Header.TimestampMap == nil || cues[1].Header.TimestampMap.MPEGTS != 180000 {
		t.Fatal("representation header or metadata escaped its own source")
	}
	result, err := Render(cues[0].Document, Options{Format: FormatWebVTT, CopyTimestamps: true})
	if err != nil || !strings.Contains(string(result.Data), "STYLE\n::cue") || !strings.Contains(string(result.Data), "REGION\nid:main") {
		t.Fatalf("style/region metadata failed to render: %q, %v", result.Data, err)
	}
}

func TestLiveWebVTTRejectsMalformedOrAmbiguousHeaders(t *testing.T) {
	for _, source := range []string{
		"",
		"00:00.000 --> 00:01.000\ncaption\n\n",
		"WEBVTT\n00:00.000 --> 00:01.000\ncaption\n\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000\n\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:8589934592\n\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:-1\n\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,LOCAL:00:00:00.000\n\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:1\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:1\n\n",
		"WEBVTT\n\n00:00.000 --> 00:01.000\ncaption\n\nSTYLE\n::cue { color: lime; }\n\n",
		"WEBVTT\n\n00:00.000 --> 00:01.000\n\x00\n\n",
		"WEBVTT\n\n00:00.000 --> 00:01.000\n\xff\n\n",
	} {
		if err := ReadLiveWebVTT(strings.NewReader(source), LiveParserOptions{}, func(LiveBatch) error { return nil }); err == nil {
			t.Fatalf("malformed live input accepted: %q", source)
		}
	}
}

func TestLiveWebVTTEnforcesHeaderBlockMetadataAndLineLimits(t *testing.T) {
	for _, test := range []struct {
		name, source string
		options      LiveParserOptions
	}{
		{"header", "WEBVTT\nLong: metadata\n\n", LiveParserOptions{MaxHeaderBytes: 8}},
		{"block", "WEBVTT\n\n00:00.000 --> 00:01.000\n" + strings.Repeat("a", 40) + "\n\n", LiveParserOptions{MaxBlockBytes: 40}},
		{"metadata", "WEBVTT\n\nSTYLE\na\n\nSTYLE\nb\n\n", LiveParserOptions{MaxMetadataBytes: 12}},
		{"line", "WEBVTT\n" + strings.Repeat("x", 40), LiveParserOptions{MaxLineBytes: 16}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ReadLiveWebVTT(strings.NewReader(test.source), test.options, func(LiveBatch) error { return nil })
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("limit returned %v", err)
			}
		})
	}
}

func TestLiveWebVTTAcceptsBareCRAndEmptyFinalHeader(t *testing.T) {
	for _, source := range []string{"WEBVTT", "WEBVTT\r\r", "WEBVTT\r\r00:00.000 --> 00:01.000\rcaption\r\r"} {
		count := 0
		if err := ReadLiveWebVTT(strings.NewReader(source), LiveParserOptions{}, func(LiveBatch) error { count++; return nil }); err != nil || count == 0 {
			t.Fatalf("valid newline/header failed: %q, %d, %v", source, count, err)
		}
	}
}

func TestLiveWebVTTCueIdentifierDoesNotResetItsHeader(t *testing.T) {
	source := "WEBVTT\n\nWEBVTT\n00:00.000 --> 00:01.000\n" + strings.Repeat("caption ", 10) + "\n\n"
	var caption LiveBatch
	err := ReadLiveWebVTT(strings.NewReader(source), LiveParserOptions{MaxHeaderBytes: 16}, func(batch LiveBatch) error {
		if !batch.HeaderOnly {
			caption = batch
		}
		return nil
	})
	if err != nil || len(caption.Document.Cues) != 1 || caption.Document.Cues[0].Identifier != "WEBVTT" {
		t.Fatalf("legal cue identifier was parsed as a new header: %+v, %v", caption, err)
	}
}
