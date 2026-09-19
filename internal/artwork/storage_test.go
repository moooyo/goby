package artwork_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

func TestManagedArtworkPreservesValidatedOriginalAndOwnsItsBytes(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			data := encodeImage(t, format, sampleImage(12, 8))
			original := append([]byte(nil), data...)
			image, err := artwork.PrepareManagedImage(context.Background(), "primary", 0, data)
			if err != nil {
				t.Fatal(err)
			}
			data[0] ^= 0xff
			if image.ImageType != "Primary" || image.Tag != digest(original) || image.Size != int64(len(original)) ||
				image.MIMEType != "image/"+format || image.Width != 12 || image.Height != 8 || !bytes.Equal(image.Content, original) {
				t.Fatalf("managed image lost original bytes or validated metadata: %+v", image)
			}
		})
	}
	data := encodeImage(t, "png", sampleImage(12, 8))
	for _, input := range []struct {
		kind  string
		index int
		body  []byte
	}{
		{"Primary", 1, data}, {"Backdrop", 32, data}, {"Screenshot", -1, data},
		{"Unknown", 0, data}, {"Primary", 0, data[:len(data)/2]}, {"Primary", 0, []byte("<svg/>")},
	} {
		if _, err := artwork.PrepareManagedImage(context.Background(), input.kind, input.index, input.body); err == nil {
			t.Errorf("accepted invalid managed image type=%s index=%d bytes=%d", input.kind, input.index, len(input.body))
		}
	}
}

func TestManagedArtworkRevisionBindsSourcesOwnerAndState(t *testing.T) {
	primary, err := artwork.PrepareManagedImage(context.Background(), "Primary", 0, encodeImage(t, "png", sampleImage(12, 8)))
	if err != nil {
		t.Fatal(err)
	}
	backdrop := primary
	backdrop.ImageType = "Backdrop"
	images := []artwork.StoredImage{primary, backdrop}
	target := artwork.Target{Kind: "item", ID: "movie"}
	revision := artwork.RevisionToken(target, 0, images)
	if len(revision) > 78 || strings.Trim(revision, "0123456789") != "" || len(revision) > 1 && revision[0] == '0' {
		t.Fatalf("revision is not canonical bounded decimal text: %q", revision)
	}
	primary.Content = nil
	primary.ModifiedAt = time.Now()
	if actual := artwork.RevisionToken(target, 0, []artwork.StoredImage{backdrop, primary}); actual != revision {
		t.Fatal("row order, fetched time or payload presence changed an otherwise identical revision")
	}
	for _, changed := range []string{
		artwork.RevisionToken(artwork.Target{Kind: "entity", ID: "1"}, 0, images),
		artwork.RevisionToken(target, 1, images),
		artwork.RevisionToken(target, 0, images[:1]),
	} {
		if changed == revision {
			t.Fatal("revision did not bind the target, stored revision and effective source list")
		}
	}
	backdrop.Tag = strings.Repeat("0", 64)
	if artwork.RevisionToken(target, 0, []artwork.StoredImage{primary, backdrop}) == revision {
		t.Fatal("automatic artwork replacement did not invalidate the editor revision")
	}
}
