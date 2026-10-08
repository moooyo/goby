//go:build linux

package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

const embeddedArtworkCountedPNGMagic = "goby-test-counted-embedded-png\n"

var embeddedArtworkCountedPNG struct {
	once    sync.Once
	configs atomic.Int64
	decodes atomic.Int64
}

func embeddedArtworkCountedPicture(t *testing.T) media.EmbeddedPicture {
	t.Helper()
	// A test-only registry entry delegates to the real PNG decoder after its
	// unique prefix, so decode work is observable without a production hook.
	embeddedArtworkCountedPNG.once.Do(func() {
		image.RegisterFormat("png", embeddedArtworkCountedPNGMagic, func(reader io.Reader) (image.Image, error) {
			embeddedArtworkCountedPNG.decodes.Add(1)
			if _, err := io.CopyN(io.Discard, reader, int64(len(embeddedArtworkCountedPNGMagic))); err != nil {
				return nil, err
			}
			return png.Decode(reader)
		}, func(reader io.Reader) (image.Config, error) {
			embeddedArtworkCountedPNG.configs.Add(1)
			if _, err := io.CopyN(io.Discard, reader, int64(len(embeddedArtworkCountedPNGMagic))); err != nil {
				return image.Config{}, err
			}
			return png.DecodeConfig(reader)
		})
	})
	picture := embeddedArtworkTestPicture(t, 2, "Front", color.NRGBA{G: 155, B: 40, A: 255})
	picture.Data = append([]byte(embeddedArtworkCountedPNGMagic), picture.Data...)
	digest := sha256.Sum256(picture.Data)
	picture.Hash = hex.EncodeToString(digest[:])
	return picture
}

func TestEmbeddedArtworkInspectionCacheSharesSuccessfulMetadataWithCollages(t *testing.T) {
	picture := embeddedArtworkCountedPicture(t)
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
	ctx, _, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Track.flac", "audio:counted-embedded-inspection")
	collection := libraryIntegrationCreate(t, ctx, store, "Counted embedded inspection", "music", filepath.Dir(path))
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	configs, decodes := embeddedArtworkCountedPNG.configs.Load(), embeddedArtworkCountedPNG.decodes.Load()
	for range 3 {
		embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
	}
	if embeddedArtworkCountedPNG.configs.Load()-configs != 1 || embeddedArtworkCountedPNG.decodes.Load()-decodes != 1 {
		t.Fatal("repeated embedded opens did not reuse one successful inspection")
	}
	first, source := collageTestOpen(t, ctx, store, Subject{UserID: userID}, collection.ID)
	// The first collage still decodes its pixels for rendering, independently
	// of the metadata cache. Its member inspection must reuse the earlier open.
	if embeddedArtworkCountedPNG.configs.Load()-configs != 2 || embeddedArtworkCountedPNG.decodes.Load()-decodes != 2 {
		t.Fatal("collage member repeated inspection or skipped its independent render")
	}
	repeated, current := collageTestOpen(t, ctx, store, Subject{UserID: userID}, collection.ID)
	if !bytes.Equal(first, repeated) || current.Tag != source.Tag ||
		embeddedArtworkCountedPNG.configs.Load()-configs != 2 || embeddedArtworkCountedPNG.decodes.Load()-decodes != 2 {
		t.Fatal("cached collage repeated successful member inspection or changed bytes")
	}
}

func TestEmbeddedArtworkInspectionCacheChecksCurrentBytesAndMetadata(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 1, "Front", color.NRGBA{R: 165, B: 25, A: 255})
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Track.flac", "audio:cached-embedded-integrity")
	collection := libraryIntegrationCreate(t, ctx, store, "Cached embedded integrity", "music", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
	corrupt := append([]byte(nil), picture.Data...)
	corrupt[0] ^= 1
	corruptDigest := sha256.Sum256(corrupt)
	corruptTag := hex.EncodeToString(corruptDigest[:])
	animation := &gif.GIF{Image: make([]*image.Paletted, 1001), Delay: make([]int, 1001)}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	for index := range animation.Image {
		animation.Image[index] = frame
	}
	var excessiveGIF bytes.Buffer
	if err := gif.EncodeAll(&excessiveGIF, animation); err != nil {
		t.Fatal(err)
	}
	excessiveDigest := sha256.Sum256(excessiveGIF.Bytes())
	for _, test := range []struct {
		name          string
		data          []byte
		tag, mime     string
		width, height int
	}{
		{"width", picture.Data, picture.Hash, picture.MIMEType, picture.Width + 1, picture.Height},
		{"height", picture.Data, picture.Hash, picture.MIMEType, picture.Width, picture.Height + 1},
		{"mime", picture.Data, picture.Hash, "image/jpeg", picture.Width, picture.Height},
		{"bytes_with_old_tag", corrupt, picture.Hash, picture.MIMEType, picture.Width, picture.Height},
		{"corruption_with_current_tag", corrupt, corruptTag, picture.MIMEType, picture.Width, picture.Height},
		{"gif_frame_limit", excessiveGIF.Bytes(), hex.EncodeToString(excessiveDigest[:]), "image/gif", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `UPDATE item_embedded_artwork SET content=$2,source_hash=$3,mime_type=$4,width=$5,height=$6 WHERE item_id=$1`,
				item.ID, test.data, test.tag, test.mime, test.width, test.height); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				reader, _, err := store.OpenEmbeddedImageFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0)
				if reader != nil {
					_ = reader.Close()
				}
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("cached metadata hid changed current artwork: %v", err)
				}
				reader, _, err = store.OpenImageContentFor(ctx, Subject{UserID: userID}, collection.ID, "Primary", 0)
				if reader != nil {
					_ = reader.Close()
				}
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("collage reused changed embedded member metadata: %v", err)
				}
			}
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE item_embedded_artwork SET content=$2,source_hash=$3,mime_type=$4,width=$5,height=$6 WHERE item_id=$1`,
		item.ID, picture.Data, picture.Hash, picture.MIMEType, picture.Width, picture.Height); err != nil {
		t.Fatal(err)
	}
	embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
}
