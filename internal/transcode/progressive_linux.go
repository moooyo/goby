//go:build linux

package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

// progressiveObserver serializes all callbacks. Readiness comes from bounded
// media-prefix inspection, independently of FFmpeg's initial progress message,
// which can precede the first usable fragment or audio packet.
type progressiveObserver struct {
	file           *os.File
	plan           Plan
	callback       func(Progress)
	cancel         func()
	mu             sync.Mutex
	last           Progress
	err            error
	stop           chan struct{}
	done           chan struct{}
	wavHeaderBytes int64
	wavDataBytes   int64
	wavAlignment   int
	wavMaxPadding  int64
}

func newProgressiveObserver(directory string, input *os.File, plan Plan, callback func(Progress), cancel func()) (*progressiveObserver, error) {
	var wavHeader []byte
	var wavDataBytes int64
	var wavAlignment int
	var wavMaxPadding int64
	if plan.Container == "wav" {
		channels, rate := progressiveAudioDimensions(plan)
		format := progressivePCMFormat(channels, rate)
		if plan.AudioCodec == "copy" {
			var err error
			var sourceSamples int64
			format, sourceSamples, err = progressiveSourceWAVFormat(input)
			if err != nil {
				return nil, err
			}
			rate = int(binary.LittleEndian.Uint32(format[4:8]))
			if rate != plan.AudioSourceSampleRate || plan.AudioSourceSampleCount > 0 && sourceSamples != plan.AudioSourceSampleCount {
				return nil, ErrInvalidInput
			}
		}
		var err error
		samples, err := ProgressiveOutputSamples(plan, rate)
		if err != nil {
			return nil, err
		}
		wavHeader, wavDataBytes, err = progressiveWAVHeader(format, samples)
		if err != nil {
			return nil, err
		}
		wavAlignment = int(binary.LittleEndian.Uint16(format[12:14]))
		wavMaxPadding = ((int64(rate)+int64(plan.AudioSourceSampleRate)-1)/int64(plan.AudioSourceSampleRate) + 1) * int64(wavAlignment)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// O_APPEND is an additional kernel guarantee: even an erroneous seek in
	// a codec/muxer cannot overwrite bytes already consumed by an HTTP reader.
	file, err := cacheOpenRegular(dir, "stream.bin", syscall.O_RDWR|syscall.O_APPEND|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if len(wavHeader) > 0 {
		if _, err := file.Write(wavHeader); err != nil {
			file.Close()
			return nil, err
		}
	}
	return &progressiveObserver{file: file, plan: plan, callback: callback, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}), wavHeaderBytes: int64(len(wavHeader)), wavDataBytes: wavDataBytes, wavAlignment: wavAlignment, wavMaxPadding: wavMaxPadding}, nil
}

func (o *progressiveObserver) start() {
	go func() {
		defer close(o.done)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-o.stop:
				return
			case <-ticker.C:
				if err := o.inspect(false); err != nil {
					o.mu.Lock()
					o.err = err
					o.mu.Unlock()
					o.cancel()
					return
				}
			}
		}
	}()
}

func (o *progressiveObserver) report(p Progress) {
	o.mu.Lock()
	defer o.mu.Unlock()
	p.Ready = o.last.Ready
	if p.Bytes < o.last.Bytes {
		p.Bytes = o.last.Bytes
	}
	o.last = p
	if o.callback != nil {
		o.callback(p)
	}
}

func (o *progressiveObserver) inspect(final bool) error {
	info, err := o.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 0 {
		return ErrInvalidProgressiveStream
	}
	if o.wavHeaderBytes > 0 && info.Size() > o.wavHeaderBytes+o.wavDataBytes {
		return ErrInvalidProgressiveStream
	}
	o.mu.Lock()
	alreadyReady := o.last.Ready
	o.mu.Unlock()
	ready := alreadyReady
	if !ready && info.Size() > 0 {
		length := min(info.Size(), int64(MaxProgressivePrefixBytes))
		prefix := make([]byte, int(length))
		n, err := o.file.ReadAt(prefix, 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		prefix = prefix[:n]
		if o.plan.AudioCodec == "copy" && o.plan.StartTicks > 0 && o.plan.Container == "ogg" && oggFLACIdentification(prefix) {
			return ErrInvalidProgressiveStream
		}
		ready, err = ProgressiveMediaReady(o.plan, prefix)
		if err != nil {
			return err
		}
		if !ready && info.Size() >= int64(MaxProgressivePrefixBytes) {
			return ErrInvalidProgressiveStream
		}
	}
	if final && !ready {
		return ErrInvalidProgressiveStream
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	changed := ready && !o.last.Ready
	o.last.Ready = ready
	if info.Size() > o.last.Bytes {
		o.last.Bytes = info.Size()
	}
	if (changed || final) && o.callback != nil {
		o.callback(o.last)
	}
	return nil
}

func (o *progressiveObserver) finish(success bool) error {
	close(o.stop)
	<-o.done
	o.mu.Lock()
	err := o.err
	o.mu.Unlock()
	if err == nil && success {
		if o.wavHeaderBytes > 0 {
			err = o.finishWAV()
		}
	}
	if err == nil && success {
		err = o.inspect(true)
	}
	if syncErr := o.file.Sync(); err == nil {
		err = syncErr
	}
	return err
}

func (o *progressiveObserver) finishWAV() error {
	info, err := o.file.Stat()
	if err != nil {
		return err
	}
	actual := info.Size() - o.wavHeaderBytes
	missing := o.wavDataBytes - actual
	if actual <= 0 || actual%int64(o.wavAlignment) != 0 || missing < 0 || missing > o.wavMaxPadding {
		return ErrInvalidProgressiveStream
	}
	if missing > 0 {
		_, err = o.file.Write(make([]byte, int(missing)))
	}
	return err
}

func progressiveSourceWAVFormat(input *os.File) ([]byte, int64, error) {
	var head [12]byte
	if _, err := input.ReadAt(head[:], 0); err != nil {
		return nil, 0, ErrInvalidInput
	}
	if (!bytes.Equal(head[:4], []byte("RIFF")) && !bytes.Equal(head[:4], []byte("RF64"))) || !bytes.Equal(head[8:], []byte("WAVE")) {
		return nil, 0, ErrInvalidInput
	}
	info, err := input.Stat()
	if err != nil {
		return nil, 0, ErrInvalidInput
	}
	var format []byte
	var alignment int
	var rf64DataSize uint64
	offset := int64(12)
	for count := 0; count < maxProgressiveHeaderElements; count++ {
		var chunk [8]byte
		if offset > MaxProgressivePrefixBytes-8 {
			return nil, 0, ErrInvalidInput
		}
		if _, err := input.ReadAt(chunk[:], offset); err != nil {
			return nil, 0, ErrInvalidInput
		}
		size := int64(binary.LittleEndian.Uint32(chunk[4:]))
		offset += 8
		if bytes.Equal(chunk[:4], []byte("fmt ")) {
			if len(format) != 0 || size < 16 || size > 1024 {
				return nil, 0, ErrInvalidInput
			}
			format = make([]byte, int(size))
			if _, err := input.ReadAt(format, offset); err != nil {
				return nil, 0, ErrInvalidInput
			}
			alignment, err = progressiveWAVFormat(format)
			if err != nil {
				return nil, 0, ErrInvalidInput
			}
			if binary.LittleEndian.Uint16(format[2:4]) > 8 {
				return nil, 0, ErrInvalidInput
			}
			if binary.LittleEndian.Uint16(format[:2]) == 1 {
				format = format[:16]
			} else {
				format = format[:40]
			}
		}
		if bytes.Equal(chunk[:4], []byte("ds64")) {
			if size < 28 {
				return nil, 0, ErrInvalidInput
			}
			var ds64 [28]byte
			if _, err := input.ReadAt(ds64[:], offset); err != nil {
				return nil, 0, ErrInvalidInput
			}
			rf64DataSize = binary.LittleEndian.Uint64(ds64[8:16])
		}
		if bytes.Equal(chunk[:4], []byte("data")) {
			if alignment == 0 {
				return nil, 0, ErrInvalidInput
			}
			if size == 0xffffffff {
				if bytes.Equal(head[:4], []byte("RF64")) {
					if rf64DataSize > uint64(info.Size()) {
						return nil, 0, ErrInvalidInput
					}
					size = int64(rf64DataSize)
				} else {
					size = info.Size() - offset
				}
			}
			if size <= 0 || size > info.Size()-offset || size%int64(alignment) != 0 {
				return nil, 0, ErrInvalidInput
			}
			return format, size / int64(alignment), nil
		}
		if size > MaxProgressivePrefixBytes-offset {
			return nil, 0, ErrInvalidInput
		}
		offset += size + size%2
	}
	return nil, 0, ErrInvalidInput
}

func oggFLACIdentification(prefix []byte) bool {
	if len(prefix) < 27 || !bytes.Equal(prefix[:4], []byte("OggS")) {
		return false
	}
	header := 27 + int(prefix[26])
	return len(prefix) >= header+5 && bytes.Equal(prefix[header:header+5], []byte{0x7f, 'F', 'L', 'A', 'C'})
}
