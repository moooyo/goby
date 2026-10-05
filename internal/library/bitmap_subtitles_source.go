package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// WithBitmapSubtitleFor borrows the exact authorized SUP file or IDX/SUB pair
// for one synchronous consumer. Nil consume performs the same source checks
// without preparing a playback asset. Descriptors must not escape the callback.
// SourceStreamIndex is a demux ordinal, never an IDX language identifier.
// Cancellation before the callback prevents it from starting later. Once the
// callback starts, return waits for it and all descriptor/IO retirement, even
// after cancellation, so its caller can safely retire private output assets.
func (s *Store) WithBitmapSubtitleFor(ctx context.Context, subject Subject, itemID, mediaSourceID string, index int,
	consume func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error) error {
	if ctx == nil || strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, 0) ||
		strings.ContainsRune(mediaSourceID, 0) || index < 0 || index > maxSubtitleStreamIndex {
		return ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	borrow := &bitmapSubtitleBorrow{done: make(chan struct{})}
	var guarded func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error
	if consume != nil {
		guarded = func(work context.Context, info BitmapSubtitle, input media.ExternalSubtitleTimelineInput) error {
			if err := borrow.enter(work); err != nil {
				return err
			}
			return consume(work, info, input)
		}
	}
	_, err := runSubtitleWorker(ctx, subtitleSourceWorkers, func() (_ SubtitleContent, resultErr error) {
		defer func() { borrow.complete(resultErr) }()
		primary, _, err := s.readBitmapSubtitleSnapshotFor(ctx, subject, itemID, mediaSourceID, index)
		if err != nil {
			return SubtitleContent{}, err
		}
		operation, hint, err := s.prepareSidecarRootIO(ctx, primary.root)
		if err != nil {
			return SubtitleContent{}, err
		}
		err = operation.Run(ctx, primary.root.id, primaryio.Foreground, func(work context.Context) error {
			if err := s.checkSidecarRootHint(work, hint); err != nil {
				return err
			}
			current, track, err := s.readBitmapSubtitleSnapshotFor(work, subject, itemID, mediaSourceID, index)
			if err != nil {
				return err
			}
			if current.root != primary.root {
				return fmt.Errorf("%w: %w: bitmap subtitle root changed while queued", ErrUnavailable, ErrSourceChanged)
			}
			// A runner may already carry the primary video's read phase. The
			// borrowed sidecars must reuse this operation when a media adapter
			// enters its synchronous reader boundary inside the consumer.
			work = media.WithSourceReadPhase(work, func(phase context.Context, read func(context.Context) error) error {
				return operation.Run(phase, primary.root.id, primaryio.Foreground, read)
			})
			if err := s.withBitmapSubtitleSource(work, current, track, guarded); err != nil {
				return err
			}
			// A consumer can cross a policy revision, rescan, pair reassignment,
			// or publication. Never authorize its result using the earlier read.
			fresh, selected, err := s.readBitmapSubtitleSnapshotFor(work, subject, itemID, mediaSourceID, index)
			if err != nil {
				return err
			}
			if fresh.mediaFile.ETag != current.mediaFile.ETag || fresh.root != current.root ||
				fresh.relativePath != current.relativePath || fresh.rootBindingRevision != current.rootBindingRevision ||
				fresh.publicationRevision != current.publicationRevision || !reflect.DeepEqual(selected, track) {
				return fmt.Errorf("%w: %w: bitmap subtitle catalog changed while consuming", ErrUnavailable, ErrSourceChanged)
			}
			return s.checkSidecarRootHint(work, hint)
		})
		return SubtitleContent{}, errors.Join(err, operation.Close())
	})
	return borrow.settle(ctx, err)
}

// The source worker can abandon a request before borrowing, but a consumer can
// own a private output file. Its completion handshake therefore outlives caller
// cancellation and prevents the output owner from racing a late callback.
type bitmapSubtitleBorrow struct {
	mu                 sync.Mutex
	started, abandoned bool
	done               chan struct{}
	err                error
}

func (borrow *bitmapSubtitleBorrow) enter(ctx context.Context) error {
	borrow.mu.Lock()
	defer borrow.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if borrow.abandoned || borrow.started {
		return context.Canceled
	}
	borrow.started = true
	return nil
}

func (borrow *bitmapSubtitleBorrow) complete(err error) {
	borrow.err = err
	close(borrow.done)
}

func (borrow *bitmapSubtitleBorrow) settle(ctx context.Context, err error) error {
	borrow.mu.Lock()
	borrow.abandoned = true
	started := borrow.started
	borrow.mu.Unlock()
	if !started {
		return err
	}
	<-borrow.done
	if ctx.Err() != nil {
		return errors.Join(err, borrow.err, ctx.Err())
	}
	return err
}

func (s *Store) readBitmapSubtitleSnapshotFor(ctx context.Context, subject Subject, itemID, sourceID string, index int) (indexedMediaSource, storedBitmapSubtitle, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, err
	}
	defer tx.Rollback(ctx)
	if !access.canPlay {
		return indexedMediaSource{}, storedBitmapSubtitle{}, ErrForbidden
	}
	snapshot, err := readIndexedMediaSource(ctx, tx, access, itemID, sourceID)
	if err != nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, err
	}
	if sourceID != snapshot.mediaFile.SourceID {
		return indexedMediaSource{}, storedBitmapSubtitle{}, ErrNotFound
	}
	// The same public projection selects tracks for playback negotiation. Do
	// not expose a colliding or overflow track that projection deliberately hid.
	visible := slices.ContainsFunc(snapshot.mediaFile.Item.BitmapSubtitles, func(track BitmapSubtitle) bool { return track.Index == index })
	var selected *storedBitmapSubtitle
	if visible {
		for _, track := range snapshot.mediaFile.Item.bitmapSubtitleFacts {
			if track.Index == index {
				selected = &track
				break
			}
		}
	}
	if selected == nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, ErrNotFound
	}
	if err := validateBitmapSubtitleSnapshot(snapshot, *selected); err != nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, storedBitmapSubtitle{}, fmt.Errorf("%w: complete authorized bitmap subtitle read: %w", ErrUnavailable, err)
	}
	return snapshot, *selected, nil
}

func (s *Store) withBitmapSubtitleSource(ctx context.Context, primary indexedMediaSource, source storedBitmapSubtitle,
	consume func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error) (resultErr error) {
	file, err := s.openPublicMediaSource(ctx, primary)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, file)) }()
	if err := s.withBitmapSubtitleComponents(ctx, primary, source, consume); err != nil {
		return err
	}
	after, err := file.Stat()
	if err != nil || !primary.matches(after) {
		return fmt.Errorf("%w: %w: primary media changed during bitmap subtitle reading", ErrUnavailable, ErrSourceChanged)
	}
	current, err := s.openPublicMediaSource(ctx, primary)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, current)) }()
	currentInfo, err := current.Stat()
	if err != nil || !sameMediaSourceFile(after, currentInfo) {
		return fmt.Errorf("%w: %w: primary media was replaced during bitmap subtitle reading", ErrUnavailable, ErrSourceChanged)
	}
	return ctx.Err()
}

func (s *Store) withBitmapSubtitleComponents(ctx context.Context, primary indexedMediaSource, source storedBitmapSubtitle,
	consume func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error) (resultErr error) {
	root, err := s.openLibraryRoot(primary.root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, root)) }()
	directoryPath := filepath.Dir(filepath.FromSlash(source.relativePath))
	parent, err := openRegisteredRoot(root, directoryPath)
	if err != nil {
		return fmt.Errorf("%w: bitmap subtitle directory cannot be opened safely", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, parent)) }()
	files := make([]subtitleTimelineExternalFile, 0, len(source.Components))
	defer func() {
		for _, held := range files {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, held.file))
		}
	}()
	for _, facts := range source.Components {
		before, err := parent.Lstat(facts.Name)
		if err != nil || !subtitleTimelineComponentMatches(facts, before) {
			return fmt.Errorf("%w: %w: bitmap subtitle component changed before opening", ErrUnavailable, ErrSourceChanged)
		}
		file, err := openScanFile(parent, facts.Name)
		if err != nil {
			return errors.Join(ErrUnavailable, ErrSourceChanged, err)
		}
		held := subtitleTimelineExternalFile{facts: facts, file: file}
		files = append(files, held)
		if err := recheckSubtitleTimelineComponent(ctx, parent, held); err != nil {
			return errors.Join(ErrUnavailable, err)
		}
	}
	input := media.ExternalSubtitleTimelineInput{StreamIndex: source.Index, SourceStreamIndex: source.SourceStreamIndex,
		Codec: source.Codec, Input: files[0].file}
	if source.Format == "vobsub" {
		input.Companion = files[1].file
	}
	if consume != nil {
		info := source.BitmapSubtitle
		info.Components = slices.Clone(source.Components)
		if err := consume(ctx, info, input); err != nil {
			return err
		}
	}
	currentRoot, err := s.openLibraryRoot(primary.root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, currentRoot)) }()
	currentParent, err := openRegisteredRoot(currentRoot, directoryPath)
	if err != nil {
		return fmt.Errorf("%w: %w: bitmap subtitle directory changed during reading", ErrUnavailable, ErrSourceChanged)
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, currentParent)) }()
	if !sameMediaSourceDirectory(root, currentRoot) || !sameMediaSourceDirectory(parent, currentParent) {
		return fmt.Errorf("%w: %w: bitmap subtitle directory was replaced during reading", ErrUnavailable, ErrSourceChanged)
	}
	for _, held := range files {
		if err := recheckSubtitleTimelineComponent(ctx, currentParent, held); err != nil {
			return errors.Join(ErrUnavailable, err)
		}
	}
	return ctx.Err()
}
