//go:build linux

package recovery

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/lifecycle"
)

func TestRuntimeResolvesAndRetainsPublishedGeneration(t *testing.T) {
	ctx := context.Background()
	cfg := testDeployment(t)
	runtime, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	active, before, err := runtime.ActiveConfig(ctx)
	if err != nil || !reflect.DeepEqual(active, cfg) || before.Revision != 0 {
		t.Fatalf("initial deployment resolution failed: %v", err)
	}
	source := cfg
	source.ServerName = "Restored deployment defaults"
	source.Transcoding.MaxWidth = 1280
	encoded, err := config.EncodeBackupDefaults(source)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 32)
	master := bytes.Repeat([]byte{0x76}, 32)
	generation, err := runtime.lifecycle.StageGeneration(ctx, id, encoded, master)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := runtime.lifecycle.Plan(ctx, before, lifecycle.Candidate{
		GenerationID: id, DatabaseSlot: lifecycle.DatabaseRecovery, Master: lifecycle.MasterGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.lifecycle.Activate(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	// An activated, not-yet-accepted plan still selects the new slot. Startup
	// cannot silently fall back to the old database after an interruption.
	active, state, err := runtime.ActiveConfig(ctx)
	if err != nil || state.GenerationID != id || state.DatabaseSlot != lifecycle.DatabaseRecovery || state.Revision != 1 {
		t.Fatalf("activated generation did not resolve: %v", err)
	}
	want := cfg
	want.DatabaseURL, want.Recovery.DatabaseURL = cfg.Recovery.DatabaseURL, cfg.DatabaseURL
	want.ServerName, want.Transcoding.MaxWidth = source.ServerName, source.Transcoding.MaxWidth
	want.APIKeyMasterKeyFile = filepath.Join(cfg.Recovery.Directory, "generation-"+id, generation.Master.Name)
	if !reflect.DeepEqual(active, want) {
		t.Fatal("generation altered target operational configuration or used the wrong database slot")
	}
	actualMaster, err := os.ReadFile(active.APIKeyMasterKeyFile)
	if err != nil || !bytes.Equal(actualMaster, master) {
		t.Fatalf("generation selected a different protected master: %v", err)
	}
	clear(actualMaster)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resolved, reopenedState, err := reopened.ActiveConfig(ctx)
	if err != nil || reopenedState != state || !reflect.DeepEqual(resolved, want) {
		t.Fatalf("restart lost the active generation: %v", err)
	}
	if err := reopened.lifecycle.Finish(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(cfg.Recovery.Directory, "generation-"+id, generation.Config.Name)
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.ActiveConfig(ctx); err == nil {
		t.Fatal("a missing active configuration fell back to the original database")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if invalid, err := Open(ctx, cfg); err == nil {
		invalid.Close()
		t.Fatal("startup accepted a missing active generation file")
	}
}

func TestRuntimeRejectsLostRecoverySlotAndCancelledResolution(t *testing.T) {
	ctx := context.Background()
	cfg := testDeployment(t)
	runtime, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := runtime.ActiveConfig(cancelled); err == nil {
		t.Fatal("cancelled resolution succeeded")
	}
	_, before, err := runtime.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := config.EncodeBackupDefaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("e", 32)
	if _, err := runtime.lifecycle.StageGeneration(ctx, id, encoded, bytes.Repeat([]byte{2}, 32)); err != nil {
		t.Fatal(err)
	}
	plan, err := runtime.lifecycle.Plan(ctx, before, lifecycle.Candidate{GenerationID: id, DatabaseSlot: lifecycle.DatabaseRecovery, Master: lifecycle.MasterGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.lifecycle.Activate(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Recovery.DatabaseURL = ""
	reopened, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, _, err := reopened.ActiveConfig(ctx); err == nil {
		t.Fatal("missing configured recovery URL selected the primary database")
	}
}
