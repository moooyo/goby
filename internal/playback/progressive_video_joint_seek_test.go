package playback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func progressiveVideoJointSeekTestSource() Source {
	source := progressiveVideoTestSource()
	source.Path, source.Info.Container = "/media/remux.mp4", "mp4"
	source.Info.ProbeVersion = media.CurrentProbeVersion
	source.Info.DurationTicks, source.Info.FormatStartTicks = 120*media.TicksPerSecond, 0
	source.Info.Streams = source.Info.Streams[:2]
	video, audio := &source.Info.Streams[0], &source.Info.Streams[1]
	video.Index, video.Width, video.Height = 0, 128, 72
	video.Profile, video.TimeBase = "Constrained Baseline", "1/12288"
	video.AverageFrameRate, video.RealFrameRate = "12/1", "12/1"
	video.IsAVC, video.IsAVCKnown = true, true
	audio.Index, audio.SampleRate, audio.Channels, audio.TimeBase = 1, 48000, 1, "1/48000"
	proof := media.VideoCopySeekAudio{StreamIndex: 1, Codec: "aac", TimeBaseNumerator: 1, TimeBaseDenominator: 48000,
		SampleRate: 48000, Channels: 1, PTS: 1152000, Duration: 1024, PacketSHA256: strings.Repeat("f", 64)}
	source.Info.VideoSeekIndexes = []media.VideoSeekIndex{{
		Version: media.VideoSeekIndexVersion, StreamIndex: 0, DurationTicks: source.Info.DurationTicks,
		TimeBaseNumerator: 1, TimeBaseDenominator: 12288, SourceIdentity: strings.Repeat("a", 64),
		ToolIdentity: strings.Repeat("b", 64), ParameterSetsSHA256: strings.Repeat("c", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: 128, Height: 72, PixelFormat: "yuv420p", DecodedFrameBytes: 13824,
		Entries: []media.VideoSeekPoint{
			{PTS: 294912, DTS: 294912, CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64), Audio: []media.VideoCopySeekAudio{proof}},
			{PTS: 368640, DTS: 368640, CodedSHA256: strings.Repeat("1", 64), DecodedSHA256: strings.Repeat("2", 64)},
		},
	}}
	return source
}

func TestProgressiveVideoJointSeekRequiresAlignmentAndKeepsFullRemux(t *testing.T) {
	source := progressiveVideoJointSeekTestSource()
	request := ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "copy", AudioCodec: "copy",
		StartTimeTicks: 30 * media.TicksPerSecond}
	limits := ConversionLimits{AllowRemux: true}
	progressiveVideoTestDeclined(t, source, request, limits)
	request.AllowVideoSeekAlignment = true
	decision := progressiveVideoTestPlan(t, source, request, limits)
	candidate, err := media.ValidateVideoCopySeekCandidate(decision.Plan.VideoCopySeekCandidate)
	if err != nil || decision.Method != "DirectStream" || decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "copy" ||
		decision.Plan.StartTicks != 24*media.TicksPerSecond || !decision.Plan.CopyTimestamps ||
		candidate.OriginalRequestedStartTicks != request.StartTimeTicks || candidate.Audio == nil ||
		candidate.Audio.PTS != 1152000 || decision.OutputSource.Info.DurationTicks != source.Info.DurationTicks {
		t.Fatalf("an aligned remux lost its original request, copied audio, or source clock: %+v, %+v, %v", decision, candidate, err)
	}
	request.StartTimeTicks = 35 * media.TicksPerSecond
	progressiveVideoTestDeclined(t, source, request, limits)
}

func TestVideoProfileAndDirectURLChooseSameJointCopySeek(t *testing.T) {
	source := progressiveVideoJointSeekTestSource()
	before, _ := json.Marshal(source)
	limits := ConversionLimits{AllowRemux: true}
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.StartTimeTicks = profileTestPtr(int64(30 * media.TicksPerSecond))
	request.AllowVideoSeekAlignment = profileTestPtr(true)
	direct := ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "copy", AudioCodec: "copy",
		StartTimeTicks: *request.StartTimeTicks, AllowVideoSeekAlignment: true}
	want := progressiveVideoTestPlan(t, source, direct, limits)
	got := videoProfilesTestPlan(t, source, request, limits, "http", 0)
	if got.Plan.VideoCodec != "copy" || got.Plan.AudioCodec != "copy" || got.Method != "DirectStream" ||
		got.Plan.StartTicks != want.Plan.StartTicks || got.Plan.CopyTimestamps != want.Plan.CopyTimestamps ||
		got.Plan.VideoCopySeekCandidate != want.Plan.VideoCopySeekCandidate {
		t.Fatalf("profile reduction discarded the shared audio boundary: got %+v, want %+v", got.Plan, want.Plan)
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("profile candidate reduction mutated the source index")
	}
	request.AllowVideoSeekAlignment = profileTestPtr(false)
	videoProfilesTestDeclined(t, source, request, limits)
	request.AllowVideoSeekAlignment = profileTestPtr(true)
	request.StartTimeTicks = profileTestPtr(int64(35 * media.TicksPerSecond))
	videoProfilesTestDeclined(t, source, request, limits)
}

func TestVideoProfileJointReductionKeepsExactVideoForEncodedAudio(t *testing.T) {
	source := progressiveVideoJointSeekTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.StartTimeTicks = profileTestPtr(int64(30 * media.TicksPerSecond))
	request.AllowVideoSeekAlignment, request.AllowAudioStreamCopy = profileTestPtr(true), profileTestPtr(false)
	decision := videoProfilesTestPlan(t, source, request, ConversionLimits{AllowRemux: true, AllowAudioTranscode: true}, "http", 0)
	candidate, err := media.ValidateVideoCopySeekCandidate(decision.Plan.VideoCopySeekCandidate)
	if err != nil || decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" ||
		decision.Plan.StartTicks != *request.StartTimeTicks || candidate.Audio != nil || candidate.OriginalRequestedStartTicks != 0 {
		t.Fatalf("the optional shared boundary displaced the exact independent-audio path: %+v, %+v, %v", decision.Plan, candidate, err)
	}
}
