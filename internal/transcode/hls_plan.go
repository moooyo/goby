package transcode

import (
	"fmt"
	"strconv"
)

const MaxHLSRenditions = 4

// HLSPlan describes actual independently encoded outputs in one bounded job.
// The fixed array preserves immutable comparable session and deduplication keys.
type HLSPlan struct {
	SegmentType    string                         `json:"SegmentType,omitempty"`
	RenditionCount int                            `json:"RenditionCount,omitempty"`
	Renditions     [MaxHLSRenditions]HLSRendition `json:"Renditions,omitempty"`
}

type HLSRendition struct {
	Width        int   `json:"Width"`
	Height       int   `json:"Height"`
	VideoBitrate int64 `json:"VideoBitrate"`
}

// GeneratedHLS distinguishes measured multi-resource HLS from the original
// source-timeline TS API. Empty HLS retains its original output contract.
func GeneratedHLS(p Plan) bool {
	return p.HLS != (HLSPlan{}) || p.Subtitle.Mode == "hls" || p.SourceMode == "stream"
}

func validateHLSPlan(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: %s", ErrInvalidPlan, field) }
	if p.SourceMode != "" && p.SourceMode != "stream" {
		return invalid("source mode")
	}
	if p.SourceMode == "stream" && (p.StartTicks != 0 || p.DurationTicks != 0 || p.AudioSampleSeek || p.SegmentMode != "" || p.Subtitle.Mode != "") {
		return invalid("stream timeline")
	}
	if p.HLS.SegmentType != "" && p.HLS.SegmentType != "mpegts" && p.HLS.SegmentType != "fmp4" && p.HLS.SegmentType != "packed" {
		return invalid("HLS segment type")
	}
	if GeneratedHLS(p) && p.SegmentMode != "" {
		return invalid("generated HLS timeline")
	}
	if p.HLS.SegmentType == "fmp4" && p.AudioCodec == "mp3" {
		return invalid("fMP4 audio codec")
	}
	if p.HLS.SegmentType == "fmp4" && p.Container != "mp4" || p.HLS.SegmentType == "packed" && p.Container != p.AudioCodec {
		return invalid("HLS container")
	}
	if p.HLS.SegmentType == "packed" && (p.VideoStreamIndex >= 0 || p.AudioCodec != "aac" && p.AudioCodec != "mp3" || p.HLS.RenditionCount != 0 || p.SourceMode != "") {
		return invalid("packed audio")
	}
	if p.HLS.RenditionCount < 0 || p.HLS.RenditionCount > MaxHLSRenditions || p.HLS.RenditionCount == 1 {
		return invalid("rendition count")
	}
	if p.HLS.RenditionCount > 0 && (p.VideoCodec != "h264" || p.VideoStreamIndex < 0 || p.FrameRate == 0 || p.HLS.SegmentType == "packed") {
		return invalid("adaptive video")
	}
	for index, rendition := range p.HLS.Renditions {
		if index >= p.HLS.RenditionCount {
			if rendition != (HLSRendition{}) {
				return invalid("unused rendition")
			}
			continue
		}
		if rendition.Width < 2 || rendition.Width > p.Width || rendition.Width%2 != 0 || rendition.Height < 2 || rendition.Height > p.Height || rendition.Height%2 != 0 || rendition.VideoBitrate < 64000 || rendition.VideoBitrate > p.VideoBitrate {
			return invalid("rendition dimensions or bitrate")
		}
		if index == 0 && (rendition.Width != p.Width || rendition.Height != p.Height || rendition.VideoBitrate != p.VideoBitrate) {
			return invalid("primary rendition")
		}
		if index > 0 {
			prior := p.HLS.Renditions[index-1]
			if rendition.Width >= prior.Width || rendition.Height >= prior.Height || rendition.VideoBitrate >= prior.VideoBitrate {
				return invalid("rendition ordering")
			}
		}
	}
	return nil
}

func hlsPrefix(index, count int) string {
	if count == 0 {
		return ""
	}
	return "v" + strconv.Itoa(index) + "-"
}

// HLSPlaylistName returns the exact file written for an output rendition.
func HLSPlaylistName(index, count int) string {
	if count == 0 {
		return "main.m3u8"
	}
	return "v" + strconv.Itoa(index) + ".m3u8"
}
