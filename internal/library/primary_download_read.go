package library

import (
	"context"
	"fmt"
	"os"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
)

// OpenOriginalDownloadFor reopens the exact planning snapshot under fresh
// download authority. It shares actual-read and retained-owner budgets with
// original playback, without acquiring playback permission or a play lease.
// ctx must be the actual response lifetime. file is borrowed for cancellation;
// content owns all payload reads and the complete descriptor/Store lifetime.
func (s *Store) OpenOriginalDownloadFor(ctx context.Context, subject Subject, itemID, sourceID, expectedETag string) (*os.File, MediaFile, *primaryio.ReadSeeker, error) {
	return s.openOriginalReadFor(ctx, subject, itemID, sourceID, expectedETag, s.readOriginalDownloadRevalidationFor, s.openPublicMediaSource)
}

// readOriginalDownloadRevalidationFor preserves download authority independently
// from playback. It carries every primary media fact and source/publication
// proof, without fetching catalog presentation, intro or subtitle projections
// that the already captured original download does not consume.
func (s *Store) readOriginalDownloadRevalidationFor(ctx context.Context, subject Subject, itemID, sourceID string) (indexedMediaSource, error) {
	ctx, release, err := beginMediaSourceAuthorization(ctx)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer release()
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer tx.Rollback(ctx)
	// A key's independent authority is checked by beginSubjectRead. Account
	// policy scopes a catalog target without granting the key its authority.
	if subject.ApplicationCredentialID == "" && (!access.policy.EnableContentDownloading || !access.policy.AllowsFeature(identity.FeatureDownloads)) {
		return indexedMediaSource{}, ErrForbidden
	}
	snapshot, err := readIndexedMediaRevalidation(ctx, tx, access, itemID, sourceID, false)
	if err != nil {
		return indexedMediaSource{}, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
		return indexedMediaSource{}, err
	}
	// Filesystem opening starts after releasing the authorization transaction,
	// as in the full planning and playback revalidation paths.
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: complete authorized download revalidation: %w", ErrUnavailable, err)
	}
	if err := s.sealPrimaryMediaReadSnapshot(ctx, &snapshot); err != nil {
		return indexedMediaSource{}, err
	}
	return snapshot, nil
}
