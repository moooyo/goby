package playback

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestPlanHLSAdaptiveRenditionsEachMatchOutputConstraints(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	request.DeviceProfile.TranscodingProfiles[0].Container = "fmp4"
	request.DeviceProfile.TranscodingProfiles[0].EnableAdaptiveBitrate = profileTestPtr(true)
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	plan := decision.Plan
	if plan.VideoCodec != "h264" || plan.Container != "mp4" || plan.HLS.SegmentType != "fmp4" || plan.HLS.RenditionCount < 2 {
		t.Fatalf("missing adaptive output: %+v", plan)
	}
	for index := 1; index < plan.HLS.RenditionCount; index++ {
		prior, current := plan.HLS.Renditions[index-1], plan.HLS.Renditions[index]
		if current.Width >= prior.Width || current.Height >= prior.Height || current.VideoBitrate >= prior.VideoBitrate {
			t.Fatal("ladder aliases an earlier output")
		}
	}
	limits := conversionTestLimits()
	limits.AllowVideoTranscode = false
	conversionTestDeclined(t, source, request, limits)
}

func TestPlanHLSExternalBurnIsBoundToIndexedSubtitleBytes(t *testing.T) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[3].IsExternal, source.Info.Streams[3].Codec, source.Info.Streams[3].SubtitleTag = true, "ass", strings.Repeat("a", 64)
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodEncode, Format: "ass", Container: "ts", Protocol: "hls"}}
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.Subtitle.Mode != "burn" || decision.Plan.Subtitle.ExternalTag != source.Info.Streams[3].SubtitleTag || decision.Plan.VideoCodec != "h264" {
		t.Fatal("external burn did not retain the authorized subtitle identity")
	}
	source.Info.Streams[3].SubtitleTag = ""
	conversionTestDeclined(t, source, request, conversionTestLimits())
}

func TestPlanHLSPackedAudioEncodesRequestedContainer(t *testing.T) {
	for _, codec := range []string{"aac", "mp3"} {
		source := conversionTestSource()
		source.ItemType = "Audio"
		source.Info.Streams = []media.Stream{source.Info.Streams[1]}
		request := conversionTestRequest()
		request.DeviceProfile.TranscodingProfiles[0].Type = DlnaProfileTypeAudio
		request.DeviceProfile.TranscodingProfiles[0].Container, request.DeviceProfile.TranscodingProfiles[0].AudioCodec = codec, codec
		decision := conversionTestPlan(t, source, request, conversionTestLimits())
		if decision.Plan.HLS.SegmentType != "packed" || decision.Plan.AudioCodec != codec || decision.Plan.VideoStreamIndex != -1 {
			t.Fatalf("unexpected packed plan: %+v", decision.Plan)
		}
	}
}

func TestPlanHLSSelectedSubtitleRenditionAndBurnIn(t *testing.T) {
	for _, method := range []SubtitleDeliveryMethod{SubtitleDeliveryMethodHls, SubtitleDeliveryMethodEncode} {
		source, request := conversionTestSource(), conversionTestRequest()
		request.SubtitleStreamIndex = profileTestPtr(12)
		format := "vtt"
		if method == SubtitleDeliveryMethodEncode {
			format = "subrip"
		}
		request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: method, Format: format, Container: "ts", Protocol: "hls"}}
		decision := conversionTestPlan(t, source, request, conversionTestLimits())
		if method == SubtitleDeliveryMethodEncode {
			if decision.Plan.VideoCodec != "h264" {
				t.Fatal("burn-in did not encode video")
			}
			if decision.Plan.Subtitle.Mode != "burn" || decision.Plan.Subtitle.StreamIndex != 12 {
				t.Fatal("burn-in lost its selected original track")
			}
		} else {
			track, ok := transcode.HLSSubtitleTrackAt(*decision.Plan, 0)
			if !ok || track.StreamIndex != 12 || decision.SubtitleView.SelectedStreamIndex != 12 {
				t.Fatal("the one-track HLS rendition lost its original selection")
			}
		}
		if decision.Output.SubtitleMethod != method {
			t.Fatalf("subtitle plan = %+v", decision)
		}
	}
}
