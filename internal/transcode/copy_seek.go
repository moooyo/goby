package transcode

import (
	"fmt"

	"github.com/moooyo/goby/internal/media"
)

// AttachVideoCopySeekCandidate prepares a narrow packet-copy seek after stream
// selection. Failure leaves the plan unchanged so the caller can select video
// encoding or reject the request. AAC audio retains the linear shared-clock
// path; copied audio has no packet-alignment proof and is not admitted.
func AttachVideoCopySeekCandidate(plan *Plan, info media.Info) bool {
	if plan == nil || plan.OutputMode != "progressive" || plan.Container != "mp4" || plan.VideoCodec != "copy" ||
		plan.StartTicks <= 0 || plan.AudioStreamIndex >= 0 && plan.AudioCodec != "aac" ||
		plan.VideoSeekCandidate != "" || !info.FormatStartKnown || !plan.SourceFormatStartKnown ||
		plan.SourceFormatStartTicks != info.FormatStartTicks || plan.DurationTicks != info.DurationTicks {
		return false
	}
	candidate, err := media.SelectVideoCopySeekCandidateForInfo(info, plan.VideoStreamIndex, plan.StartTicks)
	if err != nil {
		return false
	}
	proposed := *plan
	proposed.VideoCopySeekCandidate = candidate
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
		!p.SourceFormatStartKnown || p.VideoSeekCandidate != "" || p.AudioStreamIndex >= 0 && p.AudioCodec != "aac" {
		return invalid()
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(p.VideoCopySeekCandidate)
	if err != nil || candidate.RequestedStartTicks != p.StartTicks || candidate.Index.StreamIndex != p.VideoStreamIndex ||
		candidate.Index.FormatStartTicks != p.SourceFormatStartTicks || candidate.Index.DurationTicks != p.DurationTicks {
		return invalid()
	}
	return nil
}
