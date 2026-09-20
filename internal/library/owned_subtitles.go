package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/subtitle"
)

const maxOwnedSubtitleBytes = subtitle.MaxInputBytes

// The same complete source stamp binds administrator edits and owned captions.
// Neither an unchanged path nor an unchanged media source ID can revive a
// derivative after replacement, reprobe, or a library-root rebind.
const ownedSubtitleSourceRevisionSQL = MediaOperationSourceRevisionSQL

const ownedSubtitleColumns = `s.stream_index,s.codec,s.language,s.title,s.content_sha256,
	s.is_default,s.is_forced,s.is_hearing_impaired,octet_length(s.content),s.created_at`

func scanOwnedSubtitle(row rowScanner, additional ...any) (Subtitle, error) {
	var track Subtitle
	values := []any{&track.Index, &track.Codec, &track.Language, &track.Title, &track.Tag,
		&track.IsDefault, &track.IsForced, &track.IsHearingImpaired, &track.Size, &track.ModifiedAt}
	if err := row.Scan(append(values, additional...)...); err != nil {
		return Subtitle{}, err
	}
	track.Owned = true
	track.MIMEType = subtitleMIME(track.Codec)
	track.ModifiedAt = track.ModifiedAt.UTC()
	return track, nil
}

func attachOwnedSubtitles(ctx context.Context, tx pgx.Tx, items []Item, ids []string, positions map[string][]int) error {
	rows, err := tx.Query(ctx, `SELECT `+ownedSubtitleColumns+`,s.item_id FROM item_owned_subtitles s
		JOIN items i ON i.id=s.item_id AND i.root_id=s.root_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE s.item_id=ANY($1::text[]) AND s.active AND s.source_revision=`+ownedSubtitleSourceRevisionSQL+`
		ORDER BY s.item_id,s.stream_index`, ids)
	if err != nil {
		return fmt.Errorf("read owned item subtitles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		track, err := scanOwnedSubtitle(rows, &itemID)
		if err != nil {
			return err
		}
		for _, position := range positions[itemID] {
			if track.Index > highestEmbeddedStreamIndex(items[position].Media) {
				items[position].Subtitles = append(items[position].Subtitles, track)
			}
		}
	}
	return rows.Err()
}

// readOwnedSubtitle runs in the same authorized snapshot as the primary source.
// Owned tracks never impersonate files in the adjacent media directory.
func readOwnedSubtitle(ctx context.Context, tx pgx.Tx, itemID, rootID string, index int) (storedSubtitle, error) {
	var content []byte
	track, err := scanOwnedSubtitle(tx.QueryRow(ctx, `SELECT `+ownedSubtitleColumns+`,s.content
		FROM item_owned_subtitles s JOIN items i ON i.id=s.item_id AND i.root_id=s.root_id
		WHERE s.item_id=$1 AND s.root_id=$2 AND s.stream_index=$3 AND s.active
		AND s.source_revision=`+ownedSubtitleSourceRevisionSQL, itemID, rootID, index), &content)
	if err != nil {
		return storedSubtitle{}, err
	}
	return storedSubtitle{Subtitle: track, rootID: rootID, ownedData: content}, nil
}

func validateOwnedSubtitle(source storedSubtitle) error {
	if !source.Owned || source.Index < 0 || source.Index > maxSubtitleStreamIndex ||
		source.Codec != "srt" && source.Codec != "vtt" || source.ModifiedAt.IsZero() ||
		source.Size < 1 || source.Size > maxOwnedSubtitleBytes || source.Size != int64(len(source.ownedData)) ||
		source.Filename != "" || source.relativePath != "" || source.identity != "" ||
		source.MIMEType != subtitleMIME(source.Codec) || !validOwnedSubtitleMetadata(source.Language, source.Title) {
		return fmt.Errorf("%w: invalid owned subtitle metadata", ErrUnavailable)
	}
	digest := sha256.Sum256(source.ownedData)
	if source.Tag != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("%w: owned subtitle content hash changed", ErrUnavailable)
	}
	if _, err := subtitle.Parse(source.ownedData, subtitle.Format(source.Codec)); err != nil {
		return fmt.Errorf("%w: invalid owned subtitle content: %w", ErrUnavailable, err)
	}
	return nil
}

func validOwnedSubtitleMetadata(language, title string) bool {
	if len(language) > 32 || len(title) > 512 || !utf8.ValidString(title) || strings.IndexFunc(title, unicode.IsControl) >= 0 {
		return false
	}
	for _, c := range language {
		if c < 'a' || c > 'z' {
			if c < 'A' || c > 'Z' {
				if c < '0' || c > '9' {
					if c != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

// subtitleCatalogCapacity must be called while the item row is locked. Both
// namespaces retain inactive identities forever, preventing a stale stream URL
// from selecting a new OCR result or a newly discovered sidecar.
func subtitleCatalogCapacity(ctx context.Context, tx pgx.Tx, itemID string) (total, highest, active int, err error) {
	err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(stream_index),-1),count(*) FILTER(WHERE active)
		FROM (SELECT s.stream_index,s.active FROM item_subtitles s WHERE s.item_id=$1
		UNION ALL SELECT s.stream_index,s.active AND s.root_id=i.root_id AND s.source_revision=`+ownedSubtitleSourceRevisionSQL+`
		FROM item_owned_subtitles s JOIN items i ON i.id=s.item_id WHERE s.item_id=$1) tracks`, itemID).
		Scan(&total, &highest, &active)
	return
}

// deleteOwnedSubtitleAsUser retires a database-backed derivative under current
// subtitle-management authority. It never unlinks or rewrites primary media.
// A false handled result leaves ordinary sidecar deletion to its file journal.
func (s *Store) deleteOwnedSubtitleAsUser(ctx context.Context, actor identity.Principal, itemID string, index int) (handled bool, err error) {
	if s == nil || s.pool == nil {
		return true, ErrUnavailable
	}
	if ctx == nil || !metadataIdentifier(itemID) || index < 0 || index > maxSubtitleStreamIndex {
		return true, ErrInvalidInput
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return true, err
	}
	defer rollback(tx)
	access, err := checkFileMutationActor(ctx, tx, actor, true)
	if err != nil {
		return true, err
	}
	if !actor.IsApplicationKey() && (!access.policy.EnableSubtitleManagement || !access.policy.AllowsFeature(identity.FeatureSubtitleManagement)) {
		return true, ErrForbidden
	}
	var change CatalogChange
	change.Kind = CatalogUpdated
	err = tx.QueryRow(ctx, `SELECT i.id,i.library_id,COALESCE(i.parent_id,'') FROM items i
		WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND ($2::boolean OR i.library_id=ANY($3::text[])) AND `+access.directSQL("i")+`
		FOR UPDATE OF i`, itemID, access.all, access.folders).Scan(&change.ItemID, &change.LibraryID, &change.ParentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, ErrNotFound
	}
	if err != nil {
		return true, err
	}
	var present, active bool
	err = tx.QueryRow(ctx, `SELECT true,s.active AND s.root_id=i.root_id AND s.source_revision=`+ownedSubtitleSourceRevisionSQL+`
		FROM item_owned_subtitles s JOIN items i ON i.id=s.item_id
		WHERE s.item_id=$1 AND s.stream_index=$2 FOR UPDATE OF s`, itemID, index).Scan(&present, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if !active {
		return true, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE item_owned_subtitles SET active=false,retired_at=clock_timestamp()
		WHERE item_id=$1 AND stream_index=$2`, itemID, index); err != nil {
		return true, err
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		return true, err
	}
	if _, err := checkFileMutationActor(ctx, tx, actor, false); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}
