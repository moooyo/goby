package artwork

import (
	"context"
	"encoding/binary"
	"fmt"
)

// checkGIFBudget scans block boundaries without decoding or allocating frames.
// DecodeAll subsequently validates palettes, LZW data, and extension semantics.
func checkGIFBudget(ctx context.Context, data []byte, width, height int) error {
	if len(data) < 13 {
		return fmt.Errorf("%w: truncated GIF header", ErrInvalidImage)
	}
	position := 13
	if data[10]&0x80 != 0 {
		position += 3 * (1 << ((data[10] & 7) + 1))
	}
	frames, pixels := 0, int64(0)
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
			position++
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
