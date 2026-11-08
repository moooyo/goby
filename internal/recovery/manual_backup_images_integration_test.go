//go:build linux

package recovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestManualBackupImageSelectionPreservesContentReceiptsAndRetentionOwnership(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	ctx, actor := f.seed.ctx, f.seed.actor
	passphrase := []byte("manual-image-selection-passphrase")
	defer clear(passphrase)
	avatar := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	avatar.SetNRGBA(0, 0, color.NRGBA{R: 31, G: 73, B: 137, A: 255})
	var content bytes.Buffer
	if err := png.Encode(&content, avatar); err != nil {
		t.Fatal("encode the managed avatar fixture")
	}
	digest := sha256.Sum256(content.Bytes())
	var stateID int64
	if err := f.seed.source.QueryRow(ctx, `INSERT INTO artwork_state(user_id,revision,managed_types)
		VALUES($1,1,ARRAY['Primary']) RETURNING id`, actor.User.ID).Scan(&stateID); err != nil {
		t.Fatal("seed the managed avatar state")
	}
	if _, err := f.seed.source.Exec(ctx, `INSERT INTO artwork_images(state_id,image_type,image_index,content,mime_type,width,height,source_hash)
		VALUES($1,'Primary',0,$2,'image/png',2,2,$3)`, stateID, content.Bytes(), hex.EncodeToString(digest[:])); err != nil {
		t.Fatal("seed the managed avatar payload")
	}
	fullRequest := CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase}
	full, err := f.manager.Create(ctx, actor, fullRequest)
	if err != nil {
		t.Fatalf("admit the default full-image manual backup: %v", err)
	}
	full = f.waitOperation(t, full.Id, "completed")
	fullRecord, err := f.manager.operationCopy(full.Id)
	if err != nil || fullRecord.Fingerprint != requestFingerprint(struct{ Kind, Generation string }{"create", f.manager.current.Digest}) {
		t.Fatal("the default create changed the fingerprint of legacy full-image receipts")
	}
	fullBackup, err := f.manager.Backup(ctx, actor, full.BackupId)
	if err != nil || fullBackup.Source == nil {
		t.Fatal("the default manual backup omitted its verified source summary")
	}
	assertManualBackupArtworkRows(t, fullBackup.Source, "1")
	include := true
	fullRequest.IncludeImages = &include
	if replayed, err := f.manager.Create(ctx, actor, fullRequest); err != nil || replayed.Id != full.Id || replayed.BackupId != full.BackupId {
		t.Fatal("explicit inclusion did not replay the existing default-content receipt")
	}
	exclude := false
	fullRequest.IncludeImages = &exclude
	if _, err := f.manager.Create(ctx, actor, fullRequest); !errors.Is(err, ErrConflict) {
		t.Fatal("one request ID was accepted for different manual backup contents")
	}
	projectedRequest := CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase, IncludeImages: &exclude}
	projected, err := f.manager.Create(ctx, actor, projectedRequest)
	if err != nil {
		t.Fatalf("admit the image-free manual backup: %v", err)
	}
	projected = f.waitOperation(t, projected.Id, "completed")
	projectedBackup, err := f.manager.Backup(ctx, actor, projected.BackupId)
	if err != nil || projectedBackup.Source == nil || !projectedBackup.Verified {
		t.Fatal("the image-free manual backup was not fully checked and published")
	}
	assertManualBackupArtworkRows(t, projectedBackup.Source, "0")
	var intact, manualOnly bool
	if err := f.seed.source.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM artwork_state WHERE id=$1 AND user_id=$2)
		AND EXISTS(SELECT 1 FROM artwork_images WHERE state_id=$1 AND content=$3 AND source_hash=$4)`,
		stateID, actor.User.ID, content.Bytes(), hex.EncodeToString(digest[:])).Scan(&intact); err != nil || !intact {
		t.Fatal("image exclusion changed the live managed avatar")
	}
	if err := f.seed.source.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM backup_policy_runs WHERE backup_id IN ($1,$2))`,
		full.BackupId, projected.BackupId).Scan(&manualOnly); err != nil || !manualOnly {
		t.Fatal("manual backups entered the automatic backup retention ledger")
	}
	f.reopen(t)
	fullRequest.IncludeImages = &include
	if replayed, err := f.manager.Create(ctx, actor, fullRequest); err != nil || replayed.Id != full.Id || replayed.BackupId != full.BackupId {
		t.Fatal("reopened legacy-compatible receipt did not replay explicit image inclusion")
	}
	if replayed, err := f.manager.Create(ctx, actor, projectedRequest); err != nil || replayed.Id != projected.Id || replayed.BackupId != projected.BackupId {
		t.Fatal("reopened image-free receipt created another manual backup")
	}
	projectedRequest.IncludeImages = nil
	if _, err := f.manager.Create(ctx, actor, projectedRequest); !errors.Is(err, ErrConflict) {
		t.Fatal("reopened image-free receipt accepted the default full-image contents")
	}
}

func assertManualBackupArtworkRows(t *testing.T, source *SourceView, want string) {
	t.Helper()
	seen := make(map[string]bool)
	for _, table := range source.Tables {
		if table.Name == "artwork_state" || table.Name == "artwork_images" {
			seen[table.Name] = true
			if table.Rows != want {
				t.Fatalf("manual backup %s rows = %s, want %s", table.Name, table.Rows, want)
			}
		}
	}
	if len(seen) != 2 {
		t.Fatal("manual backup source summary omitted managed artwork tables")
	}
}
