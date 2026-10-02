//go:build linux

package transcode

import (
	"context"
	"os"
	"syscall"
)

// CompareGeneratedAVPCMOverlapDifferenceDiagnostic borrows completed private PCM
// captures. It validates both complete expected hashes, trusted fixed PCM interpretation,
// identities and offsets across exact streamed range statistics. No child,
// capture clipping, inferred native timestamp or encoded-source proof is added.
func CompareGeneratedAVPCMOverlapDifferenceDiagnostic(ctx context.Context, reference, query *os.File, options GeneratedAVPCMOverlapDifferenceOptions) (GeneratedAVPCMOverlapDifferenceDiagnostic, error) {
	var empty GeneratedAVPCMOverlapDifferenceDiagnostic
	if ctx == nil || !generatedAVPCMOverlapOptionsValid(options) {
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
		if err != nil {
			return empty, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		limit := int64(24 << 20)
		if index == 1 {
			limit = 8 << 20
		}
		if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0600 || info.Size() > limit || info.Size()%int64(options.Channels*2) != 0 {
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
	result, err := compareGeneratedAVPCMOverlapDifference(ctx, reference, query, sizes[0], sizes[1], options)
	if err != nil {
		return result, err
	}
	if err := fence(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.Complete, result.CapturedBytesVerified = true, true
	return result, nil
}
