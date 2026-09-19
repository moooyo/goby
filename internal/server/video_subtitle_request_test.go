package server

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestProgressiveSubtitleQueriesRequireConsistentIndexedBurnSelection(t *testing.T) {
	source := videoRequestTestSource()
	for _, values := range []map[string]string{
		{"subtitlemethod": "encode", "subtitlestreamindex": "12"},
		{"subtitledeliverymethod": "Encode", "subtitlestreamindex": "12", "burninsubtitles": "true"},
		{"burnsubtitles": "true", "subtitlestreamindex": "15"},
	} {
		index, burn, err := videoQuerySubtitle(source, values)
		if err != nil || index == nil || !burn {
			t.Fatalf("valid indexed burn selection was rejected: %v", err)
		}
	}
	for _, values := range []map[string]string{
		{"subtitlemethod": "encode", "subtitledeliverymethod": "external", "subtitlestreamindex": "12"},
		{"subtitlemethod": "none", "burnsubtitles": "true", "subtitlestreamindex": "12"},
		{"subtitlemethod": "encode", "burnsubtitles": "false", "subtitlestreamindex": "12"},
		{"burnsubtitles": "true", "burninsubtitles": "false", "subtitlestreamindex": "12"},
		{"burnsubtitles": "perhaps", "subtitlestreamindex": "12"},
		{"subtitlestreamindex": "-2"}, {"subtitlestreamindex": "2147483648"},
	} {
		if _, _, err := videoQuerySubtitle(source, values); !errors.Is(err, errVideoRequestInvalid) {
			t.Fatalf("conflicting or malformed subtitle selection was accepted: %v: %v", values, err)
		}
	}
	for _, values := range []map[string]string{
		{"subtitlemethod": "encode"}, {"subtitlemethod": "encode", "subtitlestreamindex": "-1"},
		{"subtitlemethod": "encode", "subtitlestreamindex": "99"}, {"subtitlestreamindex": "12"},
	} {
		if _, _, err := videoQuerySubtitle(source, values); !errors.Is(err, errVideoRequestUnsupported) {
			t.Fatalf("an unavailable subtitle delivery was accepted: %v: %v", values, err)
		}
	}
}

func TestProgressiveBurnURLsRetainSelectionOffsetAndExternalFingerprint(t *testing.T) {
	for _, index := range []string{"12", "15"} {
		source := videoRequestTestSource()
		source.Info.Streams[len(source.Info.Streams)-1].SubtitleTag = strings.Repeat("a", 64)
		values := map[string]string{"VideoCodec": "hevc", "SubtitleDeliveryMethod": "Encode", "SubtitleStreamIndex": index, "SubtitleOffsetTicks": "2500000"}
		result := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
		plan := *result.Conversion.Plan
		if plan.VideoCodec != "hevc" || plan.Subtitle.Mode != "burn" || plan.Subtitle.OffsetTicks != 2_500_000 {
			t.Fatal("progressive subtitle request did not become an encoded burn plan")
		}
		parsed, err := url.Parse(videoPlaybackURL(source.ItemID, source.MediaSourceID, "play", "device", "token", plan))
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		if query.Get("SubtitleStreamIndex") != index || query.Get("SubtitleDeliveryMethod") != "Encode" || query.Get("SubtitleOffsetTicks") != "2500000" || strings.Contains(parsed.String(), strings.Repeat("a", 64)) {
			t.Fatal("burn URL lost its public selection or exposed a private subtitle fingerprint")
		}
		values = map[string]string{}
		for key := range query {
			values[key] = query.Get(key)
		}
		rebuilt := videoRequestTestPlan(t, source, values, "mp4", videoRequestTestLimits())
		if *rebuilt.Conversion.Plan != plan {
			t.Fatal("burn URL round trip changed the subtitle plan")
		}
	}
}
