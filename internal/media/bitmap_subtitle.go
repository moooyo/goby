package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxSubtitleOCRCues           = 10000
	MaxSubtitleOCRCueTextBytes   = 4 << 10
	MaxSubtitleOCRTextBytes      = 8 << 20
	MaxSubtitleOCRImageBytes     = 64 << 20
	MaxSubtitleOCRCueImageBytes  = 1 << 20
	maxBitmapSubtitlePackets     = 40000
	maxBitmapSubtitlePacketBytes = 32 << 20
	maxBitmapSubtitleProbeBytes  = 160 << 20
	maxBitmapSubtitlePixels      = 4096 * 2160
	maxBitmapSubtitleTotalPixels = 32 << 20
)

var (
	ErrBitmapSubtitle = errors.New("bitmap subtitle decoding unavailable")
	ErrSubtitleOCR    = errors.New("subtitle OCR unavailable")
)

// BitmapSubtitleConfig identifies an operator-configured ffprobe binary. Hashes
// bind the admitted file descriptor; no client-supplied executable is accepted.
type BitmapSubtitleConfig struct {
	FFprobePath   string
	FFprobeSHA256 string
}

type SubtitleOCRModel struct {
	ID     string
	Path   string
	SHA256 string
}

type SubtitleOCRModelIdentity struct {
	ID     string
	SHA256 string
}

type SubtitleOCRConfig struct {
	FFprobePath     string
	FFprobeSHA256   string
	TesseractPath   string
	TesseractSHA256 string
	Models          []SubtitleOCRModel
	// The application supplies a private job-owned directory and its remaining
	// disk budget. Empty/zero values use a private system-temporary directory
	// and the media layer's fixed 512 MiB model-copy ceiling, respectively.
	ScratchDirectory string
	MaxScratchBytes  int64
}

// BitmapSubtitleLimits are decoder allocation budgets, not runtime overrides
// supplied by a user. DurationTicks is the indexed source presentation end.
type BitmapSubtitleLimits struct {
	MaxCues        int
	MaxPixels      int
	MaxTotalPixels int
	MaxPacketBytes int
	// MaxWorkPixels bounds cumulative raster work independently of retained
	// working-set pixels. Zero selects twice MaxTotalPixels for batch helpers.
	MaxWorkPixels int64
	DurationTicks int64
}

// BitmapSubtitleCue represents a real decoder display interval. Image contains
// straight-alpha original pixels; X and Y retain its video-space position.
type BitmapSubtitleCue struct {
	StartTicks      int64
	EndTicks        int64
	Image           *image.NRGBA
	X               int
	Y               int
	Forced          bool
	HearingImpaired bool
	ImageSHA256     string
}

type SubtitleOCRCue struct {
	StartTicks      int64
	EndTicks        int64
	Text            string
	Confidence      float64
	ImagePNG        []byte
	ImageSHA256     string
	Forced          bool
	HearingImpaired bool
	X               int
	Y               int
	Width           int
	Height          int
}

// SubtitleOCRResult is a review candidate, never permission to publish a track.
// Confidence is the mean of recognized TSV word confidences, in [0, 100].
type SubtitleOCRResult struct {
	EngineSHA256 string
	ModelID      string
	ModelSHA256  string
	Models       []SubtitleOCRModelIdentity
	Cues         []SubtitleOCRCue
	Warnings     []string
}

type bitmapSubtitlePacket struct {
	PTS      int64
	Duration int64
	Data     []byte
}

func bitmapSubtitleLimits(duration int64) BitmapSubtitleLimits {
	return BitmapSubtitleLimits{MaxCues: MaxSubtitleOCRCues, MaxPixels: maxBitmapSubtitlePixels,
		MaxTotalPixels: maxBitmapSubtitleTotalPixels, MaxPacketBytes: maxBitmapSubtitlePacketBytes,
		DurationTicks: duration}
}

func subtitleFileUnchanged(file *os.File, before os.FileInfo) bool {
	if file == nil || before == nil {
		return false
	}
	after, err := file.Stat()
	return err == nil && os.SameFile(before, after) && before.Size() == after.Size() &&
		before.ModTime().Equal(after.ModTime()) && FileChangeTime(before) == FileChangeTime(after)
}

// openPinnedSubtitleFile verifies the held descriptor rather than a pathname
// re-opened after hashing. It reads only a bounded regular local file.
func openPinnedSubtitleFile(ctx context.Context, path, expected string, maxBytes int64) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") || len(expected) != 64 || strings.ToLower(expected) != expected {
		return nil, nil, fmt.Errorf("invalid subtitle tool identity")
	}
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != sha256.Size {
		return nil, nil, fmt.Errorf("invalid subtitle tool digest")
	}
	file, err := openLocalMedia(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open subtitle tool: %w", err)
	}
	fail := func(err error) (*os.File, os.FileInfo, error) { file.Close(); return nil, nil, err }
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxBytes {
		return fail(fmt.Errorf("invalid subtitle tool file"))
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	reader := io.NewSectionReader(file, 0, before.Size())
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fail(fmt.Errorf("read subtitle tool: %w", readErr))
		}
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected || !subtitleFileUnchanged(file, before) {
		return fail(fmt.Errorf("subtitle tool identity mismatch"))
	}
	return file, before, nil
}
