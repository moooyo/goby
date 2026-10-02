//go:build linux

package transcode

import (
	"context"
	"os"
	"syscall"
)

// MeasureGeneratedFMP4NativeClock borrows regular, physically distinct files.
// A single source fence spans complete framing and native metadata/hashes.
// ReadAt preserves borrowed offsets. This adds raw clock facts, not the packet,
// input, restart, source-range or native-publication evidence of other helpers.
func MeasureGeneratedFMP4NativeClock(ctx context.Context, plan Plan, initialization, segment *os.File) (GeneratedFMP4NativeClock, error) {
	var empty GeneratedFMP4NativeClock
	if ctx == nil || initialization == nil || segment == nil {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if ValidatePlan(plan) != nil || plan.HLS.SegmentType != "fmp4" || !plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) {
		return empty, ErrInvalidPlan
	}
	borrowed := []*os.File{initialization, segment}
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
	if os.SameFile(before[0], before[1]) {
		return empty, ErrInvalidInput
	}
	if err := ValidateGeneratedWindowFraming(plan, owned[0], owned[1]); err != nil {
		return empty, err
	}
	result, err := parseGeneratedFMP4NativeClock(ctx, owned[0], before[0].Size(), owned[1], before[1].Size())
	if err != nil {
		return empty, err
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return empty, ErrInvalidInput
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
