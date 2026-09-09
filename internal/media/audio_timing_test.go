package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

func TestAudioTimingSupport(t *testing.T) {
	tests := []struct {
		name      string
		info      Info
		audioOnly bool
		reason    string
	}{
		{"mp3", audioTimingTestInfo("mp3", "mp3", 48000, "1/48000"), true, ""},
		{"aac", audioTimingTestInfo("aac", "aac", 48000, "1/48000"), true, ""},
		{"flac", audioTimingTestInfo("flac", "flac", 48000, "1/48000"), true, ""},
		{"wav", audioTimingTestInfo("wav", "pcm_s24le", 48000, "1/48000"), true, ""},
		{"mov_aliases", audioTimingTestInfo("mov,mp4,m4a,3gp,3g2,mj2", "alac", 48000, "1/48000"), true, ""},
		{"unsupported_format", audioTimingTestInfo("ogg", "flac", 48000, "1/48000"), true, "unsupported_format"},
		{"unsupported_codec", audioTimingTestInfo("m4a", "opus", 48000, "1/48000"), true, "unsupported_codec"},
		{"incomplete_pcm_name", audioTimingTestInfo("wav", "pcm_", 48000, "1/48000"), true, "unsupported_codec"},
		{"no_audio", Info{Container: "wav"}, false, ""},
		{"artwork_only", Info{Container: "mp3", Streams: []Stream{{CodecType: "video", IsAttachedPicture: true}}}, false, ""},
	}
	artwork := audioTimingTestInfo("mp3", "mp3", 48000, "1/48000")
	artwork.Streams = append(artwork.Streams, Stream{Index: 1, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true})
	tests = append(tests, struct {
		name      string
		info      Info
		audioOnly bool
		reason    string
	}{"audio_with_artwork", artwork, true, ""})
	video := artwork
	video.Streams = append([]Stream(nil), artwork.Streams...)
	video.Streams[1].IsAttachedPicture = false
	tests = append(tests, struct {
		name      string
		info      Info
		audioOnly bool
		reason    string
	}{"real_video", video, false, ""})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			audioOnly, reason := audioTimingSupport(test.info)
			if audioOnly != test.audioOnly || reason != test.reason {
				t.Fatalf("audioTimingSupport() = (%t, %q), want (%t, %q)", audioOnly, reason, test.audioOnly, test.reason)
			}
		})
	}
}

func TestParseAudioTimingMP3PaddingAndWholePacketDiscard(t *testing.T) {
	info := audioTimingTestInfo("mp3", "mp3", 44100, "1/14112000")
	info.DurationTicks = 9_999_999
	first := audioTimingTestPacket(0, 100, 0, 368640)
	audioTimingTestPadding(first, 1105, 0)
	firstFrame := audioTimingTestFrame(0, 100, 353600, 47)
	// A decoder can repeat trimming side data on a frame whose nb_samples has
	// already been trimmed. These values must not be subtracted a second time.
	audioTimingTestPadding(firstFrame, 1105, 0)
	last := audioTimingTestPacket(0, 300, 737280, 368640)
	audioTimingTestPadding(last, 0, 152)
	dropped := audioTimingTestPacket(0, 400, 1105920, 368640)
	audioTimingTestPadding(dropped, 0, 1152)
	result := audioTimingTestParse(t, info,
		first, firstFrame,
		audioTimingTestPacket(0, 200, 368640, 368640), audioTimingTestFrame(0, 200, 368640, 1152),
		last, audioTimingTestFrame(0, 300, 737280, 1000), dropped,
	)
	want := &AudioTiming{
		Exact: true, SampleCount: 2199, PacketCount: 4,
		StartTicks: 0, EndTicks: 498640, FirstPacketStartTicks: -250567,
		LastPacketStartTicks: 271882, MaxPacketDurationTicks: 261225,
	}
	if !reflect.DeepEqual(result.Streams[0].AudioTiming, want) {
		t.Fatalf("AudioTiming = %+v, want %+v", result.Streams[0].AudioTiming, want)
	}
	if !result.AudioDurationExact || result.DurationTicks != want.EndTicks || result.PresentationOriginTicks != 250567 || result.AudioDurationReason != "" {
		t.Fatalf("unexpected presentation timing: %+v", result)
	}
	if info.Streams[0].AudioTiming != nil || info.DurationTicks != 9_999_999 || info.AudioDurationExact {
		t.Fatalf("parseAudioTiming mutated input facts: %+v", info)
	}
}

func TestParseAudioTimingAACPartialFirstPacket(t *testing.T) {
	info := audioTimingTestInfo("mov,mp4,m4a,3gp,3g2,mj2", "aac", 48000, "1/48000")
	priming := audioTimingTestPacket(0, 10, -1024, 1024)
	audioTimingTestPadding(priming, 1024, 0)
	first := audioTimingTestPacket(0, 20, 0, 1024)
	audioTimingTestPadding(first, 512, 0)
	result := audioTimingTestParse(t, info,
		priming, first, audioTimingTestFrame(0, 20, 512, 512),
		audioTimingTestPacket(0, 30, 1024, 1024), audioTimingTestFrame(0, 30, 1024, 1024),
	)
	timing := result.Streams[0].AudioTiming
	if timing.SampleCount != 1536 || timing.PacketCount != 3 || timing.EndTicks != 320000 ||
		timing.FirstPacketStartTicks != -106667 || timing.LastPacketStartTicks != 106666 ||
		result.PresentationOriginTicks != 106667 {
		t.Fatalf("unexpected AAC timing: %+v, origin %d", timing, result.PresentationOriginTicks)
	}
}

func TestParseAudioTimingMultipleStreamsAndFrames(t *testing.T) {
	info := audioTimingTestInfo("m4a", "aac", 48000, "1/48000")
	info.Streams = append(info.Streams,
		Stream{Index: 4, CodecType: "audio", Codec: "alac", SampleRate: 44100, TimeBase: "1/44100"},
		Stream{Index: 7, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true},
	)
	result := audioTimingTestParse(t, info,
		audioTimingTestPacket(0, 100, -48000, 48000),
		audioTimingTestPacket(4, 100, 22050, 44100),
		audioTimingTestFrame(0, 100, -48000, 24000),
		audioTimingTestFrame(4, 100, 22050, 44100),
		audioTimingTestFrame(0, 100, -24000, 24000),
	)
	if !result.AudioDurationExact || result.DurationTicks != 25_000_000 || result.PresentationOriginTicks != -10_000_000 {
		t.Fatalf("unexpected shared presentation timeline: %+v", result)
	}
	first, second := result.Streams[0].AudioTiming, result.Streams[1].AudioTiming
	if first.StartTicks != 0 || first.EndTicks != 10_000_000 || first.SampleCount != 48000 || first.PacketCount != 1 ||
		second.StartTicks != 15_000_000 || second.EndTicks != 25_000_000 || second.SampleCount != 44100 ||
		result.Streams[2].AudioTiming != nil {
		t.Fatalf("unexpected stream timing: %+v, %+v", first, second)
	}
}

func TestParseAudioTimingRejectsUnprovenScans(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		records []map[string]any
	}{
		{"packet_gap", "non_contiguous_packets", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestPacket(0, 20, 1025, 1024)}},
		{"packet_overlap", "non_contiguous_packets", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestPacket(0, 20, 1023, 1024)}},
		{"frame_gap", "non_contiguous_frames", []map[string]any{audioTimingTestPacket(0, 10, 0, 2048), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 1025, 1023)}},
		{"frame_overlap", "non_contiguous_frames", []map[string]any{audioTimingTestPacket(0, 10, 0, 2048), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 1023, 1024)}},
		{"duplicate_packet_position", "invalid_scan", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestPacket(0, 10, 1024, 1024)}},
		{"decreasing_packet_position", "invalid_scan", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestPacket(0, 9, 1024, 1024)}},
		{"frame_before_packet", "unproven_packet_mapping", []map[string]any{audioTimingTestFrame(0, 10, 0, 1024)}},
		{"unknown_packet_position", "unproven_packet_mapping", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 20, 0, 1024)}},
		{"aac_edit_exceeds_packet", "packet_frame_mismatch", []map[string]any{audioTimingTestPacket(0, 10, 0, 512), audioTimingTestFrame(0, 10, 0, 1024)}},
		{"frame_before_packet_start", "packet_frame_mismatch", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, -1, 1024)}},
		{"unexplained_leading_drop", "unproven_packet_drop", []map[string]any{audioTimingTestPacket(0, 10, -1024, 1024), audioTimingTestPacket(0, 20, 0, 1024), audioTimingTestFrame(0, 20, 0, 1024)}},
		{"unexplained_trailing_drop", "unproven_packet_drop", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024), audioTimingTestPacket(0, 20, 1024, 1024)}},
		{"no_frames", "no_audio_frames", []map[string]any{audioTimingTestPacket(0, 10, 0, 1024)}},
		{"unknown_stream", "invalid_scan", []map[string]any{audioTimingTestPacket(3, 10, 0, 1024)}},
		{"pts_addition_overflow", "timing_overflow", []map[string]any{audioTimingTestPacket(0, 10, math.MaxInt64, 1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := audioTimingTestInfo("m4a", "aac", 48000, "1/48000")
			info.DurationTicks = 1234567
			audioTimingTestUnproven(t, info, audioTimingTestJSON(t, test.records...), test.reason)
		})
	}
}

func TestParseAudioTimingRejectsMalformedFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value any
		frame bool
	}{
		{"missing_pts", "pts", nil, false},
		{"missing_position", "pos", "N/A", false},
		{"negative_position", "pos", -1, false},
		{"fractional_duration", "duration", "1/2", false},
		{"zero_duration", "duration", 0, false},
		{"long_number", "pts", strings.Repeat("1", 257), false},
		{"huge_exponent", "pts", "1e1001", false},
		{"numeric_overflow", "pts", "9223372036854775808", false},
		{"invalid_scalar_type", "pts", true, false},
		{"missing_samples", "nb_samples", nil, true},
		{"negative_samples", "nb_samples", -1, true},
		{"changed_sample_rate", "sample_rate", 44100, true},
		{"long_missing_sample_rate", "sample_rate", strings.Repeat(" ", 257), true},
		{"wrong_media_type", "media_type", "video", true},
		{"unknown_record_type", "type", "subtitle", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			packet, frame := audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024)
			if test.frame {
				frame[test.field] = test.value
			} else {
				packet[test.field] = test.value
			}
			audioTimingTestUnproven(t, audioTimingTestInfo("m4a", "aac", 48000, "1/48000"), audioTimingTestJSON(t, packet, frame), "invalid_scan")
		})
	}
	for _, document := range []string{
		`{}`, `[]`, `{"packets_and_frames":null}`, `{"packets_and_frames":[`,
		`{"packets_and_frames":[],"packets_and_frames":[]}`, `{"packets_and_frames":[]} {}`,
		`{"packets_and_frames":[],"other":true}`,
	} {
		t.Run(document, func(t *testing.T) {
			audioTimingTestUnproven(t, audioTimingTestInfo("m4a", "aac", 48000, "1/48000"), document, "invalid_scan")
		})
	}
}

func TestParseAudioTimingRequiresCompletePacketDropEvidence(t *testing.T) {
	for _, test := range []struct {
		name     string
		codec    string
		base     string
		duration int64
		padding  int64
	}{
		{"short_aac_edit", "aac", "1/48000", 304, 720},
		{"partial_aac_padding", "aac", "1/48000", 1024, 1023},
		{"unknown_aac_frame_size", "aac", "1/48000", 960, 960},
		{"unknown_mp3_frame_size", "mp3", "1/48000", 1024, 1024},
		{"unverified_codec_drop", "alac", "1/48000", 1024, 1024},
		{"fractional_packet_samples", "aac", "1/96000", 1, 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := audioTimingTestInfo("m4a", test.codec, 48000, test.base)
			dropped := audioTimingTestPacket(0, 20, 1024, test.duration)
			audioTimingTestPadding(dropped, 0, test.padding)
			frameSamples := int64(1024)
			if test.base == "1/96000" {
				frameSamples = 512
			}
			audioTimingTestUnproven(t, info, audioTimingTestJSON(t,
				audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, frameSamples), dropped,
			), "unproven_packet_drop")
		})
	}
}

func TestParseAudioTimingRejectsInvalidStreamFacts(t *testing.T) {
	base := audioTimingTestInfo("m4a", "aac", 48000, "1/48000")
	document := audioTimingTestJSON(t, audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024))
	for _, name := range []string{"duplicate_index", "missing_time_base", "negative_time_base", "zero_sample_rate", "excessive_sample_rate", "missing_second_stream"} {
		t.Run(name, func(t *testing.T) {
			info := base
			info.Streams = append([]Stream(nil), base.Streams...)
			reason := "invalid_scan"
			switch name {
			case "duplicate_index":
				info.Streams = append(info.Streams, info.Streams[0])
			case "missing_time_base":
				info.Streams[0].TimeBase = ""
			case "negative_time_base":
				info.Streams[0].TimeBase = "-1/48000"
			case "zero_sample_rate":
				info.Streams[0].SampleRate = 0
			case "excessive_sample_rate":
				info.Streams[0].SampleRate = 768001
			case "missing_second_stream":
				other := info.Streams[0]
				other.Index = 1
				info.Streams = append(info.Streams, other)
				reason = "no_audio_frames"
			}
			audioTimingTestUnproven(t, info, document, reason)
		})
	}
	tooMany := base
	tooMany.Streams = nil
	for index := 0; index <= maxAudioTimingStreams; index++ {
		stream := base.Streams[0]
		stream.Index = index
		tooMany.Streams = append(tooMany.Streams, stream)
	}
	audioTimingTestUnproven(t, tooMany, document, "stream_limit")
}

func TestParseAudioTimingStreamingAndPendingLimit(t *testing.T) {
	info := audioTimingTestInfo("m4a", "aac", 48000, "1/48000")
	document := audioTimingTestJSON(t, audioTimingTestPacket(0, 10, 0, 1024), audioTimingTestFrame(0, 10, 0, 1024))
	result, err := parseAudioTiming(&audioTimingChunkReader{value: document}, info)
	if err != nil || !result.AudioDurationExact {
		t.Fatalf("single-byte streaming scan failed: result %+v, error %v", result, err)
	}
	var pending strings.Builder
	pending.WriteString(`{"packets_and_frames":[`)
	for index := 0; index <= maxAudioTimingPending; index++ {
		if index > 0 {
			pending.WriteByte(',')
		}
		fmt.Fprintf(&pending, `{"type":"packet","stream_index":0,"pos":%d,"pts":%d,"duration":1024}`, index, index*1024)
	}
	pending.WriteString(`]}`)
	audioTimingTestUnproven(t, info, pending.String(), "scan_limit")
	audioTimingTestUnproven(t, info, audioTimingTestJSON(t,
		audioTimingTestPacket(0, 10, 0, maxAudioFrameSamples+1),
		audioTimingTestFrame(0, 10, 0, maxAudioFrameSamples+1),
	), "scan_limit")
	for _, kind := range []string{"packet", "frame"} {
		t.Run(kind+"_record_limit", func(t *testing.T) {
			scan := &audioTimingScan{known: map[int]bool{0: true}, packets: maxAudioTimingPackets, frames: maxAudioTimingFrames}
			err := scan.record(audioTimingRecord{Type: kind, Stream: "0"})
			var unproven *audioTimingUnproven
			if !errors.As(err, &unproven) || unproven.Reason != "scan_limit" {
				t.Fatalf("record limit error = %v, want scan_limit", err)
			}
		})
	}
}

func TestAudioTimingTicksRoundsOutwardAndRejectsOverflow(t *testing.T) {
	for _, test := range []struct {
		numerator int64
		ceil      bool
		want      int64
	}{
		{1, false, 208}, {1, true, 209}, {-1, false, -209}, {-1, true, -208},
	} {
		got, err := audioTimingTicks(new(big.Rat).SetFrac64(test.numerator, 48000), test.ceil)
		if err != nil || got != test.want {
			t.Fatalf("audioTimingTicks(%d/48000, %t) = %d, %v; want %d", test.numerator, test.ceil, got, err, test.want)
		}
	}
	if _, err := audioTimingTicks(new(big.Rat).SetInt64(math.MaxInt64), true); err == nil {
		t.Fatal("expected tick overflow to be unproven")
	}
}

func audioTimingTestInfo(container, codec string, rate int, base string) Info {
	return Info{Container: container, Streams: []Stream{{Index: 0, CodecType: "audio", Codec: codec, SampleRate: rate, TimeBase: base}}}
}

func audioTimingTestPacket(stream int, position, pts, duration int64) map[string]any {
	return map[string]any{"type": "packet", "codec_type": "audio", "stream_index": stream, "pos": position, "pts": pts, "duration": duration}
}

func audioTimingTestFrame(stream int, position, pts, samples int64) map[string]any {
	return map[string]any{"type": "frame", "media_type": "audio", "stream_index": stream, "pkt_pos": position, "pts": pts, "nb_samples": samples}
}

func audioTimingTestPadding(record map[string]any, skip, discard int64) {
	record["side_data_list"] = []map[string]any{{"side_data_type": "Skip Samples", "skip_samples": skip, "discard_padding": discard}}
}

func audioTimingTestJSON(t *testing.T, records ...map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"packets_and_frames": records})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func audioTimingTestParse(t *testing.T, info Info, records ...map[string]any) Info {
	t.Helper()
	result, err := parseAudioTiming(strings.NewReader(audioTimingTestJSON(t, records...)), info)
	if err != nil {
		t.Fatalf("parseAudioTiming() error: %v", err)
	}
	return result
}

func audioTimingTestUnproven(t *testing.T, info Info, document, reason string) {
	t.Helper()
	result, err := parseAudioTiming(strings.NewReader(document), info)
	var unproven *audioTimingUnproven
	if !errors.As(err, &unproven) || unproven.Reason != reason {
		t.Fatalf("parseAudioTiming() error = %v, want reason %q", err, reason)
	}
	if !reflect.DeepEqual(result, info) {
		t.Fatalf("unproven scan changed original facts: got %+v, want %+v", result, info)
	}
}

type audioTimingChunkReader struct {
	value string
}

func (reader *audioTimingChunkReader) Read(buffer []byte) (int, error) {
	if reader.value == "" {
		return 0, io.EOF
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	buffer[0] = reader.value[0]
	reader.value = reader.value[1:]
	return 1, nil
}
