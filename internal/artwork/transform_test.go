package artwork_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

func TestRenderCropWhitespaceAndExplicitCropOrder(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(picture, image.Rect(2, 2, 6, 4), image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	picture.Set(3, 2, color.White)
	data := encodeImage(t, "png", picture)
	result := renderImage(t, data, artwork.Options{CropWhitespace: true})
	decoded := assertImageResult(t, result, "png", 4, 2)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{R: 255, A: 255}, 0)
	assertColorNear(t, decoded.At(1, 0), color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 0)
	result = renderImage(t, data, artwork.Options{Crop: artwork.CropRect{X: 1, Y: 1, Width: 6, Height: 4}, CropWhitespace: true, Width: 2})
	assertImageResult(t, result, "png", 2, 1)
	result = renderImage(t, data, artwork.Options{Crop: artwork.CropRect{X: 2, Y: 2, Width: 2, Height: 2}, Width: 100})
	assertImageResult(t, result, "png", 2, 2)
	if _, err := artwork.Render(context.Background(), bytes.NewReader(data), artwork.Options{Crop: artwork.CropRect{X: 7, Y: 0, Width: 2, Height: 1}}); !errors.Is(err, artwork.ErrInvalidOptions) {
		t.Fatalf("out-of-canvas crop: %v", err)
	}
}

func TestRenderWhitespaceHandlesTransparencyAndUniformCanvas(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 7, 5))
	picture.Set(4, 3, color.NRGBA{B: 255, A: 128})
	result := renderImage(t, encodeImage(t, "png", picture), artwork.Options{CropWhitespace: true})
	decoded := assertImageResult(t, result, "png", 1, 1)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{B: 255, A: 128}, 0)
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	result = renderImage(t, encodeImage(t, "png", picture), artwork.Options{CropWhitespace: true})
	decoded = assertImageResult(t, result, "png", 1, 1)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 0)
}

func TestRenderBackgroundCompositesOnlyBehindSource(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	picture.Set(1, 0, color.NRGBA{R: 255, A: 128})
	picture.Set(2, 0, color.NRGBA{G: 255, A: 255})
	result := renderImage(t, encodeImage(t, "png", picture), artwork.Options{BackgroundColor: "#0000ff"})
	decoded := assertImageResult(t, result, "png", 3, 1)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{B: 255, A: 255}, 0)
	assertColorNear(t, decoded.At(1, 0), color.NRGBA{R: 128, B: 127, A: 255}, 1)
	assertColorNear(t, decoded.At(2, 0), color.NRGBA{G: 255, A: 255}, 0)
	result = renderImage(t, encodeImage(t, "png", picture), artwork.Options{BackgroundColor: "#0000ff80"})
	decoded = assertImageResult(t, result, "png", 3, 1)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{B: 255, A: 128}, 0)
}

func TestRenderForegroundShapesAndOverlayPlacement(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	data := encodeImage(t, "png", picture)
	for _, name := range []string{"play", "music", "folder"} {
		t.Run(name, func(t *testing.T) {
			result := renderImage(t, data, artwork.Options{ForegroundLayer: name + ":#ff0000"})
			decoded := assertImageResult(t, result, "png", 100, 100)
			red, transparent := 0, 0
			for y := 0; y < 100; y++ {
				for x := 0; x < 100; x++ {
					pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					if pixel == (color.NRGBA{R: 255, A: 255}) {
						red++
						if x < 25 || x >= 75 || y < 25 || y >= 75 {
							t.Fatal("foreground escaped its center region")
						}
					} else if pixel.A == 0 {
						transparent++
					} else {
						t.Fatalf("unexpected foreground color: %#v", pixel)
					}
				}
			}
			if red < 200 || transparent < 7500 {
				t.Fatalf("foreground is missing or covers the canvas: red=%d transparent=%d", red, transparent)
			}
		})
	}
	result := renderImage(t, data, artwork.Options{ForegroundLayer: "folder", AddPlayedIndicator: true, UnplayedCount: 12, PercentPlayed: 25})
	decoded := assertImageResult(t, result, "png", 100, 100)
	assertColorNear(t, decoded.At(24, 99), color.NRGBA{R: 42, G: 166, B: 82, A: 255}, 0)
	assertColorNear(t, decoded.At(25, 99), color.NRGBA{A: 180}, 0)
	assertColorNear(t, decoded.At(77, 3), color.NRGBA{R: 42, G: 166, B: 82, A: 245}, 1)
	assertColorNear(t, decoded.At(3, 3), color.NRGBA{R: 25, G: 25, B: 25, A: 220}, 1)
	assertColorNear(t, decoded.At(6, 5), color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 0)
	for _, dimensions := range []image.Point{{1, 1}, {2, 30}, {30, 2}} {
		tiny := image.NewNRGBA(image.Rect(0, 0, dimensions.X, dimensions.Y))
		result := renderImage(t, encodeImage(t, "png", tiny), artwork.Options{ForegroundLayer: "play", AddPlayedIndicator: true, UnplayedCount: 9999, PercentPlayed: 100})
		assertImageResult(t, result, "png", dimensions.X, dimensions.Y)
	}
}

func TestTransformationOptionsCanonicalKeysAndRejection(t *testing.T) {
	first, err := artwork.OptionsKey(artwork.Options{Format: " JPG ", BackgroundColor: "WHITE", ForegroundLayer: " PLAY "})
	if err != nil {
		t.Fatal(err)
	}
	second, err := artwork.OptionsKey(artwork.Options{Format: "jpeg", BackgroundColor: "#ffffffff", ForegroundLayer: "play:#ffffffcc"})
	if err != nil || first != second || !strings.HasPrefix(first, artwork.TransformationVersion+"/") {
		t.Fatalf("noncanonical identity: %q %q %v", first, second, err)
	}
	seen := map[string]bool{}
	for _, options := range []artwork.Options{{}, {CropWhitespace: true}, {AutoOrient: true}, {DisableAnimation: true}, {BackgroundColor: "black"}, {ForegroundLayer: "play"}, {AddPlayedIndicator: true}, {PercentPlayed: 0.1}, {UnplayedCount: 1}, {Crop: artwork.CropRect{X: 1, Y: 2, Width: 3, Height: 4}}} {
		key, err := artwork.OptionsKey(options)
		if err != nil || seen[key] {
			t.Fatalf("options lost from identity: %+v %q %v", options, key, err)
		}
		seen[key] = true
	}
	for _, options := range []artwork.Options{
		{Crop: artwork.CropRect{Width: 1}}, {Crop: artwork.CropRect{X: -1, Width: 1, Height: 1}},
		{PercentPlayed: math.NaN()}, {PercentPlayed: math.Inf(1)}, {PercentPlayed: -1}, {PercentPlayed: 101},
		{UnplayedCount: -1}, {UnplayedCount: 10000}, {BackgroundColor: "#xyzxyz"}, {BackgroundColor: "red"},
		{ForegroundLayer: "https://example.invalid/image.png"}, {ForegroundLayer: "C:\\cover.png"}, {ForegroundLayer: "play:#fff"}, {ForegroundLayer: "data:image/png;base64,abc"},
		{ForegroundLayer: strings.Repeat("play:", 20)}, {BackgroundColor: strings.Repeat(" ", 33)}, {Format: strings.Repeat(" ", 17)},
	} {
		if _, err := artwork.CanonicalOptions(options); !errors.Is(err, artwork.ErrInvalidOptions) {
			t.Fatalf("invalid options accepted: %+v %v", options, err)
		}
	}
	negativeZero, _ := artwork.OptionsKey(artwork.Options{PercentPlayed: math.Copysign(0, -1)})
	zero, _ := artwork.OptionsKey(artwork.Options{})
	if negativeZero != zero {
		t.Fatal("negative zero changed cache identity")
	}
}
