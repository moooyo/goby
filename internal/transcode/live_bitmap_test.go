package transcode

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLiveBitmapHeartbeatKeepsIndependentRawClockAndCopiesSelectedMedia(t *testing.T) {
	p := commandPlan()
	p.SourceMode, p.DurationTicks = "stream", 0
	p.VideoStreamIndex, p.AudioStreamIndex = 5, 7
	p.SourceFormatStartKnown, p.SourceFormatStartTicks = true, 9*ticksPerSecond/2
	p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 9, OffsetTicks: -ticksPerSecond / 2}
	input := strings.Join(appendLiveBitmapInputArgs(nil, p, 1), " ")
	if !strings.Contains(input, "-itsoffset -3.5000000 -i pipe:6") || strings.Contains(input, "discard") || strings.Contains(input, "/proc/") || strings.Contains(input, "-0.5000000") {
		t.Fatalf("bitmap source was discarded, reopened, or shifted twice: %s", input)
	}
	output := appendLiveBitmapHeartbeatOutputArgs([]string{"existing-output"}, p)
	want := []string{"existing-output", "-map", "1:5", "-map", "1:7", "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn",
		"-c", "copy", "-avoid_negative_ts", "disabled", "-f", "null", "pipe:1"}
	if !reflect.DeepEqual(output, want) {
		t.Fatalf("heartbeat changed output ownership or clock: %q", output)
	}
	for _, unsafe := range []string{"-frames", "-shortest", "setpts", "-r", "-ss", "-re", "-vf", "-af"} {
		if strings.Contains(strings.Join(output, " "), unsafe) {
			t.Fatal("heartbeat created an artificial or truncated clock")
		}
	}
	p.AudioStreamIndex = -1
	videoOnly := strings.Join(appendLiveBitmapHeartbeatOutputArgs(nil, p), " ")
	if strings.Count(videoOnly, "-map ") != 1 || !strings.Contains(videoOnly, "-map 1:5 ") {
		t.Fatal("video-only bitmap source needs an audio stream to advance")
	}
	p.VideoStreamIndex = -1
	if err := ValidateSubtitlePlan(p); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("audio-only source accepted bitmap burn without a video clock")
	}
}

func TestLiveBitmapHeartbeatDoesNotChangeFiniteOrUnselectedInputs(t *testing.T) {
	base := commandPlan()
	for _, p := range []Plan{base, {SourceMode: "stream"}, {SourceMode: "stream", Subtitle: SubtitlePlan{Mode: "hls", Codec: "webvtt", StreamIndex: 3}}} {
		args := []string{"unchanged"}
		if !reflect.DeepEqual(appendLiveBitmapInputArgs(args, p, 1), args) || !reflect.DeepEqual(appendLiveBitmapHeartbeatOutputArgs(args, p), args) {
			t.Fatal("bitmap heartbeat changed a finite or text-only producer")
		}
	}
}
