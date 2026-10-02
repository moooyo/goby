package transcode

import (
	"encoding/json"
	"math/big"
)

// generatedMP4SourceTailPlan binds a bounded decoded-source interval to the
// finite sample-set endpoint declared by an admitted ordinary MP4 track.
// SampleCount remains a sample-table count, not a decoded-frame-count claim.
func generatedMP4SourceTailPlan(plan Plan, cert GeneratedSourceEndpointCertificate) (int64, error) {
	if ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) || plan.VideoStreamIndex != 0 {
		return 0, ErrInvalidPlan
	}
	if cert.SourceIdentity == "" {
		return 0, ErrInvalidInput
	}
	if cert.StreamIndex != plan.VideoStreamIndex || cert.TrackID == 0 || cert.SampleCount < 1 || cert.SampleCount > generatedMP4MaxSamples ||
		cert.MetadataSHA256 == ([32]byte{}) || cert.SampleExtentsSHA256 == ([32]byte{}) || !cert.DurationTicksExact || cert.DurationTicks <= 0 ||
		plan.HLS.Window.EndTicks != cert.DurationTicks || cert.Origin.Den <= 0 || cert.Last.Den <= 0 || cert.End.Den <= 0 || cert.FrameDuration.Den <= 0 {
		return 0, ErrInvalidTimeline
	}
	origin := new(big.Rat).SetFrac64(cert.Origin.Num, cert.Origin.Den)
	last := new(big.Rat).SetFrac64(cert.Last.Num, cert.Last.Den)
	end := new(big.Rat).SetFrac64(cert.End.Num, cert.End.Den)
	period := new(big.Rat).SetFrac64(cert.FrameDuration.Num, cert.FrameDuration.Den)
	duration := new(big.Rat).Sub(end, origin)
	expectedPeriod := new(big.Rat).SetFrac64(1, int64(plan.FrameRate))
	expectedLast := new(big.Rat).Sub(end, period)
	tableDuration := new(big.Rat).Mul(period, new(big.Rat).SetInt64(cert.SampleCount))
	if period.Cmp(expectedPeriod) != 0 || duration.Cmp(generatedTicksSeconds(cert.DurationTicks)) != 0 ||
		last.Cmp(expectedLast) != 0 || tableDuration.Cmp(duration) != 0 || origin.Sign() < 0 || end.Cmp(generatedTicksSeconds(maxDurationTicks)) > 0 {
		return 0, ErrInvalidTimeline
	}
	originTicks := new(big.Rat).Mul(origin, new(big.Rat).SetInt64(ticksPerSecond))
	if !originTicks.IsInt() || !originTicks.Num().IsInt64() || originTicks.Num().Int64() < -maxDurationTicks || originTicks.Num().Int64() > maxDurationTicks {
		return 0, ErrInvalidTimeline
	}
	ticks := originTicks.Num().Int64()
	if err := generatedSourceRangePlan(plan, ticks); err != nil {
		return 0, err
	}
	return ticks, nil
}

// parseGeneratedMP4SourceTail retains the strict source-range schema and its
// whole-document budgets, then checks every observed decoded frame against
// the independently declared absolute source endpoint. This does not certify
// CLI or demuxer EOF, payload completeness, or a full-source decoded timeline.
func parseGeneratedMP4SourceTail(data []byte, plan Plan, cert GeneratedSourceEndpointCertificate) (GeneratedSourceRange, error) {
	var empty GeneratedSourceRange
	originTicks, err := generatedMP4SourceTailPlan(plan, cert)
	if err != nil {
		return empty, err
	}
	result, err := parseGeneratedSourceRange(data, plan, originTicks)
	if err != nil {
		return empty, err
	}
	// The shared parser already proved unique canonical keys, exact integer
	// clocks, the sole stream, and every record, including ignored preroll and
	// postroll. Reusing its typed document cannot admit a second JSON schema.
	var document generatedSourceRangeDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return empty, ErrTimelineProbe
	}
	var stream generatedSourceRangeStream
	if err := json.Unmarshal(document.Streams[0], &stream); err != nil {
		return empty, ErrTimelineProbe
	}
	base, err := generatedBoundsTimeBase(stream.TimeBase)
	if err != nil {
		return empty, err
	}
	endpoint := new(big.Rat).SetFrac64(cert.End.Num, cert.End.Den)
	sourceOrigin := new(big.Rat).SetFrac64(cert.Origin.Num, cert.Origin.Den)
	var lastObservedEnd *big.Rat
	for _, raw := range document.Frames {
		var frame generatedSourceRangeFrame
		if err := json.Unmarshal(raw, &frame); err != nil || frame.PTS == nil || frame.Duration == nil {
			return empty, ErrTimelineProbe
		}
		pts := generatedClockSeconds(*frame.PTS, base.Num, base.Den)
		duration := generatedClockSeconds(*frame.Duration, base.Num, base.Den)
		frameEnd := new(big.Rat).Add(pts, duration)
		if pts.Cmp(sourceOrigin) < 0 || pts.Cmp(endpoint) >= 0 || frameEnd.Cmp(endpoint) > 0 {
			return empty, ErrInvalidTimeline
		}
		lastObservedEnd = frameEnd
	}
	if lastObservedEnd == nil || lastObservedEnd.Cmp(endpoint) != 0 {
		return empty, ErrInvalidTimeline
	}
	return result, nil
}
