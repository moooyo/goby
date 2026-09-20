package artwork

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"
)

func decorateFrame(ctx context.Context, source image.Image, options Options) (image.Image, error) {
	if options.BackgroundColor == "" && options.ForegroundLayer == "" && !options.AddPlayedIndicator && options.PercentPlayed == 0 && options.UnplayedCount == 0 {
		return source, nil
	}
	bounds := source.Bounds()
	output := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	var background color.NRGBA
	if options.BackgroundColor != "" {
		_, background, _ = parseLayerColor(options.BackgroundColor)
	}
	for y := 0; y < bounds.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := image.Rect(0, y, bounds.Dx(), y+1)
		if options.BackgroundColor != "" {
			draw.Draw(output, row, image.NewUniform(background), image.Point{}, draw.Src)
			draw.Draw(output, row, source, bounds.Min.Add(image.Pt(0, y)), draw.Over)
		} else {
			draw.Draw(output, row, source, bounds.Min.Add(image.Pt(0, y)), draw.Src)
		}
	}
	edge := min(bounds.Dx(), bounds.Dy())
	if options.ForegroundLayer != "" {
		parts := strings.SplitN(options.ForegroundLayer, ":", 2)
		_, ink, _ := parseLayerColor(parts[1])
		size := max(1, edge/2)
		region := image.Rect((bounds.Dx()-size)/2, (bounds.Dy()-size)/2, (bounds.Dx()+size)/2, (bounds.Dy()+size)/2)
		if err := paintShape(ctx, output, region, ink, func(x, y int) bool {
			return foregroundPixel(parts[0], x*1000/size, y*1000/size)
		}); err != nil {
			return nil, err
		}
	}
	margin := max(1, edge/32)
	if options.UnplayedCount != 0 {
		text := strconv.Itoa(options.UnplayedCount)
		scale := max(1, edge/80)
		width, height := (len(text)*4-1)*scale+4*scale, 9*scale
		badge := image.Rect(margin, margin, margin+width, margin+height)
		if err := paintShape(ctx, output, badge, color.NRGBA{R: 25, G: 25, B: 25, A: 220}, nil); err != nil {
			return nil, err
		}
		for index, digit := range text {
			glyph := digitPixels[digit-'0']
			region := image.Rect(margin+(2+index*4)*scale, margin+2*scale, margin+(5+index*4)*scale, margin+7*scale)
			if err := paintShape(ctx, output, region, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, func(x, y int) bool {
				return glyph[y/scale]&(1<<uint(2-x/scale)) != 0
			}); err != nil {
				return nil, err
			}
		}
	}
	if options.AddPlayedIndicator {
		size := max(5, edge/5)
		region := image.Rect(bounds.Dx()-margin-size, margin, bounds.Dx()-margin, margin+size)
		if err := paintShape(ctx, output, region, color.NRGBA{R: 42, G: 166, B: 82, A: 245}, nil); err != nil {
			return nil, err
		}
		if err := paintShape(ctx, output, region, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, func(x, y int) bool {
			xn, yn := x*1000/size, y*1000/size
			return xn >= 150 && xn <= 440 && abs(yn-(xn+250)) <= 110 || xn > 440 && xn <= 850 && abs(yn-(1130-xn)) <= 110
		}); err != nil {
			return nil, err
		}
	}
	if options.PercentPlayed > 0 {
		height := max(1, bounds.Dy()/24)
		region := image.Rect(0, bounds.Dy()-height, bounds.Dx(), bounds.Dy())
		if err := paintShape(ctx, output, region, color.NRGBA{A: 180}, nil); err != nil {
			return nil, err
		}
		region.Max.X = min(bounds.Dx(), max(1, int(math.Ceil(float64(bounds.Dx())*options.PercentPlayed/100))))
		if err := paintShape(ctx, output, region, color.NRGBA{R: 42, G: 166, B: 82, A: 255}, nil); err != nil {
			return nil, err
		}
	}
	return output, nil
}

func foregroundPixel(name string, x, y int) bool {
	switch name {
	case "play":
		return x >= 180 && x <= 820 && abs(y-500) <= (820-x)*3/4
	case "music":
		return x >= 330 && x <= 430 && y >= 150 && y <= 700 ||
			x >= 730 && x <= 830 && y >= 80 && y <= 630 ||
			x >= 330 && x <= 830 && y >= 100-(x-330)/8 && y <= 240-(x-330)/8 ||
			(x-260)*(x-260)+(y-720)*(y-720) <= 150*150 ||
			(x-660)*(x-660)+(y-650)*(y-650) <= 150*150
	case "folder":
		return x >= 80 && x <= 920 && y >= 320 && y <= 850 ||
			x >= 80 && x <= 430 && y >= 180 && y <= 320
	}
	return false
}

// paintShape clips before drawing but supplies coordinates relative to the
// requested shape. Tiny images therefore remain bounded and deterministic.
func paintShape(ctx context.Context, destination *image.RGBA, region image.Rectangle, ink color.NRGBA, mask func(int, int) bool) error {
	clipped := region.Intersect(destination.Bounds())
	uniform := image.NewUniform(ink)
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for x := clipped.Min.X; x < clipped.Max.X; x++ {
			if mask == nil || mask(x-region.Min.X, y-region.Min.Y) {
				draw.Draw(destination, image.Rect(x, y, x+1, y+1), uniform, image.Point{}, draw.Over)
			}
		}
	}
	return nil
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

var digitPixels = [10][5]byte{
	{7, 5, 5, 5, 7}, {2, 6, 2, 2, 7}, {7, 1, 7, 4, 7}, {7, 1, 7, 1, 7}, {5, 5, 7, 1, 1},
	{7, 4, 7, 1, 7}, {7, 4, 7, 5, 7}, {7, 1, 2, 2, 2}, {7, 5, 7, 5, 7}, {7, 5, 7, 1, 7},
}
