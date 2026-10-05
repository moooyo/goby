package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const maxCanonicalVobSubIndexBytes = 8 << 20

// WriteCanonicalVobSubIndex writes an independently validated IDX/SUB pair's
// index using the narrower lexical grammar accepted by FFmpeg. It borrows both
// descriptors without changing their offsets and never emits a source path.
// The caller must supply the held SUB separately and discard output on error.
func WriteCanonicalVobSubIndex(ctx context.Context, input, companion *os.File, output io.Writer) (resultErr error) {
	if ctx == nil || input == nil || companion == nil || output == nil {
		return fmt.Errorf("%w: invalid canonical VobSub descriptors or output", ErrBitmapSubtitle)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before, err := input.Stat()
	if err != nil {
		return err
	}
	paired, err := companion.Stat()
	if err != nil {
		return err
	}
	defer func() {
		if err := ctx.Err(); err != nil {
			resultErr = errors.Join(resultErr, err)
		} else if !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(companion, paired) {
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: canonical VobSub source changed", ErrBitmapSubtitle))
		}
	}()
	document, err := readExternalBitmapSubtitles(ctx, "vobsub", input, companion, -1)
	if err != nil {
		return err
	}
	size, err := canonicalVobSubSize(ctx, io.NewSectionReader(input, 0, before.Size()))
	if err != nil {
		return err
	}
	palette, hasPalette, err := dvdSubtitlePalette(document.extra)
	if err != nil {
		return fmt.Errorf("%w: canonical VobSub palette: %w", ErrBitmapSubtitle, err)
	}
	var buffer bytes.Buffer
	appendLine := func(line string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(line) > maxCanonicalVobSubIndexBytes-buffer.Len() {
			return fmt.Errorf("%w: canonical VobSub index byte limit", ErrAnalysisBudget)
		}
		buffer.WriteString(line)
		return nil
	}
	if err := appendLine("# VobSub index file, v7 (do not modify this line!)\n" + size); err != nil {
		return err
	}
	if hasPalette {
		var colors [16]string
		for index, color := range palette {
			colors[index] = fmt.Sprintf("%02x%02x%02x", color.R, color.G, color.B)
		}
		if err := appendLine("palette: " + strings.Join(colors[:], ", ") + "\n"); err != nil {
			return err
		}
	}
	// FFmpeg's timestamp scanner limits HH to two digits. Its signed delay
	// scanner accepts the full bounded clock. Rebase only the lexical fields;
	// each timestamp plus delay retains its original absolute presentation.
	const timestampWindow = 100 * 60 * 60 * TicksPerSecond
	delay := int64(0)
	for _, track := range document.tracks {
		if err := appendLine(fmt.Sprintf("id: %s, index: %d\n", track.Language, track.id)); err != nil {
			return err
		}
		for _, entry := range track.entries {
			stamp := entry.pts - delay
			if stamp < 0 || stamp >= timestampWindow {
				delay, stamp = entry.pts, 0
				if err := appendLine("delay: " + canonicalVobSubClock(delay, true) + "\n"); err != nil {
					return err
				}
			}
			if err := appendLine(fmt.Sprintf("timestamp: %s, filepos: %09x\n", canonicalVobSubClock(stamp, false), entry.pos)); err != nil {
				return err
			}
		}
	}
	// Do not publish any index bytes after a failed validation or a source
	// change between structural inspection and the presentation-header pass.
	if !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(companion, paired) {
		return fmt.Errorf("%w: canonical VobSub source changed", ErrBitmapSubtitle)
	}
	for buffer.Len() != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := buffer.Next(min(buffer.Len(), 64<<10))
		n, err := output.Write(chunk)
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrShortWrite
		}
	}
	return nil
}

func canonicalVobSubClock(ticks int64, signed bool) string {
	prefix := ""
	if signed {
		prefix = "+"
		if ticks < 0 {
			prefix, ticks = "-", -ticks
		}
	}
	milliseconds := ticks / 10000
	return fmt.Sprintf("%s%02d:%02d:%02d:%03d", prefix, milliseconds/3600000, milliseconds/60000%60, milliseconds/1000%60, milliseconds%1000)
}

func canonicalVobSubSize(ctx context.Context, reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var size string
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := strings.TrimSpace(scanner.Text())
		key, value, found := strings.Cut(line, ":")
		if !found || key != "size" {
			continue
		}
		widthText, heightText, ok := strings.Cut(strings.TrimSpace(value), "x")
		width, widthErr := strconv.Atoi(strings.TrimSpace(widthText))
		height, heightErr := strconv.Atoi(strings.TrimSpace(heightText))
		if !ok || widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 4096 || height > 4096 || width > maxBitmapSubtitlePixels/height {
			return "", fmt.Errorf("%w: unsupported VobSub canvas dimensions", ErrSubtitleTimelineUnsupported)
		}
		canonical := fmt.Sprintf("size: %dx%d\n", width, height)
		if size != "" && size != canonical {
			return "", fmt.Errorf("%w: conflicting VobSub canvas dimensions", ErrSubtitleTimelineUnsupported)
		}
		size = canonical
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%w: canonical VobSub header read: %w", ErrBitmapSubtitle, err)
	}
	return size, nil
}
