package library

import (
	"context"
	"fmt"
	"github.com/moooyo/goby/internal/identity"
	"os"
	"strings"
)

// OpenDownload opens the original indexed local source for a user download.
func (s *Store) OpenDownload(ctx context.Context, userID, itemID, sourceID string) (*os.File, MediaFile, error) {
	return s.OpenDownloadFor(ctx, Subject{UserID: userID}, itemID, sourceID)
}

// OpenDownloadFor authorizes content downloading independently from playback.
// Source identifiers select the indexed original; they never resolve paths or
// URLs. A successful caller owns the descriptor at offset zero.
func (s *Store) OpenDownloadFor(ctx context.Context, subject Subject, itemID, sourceID string) (*os.File, MediaFile, error) {
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') || strings.ContainsRune(sourceID, '\x00') {
		return nil, MediaFile{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, MediaFile{}, ErrUnavailable
	}
	return runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		snapshot, err := s.readDownloadSource(ctx, subject, itemID, sourceID)
		if err != nil {
			return nil, MediaFile{}, err
		}
		file, err := s.openMediaSource(ctx, snapshot)
		if err != nil {
			return nil, MediaFile{}, err
		}
		return file, snapshot.mediaFile, nil
	})
}

func (s *Store) readDownloadSource(ctx context.Context, subject Subject, itemID, sourceID string) (indexedMediaSource, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer tx.Rollback(ctx)
	// Application credentials have independent authority. Account policy still
	// scopes an explicit catalog target, but never grants a key its authority.
	if subject.ApplicationCredentialID == "" && (!access.policy.EnableContentDownloading || !access.policy.AllowsFeature(identity.FeatureDownloads)) {
		return indexedMediaSource{}, ErrForbidden
	}
	snapshot, err := readIndexedMediaSource(ctx, tx, access, itemID, sourceID)
	if err != nil {
		return indexedMediaSource{}, err
	}
	// Filesystem work starts only after releasing the database snapshot.
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: complete authorized download source read: %w", ErrUnavailable, err)
	}
	return snapshot, nil
}
