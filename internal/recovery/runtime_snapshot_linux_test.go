//go:build linux

package recovery

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/lifecycle"
	"golang.org/x/sys/unix"
)

func TestRuntimeActiveConfigVerifiesRetainedHistoryOnce(t *testing.T) {
	ctx := context.Background()
	cfg := testDeployment(t)
	runtime, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	encoded, err := config.EncodeBackupDefaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	historicalID, currentID := strings.Repeat("1", 32), strings.Repeat("2", 32)
	historical, err := runtime.lifecycle.StageGeneration(ctx, historicalID, encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := runtime.lifecycle.StageGeneration(ctx, currentID, encoded, bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.lifecycle.Current()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := runtime.lifecycle.Plan(ctx, before, lifecycle.Candidate{GenerationID: currentID, DatabaseSlot: lifecycle.DatabaseRecovery, Master: lifecycle.MasterGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.lifecycle.Activate(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watch)
	historyPath := filepath.Join(cfg.Recovery.Directory, "generation-"+historicalID, historical.Config.Name)
	if _, err := unix.InotifyAddWatch(watch, historyPath, unix.IN_OPEN|unix.IN_CLOSE_NOWRITE); err != nil {
		t.Fatal(err)
	}
	active, state, err := runtime.ActiveConfig(ctx)
	if err != nil || state != plan.After || active.DatabaseURL != cfg.Recovery.DatabaseURL || active.APIKeyMasterKeyFile != filepath.Join(cfg.Recovery.Directory, "generation-"+currentID, current.Master.Name) {
		t.Fatalf("startup did not resolve the activated snapshot: %v", err)
	}
	// Including close events prevents repeated opens from being coalesced.
	var buffer [4096]byte
	opens := 0
	for {
		n, err := unix.Read(watch, buffer[:])
		if errors.Is(err, unix.EAGAIN) {
			break
		}
		if err != nil || n == 0 {
			t.Fatalf("read history access events: %v", err)
		}
		for offset := 0; offset < n; {
			if n-offset < unix.SizeofInotifyEvent {
				t.Fatal("truncated history access event")
			}
			event := buffer[offset:n]
			mask := binary.NativeEndian.Uint32(event[4:8])
			length := int(binary.NativeEndian.Uint32(event[12:16]))
			if mask&unix.IN_Q_OVERFLOW != 0 || unix.SizeofInotifyEvent+length > len(event) {
				t.Fatal("incomplete history access evidence")
			}
			if mask&unix.IN_OPEN != 0 {
				opens++
			}
			offset += unix.SizeofInotifyEvent + length
		}
	}
	if opens != 1 {
		t.Fatalf("historical configuration was opened %d times, want 1", opens)
	}
	if err := os.WriteFile(historyPath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.ActiveConfig(ctx); err == nil {
		t.Fatal("startup accepted damaged unselected history")
	}
}
