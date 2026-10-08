package library

import (
	"context"
	"strings"
)

// PlanDownloadFor captures download authority and the indexed original source
// without touching its file. Delivery must use OpenPreparedOriginalDownloadFor
// to reacquire authority after admission and prove the actual file identity.
func (s *Store) PlanDownloadFor(ctx context.Context, subject Subject, itemID, sourceID string) (MediaFile, error) {
	if ctx == nil || strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') || strings.ContainsRune(sourceID, '\x00') {
		return MediaFile{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return MediaFile{}, ErrUnavailable
	}
	work, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return MediaFile{}, err
	}
	defer finish()
	snapshot, err := s.readOriginalDownloadRevalidationFor(work, subject, itemID, sourceID)
	if err != nil {
		return MediaFile{}, err
	}
	return snapshot.mediaFile, nil
}
