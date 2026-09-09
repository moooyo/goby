package playback

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func progressiveVideoSeekTestSource() Source {
	source := progressiveVideoTestSource()
	source.Info.ProbeVersion = media.CurrentProbeVersion
	source.Info.Streams[0].TimeBase = "1/1000"
	video := source.Info.Streams[0]
	source.Info.VideoSeekIndexes = []media.VideoSeekIndex{{
		Version: media.VideoSeekIndexVersion, StreamIndex: video.Index, FormatStartTicks: source.Info.FormatStartTicks,
		DurationTicks: source.Info.DurationTicks, TimeBaseNumerator: 1, TimeBaseDenominator: 1000,
		SourceIdentity: strings.Repeat("a", 64), ToolIdentity: strings.Repeat("b", 64), ParameterSetsSHA256: strings.Repeat("c", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: video.Width, Height: video.Height, PixelFormat: video.PixelFormat,
		DecodedFrameBytes: int64(video.Width) * int64(video.Height) * 3 / 2,
		Entries: []media.VideoSeekPoint{
			{PTS: 1400, DTS: 1320, CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64)},
			{PTS: 3400, DTS: 3320, CodedSHA256: strings.Repeat("f", 64), DecodedSHA256: strings.Repeat("1", 64)},
			{PTS: 5400, DTS: 5320, CodedSHA256: strings.Repeat("2", 64), DecodedSHA256: strings.Repeat("3", 64)},
		},
	}}
	return source
}

func progressiveVideoSeekTestRequest() ProgressiveVideoRequest {
	request := progressiveVideoTestRequest()
	request.StartTimeTicks = 43_700_000
	return request
}

func TestPlanProgressiveVideoSelectsOwnedPrivateSeekCandidate(t *testing.T) {
	source, request := progressiveVideoSeekTestSource(), progressiveVideoSeekTestRequest()
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	want, err := media.SelectVideoSeekCandidate(source.Info.VideoSeekIndexes[0], request.StartTimeTicks)
	if err != nil {
		t.Fatal(err)
	}
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoSeekCandidate != want || decision.Plan.VideoCodec != "h264" {
		t.Fatal("the selected encoded video did not retain its matching scan candidate")
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) || len(decision.OutputSource.Info.VideoSeekIndexes) != 0 {
		t.Fatal("planning mutated catalog evidence or projected it into output media")
	}
	encoded, err := json.Marshal(decision.Plan)
	if err != nil || len(encoded) > 128*1024 {
		t.Fatalf("private plan exceeds its serialization contract: %d, %v", len(encoded), err)
	}
	var restored transcode.Plan
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != *decision.Plan {
		t.Fatalf("private plan lost comparable roundtrip equality: %v", err)
	}
	source.Info.VideoSeekIndexes[0].Entries[2].DecodedSHA256 = strings.Repeat("9", 64)
	if decision.Plan.VideoSeekCandidate != want {
		t.Fatal("accepted plan aliases mutable catalog evidence")
	}
}

func TestPlanProgressiveVideoUnusableSeekEvidenceKeepsLinearPlayback(t *testing.T) {
	for name, change := range map[string]func(*Source){
		"absent":           func(s *Source) { s.Info.VideoSeekIndexes = nil },
		"older probe":      func(s *Source) { s.Info.ProbeVersion-- },
		"newer probe":      func(s *Source) { s.Info.ProbeVersion++ },
		"wrong stream":     func(s *Source) { s.Info.VideoSeekIndexes[0].StreamIndex++ },
		"wrong duration":   func(s *Source) { s.Info.VideoSeekIndexes[0].DurationTicks++ },
		"wrong clock":      func(s *Source) { s.Info.VideoSeekIndexes[0].FormatStartTicks++ },
		"wrong dimensions": func(s *Source) { s.Info.VideoSeekIndexes[0].Width += 2 },
		"wrong format":     func(s *Source) { s.Info.VideoSeekIndexes[0].PixelFormat = "yuv420p10le" },
		"wrong time base":  func(s *Source) { s.Info.VideoSeekIndexes[0].TimeBaseDenominator++ },
		"invalid hash":     func(s *Source) { s.Info.VideoSeekIndexes[0].SourceIdentity = "unverified" },
		"unchecked scope":  func(s *Source) { s.Info.VideoSeekIndexes[0].NALScopeChecked = false },
		"duplicate stream": func(s *Source) { s.Info.VideoSeekIndexes = append(s.Info.VideoSeekIndexes, s.Info.VideoSeekIndexes[0]) },
		"different source": func(s *Source) {
			other := s.Info.VideoSeekIndexes[0]
			other.StreamIndex, other.SourceIdentity = 9, strings.Repeat("8", 64)
			s.Info.VideoSeekIndexes = append(s.Info.VideoSeekIndexes, other)
		},
		"excess indexes": func(s *Source) {
			s.Info.VideoSeekIndexes = make([]media.VideoSeekIndex, maxSourceStreams+1)
		},
		"excess entries": func(s *Source) {
			s.Info.VideoSeekIndexes[0].Entries = make([]media.VideoSeekPoint, media.MaxVideoSeekEntries+1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			source, request := progressiveVideoSeekTestSource(), progressiveVideoSeekTestRequest()
			change(&source)
			linear := source
			linear.Info.VideoSeekIndexes = nil
			want := progressiveVideoTestPlan(t, linear, request, conversionTestLimits())
			got := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
			if !reflect.DeepEqual(got, want) || got.Plan.VideoSeekCandidate != "" {
				t.Fatal("optional unusable evidence changed a playable linear conversion")
			}
		})
	}
}

func TestPlanProgressiveVideoSeekUsesSelectedSourceStreamAndSoftwareDecoder(t *testing.T) {
	source, request := progressiveVideoSeekTestSource(), progressiveVideoSeekTestRequest()
	other := source.Info.Streams[0]
	other.Index, other.IsDefault = 17, false
	source.Info.Streams = append(source.Info.Streams, other)
	request.VideoStreamIndex = &other.Index
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoStreamIndex != other.Index || decision.Plan.VideoSeekCandidate != "" {
		t.Fatal("evidence for another video track entered the plan")
	}
	request.VideoStreamIndex = nil
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi"}
	decision = progressiveVideoTestPlan(t, source, request, limits)
	if decision.Plan.VideoSeekCandidate != "" || decision.Plan.Hardware != limits.Hardware {
		t.Fatal("hardware decoding must retain its existing linear path")
	}
	limits.Hardware = transcode.Hardware{Encode: "vaapi"}
	decision = progressiveVideoTestPlan(t, source, request, limits)
	if decision.Plan.VideoSeekCandidate == "" {
		t.Fatal("independent hardware encoding must not exclude software decoder evidence")
	}
	request.StartTimeTicks = 0
	decision = progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoSeekCandidate != "" {
		t.Fatal("zero-start video copy must not carry seek evidence")
	}
}

func TestPlanVideoConversionAndURLPlanningSelectTheSameSeekCandidate(t *testing.T) {
	source := progressiveVideoSeekTestSource()
	request := videoProfilesTestRequest(videoProfilesTestProfile("http", "mp4"))
	start := progressiveVideoSeekTestRequest().StartTimeTicks
	request.StartTimeTicks = &start
	decision := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "http", 0)
	want := progressiveVideoTestPlan(t, source, progressiveVideoSeekTestRequest(), conversionTestLimits())
	if decision.Plan.VideoSeekCandidate == "" || decision.Plan.VideoSeekCandidate != want.Plan.VideoSeekCandidate {
		t.Fatal("profile negotiation and direct URL planning selected different private evidence")
	}
	request.DeviceProfile.TranscodingProfiles = []TranscodingProfile{videoProfilesTestProfile("hls", "ts")}
	hls := videoProfilesTestPlan(t, source, request, conversionTestLimits(), "hls", 0)
	if hls.Plan.VideoSeekCandidate != "" {
		t.Fatal("progressive restart evidence entered an HLS plan")
	}
}

func TestPlanProgressiveVideoLargeIndexRetainsOnlyBoundedCandidateWindow(t *testing.T) {
	source, request := progressiveVideoSeekTestSource(), progressiveVideoSeekTestRequest()
	source.Info.DurationTicks = 1000 * media.TicksPerSecond
	index := &source.Info.VideoSeekIndexes[0]
	index.DurationTicks = source.Info.DurationTicks
	index.Entries = make([]media.VideoSeekPoint, 2000)
	for position := range index.Entries {
		pts := int64(1400 + position*250)
		index.Entries[position] = media.VideoSeekPoint{PTS: pts, DTS: pts - 80,
			CodedSHA256: strings.Repeat("d", 64), DecodedSHA256: strings.Repeat("e", 64)}
	}
	request.StartTimeTicks = 500 * media.TicksPerSecond
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	candidate, err := media.ValidateVideoSeekCandidate(decision.Plan.VideoSeekCandidate)
	if err != nil || len(candidate.Index.Entries) != 64 || len(index.Entries) != 2000 {
		t.Fatalf("large scan evidence did not retain a private bounded candidate: %v", err)
	}
	encoded, err := json.Marshal(decision.Plan)
	if err != nil || len(encoded) > 128*1024 {
		t.Fatalf("large scan evidence escaped the private plan budget: %d, %v", len(encoded), err)
	}
}
