package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// CreditsPoint identifies the start of credits on one current media source.
type CreditsPoint struct {
	StartTicks int64  `json:"StartTicks"`
	Provenance string `json:"Provenance"`
}

type CreditsDetail struct {
	ItemID            string            `json:"ItemId"`
	MediaSourceID     string            `json:"MediaSourceId"`
	SourceRevision    string            `json:"SourceRevision"`
	Revision          string            `json:"Revision"`
	DurationTicks     int64             `json:"DurationTicks"`
	Automatic         *CreditsPoint     `json:"Automatic"`
	Effective         *CreditsPoint     `json:"Effective"`
	Override          *CreditsPoint     `json:"Override"`
	OverrideStale     bool              `json:"OverrideStale"`
	LastEditedBy      string            `json:"LastEditedBy"`
	LastEditedAt      *time.Time        `json:"LastEditedAt"`
	Detected          []CreditsInterval `json:"Detected"`
	DetectedStale     bool              `json:"DetectedStale"`
	DetectedRevision  string            `json:"DetectedRevision"`
	DetectedStatus    string            `json:"DetectedStatus"`
	DetectedReason    string            `json:"DetectedReason"`
	DetectedUpdatedAt *time.Time        `json:"DetectedUpdatedAt"`
}

type CreditsEdit struct {
	Revision       string `json:"Revision"`
	SourceRevision string `json:"SourceRevision"`
	StartTicks     int64  `json:"StartTicks"`
	Provenance     string `json:"Provenance"`
}

var ErrCreditsRevisionConflict = errors.New("credits revision conflict")

// Both boundary types use the same existing source invalidation stamp. A root
// rebind, file replacement or reprobe invalidates their overrides independently.
const itemCreditsColumn = `CASE WHEN i.type IN ('Movie','Episode') AND NOT i.is_folder THEN (
    SELECT jsonb_build_object('StartTicks', credits.start_ticks, 'Provenance', credits.provenance)
    FROM item_credits_state credits WHERE credits.item_id=i.id AND credits.start_ticks IS NOT NULL
    AND credits.source_revision=` + introSourceRevisionSQL + `) END`

func validCreditsPoint(point *CreditsPoint, duration int64) bool {
	return point != nil && duration > 0 && point.StartTicks >= 0 && point.StartTicks < duration
}

// Only the explicit reserved chapter title is evidence; ordinary chapter
// names such as "End" and duplicate markers do not establish a credits point.
func ExplicitChapterCredits(info *media.Info) *CreditsPoint {
	if info == nil {
		return nil
	}
	var result *CreditsPoint
	for _, chapter := range info.Chapters {
		if strings.TrimSpace(chapter.Title) != "CreditsStart" {
			continue
		}
		if result != nil {
			return nil
		}
		result = &CreditsPoint{StartTicks: chapter.StartTicks, Provenance: "Chapter"}
	}
	if !validCreditsPoint(result, info.DurationTicks) {
		return nil
	}
	return result
}

func projectItemCredits(item *Item, encoded []byte) error {
	if item == nil || item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		return nil
	}
	item.Credits = ExplicitChapterCredits(item.Media)
	if len(encoded) == 0 || string(encoded) == "null" {
		return nil
	}
	var point CreditsPoint
	if err := json.Unmarshal(encoded, &point); err != nil {
		return fmt.Errorf("decode credits point: %w", err)
	}
	if validCreditsPoint(&point, item.Media.DurationTicks) {
		item.Credits = &point
	}
	return nil
}

type creditsRecord struct {
	detail              CreditsDetail
	libraryID, parentID string
}

func readCreditsRecord(ctx context.Context, tx pgx.Tx, itemID string, lock bool) (creditsRecord, error) {
	var result creditsRecord
	if lock {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM items WHERE id=$1 AND type IN ('Movie','Episode') AND NOT is_folder FOR UPDATE`, itemID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result, ErrNotFound
			}
			return result, err
		}
	}
	var raw []byte
	var stored, provenance string
	var start *int64
	statement := `SELECT i.id,i.library_id,COALESCE(i.parent_id,''),i.media,` + introSourceRevisionSQL + `,
        COALESCE(credits.revision,0)::text,COALESCE(credits.source_revision,''),credits.start_ticks,
        COALESCE(credits.provenance,''),COALESCE(credits.last_edited_by,''),credits.last_edited_at
        FROM items i LEFT JOIN item_credits_state credits ON credits.item_id=i.id
        WHERE i.id=$1 AND i.type IN ('Movie','Episode') AND NOT i.is_folder`
	err := tx.QueryRow(ctx, statement, itemID).Scan(&result.detail.ItemID, &result.libraryID, &result.parentID,
		&raw, &result.detail.SourceRevision, &result.detail.Revision, &stored, &start, &provenance,
		&result.detail.LastEditedBy, &result.detail.LastEditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	var info media.Info
	if json.Unmarshal(raw, &info) != nil || info.DurationTicks <= 0 || len(info.Streams) == 0 {
		return result, ErrUnavailable
	}
	result.detail.MediaSourceID = media.SourceID(itemID)
	result.detail.DurationTicks = info.DurationTicks
	result.detail.Automatic = ExplicitChapterCredits(&info)
	result.detail.Effective = result.detail.Automatic
	if start != nil {
		result.detail.Override = &CreditsPoint{StartTicks: *start, Provenance: provenance}
		result.detail.OverrideStale = stored != result.detail.SourceRevision || !validCreditsPoint(result.detail.Override, info.DurationTicks)
		if !result.detail.OverrideStale {
			result.detail.Effective = result.detail.Override
		}
	}
	return result, nil
}

func (s *Store) GetItemCredits(ctx context.Context, actor identity.Principal, itemID string) (CreditsDetail, error) {
	// Reuse the existing administrator/source-open boundary without holding a
	// database connection across filesystem access.
	source, err := s.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		return CreditsDetail{}, err
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return CreditsDetail{}, err
	}
	defer rollback(tx)
	result, err := readCreditsRecord(ctx, tx, itemID, false)
	if err != nil {
		return CreditsDetail{}, err
	}
	if result.detail.SourceRevision != source.SourceRevision {
		return CreditsDetail{}, ErrCreditsRevisionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return CreditsDetail{}, err
	}
	detection, err := s.creditsDetectionAsAdministrator(ctx, actor, itemID, source.SourceRevision)
	if err != nil {
		return CreditsDetail{}, err
	}
	// Physical support checks may block on remote storage. Recheck the marker
	// revision afterward so an administrator edit cannot be hidden by an older
	// effective point in this response.
	finalTx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return CreditsDetail{}, err
	}
	defer rollback(finalTx)
	finalRecord, err := readCreditsRecord(ctx, finalTx, itemID, false)
	if err != nil {
		return CreditsDetail{}, err
	}
	if finalRecord.detail.SourceRevision != result.detail.SourceRevision || finalRecord.detail.Revision != result.detail.Revision {
		return CreditsDetail{}, ErrCreditsRevisionConflict
	}
	if err := finalTx.Commit(ctx); err != nil {
		return CreditsDetail{}, err
	}
	applyCreditsDetection(&result.detail, detection)
	return result.detail, nil
}

func (s *Store) UpdateItemCredits(ctx context.Context, actor identity.Principal, itemID string, edit CreditsEdit, reset bool) (CreditsDetail, error) {
	if s == nil || s.pool == nil {
		return CreditsDetail{}, ErrUnavailable
	}
	if !validMetadataActor(actor) {
		return CreditsDetail{}, ErrForbidden
	}
	revision, err := strconv.ParseInt(edit.Revision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != edit.Revision || edit.SourceRevision == "" || !metadataIdentifier(itemID) {
		return CreditsDetail{}, ErrInvalidInput
	}
	if !reset && edit.Provenance != "Manual" && edit.Provenance != "Import" {
		return CreditsDetail{}, ErrInvalidInput
	}
	current, err := s.GetItemCredits(ctx, actor, itemID)
	if err != nil {
		return CreditsDetail{}, err
	}
	if current.Revision != edit.Revision || current.SourceRevision != edit.SourceRevision {
		return CreditsDetail{}, ErrCreditsRevisionConflict
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return CreditsDetail{}, err
	}
	defer rollback(tx)
	if err := lockMetadataActor(ctx, tx, actor); err != nil {
		return CreditsDetail{}, err
	}
	record, err := readCreditsRecord(ctx, tx, itemID, true)
	if err != nil {
		return CreditsDetail{}, err
	}
	if record.detail.Revision != edit.Revision || record.detail.SourceRevision != edit.SourceRevision {
		return CreditsDetail{}, ErrCreditsRevisionConflict
	}
	var start *int64
	source, provenance := "", ""
	if !reset {
		point := &CreditsPoint{StartTicks: edit.StartTicks, Provenance: edit.Provenance}
		if !validCreditsPoint(point, record.detail.DurationTicks) {
			return CreditsDetail{}, ErrInvalidInput
		}
		start, source, provenance = &edit.StartTicks, edit.SourceRevision, edit.Provenance
	}
	var saved int64
	err = tx.QueryRow(ctx, `INSERT INTO item_credits_state(item_id,source_revision,start_ticks,provenance,last_edited_by)
        VALUES($1,$2,$3,$4,$5) ON CONFLICT(item_id) DO UPDATE SET revision=item_credits_state.revision+1,
        source_revision=excluded.source_revision,start_ticks=excluded.start_ticks,provenance=excluded.provenance,
        last_edited_by=excluded.last_edited_by,last_edited_at=clock_timestamp()
        WHERE item_credits_state.revision=$6 RETURNING revision`, itemID, source, start, provenance, actor.User.ID, revision).Scan(&saved)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreditsDetail{}, ErrCreditsRevisionConflict
	}
	if err != nil {
		return CreditsDetail{}, err
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: record.libraryID, ParentID: record.parentID}); err != nil {
		return CreditsDetail{}, err
	}
	if err := (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, false); err != nil {
		return CreditsDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreditsDetail{}, err
	}
	return s.GetItemCredits(ctx, actor, itemID)
}
