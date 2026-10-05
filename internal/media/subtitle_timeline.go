package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"time"
)

const (
	SubtitleTimelineProfile               = "subtitle-timeline-v1;source=bitmap-display;clock=container;intervals=union"
	SubtitleTimelineExternalProfile       = "subtitle-timeline-v2;source=bitmap-display;clock=container;intervals=union"
	MaxSubtitleTimelineBytes        int64 = 8 << 20
	MaxSubtitleTimelineTracks             = 64
	subtitleTimelineDurationLimit         = 7 * 24 * 60 * 60 * TicksPerSecond
)

var ErrSubtitleTimelineUnsupported = errors.New("subtitle timeline source is unsupported")

func IsSubtitleTimelineProfile(profile string) bool {
	return profile == SubtitleTimelineProfile || profile == SubtitleTimelineExternalProfile
}

// SubtitleTimelineInterval is visible subtitle coverage on the source playback
// clock. Intervals are half-open and never contain invented gap coverage.
type SubtitleTimelineInterval struct {
	StartTicks int64
	EndTicks   int64
}

type SubtitleTimelineTrackSummary struct {
	StreamIndex   int
	Codec         string
	IntervalCount int
	Warnings      []string
}

type SubtitleTimelineTrack struct {
	SubtitleTimelineTrackSummary
	Intervals []SubtitleTimelineInterval
}

type SubtitleTimelineData struct {
	Profile       string
	FFprobeSHA256 string
	DurationTicks int64
	Tracks        []SubtitleTimelineTrack
}

type SubtitleTimelineSummary struct {
	Profile       string
	FFprobeSHA256 string
	DurationTicks int64
	Bytes         int64
	Tracks        []SubtitleTimelineTrackSummary
}

type SubtitleTimelineProgress struct {
	StreamIndex     int
	CompletedTracks int
	TotalTracks     int
}

// GenerateSubtitleTimelines borrows the authorized source descriptor and emits
// one GSTL artifact only after every admitted internal bitmap track succeeds.
// Each decoder raster is discarded synchronously after collecting its interval;
// no PNG encoding, OCR engine, video decoder, or language model is involved.
// The caller owns atomic publication and must discard output on any error.
func GenerateSubtitleTimelines(ctx context.Context, config BitmapSubtitleConfig, input *os.File, info Info, output io.Writer, progress func(SubtitleTimelineProgress)) (summary SubtitleTimelineSummary, resultErr error) {
	return GenerateSubtitleTimelinesWithExternal(ctx, config, input, info, nil, output, progress)
}

// GenerateSubtitleTimelinesWithExternal combines internal bitmap streams with
// authorized sidecars. Every track must succeed before any output is emitted.
// Internal-only generations retain the legacy profile and byte format.
func GenerateSubtitleTimelinesWithExternal(ctx context.Context, config BitmapSubtitleConfig, input *os.File, info Info, external []ExternalSubtitleTimelineInput, output io.Writer, progress func(SubtitleTimelineProgress)) (summary SubtitleTimelineSummary, resultErr error) {
	if ctx == nil || output == nil {
		return summary, fmt.Errorf("%w: missing timeline context or output", ErrBitmapSubtitle)
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	streams, err := subtitleTimelineStreams(info)
	if err != nil && !(errors.Is(err, ErrSubtitleTimelineUnsupported) && len(external) > 0) {
		return summary, err
	}
	if len(streams)+len(external) > MaxSubtitleTimelineTracks {
		return summary, fmt.Errorf("%w: subtitle timeline track limit", ErrAnalysisBudget)
	}
	seen := make(map[int]bool)
	for _, stream := range info.Streams {
		if !stream.IsExternal {
			seen[stream.Index] = true
		}
	}
	externalFiles := make(map[*os.File]os.FileInfo)
	for ordinal, track := range external {
		if track.StreamIndex < 0 || int64(track.StreamIndex) > 1<<31-1 || seen[track.StreamIndex] || track.SourceStreamIndex < 0 || track.SourceStreamIndex > 31 ||
			!subtitleTimelineCodecSupported(track.Codec) || track.Input == nil ||
			(track.Codec == "hdmv_pgs_subtitle" && (track.SourceStreamIndex != 0 || track.Companion != nil)) ||
			(track.Codec == "dvd_subtitle" && track.Companion == nil) {
			return summary, fmt.Errorf("%w: invalid external subtitle timeline track", ErrBitmapSubtitle)
		}
		seen[track.StreamIndex] = true
		for _, file := range []*os.File{track.Input, track.Companion} {
			if file == nil {
				continue
			}
			stat, statErr := file.Stat()
			if statErr != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > MaxExternalBitmapSubtitleBytes {
				return summary, fmt.Errorf("%w: invalid external subtitle timeline file", ErrBitmapSubtitle)
			}
			externalFiles[file] = stat
		}
		for _, previous := range external[:ordinal] {
			if !os.SameFile(externalFiles[track.Input], externalFiles[previous.Input]) {
				continue
			}
			if previous.SourceStreamIndex == track.SourceStreamIndex || previous.Codec != track.Codec ||
				(track.Companion != nil && (previous.Companion == nil || !os.SameFile(externalFiles[track.Companion], externalFiles[previous.Companion]))) {
				return summary, fmt.Errorf("%w: duplicated external track or inconsistent companion", ErrBitmapSubtitle)
			}
		}
	}
	if runtime.GOOS != "linux" {
		return summary, fmt.Errorf("%w: Linux is required", ErrSubtitleTimelineUnsupported)
	}
	if input == nil || info.DurationTicks <= 0 || info.DurationTicks > subtitleTimelineDurationLimit ||
		info.FormatStartTicks < -subtitleTimelineDurationLimit || info.FormatStartTicks > subtitleTimelineDurationLimit ||
		(!info.FormatStartKnown && info.FormatStartTicks != 0) {
		return summary, fmt.Errorf("%w: invalid timeline source facts", ErrBitmapSubtitle)
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxSubtitleRemovalInputBytes ||
		info.Size != before.Size() || info.FileChangeTimeNs != FileChangeTime(before) {
		return summary, fmt.Errorf("%w: timeline source identity does not match", ErrBitmapSubtitle)
	}
	bounded, release, err := analysisAcquire(ctx, 2*time.Hour)
	if err != nil {
		return summary, err
	}
	defer release()
	tool, toolBefore, err := openPinnedSubtitleFile(bounded, config.FFprobePath, config.FFprobeSHA256, 128<<20)
	if err != nil {
		return summary, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
	}
	defer tool.Close()
	check := func() error {
		if err := bounded.Err(); err != nil {
			return err
		}
		if !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(tool, toolBefore) {
			return fmt.Errorf("%w: timeline source or tool changed", ErrBitmapSubtitle)
		}
		for file, stat := range externalFiles {
			if !subtitleFileUnchanged(file, stat) {
				return fmt.Errorf("%w: external subtitle changed", ErrBitmapSubtitle)
			}
		}
		return nil
	}
	defer func() {
		if err := check(); err != nil {
			summary, resultErr = SubtitleTimelineSummary{}, errors.Join(resultErr, err)
		}
	}()
	data := SubtitleTimelineData{Profile: SubtitleTimelineProfile, FFprobeSHA256: config.FFprobeSHA256, DurationTicks: info.DurationTicks}
	if len(external) > 0 {
		data.Profile = SubtitleTimelineExternalProfile
	}
	totalTracks := len(streams) + len(external)
	for ordinal, stream := range streams {
		if progress != nil {
			progress(SubtitleTimelineProgress{StreamIndex: stream.Index, CompletedTracks: ordinal, TotalTracks: totalTracks})
		}
		if err := check(); err != nil {
			return summary, err
		}
		collector := subtitleTimelineCollector{duration: info.DurationTicks}
		warnings, err := walkBitmapSubtitles(bounded, config, input, stream, info, func(cue BitmapSubtitleCue) error {
			return collector.add(bounded, cue)
		})
		if err != nil {
			if errors.Is(err, errBitmapSubtitleUnsupported) {
				err = errors.Join(ErrSubtitleTimelineUnsupported, err)
			}
			return summary, fmt.Errorf("subtitle timeline stream %d: %w", stream.Index, err)
		}
		intervals := collector.union()
		data.Tracks = append(data.Tracks, SubtitleTimelineTrack{
			SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{StreamIndex: stream.Index, Codec: stream.Codec, IntervalCount: len(intervals), Warnings: append([]string(nil), warnings...)},
			Intervals:                    intervals,
		})
		if progress != nil {
			progress(SubtitleTimelineProgress{StreamIndex: stream.Index, CompletedTracks: ordinal + 1, TotalTracks: totalTracks})
		}
	}
	for ordinal, stream := range external {
		if progress != nil {
			progress(SubtitleTimelineProgress{StreamIndex: stream.StreamIndex, CompletedTracks: len(streams) + ordinal, TotalTracks: totalTracks})
		}
		if err := check(); err != nil {
			return summary, err
		}
		collector := subtitleTimelineCollector{duration: info.DurationTicks}
		warnings, err := walkExternalBitmapSubtitles(bounded, stream, info.DurationTicks, func(cue BitmapSubtitleCue) error { return collector.add(bounded, cue) })
		if err != nil {
			if errors.Is(err, errBitmapSubtitleUnsupported) {
				err = errors.Join(ErrSubtitleTimelineUnsupported, err)
			}
			return summary, fmt.Errorf("subtitle timeline stream %d: %w", stream.StreamIndex, err)
		}
		intervals := collector.union()
		data.Tracks = append(data.Tracks, SubtitleTimelineTrack{
			SubtitleTimelineTrackSummary: SubtitleTimelineTrackSummary{StreamIndex: stream.StreamIndex, Codec: stream.Codec, IntervalCount: len(intervals), Warnings: append([]string(nil), warnings...)},
			Intervals:                    intervals,
		})
		if progress != nil {
			progress(SubtitleTimelineProgress{StreamIndex: stream.StreamIndex, CompletedTracks: len(streams) + ordinal + 1, TotalTracks: totalTracks})
		}
	}
	sort.Slice(data.Tracks, func(i, j int) bool { return data.Tracks[i].StreamIndex < data.Tracks[j].StreamIndex })
	encoded, err := MarshalSubtitleTimelines(data)
	if err != nil {
		return summary, err
	}
	if err := check(); err != nil {
		return summary, err
	}
	if _, err := io.Copy(output, bytes.NewReader(encoded)); err != nil {
		return summary, err
	}
	return data.Summary(int64(len(encoded))), nil
}

func (data SubtitleTimelineData) Summary(size int64) SubtitleTimelineSummary {
	result := SubtitleTimelineSummary{Profile: data.Profile, FFprobeSHA256: data.FFprobeSHA256, DurationTicks: data.DurationTicks, Bytes: size, Tracks: make([]SubtitleTimelineTrackSummary, len(data.Tracks))}
	for index, track := range data.Tracks {
		result.Tracks[index] = track.SubtitleTimelineTrackSummary
		result.Tracks[index].Warnings = append([]string(nil), track.Warnings...)
	}
	return result
}

func subtitleTimelineStreams(info Info) ([]Stream, error) {
	streams := make([]Stream, 0)
	seen := make(map[int]bool)
	for _, stream := range info.Streams {
		if stream.IsExternal {
			continue
		}
		if stream.Index < 0 || stream.Index > 4095 || seen[stream.Index] {
			return nil, fmt.Errorf("%w: invalid or ambiguous internal stream index", ErrBitmapSubtitle)
		}
		seen[stream.Index] = true
		if stream.CodecType != "subtitle" || !subtitleTimelineCodecSupported(stream.Codec) {
			continue
		}
		if stream.IsAttachedPicture {
			return nil, fmt.Errorf("%w: invalid subtitle stream disposition", ErrBitmapSubtitle)
		}
		streams = append(streams, stream)
		if len(streams) > MaxSubtitleTimelineTracks {
			return nil, fmt.Errorf("%w: subtitle timeline track limit", ErrAnalysisBudget)
		}
	}
	if len(streams) == 0 {
		return nil, ErrSubtitleTimelineUnsupported
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Index < streams[j].Index })
	return streams, nil
}

func subtitleTimelineCodecSupported(codec string) bool {
	return codec == "hdmv_pgs_subtitle" || codec == "dvd_subtitle"
}

type subtitleTimelineCollector struct {
	duration  int64
	intervals []SubtitleTimelineInterval
}

func (collector *subtitleTimelineCollector) add(ctx context.Context, cue BitmapSubtitleCue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cue.StartTicks < 0 || cue.EndTicks <= cue.StartTicks || cue.EndTicks > collector.duration || len(collector.intervals) >= MaxSubtitleOCRCues {
		return fmt.Errorf("%w: invalid or excessive subtitle timeline intervals", ErrBitmapSubtitle)
	}
	collector.intervals = append(collector.intervals, SubtitleTimelineInterval{StartTicks: cue.StartTicks, EndTicks: cue.EndTicks})
	return nil
}

func (collector *subtitleTimelineCollector) union() []SubtitleTimelineInterval {
	values := collector.intervals
	sort.Slice(values, func(i, j int) bool {
		if values[i].StartTicks == values[j].StartTicks {
			return values[i].EndTicks < values[j].EndTicks
		}
		return values[i].StartTicks < values[j].StartTicks
	})
	count := 0
	for _, value := range values {
		if count > 0 && value.StartTicks <= values[count-1].EndTicks {
			values[count-1].EndTicks = max(values[count-1].EndTicks, value.EndTicks)
			continue
		}
		values[count] = value
		count++
	}
	collector.intervals = values[:count]
	return collector.intervals
}
