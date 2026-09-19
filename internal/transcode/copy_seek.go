package transcode

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

// AttachVideoCopySeekCandidate prepares an exact packet-copy seek after stream
// selection. Failure leaves the plan unchanged so the caller can encode.
func AttachVideoCopySeekCandidate(plan *Plan, info media.Info) bool {
	return AttachVideoCopySeekCandidateAligned(plan, info, 0)
}

// AttachVideoCopySeekCandidateAligned allows explicit backward alignment to a
// proven keyframe. StartTicks reports the actual boundary used by the output.
func AttachVideoCopySeekCandidateAligned(plan *Plan, info media.Info, maxPrerollTicks int64) bool {
	if plan == nil || plan.OutputMode != "progressive" || plan.Container != "mp4" || plan.VideoCodec != "copy" ||
		plan.StartTicks <= 0 || plan.AudioStreamIndex >= 0 && plan.AudioCodec != "aac" && plan.AudioCodec != "copy" ||
		plan.VideoSeekCandidate != "" || !info.FormatStartKnown || !plan.SourceFormatStartKnown ||
		plan.SourceFormatStartTicks != info.FormatStartTicks || plan.DurationTicks != info.DurationTicks {
		return false
	}
	encoded, err := media.SelectVideoCopySeekCandidateForInfoAligned(info, plan.VideoStreamIndex, plan.StartTicks, maxPrerollTicks)
	if err != nil {
		return false
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(encoded)
	if err != nil {
		return false
	}
	if plan.VideoCopyCodec != "" && plan.VideoCopyCodec != media.VideoSeekCodec(candidate.Index) {
		return false
	}
	candidate.CopyTimestamps = plan.CopyTimestamps
	if plan.AudioCodec == "copy" && plan.AudioStreamIndex >= 0 {
		var source *media.Stream
		for position := range info.Streams {
			stream := &info.Streams[position]
			if stream.Index != plan.AudioStreamIndex {
				continue
			}
			if source != nil || stream.CodecType != "audio" || stream.Codec != "aac" || stream.IsExternal || !strings.EqualFold(stream.Profile, "LC") {
				return false
			}
			source = stream
		}
		if source == nil || len(source.TimeBase) == 0 || len(source.TimeBase) > 64 {
			return false
		}
		timeBase, ok := new(big.Rat).SetString(source.TimeBase)
		if !ok || timeBase.Sign() <= 0 {
			return false
		}
		for _, audio := range candidate.Index.Entries[0].Audio {
			if audio.StreamIndex == source.Index && audio.Codec == source.Codec && audio.SampleRate == source.SampleRate && audio.Channels == source.Channels &&
				timeBase.Cmp(new(big.Rat).SetFrac(big.NewInt(audio.TimeBaseNumerator), big.NewInt(audio.TimeBaseDenominator))) == 0 {
				selected := audio
				candidate.Audio = &selected
				break
			}
		}
		if candidate.Audio == nil {
			return false
		}
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return false
	}
	proposed := *plan
	proposed.VideoCopySeekCandidate = string(data)
	proposed.StartTicks = candidate.RequestedStartTicks
	proposed.VideoCopyCodec = media.VideoSeekCodec(candidate.Index)
	if err := ValidatePlan(proposed); err != nil {
		return false
	}
	*plan = proposed
	return true
}

func validateVideoCopySeekCandidate(p Plan) error {
	invalid := func() error { return fmt.Errorf("%w: progressive video copy seek candidate", ErrInvalidPlan) }
	if p.VideoCopySeekCandidate == "" {
		if p.VideoCodec == "copy" && p.StartTicks > 0 {
			return invalid()
		}
		return nil
	}
	if p.OutputMode != "progressive" || p.Container != "mp4" || p.VideoCodec != "copy" || p.StartTicks <= 0 ||
		!p.SourceFormatStartKnown || p.VideoSeekCandidate != "" || p.AudioStreamIndex >= 0 && p.AudioCodec != "aac" && p.AudioCodec != "copy" {
		return invalid()
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(p.VideoCopySeekCandidate)
	if err != nil || candidate.RequestedStartTicks != p.StartTicks || candidate.Index.StreamIndex != p.VideoStreamIndex ||
		candidate.Index.FormatStartTicks != p.SourceFormatStartTicks || candidate.Index.DurationTicks != p.DurationTicks ||
		media.VideoSeekCodec(candidate.Index) != VideoOutputCodec(p) || candidate.CopyTimestamps != p.CopyTimestamps {
		return invalid()
	}
	if p.AudioCodec == "copy" && p.AudioStreamIndex >= 0 {
		if candidate.Audio == nil || candidate.Audio.StreamIndex != p.AudioStreamIndex {
			return invalid()
		}
	} else if candidate.Audio != nil {
		return invalid()
	}
	return nil
}
