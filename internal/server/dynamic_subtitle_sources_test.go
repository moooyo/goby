package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/subtitle"
)

func dynamicSubtitleTestDefinition(target, mode string) dynamicsource.SubtitleDefinition {
	definition := dynamicsource.SubtitleDefinition{ID: "captions", Name: "Captions", Format: "webvtt", Mode: mode, URL: target, Clock: "media",
		Headers: map[string]string{"Authorization": "Bearer owned-caption-source"}}
	if mode == "webvtt-hls" {
		definition.Clock, definition.SegmentClock = "mpegts", "timestamp-map"
	}
	if mode == "webvtt-stream" {
		definition.StreamWatermarks = "goby-note-v1"
	}
	return definition
}

func TestDynamicSubtitleDocumentModesPublishParsedFiniteData(t *testing.T) {
	for _, test := range []struct{ format, data string }{
		{"webvtt", "WEBVTT\n\n00:01.000 --> 00:02.000\ncaption\n"},
		{"subrip", "1\n00:00:01,000 --> 00:00:02,000\ncaption\n"},
		{"ass", "[Script Info]\nScriptType: v4.00+\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,caption\n"},
	} {
		t.Run(test.format, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer owned-caption-source" || r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("private headers or representation negotiation changed")
				}
				_, _ = io.WriteString(w, test.data)
			}))
			defer upstream.Close()
			definition := dynamicSubtitleTestDefinition(upstream.URL, "document")
			definition.Format, definition.OffsetTicks = test.format, 2*subtitle.TicksPerSecond
			var updates []dynamicSubtitleSourceUpdate
			err := readDynamicSubtitleSource(context.Background(), definition, func(update dynamicSubtitleSourceUpdate) error { updates = append(updates, update); return nil })
			if err != nil || len(updates) < 3 || !updates[len(updates)-1].SourceComplete {
				t.Fatalf("finite document did not complete: %v", err)
			}
			cues := 0
			for _, update := range updates {
				if update.Clock != "media" || update.OffsetTicks != definition.OffsetTicks || update.HasSegment || update.SegmentComplete {
					t.Fatal("a finite document invented a playlist or media watermark")
				}
				for _, cue := range update.Batch.Document.Cues {
					cues++
					if cue.Text != "caption" || cue.StartTicks != subtitle.TicksPerSecond || cue.EndTicks != 2*subtitle.TicksPerSecond {
						t.Fatal("retrieval applied an unanchored clock or lost subtitle content")
					}
				}
			}
			if cues != 1 {
				t.Fatal("the finite response duplicated or omitted a cue")
			}
		})
	}
}

func TestDynamicSubtitleStreamPublishesBeforeEOFAndClosesOnCallback(t *testing.T) {
	closed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:8589934502\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "00:01.000 --> 00:02.000\ncaption\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	definition := dynamicSubtitleTestDefinition(upstream.URL, "webvtt-stream")
	definition.Clock = "mpegts"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stop := errors.New("caption consumer stopped")
	gotCue := false
	err := readDynamicSubtitleSource(ctx, definition, func(update dynamicSubtitleSourceUpdate) error {
		if update.SourceComplete || update.SegmentComplete || update.HasSegment {
			t.Fatal("stream bytes fabricated completion evidence")
		}
		if update.Batch.Header.TimestampMap == nil || update.Batch.Header.TimestampMap.MPEGTS != 8589934502 {
			t.Fatal("the 33-bit timestamp map was guessed or rewritten")
		}
		if len(update.Batch.Document.Cues) != 0 {
			gotCue = true
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || !gotCue {
		t.Fatalf("the incremental callback waited for upstream EOF: %v", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("callback cancellation retained the upstream response")
	}
}

func TestDynamicSubtitleMPEGTSTimingRequiresExplicitSourceMapping(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "WEBVTT\n\n00:01.000 --> 00:02.000\ncaption\n")
	}))
	defer upstream.Close()
	definition := dynamicSubtitleTestDefinition(upstream.URL, "document")
	definition.Clock = "mpegts"
	count := 0
	err := readDynamicSubtitleSource(context.Background(), definition, func(dynamicSubtitleSourceUpdate) error { count++; return nil })
	if !errors.Is(err, subtitle.ErrLiveTimestampMap) || count != 0 {
		t.Fatal("an MPEGTS source without a map was treated as clock zero")
	}
}

func TestDynamicSubtitleStreamReportsOnlyExplicitMonotonicWatermarks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "WEBVTT\n\nNOTE generic annotation\n\n00:00.000 --> 00:01.000\nNOTE GOBY-WATERMARK 00:10:00.000\n\n"+
			"NOTE GOBY-WATERMARK 00:00:01.000\n\nNOTE GOBY-WATERMARK 00:00:01.000\n\nNOTE GOBY-WATERMARK 00:00:02.000\n\n")
	}))
	defer upstream.Close()
	var marks []int64
	cues := 0
	err := readDynamicSubtitleSource(context.Background(), dynamicSubtitleTestDefinition(upstream.URL, "webvtt-stream"), func(update dynamicSubtitleSourceUpdate) error {
		if update.SourceComplete || update.SegmentComplete || update.IntervalLocalStartTicks != nil {
			t.Fatal("a stream invented finite interval completion")
		}
		if update.WatermarkTicks != nil {
			marks = append(marks, *update.WatermarkTicks)
		}
		if update.Batch.Note == "NOTE generic annotation" {
			t.Fatal("generic NOTE became a source event")
		}
		cues += len(update.Batch.Document.Cues)
		return nil
	})
	if !errors.Is(err, io.EOF) || cues != 1 || fmt.Sprint(marks) != "[10000000 20000000]" {
		t.Fatalf("stream barriers were lost, guessed, or duplicated: %v, %v", marks, err)
	}
}

func TestDynamicSubtitleStreamRejectsMalformedAndBackwardWatermarks(t *testing.T) {
	for _, note := range []string{
		"NOTE GOBY-WATERMARK 00:00:01.000\nextra",
		"NOTE GOBY-WATERMARK 00:60:00.000",
		"NOTE GOBY-WATERMARK 00:01.000",
		"NOTE GOBY-WATERMARK 721:00:00.000",
		"NOTE GOBY-WATERMARK 720:00:00.001",
		"NOTE GOBY-WATERMARK 00:00:01.000 trailing",
		"NOTE GOBY-WATERMARK 00:00:02.000\n\nNOTE GOBY-WATERMARK 00:00:01.000",
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "WEBVTT\n\n"+note+"\n\n") }))
		err := readDynamicSubtitleSource(context.Background(), dynamicSubtitleTestDefinition(upstream.URL, "webvtt-stream"), func(dynamicSubtitleSourceUpdate) error { return nil })
		upstream.Close()
		if !errors.Is(err, errDynamicSubtitleWatermark) {
			t.Fatalf("an invalid completeness marker was accepted: %q, %v", note, err)
		}
	}
}

func TestDynamicSubtitleHLSRequiresEachSegmentsOwnStartMap(t *testing.T) {
	for _, payload := range []string{
		"00:00.000 --> 00:01.000\ncaption\n",
		"WEBVTT\n\n00:00.000 --> 00:01.000\ncaption\n",
		"WEBVTT\nX-TIMESTAMP-MAP=LOCAL:720:00:00.000,MPEGTS:90000\n\n",
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/main.m3u8":
				_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MAP:URI=\"header.vtt\"\n#EXTINF:1,\nsegment.vtt\n#EXT-X-ENDLIST\n")
			case "/header.vtt":
				_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n\n")
			case "/segment.vtt":
				_, _ = io.WriteString(w, payload)
			}
		}))
		called := false
		err := readDynamicSubtitleSource(context.Background(), dynamicSubtitleTestDefinition(upstream.URL+"/main.m3u8", "webvtt-hls"), func(dynamicSubtitleSourceUpdate) error { called = true; return nil })
		upstream.Close()
		if !errors.Is(err, subtitle.ErrLiveTimestampMap) || called {
			t.Fatal("a global header or unbounded LOCAL value was treated as this segment's proven start")
		}
	}
}

func TestDynamicSubtitleHLSParsesBoundedSegmentsMapsAndDiscontinuity(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer owned-caption-source" {
			t.Error("the same-origin authorized segment lost its credentials")
		}
		switch r.URL.Path {
		case "/captions/main.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:50\n#EXT-X-DISCONTINUITY-SEQUENCE:7\n#EXT-X-MAP:URI=\"header.vtt\"\n#EXTINF:2.0,\none.vtt\n#EXT-X-DISCONTINUITY\n#EXTINF:2.0,\nempty.vtt\n#EXTINF:1.0,\nthree.vtt\n#EXT-X-ENDLIST\n")
		case "/captions/header.vtt":
			_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n\n")
		case "/captions/one.vtt":
			_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n\n00:01.000 --> 00:05.000\ncrossing\n")
		case "/captions/three.vtt":
			_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:04.000,MPEGTS:450000\n\n00:01.000 --> 00:05.000\ncrossing\n")
		case "/captions/empty.vtt":
			_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:02.000,MPEGTS:270000\n\n")
		default:
			t.Errorf("an unrecognized subtitle resource was fetched: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	definition := dynamicSubtitleTestDefinition(upstream.URL+"/captions/main.m3u8", "webvtt-hls")
	definition.Clock = "mpegts"
	var complete []dynamicSubtitleSourceUpdate
	var last dynamicSubtitleSourceUpdate
	cues := 0
	err := readDynamicSubtitleSource(context.Background(), definition, func(update dynamicSubtitleSourceUpdate) error {
		last = update
		if update.SegmentComplete {
			complete = append(complete, update)
		}
		for _, cue := range update.Batch.Document.Cues {
			cues++
			if cue.StartTicks != subtitle.TicksPerSecond || cue.EndTicks != 5*subtitle.TicksPerSecond || update.Batch.Header.TimestampMap == nil {
				t.Fatal("HLS retrieval guessed a global timestamp or clipped a crossing cue")
			}
		}
		return nil
	})
	if err != nil || len(complete) != 3 || requests.Load() != 5 || cues != 2 || !last.SourceComplete || !last.PlaylistEnd {
		t.Fatalf("finite subtitle HLS was not completely consumed: %v", err)
	}
	for index, update := range complete {
		wantDiscontinuity := int64(7)
		if index > 0 {
			wantDiscontinuity = 8
		}
		if !update.HasSegment || update.Sequence != int64(50+index) || update.DiscontinuitySequence != wantDiscontinuity || update.Discontinuity != (index == 1) {
			t.Fatal("HLS source sequence or discontinuity was lost")
		}
		if update.IntervalLocalStartTicks == nil || *update.IntervalLocalStartTicks != int64(index*2)*subtitle.TicksPerSecond || update.IntervalDurationTicks != update.DurationTicks {
			t.Fatal("the explicit per-segment LOCAL interval was replaced by a global header")
		}
	}
}

func TestDynamicSubtitleHLSPollKeepsStableSequenceAcrossEviction(t *testing.T) {
	var mu sync.Mutex
	paths := make(map[string]int)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		count := paths[r.URL.Path]
		mu.Unlock()
		if r.URL.Path == "/main.m3u8" {
			if count == 1 {
				_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:7\n#EXTINF:1,\n7.vtt\n#EXT-X-DISCONTINUITY\n#EXTINF:1,\n8.vtt\n")
			} else {
				_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:8\n#EXT-X-DISCONTINUITY-SEQUENCE:1\n#EXTINF:1,\n8.vtt\n#EXTINF:1,\n9.vtt\n#EXT-X-ENDLIST\n")
			}
			return
		}
		var sequence int
		_, _ = fmt.Sscanf(r.URL.Path, "/%d.vtt", &sequence)
		_, _ = fmt.Fprintf(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:%02d.000,MPEGTS:%d\n\n", sequence-7, (sequence-6)*90000)
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var sequences []int64
	err := readDynamicSubtitleSource(ctx, dynamicSubtitleTestDefinition(upstream.URL+"/main.m3u8", "webvtt-hls"), func(update dynamicSubtitleSourceUpdate) error {
		if update.SegmentComplete {
			sequences = append(sequences, update.Sequence)
		}
		return nil
	})
	mu.Lock()
	repeated := paths["/8.vtt"]
	polls := paths["/main.m3u8"]
	mu.Unlock()
	if err != nil || fmt.Sprint(sequences) != "[7 8 9]" || repeated != 1 || polls != 2 {
		t.Fatalf("rolling subtitle retrieval duplicated a segment or lost continuity: %v, %v", sequences, err)
	}
}

func TestDynamicSubtitleHLSRejectsRewrittenSequence(t *testing.T) {
	var polls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/main.m3u8" {
			name := "original.vtt"
			if polls.Add(1) > 1 {
				name = "replacement.vtt"
			}
			_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:7\n#EXTINF:1,\n%s\n", name)
			return
		}
		_, _ = io.WriteString(w, "WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:90000\n\n")
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err := readDynamicSubtitleSource(ctx, dynamicSubtitleTestDefinition(upstream.URL+"/main.m3u8", "webvtt-hls"), func(dynamicSubtitleSourceUpdate) error { return nil })
	if !errors.Is(err, errDynamicSubtitleContinuity) {
		t.Fatalf("a changed immutable subtitle sequence was accepted: %v", err)
	}
}

func TestDynamicSubtitleFetchRejectsRedirectCompressionAndOversizedDocument(t *testing.T) {
	var escaped atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer foreign.Close()
	for _, behavior := range []string{"redirect", "compression", "size"} {
		t.Run(behavior, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch behavior {
				case "redirect":
					http.Redirect(w, r, foreign.URL, http.StatusFound)
				case "compression":
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = io.WriteString(w, "WEBVTT\n\n")
				case "size":
					w.Header().Set("Content-Length", fmt.Sprint(subtitle.MaxInputBytes+1))
					_, _ = io.WriteString(w, "WEBVTT\n\n")
				}
			}))
			defer upstream.Close()
			called := false
			err := readDynamicSubtitleSource(context.Background(), dynamicSubtitleTestDefinition(upstream.URL, "document"), func(dynamicSubtitleSourceUpdate) error { called = true; return nil })
			if err == nil || called || escaped.Load() != 0 {
				t.Fatal("unbounded or redirected content reached the subtitle consumer")
			}
		})
	}
}

func TestDynamicSubtitleSourceCancellationClosesAWaitingBody(t *testing.T) {
	closed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := readDynamicSubtitleSource(ctx, dynamicSubtitleTestDefinition(upstream.URL, "document"), func(dynamicSubtitleSourceUpdate) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a cancelled subtitle body remained active: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("the cancelled upstream connection remained open")
	}
}

func TestDynamicSubtitlePlaylistAndRelativeURLLimits(t *testing.T) {
	base, _ := url.Parse("https://caption.example/owned/main.m3u8?private=token")
	for _, reference := range []string{"https://other.example/a.vtt", "//other.example/a.vtt", "../a.vtt", "%2e%2e/a.vtt", "a%2fb.vtt", "a%252fb.vtt", "a.vtt#fragment", "a%00.vtt", "a%1f.vtt", "a.vtt?line=%0d", "?only=query"} {
		if _, err := dynamicSubtitleRelativeURL(base, reference); err == nil {
			t.Fatalf("unsafe segment reference accepted: %q", reference)
		}
	}
	for _, reference := range []string{"segment.vtt", "captions/0001.vtt?part=2", "/captions/0001.vtt"} {
		resolved, err := dynamicSubtitleRelativeURL(base, reference)
		if err != nil || !strings.HasPrefix(resolved, "https://caption.example/") || strings.Contains(resolved, "private=token") {
			t.Fatal("a safe relative reference changed origin or inherited unrelated credentials")
		}
	}
	baseList := "#EXTM3U\n#EXT-X-TARGETDURATION:2\n"
	for _, body := range []string{
		baseList + "#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:1,\na.vtt\n",
		baseList + "#EXT-X-BYTERANGE:10@0\n#EXTINF:1,\na.vtt\n",
		baseList + "#EXT-X-STREAM-INF:BANDWIDTH=100\nchild.m3u8\n",
		baseList + "#EXT-X-MAP:URI=\"head.vtt\",BYTERANGE=\"10@0\"\n#EXTINF:1,\na.vtt\n",
		baseList + "#EXTINF:NaN,\na.vtt\n", baseList + "#EXTINF:2.6,\na.vtt\n",
		baseList + strings.Repeat("#EXTINF:1,\na.vtt\n", dynamicSubtitlePlaylistSegments+1),
	} {
		if _, err := parseDynamicSubtitlePlaylist([]byte(body)); err == nil {
			t.Fatal("unsupported or unbounded subtitle HLS was accepted")
		}
	}
}
