//go:build linux

package transcode

import (
	"context"
	"io"
	"os"
	"syscall"

	"github.com/moooyo/goby/internal/media"
)

// MeasureGeneratedAVTransportDiagnostic borrows one regular, single-link held
// segment. Its stat/ctime fence spans the entire structural observation and
// borrowed-offset check. The result never authorizes A/V publication.
func MeasureGeneratedAVTransportDiagnostic(ctx context.Context, segment *os.File) (GeneratedAVTransportDiagnostic, error) {
	var empty GeneratedAVTransportDiagnostic
	if ctx == nil || segment == nil {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	before, err := segment.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 {
		return empty, ErrInvalidInput
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return empty, ErrInvalidInput
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil {
		return empty, ErrInvalidInput
	}
	offset, err := segment.Seek(0, io.SeekCurrent)
	if err != nil {
		return empty, ErrInvalidInput
	}
	owned, err := DuplicateInput(segment)
	if err != nil {
		return empty, err
	}
	defer owned.Close()
	result, err := ParseGeneratedAVTransportDiagnostic(ctx, owned, before.Size())
	if err != nil {
		return empty, err
	}
	after, err := segment.Stat()
	afterOffset, offsetErr := segment.Seek(0, io.SeekCurrent)
	if err != nil || offsetErr != nil || afterOffset != offset || !transcodeSourceUnchanged(segment, before) {
		return empty, ErrInvalidInput
	}
	afterIdentity, err := media.VideoSeekSourceIdentity(after)
	if err != nil || afterIdentity != identity {
		return empty, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
