//go:build linux

package transcode

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
)

type liveOpenedSegment struct {
	render   LiveRendition
	files    []*os.File
	names    []string
	initHash [32]byte
}

func (opened *liveOpenedSegment) closeAndRemove(directory *os.File) error {
	var result error
	for _, file := range opened.files {
		if err := file.Close(); err != nil && result == nil {
			result = err
		}
	}
	for _, name := range opened.names {
		if err := syscall.Unlinkat(int(directory.Fd()), name); err != nil && !os.IsNotExist(err) && result == nil {
			result = err
		}
	}
	return result
}

func openLiveSegment(directory *os.File, p Plan, rendition int, record liveJournalRecord, limit int64, first bool, expectedInit [32]byte) (opened liveOpenedSegment, err error) {
	file, err := cacheOpenRegular(directory, record.name, syscall.O_RDONLY, 0)
	if err != nil {
		return opened, err
	}
	opened.files, opened.names = []*os.File{file}, []string{record.name}
	defer func() {
		if err != nil {
			_ = opened.closeAndRemove(directory)
		}
	}()
	before, err := file.Stat()
	if err != nil || before.Size() <= 0 || before.Size() > limit {
		return opened, ErrQuota
	}
	if p.HLS.SegmentType != "fmp4" {
		if err = validateLiveTransportStream(file, before.Size()); err != nil {
			return opened, err
		}
		opened.render = LiveRendition{Media: file, Size: before.Size(), ContentType: "video/mp2t"}
	} else {
		var initBytes []byte
		var offset int64
		initBytes, offset, err = liveFragmentBoundaries(file, before.Size(), p)
		if err != nil {
			return opened, err
		}
		opened.initHash = sha256.Sum256(initBytes)
		if !first && opened.initHash != expectedInit {
			return opened, fmt.Errorf("%w: live initialization changed within one producer", ErrInvalidTimeline)
		}
		mediaName := liveSegmentName(p, rendition, record.sequence, false)
		mediaFile, createErr := cacheOpenRegular(directory, mediaName, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0600)
		if createErr != nil {
			return opened, createErr
		}
		opened.files = append(opened.files, mediaFile)
		opened.names = append(opened.names, mediaName)
		written, copyErr := io.CopyBuffer(mediaFile, io.NewSectionReader(file, offset, before.Size()-offset), make([]byte, 64<<10))
		if copyErr != nil || written != before.Size()-offset {
			return opened, ErrProcess
		}
		opened.render = LiveRendition{Media: mediaFile, Size: written, ContentType: "video/mp4"}
		{
			name := hlsPrefix(rendition, p.HLS.RenditionCount) + "init.mp4"
			initFile, createErr := cacheOpenRegular(directory, name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0600)
			if createErr != nil {
				return opened, createErr
			}
			opened.files = append(opened.files, initFile)
			opened.names = append(opened.names, name)
			if _, err = initFile.Write(initBytes); err != nil {
				return opened, err
			}
			opened.render.Init = initFile
		}
	}
	if !transcodeSourceUnchanged(file, before) {
		return opened, ErrInvalidInput
	}
	return opened, nil
}

func validateLiveTransportStream(file *os.File, size int64) error {
	if size < 3*188 || size%188 != 0 {
		return ErrInvalidTimeline
	}
	buffer := make([]byte, 188*256)
	for offset := int64(0); offset < size; {
		count := min(int64(len(buffer)), size-offset)
		if _, err := file.ReadAt(buffer[:count], offset); err != nil {
			return err
		}
		for position := 0; position < int(count); position += 188 {
			if buffer[position] != 0x47 || buffer[position+1]&0x80 != 0 || buffer[position+3]&0x30 == 0 {
				return ErrInvalidTimeline
			}
		}
		offset += count
	}
	return nil
}

func liveFragmentBoundaries(file *os.File, size int64, p Plan) ([]byte, int64, error) {
	var header [16]byte
	ftyp, moov, waitingMedia := false, false, false
	mediaOffset := int64(-1)
	fragments := 0
	for offset := int64(0); offset < size; {
		if size-offset < 8 {
			return nil, 0, ErrInvalidTimeline
		}
		if _, err := file.ReadAt(header[:8], offset); err != nil {
			return nil, 0, err
		}
		length, headerSize := int64(binary.BigEndian.Uint32(header[:4])), int64(8)
		if length == 1 {
			if size-offset < 16 {
				return nil, 0, ErrInvalidTimeline
			}
			if _, err := file.ReadAt(header[8:], offset+8); err != nil {
				return nil, 0, err
			}
			large := binary.BigEndian.Uint64(header[8:])
			if large > uint64(size-offset) {
				return nil, 0, ErrInvalidTimeline
			}
			length, headerSize = int64(large), 16
		}
		if length < headerSize || length > size-offset {
			return nil, 0, ErrInvalidTimeline
		}
		switch string(header[4:8]) {
		case "ftyp":
			if offset != 0 || ftyp || length < 16 {
				return nil, 0, ErrInvalidTimeline
			}
			ftyp = true
		case "moov":
			if !ftyp || moov || mediaOffset >= 0 {
				return nil, 0, ErrInvalidTimeline
			}
			moov = true
		case "free":
			if mediaOffset >= 0 || !ftyp {
				return nil, 0, ErrInvalidTimeline
			}
		case "moof":
			if !moov || waitingMedia || length > MaxProgressivePrefixBytes {
				return nil, 0, ErrInvalidTimeline
			}
			if mediaOffset < 0 {
				mediaOffset = offset
			}
			waitingMedia = true
		case "mdat":
			if !waitingMedia || length == headerSize {
				return nil, 0, ErrInvalidTimeline
			}
			waitingMedia = false
			fragments++
		default:
			return nil, 0, ErrInvalidTimeline
		}
		offset += length
	}
	if !moov || waitingMedia || fragments == 0 || mediaOffset <= 0 || mediaOffset > MaxProgressivePrefixBytes {
		return nil, 0, ErrInvalidTimeline
	}
	prefix := make([]byte, min(size, int64(MaxProgressivePrefixBytes)))
	if _, err := file.ReadAt(prefix, 0); err != nil {
		return nil, 0, err
	}
	var ready bool
	var err error
	if p.VideoStreamIndex >= 0 {
		selected := p
		selected.OutputMode, selected.Container = "progressive", "mp4"
		ready, err = ProgressiveVideoReady(selected, prefix)
	} else {
		ready, err = ProgressiveAudioReady("m4a", prefix)
	}
	if err != nil || !ready {
		return nil, 0, ErrInvalidTimeline
	}
	return append([]byte(nil), prefix[:mediaOffset]...), mediaOffset, nil
}
