package playback

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// attachProgressiveVideoSeekCandidate only selects private scan evidence. It
// never opens a source or starts a tool, and unavailable or inconsistent evidence
// cannot turn an otherwise playable conversion into an unsupported request.
// Source.Info comes from the authorized catalog source. The opaque file/tool
// identities are checked against the borrowed descriptor and executable by Run;
// selecting a candidate here is not a claim that its restart has been proved.
func attachProgressiveVideoSeekCandidate(source Source, plan *transcode.Plan) {
	if plan == nil {
		return
	}
	plan.VideoSeekCandidate = ""
	if plan.OutputMode != "progressive" || plan.Container != "mp4" || plan.VideoCodec != "h264" || plan.StartTicks <= 0 ||
		plan.Hardware.Decode != "" && plan.Hardware.Decode != "software" || source.Info.ProbeVersion != media.CurrentProbeVersion ||
		!source.Info.FormatStartKnown || !plan.SourceFormatStartKnown || plan.DurationTicks != source.Info.DurationTicks ||
		plan.SourceFormatStartTicks != source.Info.FormatStartTicks || len(source.Info.VideoSeekIndexes) == 0 ||
		len(source.Info.VideoSeekIndexes) > maxSourceStreams {
		return
	}
	var video *media.Stream
	for index := range source.Info.Streams {
		stream := &source.Info.Streams[index]
		if stream.Index == plan.VideoStreamIndex {
			if video != nil || stream.CodecType != "video" || stream.Codec != "h264" || stream.IsExternal || stream.IsAttachedPicture {
				return
			}
			video = stream
		}
	}
	if video == nil || len(video.TimeBase) == 0 || len(video.TimeBase) > 64 {
		return
	}
	timeBase, ok := new(big.Rat).SetString(video.TimeBase)
	if !ok || timeBase.Sign() <= 0 {
		return
	}
	// Match the scan's aggregate storage budget before serializing anything.
	// Validate every retained index, including shared source/tool/clock identity,
	// so mixed or duplicated catalog evidence cannot silently choose one record.
	entries, bytes := 0, 2
	var selected *media.VideoSeekIndex
	var sourceIdentity, toolIdentity string
	seen := make(map[int]bool, len(source.Info.VideoSeekIndexes))
	for position := range source.Info.VideoSeekIndexes {
		index := &source.Info.VideoSeekIndexes[position]
		entries += len(index.Entries)
		if entries > media.MaxVideoSeekEntries || seen[index.StreamIndex] || index.DurationTicks != source.Info.DurationTicks ||
			index.FormatStartTicks != source.Info.FormatStartTicks || len(index.PixelFormat) > 64 ||
			len(index.SourceIdentity) != 64 || len(index.ToolIdentity) != 64 || len(index.ParameterSetsSHA256) != 64 {
			return
		}
		seen[index.StreamIndex] = true
		if position == 0 {
			sourceIdentity, toolIdentity = index.SourceIdentity, index.ToolIdentity
		} else if sourceIdentity != index.SourceIdentity || toolIdentity != index.ToolIdentity {
			return
		}
		if media.ValidateVideoSeekIndex(*index) != nil {
			return
		}
		encoded, err := json.Marshal(index)
		if err != nil {
			return
		}
		bytes += len(encoded)
		if position > 0 {
			bytes++
		}
		if bytes > media.MaxVideoSeekIndexBytes {
			return
		}
		if index.StreamIndex == video.Index {
			if index.Width != video.Width || index.Height != video.Height || index.PixelFormat != strings.ToLower(video.PixelFormat) ||
				timeBase.Cmp(new(big.Rat).SetFrac(big.NewInt(index.TimeBaseNumerator), big.NewInt(index.TimeBaseDenominator))) != 0 {
				return
			}
			selected = index
		}
	}
	if selected == nil {
		return
	}
	encoded, err := media.SelectVideoSeekCandidate(*selected, plan.StartTicks)
	if err != nil {
		return
	}
	// The canonical string owns its data, so subsequent catalog slice changes
	// cannot mutate an accepted plan or its comparable registry key.
	plan.VideoSeekCandidate = encoded
	if transcode.ValidatePlan(*plan) != nil {
		plan.VideoSeekCandidate = ""
	}
}
