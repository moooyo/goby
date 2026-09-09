package transcode

import (
	"encoding/json"
	"fmt"

	"github.com/moooyo/goby/internal/media"
)

// Scan evidence and preflight cover the software decoder. Hardware encoding
// remains independent, but hardware decoding retains the linear input path.
func progressiveVideoSeekPreflightEnabled(p Plan) bool {
	decode, _ := hardwareSelection(p.Hardware)
	return p.VideoSeekCandidate != "" && decode == "software"
}

func validateProgressiveVideoSeekCandidate(p Plan) error {
	if p.VideoSeekCandidate == "" {
		return nil
	}
	invalid := func() error { return fmt.Errorf("%w: progressive video seek candidate", ErrInvalidPlan) }
	if p.VideoCodec != "h264" || p.StartTicks <= 0 || !p.SourceFormatStartKnown {
		return invalid()
	}
	candidate, err := media.ValidateVideoSeekCandidate(p.VideoSeekCandidate)
	if err != nil || candidate.Index.StreamIndex != p.VideoStreamIndex || candidate.Index.DurationTicks != p.DurationTicks ||
		candidate.Index.FormatStartTicks != p.SourceFormatStartTicks || candidate.RequestedStartTicks != p.StartTicks {
		return invalid()
	}
	// Canonical private JSON preserves registry equality and leaves ample room
	// inside the plan's 128 KiB bound; client-controlled whitespace or duplicate
	// fields cannot multiply the escaped representation stored in a plan.
	canonical, err := json.Marshal(candidate)
	if err != nil || string(canonical) != p.VideoSeekCandidate {
		return invalid()
	}
	return nil
}
