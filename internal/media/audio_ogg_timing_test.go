package media

import (
	"errors"
	"strings"
	"testing"
)

func TestOggTimingRequiresIndependentPhysicalStreamEvidence(t *testing.T) {
	info := audioTimingTestInfo("ogg", "flac", 48000, "1/48000")
	document := audioTimingTestJSON(t, audioTimingTestPacket(0, 100, 0, 4), audioTimingTestFrame(0, 100, 0, 4))
	audioTimingTestUnproven(t, info, document, "ogg_structure_unverified")
}

func TestOggTimingDisambiguatesPacketsSharingOnePagePosition(t *testing.T) {
	info := audioTimingTestInfo("ogg", "flac", 48000, "1/48000")
	evidence := &oggAudioEvidence{PacketCounts: map[int]int64{0: 2}}
	result := oggTimingParse(t, info, evidence,
		audioTimingTestPacket(0, 100, 0, 4), audioTimingTestPacket(0, 100, 4, 4),
		audioTimingTestFrame(0, 100, 0, 4), audioTimingTestFrame(0, 100, 4, 4))
	facts := result.Streams[0].AudioTiming
	if !result.AudioDurationExact || facts.SampleCount != 8 || facts.PacketCount != 2 || facts.LastPacketStartTicks != 833 || facts.EndTicks != 1667 {
		t.Fatalf("shared physical positions lost an ordinal or sample span: %+v", facts)
	}
}

func TestOggOpusTimingProvesPreskipAndDiscardUsingEncodedPacketSamples(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		counts  []uint16
		preskip int64
		records func() []map[string]any
		samples int64
		last    int64
	}{
		{"single_trimmed_packet", []uint16{960}, 312, func() []map[string]any {
			packet := audioTimingTestPacket(0, 100, -312, 360)
			audioTimingTestPadding(packet, 312, 600)
			return []map[string]any{packet, audioTimingTestFrame(0, 100, 0, 48)}
		}, 48, -65_000},
		{"preskip_across_three_packets", []uint16{120, 120, 120}, 312, func() []map[string]any {
			first := audioTimingTestPacket(0, 100, -312, 120)
			audioTimingTestPadding(first, 312, 0)
			return []map[string]any{first, audioTimingTestPacket(0, 100, -192, 120), audioTimingTestPacket(0, 100, -72, 120),
				audioTimingTestFrame(0, 100, 0, 48)}
		}, 48, -15_000},
		{"fully_discarded_tail", []uint16{960, 960}, 312, func() []map[string]any {
			first := audioTimingTestPacket(0, 100, -312, 960)
			audioTimingTestPadding(first, 312, 0)
			last := audioTimingTestPacket(0, 100, 648, 1)
			audioTimingTestPadding(last, 0, 960)
			return []map[string]any{first, audioTimingTestFrame(0, 100, 0, 648), last}
		}, 648, -65_000},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			info := audioTimingTestInfo("ogg", "opus", 48000, "1/48000")
			evidence := &oggAudioEvidence{PacketCounts: map[int]int64{0: int64(len(fixture.counts))},
				OpusSamples: map[int][]uint16{0: fixture.counts}, OpusPreSkip: map[int]int64{0: fixture.preskip}}
			result := oggTimingParse(t, info, evidence, fixture.records()...)
			facts := result.Streams[0].AudioTiming
			if facts.SampleCount != fixture.samples || facts.LastPacketStartTicks != fixture.last ||
				facts.EndTicks != audioProbeCeilSampleTicks(fixture.samples, 48000) {
				t.Fatalf("Opus effective samples or last retained packet changed: %+v", facts)
			}
		})
	}
}

func TestOggVorbisTimingOnlyPermitsTheFirstOverlapInitializationPacket(t *testing.T) {
	info := audioTimingTestInfo("ogg", "vorbis", 44100, "1/44100")
	initial := []map[string]any{audioTimingTestPacket(0, 100, 0, 128), audioTimingTestPacket(0, 100, 128, 256), audioTimingTestFrame(0, 100, 128, 256)}
	evidence := &oggAudioEvidence{PacketCounts: map[int]int64{0: 2}, VorbisMaxPacketSamples: map[int]uint16{0: 1024}}
	result := oggTimingParse(t, info, evidence, initial...)
	if result.Streams[0].AudioTiming.SampleCount != 256 || result.Streams[0].AudioTiming.FirstPacketStartTicks != 0 {
		t.Fatalf("Vorbis initialization was counted as audible samples: %+v", result)
	}
	evidence.PacketCounts[0] = 3
	document := audioTimingTestJSON(t, append(initial, audioTimingTestPacket(0, 100, 384, 256))...)
	_, err := parseAudioTimingWithOgg(strings.NewReader(document), info, evidence)
	var unproven *audioTimingUnproven
	if !errors.As(err, &unproven) || unproven.Reason != "ogg_frame_coverage" {
		t.Fatalf("an unexplained later packet drop was accepted: %v", err)
	}
}

func TestOggTimingRejectsCountsParametersCorruptionAndDurationUnderflow(t *testing.T) {
	info := audioTimingTestInfo("ogg", "vorbis", 44100, "1/44100")
	for _, fixture := range []struct {
		name, reason string
		modify       func(map[string]any)
		packets      int64
	}{
		{"missing_packet", "ogg_packet_count_mismatch", func(map[string]any) {}, 3},
		{"duration_underflow", "ogg_packet_duration", func(p map[string]any) { p["duration"] = int64(4294967212) }, 2},
		{"parameter_change", "audio_parameters_changed", func(p map[string]any) {
			p["side_data_list"] = []map[string]any{{"side_data_type": "New Extradata"}}
		}, 2},
		{"corrupt_packet", "corrupt_packet", func(p map[string]any) { p["flags"] = "KC_" }, 2},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			second := audioTimingTestPacket(0, 100, 128, 256)
			fixture.modify(second)
			evidence := &oggAudioEvidence{PacketCounts: map[int]int64{0: fixture.packets}, VorbisMaxPacketSamples: map[int]uint16{0: 1024}}
			document := audioTimingTestJSON(t, audioTimingTestPacket(0, 100, 0, 128), second, audioTimingTestFrame(0, 100, 128, 256))
			_, err := parseAudioTimingWithOgg(strings.NewReader(document), info, evidence)
			var unproven *audioTimingUnproven
			if !errors.As(err, &unproven) || unproven.Reason != fixture.reason {
				t.Fatalf("unproven Ogg scan returned %v, want %s", err, fixture.reason)
			}
		})
	}
}

func oggTimingParse(t *testing.T, info Info, evidence *oggAudioEvidence, records ...map[string]any) Info {
	t.Helper()
	result, err := parseAudioTimingWithOgg(strings.NewReader(audioTimingTestJSON(t, records...)), info, evidence)
	if err != nil {
		t.Fatalf("parse independently proven Ogg timing: %v", err)
	}
	return result
}
