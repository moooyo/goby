package commanddomain

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

var (
	ErrPipeBusy      = errors.New("native template pipe already has an active consumer or read")
	ErrPipeUndrained = errors.New("native template pipe consumer returned before observed EOF")
)

// TemplatePipeSnapshot describes only this adapter's actual descriptor and
// callback ownership. Joined is the owned leader/exec-copier join, not output
// consumption, whole-cgroup retirement, storage release or backend readiness.
type TemplatePipeSnapshot struct {
	Starting            bool
	Started             bool
	Joined              bool
	Unknown             bool
	Closed              bool
	NoStartKnown        bool
	ParentWritersClosed int
	ReadersClosed       int
	ActiveReads         int
	ActiveConsumers     int
}

type templatePipe struct {
	reader               *os.File
	writer               *os.File
	readerClosed         bool
	writerClosed         bool
	readerCloseAttempted bool
	writerCloseAttempted bool
	readAdmissionClosed  bool
	reading              bool
	eof                  bool
	consumerActive       bool
	consumerReturned     bool
	consumerUnknown      bool
	consumer             func(io.Reader) error
}

// TemplatePipes owns at most one stdout and one stderr pipe made by the original
// immutable exec.Cmd template. Domain.Start copies that template into its private
// Cmd; os/exec therefore cannot retire the template's own pipe ends for it.
// No original template Start/Wait or unrestricted fallback is performed here.
//
// Consumers run synchronously in their caller's existing bounded ownership.
// This object creates no reader, waiter or callback goroutine. A normal owned
// callback return and an actual read EOF are distinct from the child Wait.
// Unrouted callbacks and other Go writers remain the enclosing job's concern.
type TemplatePipes struct {
	mu             sync.Mutex
	closeMu        sync.Mutex
	waitMu         sync.Mutex
	template       *exec.Cmd
	pipes          [2]*templatePipe
	domain         *Domain
	process        *Process
	parentContext  context.Context
	cancel         context.CancelFunc
	startAttempted bool
	starting       bool
	startReturned  bool
	noStartKnown   bool
	waitAttempted  bool
	joined         bool
	unknown        bool
	closed         bool
	closeAdmission bool
	abortRequested bool
	startErr       error
	waitErr        error
	closeErr       error
}

// NewTemplatePipes transfers the newly created pipe ends into this adapter.
// Requested streams must be unset, and the command must never have been started.
// A non-nil result with an error still owns any partial allocation. After this
// call, the caller must not change the template or close its raw stream fields.
func NewTemplatePipes(command *exec.Cmd, stdout, stderr bool) (*TemplatePipes, error) {
	if command == nil || command.Process != nil || command.ProcessState != nil || !stdout && !stderr ||
		stdout && command.Stdout != nil || stderr && command.Stderr != nil {
		return nil, ErrUnsafe
	}
	p := &TemplatePipes{template: command, noStartKnown: true}
	for index, requested := range []bool{stdout, stderr} {
		if !requested {
			continue
		}
		var reader io.ReadCloser
		var err error
		if index == 0 {
			reader, err = command.StdoutPipe()
		} else {
			reader, err = command.StderrPipe()
		}
		if err != nil {
			p.startErr = err
			return p, errors.Join(err, p.Close())
		}
		ownedReader, readerOK := reader.(*os.File)
		var ownedWriter *os.File
		if index == 0 {
			ownedWriter, _ = command.Stdout.(*os.File)
		} else {
			ownedWriter, _ = command.Stderr.(*os.File)
		}
		p.pipes[index] = &templatePipe{reader: ownedReader, writer: ownedWriter}
		if !readerOK || ownedReader == nil || ownedWriter == nil {
			// The actual os/exec API must produce these exact owned objects. An
			// unexpected shape is not permission to close an unrelated stream.
			p.unknown, p.noStartKnown = true, false
			p.startErr = ErrRetained
			return p, ErrRetained
		}
		readerInfo, readerErr := ownedReader.Stat()
		writerInfo, writerErr := ownedWriter.Stat()
		if readerErr != nil || writerErr != nil || readerInfo.Mode()&os.ModeNamedPipe == 0 ||
			writerInfo.Mode()&os.ModeNamedPipe == 0 || !os.SameFile(readerInfo, writerInfo) {
			p.startErr = errors.Join(ErrUnsafe, readerErr, writerErr)
			return p, errors.Join(p.startErr, p.Close())
		}
	}
	return p, nil
}

// Start invokes only the supplied concrete native owner. A returned non-nil
// Process is retained even together with an error and still requires Wait.
// Known rejection closes this adapter's pipe ends; retained or abnormal unknown
// start ownership preserves every original end and its exact domain reference.
func (p *TemplatePipes) Start(ctx context.Context, domain *Domain) (resultErr error) {
	if p == nil {
		return ErrUnsafe
	}
	claimed, returned := false, false
	defer func() {
		if claimed && !returned {
			p.mu.Lock()
			p.starting, p.unknown, p.noStartKnown = false, true, false
			p.startErr = errors.Join(p.startErr, ErrRetained)
			p.mu.Unlock()
		}
	}()
	p.mu.Lock()
	if p.startAttempted || p.closed || p.closeAdmission || p.unknown {
		p.mu.Unlock()
		return ErrClosed
	}
	p.startAttempted = true
	claimed = true
	p.domain, p.parentContext = domain, ctx
	p.starting = true
	p.mu.Unlock()
	preflightErr := error(nil)
	if ctx == nil {
		preflightErr = ErrUnsafe
	} else if err := ctx.Err(); err != nil {
		preflightErr = err
	} else if domain == nil {
		preflightErr = ErrUnavailable
	} else {
		p.mu.Lock()
		if p.template.Process != nil || p.template.ProcessState != nil || !p.exactTemplateStreamsLocked() {
			preflightErr = ErrUnsafe
		}
		p.mu.Unlock()
	}
	if preflightErr != nil {
		p.mu.Lock()
		p.startErr = preflightErr
		p.starting, p.startReturned = false, true
		p.mu.Unlock()
		resultErr = errors.Join(preflightErr, p.Close())
		returned = true
		return resultErr
	}
	ownedContext, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancel = cancel
	p.starting, p.noStartKnown = true, false
	abort := p.abortRequested
	p.mu.Unlock()
	if abort {
		cancel()
	}
	process, startErr := domain.Start(ownedContext, p.template)
	p.mu.Lock()
	p.process, p.startErr = process, startErr
	p.starting, p.startReturned = false, true
	if process == nil {
		if startErr == nil || errors.Is(startErr, ErrRetained) || errors.Is(startErr, ErrWaitOwnership) {
			p.unknown = true
		} else {
			p.noStartKnown = true
		}
	}
	knownRejected := p.noStartKnown
	p.mu.Unlock()
	if process != nil {
		// Fork/exec inherited these ends before Start returned its actual owner.
		// Keeping the parent's writer would prevent EOF in the consumer.
		resultErr = errors.Join(startErr, p.closeParentWriters())
	} else if knownRejected {
		cancel()
		resultErr = errors.Join(startErr, p.Close())
	} else {
		resultErr = errors.Join(startErr, ErrRetained)
	}
	returned = true
	return resultErr
}

func (p *TemplatePipes) exactTemplateStreamsLocked() bool {
	for index, pipe := range p.pipes {
		if pipe == nil {
			continue
		}
		var stream io.Writer = p.template.Stdout
		if index == 1 {
			stream = p.template.Stderr
		}
		if stream != pipe.writer || pipe.writerClosed || pipe.writerCloseAttempted {
			return false
		}
	}
	return true
}

// ConsumeStdout owns the complete callback, including parsing after its last
// Read. A successful callback must observe EOF. Wait may run before or during
// consumption and never closes this reader or discards buffered tail bytes.
func (p *TemplatePipes) ConsumeStdout(consume func(io.Reader) error) error {
	return p.consume(0, consume)
}

func (p *TemplatePipes) ConsumeStderr(consume func(io.Reader) error) error {
	return p.consume(1, consume)
}

func (p *TemplatePipes) consume(index int, consume func(io.Reader) error) (resultErr error) {
	if p == nil || consume == nil {
		return ErrUnsafe
	}
	p.mu.Lock()
	pipe := p.pipes[index]
	if pipe == nil || p.closed || p.closeAdmission || p.unknown || pipe.readAdmissionClosed || pipe.consumerReturned {
		p.mu.Unlock()
		return ErrClosed
	}
	if pipe.consumerActive {
		p.mu.Unlock()
		return ErrPipeBusy
	}
	pipe.consumerActive, pipe.consumer = true, consume
	p.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			p.mu.Lock()
			pipe.consumerActive, pipe.consumerUnknown, p.unknown = false, true, true
			p.mu.Unlock()
		}
	}()
	resultErr = consume(&templatePipeReader{owner: p, index: index})
	p.mu.Lock()
	pipe.consumerActive, pipe.consumerReturned, pipe.consumer = false, true, nil
	if resultErr == nil && !pipe.eof && !p.abortRequested {
		resultErr = ErrPipeUndrained
	}
	p.mu.Unlock()
	returned = true
	return resultErr
}

type templatePipeReader struct {
	owner *TemplatePipes
	index int
}

func (reader *templatePipeReader) Read(value []byte) (int, error) {
	p := reader.owner
	p.mu.Lock()
	pipe := p.pipes[reader.index]
	if pipe.readAdmissionClosed || pipe.readerClosed || p.closeAdmission || p.unknown || !pipe.consumerActive {
		p.mu.Unlock()
		return 0, os.ErrClosed
	}
	if pipe.reading {
		p.mu.Unlock()
		return 0, ErrPipeBusy
	}
	pipe.reading = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		pipe.reading = false
		p.mu.Unlock()
	}()
	count, err := pipe.reader.Read(value)
	if errors.Is(err, io.EOF) {
		p.mu.Lock()
		pipe.eof = true
		p.mu.Unlock()
	}
	return count, err
}

// Wait exclusively joins the actual native Process, including after Start
// returned an error with that Process. It never closes a reader: child exit does
// not mean a parser has consumed the tail or returned. Callers must still join
// their Consume callbacks and then Close. A second Wait shares the same result.
func (p *TemplatePipes) Wait() (resultErr error) {
	if p == nil {
		return ErrUnsafe
	}
	p.waitMu.Lock()
	defer p.waitMu.Unlock()
	p.mu.Lock()
	if p.waitAttempted {
		resultErr = p.waitErr
		if !p.joined {
			resultErr = errors.Join(resultErr, ErrRetained)
		}
		p.mu.Unlock()
		return resultErr
	}
	if p.starting || p.process == nil {
		resultErr = errors.Join(p.startErr, ErrWaitOwnership)
		p.mu.Unlock()
		return resultErr
	}
	p.waitAttempted = true
	process, startErr, cancel := p.process, p.startErr, p.cancel
	p.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			p.mu.Lock()
			p.unknown = true
			p.waitErr = errors.Join(p.waitErr, ErrRetained)
			p.mu.Unlock()
		}
	}()
	waitErr := process.Wait()
	_, joined := process.ExitCode()
	p.mu.Lock()
	p.joined = joined
	p.waitErr = errors.Join(startErr, waitErr)
	if !joined {
		p.unknown = true
		p.waitErr = errors.Join(p.waitErr, ErrRetained)
	}
	resultErr = p.waitErr
	p.mu.Unlock()
	if joined && cancel != nil {
		// This disarms the owned context only after exec's watcher joined. It
		// does not authorize discarding unread output as an explicit abort does.
		cancel()
	}
	returned = true
	return resultErr
}

// Cancel requests cancellation through the native command's owned context. It
// may close readers to interrupt a known command's Read, but does not join that
// command or consumer callback. Starting/unknown ownership retains all ends.
func (p *TemplatePipes) Cancel() error {
	if p == nil {
		return ErrUnsafe
	}
	p.mu.Lock()
	p.abortRequested = true
	cancel := p.cancel
	unknown, starting, noStart := p.unknown, p.starting, p.noStartKnown
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if unknown || starting {
		return ErrRetained
	}
	if noStart {
		return p.Close()
	}
	return p.closeReaders()
}

// Close requires a known absence of a start, or actual child join plus returned
// owned consumers and observed EOF. Explicit cancellation may discard buffered
// output, but cannot substitute for actual callback/read return. Unknown start,
// callback, waiter or Close ownership keeps the exact pipe objects reachable.
func (p *TemplatePipes) Close() (resultErr error) {
	if p == nil {
		return ErrUnsafe
	}
	returned := false
	defer func() {
		if !returned {
			p.mu.Lock()
			p.unknown = true
			p.closeErr = errors.Join(p.closeErr, ErrRetained)
			p.mu.Unlock()
		}
	}()
	resultErr = p.closeKnown()
	returned = true
	return resultErr
}

func (p *TemplatePipes) closeKnown() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	if p.unknown || p.starting || !p.noStartKnown && !p.joined {
		p.mu.Unlock()
		return ErrRetained
	}
	parent := p.parentContext
	abort := p.abortRequested
	p.mu.Unlock()
	if !abort && parent != nil {
		abort = parent.Err() != nil
	}
	p.mu.Lock()
	noStart, joined := p.noStartKnown, p.joined
	if p.unknown || p.starting || !noStart && !joined {
		p.mu.Unlock()
		return ErrRetained
	}
	if !noStart {
		for _, pipe := range p.pipes {
			if pipe != nil && (pipe.consumerActive || pipe.reading || pipe.consumerUnknown ||
				!abort && (!pipe.consumerReturned || !pipe.eof)) {
				p.mu.Unlock()
				return ErrRetained
			}
		}
	}
	p.closeAdmission = true
	p.mu.Unlock()
	err := errors.Join(p.closeParentWriters(), p.closeReaders())
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil || p.unknown {
		return errors.Join(err, p.closeErr, ErrRetained)
	}
	for _, pipe := range p.pipes {
		if pipe != nil && (!pipe.writerClosed || !pipe.readerClosed || pipe.consumerActive || pipe.reading || pipe.consumerUnknown) {
			return ErrRetained
		}
	}
	p.closed = true
	return nil
}

func (p *TemplatePipes) closeParentWriters() error { return p.closeEnds(false) }
func (p *TemplatePipes) closeReaders() error       { return p.closeEnds(true) }

func (p *TemplatePipes) closeEnds(readers bool) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	var result error
	for _, pipe := range p.pipes {
		if pipe == nil {
			continue
		}
		p.mu.Lock()
		file, closed, attempted := pipe.writer, pipe.writerClosed, pipe.writerCloseAttempted
		if readers {
			file, closed, attempted = pipe.reader, pipe.readerClosed, pipe.readerCloseAttempted
			pipe.readAdmissionClosed = true
		}
		if closed {
			p.mu.Unlock()
			continue
		}
		if attempted || file == nil {
			p.unknown = true
			p.mu.Unlock()
			result = errors.Join(result, ErrRetained)
			continue
		}
		if readers {
			pipe.readerCloseAttempted = true
		} else {
			pipe.writerCloseAttempted = true
		}
		p.mu.Unlock()
		err := file.Close()
		p.mu.Lock()
		if err != nil {
			p.unknown = true
			p.closeErr = errors.Join(p.closeErr, err, ErrRetained)
		} else if readers {
			pipe.readerClosed = true
		} else {
			pipe.writerClosed = true
		}
		p.mu.Unlock()
		result = errors.Join(result, err)
	}
	return result
}

func (p *TemplatePipes) Snapshot() TemplatePipeSnapshot {
	if p == nil {
		return TemplatePipeSnapshot{Unknown: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result := TemplatePipeSnapshot{Starting: p.starting, Started: p.process != nil, Joined: p.joined,
		Unknown: p.unknown, Closed: p.closed, NoStartKnown: p.noStartKnown}
	for _, pipe := range p.pipes {
		if pipe == nil {
			continue
		}
		if pipe.writerClosed {
			result.ParentWritersClosed++
		}
		if pipe.readerClosed {
			result.ReadersClosed++
		}
		if pipe.reading {
			result.ActiveReads++
		}
		if pipe.consumerActive {
			result.ActiveConsumers++
		}
	}
	return result
}
