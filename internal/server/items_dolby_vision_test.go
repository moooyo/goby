package server

import (
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestMediaStreamDTOPreservesOnlyProbedDolbyVisionConfiguration(t *testing.T) {
	stream := media.Stream{Index: 2, CodecType: "video", Codec: "hevc", VideoRange: "DOVI", VideoRangeKnown: true,
		DolbyVision: &media.DolbyVisionMetadata{Profile: 8, Level: 6, RPUPresent: true, ELPresent: false, BLPresent: true,
			CompatibilityID: 1, RPUVerified: true, ResidualDisabled: true, RPUFrameCount: 300, RPUValidationReason: "private-proof"}}
	dto := mediaStreamsDTO([]media.Stream{stream})[0]
	for field, expected := range map[string]any{"DvProfile": 8, "DvLevel": 6, "RpuPresentFlag": true,
		"ElPresentFlag": false, "BlPresentFlag": true, "DvBlSignalCompatibilityId": 1} {
		if actual, exists := dto[field]; !exists || actual != expected {
			t.Fatalf("probed Dolby Vision field %s was omitted or changed", field)
		}
	}
	for _, field := range []string{"DolbyVision", "RPUVerified", "ResidualDisabled", "RPUFrameCount", "RPUValidationReason"} {
		if _, exists := dto[field]; exists {
			t.Fatalf("private Dolby Vision verification field %s was exposed", field)
		}
	}
	stream.DolbyVision = nil
	dto = mediaStreamsDTO([]media.Stream{stream})[0]
	for _, field := range []string{"DvProfile", "DvLevel", "RpuPresentFlag", "ElPresentFlag", "BlPresentFlag", "DvBlSignalCompatibilityId"} {
		if _, exists := dto[field]; exists {
			t.Fatalf("Dolby Vision range alone invented unknown configuration field %s", field)
		}
	}
}
