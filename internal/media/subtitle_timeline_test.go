package media

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestSubtitleTimelineAdmitsOnlyInternalBitmapStreams(t *testing.T) {
	info := Info{Streams: []Stream{
		{Index: 0, CodecType: "video", Codec: "hevc"},
		{Index: 1, CodecType: "audio", Codec: "aac"},
		{Index: 2, CodecType: "subtitle", Codec: "subrip"},
		{Index: 9, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle"},
		{Index: 5, CodecType: "subtitle", Codec: "dvd_subtitle"},
		{Index: -1, CodecType: "subtitle", Codec: "dvd_subtitle", IsExternal: true},
	}}
	streams, err := subtitleTimelineStreams(info)
	if err != nil || len(streams) != 2 || streams[0].Index != 5 || streams[1].Index != 9 {
		t.Fatalf("incorrect admitted streams: %+v, %v", streams, err)
	}
	for _, codec := range []string{"subrip", "webvtt", "ass", "dvb_subtitle", "xsub", ""} {
		_, err := subtitleTimelineStreams(Info{Streams: []Stream{{Index: 0, CodecType: "subtitle", Codec: codec}}})
		if !errors.Is(err, ErrSubtitleTimelineUnsupported) {
			t.Fatalf("unsupported codec %q returned %v", codec, err)
		}
	}
	for _, changed := range []Stream{
		{Index: 0, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle"},
		{Index: -1, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle"},
		{Index: 4096, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle"},
		{Index: 3, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", IsAttachedPicture: true},
	} {
		_, err := subtitleTimelineStreams(Info{Streams: []Stream{{Index: 0, CodecType: "video"}, changed}})
		if err == nil || errors.Is(err, ErrSubtitleTimelineUnsupported) {
			t.Fatalf("invalid source metadata was mislabeled unsupported: %+v, %v", changed, err)
		}
	}
}

func TestSubtitleTimelineUnionRetainsExactGaps(t *testing.T) {
	collector := subtitleTimelineCollector{duration: 100}
	for _, interval := range []SubtitleTimelineInterval{{30, 40}, {10, 20}, {15, 25}, {25, 29}, {10, 20}, {35, 45}, {60, 61}} {
		if err := collector.add(context.Background(), BitmapSubtitleCue{StartTicks: interval.StartTicks, EndTicks: interval.EndTicks}); err != nil {
			t.Fatal(err)
		}
	}
	want := []SubtitleTimelineInterval{{10, 29}, {30, 45}, {60, 61}}
	if got := collector.union(); !slices.Equal(got, want) {
		t.Fatalf("union changed visible coverage or filled a gap: got %+v, want %+v", got, want)
	}
}

func TestSubtitleTimelineUsesAuthoredPGSDisplayEvents(t *testing.T) {
	for _, sup := range []bool{false, true} {
		collector := subtitleTimelineCollector{duration: 70_000_000}
		ctx := context.Background()
		warnings, err := walkPGSSubtitles(ctx, subtitleFixturePGSPackets(sup), bitmapSubtitleLimits(collector.duration), func(cue BitmapSubtitleCue) error {
			return collector.add(ctx, cue)
		})
		if err != nil || len(warnings) != 0 {
			t.Fatalf("authored PGS display extraction failed: %v, %v", warnings, err)
		}
		if got := collector.union(); !slices.Equal(got, []SubtitleTimelineInterval{{10_240_000, 56_320_000}}) {
			t.Fatalf("PGS clear/overlap timing was not preserved: %+v", got)
		}
	}
}

func TestSubtitleTimelineUsesAuthoredDVDControlClock(t *testing.T) {
	packet, private := subtitleFixtureDVDPacket(false)
	second := packet
	second.PTS = 30_720_000
	collector := subtitleTimelineCollector{duration: 70_000_000}
	ctx := context.Background()
	warnings, err := walkDVDSubtitles(ctx, []bitmapSubtitlePacket{packet, second}, private, bitmapSubtitleLimits(collector.duration), func(cue BitmapSubtitleCue) error {
		return collector.add(ctx, cue)
	})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("authored DVD control extraction failed: %v, %v", warnings, err)
	}
	if got := collector.union(); !slices.Equal(got, []SubtitleTimelineInterval{{10_240_000, 61_440_000}}) {
		t.Fatalf("DVD command-clock/overlap timing was not preserved: %+v", got)
	}
}

func TestSubtitleTimelineUnsupportedDVDFeatureIsTyped(t *testing.T) {
	for _, test := range []struct {
		name        string
		edit        func([]byte) []byte
		unsupported bool
	}{
		{"known color-change command", func(data []byte) []byte { data[11] = 0x07; data[12], data[13] = 0, 6; return data }, true},
		{"truncated color-change command", func(data []byte) []byte { data[35] = 0x07; return data }, false},
		{"oversized color-change command", func(data []byte) []byte { data[11] = 0x07; data[12], data[13] = 0xff, 0xff; return data }, false},
		{"unknown command", func(data []byte) []byte { data[11] = 0x7f; return data }, false},
		{"invalid pixel offsets", func(data []byte) []byte { data[27] = 7; return data }, false},
		{"zeroed DVD header", func(data []byte) []byte { data[0], data[1] = 0, 0; return data }, false},
		{"truncated HD-DVD header", func([]byte) []byte { return []byte{0, 0, 0, 0} }, false},
		{"HD-DVD", func([]byte) []byte { return []byte{0, 0, 0, 0, 0, 17, 0, 0, 0, 10, 0, 0, 0, 0, 0, 10, 0xff} }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: test.edit(dvdTestSPU(false))}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
			if err == nil || errors.Is(err, errBitmapSubtitleUnsupported) != test.unsupported {
				t.Fatalf("unsupported classification=%t, want %t; error=%v", errors.Is(err, errBitmapSubtitleUnsupported), test.unsupported, err)
			}
		})
	}
}

func TestSubtitleTimelineCancellationDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := subtitleTimelineCollector{duration: 100}
	if err := collector.add(ctx, BitmapSubtitleCue{StartTicks: 1, EndTicks: 2}); !errors.Is(err, context.Canceled) || len(collector.intervals) != 0 {
		t.Fatalf("canceled collector retained an interval: %+v, %v", collector.intervals, err)
	}
	var output bytes.Buffer
	_, err := GenerateSubtitleTimelines(ctx, BitmapSubtitleConfig{}, nil, Info{}, &output, nil)
	if !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("canceled generation published output: bytes=%d, error=%v", output.Len(), err)
	}
	capabilities, err := SubtitleTimelineAvailability(ctx, BitmapSubtitleConfig{})
	if !errors.Is(err, context.Canceled) || capabilities.Available {
		t.Fatalf("canceled inventory succeeded: %+v, %v", capabilities, err)
	}
}

func TestSubtitleTimelineMissingDemuxerIsUnavailable(t *testing.T) {
	capabilities, err := SubtitleTimelineAvailability(context.Background(), BitmapSubtitleConfig{})
	if err != nil || capabilities.Available || capabilities.Reason == "" {
		t.Fatalf("missing tool did not produce an unavailable capability: %+v, %v", capabilities, err)
	}
}
