package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func workspaceWriterTestScope(t *testing.T, directory string, capacity int) *workspaceWriterScope {
	t.Helper()
	scope, err := newWorkspaceWriterScope(directory, capacity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := scope.drain(ctx); errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Errorf("test cleanup did not join owned writers: %v", err)
			return
		}
		// A failed closer may return while a quarantined task remains live.
		// Join those fixture callbacks before closing their retained root.
		scope.mu.Lock()
		tasks := make([]*workspaceWriterTaskHandle, 0, len(scope.entries))
		for _, entry := range scope.entries {
			if entry != nil && entry.task != nil {
				tasks = append(tasks, entry.task)
			}
		}
		scope.mu.Unlock()
		for _, task := range tasks {
			select {
			case <-task.done:
			case <-ctx.Done():
				t.Errorf("test cleanup retained an unreturned quarantined task: %v", ctx.Err())
				return
			}
		}
		// Quarantined roots remain owned until the assertions complete. The
		// fixture closes them only after the fixed closer has exited.
		if !scope.snapshot().RootClosed {
			if err := scope.root.Close(); err != nil {
				t.Errorf("close quarantined test root: %v", err)
			}
		}
	})
	return scope
}

func workspaceWriterTestRelease(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	released := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	return released, release
}

func workspaceWriterTestWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("operation did not reach its barrier: %v", ctx.Err())
	}
}

func workspaceWriterTestDrain(t *testing.T, scope *workspaceWriterScope) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return scope.drain(ctx)
}

func workspaceWriterTestTaskWait(t *testing.T, task *workspaceWriterTaskHandle) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return task.wait(ctx)
}

func workspaceWriterTestExpiredContext() (context.Context, context.CancelFunc) {
	return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
}

func workspaceWriterTestAssertRetainedRoot(t *testing.T, scope *workspaceWriterScope) {
	t.Helper()
	if scope.snapshot().RootClosed {
		t.Fatal("the retained root was marked closed")
	}
	if _, err := scope.root.Stat("."); err != nil {
		t.Fatalf("the retained root descriptor was closed: %v", err)
	}
}

func workspaceWriterTestAssertDrained(t *testing.T, scope *workspaceWriterScope) {
	t.Helper()
	snapshot := scope.snapshot()
	if !snapshot.AdmissionClosed || !snapshot.RootClosed || !snapshot.Done || snapshot.Failure != nil ||
		snapshot.Opening != 0 || snapshot.Files != 0 || snapshot.Mutations != 0 || snapshot.Tasks != 0 || snapshot.Quarantined != 0 {
		t.Fatalf("successful drain retained writer ownership: %+v", snapshot)
	}
	if _, err := scope.root.Stat("."); err == nil {
		t.Fatalf("successful drain did not close the root descriptor: %v", err)
	}
}

func workspaceWriterTestAssertNoPath(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected operation changed %q: %v", name, err)
	}
}

type workspaceWriterTestObservedContext struct {
	calls atomic.Int32
}

func (c *workspaceWriterTestObservedContext) Deadline() (time.Time, bool) {
	c.calls.Add(1)
	return time.Time{}, false
}

func (c *workspaceWriterTestObservedContext) Done() <-chan struct{} {
	c.calls.Add(1)
	return nil
}

func (c *workspaceWriterTestObservedContext) Err() error {
	c.calls.Add(1)
	return nil
}

func (c *workspaceWriterTestObservedContext) Value(any) any {
	c.calls.Add(1)
	return nil
}

type workspaceWriterTestSetupContext struct {
	context.Context
	entered  chan struct{}
	released <-chan struct{}
	once     sync.Once
}

func (c *workspaceWriterTestSetupContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	<-c.released
	return c.Context.Done()
}

type workspaceWriterTestAfterFuncContext struct {
	context.Context
	done chan struct{}
	stop func()
}

func (c *workspaceWriterTestAfterFuncContext) Done() <-chan struct{} { return c.done }

func (c *workspaceWriterTestAfterFuncContext) AfterFunc(func()) func() bool {
	return func() bool {
		c.stop()
		return true
	}
}

func TestWorkspaceWriterScopeRejectsRootEscapesWithoutMutation(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "workspace")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(directory, "inside.txt")
	if err := os.WriteFile(inside, []byte("inside sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := workspaceWriterTestScope(t, directory, 1)
	for _, name := range []string{"../outside.txt", outside, "../created.txt", filepath.Join(parent, "absolute-created.txt")} {
		file, err := scope.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if file != nil {
			_ = file.Close()
		}
		if err == nil || file != nil {
			t.Fatalf("root escape opened a writable descriptor for %q: %v", name, err)
		}
	}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"mkdir", func() error { return scope.mkdir("../escaped-dir", 0o700) }},
		{"mkdir-all", func() error { return scope.mkdirAll("../escaped-tree/child", 0o700) }},
		{"rename-out", func() error { return scope.rename("inside.txt", "../renamed-out.txt") }},
		{"rename-in", func() error { return scope.rename("../outside.txt", "renamed-in.txt") }},
		{"remove", func() error { return scope.remove("../outside.txt") }},
	} {
		if err := operation.run(); err == nil {
			t.Fatalf("%s admitted a root escape", operation.name)
		}
	}
	for _, fixture := range []struct {
		name string
		want string
	}{{outside, "outside sentinel"}, {inside, "inside sentinel"}} {
		data, err := os.ReadFile(fixture.name)
		if err != nil || string(data) != fixture.want {
			t.Fatalf("root escape mutated %q: data=%q error=%v", fixture.name, data, err)
		}
	}
	for _, name := range []string{"created.txt", "absolute-created.txt", "escaped-dir", "escaped-tree", "renamed-out.txt"} {
		workspaceWriterTestAssertNoPath(t, filepath.Join(parent, name))
	}
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "renamed-in.txt"))
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatalf("normal path rejection quarantined writers: %v", err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeAllWriterKindsShareCapacity(t *testing.T) {
	directory := t.TempDir()
	scope := workspaceWriterTestScope(t, directory, 4)
	// Reserve the opening phase directly so this test does not depend on a
	// platform-specific filesystem operation blocking inside OpenFile.
	opening, err := scope.reserve(workspaceWriterOpening)
	if err != nil {
		t.Fatal(err)
	}
	var releaseOpeningOnce sync.Once
	releaseOpening := func() { releaseOpeningOnce.Do(func() { scope.finish(opening, false, nil) }) }
	t.Cleanup(releaseOpening)
	file, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	mutationRelease, releaseMutation := workspaceWriterTestRelease(t)
	mutationEntered := make(chan struct{})
	mutationDone := make(chan struct{})
	var mutationErr error
	go func() {
		defer close(mutationDone)
		mutationErr = scope.mutate(func() error {
			close(mutationEntered)
			<-mutationRelease
			return nil
		})
	}()
	workspaceWriterTestWait(t, mutationEntered)
	taskRelease, releaseTask := workspaceWriterTestRelease(t)
	taskEntered := make(chan struct{})
	task, err := scope.start(context.Background(), func(context.Context) error {
		close(taskEntered)
		<-taskRelease
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestWait(t, taskEntered)
	if snapshot := scope.snapshot(); snapshot.Capacity != 4 || snapshot.Opening != 1 || snapshot.Files != 1 ||
		snapshot.Mutations != 1 || snapshot.Tasks != 1 || snapshot.Quarantined != 0 {
		t.Fatalf("writer categories did not occupy one fixed capacity: %+v", snapshot)
	}
	if extra, err := scope.reserve(workspaceWriterOpening); extra != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("an opening exceeded shared capacity: entry=%v error=%v", extra, err)
	}
	if extra, err := scope.openFile("denied.txt", os.O_WRONLY|os.O_CREATE, 0o600); extra != nil || !errors.Is(err, ErrBusy) {
		if extra != nil {
			_ = extra.Close()
		}
		t.Fatalf("a writable file exceeded shared capacity: %v", err)
	}
	if err := scope.mkdir("denied-dir", 0o700); !errors.Is(err, ErrBusy) {
		t.Fatalf("a metadata mutation exceeded shared capacity: %v", err)
	}
	deniedTaskEntered := make(chan struct{})
	if extra, err := scope.start(context.Background(), func(context.Context) error {
		close(deniedTaskEntered)
		return nil
	}); extra != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("a task exceeded shared capacity: task=%v error=%v", extra, err)
	}
	select {
	case <-deniedTaskEntered:
		t.Fatal("a rejected task callback ran")
	default:
	}
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "denied.txt"))
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "denied-dir"))
	releaseOpening()
	releaseMutation()
	releaseTask()
	workspaceWriterTestWait(t, mutationDone)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if err := workspaceWriterTestTaskWait(t, task); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeStopClosesAdmissionBeforeJoiningTasks(t *testing.T) {
	directory := t.TempDir()
	scope := workspaceWriterTestScope(t, directory, 3)
	released, release := workspaceWriterTestRelease(t)
	entered := make(chan struct{})
	canceled := make(chan struct{})
	task, err := scope.start(context.Background(), func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-released
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestWait(t, entered)
	scope.stop()
	if file, err := scope.openFile("late.txt", os.O_WRONLY|os.O_CREATE, 0o600); file != nil || !errors.Is(err, errWorkspaceWriterClosed) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("stop admitted a writable file before drain: %v", err)
	}
	if err := scope.mkdir("late-dir", 0o700); !errors.Is(err, errWorkspaceWriterClosed) {
		t.Fatalf("stop admitted a metadata mutation before drain: %v", err)
	}
	if next, err := scope.start(context.Background(), func(context.Context) error { return nil }); next != nil || !errors.Is(err, errWorkspaceWriterClosed) {
		t.Fatalf("stop admitted another task before drain: task=%v error=%v", next, err)
	}
	workspaceWriterTestWait(t, canceled)
	ctx, cancel := workspaceWriterTestExpiredContext()
	defer cancel()
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a canceled callback that had not returned allowed drain: %v", err)
	}
	if err := task.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("task cancellation was treated as an actual return: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Tasks != 1 || snapshot.Capacity != 3 || !snapshot.AdmissionClosed ||
		snapshot.Done || snapshot.RootClosed || snapshot.Failure != nil {
		t.Fatalf("timeout or cancellation discarded an owned task: %+v", snapshot)
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "late.txt"))
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "late-dir"))
	release()
	if err := workspaceWriterTestTaskWait(t, task); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeBlockedCallbackRetainsOwnershipAfterDrainDeadline(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 1)
	released, release := workspaceWriterTestRelease(t)
	entered := make(chan struct{})
	task, err := scope.start(context.Background(), func(context.Context) error {
		close(entered)
		<-released
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestWait(t, entered)
	ctx, cancel := workspaceWriterTestExpiredContext()
	defer cancel()
	for range 2 {
		if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("an unreturned callback did not bound the drain observation: %v", err)
		}
	}
	if snapshot := scope.snapshot(); snapshot.Tasks != 1 || snapshot.Capacity != 1 || !snapshot.AdmissionClosed ||
		snapshot.Done || snapshot.RootClosed || snapshot.Failure != nil {
		t.Fatalf("repeated drain timeout lost live ownership: %+v", snapshot)
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	release()
	if err := workspaceWriterTestTaskWait(t, task); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeStoppedMutationRemainsOwnedUntilReturn(t *testing.T) {
	directory := t.TempDir()
	scope := workspaceWriterTestScope(t, directory, 2)
	released, release := workspaceWriterTestRelease(t)
	entered := make(chan struct{})
	done := make(chan struct{})
	var mutationErr error
	go func() {
		defer close(done)
		mutationErr = scope.mutate(func() error {
			close(entered)
			<-released
			return nil
		})
	}()
	workspaceWriterTestWait(t, entered)
	scope.stop()
	if err := scope.mkdir("late-dir", 0o700); !errors.Is(err, errWorkspaceWriterClosed) {
		t.Fatalf("stop admitted another mutation while an accepted mutation remained active: %v", err)
	}
	ctx, cancel := workspaceWriterTestExpiredContext()
	defer cancel()
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an unreturned metadata mutation allowed drain: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Mutations != 1 || snapshot.Capacity != 2 || !snapshot.AdmissionClosed ||
		snapshot.Done || snapshot.RootClosed || snapshot.Failure != nil {
		t.Fatalf("stop or timeout discarded mutation ownership: %+v", snapshot)
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "late-dir"))
	release()
	workspaceWriterTestWait(t, done)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeJoinsEveryTaskAcceptedBeforeStop(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 2)
	tasks := make([]*workspaceWriterTaskHandle, 0, 2)
	releases := make([]func(), 0, 2)
	for range 2 {
		released, release := workspaceWriterTestRelease(t)
		entered := make(chan struct{})
		task, err := scope.start(context.Background(), func(context.Context) error {
			close(entered)
			<-released
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		workspaceWriterTestWait(t, entered)
		tasks = append(tasks, task)
		releases = append(releases, release)
	}
	scope.stop()
	ctx, cancel := workspaceWriterTestExpiredContext()
	defer cancel()
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop discarded tasks accepted before admission closed: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Tasks != 2 || snapshot.Done || snapshot.RootClosed {
		t.Fatalf("accepted tasks were not retained together: %+v", snapshot)
	}
	releases[0]()
	if err := workspaceWriterTestTaskWait(t, tasks[0]); err != nil {
		t.Fatal(err)
	}
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("one returned task released an unreturned peer: %v", err)
	}
	select {
	case <-tasks[1].done:
		t.Fatal("the blocked peer was marked returned")
	default:
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	releases[1]()
	if err := workspaceWriterTestTaskWait(t, tasks[1]); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeRejectedTaskDoesNotSetUpParentContext(t *testing.T) {
	for _, state := range []string{"busy", "closed"} {
		t.Run(state, func(t *testing.T) {
			scope := workspaceWriterTestScope(t, t.TempDir(), 1)
			wantErr := errWorkspaceWriterClosed
			if state == "busy" {
				if _, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600); err != nil {
					t.Fatal(err)
				}
				wantErr = ErrBusy
			} else {
				scope.stop()
			}
			parent := &workspaceWriterTestObservedContext{}
			if task, err := scope.start(parent, func(context.Context) error { return nil }); task != nil || !errors.Is(err, wantErr) {
				t.Fatalf("task admission returned an unexpected result: task=%v error=%v", task, err)
			}
			if calls := parent.calls.Load(); calls != 0 {
				t.Fatalf("rejected task performed parent context setup: calls=%d", calls)
			}
		})
	}
}

func TestWorkspaceWriterScopeJoinsTaskWhoseContextSetupOverlapsStop(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 1)
	setupReleased, releaseSetup := workspaceWriterTestRelease(t)
	callbackReleased, releaseCallback := workspaceWriterTestRelease(t)
	parent := &workspaceWriterTestSetupContext{Context: context.Background(), entered: make(chan struct{}), released: setupReleased}
	callbackEntered := make(chan struct{})
	startDone := make(chan struct{})
	var task *workspaceWriterTaskHandle
	var startErr error
	go func() {
		defer close(startDone)
		task, startErr = scope.start(parent, func(ctx context.Context) error {
			close(callbackEntered)
			<-callbackReleased
			if !errors.Is(ctx.Err(), context.Canceled) {
				return errors.New("a task published after stop did not receive cancellation")
			}
			return nil
		})
	}()
	workspaceWriterTestWait(t, parent.entered)
	scope.stop()
	ctx, cancel := workspaceWriterTestExpiredContext()
	defer cancel()
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a task still constructing its context allowed drain: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Tasks != 1 || !snapshot.AdmissionClosed || snapshot.Done || snapshot.RootClosed {
		t.Fatalf("stop discarded an accepted task before publication: %+v", snapshot)
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	releaseSetup()
	workspaceWriterTestWait(t, startDone)
	if startErr != nil || task == nil {
		t.Fatalf("stop rejected a task already accepted before context setup: task=%v error=%v", task, startErr)
	}
	workspaceWriterTestWait(t, callbackEntered)
	if err := scope.drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("task publication released an unreturned callback: %v", err)
	}
	releaseCallback()
	if err := workspaceWriterTestTaskWait(t, task); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeAbnormalParentCancellationRetainsUnreturnedTask(t *testing.T) {
	for _, abnormal := range []struct {
		name string
		run  func()
	}{
		{"panic", func() { panic("injected parent cancellation panic") }},
		{"goexit", runtime.Goexit},
	} {
		t.Run(abnormal.name, func(t *testing.T) {
			scope := workspaceWriterTestScope(t, t.TempDir(), 1)
			released, release := workspaceWriterTestRelease(t)
			parent := &workspaceWriterTestAfterFuncContext{Context: context.Background(), done: make(chan struct{}), stop: abnormal.run}
			entered := make(chan struct{})
			task, err := scope.start(parent, func(context.Context) error {
				close(entered)
				<-released
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			workspaceWriterTestWait(t, entered)
			if err := workspaceWriterTestDrain(t, scope); !errors.Is(err, errWorkspaceWriterAbnormal) {
				t.Fatalf("abnormal parent cancellation did not fail closed: %v", err)
			}
			if snapshot := scope.snapshot(); snapshot.Capacity != 1 || snapshot.Quarantined != 1 || snapshot.Tasks != 0 ||
				!snapshot.AdmissionClosed || snapshot.RootClosed || !snapshot.Done || !errors.Is(snapshot.Failure, errWorkspaceWriterAbnormal) {
				t.Fatalf("abnormal cancellation discarded an unreturned task: %+v", snapshot)
			}
			ctx, cancel := workspaceWriterTestExpiredContext()
			defer cancel()
			if err := task.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("failed drain claimed the blocked task had actually returned: %v", err)
			}
			workspaceWriterTestAssertRetainedRoot(t, scope)
			release()
			if err := workspaceWriterTestTaskWait(t, task); err != nil {
				t.Fatalf("the callback fixture did not return normally after release: %v", err)
			}
			if err := workspaceWriterTestDrain(t, scope); !errors.Is(err, errWorkspaceWriterAbnormal) {
				t.Fatalf("a later normal callback return erased cancellation quarantine: %v", err)
			}
			if snapshot := scope.snapshot(); snapshot.Quarantined != 1 || snapshot.RootClosed {
				t.Fatalf("a later normal callback return released quarantine ownership: %+v", snapshot)
			}
			workspaceWriterTestAssertRetainedRoot(t, scope)
		})
	}
}

func TestWorkspaceWritableFileCloseClosesDescriptorAndRejectsLaterOperations(t *testing.T) {
	directory := t.TempDir()
	scope := workspaceWriterTestScope(t, directory, 1)
	file, err := scope.openFile("owned.txt", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	raw := file.file
	if n, err := file.Write([]byte("owned")); err != nil || n != len("owned") {
		t.Fatalf("write owned descriptor: n=%d error=%v", n, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("wrapper Close did not close the actual descriptor: %v", err)
	}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"write", func() error { _, err := file.Write([]byte("late")); return err }},
		{"write-at", func() error { _, err := file.WriteAt([]byte("late"), 0); return err }},
		{"seek", func() error { _, err := file.Seek(0, io.SeekStart); return err }},
		{"truncate", func() error { return file.Truncate(0) }},
		{"sync", file.Sync},
	} {
		if err := operation.run(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closed wrapper admitted %s: %v", operation.name, err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatalf("repeated successful Close changed its result: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Files != 0 || snapshot.Quarantined != 0 || snapshot.Failure != nil {
		t.Fatalf("actual Close did not return its owned slot: %+v", snapshot)
	}
	data, err := os.ReadFile(filepath.Join(directory, "owned.txt"))
	if err != nil || string(data) != "owned" {
		t.Fatalf("a rejected operation changed the closed file: data=%q error=%v", data, err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeAbnormalTaskRetainsQuarantineAfterDescriptorClose(t *testing.T) {
	for _, abnormal := range []struct {
		name string
		run  func()
	}{
		{"panic", func() { panic("injected task panic") }},
		{"goexit", runtime.Goexit},
	} {
		t.Run(abnormal.name, func(t *testing.T) {
			scope := workspaceWriterTestScope(t, t.TempDir(), 2)
			file, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			raw := file.file
			closed := make(chan error, 1)
			task, err := scope.start(context.Background(), func(context.Context) error {
				defer func() { closed <- file.Close() }()
				abnormal.run()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := workspaceWriterTestTaskWait(t, task); !errors.Is(err, errWorkspaceWriterAbnormal) {
				t.Fatalf("an abnormal task was treated as a normal return: %v", err)
			}
			if err := <-closed; err != nil {
				t.Fatalf("the task fixture did not close its descriptor: %v", err)
			}
			if _, err := raw.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("the task's deferred Close left its descriptor writable: %v", err)
			}
			// Drain also joins the fixed closer before inspecting whether the
			// quarantined root remains open.
			if err := workspaceWriterTestDrain(t, scope); !errors.Is(err, errWorkspaceWriterAbnormal) {
				t.Fatalf("descriptor Close erased abnormal task ownership: %v", err)
			}
			if snapshot := scope.snapshot(); snapshot.Capacity != 2 || snapshot.Quarantined != 1 || snapshot.Files != 0 ||
				snapshot.Tasks != 0 || !snapshot.AdmissionClosed || snapshot.RootClosed || !snapshot.Done ||
				!errors.Is(snapshot.Failure, errWorkspaceWriterAbnormal) {
				t.Fatalf("abnormal task quarantine was not retained: %+v", snapshot)
			}
			workspaceWriterTestAssertRetainedRoot(t, scope)
		})
	}
}

func TestWorkspaceWriterScopeAbnormalMutationRetainsQuarantineAfterDescriptorClose(t *testing.T) {
	for _, abnormal := range []struct {
		name string
		run  func()
	}{
		{"panic", func() { panic("injected mutation panic") }},
		{"goexit", runtime.Goexit},
	} {
		t.Run(abnormal.name, func(t *testing.T) {
			scope := workspaceWriterTestScope(t, t.TempDir(), 2)
			file, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			raw := file.file
			closed := make(chan error, 1)
			done := make(chan struct{})
			var mutationErr error
			returned := false
			go func() {
				defer close(done)
				mutationErr = scope.mutate(func() error {
					defer func() { closed <- file.Close() }()
					abnormal.run()
					return nil
				})
				returned = true
			}()
			workspaceWriterTestWait(t, done)
			if abnormal.name == "panic" && (!returned || !errors.Is(mutationErr, errWorkspaceWriterAbnormal)) {
				t.Fatalf("mutation panic did not report abnormal completion: returned=%t error=%v", returned, mutationErr)
			}
			if abnormal.name == "goexit" && returned {
				t.Fatal("the Goexit mutation fixture returned normally")
			}
			if err := <-closed; err != nil {
				t.Fatalf("the mutation fixture did not close its descriptor: %v", err)
			}
			if _, err := raw.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("the mutation's deferred Close left its descriptor writable: %v", err)
			}
			if err := workspaceWriterTestDrain(t, scope); !errors.Is(err, errWorkspaceWriterAbnormal) {
				t.Fatalf("descriptor Close erased abnormal mutation ownership: %v", err)
			}
			if snapshot := scope.snapshot(); snapshot.Capacity != 2 || snapshot.Quarantined != 1 || snapshot.Files != 0 ||
				snapshot.Mutations != 0 || !snapshot.AdmissionClosed || snapshot.RootClosed || !snapshot.Done ||
				!errors.Is(snapshot.Failure, errWorkspaceWriterAbnormal) {
				t.Fatalf("abnormal mutation quarantine was not retained: %+v", snapshot)
			}
			workspaceWriterTestAssertRetainedRoot(t, scope)
		})
	}
}

func TestWorkspaceWritableFileCloseFailureRetainsOwnershipAndClosesAdmission(t *testing.T) {
	directory := t.TempDir()
	scope := workspaceWriterTestScope(t, directory, 1)
	file, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// Only this package-private fixture bypasses the wrapper, injecting an
	// actual Close error without adding a production descriptor accessor.
	if err := file.file.Close(); err != nil {
		t.Fatal(err)
	}
	closeErr := file.Close()
	if !errors.Is(closeErr, os.ErrClosed) {
		t.Fatalf("the descriptor pre-close did not inject a Close failure: %v", closeErr)
	}
	if err := file.Close(); err != closeErr {
		t.Fatalf("repeated failed Close lost the original error: first=%v next=%v", closeErr, err)
	}
	if _, err := file.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("a failed Close reopened wrapper writes: %v", err)
	}
	if next, err := scope.openFile("late.txt", os.O_WRONLY|os.O_CREATE, 0o600); next != nil || !errors.Is(err, errWorkspaceWriterClosed) {
		if next != nil {
			_ = next.Close()
		}
		t.Fatalf("Close failure left writer admission open: %v", err)
	}
	if err := workspaceWriterTestDrain(t, scope); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("drain discarded the actual Close failure: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Capacity != 1 || snapshot.Quarantined != 1 || snapshot.Files != 0 ||
		!snapshot.AdmissionClosed || snapshot.RootClosed || !snapshot.Done || !errors.Is(snapshot.Failure, os.ErrClosed) {
		t.Fatalf("failed Close did not retain quarantine and root ownership: %+v", snapshot)
	}
	workspaceWriterTestAssertRetainedRoot(t, scope)
	workspaceWriterTestAssertNoPath(t, filepath.Join(directory, "late.txt"))
}

func TestWorkspaceWriterScopeNormalTaskErrorRemainsJoined(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 1)
	callbackErr := errors.New("injected normal callback error")
	task, err := scope.start(context.Background(), func(context.Context) error { return callbackErr })
	if err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestTaskWait(t, task); !errors.Is(err, callbackErr) {
		t.Fatalf("task wait lost the callback error: %v", err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatalf("a normal error return quarantined a joined task: %v", err)
	}
	if err := workspaceWriterTestTaskWait(t, task); !errors.Is(err, callbackErr) {
		t.Fatalf("scope drain changed the task result: %v", err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeNormalMutationErrorReturnsCapacity(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 1)
	mutationErr := errors.New("injected normal mutation error")
	if err := scope.mutate(func() error { return mutationErr }); !errors.Is(err, mutationErr) {
		t.Fatalf("a normal mutation lost its production error: %v", err)
	}
	if snapshot := scope.snapshot(); snapshot.Mutations != 0 || snapshot.Quarantined != 0 || snapshot.AdmissionClosed || snapshot.Failure != nil {
		t.Fatalf("a normal mutation error retained its writer slot: %+v", snapshot)
	}
	file, err := scope.openFile("next.txt", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("a joined mutation did not return shared capacity: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	workspaceWriterTestAssertDrained(t, scope)
}

func TestWorkspaceWriterScopeDrainIsIdempotentAndClosesOwnedFiles(t *testing.T) {
	scope := workspaceWriterTestScope(t, t.TempDir(), 1)
	file, err := scope.openFile("owned.txt", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	raw := file.file
	if err := workspaceWriterTestDrain(t, scope); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("drain did not close its owned descriptor: %v", err)
	}
	if _, err := file.Write([]byte("late")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("drain did not close its writable wrapper: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close after successful drain changed the result: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 2 {
		scope.stop()
		if err := scope.drain(ctx); err != nil {
			t.Fatalf("a completed drain depended on a later observation context: %v", err)
		}
	}
	workspaceWriterTestAssertDrained(t, scope)
}
