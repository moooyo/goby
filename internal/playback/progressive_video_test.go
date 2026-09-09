package playback

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func progressiveVideoTestSource() Source {
	source := conversionTestSource()
	source.Info.Container, source.Path = "matroska", "/media/movie.mkv"
	source.Info.FormatStartKnown, source.Info.FormatStartTicks = true, 14_000_000
	source.Info.Size = 45_000_000
	source.Info.Streams[0].IsAVC, source.Info.Streams[0].IsAVCKnown = false, true
	source.Info.Streams[0].RefFrames = 4
	source.Info.Streams[0].VideoRange, source.Info.Streams[0].VideoRangeKnown = "SDR", true
	source.Info.Streams[0].CodecTag, source.Info.Streams[0].CodecTagString = "source-tag", "source-tag"
	source.Info.Streams[1].Profile, source.Info.Streams[1].BitDepth = "LC", 16
	source.Info.Streams[2].SampleRate = 48_000
	return source
}

func progressiveVideoTestRequest() ProgressiveVideoRequest {
	return ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "h264", AudioCodec: "aac"}
}

func progressiveVideoTestPlan(t *testing.T, source Source, request ProgressiveVideoRequest, limits ConversionLimits) ProgressiveVideoDecision {
	t.Helper()
	decision, err := PlanProgressiveVideo(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("expected an executable progressive video plan, got %+v, %v", decision, err)
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		t.Fatalf("planner produced a plan rejected by the runner: %v", err)
	}
	return decision
}

func progressiveVideoTestDeclined(t *testing.T, source Source, request ProgressiveVideoRequest, limits ConversionLimits) ProgressiveVideoDecision {
	t.Helper()
	decision, err := PlanProgressiveVideo(source, request, limits)
	if err != nil || decision.Plan != nil || len(decision.Reasons) == 0 {
		t.Fatalf("expected an explained unsupported decision, got %+v, %v", decision, err)
	}
	return decision
}

func TestPlanProgressiveVideoRemuxUsesActualMP4FactsAndSourceIndexes(t *testing.T) {
	source := progressiveVideoTestSource()
	decision := progressiveVideoTestPlan(t, source, progressiveVideoTestRequest(), ConversionLimits{AllowRemux: true})
	plan, output := decision.Plan, decision.OutputSource
	if decision.Method != "DirectStream" || len(decision.Reasons) != 0 || plan.OutputMode != "progressive" || plan.Container != "mp4" ||
		plan.VideoCodec != "copy" || plan.AudioCodec != "copy" || plan.VideoStreamIndex != 2 || plan.AudioStreamIndex != 5 ||
		plan.Width != 0 || plan.Height != 0 || plan.FrameRate != 0 || plan.VideoBitrate != 0 || plan.AudioBitrate != 0 ||
		plan.AudioChannels != 0 || plan.AudioSampleRate != 0 || plan.Hardware != (transcode.Hardware{}) {
		t.Fatalf("unexpected remux plan: %+v", decision)
	}
	if output.Path != "output.mp4" || output.Info.Container != "mp4" || output.Info.Bitrate != 3_692_000 || output.Info.Size != 0 ||
		output.ItemID != source.ItemID || output.MediaSourceID != source.MediaSourceID || len(output.Info.Streams) != 2 {
		t.Fatalf("unexpected MP4 output projection: %+v", output)
	}
	video, audio := output.Info.Streams[0], output.Info.Streams[1]
	if video.Index != 0 || video.Codec != "h264" || !video.IsAVC || !video.IsAVCKnown || video.CodecTag != "avc1" || video.CodecTagString != "avc1" ||
		video.TimeBase != "" || video.Profile != "High" || video.RefFrames != 4 || audio.Index != 1 || audio.Codec != "aac" ||
		audio.CodecTag != "mp4a" || audio.CodecTagString != "mp4a" || audio.TimeBase != "" || audio.Profile != "LC" {
		t.Fatalf("copied codec facts or MP4 framing were misrepresented: %+v", output.Info.Streams)
	}
}

func TestPlanProgressiveVideoPermissionsAndIndependentCopyFlags(t *testing.T) {
	for _, test := range []struct {
		name         string
		mutate       func(*ProgressiveVideoRequest, *ConversionLimits)
		video, audio string
		declined     bool
	}{
		{"no permissions", func(_ *ProgressiveVideoRequest, l *ConversionLimits) { *l = ConversionLimits{} }, "", "", true},
		{"only remux", func(_ *ProgressiveVideoRequest, l *ConversionLimits) { *l = ConversionLimits{AllowRemux: true} }, "copy", "copy", false},
		{"audio conversion without remux", func(_ *ProgressiveVideoRequest, l *ConversionLimits) {
			*l = ConversionLimits{AllowAudioTranscode: true}
		}, "copy", "aac", false},
		{"video conversion without remux", func(_ *ProgressiveVideoRequest, l *ConversionLimits) {
			*l = ConversionLimits{AllowVideoTranscode: true}
		}, "h264", "copy", false},
		{"video copy disabled", func(r *ProgressiveVideoRequest, _ *ConversionLimits) { r.AllowVideoStreamCopy = profileTestPtr(false) }, "h264", "copy", false},
		{"audio copy disabled", func(r *ProgressiveVideoRequest, _ *ConversionLimits) { r.AllowAudioStreamCopy = profileTestPtr(false) }, "copy", "aac", false},
		{"all copy disabled", func(r *ProgressiveVideoRequest, _ *ConversionLimits) {
			r.AllowVideoStreamCopy, r.AllowAudioStreamCopy = profileTestPtr(false), profileTestPtr(false)
		}, "h264", "aac", false},
		{"video codec copy cannot encode", func(r *ProgressiveVideoRequest, _ *ConversionLimits) {
			r.VideoCodec, r.AllowVideoStreamCopy = "copy", profileTestPtr(false)
		}, "", "", true},
		{"audio codec copy cannot encode", func(r *ProgressiveVideoRequest, _ *ConversionLimits) {
			r.AudioCodec, r.AllowAudioStreamCopy = "copy", profileTestPtr(false)
		}, "", "", true},
		{"copy flag cannot authorize video encoder", func(r *ProgressiveVideoRequest, l *ConversionLimits) {
			r.AllowVideoStreamCopy, l.AllowVideoTranscode = profileTestPtr(false), false
		}, "", "", true},
		{"copy flag cannot authorize audio encoder", func(r *ProgressiveVideoRequest, l *ConversionLimits) {
			r.AllowAudioStreamCopy, l.AllowAudioTranscode = profileTestPtr(false), false
		}, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, limits := progressiveVideoTestRequest(), conversionTestLimits()
			test.mutate(&request, &limits)
			if test.declined {
				progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, limits)
				return
			}
			decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
			if decision.Plan.VideoCodec != test.video || decision.Plan.AudioCodec != test.audio {
				t.Fatalf("copy/encoder authorization changed: %+v", decision.Plan)
			}
		})
	}
}

func TestPlanProgressiveVideoSeekRequiresVideoEncodingAndUsesContainerClock(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	request.StartTimeTicks = 12_345_678
	progressiveVideoTestDeclined(t, source, request, ConversionLimits{AllowRemux: true, AllowAudioTranscode: true})
	for _, origin := range []int64{0, -14_000_000, 14_000_000, -progressiveMaxDuration, progressiveMaxDuration} {
		source.Info.FormatStartTicks = origin
		decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
		plan := decision.Plan
		if plan.VideoCodec != "h264" || plan.AudioCodec != "copy" || !plan.SourceFormatStartKnown || plan.SourceFormatStartTicks != origin ||
			plan.StartTicks != request.StartTimeTicks || plan.DurationTicks != source.Info.DurationTicks ||
			decision.OutputSource.Info.DurationTicks != source.Info.DurationTicks-request.StartTimeTicks {
			t.Fatalf("seek used copy or changed the source format clock: %+v", decision)
		}
	}
	request.VideoCodec = "copy"
	progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveVideoDeclinesMissingOrUnboundedTiming(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source, *ProgressiveVideoRequest)
	}{
		{"unknown origin", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.FormatStartKnown = false }},
		{"origin too large", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.FormatStartTicks = progressiveMaxDuration + 1 }},
		{"origin too negative", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.FormatStartTicks = -progressiveMaxDuration - 1 }},
		{"zero duration", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.DurationTicks = 0 }},
		{"long duration", func(s *Source, _ *ProgressiveVideoRequest) { s.Info.DurationTicks = progressiveMaxDuration + 1 }},
		{"end seek", func(s *Source, r *ProgressiveVideoRequest) { r.StartTimeTicks = s.Info.DurationTicks }},
		{"past end seek", func(s *Source, r *ProgressiveVideoRequest) { r.StartTimeTicks = s.Info.DurationTicks + 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
			test.mutate(&source, &request)
			progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
		})
	}
}

func TestPlanProgressiveVideoSelectsActualVideoAndAudioIndexes(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	alternate := source.Info.Streams[0]
	alternate.Index, alternate.Width, alternate.Height, alternate.Bitrate = 18, 1280, 720, 2_000_000
	source.Info.Streams = append(source.Info.Streams, alternate, media.Stream{Index: 0, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true})
	request.VideoStreamIndex, request.AudioStreamIndex = profileTestPtr(18), profileTestPtr(9)
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoStreamIndex != 18 || decision.Plan.AudioStreamIndex != 9 || decision.Plan.VideoCodec != "copy" || decision.Plan.AudioCodec != "aac" ||
		decision.Plan.AudioChannels != 6 || decision.OutputSource.Info.Streams[0].Width != 1280 || decision.OutputSource.Info.Streams[1].Channels != 6 {
		t.Fatalf("selected tracks were replaced by source defaults: %+v", decision)
	}
	for _, index := range []int{0, 5, 99} {
		request.VideoStreamIndex = &index
		if decision, err := PlanProgressiveVideo(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) || decision.Plan != nil {
			t.Fatalf("non-video or nonexistent selection accepted: %+v, %v", decision, err)
		}
	}
}

func TestPlanProgressiveVideoKeepsExactGeometrySeparateFromCeilings(t *testing.T) {
	for _, test := range []struct {
		name                  string
		width, height         *int
		maxWidth, maxHeight   *int
		wantWidth, wantHeight int
		declined              bool
	}{
		{"one exact width", profileTestPtr(1280), nil, nil, nil, 1280, 720, false},
		{"one exact height", nil, profileTestPtr(720), nil, nil, 1280, 720, false},
		{"two exact dimensions", profileTestPtr(640), profileTestPtr(480), nil, nil, 640, 480, false},
		{"proportional maximum", nil, nil, profileTestPtr(1000), profileTestPtr(600), 1000, 562, false},
		{"exact width above client ceiling", profileTestPtr(1280), nil, profileTestPtr(1000), nil, 0, 0, true},
		{"exact height above client ceiling", nil, profileTestPtr(720), nil, profileTestPtr(600), 0, 0, true},
		{"exact exceeds server limit", profileTestPtr(3840), profileTestPtr(2160), nil, nil, 0, 0, true},
		{"odd exact geometry", profileTestPtr(1279), nil, nil, nil, 0, 0, true},
		{"subpixel client ceiling", nil, nil, profileTestPtr(1), nil, 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := progressiveVideoTestRequest()
			request.Width, request.Height, request.MaxWidth, request.MaxHeight = test.width, test.height, test.maxWidth, test.maxHeight
			if test.declined {
				progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
				return
			}
			decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, conversionTestLimits())
			if decision.Plan.VideoCodec != "h264" || decision.Plan.Width != test.wantWidth || decision.Plan.Height != test.wantHeight ||
				decision.OutputSource.Info.Streams[0].Width != test.wantWidth || decision.OutputSource.Info.Streams[0].Height != test.wantHeight {
				t.Fatalf("geometry target was silently changed: %+v", decision)
			}
		})
	}
}

func TestPlanProgressiveVideoFrameRateRequiresRealConversion(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	request.FrameRate, request.MaxFrameRate = profileTestPtr(24.0), profileTestPtr(30.0)
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.FrameRate != 24 || decision.OutputSource.Info.Streams[0].AverageFrameRate != "24" ||
		decision.OutputSource.Info.Streams[0].RealFrameRate != "24" {
		t.Fatalf("explicit frame rate was treated as a copy property: %+v", decision)
	}
	request.FrameRate = profileTestPtr(30.0)
	request.MaxFrameRate = profileTestPtr(24.0)
	progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
	request.FrameRate = nil
	decision = progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.FrameRate != 24 {
		t.Fatalf("frame-rate ceiling did not construct a bounded output: %+v", decision.Plan)
	}
	request.MaxFrameRate = profileTestPtr(30.0)
	decision = progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "copy" || decision.OutputSource.Info.Streams[0].AverageFrameRate != "30000/1001" {
		t.Fatalf("verified copied frame rate was discarded: %+v", decision)
	}
	request.MaxFrameRate, request.AllowVideoStreamCopy = nil, profileTestPtr(false)
	decision = progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.FrameRate != 0 || decision.OutputSource.Info.Streams[0].AverageFrameRate != "" || decision.OutputSource.Info.Streams[0].RealFrameRate != "" {
		t.Fatalf("timestamp-preserving encoding invented an exact output frame rate: %+v", decision)
	}
	for _, value := range []float64{0.5, 241, 23.1234567} {
		request.FrameRate = &value
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
	}
}

func TestPlanProgressiveVideoUnknownCopiedFactsStayUnknown(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	source.Info.Streams[0].Bitrate, source.Info.Streams[1].Bitrate = 0, 0
	source.Info.Streams[0].AverageFrameRate, source.Info.Streams[0].RealFrameRate = "", ""
	decision := progressiveVideoTestPlan(t, source, request, ConversionLimits{AllowRemux: true})
	if decision.OutputSource.Info.Streams[0].Bitrate != 0 || decision.OutputSource.Info.Streams[1].Bitrate != 0 ||
		decision.OutputSource.Info.Bitrate != 2*source.Info.Bitrate || decision.OutputSource.Info.Streams[0].AverageFrameRate != "" {
		t.Fatalf("container budget was advertised as measured per-stream data: %+v", decision.OutputSource)
	}
	request.MaxFrameRate = profileTestPtr(30.0)
	progressiveVideoTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
	decision = progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" || decision.Plan.FrameRate != 30 {
		t.Fatalf("unknown copied frame rate bypassed a ceiling: %+v", decision)
	}
	request.MaxFrameRate = nil
	source.Info.Bitrate = 0
	progressiveVideoTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
}

func TestPlanProgressiveVideoAllocatesMediaBitrateWithoutStartupTax(t *testing.T) {
	for _, start := range []int64{0, 2 * media.TicksPerSecond} {
		source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
		source.Info.DurationTicks = 6 * media.TicksPerSecond
		request.StartTimeTicks, request.MaxBitrate = start, profileTestPtr(int64(1_128_000))
		request.VideoBitrate, request.AudioBitrate = profileTestPtr(int64(1_000_000)), profileTestPtr(int64(128_000))
		request.MaxVideoBitrate, request.MaxAudioBitrate = profileTestPtr(int64(1_000_000)), profileTestPtr(int64(128_000))
		decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
		if decision.Plan.VideoBitrate != 1_000_000 || decision.Plan.AudioBitrate != 128_000 || decision.OutputSource.Info.Bitrate != 1_128_000 {
			t.Fatalf("exact media targets were reduced by fictional container bandwidth: %+v", decision)
		}
		request.MaxBitrate = profileTestPtr(int64(1_127_999))
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
		request.MaxBitrate, request.MaxAudioBitrate = profileTestPtr(int64(1_128_000)), profileTestPtr(int64(127_999))
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
		request.MaxAudioBitrate, request.MaxVideoBitrate = profileTestPtr(int64(128_000)), profileTestPtr(int64(999_999))
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
	}
	request := progressiveVideoTestRequest()
	request.MaxBitrate = profileTestPtr(int64(256_000))
	request.AllowAudioStreamCopy = profileTestPtr(false)
	decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, conversionTestLimits())
	if decision.Plan.VideoBitrate != 64_000 || decision.Plan.AudioBitrate != 192_000 || decision.OutputSource.Info.Bitrate != 256_000 {
		t.Fatalf("automatic allocation ignored the combined video/audio budget: %+v", decision)
	}
}

func TestPlanProgressiveVideoAudioTargetsAreExactAndUseSelectedFacts(t *testing.T) {
	request := progressiveVideoTestRequest()
	request.AudioStreamIndex = profileTestPtr(9)
	request.AudioChannels, request.AudioSampleRate, request.AudioBitrate = profileTestPtr(8), profileTestPtr(96_000), profileTestPtr(int64(768_000))
	request.MaxAudioChannels, request.MaxSampleRate = profileTestPtr(8), profileTestPtr(96_000)
	decision := progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, conversionTestLimits())
	if decision.Plan.AudioChannels != 8 || decision.Plan.AudioSampleRate != 96_000 || decision.Plan.AudioBitrate != 768_000 ||
		decision.OutputSource.Info.Streams[1].Channels != 8 || decision.OutputSource.Info.Streams[1].SampleRate != 96_000 ||
		decision.OutputSource.Info.Streams[1].Profile != "LC" || decision.OutputSource.Info.Streams[1].BitDepth != 0 {
		t.Fatalf("AAC upmix/resample targets were weakened or fabricated: %+v", decision)
	}
	request.MaxAudioChannels = profileTestPtr(6)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	request.MaxAudioChannels, request.MaxSampleRate = profileTestPtr(8), profileTestPtr(48_000)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	request.MaxSampleRate, request.AudioSampleRate = profileTestPtr(96_000), profileTestPtr(12_345)
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	request.AudioSampleRate, request.AudioChannels, request.AudioBitrate = profileTestPtr(8_000), profileTestPtr(1), profileTestPtr(int64(48_001))
	progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
}

func TestPlanProgressiveVideoRejectsUnimplementedSourceTransforms(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Source)
	}{
		{"interlaced", func(s *Source) { s.Info.Streams[0].IsInterlaced = true }},
		{"PQ", func(s *Source) { s.Info.Streams[0].ColorTransfer = "smpte2084" }},
		{"HLG", func(s *Source) { s.Info.Streams[0].ColorTransfer = "arib-std-b67" }},
		{"declared HDR", func(s *Source) { s.Info.Streams[0].VideoRange = "HDR10" }},
		{"unknown high depth range", func(s *Source) { s.Info.Streams[0].BitDepth, s.Info.Streams[0].VideoRangeKnown = 10, false }},
		{"external video", func(s *Source) { s.Info.Streams[0].IsExternal = true }},
		{"external audio", func(s *Source) { s.Info.Streams[1].IsExternal = true }},
		{"missing width", func(s *Source) { s.Info.Streams[0].Width = 0 }},
		{"missing audio sample rate", func(s *Source) { s.Info.Streams[1].SampleRate = 0 }},
		{"negative audio depth", func(s *Source) { s.Info.Streams[1].BitDepth = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := progressiveVideoTestSource()
			test.mutate(&source)
			progressiveVideoTestDeclined(t, source, progressiveVideoTestRequest(), conversionTestLimits())
		})
	}
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	source.Info.Streams[0].InterlaceKnown = false
	request.AllowInterlacedVideoStreamCopy = profileTestPtr(false)
	progressiveVideoTestDeclined(t, source, request, ConversionLimits{AllowRemux: true})
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.VideoCodec != "h264" {
		t.Fatalf("unknown copied interlace status bypassed an explicit restriction: %+v", decision)
	}
}

func TestPlanProgressiveVideoNoAudioIsNotSilentTrackDropping(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	for _, codec := range []string{"", "none"} {
		request.AudioCodec = codec
		progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
	}
	source.Info.Streams = source.Info.Streams[:1]
	for _, codec := range []string{"", "none", "aac", "copy"} {
		request.AudioCodec = codec
		decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
		if decision.Plan.AudioStreamIndex != -1 || decision.Plan.AudioCodec != "" || decision.Plan.AudioChannels != 0 || len(decision.OutputSource.Info.Streams) != 1 {
			t.Fatalf("video-only source invented audio: %+v", decision)
		}
	}
	request.AudioChannels = profileTestPtr(2)
	progressiveVideoTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanProgressiveVideoEncodingDoesNotInventUnknownCodecFacts(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	source.Info.Streams[0].BitDepth, source.Info.Streams[0].PixelFormat = 10, "yuv420p10le"
	request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = profileTestPtr(false), profileTestPtr(false)
	limits := conversionTestLimits()
	limits.Hardware = transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	decision := progressiveVideoTestPlan(t, source, request, limits)
	video, audio := decision.OutputSource.Info.Streams[0], decision.OutputSource.Info.Streams[1]
	if decision.Plan.Hardware != limits.Hardware || video.BitDepth != 8 || video.Profile != "" || video.Level != 0 || video.RefFrames != 0 ||
		!video.IsAVC || !video.IsAVCKnown || !video.InterlaceKnown || video.IsInterlaced || video.CodecTag != "avc1" || video.AverageFrameRate != "" ||
		audio.Profile != "LC" || audio.BitDepth != 0 || audio.CodecTag != "mp4a" {
		t.Fatalf("encoded facts do not reflect explicit runner guarantees: %+v", decision)
	}
	request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = nil, nil
	decision = progressiveVideoTestPlan(t, progressiveVideoTestSource(), request, limits)
	if decision.Plan.Hardware != (transcode.Hardware{}) {
		t.Fatalf("remux unnecessarily selected a hardware encoder: %+v", decision.Plan)
	}
}

func TestPlanProgressiveVideoDoesNotMutateInputsOrProjectInputMeasurements(t *testing.T) {
	source, request := progressiveVideoTestSource(), progressiveVideoTestRequest()
	source.Info.ProbeVersion, source.Info.FileChangeTimeNs = 5, 123
	source.Info.AudioDurationExact, source.Info.AudioDurationReason, source.Info.PresentationOriginTicks = true, "source-only", 1234
	source.Info.Chapters = []media.Chapter{{StartTicks: 0, EndTicks: 100, Title: "Original"}}
	source.Info.Streams[0].AudioTiming = &media.AudioTiming{Exact: true, SampleCount: 12}
	source.Info.Streams[1].AudioTiming = &media.AudioTiming{Exact: true, SampleCount: 43_200_000}
	source.Info.Streams[0].Title, source.Info.Streams[1].Language = "Original", "eng"
	before := source
	before.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
	before.Info.Chapters = append([]media.Chapter(nil), source.Info.Chapters...)
	for index := range before.Info.Streams {
		if source.Info.Streams[index].AudioTiming != nil {
			timing := *source.Info.Streams[index].AudioTiming
			before.Info.Streams[index].AudioTiming = &timing
		}
	}
	request.VideoBitrate = profileTestPtr(int64(2_000_000))
	beforeRequest := request
	beforeRequest.VideoBitrate = profileTestPtr(*request.VideoBitrate)
	decision := progressiveVideoTestPlan(t, source, request, conversionTestLimits())
	if !reflect.DeepEqual(before, source) || !reflect.DeepEqual(request, beforeRequest) {
		t.Fatal("planning changed caller-owned source facts or requirements")
	}
	output, plan := decision.OutputSource.Info, decision.Plan
	if output.AudioDurationExact || output.AudioDurationReason != "" || output.PresentationOriginTicks != 0 || output.ProbeVersion != 0 || output.FileChangeTimeNs != 0 ||
		output.FormatStartKnown || output.FormatStartTicks != 0 || len(output.Chapters) != 0 || output.Size != 0 ||
		output.Streams[0].AudioTiming != nil || output.Streams[1].AudioTiming != nil || output.Streams[0].Title != "" || output.Streams[1].Language != "" ||
		plan.AudioSourceSampleCount != 0 || plan.AudioSourceSampleRate != 0 || plan.AudioSampleSeek || plan.AudioBitDepth != 0 ||
		plan.SegmentSeconds != 0 || plan.SegmentMode != "" || plan.SegmentTimes != "" || plan.SegmentStartNumber != 0 || plan.EndTicks != 0 || plan.ReferenceStartTicks != 0 {
		t.Fatalf("input-only measurements or HLS/audio-only options leaked into MP4: %+v", decision)
	}
	decision.OutputSource.Info.Streams[0].Width = 1
	if source.Info.Streams[0].Width != 1920 {
		t.Fatal("output projection aliases caller-owned streams")
	}
}

func TestPlanProgressiveVideoMalformedTargetsReturnSyntaxErrors(t *testing.T) {
	for _, mutate := range []func(*ProgressiveVideoRequest){
		func(r *ProgressiveVideoRequest) { r.OutputContainer = "" },
		func(r *ProgressiveVideoRequest) { r.VideoCodec = "" },
		func(r *ProgressiveVideoRequest) { r.AudioCodec = "aac,mp3" },
		func(r *ProgressiveVideoRequest) { r.StartTimeTicks = -1 },
		func(r *ProgressiveVideoRequest) { r.VideoStreamIndex = profileTestPtr(-1) },
		func(r *ProgressiveVideoRequest) { r.AudioStreamIndex = profileTestPtr(-1) },
		func(r *ProgressiveVideoRequest) { r.Width = profileTestPtr(0) },
		func(r *ProgressiveVideoRequest) { r.MaxHeight = profileTestPtr(-1) },
		func(r *ProgressiveVideoRequest) { r.MaxAudioChannels = profileTestPtr(0) },
		func(r *ProgressiveVideoRequest) { r.VideoBitrate = profileTestPtr(int64(0)) },
		func(r *ProgressiveVideoRequest) { r.MaxAudioBitrate = profileTestPtr(int64(-1)) },
		func(r *ProgressiveVideoRequest) { r.FrameRate = profileTestPtr(math.NaN()) },
		func(r *ProgressiveVideoRequest) { r.MaxFrameRate = profileTestPtr(math.Inf(1)) },
	} {
		request := progressiveVideoTestRequest()
		mutate(&request)
		if decision, err := PlanProgressiveVideo(progressiveVideoTestSource(), request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) || decision.Plan != nil {
			t.Fatalf("malformed normalized request was treated as supported media: %+v, %v", decision, err)
		}
	}
	for _, mutate := range []func(*ProgressiveVideoRequest){
		func(r *ProgressiveVideoRequest) { r.OutputContainer = "mkv" },
		func(r *ProgressiveVideoRequest) { r.VideoCodec = "hevc" },
		func(r *ProgressiveVideoRequest) { r.AudioCodec = "mp3" },
	} {
		request := progressiveVideoTestRequest()
		mutate(&request)
		progressiveVideoTestDeclined(t, progressiveVideoTestSource(), request, conversionTestLimits())
	}
}
