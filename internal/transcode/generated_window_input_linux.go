//go:build linux

package transcode

import (
	"context"
	"io"
	"os"
	"sync"
)

type generatedInputPipe struct {
	read, write *os.File
	parser      *generatedInputWriter
	err         error
}

type generatedInputObserver struct {
	ctx         context.Context
	pipes       []*generatedInputPipe
	done        chan struct{}
	mu          sync.Mutex
	latest      [MaxHLSRenditions]GeneratedInputEvidence
	err         error
	startOnce   sync.Once
	closeOnce   sync.Once
	stopContext func() bool
}

func newGeneratedInputObserver(ctx context.Context, plan Plan, report func(GeneratedInputEvidence), cancel func()) (*generatedInputObserver, error) {
	if ctx == nil {
		return nil, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	count := max(1, plan.HLS.RenditionCount)
	if count > MaxHLSRenditions {
		return nil, ErrInvalidPlan
	}
	observer := &generatedInputObserver{ctx: ctx, done: make(chan struct{})}
	fail := func() {
		// Invalid evidence invalidates the whole output set. Close every
		// reader even when the caller supplied no cancellation callback, so
		// a foreign or incomplete record cannot leave finish waiting on a
		// different child's still-open descriptor.
		observer.closePipes()
		if cancel != nil {
			cancel()
		}
	}
	for index := 0; index < count; index++ {
		parser, err := newGeneratedInputWriter(plan, index, func(evidence GeneratedInputEvidence) {
			observer.mu.Lock()
			observer.latest[evidence.Rendition] = evidence
			observer.mu.Unlock()
			if report != nil {
				report(evidence)
			}
		}, fail)
		if err != nil {
			observer.close()
			return nil, err
		}
		read, write, err := os.Pipe()
		if err != nil {
			observer.close()
			return nil, err
		}
		// Every output has a private stats path. FFmpeg's shared AVIO buffer
		// for identical paths must never interleave different output records.
		observer.pipes = append(observer.pipes, &generatedInputPipe{read: read, write: write, parser: parser})
	}
	observer.stopContext = context.AfterFunc(ctx, observer.closePipes)
	return observer, nil
}

// start releases the parent's write ends after the child inherited them and
// drains each private pipe until actual EOF. A bounded line buffer and one
// snapshot per output keep memory independent of job duration or frame count.
func (observer *generatedInputObserver) start() {
	observer.startOnce.Do(func() {
		var readers sync.WaitGroup
		for _, pipe := range observer.pipes {
			_ = pipe.write.Close()
			readers.Add(1)
			go func(pipe *generatedInputPipe) {
				defer readers.Done()
				defer pipe.read.Close()
				completed := false
				defer func() {
					_ = recover()
					if !completed {
						// Set the pipe fault before invoking cancellation: external
						// cancellation can itself end this goroutine with Goexit.
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
			for _, pipe := range observer.pipes {
				if pipe.err != nil {
					observer.err = pipe.err
					break
				}
			}
			observer.mu.Unlock()
			close(observer.done)
		}()
	})
}

func (observer *generatedInputObserver) closePipes() {
	for _, pipe := range observer.pipes {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
}

func (observer *generatedInputObserver) close() {
	observer.closeOnce.Do(func() {
		if observer.stopContext != nil {
			observer.stopContext()
		}
		observer.closePipes()
	})
}

func (observer *generatedInputObserver) finish() error {
	<-observer.done
	observer.mu.Lock()
	err := observer.err
	observer.mu.Unlock()
	if err != nil {
		return err
	}
	return observer.ctx.Err()
}

// snapshot is progress evidence only. The caller must wait for finish and
// successful process reaping before treating these snapshots as complete.
func (observer *generatedInputObserver) snapshot() [MaxHLSRenditions]GeneratedInputEvidence {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return observer.latest
}
