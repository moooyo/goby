// Package artwork validates and renders bounded JPEG, PNG, and GIF images.
//
// All dimensions are aspect-preserving bounding limits; small inputs are never
// enlarged. Output dimensions cannot exceed 4096 pixels per side or 16 Mi pixels.
// Input is limited to 20 MiB, 25 Mi pixels, and 16384 pixels per side. GIF input
// additionally allows at most 1000 frames and 32 Mi decoded pixels in total.
// Unchanged output preserves the validated original bytes, including animation.
// Resized or converted GIF output contains only the first composited frame.
// Quality affects JPEG output only: zero selects 85, and 1 through 100 are valid.
// JPEG output composites transparency onto white. Transformed GIF output uses
// binary transparency, with alpha below 128 transparent and other pixels opaque.
package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"
)

const (
	maxInputBytes    = 20 << 20
	maxInputPixels   = 25 << 20
	maxInputEdge     = 16384
	maxOutputPixels  = 16 << 20
	maxOutputEdge    = 4096
	maxOutputBytes   = 80 << 20
	defaultQuality   = 85
	maxGIFFrames     = 1000
	maxGIFPixelTotal = 32 << 20
)

var (
	ErrInvalidOptions    = errors.New("invalid artwork options")
	ErrUnsupportedFormat = errors.New("unsupported artwork format")
	ErrInvalidImage      = errors.New("invalid artwork data")
	ErrLimitExceeded     = errors.New("artwork resource limit exceeded")
	decodeSlots          = make(chan struct{}, 2)
)

type Info struct {
	Format   string
	MIMEType string
	Tag      string
	Width    int
	Height   int
}

type Options struct {
	Format    string
	Width     int
	Height    int
	MaxWidth  int
	MaxHeight int
	Quality   int
}

type Result struct {
	Source   Info
	MIMEType string
	ETag     string
	Width    int
	Height   int
	Bytes    []byte
}

type loadedImage struct {
	info       Info
	data       []byte
	image      image.Image
	background color.Color
}

type jobResult[T any] struct {
	value T
	err   error
}

// withSlot keeps the slot until the worker exits, even when cancellation has
// already returned to its caller. Consequently, arbitrary blocking readers
// cannot create unlimited abandoned decode jobs. The reader remains caller-owned.
func withSlot[T any](ctx context.Context, work func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	select {
	case decodeSlots <- struct{}{}:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	result := make(chan jobResult[T], 1)
	go func() {
		defer func() { <-decodeSlots }()
		if err := ctx.Err(); err != nil {
			result <- jobResult[T]{err: err}
			return
		}
		value, err := work()
		result <- jobResult[T]{value: value, err: err}
	}()
	select {
	case outcome := <-result:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return outcome.value, outcome.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// Inspect fully validates the image, including every GIF frame, before returning
// metadata. It shares the two decode slots with Render and does not close reader.
func Inspect(reader io.Reader) (Info, error) {
	return InspectContext(context.Background(), reader)
}

// InspectContext is the cancellable form of Inspect. Canceled workers retain
// their shared slot until any already active blocking Read or decoder exits.
func InspectContext(ctx context.Context, reader io.Reader) (Info, error) {
	return withSlot(ctx, func() (Info, error) {
		source, err := loadImage(ctx, reader)
		return source.info, err
	})
}

// Render returns promptly when ctx is canceled, including while queued. A
// currently blocked Read is not closed forcibly: its worker keeps its slot until
// the Read returns. Decoding uses bounded standard-library decoders; reading,
// row conversion/resampling, and encoding writes also check cancellation.
func Render(ctx context.Context, reader io.Reader, options Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	format, err := validateOptions(options)
	if err != nil {
		return Result{}, err
	}
	return withSlot(ctx, func() (Result, error) {
		source, err := loadImage(ctx, reader)
		if err != nil {
			return Result{}, err
		}
		if format == "" {
			format = source.info.Format
		}
		width, height := outputSize(source.info.Width, source.info.Height, options)
		result := Result{Source: source.info, MIMEType: mimeType(format), Width: width, Height: height}
		if width == source.info.Width && height == source.info.Height && format == source.info.Format &&
			(format != "jpeg" || options.Quality == 0) {
			result.Bytes = source.data
			result.ETag = source.info.Tag
			return result, nil
		}
		decoded := source.image
		if source.info.Format == "gif" {
			decoded, err = gifCanvas(ctx, source)
			if err != nil {
				return Result{}, err
			}
		}
		if width != source.info.Width || height != source.info.Height {
			decoded, err = resizeBilinear(ctx, decoded, width, height)
			if err != nil {
				return Result{}, err
			}
		}
		quality := options.Quality
		if quality == 0 {
			quality = defaultQuality
		}
		encoded, err := encodeImage(ctx, decoded, format, quality)
		if err != nil {
			return Result{}, err
		}
		result.Bytes = encoded
		result.ETag = contentHash(encoded)
		return result, nil
	})
}

func validateOptions(options Options) (string, error) {
	for name, size := range map[string]int{"width": options.Width, "height": options.Height,
		"max width": options.MaxWidth, "max height": options.MaxHeight} {
		if size < 0 || size > maxOutputEdge {
			return "", fmt.Errorf("%w: %s must be between 0 and %d", ErrInvalidOptions, name, maxOutputEdge)
		}
	}
	if options.Quality < 0 || options.Quality > 100 {
		return "", fmt.Errorf("%w: quality must be between 0 and 100", ErrInvalidOptions)
	}
	format := strings.ToLower(strings.TrimSpace(options.Format))
	switch format {
	case "", "original":
		return "", nil
	case "jpg", "jpeg":
		return "jpeg", nil
	case "png", "gif":
		return format, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedFormat, options.Format)
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(data)
	if canceled := r.ctx.Err(); canceled != nil {
		return 0, canceled
	}
	return n, err
}

func loadImage(ctx context.Context, reader io.Reader) (loadedImage, error) {
	if reader == nil {
		return loadedImage{}, fmt.Errorf("%w: reader is nil", ErrInvalidImage)
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: reader}, maxInputBytes+1))
	if err != nil {
		return loadedImage{}, fmt.Errorf("read artwork: %w", err)
	}
	if len(data) > maxInputBytes {
		return loadedImage{}, fmt.Errorf("%w: input exceeds 20 MiB", ErrLimitExceeded)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return loadedImage{}, fmt.Errorf("%w: only JPEG, PNG, and GIF input is accepted", ErrUnsupportedFormat)
		}
		return loadedImage{}, fmt.Errorf("%w: decode configuration: %v", ErrInvalidImage, err)
	}
	if format != "jpeg" && format != "png" && format != "gif" {
		return loadedImage{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return loadedImage{}, fmt.Errorf("%w: dimensions must be positive", ErrInvalidImage)
	}
	if config.Width > maxInputEdge || config.Height > maxInputEdge || int64(config.Width)*int64(config.Height) > maxInputPixels {
		return loadedImage{}, fmt.Errorf("%w: input exceeds dimension or pixel limits", ErrLimitExceeded)
	}
	if err := ctx.Err(); err != nil {
		return loadedImage{}, err
	}
	source := loadedImage{
		info: Info{Format: format, MIMEType: mimeType(format), Tag: contentHash(data), Width: config.Width, Height: config.Height},
		data: data,
	}
	if format == "gif" {
		if err := checkGIFBudget(ctx, data, config.Width, config.Height); err != nil {
			return loadedImage{}, err
		}
		animation, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return loadedImage{}, fmt.Errorf("%w: decode GIF: %v", ErrInvalidImage, err)
		}
		if len(animation.Image) == 0 {
			return loadedImage{}, fmt.Errorf("%w: GIF contains no frames", ErrInvalidImage)
		}
		source.image = animation.Image[0]
		source.background = color.Transparent
		if palette, ok := animation.Config.ColorModel.(color.Palette); ok && int(animation.BackgroundIndex) < len(palette) {
			source.background = palette[animation.BackgroundIndex]
		}
		// Transparent GIFs are rendered onto a transparent initial canvas,
		// even when their background and transparent palette indexes differ.
		for _, entry := range animation.Image[0].Palette {
			_, _, _, alpha := entry.RGBA()
			if alpha == 0 {
				source.background = color.Transparent
				break
			}
		}
	} else {
		source.image, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return loadedImage{}, fmt.Errorf("%w: decode image: %v", ErrInvalidImage, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return loadedImage{}, err
	}
	return source, nil
}

func mimeType(format string) string {
	if format == "jpeg" {
		return "image/jpeg"
	}
	return "image/" + format
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func outputSize(width, height int, options Options) (int, int) {
	scale := 1.0
	for _, bound := range []int{options.Width, options.MaxWidth, maxOutputEdge} {
		if bound > 0 {
			scale = math.Min(scale, float64(bound)/float64(width))
		}
	}
	for _, bound := range []int{options.Height, options.MaxHeight, maxOutputEdge} {
		if bound > 0 {
			scale = math.Min(scale, float64(bound)/float64(height))
		}
	}
	scale = math.Min(scale, math.Sqrt(float64(maxOutputPixels)/(float64(width)*float64(height))))
	return max(1, int(math.Floor(float64(width)*scale+1e-9))), max(1, int(math.Floor(float64(height)*scale+1e-9)))
}

func gifCanvas(ctx context.Context, source loadedImage) (image.Image, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, source.info.Width, source.info.Height))
	background := image.NewUniform(source.background)
	for y := 0; y < source.info.Height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := image.Rect(0, y, source.info.Width, y+1)
		draw.Draw(canvas, row, background, image.Point{}, draw.Src)
		row = row.Intersect(source.image.Bounds())
		if !row.Empty() {
			draw.Draw(canvas, row, source.image, row.Min, draw.Over)
		}
	}
	return canvas, nil
}

func rgbaImage(ctx context.Context, source image.Image) (*image.RGBA, error) {
	bounds := source.Bounds()
	if rgba, ok := source.(*image.RGBA); ok && bounds.Min == (image.Point{}) {
		return rgba, nil
	}
	destination := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		draw.Draw(destination, image.Rect(0, y, bounds.Dx(), y+1), source, bounds.Min.Add(image.Pt(0, y)), draw.Src)
	}
	return destination, nil
}

func resizeBilinear(ctx context.Context, source image.Image, width, height int) (image.Image, error) {
	input, err := rgbaImage(ctx, source)
	if err != nil {
		return nil, err
	}
	destination := image.NewRGBA(image.Rect(0, 0, width, height))
	sourceWidth, sourceHeight := input.Bounds().Dx(), input.Bounds().Dy()
	type sample struct {
		first, second int
		weight        float64
	}
	xSamples := make([]sample, width)
	for x := range width {
		position := math.Max(0, math.Min(float64(sourceWidth-1), (float64(x)+0.5)*float64(sourceWidth)/float64(width)-0.5))
		first := int(position)
		xSamples[x] = sample{first: first * 4, second: min(first+1, sourceWidth-1) * 4, weight: position - float64(first)}
	}
	for y := range height {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		position := math.Max(0, math.Min(float64(sourceHeight-1), (float64(y)+0.5)*float64(sourceHeight)/float64(height)-0.5))
		first := int(position)
		weight := position - float64(first)
		top, bottom := first*input.Stride, min(first+1, sourceHeight-1)*input.Stride
		for x, sample := range xSamples {
			for channel := range 4 {
				upper := float64(input.Pix[top+sample.first+channel])*(1-sample.weight) + float64(input.Pix[top+sample.second+channel])*sample.weight
				lower := float64(input.Pix[bottom+sample.first+channel])*(1-sample.weight) + float64(input.Pix[bottom+sample.second+channel])*sample.weight
				destination.Pix[y*destination.Stride+x*4+channel] = uint8(math.Round(upper*(1-weight) + lower*weight))
			}
		}
	}
	return destination, nil
}

type outputBuffer struct {
	buffer bytes.Buffer
	ctx    context.Context
}

func (w *outputBuffer) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) > maxOutputBytes-w.buffer.Len() {
		return 0, fmt.Errorf("%w: encoded output exceeds 80 MiB", ErrLimitExceeded)
	}
	return w.buffer.Write(data)
}

func encodeImage(ctx context.Context, source image.Image, format string, quality int) ([]byte, error) {
	writer := &outputBuffer{ctx: ctx}
	var err error
	switch format {
	case "png":
		err = png.Encode(writer, source)
	case "jpeg":
		var opaque *image.RGBA
		opaque, err = whiteBackground(ctx, source)
		if err == nil {
			err = jpeg.Encode(writer, opaque, &jpeg.Options{Quality: quality})
		}
	case "gif":
		var paletted *image.Paletted
		paletted, err = quantizeGIF(ctx, source)
		if err == nil {
			err = gif.Encode(writer, paletted, nil)
		}
	default:
		return nil, ErrUnsupportedFormat
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, fmt.Errorf("encode artwork: %w", err)
	}
	return writer.buffer.Bytes(), nil
}

func whiteBackground(ctx context.Context, source image.Image) (*image.RGBA, error) {
	input, err := rgbaImage(ctx, source)
	if err != nil {
		return nil, err
	}
	destination := image.NewRGBA(input.Bounds())
	for y := 0; y < input.Bounds().Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < input.Bounds().Dx(); x++ {
			position := y*input.Stride + x*4
			destinationPosition := y*destination.Stride + x*4
			alpha := input.Pix[position+3]
			for channel := range 3 {
				destination.Pix[destinationPosition+channel] = uint8(min(255, int(input.Pix[position+channel])+255-int(alpha)))
			}
			destination.Pix[destinationPosition+3] = 255
		}
	}
	return destination, nil
}

// A fixed color cube makes quantization O(pixels), avoiding a palette search
// for every output pixel. Index zero is reserved for binary transparency.
func quantizeGIF(ctx context.Context, source image.Image) (*image.Paletted, error) {
	palette := make(color.Palette, 217)
	palette[0] = color.Transparent
	for red := 0; red < 6; red++ {
		for green := 0; green < 6; green++ {
			for blue := 0; blue < 6; blue++ {
				palette[1+red*36+green*6+blue] = color.RGBA{R: uint8(red * 51), G: uint8(green * 51), B: uint8(blue * 51), A: 255}
			}
		}
	}
	bounds := source.Bounds()
	destination := image.NewPaletted(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), palette)
	for y := 0; y < bounds.Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < bounds.Dx(); x++ {
			pixel := color.NRGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			if pixel.A < 128 {
				continue
			}
			red, green, blue := (int(pixel.R)+25)/51, (int(pixel.G)+25)/51, (int(pixel.B)+25)/51
			destination.Pix[y*destination.Stride+x] = uint8(1 + red*36 + green*6 + blue)
		}
	}
	return destination, nil
}
