package transcode

import (
	"reflect"
	"strings"
	"testing"
)

func TestHLSClockWriterRetainsExactFirstMuxPacketPerRendition(t *testing.T) {
	var got []HLSMuxClock
	writer := hlsClockWriter{count: 2, callback: func(update Progress) { got = append(got, *update.HLSClock) }}
	for _, chunk := range []string{"GOBY 1 0 1/12288 -", "1024\nGOBY 0 0 1/90000 3000\n", "GOBY 1 1 1/12288 0\nGOBY 0 1 1/90000 6000\n"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	want := []HLSMuxClock{{Rendition: 1, PTS: -1024, TimeBaseNumerator: 1, TimeBaseDenominator: 12288}, {Rendition: 0, PTS: 3000, TimeBaseNumerator: 1, TimeBaseDenominator: 90000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mux evidence changed: %+v", got)
	}
	if ticks, err := got[0].Ticks(); err != nil || ticks != -833333 {
		t.Fatalf("signed exact clock conversion = %d, %v", ticks, err)
	}
}

func TestHLSClockWriterRejectsMissingReorderedAndUnboundedEvidence(t *testing.T) {
	for _, data := range []string{"GOBY 0 1 1/90000 0\n", "GOBY 1 0 1/90000 0\n", "GOBY 0 0 0/90000 0\n", "GOBY 0 0 1/0 0\n", "GOBY 0 0 1/90000 N/A\n", "GOBY 0 0 1/1 9223372036854775807\n", strings.Repeat("x", 257)} {
		cancelled := false
		writer := hlsClockWriter{count: 1, cancel: func() { cancelled = true }}
		if _, err := writer.Write([]byte(data)); err == nil || !cancelled {
			t.Fatalf("invalid evidence was accepted: %q", data)
		}
	}
	writer := hlsClockWriter{count: 2}
	_, _ = writer.Write([]byte("GOBY 0 0 1/90000 0\n"))
	if writer.finish() == nil {
		t.Fatal("an unobserved rendition inherited another clock")
	}
}

func TestHLSClockWriterAcceptsOnlyItsAssignedRendition(t *testing.T) {
	writer := hlsClockWriter{count: 2, expectRendition: true, expectedRendition: 1}
	if _, err := writer.Write([]byte("GOBY 1 0 1/12288 0\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.finish(); err != nil {
		t.Fatalf("one scoped pipe required another rendition: %v", err)
	}
	foreign := hlsClockWriter{count: 2, expectRendition: true, expectedRendition: 1}
	if _, err := foreign.Write([]byte("GOBY 0 0 1/12288 0\n")); err == nil {
		t.Fatal("clock evidence crossed rendition pipes")
	}
}

func TestHLSSubtitleClockRecordingDoesNotChangeOtherOutputCommands(t *testing.T) {
	plain, err := BuildArgs(adaptiveHLSPlan(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, argument := range plain {
		if strings.HasPrefix(argument, "-stats_mux_pre") {
			t.Fatal("subtitle-disabled output acquired a clock pipe")
		}
	}
	plan := adaptiveHLSPlan()
	plan.Subtitle = SubtitlePlan{Mode: "hls", Codec: "subrip", StreamIndex: 2}
	args, err := BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgumentPair(args, "-stats_mux_pre:v:0", "pipe:4") || !hasArgumentPair(args, "-stats_mux_pre:v:0", "pipe:5") || strings.Count(strings.Join(args, " "), "-stats_mux_pre:v:0") != 2 {
		t.Fatalf("each real rendition needs a distinct clock pipe: %v", args)
	}
}

func TestHLSCopyClockKeepsNativePTSSeparateFromNegativeDTS(t *testing.T) {
	var got *HLSMuxClock
	writer := hlsClockWriter{count: 1, copyReference: true, callback: func(update Progress) { clock := *update.HLSClock; got = &clock }}
	text := "#format: frame checksums\n#version: 1\n#hash: SHA256\n#tb 0: 1/12288\n#media_type 0: video\n" +
		"0, -1024, 0, 512, 91, " + strings.Repeat("a", 64) + "\n"
	if _, err := writer.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PTS != 0 || got.TimeBaseNumerator != 1 || got.TimeBaseDenominator != 12288 {
		t.Fatalf("copy packet clock = %+v", got)
	}
	if _, err := writer.Write([]byte("0, 0, 512, 512, 91, " + strings.Repeat("a", 64) + "\n")); err == nil {
		t.Fatal("copy evidence exceeded the single-packet output")
	}
}

func TestHLSCopyClockBoundsAndRequiresItsOwnTimebase(t *testing.T) {
	for _, text := range []string{
		"0, -1024, 0, 512, 91, " + strings.Repeat("a", 64) + "\n",
		"#tb 0: 1/0\n", "#tb 1: 1/12288\n", "#tb 0: 1/12288\n#tb 0: 1/12288\n",
		strings.Repeat("# short header\n", 600),
		"#tb 0: 1/12288\n0, -1024, N/A, 512, 91, " + strings.Repeat("a", 64) + "\n",
	} {
		writer := hlsClockWriter{count: 1, copyReference: true}
		if _, err := writer.Write([]byte(text)); err == nil {
			t.Fatalf("invalid copy clock evidence accepted: %q", text[:min(80, len(text))])
		}
	}
}

func TestHLSCopyClockUsesOnePacketWithoutSecondInputOrDecode(t *testing.T) {
	plan := Plan{Container: "ts", VideoCodec: "copy", AudioCodec: "copy", VideoStreamIndex: 2, AudioStreamIndex: 5,
		DurationTicks: 12 * ticksPerSecond, SegmentSeconds: 3, Subtitle: SubtitlePlan{Mode: "hls", Codec: "subrip", StreamIndex: 6}}
	args, err := BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if strings.Count(joined, "-i /proc/self/fd/3") != 1 || strings.Contains(joined, "-stats_mux_pre") || strings.Contains(joined, "libx264") {
		t.Fatalf("copy clock introduced a second input or encoder: %v", args)
	}
	if !strings.HasSuffix(joined, "-map 0:2 -map_metadata -1 -map_chapters -1 -sn -dn -c:v copy -frames:v:0 1 -avoid_negative_ts disabled -flush_packets 1 -f framehash -format_version 1 -hash sha256 pipe:4") {
		t.Fatalf("copy evidence is not a bounded native-clock output: %v", args)
	}
	plan.VideoCodec, plan.VideoStreamIndex = "", -1
	args, err = BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgumentPair(args, "-frames:a:0", "1") || !hasArgumentPair(args, "-map", "0:5") {
		t.Fatalf("audio copy clock reference = %v", args)
	}
}
