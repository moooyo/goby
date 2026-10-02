package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"math"
)

const maxGeneratedFMP4NativeFragments = 128

// GeneratedFMP4FragmentClock retains the native decode time and sample count
// declared by one complete fragment of a single initialized video track.
// These are container facts, not an independently decoded source range.
type GeneratedFMP4FragmentClock struct {
	SequenceNumber uint32
	TrackID        uint32
	DecodeUnits    uint64
	SampleCount    int64
}

// GeneratedFMP4NativeClock binds mdhd's actual track scale to every tfdt in the
// same held bytes. EditPresent is preserved rather than silently treating an
// edited movie clock as the raw media clock. Neither the raw clock nor its byte
// digests establishes native emission, effective presentation, source EOF,
// independently restartable output, audio priming or permission to publish.
type GeneratedFMP4NativeClock struct {
	TrackID             uint32
	MediaTimeScale      int64
	HeaderDurationUnits uint64
	// Only a positive, non-sentinel header value is known. An initialized
	// zero-duration movie remains unknown and never supplies source EOF.
	HeaderDurationKnown  bool
	EditPresent          bool
	FragmentCount        int
	TotalSamples         int64
	Fragments            [maxGeneratedFMP4NativeFragments]GeneratedFMP4FragmentClock
	InitializationSHA256 [32]byte
	SegmentSHA256        [32]byte
}

type generatedFMP4NativeBox struct {
	kind             string
	start, body, end int64
}

func generatedFMP4NativeReadBox(reader io.ReaderAt, size, offset int64) (generatedFMP4NativeBox, error) {
	var empty generatedFMP4NativeBox
	if offset < 0 || offset >= size || size-offset < 8 {
		return empty, ErrTimelineProbe
	}
	var header [16]byte
	if _, err := reader.ReadAt(header[:8], offset); err != nil {
		return empty, ErrTimelineProbe
	}
	length, headerLength := uint64(binary.BigEndian.Uint32(header[:4])), int64(8)
	if length == 1 {
		if size-offset < 16 {
			return empty, ErrTimelineProbe
		}
		if _, err := reader.ReadAt(header[8:], offset+8); err != nil {
			return empty, ErrTimelineProbe
		}
		length, headerLength = binary.BigEndian.Uint64(header[8:]), 16
	}
	if length < uint64(headerLength) || length > uint64(size-offset) {
		return empty, ErrTimelineProbe
	}
	return generatedFMP4NativeBox{kind: string(header[4:8]), start: offset, body: offset + headerLength, end: offset + int64(length)}, nil
}

func generatedFMP4NativeMetadata(reader io.ReaderAt, box generatedFMP4NativeBox) ([]byte, error) {
	length := box.end - box.body
	if length < 0 || length > 1<<20 {
		return nil, ErrTimelineLimit
	}
	data := make([]byte, int(length))
	if _, err := reader.ReadAt(data, box.body); err != nil {
		return nil, ErrTimelineProbe
	}
	return data, nil
}

func generatedFMP4NativeMovie(data []byte) (GeneratedFMP4NativeClock, error) {
	var empty GeneratedFMP4NativeClock
	parser := progressiveVideoParser{tracks: make(map[uint32]progressiveVideoTrack), wantCodec: "h264"}
	if err := parser.movie(data); err != nil || len(parser.tracks) != 1 {
		return empty, ErrTimelineProbe
	}
	children, err := parser.children(data)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	track, err := progressiveVideoOne(children, "trak", true)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	trackChildren, err := parser.children(track.payload)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	header, err := progressiveVideoOne(trackChildren, "tkhd", true)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	position, err := progressiveVideoTimedHeader(header.payload, 84, 96)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	id := binary.BigEndian.Uint32(header.payload[position : position+4])
	initialized, present := parser.tracks[id]
	if !present || !initialized.video || initialized.codec != "h264" {
		return empty, ErrTimelineProbe
	}
	edits, err := progressiveVideoOne(trackChildren, "edts", false)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	media, err := progressiveVideoOne(trackChildren, "mdia", true)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	mediaChildren, err := parser.children(media.payload)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	clock, err := progressiveVideoOne(mediaChildren, "mdhd", true)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	position, err = progressiveVideoTimedHeader(clock.payload, 24, 36)
	if err != nil || binary.BigEndian.Uint32(clock.payload[:4])&0xffffff != 0 {
		return empty, ErrTimelineProbe
	}
	scale := int64(binary.BigEndian.Uint32(clock.payload[position : position+4]))
	if scale < 1 || scale > 1_000_000_000 {
		return empty, ErrTimelineProbe
	}
	var duration uint64
	var known bool
	if clock.payload[0] == 0 {
		duration = uint64(binary.BigEndian.Uint32(clock.payload[position+4 : position+8]))
		known = duration > 0 && duration != math.MaxUint32
	} else {
		duration = binary.BigEndian.Uint64(clock.payload[position+4 : position+12])
		known = duration > 0 && duration != math.MaxUint64
	}
	return GeneratedFMP4NativeClock{TrackID: id, MediaTimeScale: scale, HeaderDurationUnits: duration,
		HeaderDurationKnown: known, EditPresent: edits.kind != ""}, nil
}

func generatedFMP4NativeFragment(data []byte, trackID uint32) (GeneratedFMP4FragmentClock, error) {
	var empty GeneratedFMP4FragmentClock
	parser := progressiveVideoParser{}
	children, err := parser.children(data)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	sequence, err := progressiveVideoOne(children, "mfhd", true)
	if err != nil || len(sequence.payload) != 8 || binary.BigEndian.Uint32(sequence.payload[:4]) != 0 {
		return empty, ErrTimelineProbe
	}
	track, err := progressiveVideoOne(children, "traf", true)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	trackChildren, err := parser.children(track.payload)
	if err != nil {
		return empty, ErrTimelineProbe
	}
	header, err := progressiveVideoOne(trackChildren, "tfhd", true)
	if err != nil || len(header.payload) < 8 || header.payload[0] != 0 || binary.BigEndian.Uint32(header.payload[4:8]) != trackID {
		return empty, ErrTimelineProbe
	}
	decode, err := progressiveVideoOne(trackChildren, "tfdt", true)
	if err != nil || len(decode.payload) < 8 || binary.BigEndian.Uint32(decode.payload[:4])&0xffffff != 0 {
		return empty, ErrTimelineProbe
	}
	var units uint64
	switch {
	case decode.payload[0] == 0 && len(decode.payload) == 8:
		units = uint64(binary.BigEndian.Uint32(decode.payload[4:]))
	case decode.payload[0] == 1 && len(decode.payload) == 12:
		units = binary.BigEndian.Uint64(decode.payload[4:])
	default:
		return empty, ErrTimelineProbe
	}
	var samples int64
	for _, run := range trackChildren {
		if run.kind == "trun" {
			if len(run.payload) < 8 {
				return empty, ErrTimelineProbe
			}
			count := int64(binary.BigEndian.Uint32(run.payload[4:8]))
			if count == 0 || count > maxProgressiveVideoSamples-samples {
				return empty, ErrTimelineLimit
			}
			samples += count
		}
	}
	number := binary.BigEndian.Uint32(sequence.payload[4:8])
	if number == 0 || samples == 0 {
		return empty, ErrTimelineProbe
	}
	return GeneratedFMP4FragmentClock{SequenceNumber: number, TrackID: trackID, DecodeUnits: units, SampleCount: samples}, nil
}

func generatedFMP4NativeDigest(ctx context.Context, reader io.ReaderAt, size int64) ([32]byte, error) {
	var empty [32]byte
	hash := sha256.New()
	var buffer [32 << 10]byte
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		length := min(int64(len(buffer)), size-offset)
		if _, err := reader.ReadAt(buffer[:int(length)], offset); err != nil {
			return empty, ErrTimelineProbe
		}
		_, _ = hash.Write(buffer[:int(length)])
		offset += length
	}
	copy(empty[:], hash.Sum(nil))
	return empty, nil
}

// The native reader requires a separate complete framing audit by its caller.
// It reads only bounded metadata and streams whole-byte hashes with ReadAt.
// Unknown boxes, missing clocks and excessive native fragment sets fail closed.
func parseGeneratedFMP4NativeClock(ctx context.Context, initialization io.ReaderAt, initSize int64, segment io.ReaderAt, mediaSize int64) (GeneratedFMP4NativeClock, error) {
	var empty GeneratedFMP4NativeClock
	if ctx == nil || initialization == nil || segment == nil || initSize <= 0 || mediaSize <= 0 {
		return empty, ErrInvalidInput
	}
	if initSize > maxGeneratedBoundsInputBytes || mediaSize > maxGeneratedBoundsInputBytes-initSize {
		return empty, ErrTimelineLimit
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var result GeneratedFMP4NativeClock
	ftyp, movie := false, false
	for offset, count := int64(0), 0; offset < initSize; count++ {
		if count >= 1024 {
			return empty, ErrTimelineLimit
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		box, err := generatedFMP4NativeReadBox(initialization, initSize, offset)
		if err != nil {
			return empty, err
		}
		switch box.kind {
		case "ftyp":
			if offset != 0 || ftyp || movie {
				return empty, ErrTimelineProbe
			}
			ftyp = true
		case "moov":
			if !ftyp || movie {
				return empty, ErrTimelineProbe
			}
			data, err := generatedFMP4NativeMetadata(initialization, box)
			if err != nil {
				return empty, err
			}
			result, err = generatedFMP4NativeMovie(data)
			if err != nil {
				return empty, err
			}
			movie = true
		case "free":
			if !ftyp || box.end-box.body > 1<<20 {
				return empty, ErrTimelineProbe
			}
		default:
			return empty, ErrTimelineProbe
		}
		offset = box.end
	}
	if !ftyp || !movie {
		return empty, ErrTimelineProbe
	}
	for offset, count := int64(0), 0; offset < mediaSize; count++ {
		if count >= 1024 {
			return empty, ErrTimelineLimit
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		box, err := generatedFMP4NativeReadBox(segment, mediaSize, offset)
		if err != nil {
			return empty, err
		}
		switch box.kind {
		case "moof":
			if result.FragmentCount >= maxGeneratedFMP4NativeFragments {
				return empty, ErrTimelineLimit
			}
			data, err := generatedFMP4NativeMetadata(segment, box)
			if err != nil {
				return empty, err
			}
			clock, err := generatedFMP4NativeFragment(data, result.TrackID)
			if err != nil {
				return empty, err
			}
			if result.FragmentCount > 0 {
				last := result.Fragments[result.FragmentCount-1]
				if clock.SequenceNumber <= last.SequenceNumber || clock.DecodeUnits <= last.DecodeUnits {
					return empty, ErrTimelineProbe
				}
			}
			result.Fragments[result.FragmentCount] = clock
			result.FragmentCount++
			result.TotalSamples += clock.SampleCount
		case "styp", "sidx", "mdat":
		default:
			return empty, ErrTimelineProbe
		}
		offset = box.end
	}
	if result.FragmentCount == 0 {
		return empty, ErrTimelineProbe
	}
	var err error
	result.InitializationSHA256, err = generatedFMP4NativeDigest(ctx, initialization, initSize)
	if err != nil {
		return empty, err
	}
	result.SegmentSHA256, err = generatedFMP4NativeDigest(ctx, segment, mediaSize)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
