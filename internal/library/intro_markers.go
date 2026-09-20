package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// IntroInterval is a single source-bound interval in 100 ns media ticks.
// Only explicit chapter boundaries or an administrator-supplied interval qualify.
type IntroInterval struct {
	StartTicks int64  `json:"StartTicks"`
	EndTicks   int64  `json:"EndTicks"`
	Provenance string `json:"Provenance"`
}

type IntroDetail struct {
	ItemID         string         `json:"ItemId"`
	MediaSourceID  string         `json:"MediaSourceId"`
	SourceRevision string         `json:"SourceRevision"`
	Revision       string         `json:"Revision"`
	DurationTicks  int64          `json:"DurationTicks"`
	Automatic      *IntroInterval `json:"Automatic"`
	Effective      *IntroInterval `json:"Effective"`
	Override       *IntroInterval `json:"Override"`
	OverrideSource string         `json:"OverrideSource"`
	OverrideStale  bool           `json:"OverrideStale"`
	LastEditedBy   string         `json:"LastEditedBy"`
	LastEditedAt   *time.Time     `json:"LastEditedAt"`
}

type IntroEdit struct {
	Revision       string `json:"Revision"`
	SourceRevision string `json:"SourceRevision"`
	StartTicks     int64  `json:"StartTicks"`
	EndTicks       int64  `json:"EndTicks"`
	Provenance     string `json:"Provenance"`
}

var ErrIntroRevisionConflict = errors.New("intro revision conflict")

// This is an invalidation stamp, not a credential or a content hash. Include
// root binding, indexed file identity and complete probe facts so a rebind,
// replacement, edit or reprobe cannot inherit an old administrator interval.
const introSourceRevisionSQL = `('intro-source-v1-' || md5(jsonb_build_array(i.root_id,
	i.relative_path, i.file_identity, i.file_size, extract(epoch FROM i.modified_at), i.media,
	(SELECT ir.binding_revision FROM library_roots ir WHERE ir.id=i.root_id))::text))`

const itemIntroColumn = `CASE WHEN i.type IN ('Movie','Episode') AND NOT i.is_folder THEN (
	SELECT jsonb_build_object('StartTicks', intro.start_ticks, 'EndTicks', intro.end_ticks,
		'Provenance', intro.provenance) FROM item_intro_state intro
	WHERE intro.item_id=i.id AND intro.start_ticks IS NOT NULL
		AND intro.source_revision=` + introSourceRevisionSQL + `) END`

func validIntroInterval(interval *IntroInterval, duration int64) bool {
	return interval != nil && duration > 0 && interval.StartTicks >= 0 &&
		interval.StartTicks < interval.EndTicks && interval.EndTicks <= duration
}

// ExplicitChapterIntro accepts the exact reserved chapter titles IntroStart
// and IntroEnd. Ordinary chapter names, including Opening, are never evidence.
// Multiple pairs or unpaired/reversed boundaries are ambiguous and fail closed.
func ExplicitChapterIntro(info *media.Info) *IntroInterval {
	if info == nil {
		return nil
	}
	var start, end *int64
	for _, chapter := range info.Chapters {
		switch strings.TrimSpace(chapter.Title) {
		case "IntroStart":
			if start != nil {
				return nil
			}
			value := chapter.StartTicks
			start = &value
		case "IntroEnd":
			if end != nil {
				return nil
			}
			value := chapter.StartTicks
			end = &value
		}
	}
	if start == nil || end == nil {
		return nil
	}
	interval := &IntroInterval{StartTicks: *start, EndTicks: *end, Provenance: "Chapter"}
	if !validIntroInterval(interval, info.DurationTicks) {
		return nil
	}
	return interval
}

func projectItemIntro(item *Item, encoded []byte) error {
	if item == nil || item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		return nil
	}
	item.Intro = ExplicitChapterIntro(item.Media)
	if len(encoded) == 0 || string(encoded) == "null" {
		return nil
	}
	var interval IntroInterval
	if err := json.Unmarshal(encoded, &interval); err != nil {
		return fmt.Errorf("decode item intro: %w", err)
	}
	if validIntroInterval(&interval, item.Media.DurationTicks) {
		item.Intro = &interval
	}
	return nil
}

type introRecord struct {
	detail              IntroDetail
	libraryID, parentID string
	storedSource        string
	start, end          *int64
	info                media.Info
}

func readIntroRecord(ctx context.Context, tx pgx.Tx, itemID string, lock bool) (introRecord, error) {
	var record introRecord
	var raw []byte
	if lock {
		// Lock the parent before reading the optional state in a fresh statement.
		// A left join evaluated before waiting must not retain an old CAS value.
		var locked string
		err := tx.QueryRow(ctx, `SELECT id FROM items WHERE id=$1 AND type IN ('Movie','Episode') AND NOT is_folder FOR UPDATE`, itemID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return introRecord{}, ErrNotFound
		}
		if err != nil {
			return introRecord{}, err
		}
	}
	statement := `SELECT i.id,i.library_id,COALESCE(i.parent_id,''),i.media,` + introSourceRevisionSQL + `,
		COALESCE(intro.revision,0)::text,COALESCE(intro.source_revision,''),intro.start_ticks,intro.end_ticks,
		COALESCE(intro.provenance,''),COALESCE(intro.last_edited_by,''),intro.last_edited_at
		FROM items i LEFT JOIN item_intro_state intro ON intro.item_id=i.id
		WHERE i.id=$1 AND i.type IN ('Movie','Episode') AND NOT i.is_folder`
	err := tx.QueryRow(ctx, statement, itemID).Scan(&record.detail.ItemID, &record.libraryID, &record.parentID,
		&raw, &record.detail.SourceRevision, &record.detail.Revision, &record.storedSource,
		&record.start, &record.end, &record.detail.OverrideSource, &record.detail.LastEditedBy, &record.detail.LastEditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return introRecord{}, ErrNotFound
	}
	if err != nil {
		return introRecord{}, fmt.Errorf("read intro state: %w", err)
	}
	if json.Unmarshal(raw, &record.info) != nil || record.info.DurationTicks <= 0 || len(record.info.Streams) == 0 {
		return introRecord{}, fmt.Errorf("%w: a current indexed media source is required", ErrUnavailable)
	}
	record.detail.MediaSourceID = media.SourceID(itemID)
	record.detail.DurationTicks = record.info.DurationTicks
	record.detail.Automatic = ExplicitChapterIntro(&record.info)
	record.detail.Effective = record.detail.Automatic
	if record.start != nil && record.end != nil {
		record.detail.Override = &IntroInterval{StartTicks: *record.start, EndTicks: *record.end, Provenance: record.detail.OverrideSource}
		record.detail.OverrideStale = record.storedSource != record.detail.SourceRevision || !validIntroInterval(record.detail.Override, record.info.DurationTicks)
		if !record.detail.OverrideStale {
			record.detail.Effective = record.detail.Override
		}
	}
	return record, nil
}

// GetIntroFor rechecks current subject authority and the actual local source
// snapshot used by playback. It never changes playback position or user state.
func (s *Store) GetIntroFor(ctx context.Context, subject Subject, itemID, sourceID string) (*IntroInterval, error) {
	file, source, err := s.OpenMediaFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return source.Item.Intro, nil
}

func (s *Store) GetItemIntro(ctx context.Context, actor identity.Principal, itemID string) (IntroDetail, error) {
	if s == nil || s.pool == nil {
		return IntroDetail{}, ErrUnavailable
	}
	if !metadataIdentifier(itemID) {
		return IntroDetail{}, ErrInvalidInput
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return IntroDetail{}, err
	}
	defer rollback(tx)
	record, err := readIntroRecord(ctx, tx, itemID, false)
	if err != nil {
		return IntroDetail{}, err
	}
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, "")
	if err != nil {
		return IntroDetail{}, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
		return IntroDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IntroDetail{}, err
	}
	// Do not hold a database connection while storage may be unavailable.
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openPublicMediaSource(ctx, snapshot)
		return file, snapshot.mediaFile, err
	})
	if err != nil {
		return IntroDetail{}, err
	}
	if err := file.Close(); err != nil {
		return IntroDetail{}, err
	}
	return record.detail, nil
}

// UpdateItemIntro replaces the administrator layer. Reset keeps a revision
// tombstone, preventing an old initial revision from succeeding after deletion.
func (s *Store) UpdateItemIntro(ctx context.Context, actor identity.Principal, itemID string, edit IntroEdit, reset bool) (IntroDetail, error) {
	if s == nil || s.pool == nil {
		return IntroDetail{}, ErrUnavailable
	}
	if !validMetadataActor(actor) {
		return IntroDetail{}, ErrForbidden
	}
	if !metadataIdentifier(itemID) {
		return IntroDetail{}, ErrInvalidInput
	}
	revision, err := strconv.ParseInt(edit.Revision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != edit.Revision || edit.SourceRevision == "" {
		return IntroDetail{}, ErrInvalidInput
	}
	if !reset && edit.Provenance != "Manual" && edit.Provenance != "Import" {
		return IntroDetail{}, ErrInvalidInput
	}
	// Confirm the actual indexed source outside the write transaction, then
	// compare its stamp again under the item lock before publishing the edit.
	current, err := s.GetItemIntro(ctx, actor, itemID)
	if err != nil {
		return IntroDetail{}, err
	}
	if current.Revision != edit.Revision || current.SourceRevision != edit.SourceRevision {
		return IntroDetail{}, ErrIntroRevisionConflict
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return IntroDetail{}, err
	}
	defer rollback(tx)
	if err := lockMetadataActor(ctx, tx, actor); err != nil {
		return IntroDetail{}, err
	}
	record, err := readIntroRecord(ctx, tx, itemID, true)
	if err != nil {
		return IntroDetail{}, err
	}
	if record.detail.Revision != edit.Revision || record.detail.SourceRevision != edit.SourceRevision {
		return IntroDetail{}, ErrIntroRevisionConflict
	}
	var start, end any
	source, provenance := "", ""
	if !reset {
		interval := &IntroInterval{StartTicks: edit.StartTicks, EndTicks: edit.EndTicks, Provenance: edit.Provenance}
		if !validIntroInterval(interval, record.info.DurationTicks) {
			return IntroDetail{}, ErrInvalidInput
		}
		start, end, source, provenance = edit.StartTicks, edit.EndTicks, edit.SourceRevision, edit.Provenance
	}
	_, err = tx.Exec(ctx, `INSERT INTO item_intro_state
		(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by,last_edited_at)
		VALUES ($1,1,$2,$3,$4,$5,$6,clock_timestamp()) ON CONFLICT (item_id) DO UPDATE SET
		revision=item_intro_state.revision+1,source_revision=EXCLUDED.source_revision,start_ticks=EXCLUDED.start_ticks,
		end_ticks=EXCLUDED.end_ticks,provenance=EXCLUDED.provenance,last_edited_by=EXCLUDED.last_edited_by,
		last_edited_at=EXCLUDED.last_edited_at`, itemID, source, start, end, provenance, actor.User.ID)
	if err != nil {
		return IntroDetail{}, fmt.Errorf("write intro state: %w", err)
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: record.libraryID, ParentID: record.parentID}); err != nil {
		return IntroDetail{}, err
	}
	record, err = readIntroRecord(ctx, tx, itemID, false)
	if err != nil {
		return IntroDetail{}, err
	}
	if err := (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, false); err != nil {
		return IntroDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IntroDetail{}, err
	}
	return record.detail, nil
}
