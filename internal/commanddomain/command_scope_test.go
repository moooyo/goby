package commanddomain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandScopeZeroAndNilFailClosed(t *testing.T) {
	for _, scope := range []*CommandScope{nil, {}} {
		template := commandScopeTestTemplate()
		if process, err := scope.Start(context.Background(), template); process != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("an unissued scope started a process: process=%v error=%v", process, err)
		}
		if pipes, err := scope.StartTemplatePipes(context.Background(), template, true, true); pipes != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("an unissued scope started template pipes: pipes=%v error=%v", pipes, err)
		}
		if err := scope.Run(context.Background(), template); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("an unissued scope ran a command: %v", err)
		}
		if err := scope.Drain(context.Background()); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("an unissued scope claimed a drain: %v", err)
		}
		scope.CloseGate()
		if snapshot := scope.Snapshot(); snapshot.Capacity != 0 || snapshot.Active != 0 || !snapshot.GateClosed || !snapshot.Quarantined || snapshot.Closed {
			t.Fatalf("an unissued scope reported native lifetime closure: %+v", snapshot)
		}
	}
}

func TestScopedHandlesZeroAndNilRejectOwnershipOperations(t *testing.T) {
	for _, process := range []*ScopedProcess{nil, {}} {
		if err := process.Wait(); !errors.Is(err, ErrWaitOwnership) {
			t.Fatalf("an unissued process claimed a wait: %v", err)
		}
		if err := process.SignalCancel(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("an unissued process accepted cancellation: %v", err)
		}
		if code, known := process.ExitCode(); code != -1 || known || process.RetirementComplete() {
			t.Fatal("an unissued process reported actual exit or retirement")
		}
	}
	for _, pipes := range []*ScopedTemplatePipes{nil, {}} {
		if err := pipes.Wait(); !errors.Is(err, ErrWaitOwnership) {
			t.Fatalf("unissued pipes claimed a process wait: %v", err)
		}
		if err := pipes.SignalCancel(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unissued pipes accepted cancellation: %v", err)
		}
		if err := pipes.Close(); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unissued pipes claimed closure: %v", err)
		}
		called := false
		consume := func(io.Reader) error { called = true; return nil }
		if err := pipes.ConsumeStdout(consume); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unissued pipes admitted a stdout consumer: %v", err)
		}
		if err := pipes.ConsumeStderr(consume); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("unissued pipes admitted a stderr consumer: %v", err)
		}
		if called {
			t.Fatal("an unissued pipe wrapper invoked caller code")
		}
		if code, known := pipes.ExitCode(); code != -1 || known || pipes.RetirementComplete() {
			t.Fatal("unissued pipes reported actual exit or retirement")
		}
	}
}

func TestCommandScopeContextPreservesExplicitInvalidPresence(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		if scope, present := CommandScopeFromContext(ctx); scope != nil || present {
			t.Fatalf("an absent binding was reported as native presence: scope=%v present=%v", scope, present)
		}
	}
	var typedNil *CommandScope
	if scope, present := CommandScopeFromContext(WithCommandScope(context.Background(), typedNil)); scope != nil || !present {
		t.Fatal("a typed nil scope was treated as absent legacy routing")
	}
	unissued := &CommandScope{}
	if scope, present := CommandScopeFromContext(WithCommandScope(context.Background(), unissued)); scope != unissued || !present {
		t.Fatal("the binder replaced its exact opaque owner")
	}
	if scope, present := CommandScopeFromContext(WithCommandScope(nil, unissued)); scope != nil || !present {
		t.Fatal("nil context did not produce an explicitly invalid present binding")
	}
	if scope, present := CommandScopeFromContext(WithDomain(context.Background(), nil)); scope != nil || !present {
		t.Fatal("legacy typed nil native presence permitted an unrestricted fallback")
	}
}

func TestCommandScopeOpaqueWrappersRejectJSONTrustTransfer(t *testing.T) {
	scope, process, pipes := CommandScope{}, ScopedProcess{}, ScopedTemplatePipes{}
	for _, value := range []any{scope, &scope, process, &process, pipes, &pipes} {
		if encoded, err := json.Marshal(value); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("opaque native ownership was serialized: bytes=%s error=%v", encoded, err)
		}
	}
	for _, encoded := range []string{"{}", "null", "\"opaque\"", "{\"native\":true,\"pid\":1}"} {
		for _, target := range []any{new(CommandScope), new(ScopedProcess), new(ScopedTemplatePipes)} {
			if err := json.Unmarshal([]byte(encoded), target); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("JSON reconstructed opaque native ownership: input=%s target=%T error=%v", encoded, target, err)
			}
		}
	}
}

func TestCommandScopeConstructorRejectsDisabledAndMalformedConfiguration(t *testing.T) {
	if scope, err := NewCommandScope(Config{}, nil, nil); scope != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled configuration issued a scope: scope=%v error=%v", scope, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config, **ExecutableCapability, *[]*ExecutableCapability)
	}{
		{name: "zero commands", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.MaxCommands = 0 }},
		{name: "excess commands", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) {
			c.MaxCommands = MaxDomainCommands + 1
		}},
		{name: "insufficient tasks", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.MaxTasks = 0 }},
		{name: "excess tasks", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.MaxTasks = 4097 }},
		{name: "missing parent", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.Parent = nil }},
		{name: "missing workspace", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.Workspace = nil }},
		{name: "missing approvals", mutate: func(c *Config, _ **ExecutableCapability, _ *[]*ExecutableCapability) { c.Tools = nil }},
		{name: "missing launcher", mutate: func(_ *Config, launcher **ExecutableCapability, _ *[]*ExecutableCapability) { *launcher = nil }},
		{name: "tool count mismatch", mutate: func(_ *Config, _ **ExecutableCapability, tools *[]*ExecutableCapability) { *tools = nil }},
		{name: "nil tool", mutate: func(_ *Config, _ **ExecutableCapability, tools *[]*ExecutableCapability) { (*tools)[0] = nil }},
		{name: "excess tools", mutate: func(c *Config, _ **ExecutableCapability, tools *[]*ExecutableCapability) {
			c.Tools = make([]ApprovedExecutable, 17)
			*tools = make([]*ExecutableCapability, 17)
			for index := range *tools {
				(*tools)[index] = new(ExecutableCapability)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// These placeholders are deliberately unissued. Every mutation is
			// rejected by the common structural preflight before native work.
			config := Config{Enabled: true, Parent: new(os.File), Workspace: new(os.File),
				MaxCommands: 1, MaxTasks: 1, Tools: []ApprovedExecutable{{Path: "/unissued/tool"}}}
			launcher := new(ExecutableCapability)
			tools := []*ExecutableCapability{new(ExecutableCapability)}
			test.mutate(&config, &launcher, &tools)
			if scope, err := NewCommandScope(config, launcher, tools); scope != nil || !errors.Is(err, ErrUnsafe) {
				t.Fatalf("malformed configuration passed structural preflight: scope=%v error=%v", scope, err)
			}
		})
	}
}

func TestCommandScopeCallerFlagsAndUnissuedCapabilitiesCannotGrantNativeScope(t *testing.T) {
	parent, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	workspace, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	approval := ApprovedExecutable{Path: "/unissued/tool", SHA256: strings.Repeat("a", 64)}
	config := Config{Enabled: true, Parent: parent, Workspace: workspace, Launcher: approval,
		Tools: []ApprovedExecutable{approval}, UID: 65534, GID: 65534, MaxCommands: 1, MaxTasks: 1, Hardware: "none"}
	scope, err := NewCommandScope(config, new(ExecutableCapability), []*ExecutableCapability{new(ExecutableCapability)})
	if scope != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := scope.Drain(ctx); err != nil {
				t.Errorf("retain failed constructor cleanup ownership: %v", err)
			}
		})
	}
	if err == nil || !errors.Is(err, ErrUnsafe) && !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrRetained) {
		t.Fatalf("caller flags reconstructed native issuance: scope=%v error=%v", scope, err)
	}
	if scope != nil {
		if snapshot := scope.Snapshot(); !errors.Is(err, ErrRetained) || !snapshot.GateClosed || !snapshot.Quarantined {
			t.Fatalf("a failed constructor returned usable admission: snapshot=%+v error=%v", snapshot, err)
		}
	}
	for _, borrowed := range []*os.File{parent, workspace} {
		if _, err := borrowed.Stat(); err != nil {
			t.Fatalf("constructor rejection consumed a caller directory: %v", err)
		}
	}
}

func TestCommandScopeBookkeepingClosedGateRejectsBeforeNativeValidation(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	scope.CloseGate()
	template := commandScopeTestTemplate()
	if process, err := scope.Start(context.Background(), template); process != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed admission reached native validation: process=%v error=%v", process, err)
	}
	if pipes, err := scope.StartTemplatePipes(context.Background(), template, true, true); pipes != nil || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed pipe admission reached native validation: pipes=%v error=%v", pipes, err)
	}
	if err := scope.Run(context.Background(), template); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Run admission reached native validation: %v", err)
	}
	if template.Process != nil || template.ProcessState != nil || template.Stdout != nil || template.Stderr != nil {
		t.Fatal("rejected admission changed the unstarted command template")
	}
	if snapshot := scope.Snapshot(); snapshot.Active != 0 || snapshot.Quarantined || snapshot.Closed {
		t.Fatalf("closed admission mutated pure bookkeeping ownership: %+v", snapshot)
	}
}

func TestCommandScopeBookkeepingFullSlotsRejectBeforeNativeValidation(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	original := commandScopeTestBookkeepingEntry(scope, 0)
	template := commandScopeTestTemplate()
	if process, err := scope.Start(context.Background(), template); process != nil || !errors.Is(err, ErrCapacity) {
		t.Fatalf("a full fixed registry reached native validation: process=%v error=%v", process, err)
	}
	if pipes, err := scope.StartTemplatePipes(context.Background(), template, true, false); pipes != nil || !errors.Is(err, ErrCapacity) {
		t.Fatalf("full pipe admission reached native validation: pipes=%v error=%v", pipes, err)
	}
	if snapshot := scope.Snapshot(); snapshot.Active != 1 || snapshot.Capacity != 1 || snapshot.GateClosed || snapshot.Quarantined {
		t.Fatalf("full admission changed the original bookkeeping owner: %+v", snapshot)
	}
	if scope.slots[0] != original {
		t.Fatal("full admission replaced the registered owner")
	}
}

func TestCommandScopeBookkeepingFreeSlotIsBoundedAndDoesNotMutateRegistry(t *testing.T) {
	occupied := &scopedCommand{}
	for _, test := range []struct {
		name  string
		slots []*scopedCommand
		want  int
	}{
		{name: "nil", want: -1},
		{name: "empty", slots: []*scopedCommand{}, want: -1},
		{name: "first", slots: []*scopedCommand{nil, occupied}, want: 0},
		{name: "middle", slots: []*scopedCommand{occupied, nil, occupied}, want: 1},
		{name: "full", slots: []*scopedCommand{occupied, occupied}, want: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := append([]*scopedCommand(nil), test.slots...)
			length, capacity := len(test.slots), cap(test.slots)
			if index := freeCommandSlot(test.slots); index != test.want {
				t.Fatalf("free slot=%d want=%d", index, test.want)
			}
			if len(test.slots) != length || cap(test.slots) != capacity {
				t.Fatal("slot lookup resized its fixed registry")
			}
			for index := range before {
				if test.slots[index] != before[index] {
					t.Fatal("slot lookup changed an existing owner")
				}
			}
		})
	}
}

func TestCommandScopeBookkeepingQuarantineRetainsExactOriginalSlot(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(2)
	original := commandScopeTestBookkeepingEntry(scope, 0)
	peer := commandScopeTestBookkeepingEntry(scope, 1)
	template, originalContext, changed := original.template, original.context, scope.changed
	original.quarantine()
	original.quarantine()
	commandScopeTestAssertRetained(t, scope, original, 2)
	if scope.slots[1] != peer || original.template != template || original.context != originalContext || freeCommandSlot(scope.slots) != -1 {
		t.Fatal("quarantine dropped, replaced or freed registered bookkeeping ownership")
	}
	select {
	case <-changed:
	default:
		t.Fatal("quarantine did not notify existing observers")
	}
}

func TestCommandScopeBookkeepingDrainRequiresFiniteDeadline(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background()} {
		scope := commandScopeTestBookkeepingScope(1)
		want := ErrRetained
		if ctx == nil {
			want = ErrUnsafe
		}
		if err := scope.Drain(ctx); !errors.Is(err, want) {
			t.Fatalf("unbounded drain returned %v want %v", err, want)
		}
		if snapshot := scope.Snapshot(); snapshot.GateClosed || snapshot.Closed || snapshot.Quarantined {
			t.Fatalf("invalid observation claimed bookkeeping closure: %+v", snapshot)
		}
	}
}

func TestCommandScopeBookkeepingDrainDeadlineRetainsSlotsWithNilParentDone(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	original := commandScopeTestBookkeepingEntry(scope, 0)
	ctx := commandScopeTestCallbackContext{Context: context.Background(),
		deadline: func() (time.Time, bool) { return time.Now().Add(-time.Second), true }}
	observed := commandScopeTestObserve(t, func() error { return scope.Drain(ctx) })
	if !observed.returned || observed.panicValue != nil || !errors.Is(observed.err, context.DeadlineExceeded) || !errors.Is(observed.err, ErrRetained) {
		t.Fatalf("finite observer failed when the parent Done channel was nil: %+v", observed)
	}
	if snapshot := scope.Snapshot(); snapshot.Active != 1 || !snapshot.GateClosed || snapshot.Closed || snapshot.Quarantined {
		t.Fatalf("deadline returned unfinished bookkeeping ownership: %+v", snapshot)
	}
	if scope.slots[0] != original || original.retired || (&ScopedProcess{command: original}).RetirementComplete() {
		t.Fatal("deadline replaced actual retirement with an observation timeout")
	}
}

func TestCommandScopeBookkeepingDrainDoesNotWaitForAnotherDrainLock(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	commandScopeTestBookkeepingEntry(scope, 0)
	scope.drainMu.Lock()
	defer scope.drainMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	observed := commandScopeTestObserve(t, func() error { return scope.Drain(ctx) })
	if !observed.returned || !errors.Is(observed.err, ErrRetained) {
		t.Fatalf("a competing drain waited beyond its bounded ownership gate: %+v", observed)
	}
	if snapshot := scope.Snapshot(); snapshot.Active != 1 || snapshot.Closed {
		t.Fatalf("competing drain erased ownership: %+v", snapshot)
	}
}

func TestCommandScopeBookkeepingContextErrAbnormalExitRetainsRegisteredOwner(t *testing.T) {
	for _, name := range []string{"panic", "goexit"} {
		t.Run(name, func(t *testing.T) {
			scope := commandScopeTestBookkeepingScope(1)
			entry := commandScopeTestBookkeepingEntry(scope, 0)
			entry.context = commandScopeTestCallbackContext{Context: context.Background(), err: func() error {
				commandScopeTestExit(name)
				return nil
			}}
			observed := commandScopeTestObserve(t, func() error { return entry.start(false, false) })
			commandScopeTestAssertAbnormal(t, name, observed)
			commandScopeTestAssertRetained(t, scope, entry, 1)
			if entry.domain != nil || entry.process != nil || entry.pipes != nil {
				t.Fatal("context callback exit reached native construction or pipe allocation")
			}
		})
	}
}

func TestCommandScopeBookkeepingCancelAbnormalExitRetainsRegisteredOwner(t *testing.T) {
	for _, name := range []string{"panic", "goexit"} {
		t.Run(name, func(t *testing.T) {
			scope := commandScopeTestBookkeepingScope(1)
			entry := commandScopeTestBookkeepingEntry(scope, 0)
			entry.cancel = func() { commandScopeTestExit(name) }
			process := &ScopedProcess{command: entry}
			observed := commandScopeTestObserve(t, process.SignalCancel)
			commandScopeTestAssertAbnormal(t, name, observed)
			commandScopeTestAssertRetained(t, scope, entry, 1)
		})
	}
}

func TestCommandScopeBookkeepingDeadlineAbnormalExitFencesWithoutDroppingOwner(t *testing.T) {
	for _, name := range []string{"panic", "goexit"} {
		t.Run(name, func(t *testing.T) {
			scope := commandScopeTestBookkeepingScope(1)
			entry := commandScopeTestBookkeepingEntry(scope, 0)
			ctx := commandScopeTestCallbackContext{Context: context.Background(), deadline: func() (time.Time, bool) {
				commandScopeTestExit(name)
				return time.Time{}, false
			}}
			observed := commandScopeTestObserve(t, func() error { return scope.Drain(ctx) })
			commandScopeTestAssertAbnormal(t, name, observed)
			if snapshot := scope.Snapshot(); snapshot.Active != 1 || !snapshot.GateClosed || !snapshot.Quarantined || snapshot.Closed {
				t.Fatalf("abnormal deadline callback erased bookkeeping ownership: %+v", snapshot)
			}
			if scope.slots[0] != entry || (&ScopedProcess{command: entry}).RetirementComplete() || (&ScopedTemplatePipes{command: entry}).RetirementComplete() {
				t.Fatal("abnormal deadline callback claimed actual handle retirement")
			}
		})
	}
}

// These fixtures model only in-memory registry and callback bookkeeping. They
// issue no native capability, create no Domain, and never call Wait, Retire or
// release on a synthetic domain owner.
func commandScopeTestBookkeepingScope(capacity int) *CommandScope {
	return &CommandScope{commandScopeState: &commandScopeState{slots: make([]*scopedCommand, capacity), changed: make(chan struct{})}}
}

func commandScopeTestBookkeepingEntry(scope *CommandScope, slot int) *scopedCommand {
	entry := &scopedCommand{scope: scope.commandScopeState, slot: slot, context: context.Background(), template: commandScopeTestTemplate()}
	scope.slots[slot] = entry
	return entry
}

func commandScopeTestTemplate() *exec.Cmd {
	return &exec.Cmd{Path: "/unstarted/command-scope", Args: []string{"/unstarted/command-scope"}}
}

func commandScopeTestAssertRetained(t *testing.T, scope *CommandScope, entry *scopedCommand, active int) {
	t.Helper()
	if snapshot := scope.Snapshot(); snapshot.Active != active || !snapshot.GateClosed || !snapshot.Quarantined || snapshot.Closed {
		t.Fatalf("abnormal callback lost bounded bookkeeping ownership: %+v", snapshot)
	}
	if scope.slots[entry.slot] != entry || !entry.unknown || entry.retired ||
		(&ScopedProcess{command: entry}).RetirementComplete() || (&ScopedTemplatePipes{command: entry}).RetirementComplete() {
		t.Fatal("abnormal callback dropped its exact slot or claimed actual retirement")
	}
}

type commandScopeTestCallbackContext struct {
	context.Context
	err      func() error
	deadline func() (time.Time, bool)
}

func (ctx commandScopeTestCallbackContext) Err() error {
	if ctx.err != nil {
		return ctx.err()
	}
	return ctx.Context.Err()
}

func (ctx commandScopeTestCallbackContext) Deadline() (time.Time, bool) {
	if ctx.deadline != nil {
		return ctx.deadline()
	}
	return ctx.Context.Deadline()
}

type commandScopeTestObservation struct {
	err        error
	panicValue any
	returned   bool
}

func commandScopeTestObserve(t *testing.T, call func() error) commandScopeTestObservation {
	t.Helper()
	finished := make(chan commandScopeTestObservation, 1)
	go func() {
		observed := commandScopeTestObservation{}
		defer func() { observed.panicValue = recover(); finished <- observed }()
		observed.err = call()
		observed.returned = true
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case observed := <-finished:
		return observed
	case <-timer.C:
		t.Fatal("source-level ownership callback did not reach its bounded observation")
		return commandScopeTestObservation{}
	}
}

func commandScopeTestExit(name string) {
	if name == "panic" {
		panic("command scope callback panic")
	}
	runtime.Goexit()
}

func commandScopeTestAssertAbnormal(t *testing.T, name string, observed commandScopeTestObservation) {
	t.Helper()
	if observed.returned {
		t.Fatalf("an abnormal %s callback returned as completed ownership: %+v", name, observed)
	}
	if name == "panic" && observed.panicValue != "command scope callback panic" || name == "goexit" && observed.panicValue != nil {
		t.Fatalf("the callback exit mode changed: mode=%s observation=%+v", name, observed)
	}
}
