package artwork

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
)

// renderAnimation encodes complete composited frames. Normalizing disposal to
// background makes each output frame independently replace the preceding one,
// including transparent holes, while preserving the input's displayed pixels.
func renderAnimation(ctx context.Context, source loadedImage, options Options) ([]byte, int, int, error) {
	animation := source.animation
	canvas := image.Rect(0, 0, source.info.Width, source.info.Height)
	if int64(canvas.Dx())*int64(canvas.Dy())*int64(len(animation.Image)) > maxGIFPixelTotal {
		return nil, 0, 0, fmt.Errorf("%w: animated composition exceeds 32 Mi full-canvas pixels", ErrLimitExceeded)
	}
	region, err := explicitCrop(canvas, options.Crop)
	if err != nil {
		return nil, 0, 0, err
	}
	if options.CropWhitespace {
		content := image.Rectangle{}
		err = visitGIFFrames(ctx, source, func(_ int, frame image.Image) error {
			bounds, err := contentBounds(ctx, frame, region)
			content = content.Union(bounds)
			return err
		})
		if err != nil {
			return nil, 0, 0, err
		}
		region = nonemptyContent(content, region)
	}
	width, height := outputSize(region.Dx(), region.Dy(), options)
	if int64(width)*int64(height)*int64(len(animation.Image)) > maxGIFPixelTotal {
		return nil, 0, 0, fmt.Errorf("%w: animated output exceeds 32 Mi accumulated pixels", ErrLimitExceeded)
	}
	output := &gif.GIF{
		Delay: append([]int(nil), animation.Delay...), LoopCount: animation.LoopCount,
		Config:          image.Config{Width: width, Height: height},
		BackgroundIndex: 0,
	}
	err = visitGIFFrames(ctx, source, func(_ int, frame image.Image) error {
		transformed, err := transformFrame(ctx, frame, region, width, height, options)
		if err != nil {
			return err
		}
		paletted, err := quantizeGIF(ctx, transformed)
		if err != nil {
			return err
		}
		output.Image = append(output.Image, paletted)
		output.Disposal = append(output.Disposal, gif.DisposalBackground)
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	output.Config.ColorModel = output.Image[0].Palette
	writer := &outputBuffer{ctx: ctx}
	if err := gif.EncodeAll(writer, output); err != nil {
		return nil, 0, 0, fmt.Errorf("encode artwork animation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	return writer.buffer.Bytes(), width, height, nil
}

// visitGIFFrames lends a reusable composited canvas to visit. The callback must
// consume it synchronously and must neither retain nor mutate it.
func visitGIFFrames(ctx context.Context, source loadedImage, visit func(int, image.Image) error) error {
	canvas := image.NewRGBA(image.Rect(0, 0, source.info.Width, source.info.Height))
	if err := fillGIFRegion(ctx, canvas, canvas.Bounds(), source.background); err != nil {
		return err
	}
	var previous *image.RGBA
	for index, frame := range source.animation.Image {
		if err := ctx.Err(); err != nil {
			return err
		}
		disposal := byte(0)
		if index < len(source.animation.Disposal) {
			disposal = source.animation.Disposal[index]
		}
		if disposal > gif.DisposalPrevious {
			return fmt.Errorf("%w: reserved GIF disposal method", ErrInvalidImage)
		}
		if disposal == gif.DisposalPrevious {
			if previous == nil {
				previous = image.NewRGBA(canvas.Bounds())
			}
			if err := copyGIFCanvas(ctx, previous, canvas); err != nil {
				return err
			}
		}
		for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			row := image.Rect(frame.Bounds().Min.X, y, frame.Bounds().Max.X, y+1)
			draw.Draw(canvas, row, frame, row.Min, draw.Over)
		}
		if err := visit(index, canvas); err != nil {
			return err
		}
		switch disposal {
		case gif.DisposalBackground:
			background := source.background
			for _, entry := range frame.Palette {
				if _, _, _, alpha := entry.RGBA(); alpha == 0 {
					background = color.Transparent
					break
				}
			}
			if err := fillGIFRegion(ctx, canvas, frame.Bounds(), background); err != nil {
				return err
			}
		case gif.DisposalPrevious:
			if err := copyGIFCanvas(ctx, canvas, previous); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

func fillGIFRegion(ctx context.Context, canvas *image.RGBA, region image.Rectangle, background color.Color) error {
	for y := region.Min.Y; y < region.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		draw.Draw(canvas, image.Rect(region.Min.X, y, region.Max.X, y+1), image.NewUniform(background), image.Point{}, draw.Src)
	}
	return nil
}

func copyGIFCanvas(ctx context.Context, destination, source *image.RGBA) error {
	for y := 0; y < source.Bounds().Dy(); y++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		copy(destination.Pix[y*destination.Stride:y*destination.Stride+source.Bounds().Dx()*4], source.Pix[y*source.Stride:y*source.Stride+source.Bounds().Dx()*4])
	}
	return nil
}

// checkGIFBudget scans block boundaries without decoding or allocating frames.
// It records at most 1000 per-frame GCE disposal bytes with the correct scope.
// DecodeAll subsequently validates palettes, LZW data, and extension semantics.
func checkGIFBudget(ctx context.Context, data []byte, width, height int, disposals *[]byte) error {
	if len(data) < 13 {
		return fmt.Errorf("%w: truncated GIF header", ErrInvalidImage)
	}
	position := 13
	if data[10]&0x80 != 0 {
		position += 3 * (1 << ((data[10] & 7) + 1))
	}
	frames, pixels := 0, int64(0)
	disposal := byte(0)
	for position < len(data) {
		if err := ctx.Err(); err != nil {
			return err
		}
		block := data[position]
		position++
		switch block {
		case 0x3b:
			if frames == 0 {
				return fmt.Errorf("%w: GIF contains no frames", ErrInvalidImage)
			}
			return nil
		case 0x21:
			if position >= len(data) {
				return fmt.Errorf("%w: truncated GIF extension", ErrInvalidImage)
			}
			label := data[position]
			position++
			if label == 0xf9 {
				if len(data)-position < 6 || data[position] != 4 || data[position+5] != 0 {
					return fmt.Errorf("%w: malformed GIF graphic control extension", ErrInvalidImage)
				}
				disposal = (data[position+1] >> 2) & 7
				if disposal > gif.DisposalPrevious {
					return fmt.Errorf("%w: reserved GIF disposal method", ErrInvalidImage)
				}
			} else if label == 0x01 {
				// The standard library does not render plain-text extensions.
				// Do not silently drop visible input from a claimed full render.
				return fmt.Errorf("%w: GIF plain-text rendering is not supported", ErrUnsupportedFormat)
			}
			var err error
			position, err = skipGIFSubBlocks(ctx, data, position)
			if err != nil {
				return err
			}
		case 0x2c:
			if len(data)-position < 9 {
				return fmt.Errorf("%w: truncated GIF image descriptor", ErrInvalidImage)
			}
			left := int(binary.LittleEndian.Uint16(data[position:]))
			top := int(binary.LittleEndian.Uint16(data[position+2:]))
			frameWidth := int(binary.LittleEndian.Uint16(data[position+4:]))
			frameHeight := int(binary.LittleEndian.Uint16(data[position+6:]))
			packed := data[position+8]
			position += 9
			if frameWidth <= 0 || frameHeight <= 0 || left+frameWidth > width || top+frameHeight > height {
				return fmt.Errorf("%w: GIF frame is outside the logical canvas", ErrInvalidImage)
			}
			frames++
			pixels += int64(frameWidth) * int64(frameHeight)
			if frames > maxGIFFrames || pixels > maxGIFPixelTotal {
				return fmt.Errorf("%w: GIF exceeds frame count or aggregate decoded pixel limits", ErrLimitExceeded)
			}
			*disposals = append(*disposals, disposal)
			disposal = 0
			if packed&0x80 != 0 {
				position += 3 * (1 << ((packed & 7) + 1))
			}
			if position >= len(data) {
				return fmt.Errorf("%w: truncated GIF palette or image data", ErrInvalidImage)
			}
			position++
			var err error
			position, err = skipGIFSubBlocks(ctx, data, position)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: invalid GIF block", ErrInvalidImage)
		}
	}
	return fmt.Errorf("%w: GIF trailer is missing", ErrInvalidImage)
}

func skipGIFSubBlocks(ctx context.Context, data []byte, position int) (int, error) {
	for position < len(data) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		size := int(data[position])
		position++
		if size == 0 {
			return position, nil
		}
		if len(data)-position < size {
			return 0, fmt.Errorf("%w: truncated GIF data sub-block", ErrInvalidImage)
		}
		position += size
	}
	return 0, fmt.Errorf("%w: unterminated GIF data sub-blocks", ErrInvalidImage)
}
