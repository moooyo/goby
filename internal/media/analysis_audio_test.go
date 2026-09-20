package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func analysisTestFingerprint(mode string) AudioFingerprintMetadata {
	value := AudioFingerprintMetadata{ProtocolVersion: 1, Mode: mode, ChromaprintVersion: "1.6.1", ChromaprintRevision: analysisChromaprintRevision,
		Algorithm: 1, SampleRate: 11025, Channels: 1, ItemDurationSamples: 1365, DelaySamples: 28666, FirstItemEndSample: 30031,
		MaxInputSamples: 6615000, MaxInputBytes: 13230000, MaxOutputBytes: 65536}
	if mode == "fingerprint" {
		value.InputSamples, value.RawCount, value.Raw = 32761, 3, []uint32{0, 0x80000000, 0xffffffff}
		value.InputBytes = value.InputSamples * 2
	}
	return value
}

func TestAnalysisFingerprintStrictProtocolAndActualIntegerTiming(t *testing.T) {
	for _, mode := range []string{"describe", "fingerprint"} {
		value := analysisTestFingerprint(mode)
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseAnalysisFingerprint(data, mode)
		if err != nil || got.ItemDurationSamples != 1365 || got.DelaySamples != 28666 || got.RawCount != value.RawCount {
			t.Fatalf("protocol: %+v %v", got, err)
		}
	}
	for _, mutate := range []func(*AudioFingerprintMetadata){
		func(v *AudioFingerprintMetadata) { v.ChromaprintVersion = "1.6.0" },
		func(v *AudioFingerprintMetadata) { v.Algorithm = 0 },
		func(v *AudioFingerprintMetadata) { v.DelaySamples = 0 },
		func(v *AudioFingerprintMetadata) { v.FirstItemEndSample-- },
		func(v *AudioFingerprintMetadata) { v.RawCount-- },
		func(v *AudioFingerprintMetadata) { v.InputBytes-- },
		func(v *AudioFingerprintMetadata) { v.Raw = v.Raw[:2] },
	} {
		value := analysisTestFingerprint("fingerprint")
		mutate(&value)
		data, _ := json.Marshal(value)
		if _, err := parseAnalysisFingerprint(data, "fingerprint"); err == nil {
			t.Fatal("unproven fingerprint metadata was accepted")
		}
	}
	data, _ := json.Marshal(analysisTestFingerprint("describe"))
	for _, bad := range [][]byte{append(append([]byte{}, data...), data...), bytes.Replace(data, []byte(`"mode":"describe"`), []byte(`"mode":"describe","mode":"describe"`), 1), bytes.Repeat([]byte{' '}, 65537)} {
		if _, err := parseAnalysisFingerprint(bad, "describe"); err == nil {
			t.Fatal("ambiguous or unbounded JSON was accepted")
		}
	}
}

func TestAnalysisAudioAnchorsUseSamplePTSAndConservativeSupport(t *testing.T) {
	metadata := analysisTestFingerprint("fingerprint")
	samples, guard, err := mapAnalysisAudioSamples(11025, 10_000_000, 10*TicksPerSecond, metadata)
	if err != nil || len(samples) != 3 || guard != 27_239_003 {
		t.Fatalf("mapped samples=%+v guard=%d error=%v", samples, guard, err)
	}
	for i, tick := range []int64{0, 1_238_095, 2_476_190} {
		if samples[i].StartTicks != tick || samples[i].Fingerprint != metadata.Raw[i] {
			t.Fatalf("anchor %d: %+v", i, samples[i])
		}
		if i > 0 && samples[i-1].EndTicks != samples[i].StartTicks {
			t.Fatal("exact rational bins overlap or invent gaps")
		}
	}
	shifted, _, err := mapAnalysisAudioSamples(11025, 9_999_993, 10*TicksPerSecond, metadata)
	if err != nil || shifted[0].StartTicks != 7 || shifted[1].StartTicks != samples[1].StartTicks+7 {
		t.Fatalf("source origin was rounded away: %+v %v", shifted, err)
	}
	negative, _, err := mapAnalysisAudioSamples(-11025, -TicksPerSecond, 10*TicksPerSecond, metadata)
	if err != nil || negative[0].StartTicks != 0 {
		t.Fatalf("negative absolute PTS was not mapped: %+v %v", negative, err)
	}
}

func TestAnalysisFFmpegVersionPinsTheExactRelease(t *testing.T) {
	for _, version := range []string{"ffmpeg version 9.0.1 Copyright", "ffmpeg version 9.0.1-goby-custom Copyright"} {
		if !analysisFFmpegVersion(version) {
			t.Fatalf("pinned version rejected: %q", version)
		}
	}
	for _, version := range []string{"ffmpeg version 9.0.10", "ffmpeg version 9.0.2", "ffprobe version 9.0.1", "garbage ffmpeg version 9.0.1"} {
		if analysisFFmpegVersion(version) {
			t.Fatalf("different version accepted: %q", version)
		}
	}
}

func analysisTestAudioLog(t *testing.T) *analysisAudioLog {
	t.Helper()
	audit, err := newAnalysisPacketPTSAudit(Stream{Index: 1, CodecType: "audio", TimeBase: "1/1000"}, 16)
	if err != nil {
		t.Fatal(err)
	}
	return &analysisAudioLog{audit: audit, limit: 64 << 10, frameLimit: 2}
}

const analysisTestAudioPackets = "demuxer -> ist_index:0:1 type:audio pkt_pts:1000 pkt_pts_time:1 pkt_dts:1000 pkt_dts_time:1 duration:1 duration_time:0.001\n"

func analysisTestAudioFrame(number, pts int, checksum string) string {
	return fmt.Sprintf("[ashowinfo@analysis_audio @ 0x1] [info] n:%d pts:%d pts_time:1 fmt:s16 channels:1 chlayout:mono rate:11025 nb_samples:2 checksum:%s plane_checksums: [ %s ]\n", number, pts, checksum, checksum)
}

func TestAnalysisAudioFrameChecksumsBindPCMTimestamps(t *testing.T) {
	log := analysisTestAudioLog(t)
	text := analysisTestAudioPackets + analysisTestAudioFrame(0, 11025, "000A0006") + analysisTestAudioFrame(1, 11027, "00320016")
	for _, b := range []byte(text) {
		if _, err := log.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	log.Close(nil)
	pcm := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	first, err := log.provePCM(pcm)
	if err != nil || first != 11025 {
		t.Fatalf("PCM timeline: %d %v", first, err)
	}
	for _, altered := range [][]byte{{0, 1, 2, 3, 4, 5, 6, 8}, {0, 1, 2, 3}, {0, 1, 2, 3, 4, 5, 6, 7, 0, 0}} {
		if _, err := log.provePCM(altered); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("unbound PCM accepted: %v", err)
		}
	}
	if analysisPCMChecksum([]byte{0, 1, 2, 3}) != 0x000a0006 {
		t.Fatal("AVAdler seed changed")
	}
}

func TestAnalysisAudioParsesInterleavedAtomicLogBodies(t *testing.T) {
	log := analysisTestAudioLog(t)
	// ashowinfo's body has no terminating newline until its plane checksums
	// have been logged. A demux thread can finish its complete record first.
	first := strings.TrimSuffix(analysisTestAudioFrame(0, 11025, "000A0006"), "\n")
	text := first + " " + analysisTestAudioPackets + "[ashowinfo@analysis_audio @ 0x1] [info] plane_checksums: [ 000A0006 ]\n" + analysisTestAudioFrame(1, 11027, "00320016")
	if _, err := log.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	log.Close(nil)
	if pts, err := log.provePCM([]byte{0, 1, 2, 3, 4, 5, 6, 7}); err != nil || pts != 11025 {
		t.Fatalf("atomic frame/packet bodies were lost at a shared newline: %d %v", pts, err)
	}
}

func TestAnalysisAudioChecksumContinuationDoesNotInventAFrameStart(t *testing.T) {
	log := analysisTestAudioLog(t)
	// The preceding frame body may already have been completed by a demuxer
	// newline. Its later plane-checksum call can acquire a fresh prefix and
	// then share another line with a packet's duration: field. The suffix n:
	// inside duration: is not another ashowinfo frame record.
	text := analysisTestAudioPackets + analysisTestAudioFrame(0, 11025, "000A0006") +
		"[ashowinfo@analysis_audio @ 0x1] [info] plane_checksums: [ " + analysisTestAudioPackets +
		"000A0006 ]\n" + analysisTestAudioFrame(1, 11027, "00320016")
	for position := 0; position < len(text); position += 7 {
		if _, err := log.Write([]byte(text[position:min(position+7, len(text))])); err != nil {
			t.Fatal(err)
		}
	}
	log.Close(nil)
	if pts, err := log.provePCM([]byte{0, 1, 2, 3, 4, 5, 6, 7}); err != nil || pts != 11025 || len(log.frames) != 2 {
		t.Fatalf("continuation changed the exact PCM inventory: pts=%d frames=%d error=%v", pts, len(log.frames), err)
	}
}

func TestAnalysisAudioRejectsMalformedFrameBodyWithoutRepeatedPrefix(t *testing.T) {
	log := analysisTestAudioLog(t)
	malformed := strings.Replace(analysisTestAudioFrame(0, 11025, "000A0006"), "[ashowinfo@analysis_audio @ 0x1] [info] ", "", 1)
	malformed = strings.Replace(malformed, "rate:11025", "rate:48000", 1)
	if _, err := log.Write([]byte(analysisTestAudioPackets + malformed)); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatalf("prefixless malformed PCM timing was ignored: %v", err)
	}
}

func TestAnalysisAudioRefusesMissingDiscontinuousAndUnboundedTiming(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(analysisTestAudioPackets, "pkt_pts:1000", "pkt_pts:NOPTS", 1),
		analysisTestAudioPackets + analysisTestAudioFrame(0, 11025, "000A0006") + analysisTestAudioFrame(1, 11028, "00320016"),
		analysisTestAudioPackets + analysisTestAudioFrame(0, 11025, "000A0006") + analysisTestAudioFrame(0, 11027, "00320016"),
		analysisTestAudioPackets + strings.Replace(analysisTestAudioFrame(0, 11025, "000A0006"), "rate:11025", "rate:48000", 1),
		strings.Repeat("x", (16<<10)+1),
	} {
		log := analysisTestAudioLog(t)
		if _, err := log.Write([]byte(bad)); err == nil {
			t.Fatal("unproven audio timing record accepted")
		}
	}
	log := analysisTestAudioLog(t)
	_, _ = log.Write([]byte("incomplete"))
	log.Close(nil)
	if _, err := log.provePCM([]byte{0, 0}); err == nil {
		t.Fatal("truncated diagnostics accepted")
	}
}

func TestAnalysisLimitsAreHardBounds(t *testing.T) {
	got, err := (AnalysisExtractor{}).analysisLimits()
	if err != nil || got.MaxPCMBytes != 13230000 || got.MaxPreviewFrames != 8192 || got.MaxStderrBytes != 512<<20 {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	for _, limits := range []AnalysisLimits{{Timeout: 3 * time.Hour}, {MaxPCMBytes: 13230001}, {MaxVisualSamples: 10001}, {MaxSourcePixels: 33 << 20}, {MaxOutputBytes: 3 << 30}, {MaxRawBytes: 9 << 30}, {MaxStderrBytes: -1}, {MaxStderrBytes: (512 << 20) + 1}} {
		if _, err := (AnalysisExtractor{Limits: limits}).analysisLimits(); !errors.Is(err, ErrAnalysisBudget) {
			t.Fatalf("unsafe bounds accepted: %+v %v", limits, err)
		}
	}
	for _, bytes := range []int64{1, 256 << 20, 512 << 20} {
		limits, err := (AnalysisExtractor{Limits: AnalysisLimits{MaxStderrBytes: bytes}}).analysisLimits()
		if err != nil || limits.MaxStderrBytes != bytes {
			t.Fatalf("an explicit cumulative diagnostic budget was replaced: %d, %+v, %v", bytes, limits, err)
		}
	}
}

func TestAnalysisAudioCommandKeepsAbsolutePTSAndDescriptorOnlyInput(t *testing.T) {
	args := analysisAudioArgs(Info{FormatStartTicks: -15_000_000}, Stream{Index: 3}, 600*TicksPerSecond, 11025)
	joined := strings.Join(args, " ")
	for _, part := range []string{"-copyts", "+nofillin-genpts", "-i /proc/self/fd/3", "-map 0:3", "atrim=start=-1.5000000:end=598.5000000", "asettb=expr=1/11025", "ashowinfo@analysis_audio", "-threads:a 1"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("missing bounded PTS argument %q: %s", part, joined)
		}
	}
	if strings.Contains(joined, " -ss ") || strings.Contains(joined, "asetpts") {
		t.Fatal("command silently rebases source audio")
	}
}
