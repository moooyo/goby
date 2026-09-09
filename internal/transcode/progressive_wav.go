package transcode

import (
	"encoding/binary"
	"fmt"
	"math"
)

func progressiveSampleLimit(ticks int64, rate int) int64 {
	return ticks/ticksPerSecond*int64(rate) + (ticks%ticksPerSecond*int64(rate)+ticksPerSecond-1)/ticksPerSecond
}

func progressiveSampleSeekTicks(p Plan) int64 {
	if p.AudioSourceSampleCount == 0 || p.StartTicks == 0 || p.AudioCodec == "copy" && p.Container != "wav" {
		return p.StartTicks
	}
	samples := progressiveSampleLimit(p.StartTicks, p.AudioSourceSampleRate)
	rate := int64(p.AudioSourceSampleRate)
	// FFmpeg's input seek uses microseconds. Choose the nearest representable
	// point to the selected source sample, rather than silently truncating a
	// 100-nanosecond request to the preceding sample/time-base position.
	microseconds := samples/rate*1_000_000 + (samples%rate*1_000_000+rate/2)/rate
	return microseconds * 10
}

// ProgressiveOutputSamples derives an output sample-frame limit without
// turning an outward-rounded duration back into an extra source sample. Exact
// counts are for a selected audio presentation whose sample origin is zero.
// DurationTicks may cover a longer sibling stream, so equality is not required.
func ProgressiveOutputSamples(p Plan, outputRate int) (int64, error) {
	invalid := func() (int64, error) { return 0, fmt.Errorf("%w: audio sample timeline", ErrInvalidPlan) }
	if outputRate <= 0 || outputRate > 768000 || p.DurationTicks <= 0 || p.DurationTicks > maxDurationTicks || p.StartTicks < 0 || p.StartTicks >= p.DurationTicks || p.AudioSourceSampleCount < 0 || p.AudioSourceSampleRate < 0 || p.AudioSourceSampleRate > 768000 {
		return invalid()
	}
	if p.AudioSourceSampleCount == 0 {
		return progressiveSampleLimit(p.DurationTicks-p.StartTicks, outputRate), nil
	}
	sourceRate := int64(p.AudioSourceSampleRate)
	if sourceRate == 0 || p.AudioSourceSampleCount > maxDurationTicks/ticksPerSecond*sourceRate {
		return invalid()
	}
	// Whole-source duration is an upper bound, not necessarily the selected
	// stream's own duration. Flooring is correct even when that bound was
	// rounded outwards to the next 100-nanosecond tick.
	maxSourceSamples := p.DurationTicks/ticksPerSecond*sourceRate + p.DurationTicks%ticksPerSecond*sourceRate/ticksPerSecond
	if p.AudioSourceSampleCount > maxSourceSamples {
		return invalid()
	}
	skip := progressiveSampleLimit(p.StartTicks, p.AudioSourceSampleRate)
	if skip >= p.AudioSourceSampleCount {
		return invalid()
	}
	remaining := p.AudioSourceSampleCount - skip
	rate := int64(outputRate)
	return remaining/sourceRate*rate + (remaining%sourceRate*rate+sourceRate-1)/sourceRate, nil
}

func progressivePCMFormat(channels, rate int) []byte {
	size := 16
	if channels > 2 {
		size = 40
	}
	format := make([]byte, size)
	binary.LittleEndian.PutUint16(format, 1)
	binary.LittleEndian.PutUint16(format[2:], uint16(channels))
	binary.LittleEndian.PutUint32(format[4:], uint32(rate))
	binary.LittleEndian.PutUint32(format[8:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(format[12:], uint16(channels*2))
	binary.LittleEndian.PutUint16(format[14:], 16)
	if channels > 2 {
		binary.LittleEndian.PutUint16(format, 0xfffe)
		binary.LittleEndian.PutUint16(format[16:], 22)
		binary.LittleEndian.PutUint16(format[18:], 16)
		masks := []uint32{0, 4, 3, 7, 0x33, 0x37, 0x3f, 0x70f, 0x63f}
		binary.LittleEndian.PutUint32(format[20:], masks[channels])
		copy(format[24:], []byte{1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71})
	}
	return format
}

func progressiveWAVHeader(format []byte, samples int64) ([]byte, int64, error) {
	alignment, err := progressiveWAVFormat(format)
	if err != nil || samples <= 0 || samples > (math.MaxUint32-int64(20+len(format)))/int64(alignment) {
		return nil, 0, fmt.Errorf("%w: classic WAV size", ErrInvalidPlan)
	}
	dataBytes := samples * int64(alignment)
	header := make([]byte, 28+len(format))
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(dataBytes+int64(len(header))-8))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], uint32(len(format)))
	copy(header[20:], format)
	copy(header[20+len(format):], "data")
	binary.LittleEndian.PutUint32(header[24+len(format):], uint32(dataBytes))
	return header, dataBytes, nil
}
