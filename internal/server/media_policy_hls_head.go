package server

import (
	"context"
	"os"

	"github.com/moooyo/goby/internal/transcode"
)

// segmentHeader may inspect an existing segment but never admits an encoder.
// An uncached authorized HEAD exposes its representation type without claiming
// a Content-Length or validator for bytes that do not exist yet.
func (h *hlsRuntime) segmentHeader(ctx context.Context, session *hlsSession, input *os.File, number int) (*transcode.ReadHandle, error) {
	defer input.Close()
	timeline, err := h.timeline(ctx, session, input)
	if err != nil {
		return nil, err
	}
	if number < 0 || number >= len(timeline.Segments) {
		return nil, transcode.ErrJobNotFound
	}
	name := "segment-" + paddedSegmentNumber(number) + ".ts"
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, transcode.ErrJobNotFound
	}
	for index := len(session.producers) - 1; index >= 0; index-- {
		producer := session.producers[index]
		if number >= producer.first && number <= producer.last {
			if handle, err := h.manager.TryOpen(session.key.scope, producer.id, name); err == nil {
				return handle, nil
			}
		}
	}
	return nil, nil
}
