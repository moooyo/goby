package commanddomain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecutableUseZeroAndNilRejectDescriptorAndClose(t *testing.T) {
	for _, use := range []*ExecutableUse{nil, {}} {
		descriptor, err := use.Descriptor()
		if descriptor != nil || err == nil {
			// A Descriptor is borrowed from the use, so this test must not close
			// or transfer even an unexpectedly returned descriptor.
			t.Fatalf("an unissued executable use exposed a descriptor: file=%v error=%v", descriptor, err)
		}
		if err := use.Close(); err == nil {
			t.Fatal("an unissued executable use claimed actual ownership closure")
		}
	}
}

func TestExecutableUseRejectsValueAndPointerJSONTrustTransfer(t *testing.T) {
	var use ExecutableUse
	for _, value := range []any{use, &use} {
		if encoded, err := json.Marshal(value); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("opaque executable use was serialized: bytes=%s error=%v", encoded, err)
		}
	}
	for _, encoded := range []string{"{}", "null", "\"opaque\"", "{\"path\":\"/approved/tool\",\"descriptor\":3}"} {
		var decoded ExecutableUse
		if err := json.Unmarshal([]byte(encoded), &decoded); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("JSON reconstructed executable-use authority: input=%s error=%v", encoded, err)
		}
		if descriptor, err := decoded.Descriptor(); descriptor != nil || err == nil {
			t.Fatalf("rejected JSON left a usable borrowed descriptor: file=%v error=%v", descriptor, err)
		}
	}
}

func TestExecutableUseCannotBeBorrowedFromNilOrZeroScope(t *testing.T) {
	for _, scope := range []*CommandScope{nil, {}} {
		use, err := scope.BorrowApprovedExecutable("/approved/tool", strings.Repeat("a", 64))
		if use != nil || err == nil {
			t.Fatalf("an unissued scope borrowed executable authority: use=%v error=%v", use, err)
		}
	}
}

func TestExecutableUseTypedNilAndUnknownClassCannotSpawn(t *testing.T) {
	for _, test := range []struct {
		name  string
		use   *ExecutableUse
		class LimitsClass
	}{
		{name: "typed nil analysis use", class: LimitsAnalysis},
		{name: "typed nil default use", class: LimitsDefault},
		{name: "unknown class", use: new(ExecutableUse), class: LimitsClass(255)},
	} {
		t.Run(test.name, func(t *testing.T) {
			// This state models rejection only. It has no issued capability,
			// cgroup, native configuration, executable use or process owner.
			scope := commandScopeTestBookkeepingScope(1)
			template := &exec.Cmd{Path: "/approved/tool", Args: []string{"/approved/tool"}}
			pipes, err := scope.StartTemplatePipesWithExecutable(context.Background(), template, test.use, test.class, true, false)
			if pipes != nil || err == nil {
				t.Fatalf("invalid executable-use policy reached process ownership: pipes=%v error=%v", pipes, err)
			}
			if template.Process != nil || template.ProcessState != nil || template.Stdout != nil || template.Stderr != nil || len(template.ExtraFiles) != 0 {
				t.Fatal("rejected executable-use policy changed the original command template")
			}
			if snapshot := scope.Snapshot(); snapshot.Active != 0 {
				t.Fatalf("rejected executable use occupied a native command slot: %+v", snapshot)
			}
		})
	}
}

func TestExecutableUseInvalidStreamRequestDoesNotAllocateTemplatePipes(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	template := &exec.Cmd{Path: "/approved/tool", Args: []string{"/approved/tool"}, ExtraFiles: []*os.File{nil}}
	pipes, err := scope.StartTemplatePipesWithExecutable(context.Background(), template, nil, LimitsAnalysis, false, false)
	if pipes != nil || err == nil {
		t.Fatalf("a request without output consumers created pipe ownership: pipes=%v error=%v", pipes, err)
	}
	if template.Stdout != nil || template.Stderr != nil || scope.Snapshot().Active != 0 {
		t.Fatal("invalid output selection allocated pipes or a command slot")
	}
}

func TestExecutableUseWrongScopeCannotRegisterOrSpawn(t *testing.T) {
	owner := commandScopeTestBookkeepingScope(1)
	target := commandScopeTestBookkeepingScope(1)
	owner.uses, target.uses = make([]*ExecutableUse, 1), make([]*ExecutableUse, 1)
	use := &ExecutableUse{executableUseState: &executableUseState{scope: owner.commandScopeState,
		slot: 0, capSlot: -1, approval: ApprovedExecutable{Path: "/approved/tool"}, commands: make([]*scopedCommand, 1)}}
	owner.uses[0] = use
	template := &exec.Cmd{Path: "/approved/tool", Args: []string{"/approved/tool"}}
	pipes, err := target.StartTemplatePipesWithExecutable(context.Background(), template, use, LimitsAnalysis, true, false)
	if pipes != nil || err == nil {
		t.Fatalf("foreign use reconstructed executable ownership: pipes=%v error=%v", pipes, err)
	}
	if owner.uses[0] != use || target.uses[0] != nil || use.commands[0] != nil ||
		owner.Snapshot().Active != 0 || target.Snapshot().Active != 0 || template.Process != nil || template.Stdout != nil {
		t.Fatal("foreign-use rejection transferred a registry entry or command ownership")
	}
}

func TestExecutableUseBookkeepingFreeSlotLookupIsBoundedAndPure(t *testing.T) {
	occupied := &ExecutableUse{}
	for _, test := range []struct {
		name string
		uses []*ExecutableUse
		want int
	}{
		{name: "nil", want: -1},
		{name: "empty", uses: []*ExecutableUse{}, want: -1},
		{name: "first", uses: []*ExecutableUse{nil, occupied}, want: 0},
		{name: "middle", uses: []*ExecutableUse{occupied, nil, occupied}, want: 1},
		{name: "full", uses: []*ExecutableUse{occupied, occupied}, want: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := append([]*ExecutableUse(nil), test.uses...)
			length, capacity := len(test.uses), cap(test.uses)
			if slot := freeExecutableUseSlot(test.uses); slot != test.want {
				t.Fatalf("fixed use slot=%d want=%d", slot, test.want)
			}
			if len(test.uses) != length || cap(test.uses) != capacity {
				t.Fatal("use-slot lookup resized its registry")
			}
			for index := range before {
				if test.uses[index] != before[index] {
					t.Fatal("use-slot lookup changed an existing owner")
				}
			}
		})
	}
}

func TestExecutableUseBookkeepingFullRegistryRejectsBeforeNativeIssuance(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	occupied := &ExecutableUse{}
	scope.uses = []*ExecutableUse{occupied}
	use, err := scope.BorrowApprovedExecutable("/unissued/tool", strings.Repeat("a", 64))
	if use != nil || !errors.Is(err, ErrCapacity) || scope.uses[0] != occupied {
		t.Fatalf("full use registry reached native issuance or replaced its owner: use=%v error=%v", use, err)
	}
	if snapshot := scope.Snapshot(); snapshot.Active != 0 || snapshot.GateClosed || snapshot.Quarantined {
		t.Fatalf("bounded-use rejection changed command admission: %+v", snapshot)
	}
}

func TestExecutableUseBookkeepingActiveAndUnknownCloseRetainsExactReferences(t *testing.T) {
	for _, name := range []string{"active command", "initializing", "unknown"} {
		t.Run(name, func(t *testing.T) {
			// These are registry tokens only: no native capability or Domain
			// exists, and no synthetic command is released or retired here.
			scope := commandScopeTestBookkeepingScope(1)
			scope.uses = make([]*ExecutableUse, 1)
			capability := &ExecutableCapability{}
			use := &ExecutableUse{executableUseState: &executableUseState{scope: scope.commandScopeState,
				capability: capability, slot: 0, capSlot: -1, commands: make([]*scopedCommand, 1)}}
			scope.uses[0] = use
			var command *scopedCommand
			switch name {
			case "active command":
				command = &scopedCommand{scope: scope.commandScopeState, slot: 0, executable: use}
				scope.slots[0], use.commands[0] = command, command
			case "initializing":
				use.initializing = true
			case "unknown":
				use.quarantine()
			}
			if err := use.Close(); !errors.Is(err, ErrRetained) {
				t.Fatalf("unresolved use ownership was closed: case=%s error=%v", name, err)
			}
			if scope.uses[0] != use || use.capability != capability || use.closed || use.commands[0] != command || scope.slots[0] != command {
				t.Fatal("failed use Close dropped its exact registry, capability or command references")
			}
			if name == "unknown" {
				if snapshot := scope.Snapshot(); !snapshot.GateClosed || !snapshot.Quarantined || !use.unknown {
					t.Fatalf("unknown use ownership failed to fence admission: %+v", snapshot)
				}
			}
		})
	}
}

func TestExecutableUseMetadataAndBorrowingRejectNilOrUnissuedFD(t *testing.T) {
	for _, scope := range []*CommandScope{nil, {}} {
		if file, err := scope.DuplicateApprovedExecutable("/approved/tool", ""); file != nil || err == nil {
			if file != nil {
				_ = file.Close()
			}
			t.Fatalf("unissued scope duplicated catalog metadata: file=%v error=%v", file, err)
		}
		if use, err := scope.BorrowApprovedExecutableFromFD(nil); use != nil || !errors.Is(err, ErrUnsafe) {
			t.Fatalf("nil metadata FD reconstructed executable use: use=%v error=%v", use, err)
		}
	}
}

func TestMetadataDuplicateBookkeepingCapacityRejectsBeforeDescriptorWork(t *testing.T) {
	scope := commandScopeTestBookkeepingScope(1)
	owner := &scopeMetadataDuplicate{slot: 0}
	scope.metadata = []*scopeMetadataDuplicate{owner}
	if file, err := scope.DuplicateApprovedExecutable("/unissued/tool", ""); file != nil || !errors.Is(err, ErrCapacity) {
		t.Fatalf("a full fixed metadata registry reached descriptor work: file=%v error=%v", file, err)
	}
	if scope.metadata[0] != owner || owner.file != nil || scope.Snapshot().Quarantined {
		t.Fatal("capacity rejection changed metadata custody")
	}
}

func TestMetadataDuplicateBookkeepingSuccessfulHandoffIsCallerOwned(t *testing.T) {
	scope, owner, file := executableUseTestMetadataOwner(t)
	result, err := scope.finishMetadataDuplicate(owner, file, nil)
	if err != nil || result != file || scope.metadata[0] != nil || owner.file != nil {
		t.Fatalf("successful metadata handoff retained or replaced caller ownership: file=%v error=%v", result, err)
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("successful metadata handoff closed the caller-owned descriptor")
	}
	if scope.Snapshot().Active != 0 || scope.Snapshot().Quarantined {
		t.Fatal("metadata custody minted command or native authority")
	}
}

func TestMetadataDuplicateBookkeepingFailureStronglyRetainsActualFD(t *testing.T) {
	scope, owner, file := executableUseTestMetadataOwner(t)
	result, err := scope.finishMetadataDuplicate(owner, nil, ErrRetained)
	if result != nil || !errors.Is(err, ErrRetained) || scope.metadata[0] != owner || owner.file != file || !owner.unknown {
		t.Fatal("failed metadata duplication dropped the exact FD or returned it to an ignoring caller")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("retaining a failed duplicate closed its actual descriptor")
	}
	if snapshot := scope.Snapshot(); !snapshot.GateClosed || !snapshot.Quarantined || snapshot.Closed || snapshot.Active != 0 {
		t.Fatalf("failed metadata custody did not fence the pure registry: %+v", snapshot)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := scope.Drain(ctx); !errors.Is(err, ErrRetained) {
		t.Fatalf("a retained metadata FD allowed scope drain: %v", err)
	}
	if scope.metadata[0] != owner || owner.file != file {
		t.Fatal("observation timeout erased metadata ownership")
	}
}

func TestMetadataDuplicateBookkeepingLateGateCloseKeepsFailedCloseOwner(t *testing.T) {
	for _, failed := range []bool{false, true} {
		scope, owner, file := executableUseTestMetadataOwner(t)
		scope.CloseGate()
		if failed {
			// Closing before handoff models an actual unexpected Close failure.
			// This is a real FD ownership negative, never native authorization.
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		result, err := scope.finishMetadataDuplicate(owner, file, nil)
		if result != nil {
			t.Fatal("a late closed scope handed a descriptor to its caller")
		}
		if failed {
			if !errors.Is(err, ErrRetained) || scope.metadata[0] != owner || owner.file != file || !owner.unknown || !scope.Snapshot().Quarantined {
				t.Fatal("an actual failed Close lost its exact metadata owner")
			}
		} else {
			if !errors.Is(err, ErrClosed) || scope.metadata[0] != nil || owner.file != nil {
				t.Fatal("known successful late Close did not return its metadata operation slot")
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("late metadata rejection did not actually close its FD")
			}
		}
	}
}

// These fixtures exercise real ordinary descriptor custody only. They create
// no native executable capability, Domain or executable-use authorization.
func executableUseTestMetadataOwner(t *testing.T) (*CommandScope, *scopeMetadataDuplicate, *os.File) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "metadata-custody-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	scope := commandScopeTestBookkeepingScope(1)
	owner := &scopeMetadataDuplicate{slot: 0, file: file}
	scope.metadata = []*scopeMetadataDuplicate{owner}
	return scope, owner, file
}
