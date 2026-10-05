package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"
)

const (
	AudioWaveformProfile            = "audio-waveform-v1;pcm=f32le;channels=peak-energy;levels=512,1024,2048,4096;quantization=u16-linear"
	MaxAudioWaveformBytes     int64 = 4 << 20
	MaxAudioWaveformTracks          = 64
	audioWaveformFinest             = 4096
	audioWaveformFrameSamples       = 65536
)

var ErrAudioWaveformUnsupported = errors.New("audio waveform source is unsupported")

// AudioWaveformLevel stores linear amplitudes and a little-bit-first validity
// map. A valid zero bucket contains digital silence; an invalid bucket has no
// decoded presentation samples. RMS is an energy average over all channels.
type AudioWaveformLevel struct {
	BucketCount int
	Peaks       []uint16
	RMS         []uint16
	Validity    []byte
}

type AudioWaveformTrackSummary struct {
	StreamIndex        int
	Channels           int
	SampleRate         int
	ChannelLayout      string
	SampleCount        int64
	CoverageStartTicks int64
	CoverageEndTicks   int64
}

type AudioWaveformTrack struct {
	AudioWaveformTrackSummary
	Levels []AudioWaveformLevel
}

type AudioWaveformData struct {
	Profile       string
	FFmpegSHA256  string
	DurationTicks int64
	Tracks        []AudioWaveformTrack
}

type AudioWaveformSummary struct {
	Profile       string
	FFmpegSHA256  string
	DurationTicks int64
	Bytes         int64
	Tracks        []AudioWaveformTrackSummary
}

type AudioWaveformProgress struct {
	StreamIndex     int
	CompletedTracks int
	TotalTracks     int
	PositionTicks   int64
	DurationTicks   int64
}

// GenerateAudioWaveforms emits a complete, validated GAWF artifact only after
// every admitted audio track has decoded successfully. It borrows the source
// descriptor, does not create files, and never owns publication or retention.
// Only compact accumulators and a single PCM frame are held in memory. Tracks
// run sequentially to bound decoder concurrency; later tracks are not omitted.
func (extractor AnalysisExtractor) GenerateAudioWaveforms(ctx context.Context, source *os.File, info Info, output io.Writer, progress func(AudioWaveformProgress)) (summary AudioWaveformSummary, resultErr error) {
	if ctx == nil || output == nil {
		return summary, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	before, err := analysisCheckSource(source, info)
	if err != nil {
		return summary, err
	}
	streams, err := audioWaveformStreams(info)
	if err != nil {
		return summary, err
	}
	timeout := extractor.Limits.Timeout
	if timeout == 0 {
		timeout = time.Hour
	}
	bounded, release, err := analysisAcquire(ctx, timeout)
	if err != nil {
		return summary, err
	}
	defer release()
	defer func() {
		if err := errors.Join(ctx.Err(), bounded.Err(), videoSeekCheckSource(source, before)); err != nil {
			summary, resultErr = AudioWaveformSummary{}, errors.Join(resultErr, err)
		}
	}()
	ffmpeg, err := analysisOpenToolExpected(bounded, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return summary, err
	}
	defer ffmpeg.file.Close()
	defer func() {
		if err := ffmpeg.check(); err != nil {
			summary, resultErr = AudioWaveformSummary{}, errors.Join(resultErr, err)
		}
	}()
	if err := analysisValidateFFmpeg(bounded, ffmpeg); err != nil {
		return summary, err
	}
	data := AudioWaveformData{Profile: AudioWaveformProfile, FFmpegSHA256: ffmpeg.sha, DurationTicks: info.DurationTicks}
	for ordinal, stream := range streams {
		if err := bounded.Err(); err != nil {
			return summary, err
		}
		report := func(position int64) {
			if progress != nil {
				progress(AudioWaveformProgress{StreamIndex: stream.Index, CompletedTracks: ordinal, TotalTracks: len(streams), PositionTicks: position, DurationTicks: info.DurationTicks})
			}
		}
		report(0)
		track, err := extractAudioWaveformTrack(bounded, source, info, stream, ffmpeg, timeout, report)
		if err != nil {
			return summary, fmt.Errorf("audio waveform stream %d: %w", stream.Index, err)
		}
		data.Tracks = append(data.Tracks, track)
		if progress != nil {
			progress(AudioWaveformProgress{StreamIndex: stream.Index, CompletedTracks: ordinal + 1, TotalTracks: len(streams), PositionTicks: info.DurationTicks, DurationTicks: info.DurationTicks})
		}
	}
	encoded, err := MarshalAudioWaveforms(data)
	if err != nil {
		return summary, err
	}
	if err := errors.Join(bounded.Err(), videoSeekCheckSource(source, before), ffmpeg.check()); err != nil {
		return summary, err
	}
	if _, err := io.Copy(output, bytes.NewReader(encoded)); err != nil {
		return summary, err
	}
	return data.Summary(int64(len(encoded))), nil
}

func (data AudioWaveformData) Summary(size int64) AudioWaveformSummary {
	result := AudioWaveformSummary{Profile: data.Profile, FFmpegSHA256: data.FFmpegSHA256, DurationTicks: data.DurationTicks, Bytes: size, Tracks: make([]AudioWaveformTrackSummary, len(data.Tracks))}
	for index, track := range data.Tracks {
		result.Tracks[index] = track.AudioWaveformTrackSummary
	}
	return result
}

func audioWaveformStreams(info Info) ([]Stream, error) {
	var streams []Stream
	seen := make(map[int]bool, len(info.Streams))
	for _, stream := range info.Streams {
		if stream.Index < 0 || stream.Index > 4095 || seen[stream.Index] {
			return nil, ErrAudioWaveformUnsupported
		}
		seen[stream.Index] = true
		if stream.CodecType != "audio" {
			continue
		}
		if stream.IsExternal || stream.IsAttachedPicture || stream.SampleRate < 1 || stream.SampleRate > 384000 || stream.Channels < 1 || stream.Channels > 64 || !audioWaveformLayoutValid(stream.ChannelLayout) {
			return nil, ErrAudioWaveformUnsupported
		}
		if _, err := analysisTimeBase(stream.TimeBase); err != nil {
			return nil, ErrAudioWaveformUnsupported
		}
		streams = append(streams, stream)
	}
	if len(streams) == 0 || len(streams) > MaxAudioWaveformTracks {
		return nil, ErrAudioWaveformUnsupported
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	return streams, nil
}

func audioWaveformArgs(stream Stream) []string {
	filter := "aformat=sample_fmts=flt,asettb=expr=1/" + strconv.Itoa(stream.SampleRate) + ",ashowinfo@audio_waveform"
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "repeat+level+info", "-debug_ts", "-xerror", "-max_alloc", "268435456", "-copyts", "-fflags", "+nofillin-genpts", "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-reinit_filter", "0", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-vn", "-sn", "-dn", "-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(stream.Index), "-vn", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1", "-af", filter, "-c:a", "pcm_f32le", "-threads:a", "1", "-f", "f32le", "-flush_packets", "1", "pipe:1"}
}

func extractAudioWaveformTrack(ctx context.Context, source *os.File, info Info, stream Stream, ffmpeg *analysisTool, timeout time.Duration, progress func(int64)) (AudioWaveformTrack, error) {
	log, err := newAudioWaveformLog(stream, info.DurationTicks)
	if err != nil {
		return AudioWaveformTrack{}, err
	}
	origin := info.FormatStartTicks
	if info.AudioDurationExact {
		origin = info.PresentationOriginTicks
	}
	accumulator := newAudioWaveformAccumulator(info.DurationTicks, origin, stream)
	// The budget counts streamed bytes, not retained memory. It admits the full
	// source plus a bounded leading/trailing decoder window without truncation.
	maxFrames := ((info.DurationTicks+2*TicksPerSecond)*int64(stream.SampleRate) + TicksPerSecond - 1) / TicksPerSecond
	limit := maxFrames * int64(stream.Channels) * 4
	err = runAudioWaveformProcess(ctx, source, ffmpeg, audioWaveformArgs(stream), timeout, limit, log, func(reader io.Reader) error {
		buffer := make([]byte, audioWaveformFrameSamples*stream.Channels*4)
		lastProgress := int64(-1)
		for {
			frame, err := log.next(ctx)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			data := buffer[:frame.samples*stream.Channels*4]
			if _, err := io.ReadFull(reader, data); err != nil {
				return fmt.Errorf("%w: incomplete waveform PCM: %v", ErrAnalysisUnproven, err)
			}
			if analysisPCMChecksum(data) != frame.checksum {
				return fmt.Errorf("%w: waveform PCM checksum mismatch", ErrAnalysisUnproven)
			}
			if err := accumulator.add(frame.pts, data); err != nil {
				return err
			}
			position := accumulator.endTicks
			if position/TicksPerSecond != lastProgress {
				progress(position)
				lastProgress = position / TicksPerSecond
			}
		}
	})
	if err != nil {
		return AudioWaveformTrack{}, err
	}
	if err := log.result(); err != nil {
		return AudioWaveformTrack{}, err
	}
	return accumulator.finish()
}
