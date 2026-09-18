package playback

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func progressiveVideoCopySeekTestSource() Source {
	source := progressiveVideoSeekTestSource()
	for i := range source.Info.VideoSeekIndexes[0].Entries {
		source.Info.VideoSeekIndexes[0].Entries[i].DTS = source.Info.VideoSeekIndexes[0].Entries[i].PTS
	}
	return source
}

func TestProgressiveVideoCopySeekSelectsExactIDRAndLinearAAC(t *testing.T) {
	source, request := progressiveVideoCopySeekTestSource(), progressiveVideoTestRequest()
	request.StartTimeTicks = 20_000_000
	before, _ := json.Marshal(source)
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" || decision.Plan.VideoCopySeekCandidate == "" || decision.Plan.VideoSeekCandidate != "" || decision.Method != "Transcode" {
		t.Fatalf("copy seek did not retain video packets and independently trim encoded audio: %+v", decision)
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(decision.Plan.VideoCopySeekCandidate)
	if err != nil || candidate.RequestedStartTicks != request.StartTimeTicks || len(candidate.Index.Entries) != 1 || candidate.Index.Entries[0].PTS != candidate.Index.Entries[0].DTS {
		t.Fatalf("copy seek lacks its exact packet boundary: %+v, %v", candidate, err)
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) || len(decision.OutputSource.Info.VideoSeekIndexes) != 0 {
		t.Fatal("copy planning mutated source evidence or exposed it in output metadata")
	}
	if decision.OutputSource.Info.DurationTicks != source.Info.DurationTicks-request.StartTimeTicks {
		t.Fatal("copy seek projected the whole source instead of its requested window")
	}
}

func TestProgressiveVideoCopySeekFallsBackWithoutPacketProof(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *ProgressiveVideoRequest)
	}{
		{"between IDRs", func(_ *Source, r *ProgressiveVideoRequest) { r.StartTimeTicks++ }},
		{"decode pre-roll", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.VideoSeekIndexes[0].Entries[1].DTS-- }},
		{"missing scan", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.VideoSeekIndexes = nil }},
		{"outdated probe", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.ProbeVersion-- }},
		{"wrong dimensions", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.VideoSeekIndexes[0].Width += 2 }},
		{"duplicate stream", func(s *Source, _ *ProgressiveVideoRequest) {
			s.Info.VideoSeekIndexes = append(s.Info.VideoSeekIndexes, s.Info.VideoSeekIndexes[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := progressiveVideoCopySeekTestSource(), progressiveVideoTestRequest()
			request.StartTimeTicks = 20_000_000
			test.mutate(&source, &request)
			decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
			if decision.Plan.VideoCodec != "h264" || decision.Plan.VideoCopySeekCandidate != "" {
				t.Fatalf("unproven copied packets replaced the precise encoded fallback: %+v", decision.Plan)
			}
			request.VideoCodec = "copy"
			progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
		})
	}
}

func TestProgressiveVideoCopySeekRetainsIndependentAudioPermissions(t *testing.T) {
	source, request := progressiveVideoCopySeekTestSource(), progressiveVideoTestRequest()
	request.StartTimeTicks, request.VideoCodec = 20_000_000, "copy"
	limits := conversionTestLimits()
	limits.AllowVideoTranscode = false
	decision := progressiveVideoTestPlan(t, source, request, limits)
	if decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" {
		t.Fatalf("video-copy authorization did not preserve independent audio encoding: %+v", decision.Plan)
	}
	limits.AllowAudioTranscode = false
	progressiveVideoTestDeclined(t, source, request, limits)
	limits.AllowAudioTranscode = true
	request.AudioCodec = "copy"
	progressiveVideoTestDeclined(t, source, request, limits)
	source.Info.Streams = source.Info.Streams[:1]
	request.AudioCodec = "none"
	decision = progressiveVideoTestPlan(t, source, request, limits)
	if decision.Plan.AudioStreamIndex != -1 || decision.Method != "DirectStream" || decision.Plan.VideoCopySeekCandidate == "" {
		t.Fatalf("video-only copy seek invented an audio transform: %+v", decision)
	}
}

func TestVideoProfileAndURLPlanningChooseSameCopySeekContract(t *testing.T) {
	source := progressiveVideoCopySeekTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	request.StartTimeTicks = profileTestPtr(int64(20_000_000))
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	direct := progressiveVideoTestRequest()
	direct.StartTimeTicks = *request.StartTimeTicks
	want := progressiveVideoTestPlan(t, source, direct, conversionTestLimits())
	if decision.Plan.VideoCopySeekCandidate == "" || decision.Plan.VideoCopySeekCandidate != want.Plan.VideoCopySeekCandidate || decision.Plan.AudioCodec != "aac" {
		t.Fatalf("profile search changed the packet-copy or audio contract: %+v, %+v", decision.Plan, want.Plan)
	}
}
