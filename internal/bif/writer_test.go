package bif

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"testing"
)

func TestWriterProducesExactLayoutAndByteCount(t *testing.T) {
	frames := [][]byte{writerJPEG(t, 2, 3, 31), writerJPEG(t, 3, 2, 173)}
	timestamps := []uint32{0, 1500}
	cases := []struct {
		name       string
		multiplier uint32
		limits     Limits
	}{
		{name: "default multiplier"},
		{name: "explicit multiplier", multiplier: 25},
		{name: "exact independent limits", multiplier: 1000, limits: Limits{
			MaxFrames:     2,
			MaxFrameBytes: uint32(max(len(frames[0]), len(frames[1]))),
			MaxTotalBytes: uint64(64 + 3*8 + len(frames[0]) + len(frames[1])),
			MaxDimension:  3,
			MaxPixels:     6,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var destination writerObservedSink
			written, err := Write(context.Background(), &destination, writerSources(frames, timestamps), tc.multiplier, tc.limits)
			if err != nil {
				t.Fatal(err)
			}
			want := writerExpectedBIF(frames, timestamps, tc.multiplier)
			if !bytes.Equal(destination.Bytes(), want) {
				t.Fatal("output differs from the expected header, absolute index offsets, or frame bytes")
			}
			if written != int64(len(want)) {
				t.Fatalf("written = %d, want %d", written, len(want))
			}
			if destination.closed != 0 {
				t.Fatal("closed the caller-owned destination")
			}
		})
	}
}

func TestWriterPreflightsAllSourcesBeforeOpeningOrWriting(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 80)
	total := uint64(64 + 3*8 + 2*len(frame))
	cases := []struct {
		name   string
		change func([]FrameSource)
		limits Limits
		want   error
	}{
		{name: "duplicate timestamps", change: func(frames []FrameSource) { frames[1].Timestamp = 10 }, want: ErrFormat},
		{name: "descending timestamps", change: func(frames []FrameSource) { frames[1].Timestamp = 9 }, want: ErrFormat},
		{name: "reserved timestamp", change: func(frames []FrameSource) { frames[1].Timestamp = ^uint32(0) }, want: ErrFormat},
		{name: "negative size", change: func(frames []FrameSource) { frames[1].Size = -1 }, want: ErrFormat},
		{name: "zero size", change: func(frames []FrameSource) { frames[1].Size = 0 }, want: ErrFormat},
		{name: "nil opener", change: func(frames []FrameSource) { frames[1].Open = nil }, want: ErrFormat},
		{name: "maximum int64 size", change: func(frames []FrameSource) { frames[1].Size = math.MaxInt64 }, want: ErrLimit},
		{name: "size exceeds uint32 offset", change: func(frames []FrameSource) { frames[1].Size = int64(math.MaxUint32) + 1 }, want: ErrLimit},
		{name: "frame count limit", limits: Limits{MaxFrames: 1}, want: ErrLimit},
		{name: "frame byte limit", limits: Limits{MaxFrameBytes: uint32(len(frame) - 1)}, want: ErrLimit},
		{name: "total byte limit", limits: Limits{MaxTotalBytes: total - 1}, want: ErrLimit},
		{name: "invalid limits", limits: Limits{MaxTotalBytes: 71}, want: ErrInvalidLimits},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opened := 0
			frames := writerSources([][]byte{frame, frame}, []uint32{10, 20})
			for i := range frames {
				frames[i].Open = func(context.Context) (io.ReadCloser, error) {
					opened++
					return io.NopCloser(bytes.NewReader(frame)), nil
				}
			}
			if tc.change != nil {
				tc.change(frames)
			}
			var destination writerObservedSink
			written, err := Write(context.Background(), &destination, frames, 1000, tc.limits)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if opened != 0 || destination.calls != 0 || written != 0 {
				t.Fatalf("preflight had side effects: opened = %d, writes = %d, written = %d", opened, destination.calls, written)
			}
		})
	}
}

func TestWriterRejectsNilDestinationBeforeOpeningSources(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 80)
	opened := false
	frames := []FrameSource{{Timestamp: 0, Size: int64(len(frame)), Open: func(context.Context) (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(bytes.NewReader(frame)), nil
	}}}
	written, err := Write(context.Background(), nil, frames, 1000, Limits{})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("error = %v, want format error", err)
	}
	if opened || written != 0 {
		t.Fatalf("nil destination had side effects: opened = %v, written = %d", opened, written)
	}
}

func TestWriterFreezesSourceDeclarationsBeforeOpeningCallbacks(t *testing.T) {
	data := [][]byte{writerJPEG(t, 2, 2, 50), writerJPEG(t, 2, 2, 150)}
	timestamps := []uint32{0, 1}
	frames := writerSources(data, timestamps)
	secondOpened := 0
	frames[0].Open = func(context.Context) (io.ReadCloser, error) {
		frames[1].Size = -1
		frames[1].Timestamp = math.MaxUint32
		frames[1].Open = nil
		return io.NopCloser(bytes.NewReader(data[0])), nil
	}
	frames[1].Open = func(context.Context) (io.ReadCloser, error) {
		secondOpened++
		return io.NopCloser(bytes.NewReader(data[1])), nil
	}
	var destination bytes.Buffer
	written, err := Write(context.Background(), &destination, frames, 1000, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := writerExpectedBIF(data, timestamps, 1000)
	if !bytes.Equal(destination.Bytes(), want) {
		t.Fatal("callback mutation changed the preflighted index or frame bytes")
	}
	writerCheckCount(t, written, len(want))
	if secondOpened != 1 {
		t.Fatalf("original second source opened %d times, want 1", secondOpened)
	}
}

func TestWriterClosesEachSourceBeforeOpeningTheNext(t *testing.T) {
	data := [][]byte{writerJPEG(t, 2, 2, 10), writerJPEG(t, 2, 2, 90), writerJPEG(t, 2, 2, 190)}
	frames := writerSources(data, []uint32{0, 1, 2})
	active := 0
	opened := 0
	closed := make([]int, len(frames))
	for i := range frames {
		frames[i].Open = func(context.Context) (io.ReadCloser, error) {
			if active != 0 {
				t.Errorf("opening source %d with %d readers still active", i, active)
			}
			active++
			opened++
			return &writerReadCloser{
				Reader: bytes.NewReader(data[i]),
				onClose: func() error {
					closed[i]++
					active--
					return nil
				},
			}, nil
		}
	}
	var destination bytes.Buffer
	written, err := Write(context.Background(), &destination, frames, 1000, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	writerCheckCount(t, written, destination.Len())
	if active != 0 || opened != len(frames) {
		t.Fatalf("active = %d, opened = %d", active, opened)
	}
	for i, count := range closed {
		if count != 1 {
			t.Errorf("source %d closed %d times, want 1", i, count)
		}
	}
}

func TestWriterRejectsChangedOrUnreadableSourcesAndClosesReaders(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 120)
	readFailure := errors.New("injected read failure")
	cases := []struct {
		name   string
		reader func() io.Reader
		want   error
	}{
		{name: "short source", reader: func() io.Reader { return bytes.NewReader(frame[:len(frame)-1]) }, want: ErrInvalidJPEG},
		{name: "oversized source", reader: func() io.Reader { return io.MultiReader(bytes.NewReader(frame), bytes.NewReader(make([]byte, 4096))) }, want: ErrInvalidJPEG},
		{name: "read failure", reader: func() io.Reader { return &writerFailingReader{data: frame[:len(frame)/2], err: readFailure} }, want: readFailure},
		{name: "read failure with complete data", reader: func() io.Reader { return &writerFailingReader{data: frame, err: readFailure} }, want: readFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closed := 0
			laterOpened := false
			reader := &writerCountingReader{Reader: tc.reader()}
			frames := writerSources([][]byte{frame, frame}, []uint32{0, 1})
			frames[0].Open = func(context.Context) (io.ReadCloser, error) {
				return &writerReadCloser{Reader: reader, onClose: func() error { closed++; return nil }}, nil
			}
			frames[1].Open = func(context.Context) (io.ReadCloser, error) {
				laterOpened = true
				return io.NopCloser(bytes.NewReader(frame)), nil
			}
			var destination bytes.Buffer
			written, err := Write(context.Background(), &destination, frames, 1000, Limits{})
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			writerCheckCount(t, written, destination.Len())
			if destination.Len() > 64+3*8 {
				t.Fatal("unvalidated frame bytes reached the destination")
			}
			if closed != 1 || laterOpened {
				t.Fatalf("closed = %d, later source opened = %v", closed, laterOpened)
			}
			if reader.read > int64(len(frame)+1) {
				t.Fatalf("read %d bytes, exceeding declared size plus one (%d)", reader.read, len(frame)+1)
			}
		})
	}
}

func TestWriterChecksJPEGContentBeforePublishingFrame(t *testing.T) {
	valid := writerJPEG(t, 2, 2, 120)
	cases := []struct {
		name   string
		data   []byte
		limits Limits
		want   error
	}{
		{name: "invalid JPEG", data: []byte{0xff, 0xd8, 0x00, 0x00, 0xff, 0xd9}, want: ErrInvalidJPEG},
		{name: "trailing data", data: append(append([]byte{}, valid...), 0), want: ErrInvalidJPEG},
		{name: "concatenated JPEGs", data: append(append([]byte{}, valid...), valid...), want: ErrInvalidJPEG},
		{name: "dimension limit", data: valid, limits: Limits{MaxDimension: 1}, want: ErrLimit},
		{name: "pixel limit", data: valid, limits: Limits{MaxPixels: 3}, want: ErrLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			closed := 0
			frames := []FrameSource{{Timestamp: 0, Size: int64(len(tc.data)), Open: func(context.Context) (io.ReadCloser, error) {
				return &writerReadCloser{Reader: bytes.NewReader(tc.data), onClose: func() error { closed++; return nil }}, nil
			}}}
			var destination bytes.Buffer
			written, err := Write(context.Background(), &destination, frames, 1000, tc.limits)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			writerCheckCount(t, written, destination.Len())
			if destination.Len() > 64+2*8 {
				t.Fatal("invalid frame bytes reached the destination")
			}
			if closed != 1 {
				t.Fatalf("source closed %d times, want 1", closed)
			}
		})
	}
}

func TestWriterPreservesCompletedFramesOnLaterOpenFailure(t *testing.T) {
	data := [][]byte{writerJPEG(t, 2, 2, 50), writerJPEG(t, 2, 2, 150)}
	frames := writerSources(data, []uint32{0, 1})
	openFailure := errors.New("injected open failure")
	closed := 0
	frames[0].Open = func(context.Context) (io.ReadCloser, error) {
		return &writerReadCloser{Reader: bytes.NewReader(data[0]), onClose: func() error { closed++; return nil }}, nil
	}
	frames[1].Open = func(context.Context) (io.ReadCloser, error) { return nil, openFailure }
	var destination bytes.Buffer
	written, err := Write(context.Background(), &destination, frames, 1000, Limits{})
	if !errors.Is(err, openFailure) {
		t.Fatalf("error = %v, want open failure", err)
	}
	writerCheckCount(t, written, destination.Len())
	want := writerExpectedBIF(data, []uint32{0, 1}, 1000)
	end := 64 + 3*8 + len(data[0])
	if !bytes.Equal(destination.Bytes(), want[:end]) {
		t.Fatal("destination does not preserve exactly the completed prefix")
	}
	if closed != 1 {
		t.Fatalf("first source closed %d times, want 1", closed)
	}
}

func TestWriterClosesReadersReturnedWithOpenErrorsAndReportsCloseFailures(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 90)
	openFailure := errors.New("injected open failure")
	closeFailure := errors.New("injected close failure")
	for _, openErr := range []error{nil, openFailure} {
		name := "close failure"
		if openErr != nil {
			name = "open and close failure"
		}
		t.Run(name, func(t *testing.T) {
			closed := 0
			laterOpened := false
			frames := writerSources([][]byte{frame, frame}, []uint32{0, 1})
			frames[0].Open = func(context.Context) (io.ReadCloser, error) {
				return &writerReadCloser{Reader: bytes.NewReader(frame), onClose: func() error {
					closed++
					return closeFailure
				}}, openErr
			}
			frames[1].Open = func(context.Context) (io.ReadCloser, error) {
				laterOpened = true
				return io.NopCloser(bytes.NewReader(frame)), nil
			}
			var destination bytes.Buffer
			written, err := Write(context.Background(), &destination, frames, 1000, Limits{})
			if !errors.Is(err, closeFailure) || (openErr != nil && !errors.Is(err, openErr)) {
				t.Fatalf("error = %v, want close failure and any open failure", err)
			}
			writerCheckCount(t, written, destination.Len())
			if destination.Len() > 64+3*8 {
				t.Fatal("frame bytes reached the destination before the source closed successfully")
			}
			if closed != 1 || laterOpened {
				t.Fatalf("closed = %d, later source opened = %v", closed, laterOpened)
			}
		})
	}
}

func TestWriterReportsSinkFailuresAndClosesSources(t *testing.T) {
	data := [][]byte{writerJPEG(t, 2, 2, 50), writerJPEG(t, 2, 2, 150)}
	want := writerExpectedBIF(data, []uint32{0, 1}, 1000)
	sinkFailure := errors.New("injected sink failure")
	cases := []struct {
		name string
		at   int
		err  error
		want error
	}{
		{name: "header failure", at: 10, err: sinkFailure, want: sinkFailure},
		{name: "index failure", at: 70, err: sinkFailure, want: sinkFailure},
		{name: "frame failure", at: 64 + 3*8 + len(data[0])/2, err: sinkFailure, want: sinkFailure},
		{name: "short frame write", at: 64 + 3*8 + len(data[0])/2, want: io.ErrShortWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opened := 0
			closed := 0
			frames := writerSources(data, []uint32{0, 1})
			for i := range frames {
				frames[i].Open = func(context.Context) (io.ReadCloser, error) {
					opened++
					return &writerReadCloser{Reader: bytes.NewReader(data[i]), onClose: func() error { closed++; return nil }}, nil
				}
			}
			destination := &writerLimitedSink{remaining: tc.at, err: tc.err}
			written, err := Write(context.Background(), destination, frames, 1000, Limits{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			writerCheckCount(t, written, destination.Len())
			if written != int64(tc.at) || !bytes.Equal(destination.Bytes(), want[:tc.at]) {
				t.Fatalf("destination does not contain the expected %d-byte prefix", tc.at)
			}
			if opened != closed || opened > 1 {
				t.Fatalf("opened = %d, closed = %d", opened, closed)
			}
			if tc.at < 64+3*8 && opened != 0 {
				t.Fatal("opened a frame source after the header or index write failed")
			}
		})
	}
}

func TestWriterRejectsNonzeroShortWritesWithoutRetrying(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 80)
	opened := false
	frames := []FrameSource{{Timestamp: 0, Size: int64(len(frame)), Open: func(context.Context) (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(bytes.NewReader(frame)), nil
	}}}
	destination := &writerAlwaysShortSink{}
	written, err := Write(context.Background(), destination, frames, 1000, Limits{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want short write", err)
	}
	writerCheckCount(t, written, destination.Len())
	if written == 0 || destination.calls != 1 || opened {
		t.Fatalf("short write result: written = %d, writes = %d, source opened = %v", written, destination.calls, opened)
	}
}

func TestWriterHonorsCancellationBeforeOpeningSources(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 80)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	frames := []FrameSource{{Timestamp: 0, Size: int64(len(frame)), Open: func(context.Context) (io.ReadCloser, error) {
		opened = true
		return io.NopCloser(bytes.NewReader(frame)), nil
	}}}
	var destination writerObservedSink
	written, err := Write(ctx, &destination, frames, 1000, Limits{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if opened || destination.calls != 0 || written != 0 {
		t.Fatalf("cancelled call had side effects: opened = %v, writes = %d, written = %d", opened, destination.calls, written)
	}
}

func TestWriterClosesReaderWhenCancelledDuringRead(t *testing.T) {
	frame := writerJPEG(t, 2, 2, 80)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := 0
	closed := 0
	frames := writerSources([][]byte{frame, frame}, []uint32{0, 1})
	for i := range frames {
		frames[i].Open = func(sourceContext context.Context) (io.ReadCloser, error) {
			opened++
			return &writerReadCloser{Reader: writerReaderFunc(func([]byte) (int, error) {
				cancel()
				return 0, sourceContext.Err()
			}), onClose: func() error { closed++; return nil }}, nil
		}
	}
	var destination bytes.Buffer
	written, err := Write(ctx, &destination, frames, 1000, Limits{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	writerCheckCount(t, written, destination.Len())
	if opened != 1 || closed != 1 {
		t.Fatalf("opened = %d, closed = %d; want one opened and closed source", opened, closed)
	}
	if destination.Len() > 64+3*8 {
		t.Fatal("cancelled frame bytes reached the destination")
	}
}

func TestWriterHonorsCancellationBetweenFrames(t *testing.T) {
	data := [][]byte{writerJPEG(t, 2, 2, 50), writerJPEG(t, 2, 2, 150)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := writerSources(data, []uint32{0, 1})
	opened := 0
	closed := 0
	for i := range frames {
		frames[i].Open = func(context.Context) (io.ReadCloser, error) {
			opened++
			return &writerReadCloser{Reader: bytes.NewReader(data[i]), onClose: func() error { closed++; return nil }}, nil
		}
	}
	end := 64 + 3*8 + len(data[0])
	destination := &writerCancelSink{cancel: cancel, at: end}
	written, err := Write(ctx, destination, frames, 1000, Limits{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	writerCheckCount(t, written, destination.Len())
	want := writerExpectedBIF(data, []uint32{0, 1}, 1000)
	if !bytes.Equal(destination.Bytes(), want[:end]) {
		t.Fatal("destination does not preserve exactly the completed frame before cancellation")
	}
	if opened != 1 || closed != 1 {
		t.Fatalf("opened = %d, closed = %d; want one opened and closed source", opened, closed)
	}
}

func writerJPEG(t *testing.T, width, height int, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: shade, G: uint8(x * 31), B: uint8(y * 47), A: 255})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writerSources(data [][]byte, timestamps []uint32) []FrameSource {
	frames := make([]FrameSource, len(data))
	for i := range data {
		frames[i] = FrameSource{Timestamp: timestamps[i], Size: int64(len(data[i])), Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data[i])), nil
		}}
	}
	return frames
}

func writerExpectedBIF(frames [][]byte, timestamps []uint32, multiplier uint32) []byte {
	indexEnd := 64 + 8*(len(frames)+1)
	output := make([]byte, indexEnd)
	copy(output, []byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a})
	binary.LittleEndian.PutUint32(output[12:16], uint32(len(frames)))
	binary.LittleEndian.PutUint32(output[16:20], multiplier)
	for i, frame := range frames {
		entry := output[64+8*i : 64+8*(i+1)]
		binary.LittleEndian.PutUint32(entry[:4], timestamps[i])
		binary.LittleEndian.PutUint32(entry[4:], uint32(len(output)))
		output = append(output, frame...)
	}
	binary.LittleEndian.PutUint32(output[indexEnd-8:indexEnd-4], ^uint32(0))
	binary.LittleEndian.PutUint32(output[indexEnd-4:indexEnd], uint32(len(output)))
	return output
}

func writerCheckCount(t *testing.T, written int64, actual int) {
	t.Helper()
	if written != int64(actual) {
		t.Fatalf("written = %d, destination contains %d bytes", written, actual)
	}
}

type writerReadCloser struct {
	io.Reader
	onClose func() error
}

func (r *writerReadCloser) Close() error { return r.onClose() }

type writerCountingReader struct {
	io.Reader
	read int64
}

func (r *writerCountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	return n, err
}

type writerFailingReader struct {
	data []byte
	err  error
}

func (r *writerFailingReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

type writerReaderFunc func([]byte) (int, error)

func (f writerReaderFunc) Read(p []byte) (int, error) { return f(p) }

type writerObservedSink struct {
	bytes.Buffer
	calls  int
	closed int
}

func (w *writerObservedSink) Write(p []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(p)
}

func (w *writerObservedSink) Close() error {
	w.closed++
	return nil
}

type writerLimitedSink struct {
	bytes.Buffer
	remaining int
	err       error
}

func (w *writerLimitedSink) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n, _ := w.Buffer.Write(p[:w.remaining])
		w.remaining = 0
		return n, w.err
	}
	n, err := w.Buffer.Write(p)
	w.remaining -= n
	return n, err
}

type writerCancelSink struct {
	bytes.Buffer
	cancel context.CancelFunc
	at     int
}

func (w *writerCancelSink) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if w.Len() >= w.at {
		w.cancel()
	}
	return n, err
}

type writerAlwaysShortSink struct {
	bytes.Buffer
	calls int
}

func (w *writerAlwaysShortSink) Write(p []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(p[:len(p)/2])
}
