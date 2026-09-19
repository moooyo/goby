package dynamicsource

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

const pipeChunkBytes = 32 * 1024

// Input owns exactly one authorized connection. Pipe readers are created
// together before any bytes are consumed; they never reopen the live source.
type Input struct {
	Lease
	reader                 io.ReadCloser
	ctx                    context.Context
	release                func()
	reserve                func() (func(), error)
	bufferBytes            int
	stallTimeout           time.Duration
	mu                     sync.Mutex
	set                    *PipeSet
	reading, closed        bool
	closeOnce, releaseOnce sync.Once
	closeErr               error
}

func (input *Input) Read(data []byte) (int, error) {
	input.mu.Lock()
	if input.closed {
		input.mu.Unlock()
		return 0, ErrClosed
	}
	if input.set != nil {
		input.mu.Unlock()
		return 0, ErrBusy
	}
	input.reading = true
	input.mu.Unlock()
	return input.reader.Read(data)
}

func (input *Input) closeReader() error {
	input.closeOnce.Do(func() { input.closeErr = input.reader.Close() })
	return input.closeErr
}

func (input *Input) finish() {
	input.releaseOnce.Do(func() {
		input.mu.Lock()
		input.closed = true
		input.mu.Unlock()
		input.release()
	})
}

// Close cancels every reader together and joins its pumps. The manager also
// retains ownership until those pumps have actually terminated.
func (input *Input) Close() error {
	input.mu.Lock()
	input.closed = true
	set := input.set
	input.mu.Unlock()
	if set != nil {
		set.abort(ErrClosed)
		err := input.closeReader()
		<-set.done
		return err
	}
	err := input.closeReader()
	input.finish()
	return err
}

// PipeSet contains separate anonymous pipes carrying identical bytes from
// generation byte zero. Readers[0] is media and Readers[1], when present, is
// the independent bitmap-subtitle demuxer. The recipient owns the read ends.
// A slow or failed reader fails the entire group; no reader skips bytes.
type PipeSet struct {
	Readers    []*os.File
	LeaseID    string
	Generation uint64
	Stamp      string
	input      *Input
	writers    []*os.File
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	err        error
	finished   bool
	buffer     []byte
	writeAt    uint64
	cursors    []uint64
	eof        bool
	changed    chan struct{}
}

// OpenPipe preserves the single-reader API without duplicating source access.
func (input *Input) OpenPipe() (*os.File, error) {
	set, err := input.OpenPipeSet(1)
	if err != nil {
		return nil, err
	}
	return set.Readers[0], nil
}

// OpenPipeSet atomically establishes one or two readers at the same byte
// origin. The bounded journal is decoder lag storage, never a replay archive.
func (input *Input) OpenPipeSet(readerCount int) (*PipeSet, error) {
	if readerCount != 1 && readerCount != 2 {
		return nil, ErrInvalid
	}
	input.mu.Lock()
	defer input.mu.Unlock()
	if input.closed {
		return nil, ErrClosed
	}
	if input.reading || input.set != nil {
		return nil, ErrBusy
	}
	if err := input.ctx.Err(); err != nil {
		return nil, err
	}
	var releaseBuffer func()
	if readerCount == 2 {
		var err error
		releaseBuffer, err = input.reserve()
		if err != nil {
			return nil, err
		}
	}
	set := &PipeSet{input: input, LeaseID: input.ID, Generation: input.Generation, Stamp: input.Stamp,
		done: make(chan struct{}), changed: make(chan struct{}), cursors: make([]uint64, readerCount)}
	for index := 0; index < readerCount; index++ {
		reader, writer, err := os.Pipe()
		if err != nil {
			for _, file := range append(set.Readers, set.writers...) {
				_ = file.Close()
			}
			if releaseBuffer != nil {
				releaseBuffer()
			}
			return nil, ErrUnavailable
		}
		set.Readers = append(set.Readers, reader)
		set.writers = append(set.writers, writer)
	}
	set.ctx, set.cancel = context.WithCancel(input.ctx)
	if readerCount == 2 {
		set.buffer = make([]byte, input.bufferBytes)
	}
	input.set = set
	stop := context.AfterFunc(input.ctx, func() { set.abort(input.ctx.Err()) })
	var pumps sync.WaitGroup
	if readerCount == 1 {
		pumps.Add(1)
		go func() { defer pumps.Done(); set.copySingle() }()
	} else {
		pumps.Add(3)
		go func() { defer pumps.Done(); set.ingress() }()
		for index := range set.writers {
			go func() { defer pumps.Done(); set.egress(index) }()
		}
	}
	go func() {
		pumps.Wait()
		stop()
		_ = input.closeReader()
		set.mu.Lock()
		if set.err == nil && input.ctx.Err() != nil {
			set.err = input.ctx.Err()
		}
		set.finished = true
		set.buffer = nil
		set.mu.Unlock()
		set.cancel()
		if releaseBuffer != nil {
			releaseBuffer()
		}
		input.finish()
		close(set.done)
	}()
	return set, nil
}

func (set *PipeSet) signalLocked() {
	close(set.changed)
	set.changed = make(chan struct{})
}

func (set *PipeSet) abort(err error) {
	set.mu.Lock()
	if set.finished || set.err != nil {
		set.mu.Unlock()
		return
	}
	set.err = err
	set.signalLocked()
	set.mu.Unlock()
	set.cancel()
	// Closing write ends interrupts blocked Write calls. Read ends remain
	// recipient-owned until explicit PipeSet.Close or recipient cleanup.
	for _, writer := range set.writers {
		_ = writer.Close()
	}
	_ = set.input.closeReader()
}

// Close additionally releases every returned read end. It is safe after
// those ends have already been closed by the conversion manager.
func (set *PipeSet) Close() error {
	if set == nil {
		return nil
	}
	err := set.input.Close()
	for _, reader := range set.Readers {
		_ = reader.Close()
	}
	return err
}

// Wait returns only after ingress, every egress, and upstream cleanup finish.
// A caller timeout does not leave untracked background work: Close cancels it.
func (set *PipeSet) Wait(ctx context.Context) error {
	select {
	case <-set.done:
		set.mu.Lock()
		defer set.mu.Unlock()
		return set.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (set *PipeSet) write(writer *os.File, data []byte) bool {
	// The timer covers writes even after upstream EOF, when no further
	// ingress activity could detect a reader that has stopped consuming.
	timer := time.AfterFunc(set.input.stallTimeout, func() { set.abort(ErrFanoutStalled) })
	n, err := writer.Write(data)
	stopped := timer.Stop()
	if !stopped {
		set.abort(ErrFanoutStalled)
		return false
	}
	if err != nil || n != len(data) {
		set.abort(ErrUnavailable)
		return false
	}
	return set.ctx.Err() == nil
}

func (set *PipeSet) copySingle() {
	defer set.writers[0].Close()
	buffer := make([]byte, pipeChunkBytes)
	emptyReads := 0
	for set.ctx.Err() == nil {
		n, err := set.input.reader.Read(buffer)
		if n != 0 {
			emptyReads = 0
			if !set.write(set.writers[0], buffer[:n]) {
				return
			}
		} else {
			emptyReads++
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil || emptyReads >= 100 {
			set.abort(ErrUnavailable)
			return
		}
	}
}

func (set *PipeSet) ingress() {
	buffer := make([]byte, pipeChunkBytes)
	emptyReads := 0
	for {
		set.mu.Lock()
		free := uint64(len(set.buffer)) - (set.writeAt - min(set.cursors[0], set.cursors[1]))
		changed := set.changed
		set.mu.Unlock()
		if set.ctx.Err() != nil {
			return
		}
		if free == 0 {
			select {
			case <-changed:
			case <-set.ctx.Done():
				return
			}
			continue
		}
		n, err := set.input.reader.Read(buffer[:min(uint64(len(buffer)), free)])
		set.mu.Lock()
		if n != 0 {
			emptyReads = 0
			position := set.writeAt % uint64(len(set.buffer))
			first := copy(set.buffer[position:], buffer[:n])
			copy(set.buffer, buffer[first:n])
			set.writeAt += uint64(n)
		} else {
			emptyReads++
		}
		if errors.Is(err, io.EOF) {
			set.eof = true
		}
		set.signalLocked()
		set.mu.Unlock()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil || emptyReads >= 100 {
			set.abort(ErrUnavailable)
			return
		}
	}
}

func (set *PipeSet) egress(index int) {
	defer set.writers[index].Close()
	buffer := make([]byte, pipeChunkBytes)
	for {
		set.mu.Lock()
		available := set.writeAt - set.cursors[index]
		changed, eof := set.changed, set.eof
		n := min(uint64(len(buffer)), available)
		if n != 0 {
			position := set.cursors[index] % uint64(len(set.buffer))
			first := copy(buffer[:n], set.buffer[position:])
			copy(buffer[first:n], set.buffer)
		}
		set.mu.Unlock()
		if set.ctx.Err() != nil {
			return
		}
		if n == 0 {
			if eof {
				return
			}
			select {
			case <-changed:
			case <-set.ctx.Done():
				return
			}
			continue
		}
		if !set.write(set.writers[index], buffer[:n]) {
			return
		}
		set.mu.Lock()
		set.cursors[index] += n
		set.signalLocked()
		set.mu.Unlock()
	}
}
