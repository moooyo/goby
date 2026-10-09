package server

import (
	"context"
	"os"
	"path/filepath"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	hlsSubtitleSessionCacheBytes = 16 << 20
	hlsSubtitleServiceCacheBytes = 64 << 20
)

type hlsSubtitleExtractionKey struct {
	source, codec, format, tool string
	stream                      int
}

type hlsSubtitleExtractionEntry struct {
	key     hlsSubtitleExtractionKey
	tool    os.FileInfo
	content library.SubtitleContent
}

// Each request still opens and authorizes the actual source before lookup.
// Only immutable bytes are retained: parsed documents and rendered windows
// belong to the request, so caption offsets and producer clocks remain local.
type hlsSubtitleExtractionRead struct {
	runtime   *hlsRuntime
	session   *hlsSession
	key       hlsSubtitleExtractionKey
	tool      os.FileInfo
	candidate library.SubtitleContent
}

func (read *hlsSubtitleExtractionRead) prepare(ctx context.Context, source library.MediaFile, stream media.Stream, format, configuredTool string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session := read.session
	session.mu.Lock()
	closed := session.closed || session.ctx == nil || session.ctx.Err() != nil
	session.mu.Unlock()
	if closed {
		return transcode.ErrJobCancelled
	}
	// This is the witness returned by this extraction's own authorized open,
	// not an earlier HLS request snapshot applied to arbitrary returned bytes.
	if stream.IsExternal || source.ETag == "" || source.ETag != session.key.stamp ||
		source.Item.ID != session.key.scope.ItemID || source.SourceID != session.key.scope.SourceID {
		return library.ErrSourceChanged
	}
	read.key = hlsSubtitleExtractionKey{source: source.ETag, stream: stream.Index, codec: stream.Codec,
		format: format, tool: configuredTool}
	// Keep the configured invocation unchanged. Resolving a wrapper symlink
	// can change its $0-relative resources or an executable's argv[0]. Cache
	// only direct absolute paths; PATH and alias invocations retain extraction.
	if !filepath.IsAbs(configuredTool) || filepath.Clean(configuredTool) != configuredTool {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(configuredTool)
	if err != nil || resolved != configuredTool {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	read.tool = info
	return nil
}

func sameSubtitleExtractionTool(first, second os.FileInfo) bool {
	return first != nil && second != nil && first.Mode().IsRegular() && second.Mode().IsRegular() &&
		os.SameFile(first, second) && first.Size() == second.Size() && first.ModTime().Equal(second.ModTime()) &&
		media.FileChangeTime(first) == media.FileChangeTime(second)
}

func (read *hlsSubtitleExtractionRead) get() (library.SubtitleContent, bool) {
	if read.tool == nil {
		return library.SubtitleContent{}, false
	}
	read.session.mu.Lock()
	defer read.session.mu.Unlock()
	if read.session.closed || read.session.ctx.Err() != nil {
		return library.SubtitleContent{}, false
	}
	entry, found := read.session.subtitleExtractions[read.key.stream]
	if !found || entry.key != read.key || !sameSubtitleExtractionTool(entry.tool, read.tool) {
		return library.SubtitleContent{}, false
	}
	return entry.content, true
}

// Publish is called only after successful parsing/rendering and final HLS
// authority/source revalidation. Cache pressure never rejects media delivery.
// Retained bytes have independent session and service limits; active request
// buffers remain bounded by the existing four subtitle slots and input limit.
func (read *hlsSubtitleExtractionRead) publish(ctx context.Context) {
	if read.tool == nil || len(read.candidate.Data) == 0 || len(read.candidate.Data) > media.MaxSubtitleExtractionBytes || ctx.Err() != nil {
		return
	}
	resolved, err := filepath.EvalSymlinks(read.key.tool)
	if err != nil || resolved != read.key.tool {
		return
	}
	currentTool, err := os.Stat(read.key.tool)
	if err != nil || !sameSubtitleExtractionTool(read.tool, currentTool) {
		return
	}
	session, runtime := read.session, read.runtime
	session.mu.Lock()
	defer session.mu.Unlock()
	if ctx.Err() != nil || session.closed || session.ctx.Err() != nil || session.key.stamp != read.key.source {
		return
	}
	previous, found := session.subtitleExtractions[read.key.stream]
	if !found && len(session.subtitleExtractions) >= transcode.MaxHLSSubtitleTracks {
		return
	}
	delta := len(read.candidate.Data) - len(previous.content.Data)
	runtime.subtitleExtractionMu.Lock()
	defer runtime.subtitleExtractionMu.Unlock()
	if session.subtitleExtractionBytes+delta > hlsSubtitleSessionCacheBytes || runtime.subtitleExtractionBytes+delta > hlsSubtitleServiceCacheBytes {
		return
	}
	// Detach the retained representation from the extractor's buffer capacity.
	content := read.candidate
	content.Data = make([]byte, len(read.candidate.Data))
	copy(content.Data, read.candidate.Data)
	if session.subtitleExtractions == nil {
		session.subtitleExtractions = make(map[int]hlsSubtitleExtractionEntry)
	}
	session.subtitleExtractions[read.key.stream] = hlsSubtitleExtractionEntry{key: read.key, tool: read.tool, content: content}
	session.subtitleExtractionBytes += delta
	runtime.subtitleExtractionBytes += delta
}

// The caller holds session.mu. No path holds the byte-accounting mutex while
// acquiring session.mu, including Stop, source replacement and runtime Close.
func (runtime *hlsRuntime) clearSubtitleExtractionsLocked(session *hlsSession) {
	runtime.subtitleExtractionMu.Lock()
	runtime.subtitleExtractionBytes -= session.subtitleExtractionBytes
	session.subtitleExtractionBytes = 0
	session.subtitleExtractions = nil
	runtime.subtitleExtractionMu.Unlock()
}
