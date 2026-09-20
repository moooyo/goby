package server

import (
	"context"
	"io"

	"github.com/moooyo/goby/internal/identity"
)

// analysisPreviewProvider opens only an already published derivative. It must
// revalidate the authenticated principal and use library.OpenMediaFor (or an
// equivalent library-owned authorization/source operation) before selecting a
// source-bound cache entry. It never queues analysis or executes a media tool.
// Width 0 selects the largest ready variant; other widths are 240, 320 or 400.
// An authorized current video without a ready derivative returns a non-nil
// lease with Ready=false. Missing/inaccessible media remains an ordinary error.
type analysisPreviewProvider interface {
	OpenPreview(context.Context, identity.Principal, string, string, int) (analysisPreviewLease, error)
}

type analysisPreviewReader interface {
	io.ReaderAt
	io.ReadSeeker
}

// The lease owns the cache reader and source-retirement registration. Context
// is derived from the request and canceled on retirement or runtime shutdown.
// Revalidate checks current session, media ACL, actual source identity and the
// same immutable derivative immediately before HTTP validators or headers.
// Close must unblock reader I/O, release the cache lease and join owned work.
type analysisPreviewLease interface {
	Context() context.Context
	Metadata() analysisPreviewMetadata
	Reader() analysisPreviewReader
	Revalidate(context.Context) error
	Close() error
}

// SourceRevision is an opaque server-owned identity, never a path. BIFSHA256
// identifies the complete validated, immutable file, including its index.
type analysisPreviewMetadata struct {
	ItemID, MediaSourceID, SourceRevision string
	BIFSHA256                             string
	Width, Height                         int
	Size                                  int64
	Ready                                 bool
}

// These are the exact two properties and nested properties in the pinned SDK's
// RokuMetadata.Api.ThumbnailSetInfo and RokuMetadata.Api.ThumbnailInfo models.
type analysisThumbnailSetDTO struct {
	AspectRatio float64
	Thumbnails  []analysisThumbnailDTO
}

type analysisThumbnailDTO struct {
	PositionTicks int64
	ImageTag      string
}
