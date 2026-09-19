package transcode

import (
	"slices"
	"strings"
	"testing"
)

func liveTestPlan() Plan {
	return Plan{SourceMode: "stream", Container: "ts", VideoCodec: "h264", AudioCodec: "aac", VideoStreamIndex: 0, AudioStreamIndex: 1,
		Width: 160, Height: 90, FrameRate: 25, VideoBitrate: 300000, AudioBitrate: 64000, AudioChannels: 1, AudioSampleRate: 48000, SegmentSeconds: 1, HLS: HLSPlan{SegmentType: "mpegts"}}
}

func TestLiveJournalRequiresOrderedCompleteBoundedRecords(t *testing.T) {
	p := liveTestPlan()
	r, err := parseLiveJournalRecord("segment-000001.ts.tmp,3.125000,4.165000\n", p, 0, 1)
	if err != nil || r.start != 31_250_000 || r.end != 41_650_000 {
		t.Fatalf("actual CSV boundaries were not preserved: %+v %v", r, err)
	}
	for _, line := range []string{
		"segment-000002.ts.tmp,3.125000,4.165000\n",
		"../segment-000001.ts.tmp,3.125000,4.165000\n",
		"segment-000001.ts.tmp,4.165000,3.125000\n",
		"segment-000001.ts.tmp,3.125000,3.125000\n",
		"segment-000001.ts.tmp,3.125000,124.125000\n",
		"segment-000001.ts.tmp,3e1,4.165000\n",
		"segment-000001.ts.tmp,3.125000,4.165000",
		"segment-000001.ts.tmp,3.125000,4.165000\nsecond,row,line\n",
		strings.Repeat("x", liveJournalLineBytes+1) + "\n",
	} {
		if _, err := parseLiveJournalRecord(line, p, 0, 1); err == nil {
			t.Fatalf("invalid journal record accepted: %q", line)
		}
	}
	if _, err := parseLiveJournalRecord("segment-000000.ts.tmp,0.000000,5.040000\n", p, 0, 0); err != nil {
		t.Fatal("first CSV placeholder must wait for independent clock evidence")
	}
}

func TestLiveCommandsUseJournalsAndOneContinuousPrimaryInput(t *testing.T) {
	for _, format := range []string{"mpegts", "fmp4"} {
		p := liveTestPlan()
		p.HLS.SegmentType = format
		if format == "fmp4" {
			p.Container = "mp4"
		}
		p.SourceFormatStartKnown, p.SourceFormatStartTicks = true, 14_000_000
		args, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !hasArgumentPair(args, "-i", "pipe:3") || !slices.Contains(args, "-copyts") || !hasArgumentPair(args, "-itsoffset", "-0.4000000") ||
			!hasArgumentPair(args, "-segment_time_delta", "0.000001") || !hasArgumentPair(args, "-segment_list_type", "csv") || !hasArgumentPair(args, "-segment_list", "pipe:5") || !hasArgumentPair(args, "-stats_mux_pre:v:0", "pipe:4") {
			t.Fatalf("live command lost its primary clock or closed-segment journal: %v", args)
		}
		if format == "mpegts" && !hasArgumentPair(args, "-segment_format_options", "mpegts_copyts=1:mpegts_flags=+initial_discontinuity") {
			t.Fatal("new TS muxers did not declare their continuity-counter resets")
		}
		if format == "fmp4" && !hasArgumentPair(args, "-segment_format_options", "movflags=+empty_moov+frag_keyframe+default_base_moof+skip_trailer+frag_discont:use_editlist=0:movie_timescale=1000:avoid_negative_ts=disabled") {
			t.Fatal("child MP4 muxers did not retain a stable initialization and source clock")
		}
		for _, forbidden := range []string{"-hls_playlist_type", "-hls_list_size", "-segment_wrap", "-reset_timestamps 1"} {
			if strings.Contains(strings.Join(args, " "), forbidden) {
				t.Fatalf("unbounded or clock-resetting live output: %s", forbidden)
			}
		}
		if strings.Contains(strings.Join(args, " "), "-t ") || countVideoSeekArgument(args, "-i") != 1 {
			t.Fatal("continuous live input acquired a finite restart boundary")
		}
	}
}

func TestLiveLadderUsesDistinctJournalsAndFixedClockSlots(t *testing.T) {
	p := liveTestPlan()
	p.HLS.RenditionCount = 2
	p.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 90, VideoBitrate: 300000}
	p.HLS.Renditions[1] = HLSRendition{Width: 96, Height: 54, VideoBitrate: 160000}
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-stats_mux_pre:v:0", "pipe:4"}, {"-stats_mux_pre:v:0", "pipe:5"}, {"-segment_list", "pipe:6"}, {"-segment_list", "pipe:7"}} {
		if !hasArgumentPair(args, pair[0], pair[1]) {
			t.Fatalf("renditions share a journal or clock pipe: %v", args)
		}
	}
	if liveBitmapFD(p) != 8 || LiveSourceClockBiasTicks(p) != ticksPerSecond {
		t.Fatal("live descriptor or common source bias layout changed")
	}
}
