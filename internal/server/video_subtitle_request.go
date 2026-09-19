package server

import (
	"math"
	"strings"

	"github.com/moooyo/goby/internal/playback"
)

// videoQuerySubtitle maps only indexed subtitle choices. The planner binds the
// indexed fingerprint, and delivery reauthorizes that asset before it is read.
func videoQuerySubtitle(source playback.Source, values map[string]string) (*int, bool, error) {
	method := ""
	for _, name := range []string{"subtitlemethod", "subtitledeliverymethod"} {
		if raw, supplied := values[name]; supplied {
			value := strings.ToLower(raw)
			if value != "none" && value != "external" && value != "encode" {
				return nil, false, errVideoRequestUnsupported
			}
			if method != "" && method != value {
				return nil, false, errVideoRequestInvalid
			}
			method = value
		}
	}
	var burnFlag *bool
	for _, name := range []string{"burnsubtitles", "burninsubtitles"} {
		flag, err := hlsQueryBoolean(values, name)
		if err != nil || flag != nil && burnFlag != nil && *flag != *burnFlag {
			return nil, false, errVideoRequestInvalid
		}
		if flag != nil {
			burnFlag = flag
		}
	}
	burn := method == "encode" || burnFlag != nil && *burnFlag
	if burn && (method == "none" || method == "external" || burnFlag != nil && !*burnFlag) {
		return nil, false, errVideoRequestInvalid
	}
	parsed, err := hlsQueryInteger(values, -1, math.MaxInt32, "subtitlestreamindex")
	if err != nil {
		return nil, false, errVideoRequestInvalid
	}
	if parsed == nil || *parsed == -1 {
		if burn {
			return nil, false, errVideoRequestUnsupported
		}
		return nil, false, nil
	}
	if method == "none" {
		return nil, false, errVideoRequestInvalid
	}
	index := int(*parsed)
	for _, stream := range source.Info.Streams {
		if stream.Index == index && stream.CodecType == "subtitle" {
			if burn {
				return &index, true, nil
			}
			if stream.IsExternal && stream.IsTextSubtitleStream {
				// External delivery does not place the track inside the MP4.
				return nil, false, nil
			}
			return nil, false, errVideoRequestUnsupported
		}
	}
	return nil, false, errVideoRequestUnsupported
}
