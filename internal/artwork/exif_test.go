package artwork

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"
	"time"
)

func TestRenderEXIFOrientation(t *testing.T) {
	source := image.NewGray(image.Rect(0, 0, 3, 2))
	for index, gray := range []uint8{20, 60, 100, 140, 180, 220} {
		source.SetGray(index%3, index/3, color.Gray{Y: gray})
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	// Compare against the decoded JPEG so codec rounding cannot make orientation
	// assertions depend on the original fixture's exact grayscale values.
	decodedSource, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	withEXIF := func(tiff []byte) []byte {
		segment := jpegSegmentFixture(0xe1, append([]byte("Exif\x00\x00"), tiff...))
		data := append([]byte(nil), encoded.Bytes()[:2]...)
		data = append(data, segment...)
		return append(data, encoded.Bytes()[2:]...)
	}
	data := withEXIF(exifTIFFFixture(binary.LittleEndian, 6))
	assertPixel := func(t *testing.T, output image.Image, x, y, sourceX, sourceY int) {
		t.Helper()
		got := color.RGBAModel.Convert(output.At(x, y)).(color.RGBA)
		want := color.RGBAModel.Convert(decodedSource.At(sourceX, sourceY)).(color.RGBA)
		if got != want {
			t.Fatalf("pixel (%d,%d) = %v, want source (%d,%d) = %v", x, y, got, sourceX, sourceY, want)
		}
	}
	t.Run("disabled preserves original bytes", func(t *testing.T) {
		result, err := Render(context.Background(), bytes.NewReader(data), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Width != 3 || result.Height != 2 || !bytes.Equal(result.Bytes, data) {
			t.Fatalf("disabled orientation changed original JPEG: size = %dx%d, equal bytes = %t", result.Width, result.Height, bytes.Equal(result.Bytes, data))
		}
	})
	t.Run("orientation changes output dimensions", func(t *testing.T) {
		result, err := Render(context.Background(), bytes.NewReader(data), Options{AutoOrient: true, Format: "png"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Width != 2 || result.Height != 3 || result.Source.Width != 3 || result.Source.Height != 2 {
			t.Fatalf("oriented dimensions = %dx%d, source = %dx%d", result.Width, result.Height, result.Source.Width, result.Source.Height)
		}
		output, err := png.Decode(bytes.NewReader(result.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		if output.Bounds() != image.Rect(0, 0, 2, 3) {
			t.Fatalf("encoded oriented bounds = %v, want 2 by 3", output.Bounds())
		}
		assertPixel(t, output, 0, 0, 0, 1)
		assertPixel(t, output, 1, 2, 2, 0)
	})
	t.Run("crop uses oriented coordinates", func(t *testing.T) {
		result, err := Render(context.Background(), bytes.NewReader(data), Options{
			AutoOrient: true, Format: "png", Crop: CropRect{X: 0, Y: 1, Width: 2, Height: 2},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Width != 2 || result.Height != 2 {
			t.Fatalf("oriented crop dimensions = %dx%d, want 2 by 2", result.Width, result.Height)
		}
		output, err := png.Decode(bytes.NewReader(result.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		if output.Bounds() != image.Rect(0, 0, 2, 2) {
			t.Fatalf("encoded crop bounds = %v, want 2 by 2", output.Bounds())
		}
		// ABC/DEF becomes DA/EB/FC, then the explicit crop keeps EB/FC.
		assertPixel(t, output, 0, 0, 1, 1)
		assertPixel(t, output, 1, 0, 1, 0)
		assertPixel(t, output, 0, 1, 2, 1)
		assertPixel(t, output, 1, 1, 2, 0)
	})
	t.Run("disabled conversion preserves pixel orientation", func(t *testing.T) {
		result, err := Render(context.Background(), bytes.NewReader(data), Options{Format: "png"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Width != 3 || result.Height != 2 {
			t.Fatalf("unoriented dimensions = %dx%d, want 3 by 2", result.Width, result.Height)
		}
		output, err := png.Decode(bytes.NewReader(result.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				assertPixel(t, output, x, y, x, y)
			}
		}
	})
	t.Run("malformed EXIF remains readable", func(t *testing.T) {
		tiff := exifTIFFFixture(binary.BigEndian, 6)
		binary.BigEndian.PutUint32(tiff[4:8], 0xffffffff)
		result, err := Render(context.Background(), bytes.NewReader(withEXIF(tiff)), Options{AutoOrient: true, Format: "png"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Width != 3 || result.Height != 2 {
			t.Fatalf("malformed metadata changed dimensions to %dx%d", result.Width, result.Height)
		}
		output, err := png.Decode(bytes.NewReader(result.Bytes))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				assertPixel(t, output, x, y, x, y)
			}
		}
	})
}

func TestJPEGOrientationByteOrders(t *testing.T) {
	for _, test := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{name: "little endian", order: binary.LittleEndian},
		{name: "big endian", order: binary.BigEndian},
	} {
		for orientation := 1; orientation <= 8; orientation++ {
			t.Run(fmt.Sprintf("%s/%d", test.name, orientation), func(t *testing.T) {
				data := exifJPEGFixture(exifTIFFFixture(test.order, uint16(orientation)))
				if got := jpegOrientation(data); got != orientation {
					t.Fatalf("orientation = %d, want %d", got, orientation)
				}
			})
		}
	}
}

func TestJPEGOrientationSegmentTraversal(t *testing.T) {
	valid := exifJPEGFixture(exifTIFFFixture(binary.LittleEndian, 6))
	xmp := jpegSegmentFixture(0xe1, []byte("http://ns.adobe.com/xap/1.0/\x00metadata"))
	app0 := jpegSegmentFixture(0xe0, []byte("JFIF\x00"))
	missingTag := exifTIFFFixture(binary.LittleEndian, 3)
	binary.LittleEndian.PutUint16(missingTag[10:12], 0x0100)
	malformed := exifTIFFFixture(binary.LittleEndian, 3)
	binary.LittleEndian.PutUint32(malformed[4:8], 0xffffffff)
	for _, test := range []struct {
		name string
		data []byte
		want int
	}{
		{name: "APP0 before EXIF", data: append(append([]byte{0xff, 0xd8}, app0...), valid[2:]...), want: 6},
		{name: "XMP before EXIF", data: append(append([]byte{0xff, 0xd8}, xmp...), valid[2:]...), want: 6},
		{name: "missing tag before EXIF", data: append(exifJPEGFixture(missingTag)[:len(valid)-2], valid[2:]...), want: 6},
		{name: "malformed EXIF before valid EXIF", data: append(exifJPEGFixture(malformed)[:len(valid)-2], valid[2:]...), want: 6},
		{name: "marker padding", data: append([]byte{0xff, 0xd8, 0xff, 0xff}, valid[2:]...), want: 6},
		{name: "standalone markers", data: append([]byte{0xff, 0xd8, 0xff, 0x01, 0xff, 0xd0}, valid[2:]...), want: 6},
		{name: "stop at scan", data: append([]byte{0xff, 0xd8, 0xff, 0xda}, valid[2:]...), want: 1},
		{name: "stop at end", data: append([]byte{0xff, 0xd8, 0xff, 0xd9}, valid[2:]...), want: 1},
		{name: "reject stuffed byte outside scan", data: append([]byte{0xff, 0xd8, 0xff, 0x00}, valid[2:]...), want: 1},
		{name: "reject nested start", data: append([]byte{0xff, 0xd8, 0xff, 0xd8}, valid[2:]...), want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := jpegOrientation(test.data); got != test.want {
				t.Fatalf("orientation = %d, want %d", got, test.want)
			}
		})
	}
}

func TestJPEGOrientationMalformedMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "short TIFF header", mutate: func(data []byte) []byte { return data[:7] }},
		{name: "unknown byte order", mutate: func(data []byte) []byte { data[0] = 'X'; return data }},
		{name: "wrong TIFF magic", mutate: func(data []byte) []byte { data[2] = 43; return data }},
		{name: "IFD in header", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[4:8], 4)
			return data
		}},
		{name: "IFD past end", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)))
			return data
		}},
		{name: "maximum IFD offset", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[4:8], 0xffffffff)
			return data
		}},
		{name: "short entry", mutate: func(data []byte) []byte { return data[:21] }},
		{name: "missing next IFD pointer", mutate: func(data []byte) []byte { return data[:25] }},
		{name: "maximum entry count", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[8:10], 0xffff)
			return data
		}},
		{name: "missing orientation tag", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[10:12], 0x0100)
			return data
		}},
		{name: "orientation is not SHORT", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[12:14], 4)
			return data
		}},
		{name: "empty orientation count", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[14:18], 0)
			return data
		}},
		{name: "multiple orientations", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[14:18], 2)
			return data
		}},
		{name: "maximum orientation count", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint32(data[14:18], 0xffffffff)
			return data
		}},
		{name: "zero orientation", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[18:20], 0)
			return data
		}},
		{name: "orientation above range", mutate: func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[18:20], 9)
			return data
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := exifJPEGFixture(test.mutate(exifTIFFFixture(binary.LittleEndian, 6)))
			if got := jpegOrientation(data); got != 1 {
				t.Fatalf("malformed metadata orientation = %d, want 1", got)
			}
		})
	}
	valid := exifJPEGFixture(exifTIFFFixture(binary.LittleEndian, 6))
	for length := 0; length < len(valid)-2; length++ {
		if got := jpegOrientation(valid[:length]); got != 1 {
			t.Fatalf("truncated prefix of length %d: orientation = %d, want 1", length, got)
		}
	}
	for _, data := range [][]byte{
		{0xff, 0xd8, 0xff},
		{0xff, 0xd8, 0xff, 0xe1, 0, 0},
		{0xff, 0xd8, 0xff, 0xe1, 0, 1},
		{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff},
		{0xff, 0xd8, 0x01, 0x02},
		exifJPEGFixture([]byte("not a TIFF directory")),
	} {
		if got := jpegOrientation(data); got != 1 {
			t.Fatalf("malformed JPEG metadata %x: orientation = %d, want 1", data, got)
		}
	}
}

func TestJPEGOrientationIFDLayout(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		base := exifTIFFFixture(order, 7)
		delayedIFD := make([]byte, len(base)+8)
		copy(delayedIFD, base[:8])
		copy(delayedIFD[16:], base[8:])
		order.PutUint32(delayedIFD[4:8], 16)
		if got := jpegOrientation(exifJPEGFixture(delayedIFD)); got != 7 {
			t.Fatalf("delayed IFD with %T: orientation = %d, want 7", order, got)
		}

		twoEntries := make([]byte, len(base)+12)
		copy(twoEntries, base[:10])
		order.PutUint16(twoEntries[8:10], 2)
		order.PutUint16(twoEntries[10:12], 0x0100)
		copy(twoEntries[22:34], base[10:22])
		// A cyclic next-IFD pointer must not be traversed for primary orientation.
		order.PutUint32(twoEntries[34:38], 8)
		if got := jpegOrientation(exifJPEGFixture(twoEntries)); got != 7 {
			t.Fatalf("second entry with %T: orientation = %d, want 7", order, got)
		}
		order.PutUint16(twoEntries[22:24], 0x0101)
		if got := jpegOrientation(exifJPEGFixture(twoEntries)); got != 1 {
			t.Fatalf("cyclic IFD without orientation with %T: orientation = %d, want 1", order, got)
		}
	}
}

func TestOrientImagePixelMapping(t *testing.T) {
	for _, origin := range []image.Point{{}, {X: -4, Y: 7}} {
		bounds := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(3, 2))}
		source := image.NewNRGBA(bounds)
		colors := map[byte]color.NRGBA{}
		for index, label := range []byte("ABCDEF") {
			pixel := color.NRGBA{R: uint8(index*30 + 20), G: 91, B: 173, A: uint8(index*35 + 40)}
			colors[label] = pixel
			source.SetNRGBA(origin.X+index%3, origin.Y+index/3, pixel)
		}
		for _, test := range []struct {
			orientation int
			rows        []string
		}{
			{orientation: 1, rows: []string{"ABC", "DEF"}},
			{orientation: 2, rows: []string{"CBA", "FED"}},
			{orientation: 3, rows: []string{"FED", "CBA"}},
			{orientation: 4, rows: []string{"DEF", "ABC"}},
			{orientation: 5, rows: []string{"AD", "BE", "CF"}},
			{orientation: 6, rows: []string{"DA", "EB", "FC"}},
			{orientation: 7, rows: []string{"FC", "EB", "DA"}},
			{orientation: 8, rows: []string{"CF", "BE", "AD"}},
		} {
			t.Run(fmt.Sprintf("origin_%d_%d/orientation_%d", origin.X, origin.Y, test.orientation), func(t *testing.T) {
				result, err := orientImage(context.Background(), source, test.orientation)
				if err != nil {
					t.Fatal(err)
				}
				resultBounds := result.Bounds()
				if resultBounds.Dx() != len(test.rows[0]) || resultBounds.Dy() != len(test.rows) {
					t.Fatalf("bounds = %v, want %d by %d", resultBounds, len(test.rows[0]), len(test.rows))
				}
				for y, row := range test.rows {
					for x, label := range []byte(row) {
						got := color.NRGBAModel.Convert(result.At(resultBounds.Min.X+x, resultBounds.Min.Y+y)).(color.NRGBA)
						if got != colors[label] {
							t.Fatalf("pixel (%d,%d) = %v, want %c (%v)", x, y, got, label, colors[label])
						}
					}
				}
				for index, label := range []byte("ABCDEF") {
					if got := source.NRGBAAt(origin.X+index%3, origin.Y+index/3); got != colors[label] {
						t.Fatalf("source pixel %c changed to %v", label, got)
					}
				}
			})
		}
	}
}

func TestOrientImageCancellation(t *testing.T) {
	for _, orientation := range []int{1, 6} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if result, err := orientImage(ctx, image.NewNRGBA(image.Rect(0, 0, 3, 2)), orientation); result != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("already canceled orientation %d: result = %v, error = %v", orientation, result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	source := &orientationBlockingImage{
		Image:   image.NewNRGBA(image.Rect(0, 0, 3, 2)),
		entered: entered,
		release: release,
	}
	type outcome struct {
		image image.Image
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := orientImage(ctx, source, 6)
		done <- outcome{image: result, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("orientation did not begin reading pixels")
	}
	cancel()
	unblock()
	select {
	case result := <-done:
		if result.image != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled during transformation: image = %v, error = %v", result.image, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("orientation did not observe cancellation")
	}
}

type orientationBlockingImage struct {
	image.Image
	entered chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (source *orientationBlockingImage) At(x, y int) color.Color {
	source.once.Do(func() {
		close(source.entered)
		<-source.release
	})
	return source.Image.At(x, y)
}

func exifTIFFFixture(order binary.ByteOrder, orientation uint16) []byte {
	data := make([]byte, 26)
	if order == binary.LittleEndian {
		copy(data[:2], "II")
	} else {
		copy(data[:2], "MM")
	}
	order.PutUint16(data[2:4], 42)
	order.PutUint32(data[4:8], 8)
	order.PutUint16(data[8:10], 1)
	order.PutUint16(data[10:12], 0x0112)
	order.PutUint16(data[12:14], 3)
	order.PutUint32(data[14:18], 1)
	order.PutUint16(data[18:20], orientation)
	return data
}

func exifJPEGFixture(tiff []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), tiff...)
	data := append([]byte{0xff, 0xd8}, jpegSegmentFixture(0xe1, payload)...)
	return append(data, 0xff, 0xd9)
}

func jpegSegmentFixture(marker byte, payload []byte) []byte {
	data := make([]byte, len(payload)+4)
	data[0], data[1] = 0xff, marker
	binary.BigEndian.PutUint16(data[2:4], uint16(len(payload)+2))
	copy(data[4:], payload)
	return data
}
