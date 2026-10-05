package playback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestExternalBitmapOrdinalPreservesExistingProbeSerialization(t *testing.T) {
	// Primary probe JSON participates in persisted source fingerprints. Adding
	// an unused external ordinal must leave existing probe documents unchanged.
	source := conversionTestSource()
	encoded, err := json.Marshal(source.Info)
	if err != nil || strings.Contains(string(encoded), "SubtitleSourceStreamIndex") {
		t.Fatalf("existing primary probe acquired an external ordinal: %s, %v", encoded, err)
	}
	source.Info.Streams[3].IsExternal = true
	source.Info.Streams[3].SubtitleSourceStreamIndex = 1
	encoded, err = json.Marshal(source.Info)
	var restored media.Info
	if err != nil || json.Unmarshal(encoded, &restored) != nil || restored.Streams[3].SubtitleSourceStreamIndex != 1 {
		t.Fatalf("a nonzero external ordinal did not survive its private projection: %v", err)
	}
}

func TestExternalBitmapBurnPlansRetainCatalogTagAndSourceOrdinal(t *testing.T) {
	for _, codec := range []string{"hdmv_pgs_subtitle", "dvd_subtitle"} {
		t.Run(codec, func(t *testing.T) {
			source, request := conversionTestSource(), conversionTestRequest()
			subtitle := &source.Info.Streams[3]
			subtitle.IsExternal, subtitle.IsTextSubtitleStream, subtitle.Codec = true, false, codec
			subtitle.Index, subtitle.SubtitleTag = 1000000, strings.Repeat("a", 64)
			if codec == "dvd_subtitle" {
				subtitle.SubtitleSourceStreamIndex = 1
			}
			request.SubtitleStreamIndex = profileTestPtr(subtitle.Index)
			request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodEncode, Format: codec, Container: "ts", Protocol: "hls"}}
			hls := conversionTestPlan(t, source, request, conversionTestLimits())
			if hls.Plan.Subtitle.Mode != "burn" || hls.Plan.Subtitle.StreamIndex != subtitle.Index || hls.Plan.Subtitle.ExternalStreamIndex != subtitle.SubtitleSourceStreamIndex ||
				hls.Plan.Subtitle.ExternalTag != subtitle.SubtitleTag || hls.Plan.Subtitle.Codec != codec || hls.Plan.VideoCodec == "copy" ||
				hls.Plan.Subtitle.ExternalCanvasWidth != source.Info.Streams[0].Width || hls.Plan.Subtitle.ExternalCanvasHeight != source.Info.Streams[0].Height {
				t.Fatalf("HLS burn lost sidecar identity: %+v", hls.Plan)
			}
			progressiveSource := progressiveVideoTestSource()
			progressiveSource.Info.Streams[3] = *subtitle
			progressiveRequest := progressiveVideoTestRequest()
			progressiveRequest.BurnSubtitles, progressiveRequest.SubtitleStreamIndex = true, profileTestPtr(subtitle.Index)
			progressiveRequest.SubtitleOffsetTicks = media.TicksPerSecond / 2
			progressive := progressiveVideoTestPlan(t, progressiveSource, progressiveRequest, conversionTestLimits())
			if progressive.Plan.Subtitle.ExternalStreamIndex != subtitle.SubtitleSourceStreamIndex || progressive.Plan.Subtitle.OffsetTicks != media.TicksPerSecond/2 ||
				progressive.Plan.Subtitle.ExternalTag != subtitle.SubtitleTag || progressive.Plan.VideoCodec == "copy" ||
				progressive.Plan.Subtitle.ExternalCanvasWidth != progressiveSource.Info.Streams[0].Width || progressive.Plan.Subtitle.ExternalCanvasHeight != progressiveSource.Info.Streams[0].Height {
				t.Fatalf("progressive burn lost sidecar selection or clock: %+v", progressive.Plan)
			}
			conversionTestDeclined(t, source, request, ConversionLimits{AllowRemux: true, AllowAudioTranscode: true})
			subtitle.SubtitleTag = ""
			conversionTestDeclined(t, source, request, conversionTestLimits())
		})
	}
}
