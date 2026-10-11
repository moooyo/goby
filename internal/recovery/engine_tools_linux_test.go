//go:build linux

package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
)

func recoveryToolFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "owned-recovery-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEngineSeparatesCreationAndDecoderAdmission(t *testing.T) {
	cfg := testDeployment(t)
	cfg.Recovery.PGDumpPath = filepath.Join(t.TempDir(), "missing-pg-dump")
	cfg.Recovery.PGRestorePath = recoveryToolFixture(t, "exit 97")
	objects, err := backupstore.Open(cfg.Recovery.Backups)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Close()
	// Constructors only resolve paths; no pool methods or subprocesses run.
	pool := &pgxpool.Pool{}
	vault := identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile)
	engine, err := NewEngine(cfg, pool, vault, objects, "tool-admission-test")
	if err != nil || !engine.canRestore() || engine.canCreate() {
		t.Fatalf("missing dump tool disabled restoration or enabled creation: %v", err)
	}
	writer, err := objects.Begin(context.Background(), backupstore.BeginOptions{Kind: backupstore.KindGenerated})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	manifest, err := engine.Create(context.Background(), writer, []byte("tool-admission-passphrase"))
	if !errors.Is(err, ErrUnavailable) || manifest.Format != "" || writer.Metadata().Size != 0 {
		t.Fatalf("missing dump tool entered backup creation: %v", err)
	}
	offline, err := NewOfflineEngine(cfg, objects, "tool-admission-test")
	if err != nil || !offline.canRestore() || offline.canCreate() || offline.options.PGDump != "" {
		t.Fatalf("offline construction retained a dump requirement: %v", err)
	}
	cfg.Recovery.PGRestorePath = filepath.Join(t.TempDir(), "missing-pg-restore")
	if engine, err := NewEngine(cfg, pool, vault, objects, "tool-admission-test"); !errors.Is(err, ErrUnavailable) || engine != nil {
		t.Fatalf("online recovery admitted a missing required decoder: %v", err)
	}
	if engine, err := NewOfflineEngine(cfg, objects, "tool-admission-test"); !errors.Is(err, ErrUnavailable) || engine != nil {
		t.Fatalf("offline recovery admitted a missing required decoder: %v", err)
	}
}

func TestOfflineMissingDecoderRefusesPlanBeforeTargetWork(t *testing.T) {
	ctx := context.Background()
	cfg := testDeployment(t)
	cfg.Recovery.PGDumpPath = filepath.Join(t.TempDir(), "missing-pg-dump")
	cfg.Recovery.PGRestorePath = filepath.Join(t.TempDir(), "missing-pg-restore")
	runtime, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	manager, err := NewOfflineManager(ctx, runtime, cfg, "missing-decoder-test")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(ctx)
	status, err := manager.OperatorStatus(ctx)
	if err != nil || status.Available || status.RestoreAvailable || status.RestoreUnavailableReason != "tools_unavailable" {
		t.Fatalf("missing decoder did not remain separately unavailable: %v", err)
	}
	_, err = manager.OperatorPlan(ctx, PlanRequest{
		RequestId: strings.Repeat("1", 32), BackupId: strings.Repeat("2", 32), SHA256: strings.Repeat("3", 64),
		Passphrase: []byte("missing-decoder-passphrase"), GenerationRevision: "0", ReplaceRollback: true,
	})
	if !errors.Is(err, ErrUnavailable) || len(manager.data.Operations) != 0 || len(manager.jobs) != 0 {
		t.Fatalf("missing decoder admitted target replacement work: %v", err)
	}
}
