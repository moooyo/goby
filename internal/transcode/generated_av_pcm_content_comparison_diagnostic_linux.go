//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// CompareGeneratedAVPCMContentDiagnostic borrows private completed PCM captures.
// It verifies whole capture hashes and unchanged identities/offsets before and
// after a fixed bounded candidate scan. No child, native clock or trim is added.
func CompareGeneratedAVPCMContentDiagnostic(ctx context.Context, reference, query *os.File, options GeneratedAVPCMContentOptions) (GeneratedAVPCMContentCandidate, error) {
	var empty GeneratedAVPCMContentCandidate
	if ctx == nil || !generatedAVPCMContentOptionsValid(options) {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	inputs := []*os.File{reference, query}
	fence, err := generatedAVDiagnosticOuterFence(inputs)
	if err != nil {
		return empty, err
	}
	var sizes [2]int64
	var infos [2]os.FileInfo
	for index, file := range inputs {
		info, _, err := generatedAVAssociationInputInfo(file)
		if err != nil || info.Mode().Perm() != 0600 {
			return empty, ErrInvalidInput
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) || (index == 0 && info.Size() > 24<<20) || (index == 1 && info.Size() > 8<<20) || info.Size()%int64(options.Channels*2) != 0 {
			return empty, ErrInvalidInput
		}
		sizes[index], infos[index] = info.Size(), info
	}
	if os.SameFile(infos[0], infos[1]) {
		return empty, ErrInvalidInput
	}
	for index, file := range inputs {
		hash, err := generatedAVAssociationSourceHash(ctx, file, sizes[index])
		if err != nil {
			return empty, err
		}
		if hash != [2][32]byte{options.ReferenceSHA256, options.QuerySHA256}[index] {
			return empty, ErrInvalidInput
		}
	}
	if err := fence(); err != nil {
		return empty, err
	}
	frameBytes := int64(options.Channels * 2)
	spanSamples := int64(options.CandidateCount - 1 + options.WindowSamples)
	if options.ReferenceFirstCandidateSample > sizes[0]/frameBytes-spanSamples || options.QueryFirstSample > sizes[1]/frameBytes-int64(options.WindowSamples) {
		return empty, ErrInvalidInput
	}
	span, window := make([]byte, spanSamples*frameBytes), make([]byte, int64(options.WindowSamples)*frameBytes)
	if n, err := reference.ReadAt(span, options.ReferenceFirstCandidateSample*frameBytes); err != nil || n != len(span) {
		return empty, errors.Join(ErrInvalidInput, err)
	}
	if n, err := query.ReadAt(window, options.QueryFirstSample*frameBytes); err != nil || n != len(window) {
		return empty, errors.Join(ErrInvalidInput, err)
	}
	result, err := compareGeneratedAVPCMContentWindows(ctx, span, window, options)
	if err != nil {
		return result, err
	}
	if err := fence(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.ReferenceBytes, result.QueryBytes = sizes[0], sizes[1]
	result.CapturedBytesVerified, result.Complete = true, true
	return result, nil
}
