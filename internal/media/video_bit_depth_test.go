package media

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestEffectiveVideoBitDepthUsesExplicitModernDecoderFactsWithoutChangingProbe(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		for _, format := range []struct {
			name  string
			depth int
		}{{"yuv420p", 8}, {"nv12", 8}, {"yuv420p10le", 10}, {"yuv420p10be", 10}, {"p010le", 10}, {"p010be", 10}} {
			t.Run(codec+"/"+format.name, func(t *testing.T) {
				stream := parseFactsStream(t, fmt.Sprintf(`"codec_type":"video","codec_name":%q,"pix_fmt":%q,"bits_per_raw_sample":"N/A","bits_per_sample":"0"`, codec, format.name))
				before := stream
				if stream.BitDepth != 0 || EffectiveVideoBitDepth(stream) != format.depth || VideoBitDepthConflict(stream) {
					t.Fatalf("missing sample report did not retain separate effective precision: %+v", stream)
				}
				if !reflect.DeepEqual(stream, before) {
					t.Fatal("reading effective precision changed the stored probe facts")
				}
				encoded, err := json.Marshal(stream)
				var restored Stream
				if err != nil || json.Unmarshal(encoded, &restored) != nil || restored.BitDepth != 0 || EffectiveVideoBitDepth(restored) != format.depth {
					t.Fatal("effective precision became a fabricated reported value after cache round trip")
				}
			})
		}
	}
}

func TestEffectiveVideoBitDepthPreservesReportedPrecedenceAndContradictions(t *testing.T) {
	for _, test := range []struct {
		fields   string
		depth    int
		conflict bool
	}{
		{`"bits_per_raw_sample":10,"bits_per_sample":8,"pix_fmt":"yuv420p10le"`, 10, false},
		{`"bits_per_raw_sample":8,"pix_fmt":"yuv420p10le"`, 8, true},
		{`"bits_per_sample":10,"pix_fmt":"yuv420p"`, 10, true},
		{`"bits_per_raw_sample":12,"pix_fmt":"yuv420p10le"`, 12, true},
		{`"bits_per_raw_sample":12,"pix_fmt":"unknown"`, 12, false},
	} {
		stream := parseFactsStream(t, `"codec_type":"video","codec_name":"hevc",`+test.fields)
		if stream.BitDepth != test.depth || EffectiveVideoBitDepth(stream) != test.depth || VideoBitDepthConflict(stream) != test.conflict {
			t.Fatalf("reported sample precision was replaced or its contradiction hidden: %+v", stream)
		}
	}
}

func TestEffectiveVideoBitDepthDoesNotGuessFromUnrelatedOrPartialFacts(t *testing.T) {
	for _, stream := range []Stream{
		{CodecType: "video", Codec: "h264", PixelFormat: "yuv420p10le"},
		{CodecType: "audio", Codec: "av1", PixelFormat: "yuv420p"},
		{CodecType: "video", Codec: "hevc", PixelFormat: "yuv420p", IsAttachedPicture: true},
		{CodecType: "video", Codec: "hevc", Profile: "Main 10"},
		{CodecType: "video", Codec: "av1", PixelFormat: "vaapi"},
		{CodecType: "video", Codec: "av1", PixelFormat: "yuv420p10"},
		{CodecType: "video", Codec: "av1", PixelFormat: "yuv420p10le_extra"},
		{CodecType: "video", Codec: "hevc", PixelFormat: "p010"},
	} {
		if EffectiveVideoBitDepth(stream) != 0 || VideoBitDepthConflict(stream) {
			t.Fatalf("unverified decoder facts acquired a bit depth: %+v", stream)
		}
	}
	invalid := Stream{CodecType: "video", Codec: "hevc", BitDepth: -1, PixelFormat: "yuv420p10le"}
	if EffectiveVideoBitDepth(invalid) != -1 {
		t.Fatal("an invalid reported depth was repaired from another field")
	}
}
