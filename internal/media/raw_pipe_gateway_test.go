//go:build linux

package media

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

const rawPipeGatewayHelperSelector = "goby-media-raw-pipe-v1"

// This helper emits fixed parser transport fixtures through actual pipes. It
// is not an approved native executable or a passing media preservation proof.
func TestMediaRawPipeGatewayHelper(t *testing.T) {
	for index, argument := range os.Args {
		if argument != "--" || index+2 >= len(os.Args) || os.Args[index+1] != rawPipeGatewayHelperSelector {
			continue
		}
		mode := os.Args[index+2]
		switch mode {
		case "framehash", "framehash-error", "framehash-nonzero", "framehash-stderr-budget":
			fmt.Print(rawPipeGatewayFrameHash())
			if mode == "framehash-error" {
				fmt.Fprintln(os.Stderr, "[error] decoder fixture failure")
			}
			if mode == "framehash-stderr-budget" {
				fmt.Fprint(os.Stderr, strings.Repeat("x", maxProcessStderr+1))
			}
			if mode == "framehash-nonzero" {
				os.Exit(7)
			}
		case "packets", "packets-trailing", "packets-nonzero", "packets-stderr", "packets-stderr-budget":
			fmt.Print(rawPipeGatewayPackets())
			if mode == "packets-trailing" {
				fmt.Print("unexpected trailing sentinel")
			}
			if mode == "packets-stderr" {
				fmt.Fprintln(os.Stderr, "packet fixture diagnostics")
			}
			if mode == "packets-stderr-budget" {
				fmt.Fprint(os.Stderr, strings.Repeat("x", maxProcessStderr+1))
			}
			if mode == "packets-nonzero" {
				os.Exit(7)
			}
		default:
			os.Exit(8)
		}
		os.Exit(0)
	}
}

func rawPipeGatewayFrameHash() string {
	return videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000") + videoSeekHashTestPoint(0) + videoSeekHashTestPoint(1000)
}

func rawPipeGatewayPackets() string {
	return mediaEditPacketsDocument(mediaEditPacketFixture(0, 0, -1, 40, 120, 'a'), mediaEditPacketFixture(0, 40, 39, 40, 121, 'd'))
}

func rawPipeGatewayInput(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "readonly-input")
	if err := os.WriteFile(path, []byte("unchanged raw pipe source sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func rawPipeGatewayCapacityBefore(t *testing.T) ProcessCapacitySnapshot {
	t.Helper()
	stats := GetProcessCapacityStats()
	if stats.Active != 0 || stats.Background != 0 || stats.Queued != 0 {
		t.Fatalf("isolated raw pipe case began with active admission: %+v", stats)
	}
	return stats
}

func rawPipeGatewayAssertCapacityReturned(t *testing.T, before ProcessCapacitySnapshot) {
	t.Helper()
	after := GetProcessCapacityStats()
	if after.Active != 0 || after.Background != 0 || after.Queued != 0 || after.RetirementUnknown != before.RetirementUnknown {
		t.Fatalf("actual raw pipe process/copier join did not return original admission: before=%+v after=%+v", before, after)
	}
}

func rawPipeGatewayExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func rawPipeGatewayFrameHashRun(t *testing.T, mode string, budget int) (VideoSeekIndex, error) {
	t.Helper()
	before := rawPipeGatewayCapacityBefore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{"-test.run=^TestMediaRawPipeGatewayHelper$", "--", rawPipeGatewayHelperSelector, mode}
	index, err := runVideoSeekFrameHash(ctx, rawPipeGatewayExecutable(t), rawPipeGatewayInput(t), args, videoSeekHashTestBase(), MaxVideoSeekEntries, budget)
	rawPipeGatewayAssertCapacityReturned(t, before)
	return index, err
}

func TestRawPipeGatewayVideoSeekPreservesCompleteEvidenceAndActualJoin(t *testing.T) {
	index, err := rawPipeGatewayFrameHashRun(t, "framehash", len(rawPipeGatewayFrameHash()))
	if err != nil || len(index.Entries) != 2 || index.Entries[1].PTS != 1000 || index.Entries[1].DecodedSHA256 != strings.Repeat("d", 64) ||
		!index.PacketSideDataChecked || !index.NALScopeChecked {
		t.Fatalf("the real pipe lost complete framehash tail evidence: index=%+v error=%v", index, err)
	}
}

func TestRawPipeGatewayVideoSeekKeepsBudgetAndDecoderErrorPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		budget     int
		expected   error
		detail     string
	}{
		{"stdout budget", "framehash", 8, ErrOutputLimit, ""},
		{"stderr budget", "framehash-stderr-budget", 1 << 20, ErrOutputLimit, ""},
		{"decoder severity", "framehash-error", 1 << 20, nil, "decoder reported an error"},
		{"nonzero child", "framehash-nonzero", 1 << 20, nil, "execute video seek analysis"},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, err := rawPipeGatewayFrameHashRun(t, test.mode, test.budget)
			if err == nil || len(index.Entries) != 0 || test.expected != nil && !errors.Is(err, test.expected) ||
				test.detail != "" && !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("raw framehash error classification changed: index=%+v error=%v", index, err)
			}
		})
	}
}

func rawPipeGatewayPacketTool(t *testing.T, mode string) string {
	t.Helper()
	// The real prlimit invocation retains all original arguments and inherited
	// source FD. This ordinary test shim selects one transport helper; it does
	// not impersonate a native launcher, fixed volume or approved tool capability.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	body := "#!/bin/sh\nexec " + quote(rawPipeGatewayExecutable(t)) + " '-test.run=^TestMediaRawPipeGatewayHelper$' -- " + quote(rawPipeGatewayHelperSelector) + " " + quote(mode) + " \"$@\"\n"
	path := filepath.Join(t.TempDir(), "packet-transport-helper")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func rawPipeGatewayPacketRun(t *testing.T, mode string) (map[int]mediaEditPacketDigest, error) {
	t.Helper()
	before := rawPipeGatewayCapacityBefore(t)
	ctx, cancel := context.WithTimeout(WithBackgroundProcess(context.Background()), 5*time.Second)
	defer cancel()
	packets, err := probeMediaEditPackets(ctx, rawPipeGatewayPacketTool(t, mode), rawPipeGatewayInput(t), map[int]*big.Rat{0: big.NewRat(1, 1000)})
	rawPipeGatewayAssertCapacityReturned(t, before)
	return packets, err
}

func TestRawPipeGatewayPacketProbePreservesTailAndExactDigests(t *testing.T) {
	packets, err := rawPipeGatewayPacketRun(t, "packets")
	expected, parseErr := parseMediaEditPackets(strings.NewReader(rawPipeGatewayPackets()), map[int]*big.Rat{0: big.NewRat(1, 1000)}, mediaEditMaxPackets)
	if err != nil || parseErr != nil || !reflect.DeepEqual(packets, expected) || packets[0].Packets != 2 || packets[0].MaxPacketBytes != 121 {
		t.Fatalf("the real packet pipe lost its tail or exact clocks: packets=%+v error=%v", packets, err)
	}
}

func TestRawPipeGatewayPacketProbeKeepsTrailingDiagnosticsAndBudgetFailures(t *testing.T) {
	for _, test := range []struct {
		mode     string
		expected error
		detail   string
	}{
		{"packets-trailing", ErrSubtitleRemovalUnsupported, ""},
		{"packets-stderr", ErrSubtitleRemovalUnsupported, "clean diagnostics"},
		{"packets-stderr-budget", ErrSubtitleRemovalBudget, ""},
		{"packets-nonzero", nil, "packet probe failed"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			packets, err := rawPipeGatewayPacketRun(t, test.mode)
			if err == nil || packets != nil || test.expected != nil && !errors.Is(err, test.expected) ||
				test.detail != "" && !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("raw packet probe error classification changed: packets=%+v error=%v", packets, err)
			}
		})
	}
}

func TestRawPipeGatewayRequiredMissingScopeRejectsBeforeParserOrSpawn(t *testing.T) {
	before := rawPipeGatewayCapacityBefore(t)
	ctx, cancel := context.WithTimeout(commanddomain.WithCommandScope(context.Background(), nil), 5*time.Second)
	defer cancel()
	file := rawPipeGatewayInput(t)
	index, err := runVideoSeekFrameHash(ctx, "/must-never-spawn", file, nil, videoSeekHashTestBase(), MaxVideoSeekEntries, 1<<20)
	if !errors.Is(err, commanddomain.ErrUnavailable) || len(index.Entries) != 0 {
		t.Fatalf("video native missing-owner boundary changed: %v", err)
	}
	packets, err := probeMediaEditPackets(ctx, "/must-never-spawn", file, map[int]*big.Rat{0: big.NewRat(1, 1000)})
	if !errors.Is(err, commanddomain.ErrUnavailable) || packets != nil {
		t.Fatalf("packet native missing-owner boundary changed: %v", err)
	}
	rawPipeGatewayAssertCapacityReturned(t, before)
}
