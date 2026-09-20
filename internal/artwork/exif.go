package artwork

import (
	"context"
	"encoding/binary"
	"image"
)

// jpegOrientation reads the primary image's EXIF orientation before JPEG scan
// data. Missing or malformed metadata is ignored so an otherwise valid JPEG
// remains readable. Segment lengths bound all work, and TIFF pointers are never
// followed beyond the first image directory.
func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for position := 2; position < len(data); {
		if data[position] != 0xff {
			return 1
		}
		for position < len(data) && data[position] == 0xff {
			position++
		}
		if position == len(data) {
			return 1
		}
		marker := data[position]
		position++
		switch {
		case marker == 0xda || marker == 0xd9:
			return 1
		case marker == 0x00 || marker == 0xd8:
			return 1
		case marker == 0x01 || marker >= 0xd0 && marker <= 0xd7:
			continue
		}
		if len(data)-position < 2 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(data[position : position+2]))
		if length < 2 || length > len(data)-position {
			return 1
		}
		payload := data[position+2 : position+length]
		if marker == 0xe1 && len(payload) >= 6 &&
			payload[0] == 'E' && payload[1] == 'x' && payload[2] == 'i' &&
			payload[3] == 'f' && payload[4] == 0 && payload[5] == 0 {
			if orientation, ok := tiffOrientation(payload[6:]); ok {
				return orientation
			}
		}
		position += length
	}
	return 1
}

func tiffOrientation(data []byte) (int, bool) {
	if len(data) < 8 {
		return 1, false
	}
	var order binary.ByteOrder
	switch {
	case data[0] == 'I' && data[1] == 'I':
		order = binary.LittleEndian
	case data[0] == 'M' && data[1] == 'M':
		order = binary.BigEndian
	default:
		return 1, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1, false
	}
	// Check the uint32 offset before converting it to an int, including on
	// 32-bit platforms. Every directory includes a count and a next-IFD pointer.
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset > uint64(len(data)) || uint64(len(data))-offset < 6 {
		return 1, false
	}
	position := int(offset)
	entries := int(order.Uint16(data[position : position+2]))
	position += 2
	if entries > (len(data)-position-4)/12 {
		return 1, false
	}
	for index := 0; index < entries; index++ {
		entry := data[position : position+12]
		position += 12
		if order.Uint16(entry[0:2]) != 0x0112 {
			continue
		}
		// EXIF orientation is exactly one SHORT, stored inline in the entry.
		if order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 1, false
		}
		orientation := int(order.Uint16(entry[8:10]))
		return orientation, orientation >= 1 && orientation <= 8
	}
	return 1, false
}

// orientImage applies the eight EXIF coordinate transforms without modifying
// the source. Mirrored orientations are handled directly instead of composing
// transformations, keeping the work to one allocation and one pixel traversal.
func orientImage(ctx context.Context, source image.Image, orientation int) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if orientation <= 1 || orientation > 8 {
		return source, nil
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	outputWidth, outputHeight := width, height
	if orientation >= 5 {
		outputWidth, outputHeight = height, width
	}
	destination := image.NewNRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	for y := 0; y < outputHeight; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < outputWidth; x++ {
			var sourceX, sourceY int
			switch orientation {
			case 2:
				sourceX, sourceY = width-1-x, y
			case 3:
				sourceX, sourceY = width-1-x, height-1-y
			case 4:
				sourceX, sourceY = x, height-1-y
			case 5:
				sourceX, sourceY = y, x
			case 6:
				sourceX, sourceY = y, height-1-x
			case 7:
				sourceX, sourceY = width-1-y, height-1-x
			case 8:
				sourceX, sourceY = width-1-y, x
			}
			destination.Set(x, y, source.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return destination, nil
}
