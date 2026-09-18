package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const MaxSubtitleExtractionBytes = 8 << 20
const MaxFontAttachmentBytes = 16 << 20

var ErrSubtitleExtraction = errors.New("subtitle extraction unavailable")
var subtitleExtractionSlots = make(chan struct{}, 4)

// SubtitleExtractFormat returns the native, style-preserving text format that
// the bounded extractor can produce for an indexed codec.
func SubtitleExtractFormat(codec string) string {
	switch strings.ToLower(codec) {
	case "ass", "ssa":
		return "ass"
	case "srt", "subrip", "text", "mov_text":
		return "srt"
	case "vtt", "webvtt":
		return "vtt"
	default:
		return ""
	}
}

// ExtractSubtitle borrows an authorized source descriptor and addresses an
// absolute catalog stream index. Paths, remote protocols and arbitrary output
// arguments never enter this API. The source is rechecked after extraction.
func ExtractSubtitle(ctx context.Context, executable string, input *os.File, stream Stream, format string) ([]byte, error) {
	if stream.CodecType != "subtitle" || stream.IsExternal || SubtitleExtractFormat(stream.Codec) == "" {
		return nil, ErrSubtitleExtraction
	}
	codec, muxer := "", ""
	switch format {
	case "srt":
		codec, muxer = "srt", "srt"
	case "vtt":
		codec, muxer = "webvtt", "webvtt"
	case "ass", "ssa":
		codec, muxer = "ass", "ass"
		if stream.Codec == "ass" || stream.Codec == "ssa" {
			codec = "copy"
		}
	default:
		return nil, ErrSubtitleExtraction
	}
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(stream.Index), "-vn", "-an", "-dn",
		"-c:s", codec, "-f", muxer, "pipe:1"}
	return extractSubtitleBytes(ctx, executable, input, stream.Index, MaxSubtitleExtractionBytes, args)
}

// FontAttachment identifies only embedded OpenType/TrueType fonts. Metadata is
// advisory; ExtractFontAttachment also checks the actual font magic bytes.
func FontAttachment(stream Stream) bool {
	if stream.CodecType != "attachment" || stream.Index < 0 || stream.Index > 4095 {
		return false
	}
	switch strings.ToLower(stream.Codec) {
	case "ttf", "otf":
		return true
	}
	switch strings.ToLower(stream.MIMEType) {
	case "application/x-truetype-font", "application/vnd.ms-opentype", "application/x-font-ttf", "application/x-font-opentype", "font/ttf", "font/otf":
		return true
	}
	return false
}

// ExtractFontAttachment streams one bounded attachment into memory. It does not
// trust attachment filenames, write fonts to disk, or enumerate unrelated data.
func ExtractFontAttachment(ctx context.Context, executable string, input *os.File, stream Stream) ([]byte, error) {
	if !FontAttachment(stream) {
		return nil, ErrSubtitleExtraction
	}
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1",
		"-dump_attachment:" + strconv.Itoa(stream.Index), "pipe:1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", "0:v:0?", "-map", "0:a:0?", "-t", "0", "-f", "null", "-"}
	data, err := extractSubtitleBytes(ctx, executable, input, stream.Index, MaxFontAttachmentBytes, args)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || !(string(data[:4]) == "\x00\x01\x00\x00" || string(data[:4]) == "OTTO" || string(data[:4]) == "ttcf" || string(data[:4]) == "true") {
		return nil, ErrSubtitleExtraction
	}
	return data, nil
}

func extractSubtitleBytes(ctx context.Context, executable string, input *os.File, index, limit int, args []string) ([]byte, error) {
	if runtime.GOOS != "linux" || input == nil || index < 0 || index > 4095 || strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return nil, ErrSubtitleExtraction
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case subtitleExtractionSlots <- struct{}{}:
		defer func() { <-subtitleExtractionSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrSubtitleExtraction
	}
	data, err := runLimitedFiles(ctx, 20*time.Second, limit, executable, []*os.File{input}, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSubtitleExtraction, err)
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || FileChangeTime(before) != FileChangeTime(after) {
		return nil, ErrSubtitleExtraction
	}
	if len(data) == 0 || len(data) > limit {
		return nil, ErrSubtitleExtraction
	}
	return data, nil
}
