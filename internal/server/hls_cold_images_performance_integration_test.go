//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type hlsColdImageTemplate struct {
	Type   string `json:"type"`
	Suffix string `json:"suffix"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	data   []byte
}

type hlsColdImageCorpus struct {
	Templates    []hlsColdImageTemplate `json:"templates"`
	Files        int                    `json:"files"`
	LogicalBytes int64                  `json:"logical_bytes"`
	StatBlocks   int64                  `json:"stat_blocks_512_bytes"`
}

// Only prefixed names belong to these new movies. Generic artwork would also
// alter the already indexed HLS playback item and change the workload contract.
func hlsColdPrepareImages(t *testing.T, paths []string) *hlsColdImageCorpus {
	if value := os.Getenv("GOBY_COLD_MIXED_IMAGES"); value == "" {
		return nil
	} else if value != "1" {
		t.Fatal("GOBY_COLD_MIXED_IMAGES must be empty or 1")
	}
	t.Helper()
	if len(paths) != hlsColdCopies {
		t.Fatal("cold image corpus requires all independent cold media paths")
	}
	corpus := &hlsColdImageCorpus{Templates: []hlsColdImageTemplate{
		{Type: "Primary", Suffix: "-poster.png", Width: 320, Height: 480},
		{Type: "Backdrop", Suffix: "-backdrop.png", Width: 640, Height: 360},
	}}
	for index := range corpus.Templates {
		template := &corpus.Templates[index]
		pixels := image.NewNRGBA(image.Rect(0, 0, template.Width, template.Height))
		for y := 0; y < template.Height; y++ {
			for x := 0; x < template.Width; x++ {
				pixels.SetNRGBA(x, y, color.NRGBA{R: uint8(x/8 + index*67), G: uint8(y/8 + index*31), B: uint8((x/16 ^ y/16) * 7), A: 255})
			}
		}
		var encoded bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := encoder.Encode(&encoded, pixels); err != nil {
			t.Fatal("encode deterministic cold artwork")
		}
		template.data, template.Bytes = encoded.Bytes(), int64(encoded.Len())
		digest := sha256.Sum256(template.data)
		template.SHA256 = hex.EncodeToString(digest[:])
		corpus.LogicalBytes += template.Bytes * int64(len(paths))
	}
	if corpus.LogicalBytes > 64<<20 {
		t.Fatal("cold artwork exceeds its 64 MiB logical bound")
	}
	for _, path := range paths {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		for _, template := range corpus.Templates {
			file, err := os.OpenFile(base+template.Suffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal("create an independent prefixed cold image")
			}
			written, writeErr := file.Write(template.data)
			info, statErr := file.Stat()
			if err := errors.Join(writeErr, statErr, file.Close()); err != nil || int64(written) != template.Bytes {
				t.Fatal("write the complete cold artwork file")
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() != template.Bytes {
				t.Fatal("cold artwork lost its independent regular-file identity")
			}
			corpus.StatBlocks += stat.Blocks
			corpus.Files++
		}
	}
	return corpus
}

// Verification runs after terminal task completion and outside measured SQL.
// Cached Stop remains a control/ownership check, not live encoder retirement.
func (corpus *hlsColdImageCorpus) verify(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID, playbackPath string, paths []string) {
	if corpus == nil {
		return
	}
	t.Helper()
	expected := make(map[string]map[string]bool, len(paths))
	for _, path := range paths {
		if path == playbackPath || expected[path] != nil {
			t.Fatal("cold artwork paths overlap playback or another source")
		}
		expected[path] = make(map[string]bool, 2)
	}
	rows, err := pool.Query(ctx, `SELECT i.path,i.root_id,im.root_id,r.path,im.image_type,im.image_index,
		im.relative_path,im.source_hash,im.width,im.height,im.mime_type,im.file_size
		FROM items i JOIN item_images im ON im.item_id=i.id JOIN library_roots r ON r.id=i.root_id
		WHERE i.library_id=$1 ORDER BY i.id,im.image_type,im.image_index`, libraryID)
	if err != nil {
		t.Fatal("read completed cold image population")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var path, itemRoot, imageRoot, rootPath, kind, relative, hash, mime string
		var index, width, height int
		var size int64
		if err := rows.Scan(&path, &itemRoot, &imageRoot, &rootPath, &kind, &index, &relative, &hash, &width, &height, &mime, &size); err != nil {
			t.Fatal("decode completed cold image facts")
		}
		seen := expected[path]
		if seen == nil || seen[kind] || itemRoot == "" || imageRoot != itemRoot || index != 0 || mime != "image/png" {
			t.Fatal("cold scan associated unexpected artwork or affected the HLS playback item")
		}
		matched := false
		for _, template := range corpus.Templates {
			if template.Type != kind {
				continue
			}
			wanted, err := filepath.Rel(rootPath, strings.TrimSuffix(path, filepath.Ext(path))+template.Suffix)
			matched = err == nil && filepath.ToSlash(wanted) == relative && hash == template.SHA256 &&
				width == template.Width && height == template.Height && size == template.Bytes
		}
		if !matched {
			t.Fatal("completed cold image changed its expected path, hash, size or dimensions")
		}
		seen[kind], count = true, count+1
	}
	if rows.Err() != nil || count != 2*hlsColdCopies || corpus.Files != count {
		t.Fatal("cold scan did not publish the complete 256-image population")
	}
	for _, seen := range expected {
		if !seen["Primary"] || !seen["Backdrop"] || len(seen) != 2 {
			t.Fatal("a cold media item lacks its exact two artwork types")
		}
	}
	encoded, err := json.Marshal(corpus)
	if err != nil {
		t.Fatal("encode completed cold image evidence")
	}
	t.Logf("completed_cold_actual_images_json=%s", encoded)
}
