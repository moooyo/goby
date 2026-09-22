package transcode

import (
	"encoding/json"
	"fmt"

	"github.com/moooyo/goby/internal/media"
)

// AttachVideoCopySeekCandidate prepares an exact packet-copy seek after stream
// selection. Failure leaves the plan unchanged so the caller can encode.
func AttachVideoCopySeekCandidate(plan *Plan, info media.Info) bool {
	return AttachVideoCopySeekCandidateAligned(plan, info, 0)
}

// AttachVideoCopySeekCandidateAligned allows explicit backward alignment to a
// proven keyframe shared by every copied stream. StartTicks reports the actual
// boundary used by the output.
func AttachVideoCopySeekCandidateAligned(plan *Plan, info media.Info, maxPrerollTicks int64) bool {
	if plan == nil || plan.OutputMode != "progressive" || plan.Container != "mp4" || plan.VideoCodec != "copy" ||
		plan.StartTicks <= 0 || plan.AudioStreamIndex >= 0 && plan.AudioCodec != "aac" && plan.AudioCodec != "copy" ||
		plan.VideoSeekCandidate != "" || !info.FormatStartKnown || !plan.SourceFormatStartKnown ||
		plan.SourceFormatStartTicks != info.FormatStartTicks || plan.DurationTicks != info.DurationTicks {
		return false
	}
	audioStreamIndex := -1
	if plan.AudioCodec == "copy" && plan.AudioStreamIndex >= 0 {
		audioStreamIndex = plan.AudioStreamIndex
	}
	encoded, err := media.SelectVideoCopySeekCandidateForStreamsAligned(info, plan.VideoStreamIndex, audioStreamIndex, plan.StartTicks, maxPrerollTicks)
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
