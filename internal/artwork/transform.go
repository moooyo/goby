package artwork

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// TransformationVersion identifies the pixel pipeline and must participate in
// derivative identities whenever its rendering semantics change.
const TransformationVersion = "artwork-v2"

// CropRect is expressed in pixels after optional EXIF orientation, before
// whitespace removal and scaling. The zero value leaves the canvas unchanged.
type CropRect struct {
	X, Y, Width, Height int
}

// CanonicalOptions validates and normalizes every transformation. It does not
// need an input image. A crop extending outside the actual image is rejected by
// Render. ForegroundLayer accepts play, music, or folder, optionally followed by
// a colon and a color. It never interprets paths, URLs, or encoded image data.
func CanonicalOptions(options Options) (Options, error) {
	if len(options.Format) > 16 || len(options.BackgroundColor) > 32 || len(options.ForegroundLayer) > 64 {
		return Options{}, fmt.Errorf("%w: textual transformation option exceeds its size limit", ErrInvalidOptions)
	}
	format, err := validateOptions(options)
	if err != nil {
		return Options{}, err
	}
	options.Format = format
	if options.Crop != (CropRect{}) {
		crop := options.Crop
		if crop.X < 0 || crop.Y < 0 || crop.Width <= 0 || crop.Height <= 0 ||
			crop.X > maxInputEdge || crop.Y > maxInputEdge || crop.Width > maxInputEdge || crop.Height > maxInputEdge {
			return Options{}, fmt.Errorf("%w: crop must have nonnegative coordinates and positive bounded dimensions", ErrInvalidOptions)
		}
	}
	if math.IsNaN(options.PercentPlayed) || math.IsInf(options.PercentPlayed, 0) || options.PercentPlayed < 0 || options.PercentPlayed > 100 {
		return Options{}, fmt.Errorf("%w: percent played must be between 0 and 100", ErrInvalidOptions)
	}
	if options.PercentPlayed == 0 {
		options.PercentPlayed = 0 // Normalize negative zero in cache identities.
	}
	if options.UnplayedCount < 0 || options.UnplayedCount > 9999 {
		return Options{}, fmt.Errorf("%w: unplayed count must be between 0 and 9999", ErrInvalidOptions)
	}
	if options.BackgroundColor != "" {
		options.BackgroundColor, _, err = parseLayerColor(options.BackgroundColor)
		if err != nil {
			return Options{}, err
		}
	}
	if options.ForegroundLayer != "" {
		parts := strings.Split(strings.ToLower(strings.TrimSpace(options.ForegroundLayer)), ":")
		if len(parts) > 2 || (parts[0] != "play" && parts[0] != "music" && parts[0] != "folder") {
			return Options{}, fmt.Errorf("%w: foreground must be play, music, or folder with an optional color", ErrInvalidOptions)
		}
		layerColor := "#ffffffcc"
		if len(parts) == 2 {
			layerColor, _, err = parseLayerColor(parts[1])
			if err != nil {
				return Options{}, err
			}
		}
		options.ForegroundLayer = parts[0] + ":" + layerColor
	}
	return options, nil
}

// OptionsKey is a stable, delimiter-safe cache identity for canonical options.
// Source identities and current authorization must be checked independently.
func OptionsKey(options Options) (string, error) {
	o, err := CanonicalOptions(options)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%d/%d/%d/%d/%d/%d,%d,%d,%d/%t/%t/%t/%s/%s/%t/%s/%d",
		TransformationVersion, o.Format, o.Width, o.Height, o.MaxWidth, o.MaxHeight, o.Quality,
		o.Crop.X, o.Crop.Y, o.Crop.Width, o.Crop.Height, o.CropWhitespace, o.AutoOrient, o.DisableAnimation,
		o.BackgroundColor, o.ForegroundLayer, o.AddPlayedIndicator, strconv.FormatFloat(o.PercentPlayed, 'g', -1, 64), o.UnplayedCount), nil
}

func hasTransform(options Options) bool {
	return options.Crop != (CropRect{}) || options.CropWhitespace || options.AutoOrient || options.DisableAnimation ||
		options.BackgroundColor != "" || options.ForegroundLayer != "" || options.AddPlayedIndicator || options.PercentPlayed != 0 || options.UnplayedCount != 0
}

func parseLayerColor(raw string) (string, color.NRGBA, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "transparent":
		value = "#00000000"
	case "black":
		value = "#000000ff"
	case "white":
		value = "#ffffffff"
	}
	if len(value) != 7 && len(value) != 9 || !strings.HasPrefix(value, "#") {
		return "", color.NRGBA{}, fmt.Errorf("%w: color must be #RRGGBB, #RRGGBBAA, black, white, or transparent", ErrInvalidOptions)
	}
	if len(value) == 7 {
		value += "ff"
	}
	packed, err := strconv.ParseUint(value[1:], 16, 32)
	if err != nil {
		return "", color.NRGBA{}, fmt.Errorf("%w: color contains invalid hexadecimal digits", ErrInvalidOptions)
	}
	return value, color.NRGBA{R: uint8(packed >> 24), G: uint8(packed >> 16), B: uint8(packed >> 8), A: uint8(packed)}, nil
}

func renderLoaded(ctx context.Context, source loadedImage, options Options, format string) ([]byte, int, int, error) {
	if source.animation != nil && format == "gif" && !options.DisableAnimation {
		return renderAnimation(ctx, source, options)
	}
	decoded := source.image
	var err error
	if source.animation != nil {
		decoded, err = gifCanvas(ctx, source)
	} else if options.AutoOrient && source.info.Format == "jpeg" {
		decoded, err = orientImage(ctx, decoded, jpegOrientation(source.data))
	}
	if err != nil {
		return nil, 0, 0, err
	}
	region, err := explicitCrop(decoded.Bounds(), options.Crop)
	if err != nil {
		return nil, 0, 0, err
	}
	if options.CropWhitespace {
		content, err := contentBounds(ctx, decoded, region)
		if err != nil {
			return nil, 0, 0, err
		}
		region = nonemptyContent(content, region)
	}
	width, height := outputSize(region.Dx(), region.Dy(), options)
	decoded, err = transformFrame(ctx, decoded, region, width, height, options)
	if err != nil {
		return nil, 0, 0, err
	}
	quality := options.Quality
	if quality == 0 {
		quality = defaultQuality
	}
	data, err := encodeImage(ctx, decoded, format, quality)
	return data, width, height, err
}

func explicitCrop(bounds image.Rectangle, crop CropRect) (image.Rectangle, error) {
	if crop == (CropRect{}) {
		return bounds, nil
	}
	region := image.Rect(bounds.Min.X+crop.X, bounds.Min.Y+crop.Y, bounds.Min.X+crop.X+crop.Width, bounds.Min.Y+crop.Y+crop.Height)
	if !region.In(bounds) {
		return image.Rectangle{}, fmt.Errorf("%w: crop extends outside the oriented canvas", ErrInvalidOptions)
	}
	return region, nil
}

// contentBounds removes only transparent or near-white outer margins. Interior
// white pixels remain intact. For animation the caller unions every displayed
// frame's content before choosing a single stable crop rectangle.
func contentBounds(ctx context.Context, source image.Image, region image.Rectangle) (image.Rectangle, error) {
	result := image.Rectangle{}
	for y := region.Min.Y; y < region.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return image.Rectangle{}, err
		}
		for x := region.Min.X; x < region.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			if pixel.A <= 8 || pixel.R >= 245 && pixel.G >= 245 && pixel.B >= 245 {
				continue
			}
			point := image.Rect(x, y, x+1, y+1)
			result = result.Union(point)
		}
	}
	return result, nil
}

func nonemptyContent(content, region image.Rectangle) image.Rectangle {
	if content.Empty() {
		return image.Rect(region.Min.X, region.Min.Y, region.Min.X+1, region.Min.Y+1)
	}
	return content
}

type croppedImage struct {
	image.Image
	region image.Rectangle
}

func (i croppedImage) Bounds() image.Rectangle { return image.Rect(0, 0, i.region.Dx(), i.region.Dy()) }
func (i croppedImage) At(x, y int) color.Color { return i.Image.At(i.region.Min.X+x, i.region.Min.Y+y) }

func transformFrame(ctx context.Context, source image.Image, region image.Rectangle, width, height int, options Options) (image.Image, error) {
	var output image.Image = croppedImage{Image: source, region: region}
	if region == source.Bounds() && region.Min == (image.Point{}) {
		output = source
	}
	var err error
	if region.Dx() != width || region.Dy() != height {
		output, err = resizeBilinear(ctx, output, width, height)
		if err != nil {
			return nil, err
		}
	}
	return decorateFrame(ctx, output, options)
}
