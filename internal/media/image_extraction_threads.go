package media

import (
	"errors"
	"fmt"
	"runtime"
)

var ErrInvalidImageExtractionThreads = errors.New("invalid image extraction thread policy")

// ImageExtractionThreads is an immutable decoder resource choice for seek
// previews. An absent choice preserves Goby's historical single-thread decode.
// It does not configure filter, output, subtitle, or video-encoding threads.
type ImageExtractionThreads struct {
	Count      int
	Configured bool
}

// ValidateImageExtractionThreadCount applies Goby's bounded management input
// contract. Zero requests the native extractor's automatic decoder selection.
func ValidateImageExtractionThreadCount(count int) error {
	if count < 0 || count > 16 {
		return fmt.Errorf("%w: count must be between 0 and 16", ErrInvalidImageExtractionThreads)
	}
	return nil
}

// ResolveImageExtractionDecoderThreads implements the native image extractor's
// threshold: only a positive requested count below the processor count emits
// a decoder limit. Zero means omit the input -threads argument, not -threads 0.
func ResolveImageExtractionDecoderThreads(options ImageExtractionThreads, processorCount int) (int, error) {
	if processorCount < 1 {
		return 0, fmt.Errorf("%w: processor count must be positive", ErrInvalidImageExtractionThreads)
	}
	if !options.Configured {
		if options.Count != 0 {
			return 0, fmt.Errorf("%w: absent choice has a nonzero count", ErrInvalidImageExtractionThreads)
		}
		return 1, nil
	}
	if err := ValidateImageExtractionThreadCount(options.Count); err != nil {
		return 0, err
	}
	if options.Count == 0 || options.Count >= processorCount {
		return 0, nil
	}
	return options.Count, nil
}

func (options ImageExtractionThreads) decoderThreads() (int, error) {
	return ResolveImageExtractionDecoderThreads(options, runtime.NumCPU())
}
