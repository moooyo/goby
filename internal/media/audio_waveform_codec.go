package media

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

var audioWaveformMagic = [8]byte{'G', 'A', 'W', 'F', 0, 1, '\r', '\n'}
var audioWaveformBucketCounts = [...]int{512, 1024, 2048, 4096}

// GAWF v1 is little endian. Its header contains magic[8], duration i64,
// FFmpeg SHA-256[32], track-count u16, and reserved u16. Each track contains
// index u16, channels u16, rate u32, sample-count i64, coverage-start/end i64,
// layout-length u16, reserved u16, and UTF-8 layout bytes. Each of its four
// fixed levels contains bucket-count u32, interleaved peak/RMS u16 pairs, then
// a little-bit-first validity map. No paths, credentials, or source IDs occur.
func MarshalAudioWaveforms(data AudioWaveformData) ([]byte, error) {
	if err := validateAudioWaveformData(data); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.Write(audioWaveformMagic[:])
	put := func(value any) { _ = binary.Write(&output, binary.LittleEndian, value) }
	put(data.DurationTicks)
	hash, _ := hex.DecodeString(data.FFmpegSHA256)
	output.Write(hash)
	put(uint16(len(data.Tracks)))
	put(uint16(0))
	for _, track := range data.Tracks {
		put(uint16(track.StreamIndex))
		put(uint16(track.Channels))
		put(uint32(track.SampleRate))
		put(track.SampleCount)
		put(track.CoverageStartTicks)
		put(track.CoverageEndTicks)
		put(uint16(len(track.ChannelLayout)))
		put(uint16(0))
		output.WriteString(track.ChannelLayout)
		for _, level := range track.Levels {
			put(uint32(level.BucketCount))
			for index, peak := range level.Peaks {
				put(peak)
				put(level.RMS[index])
			}
			output.Write(level.Validity)
		}
	}
	if int64(output.Len()) > MaxAudioWaveformBytes {
		return nil, ErrAnalysisBudget
	}
	return output.Bytes(), nil
}

// ParseAudioWaveforms rejects malformed, trailing, oversized, and unsupported
// artifacts before allocating their arrays. Returned values own their storage.
func ParseAudioWaveforms(encoded []byte) (AudioWaveformData, error) {
	var data AudioWaveformData
	if len(encoded) < 52 || int64(len(encoded)) > MaxAudioWaveformBytes {
		return data, ErrAnalysisUnproven
	}
	reader := bytes.NewReader(encoded)
	read := func(value any) error { return binary.Read(reader, binary.LittleEndian, value) }
	var magic [8]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil || magic != audioWaveformMagic {
		return data, ErrAnalysisUnproven
	}
	if err := read(&data.DurationTicks); err != nil {
		return data, ErrAnalysisUnproven
	}
	var hash [32]byte
	if _, err := io.ReadFull(reader, hash[:]); err != nil {
		return data, ErrAnalysisUnproven
	}
	data.Profile, data.FFmpegSHA256 = AudioWaveformProfile, hex.EncodeToString(hash[:])
	var count, reserved uint16
	if read(&count) != nil || read(&reserved) != nil || count == 0 || count > MaxAudioWaveformTracks || reserved != 0 {
		return AudioWaveformData{}, ErrAnalysisUnproven
	}
	for range int(count) {
		var track AudioWaveformTrack
		var index, channels, layoutLength uint16
		var rate uint32
		if read(&index) != nil || read(&channels) != nil || read(&rate) != nil || read(&track.SampleCount) != nil || read(&track.CoverageStartTicks) != nil || read(&track.CoverageEndTicks) != nil || read(&layoutLength) != nil || read(&reserved) != nil || layoutLength > 128 || reserved != 0 {
			return AudioWaveformData{}, ErrAnalysisUnproven
		}
		track.StreamIndex, track.Channels, track.SampleRate = int(index), int(channels), int(rate)
		layout := make([]byte, layoutLength)
		if _, err := io.ReadFull(reader, layout); err != nil {
			return AudioWaveformData{}, ErrAnalysisUnproven
		}
		track.ChannelLayout = string(layout)
		for _, expected := range audioWaveformBucketCounts {
			var buckets uint32
			if read(&buckets) != nil || int(buckets) != expected || reader.Len() < expected*4+expected/8 {
				return AudioWaveformData{}, ErrAnalysisUnproven
			}
			level := AudioWaveformLevel{BucketCount: expected, Peaks: make([]uint16, expected), RMS: make([]uint16, expected), Validity: make([]byte, expected/8)}
			for index := range expected {
				if read(&level.Peaks[index]) != nil || read(&level.RMS[index]) != nil {
					return AudioWaveformData{}, ErrAnalysisUnproven
				}
			}
			if _, err := io.ReadFull(reader, level.Validity); err != nil {
				return AudioWaveformData{}, ErrAnalysisUnproven
			}
			track.Levels = append(track.Levels, level)
		}
		data.Tracks = append(data.Tracks, track)
	}
	if reader.Len() != 0 {
		return AudioWaveformData{}, ErrAnalysisUnproven
	}
	if err := validateAudioWaveformData(data); err != nil {
		return AudioWaveformData{}, err
	}
	return data, nil
}

func audioWaveformLayoutValid(layout string) bool {
	if len(layout) > 128 {
		return false
	}
	for _, ch := range layout {
		if ch < 32 || ch > 126 {
			return false
		}
	}
	return true
}

func validateAudioWaveformData(data AudioWaveformData) error {
	bad := func() error { return fmt.Errorf("%w: invalid waveform artifact", ErrAnalysisUnproven) }
	hash, err := hex.DecodeString(data.FFmpegSHA256)
	if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != data.FFmpegSHA256 || data.Profile != AudioWaveformProfile || data.DurationTicks <= 0 || data.DurationTicks > MaxAnalysisDurationTicks || len(data.Tracks) == 0 || len(data.Tracks) > MaxAudioWaveformTracks {
		return bad()
	}
	lastIndex := -1
	for _, track := range data.Tracks {
		if track.StreamIndex <= lastIndex || track.StreamIndex > 4095 || track.Channels < 1 || track.Channels > 64 || track.SampleRate < 1 || track.SampleRate > 384000 || !audioWaveformLayoutValid(track.ChannelLayout) || track.SampleCount < 1 || track.SampleCount > (data.DurationTicks*int64(track.SampleRate)+TicksPerSecond-1)/TicksPerSecond+1 || track.CoverageStartTicks < 0 || track.CoverageEndTicks <= track.CoverageStartTicks || track.CoverageEndTicks > data.DurationTicks || len(track.Levels) != len(audioWaveformBucketCounts) {
			return bad()
		}
		lastIndex = track.StreamIndex
		for levelIndex, level := range track.Levels {
			count := audioWaveformBucketCounts[levelIndex]
			if level.BucketCount != count || len(level.Peaks) != count || len(level.RMS) != count || len(level.Validity) != count/8 {
				return bad()
			}
			validCount := 0
			for index, peak := range level.Peaks {
				valid := level.Validity[index/8]&(1<<uint(index%8)) != 0
				if level.RMS[index] > peak || !valid && (peak != 0 || level.RMS[index] != 0) {
					return bad()
				}
				if valid {
					validCount++
				}
				if levelIndex < len(track.Levels)-1 {
					fine := track.Levels[levelIndex+1]
					if len(fine.Peaks) != count*2 || len(fine.Validity) != count/4 {
						return bad()
					}
					left, right := index*2, index*2+1
					fineValid := fine.Validity[left/8]&(1<<uint(left%8)) != 0 || fine.Validity[right/8]&(1<<uint(right%8)) != 0
					if valid != fineValid || peak != max(fine.Peaks[left], fine.Peaks[right]) {
						return bad()
					}
				}
			}
			if validCount == 0 {
				return bad()
			}
		}
	}
	return nil
}
