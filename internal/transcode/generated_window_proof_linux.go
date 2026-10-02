//go:build linux

package transcode

import (
	"context"
	"os"
	"syscall"
)

// MeasureGeneratedWindowSegment binds physical framing, every demuxed packet
// and the first independently restartable access unit to the same retained
// bytes. A single identity fence spans the entire sequence; separate helpers'
// individual fences cannot establish that bytes stayed unchanged between them.
// This evidence still needs normally completed source input association before
// the publisher can assign an exact source range to the segment.
func MeasureGeneratedWindowSegment(ctx context.Context, ffprobe string, plan Plan, initialization, segment *os.File) (GeneratedSegmentBounds, error) {
	var empty GeneratedSegmentBounds
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) {
		return empty, ErrInvalidPlan
	}
	if segment == nil || plan.HLS.SegmentType == "fmp4" && initialization == nil || plan.HLS.SegmentType == "mpegts" && initialization != nil {
		return empty, ErrInvalidInput
	}
	borrowed := []*os.File{segment}
	if initialization != nil {
		borrowed = []*os.File{initialization, segment}
	}
	var owned []*os.File
	defer func() {
		for _, file := range owned {
			_ = file.Close()
		}
	}()
	var before []os.FileInfo
	var total int64
	for _, file := range borrowed {
		duplicate, err := DuplicateInput(file)
		if err != nil {
			return empty, err
		}
		owned = append(owned, duplicate)
		info, err := duplicate.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return empty, ErrInvalidInput
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return empty, ErrInvalidInput
		}
		if info.Size() > maxGeneratedBoundsInputBytes-total {
			return empty, ErrTimelineLimit
		}
		total += info.Size()
		before = append(before, info)
	}
	media := owned[len(owned)-1]
	var init *os.File
	if initialization != nil {
		init = owned[0]
	}
	if err := ValidateGeneratedWindowFraming(plan, init, media); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	bounds, err := MeasureGeneratedSegmentBounds(ctx, ffprobe, init, media, true)
	if err != nil {
		return empty, err
	}
	if err := ValidateLiveVideoRestart(ctx, ffprobe, init, media, "h264"); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return empty, ErrInvalidInput
		}
	}
	return bounds, nil
}
