package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maxProbeOutput = 8 * 1024 * 1024

// Only self-contained formats are accepted. In particular, HLS, DASH, concat,
// and image-sequence manifests could otherwise open files outside the root
// that supplied the primary descriptor. Format detection still uses content.
const probeFormats = "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,avi,asf,flv,ogg," +
	"mp3,flac,wav,aiff,aac,ac3,eac3,dts,truehd,ape,wv,tta,tak,mpc,mpc8,dsf,iff,amr," +
	"mpeg,mpegts,mpegvideo,h264,hevc,av1,ivf,mjpeg,gif,apng,png_pipe,jpeg_pipe," +
	"bmp_pipe,webp_pipe,tiff_pipe,ass,srt,webvtt,lrc,subviewer,subviewer1," +
	"microdvd,mpl2,jacosub,realtext,sami,stl,pjs,vplayer,aqtitle"

// CacheVersion reports the version of facts produced by Probe and ProbeFile.
func (p Prober) CacheVersion() int {
	return CurrentProbeVersion
}

// MusicMetadataVersion identifies the independently refreshable music facts.
// It does not invalidate already accepted technical playback snapshots.
func (p Prober) MusicMetadataVersion() int {
	return CurrentMusicMetadataVersion
}

// Probe accepts regular local files only. Stream indexes are the original
// ffprobe indexes and must not be replaced with positions in a filtered list.
func (p Prober) Probe(ctx context.Context, path string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
		return Info{}, fmt.Errorf("media path must identify a regular local file")
	}
	parsed, err := url.Parse(path)
	if err == nil && parsed.Scheme != "" && !filepath.IsAbs(path) {
		return Info{}, fmt.Errorf("media URLs and protocol inputs are not supported")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return Info{}, fmt.Errorf("resolve media path: %w", err)
	}
	file, err := openLocalMedia(absolutePath)
	if err != nil {
		return Info{}, fmt.Errorf("open media file: %w", err)
	}
	defer file.Close()
	return p.ProbeFile(ctx, file)
}

// ProbeFile borrows a caller-owned regular file without closing or seeking it.
// The caller must keep the file open until this method returns. On Linux the
// child opens its inherited descriptor through procfs, obtaining an independent
// file offset without resolving the original, potentially replaced pathname.
func (p Prober) ProbeFile(ctx context.Context, file *os.File) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if runtime.GOOS != "linux" {
		return Info{}, fmt.Errorf("media probing requires Linux")
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = defaultProcessTimeout
	}
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if file == nil {
		return Info{}, fmt.Errorf("media file is nil")
	}
	stat, err := file.Stat()
	if err != nil {
		return Info{}, fmt.Errorf("stat media file: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return Info{}, fmt.Errorf("media input is not a regular file")
	}
	changeTime := FileChangeTime(stat)
	executable := p.FFprobePath
	if executable == "" {
		executable = "ffprobe"
	}
	output, err := runLimitedFiles(probeContext, timeout, maxProbeOutput, executable, []*os.File{file},
		"-v", "error", "-show_format", "-show_streams", "-show_chapters",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3")
	if err != nil {
		return Info{}, fmt.Errorf("probe media: %w", err)
	}
	info, err := parseProbe(output)
	if err != nil {
		return Info{}, err
	}
	if audioOnly, reason := audioTimingSupport(info); audioOnly {
		info.AudioDurationReason = reason
		if reason == "" {
			accurate, scanErr := runAudioTimingProbe(probeContext, min(timeout, 2*time.Minute), executable, file, info)
			var unproven *audioTimingUnproven
			if errors.As(scanErr, &unproven) {
				info.AudioDurationReason = unproven.Reason
			} else if scanErr != nil {
				return Info{}, fmt.Errorf("scan audio presentation: %w", scanErr)
			} else {
				info = accurate
			}
		}
	}
	if p.AnalyzeVideoSeek && strings.TrimSpace(p.FFmpegPath) != "" {
		info.VideoSeekIndexes, err = AnalyzeVideoSeekIndexes(ctx, p.FFmpegPath, file, info)
		if err != nil {
			return Info{}, fmt.Errorf("analyze video seek evidence: %w", err)
		}
	}
	after, err := file.Stat()
	if err != nil {
		return Info{}, fmt.Errorf("stat media file after probe: %w", err)
	}
	if !os.SameFile(stat, after) || stat.Size() != after.Size() ||
		!stat.ModTime().Equal(after.ModTime()) || changeTime != FileChangeTime(after) {
		return Info{}, fmt.Errorf("media file changed during probe")
	}
	// The held filesystem object is authoritative, including when the pathname
	// was replaced or ffprobe reports a different format.size value.
	info.Size = stat.Size()
	info.FileChangeTimeNs = changeTime
	return info, nil
}

// scalar accepts ffprobe fields emitted as either JSON strings or numbers.
// Missing, null, and N/A values remain distinguishable from malformed values.
type scalar string

func (s *scalar) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	switch value := value.(type) {
	case nil:
		*s = ""
	case string:
		*s = scalar(value)
	case json.Number:
		*s = scalar(value.String())
	default:
		return fmt.Errorf("expected an ffprobe numeric scalar, got %T", value)
	}
	return nil
}

func (s scalar) missing() bool {
	return strings.TrimSpace(string(s)) == "" || strings.EqualFold(string(s), "N/A")
}

func (s scalar) rational() (*big.Rat, error) {
	if len(s) > 256 {
		return nil, fmt.Errorf("numeric value is too long")
	}
	for _, character := range s {
		if !strings.ContainsRune("0123456789+-.eE/", character) {
			return nil, fmt.Errorf("invalid numeric value %q", s)
		}
	}
	if position := strings.IndexAny(string(s), "eE"); position >= 0 {
		exponent, err := strconv.ParseInt(string(s[position+1:]), 10, 64)
		if err != nil || exponent < -1000 || exponent > 1000 {
			return nil, fmt.Errorf("numeric exponent is out of range")
		}
	}
	value, ok := new(big.Rat).SetString(string(s))
	if !ok {
		return nil, fmt.Errorf("invalid numeric value %q", s)
	}
	return value, nil
}

func (s scalar) integer() (int64, error) {
	if s.missing() {
		return 0, nil
	}
	value, err := s.rational()
	if err != nil {
		return 0, err
	}
	if !value.IsInt() || !value.Num().IsInt64() {
		return 0, fmt.Errorf("value %q is not an int64", s)
	}
	return value.Num().Int64(), nil
}

// probeBoolean preserves the distinction between false and an absent fact.
type probeBoolean struct {
	value bool
	known bool
}

func (b *probeBoolean) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*b = probeBoolean{}
	switch value := value.(type) {
	case nil:
		return nil
	case bool:
		b.value, b.known = value, true
		return nil
	case string:
		if scalar(value).missing() {
			return nil
		}
		return b.parse(value)
	case json.Number:
		return b.parse(value.String())
	default:
		return fmt.Errorf("expected an ffprobe boolean scalar, got %T", value)
	}
}

func (b *probeBoolean) parse(value string) error {
	switch value {
	case "true", "1":
		b.value, b.known = true, true
	case "false", "0":
		b.value, b.known = false, true
	default:
		return fmt.Errorf("invalid ffprobe boolean value %q", value)
	}
	return nil
}

type probeDocument struct {
	Format struct {
		Name      string          `json:"format_name"`
		Duration  scalar          `json:"duration"`
		StartTime scalar          `json:"start_time"`
		Bitrate   scalar          `json:"bit_rate"`
		Size      scalar          `json:"size"`
		Tags      json.RawMessage `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index          scalar            `json:"index"`
		Codec          string            `json:"codec_name"`
		CodecType      string            `json:"codec_type"`
		Width          scalar            `json:"width"`
		Height         scalar            `json:"height"`
		Channels       scalar            `json:"channels"`
		SampleRate     scalar            `json:"sample_rate"`
		Bitrate        scalar            `json:"bit_rate"`
		Profile        string            `json:"profile"`
		Level          scalar            `json:"level"`
		RawBitDepth    scalar            `json:"bits_per_raw_sample"`
		BitDepth       scalar            `json:"bits_per_sample"`
		CodecTag       string            `json:"codec_tag"`
		CodecTagString string            `json:"codec_tag_string"`
		PixelFormat    string            `json:"pix_fmt"`
		FrameRate      string            `json:"avg_frame_rate"`
		RealFrameRate  string            `json:"r_frame_rate"`
		ChannelLayout  string            `json:"channel_layout"`
		RefFrames      scalar            `json:"refs"`
		FieldOrder     string            `json:"field_order"`
		IsAVC          probeBoolean      `json:"is_avc"`
		ColorRange     string            `json:"color_range"`
		ColorSpace     string            `json:"color_space"`
		ColorTransfer  string            `json:"color_transfer"`
		ColorPrimaries string            `json:"color_primaries"`
		Duration       scalar            `json:"duration"`
		DurationTS     scalar            `json:"duration_ts"`
		TimeBase       scalar            `json:"time_base"`
		Tags           map[string]string `json:"tags"`
		Disposition    struct {
			Default         scalar `json:"default"`
			Forced          scalar `json:"forced"`
			AttachedPicture scalar `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Chapters []struct {
		Start     scalar            `json:"start"`
		End       scalar            `json:"end"`
		TimeBase  scalar            `json:"time_base"`
		StartTime scalar            `json:"start_time"`
		EndTime   scalar            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

func parseProbe(data []byte) (Info, error) {
	var document probeDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return Info{}, fmt.Errorf("decode ffprobe JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Info{}, fmt.Errorf("ffprobe returned trailing data")
	}
	if document.Format.Name == "" || len(document.Streams) == 0 {
		return Info{}, fmt.Errorf("ffprobe returned no media format or streams")
	}
	info := Info{
		ProbeVersion: CurrentProbeVersion,
		Container:    document.Format.Name,
		Streams:      make([]Stream, 0, len(document.Streams)),
		Chapters:     make([]Chapter, 0, len(document.Chapters)),
	}
	var err error
	if len(document.Format.StartTime) > 256 {
		return Info{}, invalidField("format.start_time", fmt.Errorf("numeric value is too long"))
	}
	if !document.Format.StartTime.missing() {
		info.FormatStartTicks, err = secondsToTicks(document.Format.StartTime)
		if err != nil {
			return Info{}, invalidField("format.start_time", err)
		}
		info.FormatStartKnown = true
	}
	info.DurationTicks, err = secondsToTicks(document.Format.Duration)
	if err != nil || info.DurationTicks < 0 {
		return Info{}, invalidField("format.duration", err)
	}
	info.Bitrate, err = document.Format.Bitrate.integer()
	if err != nil || info.Bitrate < 0 {
		return Info{}, invalidField("format.bit_rate", err)
	}
	info.Size, err = document.Format.Size.integer()
	if err != nil || info.Size < 0 {
		return Info{}, invalidField("format.size", err)
	}
	indexes := make(map[int]bool, len(document.Streams))
	var longestStream int64
	for position, source := range document.Streams {
		stream := Stream{
			Codec:                source.Codec,
			CodecType:            source.CodecType,
			Language:             tagValue(source.Tags, "language"),
			Title:                tagValue(source.Tags, "title"),
			Profile:              source.Profile,
			CodecTag:             source.CodecTag,
			CodecTagString:       source.CodecTagString,
			PixelFormat:          source.PixelFormat,
			TimeBase:             string(source.TimeBase),
			AverageFrameRate:     source.FrameRate,
			RealFrameRate:        source.RealFrameRate,
			ChannelLayout:        source.ChannelLayout,
			FieldOrder:           source.FieldOrder,
			IsAVC:                source.IsAVC.value,
			IsAVCKnown:           source.IsAVC.known,
			ColorRange:           source.ColorRange,
			ColorSpace:           source.ColorSpace,
			ColorTransfer:        source.ColorTransfer,
			ColorPrimaries:       source.ColorPrimaries,
			IsTextSubtitleStream: source.CodecType == "subtitle" && isTextSubtitle(source.Codec),
		}
		if stream.CodecType == "video" {
			switch stream.FieldOrder {
			case "progressive":
				stream.InterlaceKnown = true
			case "tt", "bb", "tb", "bt":
				stream.IsInterlaced, stream.InterlaceKnown = true, true
			}
		}
		if source.Index.missing() {
			return Info{}, fmt.Errorf("stream %d has no index", position)
		}
		var rawBitDepth int
		fields := []struct {
			name   string
			source scalar
			target *int
		}{
			{"index", source.Index, &stream.Index},
			{"width", source.Width, &stream.Width},
			{"height", source.Height, &stream.Height},
			{"channels", source.Channels, &stream.Channels},
			{"sample_rate", source.SampleRate, &stream.SampleRate},
			{"level", source.Level, &stream.Level},
			{"bits_per_raw_sample", source.RawBitDepth, &rawBitDepth},
			{"bits_per_sample", source.BitDepth, &stream.BitDepth},
			{"refs", source.RefFrames, &stream.RefFrames},
		}
		for _, field := range fields {
			value, err := field.source.integer()
			if err != nil || int64(int(value)) != value || (value < 0 && field.name != "level") {
				return Info{}, invalidField(fmt.Sprintf("stream[%d].%s", position, field.name), err)
			}
			*field.target = int(value)
		}
		if rawBitDepth > 0 {
			stream.BitDepth = rawBitDepth
		}
		if indexes[stream.Index] {
			return Info{}, fmt.Errorf("ffprobe returned duplicate stream index %d", stream.Index)
		}
		indexes[stream.Index] = true
		stream.Bitrate, err = source.Bitrate.integer()
		if err != nil || stream.Bitrate < 0 {
			return Info{}, invalidField(fmt.Sprintf("stream[%d].bit_rate", position), err)
		}
		defaultValue, err := source.Disposition.Default.integer()
		if err != nil || (defaultValue != 0 && defaultValue != 1) {
			return Info{}, invalidField(fmt.Sprintf("stream[%d].disposition.default", position), err)
		}
		forcedValue, err := source.Disposition.Forced.integer()
		if err != nil || (forcedValue != 0 && forcedValue != 1) {
			return Info{}, invalidField(fmt.Sprintf("stream[%d].disposition.forced", position), err)
		}
		stream.IsDefault = defaultValue == 1
		stream.IsForced = forcedValue == 1
		attachedPicture, err := source.Disposition.AttachedPicture.integer()
		if err != nil || (attachedPicture != 0 && attachedPicture != 1) {
			return Info{}, invalidField(fmt.Sprintf("stream[%d].disposition.attached_pic", position), err)
		}
		stream.IsAttachedPicture = attachedPicture == 1
		duration, err := timestampTicks(source.DurationTS, source.TimeBase, source.Duration)
		if err != nil || duration < 0 {
			return Info{}, invalidField(fmt.Sprintf("stream[%d].duration", position), err)
		}
		if duration > longestStream {
			longestStream = duration
		}
		info.Streams = append(info.Streams, stream)
	}
	if document.Format.Duration.missing() {
		info.DurationTicks = longestStream
	}
	for index, source := range document.Chapters {
		start, err := timestampTicks(source.Start, source.TimeBase, source.StartTime)
		if err != nil {
			return Info{}, invalidField(fmt.Sprintf("chapter[%d].start", index), err)
		}
		end, err := timestampTicks(source.End, source.TimeBase, source.EndTime)
		if err != nil || end < start {
			return Info{}, invalidField(fmt.Sprintf("chapter[%d].end", index), err)
		}
		info.Chapters = append(info.Chapters, Chapter{StartTicks: start, EndTicks: end, Title: tagValue(source.Tags, "title")})
	}
	if isMusicMetadataSource(info) {
		music, err := parseMusicMetadata(document.Format.Tags)
		if err != nil {
			return Info{}, err
		}
		info.EmbeddedMusic = &music
	}
	return info, nil
}

func invalidField(field string, err error) error {
	if err == nil {
		return fmt.Errorf("invalid ffprobe %s value", field)
	}
	return fmt.Errorf("invalid ffprobe %s: %w", field, err)
}

func tagValue(tags map[string]string, name string) string {
	if value, ok := tags[name]; ok {
		return value
	}
	for key, value := range tags {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func timestampTicks(timestamp, timeBase, seconds scalar) (int64, error) {
	if !timestamp.missing() && !timeBase.missing() {
		value, err := timestamp.integer()
		if err != nil {
			return 0, err
		}
		base, err := timeBase.rational()
		if err != nil || base.Sign() <= 0 {
			return 0, fmt.Errorf("invalid time base %q", timeBase)
		}
		return rationalTicks(new(big.Rat).Mul(new(big.Rat).SetInt64(value), base))
	}
	return secondsToTicks(seconds)
}

func secondsToTicks(seconds scalar) (int64, error) {
	if seconds.missing() {
		return 0, nil
	}
	value, err := seconds.rational()
	if err != nil {
		return 0, err
	}
	return rationalTicks(value)
}

// rationalTicks rounds to the nearest 100 ns, with ties away from zero.
// No binary floating-point value is introduced during conversion.
func rationalTicks(seconds *big.Rat) (int64, error) {
	ticks := new(big.Rat).Mul(seconds, new(big.Rat).SetInt64(TicksPerSecond))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(ticks.Num(), ticks.Denom(), remainder)
	if new(big.Int).Lsh(new(big.Int).Abs(remainder), 1).Cmp(ticks.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(int64(ticks.Sign())))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("duration exceeds the int64 tick range")
	}
	return quotient.Int64(), nil
}

func isTextSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "ass", "ssa", "subrip", "srt", "text", "mov_text", "webvtt", "ttml",
		"realtext", "sami", "microdvd", "mpl2", "jacosub", "pjs", "subviewer",
		"subviewer1", "vplayer", "stl", "aqtitle", "eia_608":
		return true
	default:
		return false
	}
}
