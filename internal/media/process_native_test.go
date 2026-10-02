package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

func TestMediaNativeBindingFailsClosedBeforeSpawn(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{"explicit nil scope", commanddomain.WithCommandScope(context.Background(), nil)},
		{"zero scope", commanddomain.WithCommandScope(context.Background(), &commanddomain.CommandScope{})},
		{"legacy typed nil domain", commanddomain.WithDomain(context.Background(), nil)},
		{"legacy zero domain", commanddomain.WithDomain(context.Background(), &commanddomain.Domain{})},
	} {
		t.Run(test.name, func(t *testing.T) {
			governor := newMediaProcessAdmission(1, 1, 2)
			command := exec.Command("/must-never-spawn")
			process, err := startMediaProcessWithAdmission(test.ctx, command, governor, false)
			if process != nil || err == nil || command.Process != nil || command.ProcessState != nil {
				t.Fatalf("required native binding fell back: process=%v error=%v", process, err)
			}
			stats := processCapacityStatsFor(governor, &atomic.Uint64{})
			if stats.Active != 0 || stats.Background != 0 || stats.Queued != 0 {
				t.Fatalf("known native prestart rejection leaked admission: %+v", stats)
			}
			called := false
			err = runProcessWithRetirement(test.ctx, command, func() error { called = true; return nil }, governor, &atomic.Uint64{})
			if err == nil || called || command.Process != nil {
				t.Fatal("native rejection invoked an unrestricted retirement/spawn path")
			}
		})
	}
}

func TestMediaNativeStreamRejectsMissingOwnerWithoutParsing(t *testing.T) {
	ctx := commanddomain.WithCommandScope(context.Background(), nil)
	called := false
	parseErr, waitErr, startErr := runMediaStdout(ctx, exec.Command("/must-never-spawn"), func(io.Reader) error { called = true; return nil })
	if parseErr != nil || waitErr != nil || !errors.Is(startErr, commanddomain.ErrUnavailable) || called {
		t.Fatalf("native stream missing-owner boundary: parse=%v wait=%v start=%v", parseErr, waitErr, startErr)
	}
}

func TestMediaNativeUnknownOwnerRetainsOriginalChargeAndReference(t *testing.T) {
	// This is an admission/quarantine contract, not a fake native Domain or a
	// passing operating-system retirement. No process or storage is constructed.
	governor := newMediaProcessAdmission(1, 1, 2)
	release, err := governor.acquire(WithBackgroundProcess(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	var counter atomic.Uint64
	owner, err := newNativeMediaOwner(&commanddomain.CommandScope{}, governor, release, &counter)
	if err != nil {
		release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Only the isolated test's synthetic charge is discarded. Production
		// quarantine has no such restart or operating-system release operation.
		nativeMediaOwners.mu.Lock()
		if nativeMediaOwners.entries[owner.slot] == owner {
			nativeMediaOwners.entries[owner.slot] = nil
		}
		nativeMediaOwners.mu.Unlock()
		release()
	})
	owner.markUnknown()
	owner.markUnknown()
	owner.returnKnownCapacity()
	stats := processCapacityStatsFor(governor, &counter)
	if stats.Active != 1 || stats.Background != 1 || stats.RetirementUnknown != 1 {
		t.Fatalf("unknown native admission was returned: %+v", stats)
	}
	nativeMediaOwners.mu.Lock()
	held := nativeMediaOwners.entries[owner.slot] == owner
	nativeMediaOwners.mu.Unlock()
	if !held {
		t.Fatal("unknown exact owner was discarded")
	}
}

func TestMediaStdoutLegacyParserPreservesActualOutputAndJoin(t *testing.T) {
	t.Setenv("GOBY_MEDIA_HELPER_PROCESS", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestMediaProcessHelper$", "--", "echo", "legacy tail sentinel")
	var output string
	parseErr, waitErr, startErr := runMediaStdout(ctx, command, func(reader io.Reader) error {
		data, err := io.ReadAll(reader)
		output = string(data)
		return err
	})
	if parseErr != nil || waitErr != nil || startErr != nil || output != "legacy tail sentinel" || command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("legacy stream output/join changed: output=%q parse=%v wait=%v start=%v", output, parseErr, waitErr, startErr)
	}
}

func TestMediaNativeRawTemplatePipeRequiresOwnedAdapter(t *testing.T) {
	command := exec.Command("/must-never-spawn")
	reader, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if writer, ok := command.Stdout.(*os.File); ok {
		defer writer.Close()
	}
	if !nativeRawPipeTemplate(command) {
		t.Fatal("raw copied template writer lost its original reader ownership")
	}
	plain := exec.Command("/must-never-spawn")
	plain.Stdout = io.Discard
	plain.Stderr = &strings.Builder{}
	if nativeRawPipeTemplate(plain) {
		t.Fatal("ordinary joined os/exec writer was rejected as an unowned pipe")
	}
}
