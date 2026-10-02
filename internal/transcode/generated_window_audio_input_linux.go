//go:build linux

package transcode

import (
	"context"
	"io"
	"os"
	"sync"
)

type generatedAudioInputPipe struct {
	read, write *os.File
	parser      *generatedAudioInputWriter
	err         error
}

// generatedAudioInputObserver follows generatedInputObserver's owned-reader
// contract. It is a leaf primitive; runner and manager admission are unchanged.
type generatedAudioInputObserver struct {
	ctx         context.Context
	pipes       []*generatedAudioInputPipe
	done        chan struct{}
	mu          sync.Mutex
	latest      [MaxHLSRenditions]RawGeneratedAudioInputEvidence
	err         error
	startOnce   sync.Once
	closeOnce   sync.Once
	stopContext func() bool
}

// cancel, when present, must tolerate concurrent repeated calls, as a
// context.CancelFunc does. A failing reader closes the whole set independently
// of that callback; callback completion is not the reader-join authority.
func newGeneratedAudioInputObserver(ctx context.Context, options []RawGeneratedAudioInputOptions, report func(RawGeneratedAudioInputEvidence), cancel func()) (*generatedAudioInputObserver, error) {
	if ctx == nil || len(options) < 1 || len(options) > MaxHLSRenditions {
		return nil, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var seen [MaxHLSRenditions]bool
	for _, option := range options {
		if err := validateRawGeneratedAudioInputOptions(option); err != nil {
			return nil, err
		}
		if seen[option.Rendition] {
			return nil, ErrInvalidOptions
		}
		seen[option.Rendition] = true
	}
	observer := &generatedAudioInputObserver{ctx: ctx, done: make(chan struct{})}
	fail := func(err error) {
		// Preserve the originating fault before closing siblings. A lower
		// indexed reader can otherwise report ErrClosed before aggregation
		// inspects the actual report or parser failure.
		observer.mu.Lock()
		if observer.err == nil {
			observer.err = err
		}
		observer.mu.Unlock()
		// Close every reader before external cancellation, including when
		// that callback panics or terminates this owned goroutine with Goexit.
		observer.closePipes()
		if cancel != nil {
			cancel()
		}
	}
	for _, option := range options {
		var parser *generatedAudioInputWriter
		var err error
		parser, err = newGeneratedAudioInputWriter(option, func(evidence RawGeneratedAudioInputEvidence) {
			observer.mu.Lock()
			observer.latest[evidence.Rendition] = evidence
			observer.mu.Unlock()
			if report != nil {
				report(evidence)
			}
		}, func() { fail(parser.err) })
		if err != nil {
			observer.close()
			return nil, err
		}
		read, write, err := os.Pipe()
		if err != nil {
			observer.close()
			return nil, err
		}
		observer.pipes = append(observer.pipes, &generatedAudioInputPipe{read: read, write: write, parser: parser})
	}
	observer.stopContext = context.AfterFunc(ctx, observer.closePipes)
	return observer, nil
}

// start must follow successful child Start, after it inherited every private
// write descriptor. Each reader retains a bounded parser and reaches real EOF.
func (observer *generatedAudioInputObserver) start() {
	observer.startOnce.Do(func() {
		var readers sync.WaitGroup
		for _, pipe := range observer.pipes {
			_ = pipe.write.Close()
			readers.Add(1)
			go func(pipe *generatedAudioInputPipe) {
				defer readers.Done()
				defer pipe.read.Close()
				completed := false
				defer func() {
					_ = recover()
					if !completed {
						pipe.err = ErrProgress
						_ = pipe.parser.fail(pipe.err)
					}
				}()
				_, pipe.err = io.Copy(pipe.parser, pipe.read)
				if pipe.err == nil {
					pipe.err = pipe.parser.finish()
				} else {
					pipe.err = pipe.parser.fail(pipe.err)
				}
				completed = true
			}(pipe)
		}
		go func() {
			readers.Wait()
			observer.mu.Lock()
			if observer.err == nil {
				for _, pipe := range observer.pipes {
					if pipe.err != nil {
						observer.err = pipe.err
						break
					}
				}
			}
			observer.mu.Unlock()
			close(observer.done)
		}()
	})
}

func (observer *generatedAudioInputObserver) closePipes() {
	for _, pipe := range observer.pipes {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
}

func (observer *generatedAudioInputObserver) close() {
	observer.closeOnce.Do(func() {
		if observer.stopContext != nil {
			observer.stopContext()
		}
		observer.closePipes()
	})
}

// finish follows start. A caller whose child failed to Start must close the
// observer without waiting for readers that were never started.
func (observer *generatedAudioInputObserver) finish() error {
	<-observer.done
	observer.mu.Lock()
	err := observer.err
	observer.mu.Unlock()
	if err != nil {
		return err
	}
	return observer.ctx.Err()
}

func (observer *generatedAudioInputObserver) snapshot() [MaxHLSRenditions]RawGeneratedAudioInputEvidence {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return observer.latest
}
