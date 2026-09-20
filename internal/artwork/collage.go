package artwork

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

const collageEdge = 512

var collageBackground = color.RGBA{R: 24, G: 28, B: 36, A: 255}

// Collage renders up to four already selected images in caller-provided order.
// It does not read files, resolve image identities, or perform authorization.
// The result is always an opaque 512 by 512 PNG. Each tile uses a centered cover
// crop, including enlargement of small inputs within this fixed output canvas.
// GIF inputs are fully validated and use their first composited frame.
// Source identifies the generated PNG, so it can itself be passed to Render.
func Collage(ctx context.Context, sources [][]byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(sources) > 4 {
		return Result{}, fmt.Errorf("%w: collages accept at most four sources", ErrLimitExceeded)
	}
	for index, data := range sources {
		if len(data) > maxInputBytes {
			return Result{}, fmt.Errorf("%w: collage source %d exceeds 20 MiB", ErrLimitExceeded, index)
		}
	}
	return withSlot(ctx, func() (Result, error) {
		canvas := image.NewRGBA(image.Rect(0, 0, collageEdge, collageEdge))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(collageBackground), image.Point{}, draw.Src)
		for index, region := range collageRegions(len(sources)) {
			// Keep only one decoded source and its first-frame canvas live at a
			// time. Do not call Render here: this worker already owns a slot.
			if err := paintCollageSource(ctx, canvas, region, sources[index]); err != nil {
				return Result{}, fmt.Errorf("collage source %d: %w", index, err)
			}
		}
		data, err := encodeImage(ctx, canvas, "png", 0)
		if err != nil {
			return Result{}, err
		}
		tag := contentHash(data)
		return Result{
			Source:   Info{Format: "png", MIMEType: "image/png", Tag: tag, Width: collageEdge, Height: collageEdge},
			MIMEType: "image/png", ETag: tag, Width: collageEdge, Height: collageEdge, Bytes: data,
		}, nil
	})
}

func collageRegions(count int) []image.Rectangle {
	half := collageEdge / 2
	switch count {
	case 1:
		return []image.Rectangle{image.Rect(0, 0, collageEdge, collageEdge)}
	case 2:
		return []image.Rectangle{image.Rect(0, 0, half, collageEdge), image.Rect(half, 0, collageEdge, collageEdge)}
	case 3:
		return []image.Rectangle{image.Rect(0, 0, half, collageEdge), image.Rect(half, 0, collageEdge, half), image.Rect(half, half, collageEdge, collageEdge)}
	case 4:
		return []image.Rectangle{image.Rect(0, 0, half, half), image.Rect(half, 0, collageEdge, half), image.Rect(0, half, half, collageEdge), image.Rect(half, half, collageEdge, collageEdge)}
	default:
		return nil
	}
}

func paintCollageSource(ctx context.Context, canvas *image.RGBA, region image.Rectangle, data []byte) error {
	source, err := loadImage(ctx, bytes.NewReader(data))
	if err != nil {
		return err
	}
	decoded := source.image
	if source.animation != nil {
		decoded, err = gifCanvas(ctx, source)
		if err != nil {
			return err
		}
	}
	return paintCollageTile(ctx, canvas, region, decoded)
}

type collageSample struct {
	first, second int
	weight        float64
}

func collageSamples(start, extent float64, sourceSize, outputSize int) []collageSample {
	result := make([]collageSample, outputSize)
	for i := range result {
		position := math.Max(0, math.Min(float64(sourceSize-1), start+(float64(i)+0.5)*extent/float64(outputSize)-0.5))
		first := int(position)
		result[i] = collageSample{first: first, second: min(first+1, sourceSize-1), weight: position - float64(first)}
	}
	return result
}

// Sample directly into the bounded canvas. Scaling an extreme aspect ratio
// before cropping would otherwise allocate a potentially enormous intermediate.
func paintCollageTile(ctx context.Context, canvas *image.RGBA, region image.Rectangle, source image.Image) error {
	bounds := source.Bounds()
	sourceWidth, sourceHeight := float64(bounds.Dx()), float64(bounds.Dy())
	outputWidth, outputHeight := region.Dx(), region.Dy()
	scale := math.Max(float64(outputWidth)/sourceWidth, float64(outputHeight)/sourceHeight)
	visibleWidth, visibleHeight := float64(outputWidth)/scale, float64(outputHeight)/scale
	xSamples := collageSamples((sourceWidth-visibleWidth)/2, visibleWidth, bounds.Dx(), outputWidth)
	ySamples := collageSamples((sourceHeight-visibleHeight)/2, visibleHeight, bounds.Dy(), outputHeight)
	for y, sy := range ySamples {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x, sx := range xSamples {
			r00, g00, b00, a00 := source.At(bounds.Min.X+sx.first, bounds.Min.Y+sy.first).RGBA()
			r10, g10, b10, a10 := source.At(bounds.Min.X+sx.second, bounds.Min.Y+sy.first).RGBA()
			r01, g01, b01, a01 := source.At(bounds.Min.X+sx.first, bounds.Min.Y+sy.second).RGBA()
			r11, g11, b11, a11 := source.At(bounds.Min.X+sx.second, bounds.Min.Y+sy.second).RGBA()
			interpolate := func(v00, v10, v01, v11 uint32) float64 {
				upper := float64(v00)*(1-sx.weight) + float64(v10)*sx.weight
				lower := float64(v01)*(1-sx.weight) + float64(v11)*sx.weight
				return (upper*(1-sy.weight) + lower*sy.weight) / 257
			}
			alpha := interpolate(a00, a10, a01, a11)
			backgroundWeight := (255 - alpha) / 255
			// RGBA returns premultiplied channels. Interpolate those before
			// compositing to avoid colored halos around transparent pixels.
			canvas.SetRGBA(region.Min.X+x, region.Min.Y+y, color.RGBA{
				R: uint8(math.Round(interpolate(r00, r10, r01, r11) + float64(collageBackground.R)*backgroundWeight)),
				G: uint8(math.Round(interpolate(g00, g10, g01, g11) + float64(collageBackground.G)*backgroundWeight)),
				B: uint8(math.Round(interpolate(b00, b10, b01, b11) + float64(collageBackground.B)*backgroundWeight)),
				A: 255,
			})
		}
	}
	return nil
}
