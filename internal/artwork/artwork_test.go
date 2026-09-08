package artwork_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

func TestInspectSupportedFormats(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "gif"} {
		t.Run(format, func(t *testing.T) {
			data := encodeImage(t, format, sampleImage(12, 8))
			info, err := artwork.Inspect(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if info.Format != format || info.MIMEType != "image/"+format || info.Width != 12 || info.Height != 8 {
				t.Fatalf("incorrect image metadata: %+v", info)
			}
			if info.Tag != digest(data) {
				t.Fatalf("source tag = %q, want the SHA-256 digest of the source bytes", info.Tag)
			}
		})
	}
}

func TestRenderPreservesUnchangedSource(t *testing.T) {
	for _, format := range []string{"jpeg", "png", "gif"} {
		t.Run(format, func(t *testing.T) {
			data := encodeImage(t, format, sampleImage(12, 8))
			for _, outputFormat := range []string{"", "original", format} {
				result := renderImage(t, data, artwork.Options{Format: outputFormat, Width: 120, Height: 80})
				if !bytes.Equal(result.Bytes, data) {
					t.Fatalf("format %q unnecessarily changed the original bytes", outputFormat)
				}
				assertImageResult(t, result, format, 12, 8)
				if result.Source.Tag != digest(data) || result.ETag != result.Source.Tag {
					t.Fatalf("unchanged source has incorrect source or output tag: %+v", result)
				}
			}
		})
	}
}

func TestRenderConvertsSupportedFormats(t *testing.T) {
	for _, sourceFormat := range []string{"jpeg", "png", "gif"} {
		for _, targetFormat := range []string{"jpeg", "png", "gif"} {
			t.Run(sourceFormat+"_to_"+targetFormat, func(t *testing.T) {
				data := encodeImage(t, sourceFormat, sampleImage(12, 8))
				result := renderImage(t, data, artwork.Options{Format: targetFormat})
				assertImageResult(t, result, targetFormat, 12, 8)
				if result.Source.Format != sourceFormat || result.Source.MIMEType != "image/"+sourceFormat || result.Source.Tag != digest(data) {
					t.Fatalf("conversion lost source metadata: %+v", result.Source)
				}
			})
		}
	}
	data := encodeImage(t, "png", sampleImage(12, 8))
	assertImageResult(t, renderImage(t, data, artwork.Options{Format: "jpg"}), "jpeg", 12, 8)
}

func TestRenderBoundsKeepAspectRatioWithoutUpscaling(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(120, 80))
	for _, test := range []struct {
		name    string
		options artwork.Options
		width   int
		height  int
	}{
		{"width", artwork.Options{Width: 60}, 60, 40},
		{"height", artwork.Options{Height: 20}, 30, 20},
		{"both", artwork.Options{Width: 90, Height: 40}, 60, 40},
		{"maximum width", artwork.Options{MaxWidth: 30}, 30, 20},
		{"maximum height", artwork.Options{MaxHeight: 40}, 60, 40},
		{"combined bounds", artwork.Options{Width: 90, Height: 60, MaxWidth: 60, MaxHeight: 20}, 30, 20},
		{"large requested size", artwork.Options{Width: 240, Height: 160}, 120, 80},
		{"large maximum size", artwork.Options{MaxWidth: 240, MaxHeight: 160}, 120, 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := renderImage(t, data, test.options)
			assertImageResult(t, result, "png", test.width, test.height)
			if result.Source.Width != 120 || result.Source.Height != 80 {
				t.Fatalf("resizing overwrote source dimensions: %+v", result.Source)
			}
		})
	}

	portrait := encodeImage(t, "png", sampleImage(80, 120))
	assertImageResult(t, renderImage(t, portrait, artwork.Options{Width: 40, Height: 90}), "png", 40, 60)
}

func TestRenderAutomaticallyBoundsOversizedOutput(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(8192, 2))
	for _, options := range []artwork.Options{{}, {Width: 4096, Height: 4096}} {
		result := renderImage(t, data, options)
		assertImageResult(t, result, "png", 4096, 1)
		if result.Source.Width != 8192 || result.Source.Height != 2 {
			t.Fatalf("incorrect original dimensions: %+v", result.Source)
		}
	}
}

func TestRenderRejectsInvalidDimensions(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	for _, size := range []int{-1, 4097, 12000} {
		for _, options := range []artwork.Options{
			{Width: size}, {Height: size}, {MaxWidth: size}, {MaxHeight: size},
		} {
			if _, err := artwork.Render(context.Background(), bytes.NewReader(data), options); !errors.Is(err, artwork.ErrInvalidOptions) {
				t.Errorf("dimensions %+v returned %v, want ErrInvalidOptions", options, err)
			}
		}
	}
}

func TestRenderPreservesPNGAlphaAndKnownColors(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	draw.Draw(source, image.Rect(0, 0, 4, 4), &image.Uniform{C: color.NRGBA{R: 240, G: 30, B: 10, A: 128}}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(4, 0, 8, 4), &image.Uniform{C: color.NRGBA{R: 10, G: 60, B: 230, A: 255}}, image.Point{}, draw.Src)
	result := renderImage(t, encodeImage(t, "png", source), artwork.Options{Width: 4})
	decoded := assertImageResult(t, result, "png", 4, 2)
	assertColorNear(t, decoded.At(0, 0), color.NRGBA{R: 240, G: 30, B: 10, A: 128}, 2)
	assertColorNear(t, decoded.At(3, 1), color.NRGBA{R: 10, G: 60, B: 230, A: 255}, 2)
}

func TestRenderJPEGCompositesTransparencyOntoWhite(t *testing.T) {
	for _, test := range []struct {
		name   string
		source color.NRGBA
		want   color.NRGBA
	}{
		{"transparent", color.NRGBA{R: 255, A: 0}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"half transparent red", color.NRGBA{R: 255, A: 128}, color.NRGBA{R: 255, G: 127, B: 127, A: 255}},
		{"opaque blue", color.NRGBA{B: 255, A: 255}, color.NRGBA{B: 255, A: 255}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, 16, 16))
			draw.Draw(source, source.Bounds(), &image.Uniform{C: test.source}, image.Point{}, draw.Src)
			result := renderImage(t, encodeImage(t, "png", source), artwork.Options{Format: "jpeg", Quality: 100})
			decoded := assertImageResult(t, result, "jpeg", 16, 16)
			assertColorNear(t, decoded.At(8, 8), test.want, 3)
		})
	}
}

func TestRenderGIFPreservesAnimationUntilTransformation(t *testing.T) {
	palette := color.Palette{color.Black, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 8, 4), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range first.Pix {
		first.Pix[i], second.Pix[i] = 1, 2
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 20}, LoopCount: 3}); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	result := renderImage(t, data, artwork.Options{})
	if !bytes.Equal(result.Bytes, data) {
		t.Fatal("unchanged GIF animation was rewritten")
	}
	animation, err := gif.DecodeAll(bytes.NewReader(result.Bytes))
	if err != nil || len(animation.Image) != 2 || animation.LoopCount != 3 || animation.Delay[1] != 20 {
		t.Fatalf("GIF animation was not preserved: %+v, %v", animation, err)
	}

	result = renderImage(t, data, artwork.Options{Width: 4})
	assertImageResult(t, result, "gif", 4, 2)
	animation, err = gif.DecodeAll(bytes.NewReader(result.Bytes))
	if err != nil || len(animation.Image) != 1 {
		t.Fatalf("resized GIF must contain one frame: %+v, %v", animation, err)
	}
	assertColorNear(t, animation.Image[0].At(1, 1), color.NRGBA{R: 255, A: 255}, 10)

	result = renderImage(t, data, artwork.Options{Format: "png"})
	decoded := assertImageResult(t, result, "png", 8, 4)
	assertColorNear(t, decoded.At(4, 2), color.NRGBA{R: 255, A: 255}, 0)
}

func TestRenderGIFPreservesTransparencyIndependentOfBackgroundIndex(t *testing.T) {
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.Transparent, color.RGBA{B: 255, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 8, 4), palette)
	for y := range 4 {
		for x := range 8 {
			index := uint8(1)
			if x >= 4 {
				index = 2
			}
			frame.SetColorIndex(x, y, index)
		}
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{
		Image:           []*image.Paletted{frame},
		Delay:           []int{0},
		Config:          image.Config{ColorModel: palette, Width: 8, Height: 4},
		BackgroundIndex: 0,
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		options       artwork.Options
		format        string
		width, height int
	}{
		{"convert to PNG", artwork.Options{Format: "png"}, "png", 8, 4},
		{"resize to PNG", artwork.Options{Format: "png", Width: 4}, "png", 4, 2},
		{"resize GIF", artwork.Options{Width: 4}, "gif", 4, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := renderImage(t, encoded.Bytes(), test.options)
			decoded := assertImageResult(t, result, test.format, test.width, test.height)
			assertColorNear(t, decoded.At(0, 0), color.NRGBA{}, 0)
			assertColorNear(t, decoded.At(test.width-1, test.height-1), color.NRGBA{B: 255, A: 255}, 0)
		})
	}
}

func TestRenderTagsIdentifySourceAndActualVariant(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(120, 80))
	first := renderImage(t, data, artwork.Options{Width: 60})
	repeated := renderImage(t, data, artwork.Options{Width: 60})
	smaller := renderImage(t, data, artwork.Options{Width: 30})
	converted := renderImage(t, data, artwork.Options{Width: 60, Format: "jpeg"})
	if !bytes.Equal(first.Bytes, repeated.Bytes) || first.ETag != repeated.ETag {
		t.Fatal("identical rendering options did not produce stable output and tags")
	}
	for _, result := range []artwork.Result{first, repeated, smaller, converted} {
		if result.Source.Tag != digest(data) || result.ETag != digest(result.Bytes) {
			t.Fatalf("tags are not hashes of the source and output bytes: source=%q, output=%q", result.Source.Tag, result.ETag)
		}
	}
	if first.ETag == smaller.ETag || first.ETag == converted.ETag || smaller.ETag == converted.ETag {
		t.Fatal("distinct output variants share a content tag")
	}
}

func TestRenderJPEGQualityDefaultsAndBounds(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(64, 48))
	defaultResult := renderImage(t, data, artwork.Options{Format: "jpeg"})
	explicitDefault := renderImage(t, data, artwork.Options{Format: "jpeg", Quality: 85})
	if !bytes.Equal(defaultResult.Bytes, explicitDefault.Bytes) {
		t.Fatal("default JPEG quality is not equivalent to quality 85")
	}
	low := renderImage(t, data, artwork.Options{Format: "jpeg", Quality: 1})
	high := renderImage(t, data, artwork.Options{Format: "jpeg", Quality: 100})
	assertImageResult(t, low, "jpeg", 64, 48)
	assertImageResult(t, high, "jpeg", 64, 48)
	if bytes.Equal(low.Bytes, high.Bytes) {
		t.Fatal("JPEG quality does not affect encoded output")
	}
	for _, quality := range []int{-1, 101} {
		for _, format := range []string{"jpeg", "png", "original"} {
			if _, err := artwork.Render(context.Background(), bytes.NewReader(data), artwork.Options{Format: format, Quality: quality}); err == nil {
				t.Errorf("accepted invalid quality %d for %s", quality, format)
			}
		}
	}
	firstPNG := renderImage(t, data, artwork.Options{Width: 32, Quality: 1})
	secondPNG := renderImage(t, data, artwork.Options{Width: 32, Quality: 100})
	if !bytes.Equal(firstPNG.Bytes, secondPNG.Bytes) {
		t.Fatal("JPEG quality unexpectedly affects PNG encoding")
	}
}

func TestInspectAndRenderRejectInvalidImageData(t *testing.T) {
	validPNG := encodeImage(t, "png", sampleImage(4, 4))
	corruptPNG := append([]byte(nil), validPNG...)
	corruptPNG[len(corruptPNG)/2] ^= 0xff
	for name, data := range map[string][]byte{
		"empty":                    nil,
		"arbitrary bytes":          []byte("not an image"),
		"SVG":                      []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`),
		"PNG header without image": validPNG[:33],
		"corrupt PNG":              corruptPNG,
		"excessive width":          pngHeader(16385, 1),
		"excessive height":         pngHeader(1, 16385),
		"excessive pixel count":    pngHeader(6000, 5000),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := artwork.Inspect(bytes.NewReader(data)); err == nil {
				t.Error("Inspect accepted an invalid or oversized image")
			}
			if _, err := artwork.Render(context.Background(), bytes.NewReader(data), artwork.Options{}); err == nil {
				t.Error("Render accepted an invalid or oversized image")
			}
		})
	}
	for _, format := range []string{"jpeg", "gif"} {
		data := encodeImage(t, format, sampleImage(12, 8))
		data = data[:len(data)/2]
		if _, err := artwork.Inspect(bytes.NewReader(data)); err == nil {
			t.Errorf("Inspect accepted truncated %s image data", format)
		}
		if _, err := artwork.Render(context.Background(), bytes.NewReader(data), artwork.Options{}); err == nil {
			t.Errorf("Render accepted truncated %s image data", format)
		}
	}
}

func TestInspectAndRenderRejectInputByteLimit(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(4, 4))
	newInput := func() io.Reader {
		return io.MultiReader(bytes.NewReader(data), io.LimitReader(zeroReader{}, 20<<20))
	}
	if _, err := artwork.Inspect(newInput()); err == nil {
		t.Error("Inspect accepted an image exceeding the 20 MiB byte limit")
	}
	if _, err := artwork.Render(context.Background(), newInput(), artwork.Options{}); err == nil {
		t.Error("Render accepted an image exceeding the 20 MiB byte limit")
	}
}

func TestInspectAndRenderValidateEveryGIFFrame(t *testing.T) {
	data := repeatedGIF(t, 2, 2, 2)
	broken := data[:len(data)-3]
	if _, err := gif.Decode(bytes.NewReader(broken)); err != nil {
		t.Fatalf("fixture must retain a valid first frame: %v", err)
	}
	if _, err := artwork.Inspect(bytes.NewReader(broken)); err == nil {
		t.Error("Inspect accepted a GIF with a damaged second frame")
	}
	if _, err := artwork.Render(context.Background(), bytes.NewReader(broken), artwork.Options{Width: 1}); err == nil {
		t.Error("Render accepted a GIF with a damaged second frame")
	}
}

func TestInspectAndRenderRejectGIFAnimationResourceLimits(t *testing.T) {
	for _, test := range []struct {
		name                  string
		width, height, frames int
	}{
		{"too many frames", 1, 1, 1001},
		{"more than 32 Mi decoded pixels", 256, 512, 257},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := repeatedGIF(t, test.width, test.height, test.frames)
			if _, err := artwork.Inspect(bytes.NewReader(data)); err == nil {
				t.Error("Inspect accepted an animation exceeding its decoded resource limit")
			}
			if _, err := artwork.Render(context.Background(), bytes.NewReader(data), artwork.Options{}); err == nil {
				t.Error("Render accepted an animation exceeding its decoded resource limit")
			}
		})
	}
}

func TestInspectAndRenderPropagateReaderFailure(t *testing.T) {
	want := errors.New("source read failed")
	if _, err := artwork.Inspect(errorReader{err: want}); !errors.Is(err, want) {
		t.Errorf("Inspect error = %v, want source read failure", err)
	}
	if _, err := artwork.Render(context.Background(), errorReader{err: want}, artwork.Options{}); !errors.Is(err, want) {
		t.Errorf("Render error = %v, want source read failure", err)
	}
}

func TestRenderCancellationBeforeAndDuringRead(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := artwork.Render(ctx, bytes.NewReader(data), artwork.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled Render error = %v, want context.Canceled", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelReader{source: bytes.NewReader(data), cancel: cancel}
	if _, err := artwork.Render(ctx, reader, artwork.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Render canceled during reading returned %v, want context.Canceled", err)
	}
}

func TestRenderCancellationDoesNotWaitForBlockedReader(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	reader := &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	finished := make(chan error, 1)
	go func() {
		_, err := artwork.Render(ctx, reader, artwork.Options{})
		finished <- err
	}()
	select {
	case <-reader.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Render did not start reading its input")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Render with a blocked input returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Render waited for the blocked reader instead of returning cancellation")
	}
}

func TestRenderCancelsWhileSharedWorkSlotsAreOccupied(t *testing.T) {
	data := encodeImage(t, "png", sampleImage(12, 8))
	release := make(chan struct{})
	first := &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	second := &blockingReader{source: bytes.NewReader(data), entered: make(chan struct{}), release: release}
	finished := make(chan error, 2)
	go func() {
		_, err := artwork.Render(context.Background(), first, artwork.Options{})
		finished <- err
	}()
	go func() {
		_, err := artwork.Inspect(second)
		finished <- err
	}()
	defer func() {
		close(release)
		for range 2 {
			select {
			case err := <-finished:
				if err != nil {
					t.Errorf("occupied slot operation failed after release: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Error("occupied slot operation did not finish after release")
			}
		}
	}()
	for _, entered := range []<-chan struct{}{first.entered, second.entered} {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("Render and Inspect could not enter their two shared work slots")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	queued := &countingReader{source: bytes.NewReader(data)}
	result := make(chan error, 1)
	go func() {
		_, err := artwork.Render(ctx, queued, artwork.Options{})
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("queued Render returned %v, want context.DeadlineExceeded", err)
		}
		if queued.reads.Load() != 0 {
			t.Fatal("third operation read input while both shared work slots were occupied")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued Render ignored cancellation")
	}
}

func sampleImage(width, height int) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			result.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 17), G: uint8(y * 23), B: uint8((x + y) * 11), A: 255})
		}
	}
	return result
}

func encodeImage(t *testing.T, format string, source image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 95})
	case "png":
		err = png.Encode(&encoded, source)
	case "gif":
		err = gif.Encode(&encoded, source, nil)
	default:
		t.Fatalf("unsupported fixture format %q", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func renderImage(t *testing.T, source []byte, options artwork.Options) artwork.Result {
	t.Helper()
	result, err := artwork.Render(context.Background(), bytes.NewReader(source), options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertImageResult(t *testing.T, result artwork.Result, format string, width, height int) image.Image {
	t.Helper()
	if result.MIMEType != "image/"+format || result.Width != width || result.Height != height {
		t.Fatalf("incorrect result: MIME=%q, dimensions=%dx%d; want image/%s, %dx%d", result.MIMEType, result.Width, result.Height, format, width, height)
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatalf("output cannot be decoded: %v", err)
	}
	if decodedFormat != format || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		t.Fatalf("encoded output disagrees with result metadata: format=%q, bounds=%v", decodedFormat, decoded.Bounds())
	}
	if result.ETag != digest(result.Bytes) {
		t.Fatalf("output tag = %q, want SHA-256 of encoded output", result.ETag)
	}
	return decoded
}

func assertColorNear(t *testing.T, got color.Color, want color.NRGBA, tolerance int) {
	t.Helper()
	actual := color.NRGBAModel.Convert(got).(color.NRGBA)
	for _, pair := range [][2]uint8{{actual.R, want.R}, {actual.G, want.G}, {actual.B, want.B}, {actual.A, want.A}} {
		difference := int(pair[0]) - int(pair[1])
		if difference < -tolerance || difference > tolerance {
			t.Fatalf("pixel = %+v, want %+v with tolerance %d", actual, want, tolerance)
		}
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 33)
	copy(data, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	data[24], data[25] = 8, 6
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func repeatedGIF(t *testing.T, width, height, frames int) []byte {
	t.Helper()
	source := image.NewPaletted(image.Rect(0, 0, width, height), color.Palette{color.Black, color.White})
	data := encodeImage(t, "gif", source)
	frameStart := 13
	if data[10]&0x80 != 0 {
		frameStart += 3 * (1 << ((data[10] & 7) + 1))
	}
	if data[frameStart] != 0x2c || data[len(data)-1] != 0x3b {
		t.Fatal("single-frame GIF fixture has an unexpected structure")
	}
	frame := data[frameStart : len(data)-1]
	result := make([]byte, 0, frameStart+len(frame)*frames+1)
	result = append(result, data[:frameStart]...)
	for range frames {
		result = append(result, frame...)
	}
	return append(result, 0x3b)
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

type cancelReader struct {
	source io.Reader
	cancel context.CancelFunc
}

func (reader *cancelReader) Read(p []byte) (int, error) {
	n, err := reader.source.Read(p)
	reader.cancel()
	return n, err
}

type blockingReader struct {
	source  io.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (reader *blockingReader) Read(p []byte) (int, error) {
	reader.once.Do(func() {
		close(reader.entered)
		<-reader.release
	})
	return reader.source.Read(p)
}

type countingReader struct {
	source io.Reader
	reads  atomic.Int32
}

func (reader *countingReader) Read(p []byte) (int, error) {
	reader.reads.Add(1)
	return reader.source.Read(p)
}
