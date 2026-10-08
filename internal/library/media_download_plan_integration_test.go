//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestDownloadPlanDoesNotOpenSourceAndDeliveryChecksIt(t *testing.T) {
	f := mediaSourceTestCatalog(t, nil)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, f.userID); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.path, f.path+".displaced"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(f.path+".displaced", f.path)
	subject := Subject{UserID: f.userID}
	plan, err := f.store.PlanDownloadFor(f.ctx, subject, f.item.ID, "")
	if err != nil || plan.ETag == "" || plan.Item.CanPlay {
		t.Fatalf("database-only download planning opened the missing source or granted playback: %+v %v", plan, err)
	}
	file, _, content, err := f.store.OpenPreparedOriginalDownloadFor(f.ctx, f.ctx, subject, plan.Item.ID, plan.SourceID, plan.ETag)
	if content != nil {
		_ = content.Close()
	} else if file != nil {
		_ = file.Close()
	}
	if err == nil || file != nil || content != nil {
		t.Fatalf("delivery accepted an unproven database-only source: %v", err)
	}
}

func TestDownloadPreparationCancellationDoesNotBecomeReaderLifetime(t *testing.T) {
	f := mediaSourceTestCatalog(t, nil)
	subject := Subject{UserID: f.userID}
	plan, err := f.store.PlanDownloadFor(f.ctx, subject, f.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	prepare, cancel := context.WithCancel(f.ctx)
	defer cancel()
	file, source, content, err := f.store.OpenPreparedOriginalDownloadFor(prepare, f.ctx, subject, f.item.ID, media.SourceID(f.item.ID), plan.ETag)
	if err != nil || file == nil || content == nil || source.ETag != plan.ETag {
		t.Fatalf("open planned original: %v", err)
	}
	defer content.Close()
	cancel()
	buffer := make([]byte, 8)
	if n, err := content.Read(buffer); err != nil || n != len(buffer) || string(buffer) != f.contents[:8] {
		t.Fatalf("preparation cancellation killed the handed-off reader: bytes=%q error=%v", buffer, err)
	}
	if _, _, reader, err := f.store.OpenPreparedOriginalDownloadFor(prepare, f.ctx, subject, f.item.ID, plan.SourceID, plan.ETag); !errors.Is(err, context.Canceled) || reader != nil {
		if reader != nil {
			_ = reader.Close()
		}
		t.Fatalf("cancelled preparation admitted another reader: %v", err)
	}
}
