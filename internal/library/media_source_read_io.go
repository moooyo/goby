package library

import (
	"context"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// primaryMediaReadProof seals route provenance after an authorized catalog
// snapshot has committed. Exported MediaFile fields cannot manufacture a proof.
type primaryMediaReadProof struct {
	store                       *Store
	hint                        mediaSourceRootHint
	itemID, libraryID, sourceID string
	etag                        string
	class                       primaryio.Class
}

func (s *Store) sealPrimaryMediaReadSnapshot(ctx context.Context, snapshot *indexedMediaSource) error {
	if s == nil || snapshot == nil {
		return ErrUnavailable
	}
	hint := mediaSourceRootHint{root: snapshot.root, bindingRevision: snapshot.rootBindingRevision}
	if hint.bindingRevision <= 0 {
		current, err := s.readMediaSourceRootHint(ctx, snapshot.mediaFile.Item.ID)
		if err != nil {
			return err
		}
		if current.root != snapshot.root {
			return ErrSourceChanged
		}
		hint = current
	}
	snapshot.mediaFile.primaryReadProof = &primaryMediaReadProof{store: s, hint: hint,
		itemID: snapshot.mediaFile.Item.ID, libraryID: snapshot.mediaFile.Item.LibraryID,
		sourceID: snapshot.mediaFile.SourceID, etag: snapshot.mediaFile.ETag}
	return nil
}

// MediaSourceReadIO carries an opaque, committed catalog route through input
// handoff, queueing, subprocess reads and final descriptor cleanup. It grants no
// playback authority and cannot be constructed from a pathname or client Spec.
type MediaSourceReadIO struct {
	root  *PrimaryRootIO
	class primaryio.Class
}

type mediaSourceReadIOContextKey struct{}

func MediaSourceReadIOFromContext(ctx context.Context) *MediaSourceReadIO {
	if ctx == nil {
		return nil
	}
	read, _ := ctx.Value(mediaSourceReadIOContextKey{}).(*MediaSourceReadIO)
	return read
}

func (s *Store) PrepareMediaSourceIO(ctx context.Context, source MediaFile) (*MediaSourceReadIO, error) {
	proof := source.primaryReadProof
	if proof == nil || proof.store != s || source.Item.ID != proof.itemID || source.Item.LibraryID != proof.libraryID ||
		source.SourceID != proof.sourceID || source.ETag == "" || source.ETag != proof.etag {
		return nil, ErrUnavailable
	}
	root, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{proof.hint})
	if err != nil {
		return nil, err
	}
	return &MediaSourceReadIO{root: root, class: proof.class}, nil
}

func (read *MediaSourceReadIO) Context(ctx context.Context) context.Context {
	if read == nil || read.root == nil || ctx == nil {
		return ctx
	}
	ctx = context.WithValue(read.root.Context(ctx), mediaSourceReadIOContextKey{}, read)
	return media.WithSourceReadPhase(ctx, read.Run)
}

func (read *MediaSourceReadIO) Fork(ctx context.Context) (*MediaSourceReadIO, error) {
	if read == nil || read.root == nil {
		return nil, ErrUnavailable
	}
	root, err := read.root.Fork(ctx)
	if err != nil {
		return nil, err
	}
	return &MediaSourceReadIO{root: root, class: read.class}, nil
}

func (read *MediaSourceReadIO) Run(ctx context.Context, work func(context.Context) error) error {
	if read == nil || read.root == nil {
		return ErrUnavailable
	}
	return read.root.Run(ctx, "", read.class, work)
}

func (read *MediaSourceReadIO) Close() error {
	if read == nil || read.root == nil {
		return nil
	}
	return read.root.Close()
}

func (read *MediaSourceReadIO) MarkUnknown(err error) error {
	if read == nil || read.root == nil {
		return ErrUnavailable
	}
	return read.root.MarkUnknown(err)
}
