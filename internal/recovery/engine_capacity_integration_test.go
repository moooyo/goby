//go:build linux

package recovery

import (
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
)

func TestEngineRejectsUnsupportedDumpExpansionBeforeEncryptedPublication(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newEngineRecoveryFixture(t)
	var original string
	if f.source.QueryRow(f.ctx, `SELECT configuration::text FROM users WHERE id=$1`, f.actor.User.ID).Scan(&original) != nil {
		t.Fatal("capture the original synthetic administrator configuration")
	}
	if _, err := f.source.Exec(f.ctx, `UPDATE users SET configuration=jsonb_build_object('ReviewCapacity',repeat('x',$2)) WHERE id=$1`, f.actor.User.ID, 10<<20); err != nil {
		t.Fatal("seed source data above the expanded eight MiB budget")
	}
	writer, err := f.objects.Begin(f.ctx, backupstore.BeginOptions{Kind: backupstore.KindGenerated})
	if err != nil {
		t.Fatal("begin the unpublished capacity witness")
	}
	defer writer.Close()
	passphrase := []byte("recovery-capacity-passphrase")
	defer clear(passphrase)
	manifest, err := f.engine.Create(f.ctx, writer, passphrase)
	if !errors.Is(err, backuppg.ErrLimit) || manifest.Format != "" {
		t.Fatalf("unsupported generated archive was not rejected: %v", err)
	}
	metadata := writer.Metadata()
	if metadata.State == backupstore.StateReady || metadata.Verified || metadata.Summary != nil || metadata.Size != 0 || metadata.Digest != "" {
		t.Fatal("rejected expansion produced verified or publishable encrypted bytes")
	}
	if _, err := writer.Prepare(f.ctx); !errors.Is(err, backupstore.ErrInvalid) {
		t.Fatalf("rejected expansion left a prepared object: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal("close the rejected capacity witness")
	}
	status := f.objects.Status()
	if !status.Healthy || status.Bytes != 0 || status.Writers != 0 || status.Readers != 0 || status.ScratchFiles != 0 || status.ScratchBytes != 0 {
		t.Fatalf("capacity rejection retained storage resources: %+v", status)
	}

	// A new supported backup through the same engine must succeed, proving
	// that failed preflight released its gate and anonymous scratch lifetime.
	if _, err := f.source.Exec(f.ctx, `UPDATE users SET configuration=$2::jsonb WHERE id=$1`, f.actor.User.ID, original); err != nil {
		t.Fatal("restore the original synthetic administrator configuration")
	}
	_, completed := f.create(t)
	if completed.State != backupstore.StateReady || !completed.Verified || completed.Size <= 0 || completed.Digest == "" {
		t.Fatal("supported retry did not publish a verified backup")
	}
}
