package artwork

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestWhitespaceAndLayerProcessingObserveCancellation(t *testing.T) {
	for _, operation := range []string{"whitespace", "layers"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &cancelArtworkImage{Image: image.NewNRGBA(image.Rect(0, 0, 8, 8)), cancel: cancel}
			var err error
			if operation == "whitespace" {
				_, err = contentBounds(ctx, source, source.Bounds())
			} else {
				_, err = decorateFrame(ctx, source, Options{BackgroundColor: "#000000ff", ForegroundLayer: "play:#ffffffff"})
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled %s transformation returned %v", operation, err)
			}
			if source.reads >= 64 {
				t.Fatalf("%s ignored cancellation through the full canvas", operation)
			}
		})
	}
}

type cancelArtworkImage struct {
	image.Image
	cancel context.CancelFunc
	reads  int
}

func (source *cancelArtworkImage) At(x, y int) color.Color {
	source.reads++
	if source.reads == 12 {
		source.cancel()
	}
	return source.Image.At(x, y)
}
