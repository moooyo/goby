package library

import (
	"context"
	"os"
)

type publicImageWorkResult struct {
	file  *os.File
	image Image
	err   error
}

// runPublicImageWorker preserves the raw-file worker fixture used by lifecycle
// tests. Production image bodies are owned by runImageContentWorker.
func runPublicImageWorker(ctx context.Context, slots chan struct{}, work func() (*os.File, Image, error)) (*os.File, Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, Image{}, err
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return nil, Image{}, err
	}
	result := make(chan publicImageWorkResult)
	go func() {
		defer func() { <-slots }()
		if ctx.Err() != nil {
			return
		}
		file, image, err := work()
		if err != nil && file != nil {
			_ = file.Close()
			file = nil
		}
		select {
		case result <- publicImageWorkResult{file: file, image: image, err: err}:
			// Ownership moves to the receiver only after the unbuffered handoff.
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case outcome := <-result:
		if err := ctx.Err(); err != nil {
			if outcome.file != nil {
				_ = outcome.file.Close()
			}
			return nil, Image{}, err
		}
		return outcome.file, outcome.image, outcome.err
	case <-ctx.Done():
		return nil, Image{}, ctx.Err()
	}
}
