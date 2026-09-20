package artwork_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

func TestCollageLayoutsAndGeneratedSourceIdentity(t *testing.T) {
	colors := []color.NRGBA{
		{R: 220, G: 20, B: 30, A: 255},
		{R: 30, G: 210, B: 40, A: 255},
		{R: 40, G: 50, B: 200, A: 255},
		{R: 210, G: 190, B: 30, A: 255},
	}
	sources := make([][]byte, len(colors))
	for index, value := range colors {
		sources[index] = collageSolidPNG(t, value, 8, 8)
	}
	for _, test := range []struct {
		name  string
		count int
		// Values identify the source covering each quarter of the canvas.
		quarters [4]int
	}{
		{"empty", 0, [4]int{-1, -1, -1, -1}},
		{"one", 1, [4]int{0, 0, 0, 0}},
		{"two", 2, [4]int{0, 1, 0, 1}},
		{"three", 3, [4]int{0, 1, 0, 2}},
		{"four", 4, [4]int{0, 1, 2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := artwork.Collage(context.Background(), sources[:test.count])
			if err != nil {
				t.Fatal(err)
			}
			decoded := assertImageResult(t, result, "png", 512, 512)
			wantSource := artwork.Info{Format: "png", MIMEType: "image/png", Tag: digest(result.Bytes), Width: 512, Height: 512}
			if result.Source != wantSource {
				t.Fatalf("generated source = %+v, want %+v", result.Source, wantSource)
			}
			for _, y := range []int{0, 128, 255, 256, 384, 511} {
				for _, x := range []int{0, 128, 255, 256, 384, 511} {
					index := test.quarters[(y/256)*2+x/256]
					want := color.NRGBA{R: 24, G: 28, B: 36, A: 255}
					if index >= 0 {
						want = colors[index]
					}
					assertColorNear(t, decoded.At(x, y), want, 0)
				}
			}
			second, err := artwork.Collage(context.Background(), sources[:test.count])
			if err != nil || !bytes.Equal(second.Bytes, result.Bytes) {
				t.Fatalf("repeated collage changed its encoded result: %v", err)
			}
			derivative := renderImage(t, result.Bytes, artwork.Options{Width: 128})
			if derivative.Source.Tag != result.Source.Tag {
				t.Fatal("rendering the generated PNG changed its source identity")
			}
		})
	}
	forward, err := artwork.Collage(context.Background(), sources[:2])
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := artwork.Collage(context.Background(), [][]byte{sources[1], sources[0]})
	if err != nil {
		t.Fatal(err)
	}
	if forward.ETag == reverse.ETag {
		t.Fatal("reordering distinct sources did not change the collage identity")
	}
	decoded := assertImageResult(t, reverse, "png", 512, 512)
	assertColorNear(t, decoded.At(128, 256), colors[1], 0)
	assertColorNear(t, decoded.At(384, 256), colors[0], 0)
}

func TestCollageCenterCoverAndExtremeAspectRatios(t *testing.T) {
	center := color.NRGBA{R: 30, G: 180, B: 70, A: 255}
	edge := color.NRGBA{R: 230, G: 20, B: 10, A: 255}
	for _, test := range []struct {
		name   string
		bounds image.Rectangle
		center image.Rectangle
	}{
		{"wide", image.Rect(0, 0, 12, 4), image.Rect(4, 0, 8, 4)},
		{"tall", image.Rect(0, 0, 4, 12), image.Rect(0, 4, 4, 8)},
		{"extremely wide", image.Rect(0, 0, 16384, 1), image.Rect(8191, 0, 8193, 1)},
		{"extremely tall", image.Rect(0, 0, 1, 16384), image.Rect(0, 8191, 1, 8193)},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := image.NewNRGBA(test.bounds)
			draw.Draw(source, source.Bounds(), image.NewUniform(edge), image.Point{}, draw.Src)
			draw.Draw(source, test.center, image.NewUniform(center), image.Point{}, draw.Src)
			result, err := artwork.Collage(context.Background(), [][]byte{encodeImage(t, "png", source)})
			if err != nil {
				t.Fatal(err)
			}
			decoded := assertImageResult(t, result, "png", 512, 512)
			for _, point := range []image.Point{{128, 128}, {384, 128}, {128, 384}, {384, 384}} {
				assertColorNear(t, decoded.At(point.X, point.Y), center, 0)
			}
		})
	}
}

func TestCollageCompositesTransparencyOntoOpaqueBackground(t *testing.T) {
	for _, test := range []struct {
		name string
		in   color.NRGBA
		want color.NRGBA
	}{
		{"transparent", color.NRGBA{R: 255}, color.NRGBA{R: 24, G: 28, B: 36, A: 255}},
		{"half red", color.NRGBA{R: 255, A: 128}, color.NRGBA{R: 140, G: 14, B: 18, A: 255}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := artwork.Collage(context.Background(), [][]byte{collageSolidPNG(t, test.in, 1, 1)})
			if err != nil {
				t.Fatal(err)
			}
			decoded := assertImageResult(t, result, "png", 512, 512)
			assertColorNear(t, decoded.At(0, 0), test.want, 1)
			assertColorNear(t, decoded.At(511, 511), test.want, 1)
		})
	}
}

func TestCollageUsesFirstCompositedGIFFrame(t *testing.T) {
	green := color.NRGBA{G: 220, A: 255}
	red := color.NRGBA{R: 220, A: 255}
	palette := color.Palette{green, red, color.NRGBA{B: 220, A: 255}}
	first := image.NewPaletted(image.Rect(1, 1, 3, 3), palette)
	second := image.NewPaletted(image.Rect(0, 0, 4, 4), palette)
	for index := range first.Pix {
		first.Pix[index] = 1
	}
	for index := range second.Pix {
		second.Pix[index] = 2
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{
		Image: []*image.Paletted{first, second}, Delay: []int{10, 20},
		Disposal: []byte{gif.DisposalPrevious, gif.DisposalBackground}, LoopCount: 3,
		Config: image.Config{ColorModel: palette, Width: 4, Height: 4}, BackgroundIndex: 0,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := artwork.Collage(context.Background(), [][]byte{data.Bytes()})
	if err != nil {
		t.Fatal(err)
	}
	decoded := assertImageResult(t, result, "png", 512, 512)
	assertColorNear(t, decoded.At(0, 0), green, 0)
	assertColorNear(t, decoded.At(256, 256), red, 0)
	assertColorNear(t, decoded.At(511, 511), green, 0)
}

func TestCollageRejectsInvalidSourcesAndResourceLimits(t *testing.T) {
	valid := collageSolidPNG(t, color.NRGBA{G: 120, A: 255}, 4, 4)
	animation := repeatedGIF(t, 2, 2, 2)
	damagedAnimation := animation[:len(animation)-3]
	if _, err := gif.Decode(bytes.NewReader(damagedAnimation)); err != nil {
		t.Fatalf("damaged animation must retain a decodable first frame: %v", err)
	}
	for _, test := range []struct {
		name    string
		sources [][]byte
		want    error
	}{
		{"too many sources", [][]byte{valid, valid, valid, valid, valid}, artwork.ErrLimitExceeded},
		{"oversized source", [][]byte{valid, make([]byte, (20<<20)+1)}, artwork.ErrLimitExceeded},
		{"oversized width", [][]byte{pngHeader(16385, 1)}, artwork.ErrLimitExceeded},
		{"oversized height", [][]byte{pngHeader(1, 16385)}, artwork.ErrLimitExceeded},
		{"oversized pixels", [][]byte{pngHeader(6000, 5000)}, artwork.ErrLimitExceeded},
		{"too many GIF frames", [][]byte{repeatedGIF(t, 1, 1, 1001)}, artwork.ErrLimitExceeded},
		{"missing image", [][]byte{nil}, artwork.ErrUnsupportedFormat},
		{"unsupported image", [][]byte{[]byte("<svg></svg>")}, artwork.ErrUnsupportedFormat},
		{"corrupt later source", [][]byte{valid, valid[:33]}, artwork.ErrInvalidImage},
		{"corrupt later GIF frame", [][]byte{damagedAnimation}, artwork.ErrInvalidImage},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := artwork.Collage(context.Background(), test.sources)
			if !errors.Is(err, test.want) {
				t.Fatalf("Collage error = %v, want %v", err, test.want)
			}
			if len(result.Bytes) != 0 || result.ETag != "" {
				t.Fatal("failed collage returned a partial image or identity")
			}
		})
	}
}

func TestCollageHonorsPriorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := artwork.Collage(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled collage error = %v", err)
	}
}

func TestCollageCancellationSharesDecodeSlots(t *testing.T) {
	data := collageSolidPNG(t, color.NRGBA{B: 200, A: 255}, 8, 8)
	release := make(chan struct{})
	finished := make(chan error, 2)
	defer func() {
		close(release)
		for range 2 {
			select {
			case err := <-finished:
				if err != nil {
					t.Errorf("occupying decoder failed after release: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Error("occupying decoder did not finish after release")
			}
		}
	}()
	for range 2 {
		reader := &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release}
		go func() {
			_, err := artwork.Inspect(reader)
			finished <- err
		}()
		select {
		case <-reader.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("occupying decoder did not acquire its shared slot")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := artwork.Collage(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued collage error = %v, want context.DeadlineExceeded", err)
	}
}

func collageSolidPNG(t *testing.T, value color.NRGBA, width, height int) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(source, source.Bounds(), image.NewUniform(value), image.Point{}, draw.Src)
	return encodeImage(t, "png", source)
}
