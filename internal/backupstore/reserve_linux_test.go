//go:build linux

package backupstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReopenBelowFreeSpaceReserveKeepsReadAndDelete(t *testing.T) {
	f := newStoreFixture(t)
	content := []byte("retained encrypted object fixture")
	metadata := publishTestObject(t, f.store, content)
	free, err := availableBytes(int(f.store.directory.Fd()))
	if err != nil {
		t.Fatal("sample the isolated store filesystem")
	}
	const maximumReserve = int64(1 << 40)
	if free >= uint64(maximumReserve) {
		t.Skip("the filesystem has more free space than a legal test reserve")
	}
	if err := f.store.Close(); err != nil {
		t.Fatal("close the original store")
	}
	f.config.MinFreeBytes = maximumReserve
	f.store, err = Open(f.config)
	if err != nil {
		f.abandoned = true
		t.Fatalf("capacity reserve blocked opening intact storage: %v", err)
	}
	page, err := f.store.List(t.Context(), 0, 1)
	if err != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 || page.Items[0].ID != metadata.ID {
		t.Fatalf("below-reserve catalog was not readable: %+v, %v", page, err)
	}
	snapshot, err := f.store.Snapshot(t.Context(), metadata.ID)
	if err != nil {
		t.Fatalf("below-reserve object was not downloadable: %v", err)
	}
	actual, readErr := io.ReadAll(snapshot)
	closeErr := snapshot.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, content) {
		t.Fatal("reading below the reserve changed the retained object")
	}
	if writer, err := f.store.Begin(t.Context(), BeginOptions{Kind: KindImported}); !errors.Is(err, ErrQuota) || writer != nil {
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatalf("below-reserve object admission = %v", err)
	}
	if scratch, err := f.store.Scratch(t.Context(), 1); !errors.Is(err, ErrQuota) || scratch != nil {
		if scratch != nil {
			_ = scratch.Close()
		}
		t.Fatalf("below-reserve scratch admission = %v", err)
	}
	if err := f.store.Delete(t.Context(), metadata.ID, metadata.Digest); err != nil {
		t.Fatalf("below-reserve deletion could not reclaim space: %v", err)
	}
	status := f.store.Status()
	if !status.Healthy || status.Objects != 0 || status.Bytes != 0 || status.Readers != 0 || status.Writers != 0 || status.ScratchFiles != 0 {
		t.Fatalf("below-reserve access damaged storage state: %+v", status)
	}
}

func TestFreeSpaceReserveStillBoundsEveryAllocation(t *testing.T) {
	for _, available := range []int64{-1, 0, 1} {
		name := "below"
		if available == 0 {
			name = "exact"
		} else if available == 1 {
			name = "one_byte_above"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t)
			writer := beginTestWriter(t, f.store, context.Background())
			reserve := uint64(f.config.MinFreeBytes) + uint64(metadataReserve)
			f.store.freeBytes = func(int) (uint64, error) {
				return uint64(int64(reserve) + available), nil
			}
			newWriter, beginErr := f.store.Begin(t.Context(), BeginOptions{Kind: KindImported})
			if available < 0 {
				if !errors.Is(beginErr, ErrQuota) || newWriter != nil {
					t.Fatalf("below-reserve Begin = %v", beginErr)
				}
			} else {
				if beginErr != nil || newWriter == nil {
					t.Fatalf("zero-byte admission at the reserve = %v", beginErr)
				}
				if err := newWriter.Close(); err != nil {
					t.Fatal("close empty admitted writer")
				}
			}
			n, writeErr := writer.Write([]byte{'x'})
			if available < 1 {
				if n != 0 || !errors.Is(writeErr, ErrQuota) {
					t.Fatalf("one-byte write below its reserve = (%d, %v)", n, writeErr)
				}
			} else if n != 1 || writeErr != nil {
				t.Fatalf("exact one-byte write allowance = (%d, %v)", n, writeErr)
			}
			if err := writer.Close(); err != nil {
				t.Fatal("close allocation witness")
			}
			scratch, scratchErr := f.store.Scratch(t.Context(), 1)
			if available < 1 {
				if !errors.Is(scratchErr, ErrQuota) || scratch != nil {
					t.Fatalf("one-byte scratch below its reserve = %v", scratchErr)
				}
			} else {
				if scratchErr != nil || scratch == nil {
					t.Fatalf("exact one-byte scratch allowance = %v", scratchErr)
				}
				if err := scratch.Close(); err != nil {
					t.Fatal("close reserved scratch")
				}
			}
			if !f.store.Status().Healthy {
				t.Fatal("ordinary capacity rejection degraded intact storage")
			}
		})
	}
}

func TestReopenWithHighReserveStillRejectsCorruptObject(t *testing.T) {
	f := newStoreFixture(t)
	metadata := publishTestObject(t, f.store, []byte("retained encrypted object fixture"))
	if err := f.store.Close(); err != nil {
		t.Fatal("close original storage")
	}
	path := filepath.Join(f.config.Directory, basename(metadata.ID, true))
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal("introduce an unsafe object permission")
	}
	config := f.config
	config.MinFreeBytes = 1 << 40
	opened, err := Open(config)
	if opened != nil {
		_ = opened.Close()
	}
	if !errors.Is(err, ErrUnavailable) || opened != nil {
		t.Fatalf("capacity handling concealed a corrupt object: %v", err)
	}
}
