package transcode

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	MaxLiveSegmentBytes int64 = 64 << 20
	MaxLiveScratchBytes int64 = 256 << 20
	LiveAlignmentTicks  int64 = ticksPerSecond / 1000
)

// StreamInputs transfers each pipe to Manager together. Bitmap, when selected,
// is an independent reader of the same authorized source generation; reading
// Media twice would distribute bytes between consumers rather than duplicate it.
type StreamInputs struct {
	Media  *os.File
	Bitmap *os.File
}

func (inputs StreamInputs) close() {
	if inputs.Media != nil {
		_ = inputs.Media.Close()
	}
	if inputs.Bitmap != nil && inputs.Bitmap != inputs.Media {
		_ = inputs.Bitmap.Close()
	}
}

// LiveRendition borrows complete, stable regular files until LivePublish returns.
// Init is supplied for every fMP4 segment and remains byte-stable within this
// producer; the receiver stores it only once per generation. PreMuxClockTicks is the true first reference packet PTS
// before the segment container applies any timestamp shift.
type LiveRendition struct {
	Init             *os.File
	Media            *os.File
	Size             int64
	PreMuxClockTicks int64
	DurationTicks    int64
	ContentType      string
}

// LiveSegment is one closed, aligned bundle from a single continuous producer.
// Sequence is producer-local; it must never replace a store's epoch/global ID.
// StartTicks uses the common FFmpeg pre-container reference clock, after the
// explicitly bound source-origin offset. It is not wall time or store time.
type LiveSegment struct {
	Sequence       int64
	StartTicks     int64
	DurationTicks  int64
	Discontinuity  bool
	RenditionCount int
	Renditions     [MaxHLSRenditions]LiveRendition
}

type liveRuntimeKey struct{}
type liveRuntime struct {
	spec     Spec
	jobID    string
	inputs   StreamInputs
	maxBytes int64
	timeout  time.Duration
	publish  func(context.Context, Spec, string, LiveSegment) error
	subtitle func(context.Context, Spec, string, int, io.Reader) error
	caption  func(context.Context, Spec, string, LiveCaptionSegment) error
}

func withLiveRuntime(ctx context.Context, live liveRuntime) context.Context {
	return context.WithValue(ctx, liveRuntimeKey{}, live)
}

func liveRuntimeFromContext(ctx context.Context) (liveRuntime, bool) {
	live, ok := ctx.Value(liveRuntimeKey{}).(liveRuntime)
	return live, ok && live.publish != nil
}

func liveRenditionCount(p Plan) int { return max(1, p.HLS.RenditionCount) }

// LiveSourceClockBiasTicks applies one common origin and bounded encoder-delay
// headroom to every demuxer. Source positions, companion subtitles, and mux
// clock evidence all retain this same affine clock; stores map it separately.
func LiveSourceClockBiasTicks(p Plan) int64 {
	bias := ticksPerSecond
	if p.SourceFormatStartKnown {
		bias -= p.SourceFormatStartTicks
	}
	return bias
}

func liveJournalFD(p Plan, rendition int) int { return 4 + liveRenditionCount(p) + rendition }
func liveBitmapFD(p Plan) int                 { return 4 + 2*liveRenditionCount(p) }

func liveSubtitleFD(p Plan, slot int) int {
	tracks := PlanHLSSubtitles(p)
	if slot < 0 || slot >= tracks.Count || tracks.Tracks[slot].ExternalTag != "" {
		return -1
	}
	fd := liveBitmapFD(p)
	if hasBitmapSubtitleInput(p) {
		fd++
	}
	for index := 0; index < slot; index++ {
		if tracks.Tracks[index].ExternalTag == "" {
			fd++
		}
	}
	return fd
}

func isLiveCaptionScratchName(name string) bool {
	value, ok := strings.CutPrefix(name, "caption-s")
	if !ok {
		return false
	}
	slot, rest, ok := strings.Cut(value, "-")
	if !ok || len(slot) != 1 || slot[0] < '0' || slot[0] >= '0'+MaxHLSSubtitleTracks {
		return false
	}
	digits, suffix, ok := strings.Cut(rest, ".")
	if !ok || len(digits) < 6 || len(digits) > 10 || !asciiDigits(digits) || suffix != "mkv.tmp" && suffix != "mp4.tmp" {
		return false
	}
	_, err := strconv.ParseInt(digits, 10, 32)
	return err == nil
}
