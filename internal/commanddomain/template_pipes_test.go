package commanddomain

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const templatePipeHelperEnvironment = "GOBY_COMMANDDOMAIN_TEMPLATE_PIPE_HELPER"
const templatePipeHelperArgument = "emit-template-pipe-tail"
const templatePipeStdoutPrefix = "template stdout prefix\n"
const templatePipeStdoutTail = "template stdout tail sentinel\n"
const templatePipeStderrPrefix = "template stderr prefix\n"
const templatePipeStderrTail = "template stderr tail sentinel\n"

// Ordinary descriptor fixtures never certify native cgroup ownership. The
// positive Domain.Start/Wait path requires a separately prepared root fixture.
func templatePipesFixture(t *testing.T, stdout, stderr bool) (*TemplatePipes, *exec.Cmd) {
	t.Helper()
	command := exec.Command("/unused-template")
	pipes, err := NewTemplatePipes(command, stdout, stderr)
	if err != nil || pipes == nil {
		t.Fatalf("construct template pipes: %v", err)
	}
	t.Cleanup(func() {
		// These ordinary fixtures never start an actual native Domain command.
		// Unknown negative fixtures retain their public ownership state; only
		// this package-private teardown closes their verified no-child ends.
		if err := pipes.closeParentWriters(); err != nil {
			t.Errorf("close fixture parent writers: %v", err)
		}
		if err := pipes.closeReaders(); err != nil {
			t.Errorf("close fixture readers: %v", err)
		}
	})
	return pipes, command
}

func templatePipesReceive[T any](t *testing.T, channel <-chan T, boundary string) T {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case value := <-channel:
		return value
	case <-timer.C:
		t.Fatalf("did not reach %s", boundary)
		var zero T
		return zero
	}
}

func templatePipesWaitActiveReads(t *testing.T, pipes *TemplatePipes, count int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		if snapshot := pipes.Snapshot(); snapshot.ActiveReads == count && snapshot.ActiveConsumers == count {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("the owned callback did not enter its actual pipe Read")
		case <-poll.C:
		}
	}
}

func templatePipesAssertNoRawReader(t *testing.T, reader io.Reader) {
	t.Helper()
	if _, raw := reader.(*os.File); raw {
		t.Fatal("a consumer received the raw pipe file")
	}
	if _, raw := reader.(interface{ Fd() uintptr }); raw {
		t.Fatal("a consumer received a raw descriptor accessor")
	}
	if _, closer := reader.(io.Closer); closer {
		t.Fatal("a consumer can close the adapter's owned descriptor")
	}
}

func TestTemplatePipesConstructorOwnsOnlySelectedPairs(t *testing.T) {
	for _, streams := range []struct {
		name           string
		stdout, stderr bool
		count          int
	}{{"stdout", true, false, 1}, {"stderr", false, true, 1}, {"both", true, true, 2}} {
		t.Run(streams.name, func(t *testing.T) {
			pipes, command := templatePipesFixture(t, streams.stdout, streams.stderr)
			if snapshot := pipes.Snapshot(); !snapshot.NoStartKnown || snapshot.Started || snapshot.Joined || snapshot.Unknown ||
				snapshot.ParentWritersClosed != 0 || snapshot.ReadersClosed != 0 || snapshot.ActiveConsumers != 0 {
				t.Fatalf("constructor changed actual launch or ownership state: %+v", snapshot)
			}
			for _, stream := range []struct {
				requested bool
				writer    io.Writer
			}{{streams.stdout, command.Stdout}, {streams.stderr, command.Stderr}} {
				if stream.requested {
					if _, err := io.WriteString(stream.writer, "selected stream payload"); err != nil {
						t.Fatal(err)
					}
				} else if stream.writer != nil {
					t.Fatal("constructor allocated an unrequested stream")
				}
			}
			if err := pipes.closeParentWriters(); err != nil {
				t.Fatal(err)
			}
			consume := func(reader io.Reader) error {
				templatePipesAssertNoRawReader(t, reader)
				data, err := io.ReadAll(reader)
				if string(data) != "selected stream payload" {
					return errors.New("selected stream lost its actual payload")
				}
				return err
			}
			if streams.stdout {
				if err := pipes.ConsumeStdout(consume); err != nil {
					t.Fatal(err)
				}
			} else if err := pipes.ConsumeStdout(consume); !errors.Is(err, ErrClosed) {
				t.Fatalf("an unrequested stdout admitted a consumer: %v", err)
			}
			if streams.stderr {
				if err := pipes.ConsumeStderr(consume); err != nil {
					t.Fatal(err)
				}
			} else if err := pipes.ConsumeStderr(consume); !errors.Is(err, ErrClosed) {
				t.Fatalf("an unrequested stderr admitted a consumer: %v", err)
			}
			if snapshot := pipes.Snapshot(); snapshot.ReadersClosed != 0 || snapshot.ActiveConsumers != 0 || snapshot.ActiveReads != 0 {
				t.Fatalf("normal consumption closed a reader or retained its callback: %+v", snapshot)
			}
			if err := pipes.Close(); err != nil {
				t.Fatal(err)
			}
			if err := pipes.Close(); err != nil {
				t.Fatal(err)
			}
			if snapshot := pipes.Snapshot(); !snapshot.Closed || snapshot.ParentWritersClosed != streams.count ||
				snapshot.ReadersClosed != streams.count || snapshot.Joined || snapshot.Started || snapshot.Unknown {
				t.Fatalf("known no-start close did not retire exactly its selected ends: %+v", snapshot)
			}
		})
	}
}

func TestTemplatePipesConstructorRejectsBorrowedStreamsBeforeAllocation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	for _, stream := range []string{"stdout", "stderr"} {
		command := exec.Command("/unused-template")
		if stream == "stdout" {
			command.Stdout = writer
		} else {
			command.Stderr = writer
		}
		pipes, err := NewTemplatePipes(command, true, true)
		if pipes != nil || !errors.Is(err, ErrUnsafe) {
			t.Fatalf("constructor accepted an existing %s: %v", stream, err)
		}
		if _, err := writer.Write([]byte{1}); err != nil {
			t.Fatal("rejection closed the borrowed writer")
		}
		var value [1]byte
		if _, err := io.ReadFull(reader, value[:]); err != nil || value[0] != 1 {
			t.Fatalf("rejection modified the borrowed pair: %v", err)
		}
		if stream == "stdout" && (command.Stdout != writer || command.Stderr != nil) ||
			stream == "stderr" && (command.Stderr != writer || command.Stdout != nil) {
			t.Fatal("rejection changed an original template stream")
		}
	}
	for _, command := range []*exec.Cmd{nil, exec.Command("/unused-template")} {
		if pipes, err := NewTemplatePipes(command, false, false); pipes != nil || !errors.Is(err, ErrUnsafe) {
			t.Fatalf("an absent template or empty pipe selection allocated ownership: %v", err)
		}
	}
}

func TestTemplatePipesKnownNoStartClosesEveryActualEnd(t *testing.T) {
	for _, failure := range []string{"nil_owner", "nil_context", "cancelled_context"} {
		t.Run(failure, func(t *testing.T) {
			pipes, _ := templatePipesFixture(t, true, true)
			var ctx context.Context = context.Background()
			var domain *Domain
			wantErr := ErrUnavailable
			if failure == "nil_context" {
				ctx, domain, wantErr = nil, &Domain{}, ErrUnsafe
			} else if failure == "cancelled_context" {
				cancelled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx, domain, wantErr = cancelled, &Domain{}, context.Canceled
			}
			if err := pipes.Start(ctx, domain); !errors.Is(err, wantErr) {
				t.Fatalf("known no-start result = %v, want %v", err, wantErr)
			}
			if snapshot := pipes.Snapshot(); !snapshot.Closed || !snapshot.NoStartKnown || snapshot.Unknown ||
				snapshot.Starting || snapshot.Started || snapshot.Joined || snapshot.ParentWritersClosed != 2 || snapshot.ReadersClosed != 2 {
				t.Fatalf("known rejection retained or invented launch ownership: %+v", snapshot)
			}
			for _, pipe := range pipes.pipes {
				for _, file := range []*os.File{pipe.reader, pipe.writer} {
					if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
						t.Fatal("known rejection did not close an actual end")
					}
				}
			}
			if err := pipes.Wait(); !errors.Is(err, ErrWaitOwnership) {
				t.Fatal("known rejection invented a Process join")
			}
			if err := pipes.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type templatePipeAbnormalStartContext struct {
	context.Context
	abnormal func()
}

func (ctx *templatePipeAbnormalStartContext) Err() error {
	ctx.abnormal()
	return nil
}

func TestTemplatePipesAbnormalStartContextRetainsEveryOwnedEnd(t *testing.T) {
	for _, abnormal := range []struct {
		name string
		run  func()
	}{
		{"panic", func() { panic("start context failure") }},
		{"goexit", runtime.Goexit},
	} {
		t.Run(abnormal.name, func(t *testing.T) {
			pipes, template := templatePipesFixture(t, true, true)
			ctx := &templatePipeAbnormalStartContext{Context: context.Background(), abnormal: abnormal.run}
			done := make(chan struct{})
			var returned atomic.Bool
			go func() {
				defer func() { _ = recover(); close(done) }()
				// The actual Context.Err call exits before any concrete Domain
				// invocation. No mocked start or positive native owner is used.
				_ = pipes.Start(ctx, nil)
				returned.Store(true)
			}()
			templatePipesReceive(t, done, "the abnormal Start caller's actual goroutine exit")
			if returned.Load() {
				t.Fatal("the abnormal Start context returned normally")
			}
			if snapshot := pipes.Snapshot(); !snapshot.Unknown || snapshot.Starting || snapshot.Started || snapshot.Joined ||
				snapshot.NoStartKnown || snapshot.Closed || snapshot.ParentWritersClosed != 0 || snapshot.ReadersClosed != 0 ||
				snapshot.ActiveReads != 0 || snapshot.ActiveConsumers != 0 {
				t.Fatalf("abnormal Start lost or invented actual ownership: %+v", snapshot)
			}
			if template.Process != nil || template.ProcessState != nil {
				t.Fatal("the original template was started or waited")
			}
			// Reusing this permanently abnormal context would exit again. Unknown
			// ownership must refuse Close without invoking it a second time.
			if err := pipes.Close(); !errors.Is(err, ErrRetained) {
				t.Fatalf("abnormal Start accepted Close: %v", err)
			}
			if err := pipes.Cancel(); !errors.Is(err, ErrRetained) {
				t.Fatalf("cancellation erased abnormal Start: %v", err)
			}
			for _, pipe := range pipes.pipes {
				for _, file := range []*os.File{pipe.reader, pipe.writer} {
					if _, err := file.Stat(); err != nil {
						t.Fatalf("abnormal Start closed an owned pipe end: %v", err)
					}
				}
			}
		})
	}
}

func TestTemplatePipesUnknownNativeStartRetainsEveryOriginalEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux zero domain returns unknown retained ownership")
	}
	pipes, _ := templatePipesFixture(t, true, true)
	if err := pipes.Start(context.Background(), &Domain{}); !errors.Is(err, ErrRetained) {
		t.Fatalf("actual zero-domain rejection = %v", err)
	}
	if err := pipes.Close(); !errors.Is(err, ErrRetained) {
		t.Fatal("unknown start accepted Close")
	}
	if err := pipes.Cancel(); !errors.Is(err, ErrRetained) {
		t.Fatal("cancellation erased unknown start ownership")
	}
	if snapshot := pipes.Snapshot(); !snapshot.Unknown || snapshot.NoStartKnown || snapshot.Closed || snapshot.Started ||
		snapshot.Joined || snapshot.ParentWritersClosed != 0 || snapshot.ReadersClosed != 0 {
		t.Fatalf("unknown start disowned original pipe ends: %+v", snapshot)
	}
	for _, pipe := range pipes.pipes {
		for _, file := range []*os.File{pipe.reader, pipe.writer} {
			if _, err := file.Stat(); err != nil {
				t.Fatalf("unknown start closed an original end: %v", err)
			}
		}
	}
}

func TestTemplatePipesConsumeAndReadAreSingleOwnerBoundaries(t *testing.T) {
	pipes, _ := templatePipesFixture(t, true, false)
	readerEntered := make(chan io.Reader, 1)
	readEOF := make(chan struct{})
	returnGate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(returnGate) }) }
	defer release()
	done := make(chan error, 1)
	go func() {
		done <- pipes.ConsumeStdout(func(reader io.Reader) error {
			readerEntered <- reader
			var value [1]byte
			_, err := reader.Read(value[:])
			if !errors.Is(err, io.EOF) {
				return errors.New("the actual pipe read did not observe EOF")
			}
			close(readEOF)
			<-returnGate
			return nil
		})
	}()
	joined := false
	defer func() {
		_ = pipes.closeParentWriters()
		release()
		if !joined {
			templatePipesReceive(t, done, "consumer fixture cleanup")
		}
	}()
	reader := templatePipesReceive(t, readerEntered, "the admitted consumer")
	templatePipesWaitActiveReads(t, pipes, 1)
	var value [1]byte
	if _, err := reader.Read(value[:]); !errors.Is(err, ErrPipeBusy) {
		t.Fatalf("concurrent Read was accepted: %v", err)
	}
	var entered atomic.Bool
	if err := pipes.ConsumeStdout(func(io.Reader) error { entered.Store(true); return nil }); !errors.Is(err, ErrPipeBusy) || entered.Load() {
		t.Fatalf("concurrent callback was accepted: %v", err)
	}
	if err := pipes.closeParentWriters(); err != nil {
		t.Fatal(err)
	}
	templatePipesReceive(t, readEOF, "actual EOF before callback return")
	if snapshot := pipes.Snapshot(); snapshot.ActiveReads != 0 || snapshot.ActiveConsumers != 1 || snapshot.ReadersClosed != 0 {
		t.Fatalf("EOF was mistaken for callback completion: %+v", snapshot)
	}
	if err := pipes.Close(); !errors.Is(err, ErrRetained) {
		t.Fatalf("Close ignored an active no-child callback: %v", err)
	}
	release()
	if err := templatePipesReceive(t, done, "the exact consumer return"); err != nil {
		t.Fatal(err)
	}
	joined = true
	if err := pipes.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(value[:]); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retired reader admitted Read: %v", err)
	}
}

func TestTemplatePipesNormalConsumerRequiresObservedEOF(t *testing.T) {
	pipes, _ := templatePipesFixture(t, true, false)
	if err := pipes.ConsumeStdout(func(io.Reader) error { return nil }); !errors.Is(err, ErrPipeUndrained) {
		t.Fatalf("normal callback return certified unread output: %v", err)
	}
	if snapshot := pipes.Snapshot(); snapshot.ActiveConsumers != 0 || snapshot.ReadersClosed != 0 || snapshot.Unknown {
		t.Fatalf("undrained callback changed descriptor ownership: %+v", snapshot)
	}
	if err := pipes.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTemplatePipesAbnormalConsumerRetainsActualOwnership(t *testing.T) {
	for _, abnormal := range []struct {
		name string
		run  func()
	}{{"panic", func() { panic("consumer failure") }}, {"goexit", runtime.Goexit}} {
		t.Run(abnormal.name, func(t *testing.T) {
			pipes, _ := templatePipesFixture(t, true, false)
			done := make(chan struct{})
			var returned atomic.Bool
			go func() {
				defer func() { _ = recover(); close(done) }()
				_ = pipes.ConsumeStdout(func(io.Reader) error { abnormal.run(); return nil })
				returned.Store(true)
			}()
			templatePipesReceive(t, done, "the abnormal consumer goroutine exit")
			if returned.Load() {
				t.Fatal("an abnormal callback returned normally")
			}
			if snapshot := pipes.Snapshot(); !snapshot.Unknown || snapshot.Closed || snapshot.ActiveConsumers != 0 ||
				snapshot.ReadersClosed != 0 || snapshot.ParentWritersClosed != 0 || snapshot.Joined {
				t.Fatalf("abnormal callback disowned its descriptors: %+v", snapshot)
			}
			if pipes.pipes[0].consumer == nil {
				t.Fatal("abnormal callback lost its exact reachable owner")
			}
			if err := pipes.Close(); !errors.Is(err, ErrRetained) {
				t.Fatal("abnormal consumer accepted Close")
			}
			for _, file := range []*os.File{pipes.pipes[0].reader, pipes.pipes[0].writer} {
				if _, err := file.Stat(); err != nil {
					t.Fatalf("abnormal callback closed an original end: %v", err)
				}
			}
		})
	}
}

type templatePipeConsumeResult struct {
	data string
	err  error
}

func TestTemplatePipesCopiedCommandWaitPreservesSlowConsumerTail(t *testing.T) {
	pipes, template := templatePipesFixture(t, true, true)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestTemplatePipesSubprocessHelper$", "--", templatePipeHelperArgument)
	command.Env = append(os.Environ(), templatePipeHelperEnvironment+"=1")
	command.Stdout, command.Stderr = template.Stdout, template.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	childWaited := false
	defer func() {
		if !childWaited {
			// The concrete owned child is still unreaped. Never signal a numeric
			// PID or call its Cancel after an actual Wait attempt.
			if command.Cancel != nil {
				_ = command.Cancel()
			}
			childWaited = true
			_ = command.Wait()
		}
	}()
	tailGate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(tailGate) }) }
	defer release()
	prefixRead := make(chan struct{}, 2)
	done := make(chan templatePipeConsumeResult, 2)
	consume := func(prefix string) func(io.Reader) error {
		return func(reader io.Reader) error {
			data := make([]byte, len(prefix))
			if _, err := io.ReadFull(reader, data); err != nil {
				return err
			}
			if string(data) != prefix {
				return errors.New("child prefix was changed")
			}
			prefixRead <- struct{}{}
			<-tailGate
			tail, err := io.ReadAll(reader)
			done <- templatePipeConsumeResult{data: string(data) + string(tail), err: err}
			return err
		}
	}
	callbackDone := make(chan error, 2)
	go func() { callbackDone <- pipes.ConsumeStdout(consume(templatePipeStdoutPrefix)) }()
	go func() { callbackDone <- pipes.ConsumeStderr(consume(templatePipeStderrPrefix)) }()
	consumersJoined := false
	defer func() {
		_ = pipes.closeParentWriters()
		release()
		if !consumersJoined {
			for range 2 {
				templatePipesReceive(t, callbackDone, "copied-command consumer cleanup")
			}
		}
	}()
	for range 2 {
		templatePipesReceive(t, prefixRead, "the child prefix before deferred tail consumption")
	}
	childWaited = true
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if template.Process != nil || template.ProcessState != nil {
		t.Fatal("the original template was started or waited")
	}
	if snapshot := pipes.Snapshot(); snapshot.ActiveConsumers != 2 || snapshot.ReadersClosed != 0 || snapshot.ParentWritersClosed != 0 ||
		snapshot.Started || snapshot.Joined {
		t.Fatalf("ordinary copied-child Wait closed output or claimed a native join: %+v", snapshot)
	}
	// The actual child has exited, but its copied Cmd treated these parent
	// writers as borrowed files. Closing their exact owners establishes EOF.
	release()
	templatePipesWaitActiveReads(t, pipes, 2)
	select {
	case result := <-done:
		t.Fatalf("the retained parent writer did not withhold EOF: %+v", result)
	default:
	}
	for _, pipe := range pipes.pipes {
		if _, err := pipe.writer.Stat(); err != nil {
			t.Fatalf("copied child Wait retired a parent writer: %v", err)
		}
	}
	if err := pipes.closeParentWriters(); err != nil {
		t.Fatal(err)
	}
	results := make(map[string]bool)
	for range 2 {
		result := templatePipesReceive(t, done, "the complete buffered tail")
		if result.err != nil {
			t.Fatal(result.err)
		}
		results[result.data] = true
	}
	for range 2 {
		if err := templatePipesReceive(t, callbackDone, "the callback after tail EOF"); err != nil {
			t.Fatal(err)
		}
	}
	consumersJoined = true
	if !results[templatePipeStdoutPrefix+templatePipeStdoutTail] || !results[templatePipeStderrPrefix+templatePipeStderrTail] {
		t.Fatalf("actual child Wait lost stdout/stderr tail sentinels: %#v", results)
	}
	if snapshot := pipes.Snapshot(); snapshot.ActiveConsumers != 0 || snapshot.ActiveReads != 0 || snapshot.ReadersClosed != 0 || snapshot.Unknown {
		t.Fatalf("normal EOF/callback return did not retain unread-close ownership: %+v", snapshot)
	}
	if err := pipes.closeReaders(); err != nil {
		t.Fatal(err)
	}
	if err := pipes.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTemplatePipesSubprocessHelper(t *testing.T) {
	if os.Getenv(templatePipeHelperEnvironment) != "1" {
		return
	}
	if len(os.Args) != 4 || os.Args[1] != "-test.run=^TestTemplatePipesSubprocessHelper$" || os.Args[2] != "--" || os.Args[3] != templatePipeHelperArgument {
		os.Exit(92)
	}
	for _, stream := range []struct {
		writer io.Writer
		data   string
	}{
		{os.Stdout, templatePipeStdoutPrefix + templatePipeStdoutTail},
		{os.Stderr, templatePipeStderrPrefix + templatePipeStderrTail},
	} {
		if _, err := io.Copy(stream.writer, strings.NewReader(stream.data)); err != nil {
			os.Exit(93)
		}
	}
	os.Exit(0)
}
