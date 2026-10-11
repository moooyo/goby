//go:build linux

package recovery

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
)

func TestEngineCreationStillRequiresCompatibleDecoderBeforePublication(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	f.engine.options.PGRestore = recoveryToolFixture(t, "echo 'pg_restore (PostgreSQL) 16.11'")
	writer, err := f.objects.Begin(f.ctx, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	manifest, err := f.engine.Create(f.ctx, writer, []byte("decoder-admission-passphrase"))
	if !errors.Is(err, backuppg.ErrUnsupported) || manifest.Format != "" {
		t.Fatalf("generated backup bypassed the decoder version check: %v", err)
	}
	metadata := writer.Metadata()
	if metadata.Verified || metadata.State == backupstore.StateReady || metadata.Size != 0 || metadata.Digest != "" {
		t.Fatal("invalid decoder left verified or encrypted publication bytes")
	}
	if _, err := writer.Prepare(f.ctx); !errors.Is(err, backupstore.ErrInvalid) {
		t.Fatalf("invalid decoder left a publishable archive: %v", err)
	}
}
