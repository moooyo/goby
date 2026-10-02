package transcode

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/media"
)

// SourceReadLifetime is an opaque catalog-issued input lifetime. The manager
// retains it while an accepted input is idle and closes it only after its last
// source descriptor. Context binds the actual readers to the shared I/O budget.
type SourceReadLifetime interface {
	Context(context.Context) context.Context
	Close() error
}

func closeSourceReadInput(file *os.File) error {
	if file == nil {
		return nil
	}
	err := file.Close()
	if err == nil || errors.Is(err, os.ErrClosed) {
		return nil
	}
	return media.SourceReadRetirementError(err, file)
}

func closeSourceReadInputs(inputs StreamInputs, read SourceReadLifetime) error {
	closeInput := func(file *os.File) error {
		if file == nil {
			return nil
		}
		err := file.Close()
		if errors.Is(err, os.ErrClosed) {
			return nil
		}
		return err
	}
	err := closeInput(inputs.Media)
	if inputs.Bitmap != inputs.Media {
		err = errors.Join(err, closeInput(inputs.Bitmap))
	}
	if err != nil {
		err = media.SourceReadRetirementError(err, inputs.Media, inputs.Bitmap)
		if unknown, ok := read.(interface{ MarkUnknown(error) error }); ok {
			err = errors.Join(err, unknown.MarkUnknown(err))
		}
		return err
	}
	if read != nil {
		return read.Close()
	}
	return nil
}
