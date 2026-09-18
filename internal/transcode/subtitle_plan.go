package transcode

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// SubtitlePlan uses only indexed stream identities and bounded scalar options.
// FontStreams is a canonical, sorted list of authorized attachment indexes.
// Assets are produced inside the job directory; no input path is serialized.
type SubtitlePlan struct {
	Mode            string `json:"Mode,omitempty"`
	Codec           string `json:"Codec,omitempty"`
	StreamIndex     int    `json:"StreamIndex,omitempty"`
	SubtitleOrdinal int    `json:"SubtitleOrdinal,omitempty"`
	OffsetTicks     int64  `json:"OffsetTicks,omitempty"`
	FontStreams     string `json:"FontStreams,omitempty"`
	ExternalTag     string `json:"ExternalTag,omitempty"`
}

type subtitleSourceContextKey struct{}
type subtitleSourceRequest struct {
	spec Spec
	load func(context.Context, Spec) ([]byte, error)
}

// withSubtitleSource passes an authorized late-bound loader into the runner.
// The manager never persists subtitle contents or opens a supplied pathname.
func withSubtitleSource(ctx context.Context, spec Spec, load func(context.Context, Spec) ([]byte, error)) context.Context {
	return context.WithValue(ctx, subtitleSourceContextKey{}, subtitleSourceRequest{spec: spec, load: load})
}

func IsBitmapSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvdsub", "dvb_subtitle", "dvbsub", "xsub":
		return true
	default:
		return false
	}
}

// ValidateSubtitlePlan rejects unbound stream identities and filters that
// require a decoded software video pipeline before command construction.
func ValidateSubtitlePlan(p Plan) error {
	s := p.Subtitle
	invalid := func() error { return fmt.Errorf("%w: subtitle selection", ErrInvalidPlan) }
	if s.Mode == "" {
		if s != (SubtitlePlan{}) {
			return invalid()
		}
		return nil
	}
	indexLimit := maxStreamIndex
	if s.ExternalTag != "" {
		digest, err := hex.DecodeString(s.ExternalTag)
		if err != nil || len(digest) != 32 || strings.ToLower(s.ExternalTag) != s.ExternalTag {
			return invalid()
		}
		indexLimit = 1<<31 - 1
	}
	if s.StreamIndex < 0 || s.StreamIndex > indexLimit || s.SubtitleOrdinal < 0 || s.SubtitleOrdinal > maxStreamIndex ||
		s.StreamIndex == p.VideoStreamIndex || s.StreamIndex == p.AudioStreamIndex ||
		s.OffsetTicks < -24*60*60*ticksPerSecond || s.OffsetTicks > 24*60*60*ticksPerSecond {
		return invalid()
	}
	text := media.SubtitleExtractFormat(s.Codec) != ""
	if s.ExternalTag != "" && !text {
		return invalid()
	}
	if _, err := subtitleFontIndexes(s.FontStreams); err != nil {
		return invalid()
	}
	switch s.Mode {
	case "hls":
		if !text || p.OutputMode != "" || s.FontStreams != "" {
			return invalid()
		}
	case "burn":
		decode, encode := hardwareSelection(p.Hardware)
		if p.SourceMode == "stream" || p.VideoStreamIndex < 0 || p.VideoCodec != "h264" || decode != "software" || encode != "software" ||
			(!text && !IsBitmapSubtitle(s.Codec)) || p.OutputMode != "" || (!text && s.FontStreams != "") {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

// TextSubtitleFilter runs after the normal software video filter chain. The
// bounded ASS asset retains source timestamps, styles and embedded font names.
func TextSubtitleFilter(p Plan) (string, error) {
	if err := ValidateSubtitlePlan(p); err != nil {
		return "", err
	}
	if p.Subtitle.Mode != "burn" || IsBitmapSubtitle(p.Subtitle.Codec) {
		return "", nil
	}
	shift := p.StartTicks - p.Subtitle.OffsetTicks
	filter := "subtitles=filename='subtitle.ass':fontsdir='.'"
	if shift == 0 {
		return filter, nil
	}
	seconds := strconv.FormatFloat(float64(shift)/float64(ticksPerSecond), 'f', 7, 64)
	return "setpts=PTS+(" + seconds + ")/TB," + filter + ",setpts=PTS-(" + seconds + ")/TB", nil
}

// BitmapSubtitleGraph transforms video before overlaying subtitles, so HDR
// mapping and deinterlacing cannot change caption colors or glyph edges. The
// source canvas is kept until after overlay, then both layers are scaled once.
func BitmapSubtitleGraph(p Plan, videoFilters string) (string, string, error) {
	if err := ValidateSubtitlePlan(p); err != nil {
		return "", "", err
	}
	if p.Subtitle.Mode != "burn" || !IsBitmapSubtitle(p.Subtitle.Codec) {
		return "", "", nil
	}
	graph := "[0:" + strconv.Itoa(p.VideoStreamIndex) + "]"
	processing := softwareVideoProcessingFilter(p)
	if processing != "" {
		videoFilters = strings.TrimPrefix(videoFilters, processing+",")
	} else {
		processing = "null"
	}
	graph += processing + "[goby_canvas];[0:" + strconv.Itoa(p.Subtitle.StreamIndex) + "]"
	var subtitleFilters []string
	if p.Subtitle.OffsetTicks != 0 {
		seconds := strconv.FormatFloat(float64(p.Subtitle.OffsetTicks)/float64(ticksPerSecond), 'f', 7, 64)
		subtitleFilters = append(subtitleFilters, "setpts=PTS+("+seconds+")/TB")
	}
	if len(subtitleFilters) == 0 {
		subtitleFilters = append(subtitleFilters, "null")
	}
	graph += strings.Join(subtitleFilters, ",") + "[goby_subtitle];[goby_canvas][goby_subtitle]overlay=eof_action=pass:shortest=0:repeatlast=0"
	if videoFilters != "" {
		graph += "," + videoFilters
	}
	graph += "[goby_video]"
	return graph, "[goby_video]", nil
}

func subtitleFontIndexes(value string) ([]int, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 16 {
		return nil, ErrInvalidPlan
	}
	indexes := make([]int, 0, len(parts))
	previous := -1
	for _, part := range parts {
		index, err := strconv.Atoi(part)
		if err != nil || index <= previous || index > maxStreamIndex || strconv.Itoa(index) != part {
			return nil, ErrInvalidPlan
		}
		indexes = append(indexes, index)
		previous = index
	}
	return indexes, nil
}

// PrepareSubtitleAssets executes after the runner has verified its empty owned
// job directory. Files use fixed names and exclusive creation; the job manager
// owns their lifetime with the rest of the conversion cache.
func PrepareSubtitleAssets(ctx context.Context, executable, directory string, input *os.File, p Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateSubtitlePlan(p); err != nil {
		return err
	}
	if p.Subtitle.Mode != "burn" || IsBitmapSubtitle(p.Subtitle.Codec) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var data []byte
	var err error
	if p.Subtitle.ExternalTag != "" {
		request, ok := ctx.Value(subtitleSourceContextKey{}).(subtitleSourceRequest)
		if !ok || request.load == nil || request.spec.Plan != p {
			return ErrInvalidInput
		}
		data, err = request.load(ctx, request.spec)
		if err == nil && (len(data) == 0 || len(data) > media.MaxSubtitleExtractionBytes) {
			return ErrInvalidInput
		}
	} else {
		data, err = media.ExtractSubtitle(ctx, executable, input, media.Stream{Index: p.Subtitle.StreamIndex, Codec: p.Subtitle.Codec, CodecType: "subtitle"}, "ass")
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	write := func(path string, data []byte) error {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if err := write(filepath.Join(directory, "subtitle.ass"), data); err != nil {
		return err
	}
	indexes, _ := subtitleFontIndexes(p.Subtitle.FontStreams)
	total := 0
	for slot, index := range indexes {
		data, err := media.ExtractFontAttachment(ctx, executable, input, media.Stream{Index: index, Codec: "ttf", CodecType: "attachment"})
		if err != nil {
			return err
		}
		total += len(data)
		if total > 32<<20 {
			return fmt.Errorf("%w: font attachment budget", ErrInvalidInput)
		}
		if err := write(filepath.Join(directory, "font-"+strconv.Itoa(slot)+".ttf"), data); err != nil {
			return err
		}
	}
	return ctx.Err()
}
