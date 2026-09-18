//go:build linux

package transcode

import (
	"io"
	"os"
	"sync"
)

type hlsClockPipe struct {
	read, write *os.File
	parser      *hlsClockWriter
	err         error
}

type hlsClockObserver struct {
	pipes []*hlsClockPipe
	done  chan struct{}
	err   error
}

func newHLSClockObserver(plan Plan, report func(Progress), cancel func()) (*hlsClockObserver, error) {
	count := max(1, plan.HLS.RenditionCount)
	if count > MaxHLSRenditions || needsHLSCopyClock(plan) && count != 1 {
		return nil, ErrInvalidPlan
	}
	observer := &hlsClockObserver{done: make(chan struct{})}
	// FFmpeg shares an AVIO buffer for identical stats paths, while its locks
	// belong to individual streams. Give every concurrent muxer a distinct
	// pipe and parser so neither bytes nor packet counters can cross outputs.
	for index := 0; index < count; index++ {
		read, write, err := os.Pipe()
		if err != nil {
			observer.close()
			return nil, err
		}
		observer.pipes = append(observer.pipes, &hlsClockPipe{read: read, write: write,
			parser: &hlsClockWriter{count: count, callback: report, cancel: cancel, copyReference: needsHLSCopyClock(plan), expectRendition: true, expectedRendition: index}})
	}
	return observer, nil
}

func (observer *hlsClockObserver) start() {
	var readers sync.WaitGroup
	for _, pipe := range observer.pipes {
		_ = pipe.write.Close()
		readers.Add(1)
		go func(pipe *hlsClockPipe) {
			defer readers.Done()
			_, pipe.err = io.Copy(pipe.parser, pipe.read)
			if pipe.err == nil {
				pipe.err = pipe.parser.finish()
			}
			_ = pipe.read.Close()
		}(pipe)
	}
	go func() {
		readers.Wait()
		for _, pipe := range observer.pipes {
			if pipe.err != nil {
				observer.err = pipe.err
				break
			}
		}
		close(observer.done)
	}()
}

func (observer *hlsClockObserver) close() {
	for _, pipe := range observer.pipes {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
}

func (observer *hlsClockObserver) finish() error { <-observer.done; return observer.err }
