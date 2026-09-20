package artwork_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

func TestTransformedGIFCompositesPartialFramesAndDisposal(t *testing.T) {
	palette := color.Palette{color.Transparent, color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{G: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}}
	frames := []*image.Paletted{
		solidGIFFrame(image.Rect(0, 0, 2, 4), palette, 1),
		solidGIFFrame(image.Rect(2, 0, 4, 4), palette, 2),
		solidGIFFrame(image.Rect(4, 0, 6, 4), palette, 3),
		solidGIFFrame(image.Rect(2, 0, 4, 4), palette, 4),
	}
	wanted := [4][3]int{{1, 0, 0}, {1, 2, 0}, {1, 0, 3}, {1, 4, 0}}
	for _, loop := range []int{-1, 0, 4} {
		input := &gif.GIF{Image: frames, Delay: []int{0, 2, 17, 9}, LoopCount: loop,
			Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalBackground, gif.DisposalNone},
			Config:   image.Config{ColorModel: palette, Width: 6, Height: 4}}
		var data bytes.Buffer
		if err := gif.EncodeAll(&data, input); err != nil {
			t.Fatal(err)
		}
		result := renderImage(t, data.Bytes(), artwork.Options{Width: 3})
		animation, err := gif.DecodeAll(bytes.NewReader(result.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		if len(animation.Image) != 4 || animation.LoopCount != loop || result.Width != 3 || result.Height != 2 {
			t.Fatalf("animation metadata changed: %+v result=%+v", animation, result)
		}
		for index, frame := range animation.Image {
			if animation.Delay[index] != input.Delay[index] || animation.Disposal[index] != gif.DisposalBackground {
				t.Fatalf("frame %d lost timing or safe disposal", index)
			}
			if frame.Bounds() != image.Rect(0, 0, 3, 2) {
				t.Fatalf("frame %d is not a complete output canvas: %v", index, frame.Bounds())
			}
			for x, paletteIndex := range wanted[index] {
				assertColorNear(t, frame.At(x, 0), color.NRGBAModel.Convert(palette[paletteIndex]).(color.NRGBA), 0)
				assertColorNear(t, frame.At(x, 1), color.NRGBAModel.Convert(palette[paletteIndex]).(color.NRGBA), 0)
			}
		}
		static := renderImage(t, data.Bytes(), artwork.Options{Format: "png"})
		decoded := assertImageResult(t, static, "png", 6, 4)
		assertColorNear(t, decoded.At(0, 0), color.NRGBA{R: 255, A: 255}, 0)
		assertColorNear(t, decoded.At(4, 0), color.NRGBA{}, 0)
	}
}

func TestAnimatedFrameWithoutGCEDoesNotInheritPreviousDisposal(t *testing.T) {
	palette := color.Palette{color.White, color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}, color.NRGBA{G: 255, A: 255}}
	input := &gif.GIF{
		Image: []*image.Paletted{solidGIFFrame(image.Rect(0, 0, 1, 1), palette, 1), solidGIFFrame(image.Rect(1, 0, 2, 1), palette, 2), solidGIFFrame(image.Rect(2, 0, 3, 1), palette, 3)},
		Delay: []int{0, 0, 0}, Disposal: []byte{gif.DisposalBackground, 0, gif.DisposalNone},
		Config: image.Config{ColorModel: palette, Width: 3, Height: 1}, LoopCount: 1,
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, input); err != nil {
		t.Fatal(err)
	}
	// The opaque middle frame has no delay or disposal and therefore no GCE.
	if count := bytes.Count(data.Bytes(), []byte{0x21, 0xf9, 0x04}); count != 2 {
		t.Fatalf("fixture should have two GCE blocks, got %d", count)
	}
	result := renderImage(t, data.Bytes(), artwork.Options{AutoOrient: true})
	animation, err := gif.DecodeAll(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	assertColorNear(t, animation.Image[2].At(0, 0), color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 0)
	assertColorNear(t, animation.Image[2].At(1, 0), color.NRGBA{B: 255, A: 255}, 0)
	assertColorNear(t, animation.Image[2].At(2, 0), color.NRGBA{G: 255, A: 255}, 0)
}

func TestAnimatedWhitespaceCropUsesAllDisplayedFrames(t *testing.T) {
	palette := color.Palette{color.Transparent, color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}}
	input := &gif.GIF{
		Image: []*image.Paletted{solidGIFFrame(image.Rect(1, 1, 2, 3), palette, 1), solidGIFFrame(image.Rect(4, 1, 5, 3), palette, 2)},
		Delay: []int{1, 7}, Disposal: []byte{gif.DisposalBackground, gif.DisposalBackground},
		Config: image.Config{ColorModel: palette, Width: 6, Height: 4}, LoopCount: 2,
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, input); err != nil {
		t.Fatal(err)
	}
	result := renderImage(t, data.Bytes(), artwork.Options{CropWhitespace: true})
	assertImageResult(t, result, "gif", 4, 2)
	animation, err := gif.DecodeAll(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	assertColorNear(t, animation.Image[0].At(0, 0), color.NRGBA{R: 255, A: 255}, 0)
	assertColorNear(t, animation.Image[0].At(3, 0), color.NRGBA{}, 0)
	assertColorNear(t, animation.Image[1].At(0, 0), color.NRGBA{}, 0)
	assertColorNear(t, animation.Image[1].At(3, 0), color.NRGBA{B: 255, A: 255}, 0)
}

func TestAnimatedTransformRejectsSparseCanvasExpansionBudget(t *testing.T) {
	palette := color.Palette{color.Transparent, color.Black}
	input := &gif.GIF{Config: image.Config{ColorModel: palette, Width: 4096, Height: 4096}, LoopCount: -1}
	for range 3 {
		input.Image = append(input.Image, solidGIFFrame(image.Rect(0, 0, 1, 1), palette, 1))
		input.Delay = append(input.Delay, 1)
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, input); err != nil {
		t.Fatal(err)
	}
	// The indexed input is only three pixels. Transforming it must not expand
	// it into unbudgeted full canvases, even when the requested output is tiny.
	if _, err := artwork.Inspect(bytes.NewReader(data.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := artwork.Render(context.Background(), bytes.NewReader(data.Bytes()), artwork.Options{Width: 1}); !errors.Is(err, artwork.ErrLimitExceeded) {
		t.Fatalf("unbounded sparse animation accepted: %v", err)
	}
	result := renderImage(t, data.Bytes(), artwork.Options{})
	if !bytes.Equal(result.Bytes, data.Bytes()) {
		t.Fatal("bounded unchanged animation should remain lossless")
	}
}

func TestAnimatedTransformsDecorateEveryFrameWithoutMutatingComposition(t *testing.T) {
	palette := color.Palette{color.Transparent, color.NRGBA{R: 255, A: 255}}
	input := &gif.GIF{
		Image: []*image.Paletted{solidGIFFrame(image.Rect(0, 0, 10, 10), palette, 0), solidGIFFrame(image.Rect(0, 0, 1, 1), palette, 1)},
		Delay: []int{3, 4}, Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config: image.Config{ColorModel: palette, Width: 10, Height: 10}, LoopCount: 0,
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, input); err != nil {
		t.Fatal(err)
	}
	result := renderImage(t, data.Bytes(), artwork.Options{BackgroundColor: "#0000ff80", PercentPlayed: 50})
	animation, err := gif.DecodeAll(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	for index, frame := range animation.Image {
		// GIF has binary alpha, so 128 alpha becomes opaque blue in both frames.
		assertColorNear(t, frame.At(8, 5), color.NRGBA{B: 255, A: 255}, 0)
		assertColorNear(t, frame.At(0, 9), color.NRGBA{R: 51, G: 153, B: 102, A: 255}, 0)
		if animation.Delay[index] != input.Delay[index] {
			t.Fatal("overlay dropped frame delay")
		}
	}
	assertColorNear(t, animation.Image[0].At(0, 0), color.NRGBA{B: 255, A: 255}, 0)
	assertColorNear(t, animation.Image[1].At(0, 0), color.NRGBA{R: 255, A: 255}, 0)
}

func solidGIFFrame(bounds image.Rectangle, palette color.Palette, index uint8) *image.Paletted {
	frame := image.NewPaletted(bounds, palette)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			frame.SetColorIndex(x, y, index)
		}
	}
	return frame
}
