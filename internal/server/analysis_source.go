package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Stream selection is deterministic across catalog ordering. An explicitly
// default local stream precedes other local streams, with original index as the
// tie breaker. Attached pictures and external resources are never inputs.
func analysisStream(info media.Info, kind string) (int, bool) {
	var selected media.Stream
	found := false
	seen := make(map[int]bool, len(info.Streams))
	for _, stream := range info.Streams {
		if stream.IsExternal {
			continue
		}
		if stream.Index < 0 || stream.Index > 4095 || seen[stream.Index] {
			return 0, false
		}
		seen[stream.Index] = true
		if stream.CodecType != kind || stream.IsAttachedPicture {
			continue
		}
		if !found || stream.IsDefault && !selected.IsDefault || stream.IsDefault == selected.IsDefault && stream.Index < selected.Index {
			selected, found = stream, true
		}
	}
	return selected.Index, found
}

// The digest identifies the entire original media, not the analysis prefix.
// ReadAt preserves the borrowed descriptor offset. A blocked syscall keeps the
// caller's task slot occupied; cancellation is observed between actual reads.
func analysisSourceDigest(ctx context.Context, file *os.File, expectedSize, maximum int64) (string, error) {
	if ctx == nil || file == nil || expectedSize <= 0 || maximum <= 0 || maximum > 1<<40 || expectedSize > maximum {
		return "", media.ErrAnalysisBudget
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	before, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Size() != expectedSize {
		return "", library.ErrAnalysisSourceChanged
	}
	digest := sha256.New()
	buffer := make([]byte, 256<<10)
	for position := int64(0); position < expectedSize; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count := min(int64(len(buffer)), expectedSize-position)
		n, err := file.ReadAt(buffer[:int(count)], position)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		if int64(n) != count {
			return "", library.ErrAnalysisSourceChanged
		}
		_, _ = digest.Write(buffer[:n])
		position += int64(n)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) ||
		media.FileChangeTime(before) != media.FileChangeTime(after) {
		return "", library.ErrAnalysisSourceChanged
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
