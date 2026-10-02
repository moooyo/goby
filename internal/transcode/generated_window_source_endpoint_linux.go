//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const generatedSourceEndpointTimeout = 30 * time.Second

// MeasureGeneratedMP4SourceEndpoint certifies the finite sample-table endpoint
// of one supported ordinary MP4 video track. It borrows the source through
// ReadAt and keeps the caller's descriptor and file offset unchanged. The
// source identity fences the complete table inspection, including its skipped
// media payload. Neither this identity nor the sample-table certificate proves
// that the coded bytes decode completely or that a CLI reached demux EOF.
//
// A caller combining this certificate with production or decoded tail evidence
// must preserve one source identity across that complete sequence and recheck
// it before publication. Separate successful inspection fences do not establish
// that their evidence came from the same source version.
func MeasureGeneratedMP4SourceEndpoint(ctx context.Context, source *os.File, videoStreamIndex int) (GeneratedSourceEndpointCertificate, error) {
	var result GeneratedSourceEndpointCertificate
	if ctx == nil {
		return result, ErrInvalidOptions
	}
	err := media.RunSourceReadPhase(ctx, func(work context.Context) error {
		var err error
		result, err = measureGeneratedMP4SourceEndpoint(work, source, videoStreamIndex)
		return err
	})
	return result, err
}

func measureGeneratedMP4SourceEndpoint(ctx context.Context, source *os.File, videoStreamIndex int) (_ GeneratedSourceEndpointCertificate, resultErr error) {
	var empty GeneratedSourceEndpointCertificate
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	file, err := DuplicateInput(source)
	if err != nil {
		return empty, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeSourceReadInput(file)) }()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return empty, ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil {
		return empty, ErrInvalidInput
	}
	proofContext, cancel := context.WithTimeout(ctx, generatedSourceEndpointTimeout)
	defer cancel()
	certificate, parseErr := parseGeneratedMP4SourceEndpoint(proofContext, file, before.Size(), videoStreamIndex)
	if !transcodeSourceUnchanged(file, before) {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := proofContext.Err(); err != nil {
		return empty, err
	}
	if parseErr != nil {
		return empty, parseErr
	}
	certificate.SourceIdentity = identity
	return certificate, nil
}

// ValidateGeneratedMP4SourceEndpointIdentity checks a held descriptor against
// the version captured by its certificate. It never refreshes a certificate or
// upgrades separately observed source frames into evidence for another version.
func ValidateGeneratedMP4SourceEndpointIdentity(source *os.File, certificate GeneratedSourceEndpointCertificate) error {
	if source == nil || certificate.SourceIdentity == "" {
		return ErrInvalidInput
	}
	info, err := source.Stat()
	if err != nil {
		return ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(info)
	if err != nil || identity != certificate.SourceIdentity {
		return ErrInvalidInput
	}
	return nil
}
