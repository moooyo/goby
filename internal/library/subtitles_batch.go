package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/subtitle"
)

// SubtitleExpectation binds a planned external rendition to its catalog entry.
// Both filesystem sidecars and owned derivatives have external content tags.
type SubtitleExpectation struct {
	Index int
	Codec string
	Tag   string
}

const maxSubtitleValidationTracks = 8

// ValidateSubtitlesFor checks one bounded group for the same primary source.
// It retains no payloads: owned bytes are validated one at a time inside each
// authoritative snapshot, and filesystem bytes are consumed one track at a
// time after admission. Groups reauthorize after storage work before the final
// source fence, and occupy one of the existing four workers.
func (s *Store) ValidateSubtitlesFor(ctx context.Context, subject Subject, itemID, sourceID string, expected []SubtitleExpectation) error {
	if len(expected) == 0 {
		return nil
	}
	if strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') ||
		strings.ContainsRune(sourceID, '\x00') || len(expected) > maxSubtitleValidationTracks {
		return ErrInvalidInput
	}
	indices := make([]int, len(expected))
	for position, track := range expected {
		if track.Index < 0 || track.Index > maxSubtitleStreamIndex || track.Tag == "" {
			return ErrInvalidInput
		}
		for _, previous := range indices[:position] {
			if track.Index == previous {
				return ErrInvalidInput
			}
		}
		indices[position] = track.Index
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if len(expected) == 1 {
		// A single track has no longer group observation window. Keep its
		// existing two snapshots rather than adding a third transaction.
		track := expected[0]
		content, err := s.ReadSubtitleFor(ctx, subject, itemID, sourceID, track.Index)
		if err != nil {
			return err
		}
		return matchSubtitleExpectation(content.Info, track)
	}
	// The worker may outlive delivery cancellation; it owns immutable selectors.
	expected = append([]SubtitleExpectation(nil), expected...)
	_, err := runSubtitleWorker(ctx, subtitleSourceWorkers, func() (SubtitleContent, error) {
		primary, tracks, err := s.readSubtitleSnapshotsFor(ctx, subject, itemID, sourceID, indices, true)
		if err != nil {
			return SubtitleContent{}, err
		}
		if err := matchSubtitleExpectations(tracks, expected); err != nil {
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
			// Admission can wait. Select current policy and all tracks again
			// in one fresh snapshot before the first filesystem operation.
			current, tracks, err := s.readSubtitleSnapshotsFor(work, subject, itemID, sourceID, indices, true)
			if err != nil {
				return err
			}
			if current.root != primary.root {
				return fmt.Errorf("%w: %w: subtitle root changed while queued", ErrUnavailable, ErrSourceChanged)
			}
			if err := matchSubtitleExpectations(tracks, expected); err != nil {
				return err
			}
			return s.withSubtitlePrimarySource(work, current, func() error {
				for _, track := range tracks {
					if err := work.Err(); err != nil {
						return err
					}
					if track.Owned {
						// The current authorized snapshot already checked its
						// complete content, hash, codec, and source revision.
						continue
					}
					if _, err := s.readSubtitleSource(work, current, track); err != nil {
						return err
					}
				}
				// A slow early sidecar must not hide later credential changes
				// or retirement of an owned track. Recheck the complete group
				// without retaining bytes, then let the primary source helper
				// perform its final descriptor, pathname and publication fence.
				final, tracks, err := s.readSubtitleSnapshotsFor(work, subject, itemID, sourceID, indices, true)
				if err != nil {
					return err
				}
				if final.root != current.root || final.publicationRevision != current.publicationRevision {
					return fmt.Errorf("%w: %w: primary media changed during subtitle validation", ErrUnavailable, ErrSourceChanged)
				}
				return matchSubtitleExpectations(tracks, expected)
			})
		})
		return SubtitleContent{}, errors.Join(err, operation.Close())
	})
	return err
}

func matchSubtitleExpectations(tracks []storedSubtitle, expected []SubtitleExpectation) error {
	for index, track := range tracks {
		if err := matchSubtitleExpectation(track.Subtitle, expected[index]); err != nil {
			return err
		}
	}
	return nil
}

func matchSubtitleExpectation(track Subtitle, expected SubtitleExpectation) error {
	actual, actualErr := subtitle.NormalizeFormat(track.Codec)
	wanted, wantedErr := subtitle.NormalizeFormat(expected.Codec)
	if actualErr != nil || wantedErr != nil || actual != wanted ||
		track.Index != expected.Index || track.Tag != expected.Tag {
		return ErrSourceChanged
	}
	return nil
}
