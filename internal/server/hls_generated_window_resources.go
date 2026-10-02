package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/transcode"
)

// A logical resource is an admission role, never an exact manager filename.
// Initialization resources belong to the same closed source slot as their
// media, so a map cannot select another window's bytes or producer epoch.
type hlsGeneratedWindowResource struct {
	number         int
	variant        int
	initialization bool
}

func hlsGeneratedWindowResourceName(plan transcode.Plan, resource hlsGeneratedWindowResource) string {
	count := plan.HLS.RenditionCount
	if resource.number < 0 || resource.number >= transcode.MaxPlaylistSegments || count < 0 || count == 1 || count > transcode.MaxHLSRenditions ||
		resource.variant < 0 || resource.variant >= max(1, count) {
		return ""
	}
	stem, extension := "window-segment", "ts"
	switch plan.HLS.SegmentType {
	case "mpegts":
		if resource.initialization {
			return ""
		}
	case "fmp4":
		extension = "m4s"
		if resource.initialization {
			stem, extension = "window-init", "mp4"
		}
	default:
		return ""
	}
	prefix := ""
	if count > 0 {
		prefix = fmt.Sprintf("v%d-", resource.variant)
	}
	return fmt.Sprintf("%s%s-%06d.%s", prefix, stem, resource.number, extension)
}

func hlsGeneratedWindowResourceFromName(plan transcode.Plan, name string) (hlsGeneratedWindowResource, bool) {
	count := plan.HLS.RenditionCount
	if count < 0 || count == 1 || count > transcode.MaxHLSRenditions {
		return hlsGeneratedWindowResource{}, false
	}
	for variant := 0; variant < max(1, count); variant++ {
		candidate := name
		if count > 0 {
			var prefixed bool
			candidate, prefixed = strings.CutPrefix(candidate, fmt.Sprintf("v%d-", variant))
			if !prefixed {
				continue
			}
		}
		for _, initialization := range []bool{false, true} {
			stem, extension := "window-segment-", ".ts"
			if plan.HLS.SegmentType == "fmp4" {
				extension = ".m4s"
				if initialization {
					stem, extension = "window-init-", ".mp4"
				}
			}
			value, prefixed := strings.CutPrefix(candidate, stem)
			value, suffixed := strings.CutSuffix(value, extension)
			if !prefixed || !suffixed || len(value) != 6 {
				continue
			}
			number, err := strconv.Atoi(value)
			resource := hlsGeneratedWindowResource{number: number, variant: variant, initialization: initialization}
			if err == nil && hlsGeneratedWindowResourceName(plan, resource) == name {
				return resource, true
			}
		}
	}
	return hlsGeneratedWindowResource{}, false
}
