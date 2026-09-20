package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/artwork"
)

const EmbeddedArtworkVersion = 1
const MaxEmbeddedArtworkBytes = 20 << 20
const MaxEmbeddedArtworkTotalBytes = 32 << 20
const MaxEmbeddedArtworkPictures = 8

var ErrEmbeddedArtwork = errors.New("embedded artwork extraction unavailable")
var embeddedArtworkSlots = make(chan struct{}, 2)

// EmbeddedPicture preserves the absolute input stream index and validated
// original image bytes. PictureType is Front, Back, or Other; it is advisory
// ordering metadata, never a substitute for decoding the actual image.
type EmbeddedPicture struct {
	StreamIndex    int
	PictureType    string
	Data           []byte
	MIMEType, Hash string
	Width, Height  int
}

// An empty successful result explicitly means that the current media probe
// contains no attached pictures. A corrupt or unsupported picture is an error.
type EmbeddedArtworkResult struct {
	Version  int
	Pictures []EmbeddedPicture
}

// ExtractEmbeddedArtwork borrows an already authorized regular descriptor.
// Operator-configured tool names are resolved to absolute executable paths
// before entering the strict extractor. No HTTP-supplied tool names, flags,
// filenames, remote protocols, or output paths enter the child process.
// Metadata is inspected separately from the playback probe cache so existing
// cached sources also obtain APIC/FLAC front/back picture semantics.
func (p Prober) ExtractEmbeddedArtwork(ctx context.Context, input *os.File, info Info) (EmbeddedArtworkResult, error) {
	result := EmbeddedArtworkResult{Version: EmbeddedArtworkVersion}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	attached := false
	for _, stream := range info.Streams {
		if stream.IsAttachedPicture {
			attached = true
			break
		}
	}
	if attached {
		var err error
		p.FFprobePath, err = resolveEmbeddedArtworkExecutable(p.FFprobePath, "ffprobe")
		if err != nil {
			return result, fmt.Errorf("%w: resolve configured ffprobe: %w", ErrEmbeddedArtwork, err)
		}
		p.FFmpegPath, err = resolveEmbeddedArtworkExecutable(p.FFmpegPath, "ffmpeg")
		if err != nil {
			return result, fmt.Errorf("%w: resolve configured ffmpeg: %w", ErrEmbeddedArtwork, err)
		}
	}
	return p.extractEmbeddedArtwork(ctx, input, info)
}

func resolveEmbeddedArtworkExecutable(configured, fallback string) (string, error) {
	if configured == "" {
		configured = fallback
	}
	if strings.TrimSpace(configured) == "" || strings.ContainsAny(configured, "\x00\r\n") {
		return "", errors.New("invalid configured executable")
	}
	resolved, err := exec.LookPath(configured)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !embeddedArtworkExecutable(absolute) {
		return "", errors.New("configured executable did not resolve to an absolute path")
	}
	return absolute, nil
}

// extractEmbeddedArtwork accepts only resolved operator-owned executables.
// Keeping this boundary strict prevents a later refactor from silently moving
// PATH lookup into the child process execution path.
func (p Prober) extractEmbeddedArtwork(ctx context.Context, input *os.File, info Info) (EmbeddedArtworkResult, error) {
	result := EmbeddedArtworkResult{Version: EmbeddedArtworkVersion}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if runtime.GOOS != "linux" || input == nil || info.ProbeVersion < CurrentProbeVersion {
		return result, ErrEmbeddedArtwork
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != info.Size ||
		FileChangeTime(before) != info.FileChangeTimeNs {
		return result, ErrEmbeddedArtwork
	}
	expected := make(map[int]Stream)
	for _, stream := range info.Streams {
		if !stream.IsAttachedPicture {
			continue
		}
		if stream.Index < 0 || stream.Index > 4095 || stream.CodecType != "video" || stream.IsExternal {
			return result, ErrEmbeddedArtwork
		}
		if _, exists := expected[stream.Index]; exists {
			return result, ErrEmbeddedArtwork
		}
		expected[stream.Index] = stream
	}
	if len(expected) == 0 {
		return result, nil
	}
	if len(expected) > MaxEmbeddedArtworkPictures || !embeddedArtworkExecutable(p.FFprobePath) || !embeddedArtworkExecutable(p.FFmpegPath) {
		return result, ErrEmbeddedArtwork
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case embeddedArtworkSlots <- struct{}{}:
		defer func() { <-embeddedArtworkSlots }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	metadata, err := runLimitedFilesOutput(ctx, 30*time.Second, 1<<20, p.FFprobePath, []*os.File{input},
		"-v", "error", "-show_entries", "stream=index,codec_name,codec_type,width,height:stream_disposition=attached_pic:stream_tags=title,comment",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats, "-i", "/proc/self/fd/3")
	if err == nil && len(metadata.stderr) != 0 {
		err = errors.New("picture metadata decoder reported an error")
	}
	if err != nil {
		return result, fmt.Errorf("%w: inspect picture metadata: %w", ErrEmbeddedArtwork, err)
	}
	streams, err := embeddedArtworkStreams(metadata.stdout, expected)
	if err != nil {
		return result, err
	}
	total := 0
	pictures := make([]EmbeddedPicture, 0, len(streams))
	for _, selected := range streams {
		limit := min(MaxEmbeddedArtworkBytes, MaxEmbeddedArtworkTotalBytes-total)
		if limit <= 0 {
			return result, ErrEmbeddedArtwork
		}
		// Copying packets preserves validated JPEG/PNG/GIF bytes and does not
		// allocate an unbounded decoded frame in the external process.
		output, err := runLimitedFilesOutput(ctx, 30*time.Second, limit, p.FFmpegPath, []*os.File{input},
			"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
			"-i", "/proc/self/fd/3", "-map", "0:"+strconv.Itoa(selected.StreamIndex), "-an", "-sn", "-dn",
			"-frames:v", "1", "-c:v", "copy", "-f", "image2pipe", "pipe:1")
		if err == nil && len(output.stderr) != 0 {
			err = errors.New("picture demuxer reported an error")
		}
		if err != nil {
			return result, fmt.Errorf("%w: extract picture stream: %w", ErrEmbeddedArtwork, err)
		}
		total += len(output.stdout)
		if len(output.stdout) == 0 || total > MaxEmbeddedArtworkTotalBytes {
			return result, ErrEmbeddedArtwork
		}
		validated, err := artwork.InspectContext(ctx, bytes.NewReader(output.stdout))
		if err != nil {
			return result, fmt.Errorf("%w: invalid picture: %w", ErrEmbeddedArtwork, err)
		}
		indexed := expected[selected.StreamIndex]
		if validated.Width != indexed.Width || validated.Height != indexed.Height {
			return result, ErrEmbeddedArtwork
		}
		selected.Data, selected.MIMEType, selected.Hash = output.stdout, validated.MIMEType, validated.Tag
		selected.Width, selected.Height = validated.Width, validated.Height
		pictures = append(pictures, selected)
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) || FileChangeTime(before) != FileChangeTime(after) {
		return EmbeddedArtworkResult{Version: EmbeddedArtworkVersion}, ErrEmbeddedArtwork
	}
	if err := ctx.Err(); err != nil {
		return EmbeddedArtworkResult{Version: EmbeddedArtworkVersion}, err
	}
	result.Pictures = pictures
	return result, nil
}

func embeddedArtworkExecutable(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "\x00\r\n")
}

func embeddedArtworkStreams(data []byte, expected map[int]Stream) ([]EmbeddedPicture, error) {
	var document probeDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrEmbeddedArtwork
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrEmbeddedArtwork
	}
	seen := make(map[int]bool)
	result := make([]EmbeddedPicture, 0, len(expected))
	for _, stream := range document.Streams {
		index, indexErr := stream.Index.integer()
		if indexErr != nil || stream.Index.missing() || index < 0 || index > 4095 || seen[int(index)] {
			return nil, ErrEmbeddedArtwork
		}
		seen[int(index)] = true
		attached, err := stream.Disposition.AttachedPicture.integer()
		if err != nil || attached < 0 || attached > 1 {
			return nil, ErrEmbeddedArtwork
		}
		if attached == 0 {
			continue
		}
		width, widthErr := stream.Width.integer()
		height, heightErr := stream.Height.integer()
		catalog, exists := expected[int(index)]
		if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 16384 || height > 16384 || width*height > 25<<20 || !exists ||
			stream.CodecType != "video" || stream.Codec != catalog.Codec || width != int64(catalog.Width) || height != int64(catalog.Height) {
			return nil, ErrEmbeddedArtwork
		}
		switch stream.Codec {
		case "mjpeg", "png", "gif":
		default:
			return nil, ErrEmbeddedArtwork
		}
		result = append(result, EmbeddedPicture{StreamIndex: int(index), PictureType: embeddedPictureType(tagValue(stream.Tags, "comment"), tagValue(stream.Tags, "title"))})
	}
	if len(result) != len(expected) {
		return nil, ErrEmbeddedArtwork
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := embeddedPictureRank(result[i].PictureType), embeddedPictureRank(result[j].PictureType)
		if left != right {
			return left < right
		}
		return result[i].StreamIndex < result[j].StreamIndex
	})
	return result, nil
}

func embeddedPictureType(comment, title string) string {
	for _, value := range []string{comment, title} {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "cover (front)", "front cover", "front", "3":
			return "Front"
		case "cover (back)", "back cover", "back", "4":
			return "Back"
		case "other", "0":
			return "Other"
		}
	}
	return "Other"
}

func embeddedPictureRank(value string) int {
	switch value {
	case "Front":
		return 0
	case "Other":
		return 1
	default:
		return 2
	}
}
