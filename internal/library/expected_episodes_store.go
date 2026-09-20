package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func checkEpisodeRosterActor(ctx context.Context, tx pgx.Tx, actor identity.Principal, lock bool) error {
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, lock); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			return errors.Join(ErrForbidden, err)
		}
		return err
	}
	return nil
}

func (s *Store) GetEpisodeRoster(ctx context.Context, actor identity.Principal, seriesID string) (EpisodeRosterDetail, error) {
	if s == nil || s.pool == nil {
		return EpisodeRosterDetail{}, ErrUnavailable
	}
	if !metadataIdentifier(seriesID) {
		return EpisodeRosterDetail{}, ErrInvalidInput
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	defer rollback(tx)
	if err := checkEpisodeRosterActor(ctx, tx, actor, false); err != nil {
		return EpisodeRosterDetail{}, err
	}
	detail, _, err := readEpisodeRoster(ctx, tx, seriesID, false)
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	if err := checkEpisodeRosterActor(ctx, tx, actor, false); err != nil {
		return EpisodeRosterDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EpisodeRosterDetail{}, err
	}
	return detail, nil
}

// Physical availability is a catalog fact, not a user-specific assertion that a
// resource is playable. A hidden physical episode must not become a visible
// missing placeholder simply because it is excluded by an account policy.
func episodeRosterPhysicalMatchSQL(fact, episode, parent string) string {
	return episode + `.type='Episode' AND NOT ` + episode + `.is_folder AND jsonb_typeof(` + episode + `.media)='object'
		AND ` + episode + `.root_id IS NOT NULL AND ` + episode + `.path<>'' AND ` + ordinaryItemSQL(episode) + `
		AND ` + episode + `.library_id=s.library_id AND ` + episode + `.index_number=` + fact + `.episode_number
		AND ((` + episode + `.parent_id=` + fact + `.series_id AND ` + episode + `.parent_index_number=` + fact + `.season_number)
		OR (` + parent + `.id=` + episode + `.parent_id AND ` + parent + `.library_id=` + episode + `.library_id
			AND ` + parent + `.parent_id=` + fact + `.series_id AND ` + parent + `.type='Season' AND ` + parent + `.is_folder
			AND ` + parent + `.index_number=` + fact + `.season_number AND ` + ordinaryItemSQL(parent) + `))`
}

func readEpisodeRoster(ctx context.Context, tx pgx.Tx, seriesID string, lock bool) (EpisodeRosterDetail, CatalogChange, error) {
	// ownedTx.Query is the embedded pgx method. Keep its cursor on the protected
	// write context so an HTTP cancellation cannot terminate the lock session.
	if owned, ok := tx.(*ownedTx); ok {
		ctx = owned.ctx
	}
	detail := EpisodeRosterDetail{SeriesID: seriesID, Revision: "0", State: "absent", Entries: []EpisodeRosterEntry{}}
	change := CatalogChange{Kind: CatalogUpdated, ItemID: seriesID, IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true}
	statement := `SELECT i.name,i.library_id,COALESCE(i.parent_id,'') FROM items i
		WHERE i.id=$1 AND i.type='Series' AND i.is_folder AND ` + ordinaryItemSQL("i")
	if lock {
		statement += " FOR UPDATE OF i"
	}
	err := tx.QueryRow(ctx, statement, seriesID).Scan(&detail.SeriesName, &change.LibraryID, &change.ParentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return EpisodeRosterDetail{}, CatalogChange{}, ErrNotFound
	}
	if err != nil {
		return EpisodeRosterDetail{}, CatalogChange{}, err
	}
	source := EpisodeRosterSource{Kind: "admin_import"}
	err = tx.QueryRow(ctx, `SELECT revision::text,state,source_key,source_label,source_revision,
		parser_version,payload_sha256,last_edited_by,updated_at FROM series_episode_rosters WHERE series_id=$1`, seriesID).
		Scan(&detail.Revision, &detail.State, &source.Key, &source.Label, &source.Revision,
			&source.ParserVersion, &source.SHA256, &detail.LastEditedBy, &detail.LastEditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return detail, change, nil
	}
	if err != nil {
		return EpisodeRosterDetail{}, CatalogChange{}, err
	}
	detail.Source = &source
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM expected_episodes WHERE series_id=$1 AND NOT active`, seriesID).Scan(&detail.RetiredCount); err != nil {
		return EpisodeRosterDetail{}, CatalogChange{}, err
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.entry_key,e.season_number,e.episode_number,e.name,
		COALESCE(to_char(e.premiere_date,'YYYY-MM-DD'),''),
		CASE WHEN e.premiere_date IS NULL THEN 'unknown'
			WHEN e.premiere_date > (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date THEN 'unaired' ELSE 'aired' END,
		ARRAY(SELECT physical.id FROM items physical
			LEFT JOIN items physical_parent ON physical_parent.id=physical.parent_id
			WHERE `+episodeRosterPhysicalMatchSQL("e", "physical", "physical_parent")+` ORDER BY physical.id LIMIT 8),
		(SELECT count(*) FROM items physical LEFT JOIN items physical_parent ON physical_parent.id=physical.parent_id
			WHERE `+episodeRosterPhysicalMatchSQL("e", "physical", "physical_parent")+`)
		FROM expected_episodes e JOIN items s ON s.id=e.series_id
		JOIN series_episode_rosters roster ON roster.series_id=e.series_id
		WHERE e.series_id=$1 AND e.active AND roster.state='active' AND e.source_key=roster.source_key
		ORDER BY e.season_number,e.episode_number,e.entry_key`, seriesID)
	if err != nil {
		return EpisodeRosterDetail{}, CatalogChange{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry EpisodeRosterEntry
		if err := rows.Scan(&entry.ID, &entry.Key, &entry.SeasonNumber, &entry.EpisodeNumber, &entry.Name,
			&entry.PremiereDate, &entry.Airing, &entry.AvailableItemIDs, &entry.AvailableItemCount); err != nil {
			return EpisodeRosterDetail{}, CatalogChange{}, err
		}
		entry.Availability = "missing"
		if len(entry.AvailableItemIDs) != 0 {
			entry.Availability = "available"
		}
		if entry.AvailableItemIDs == nil {
			entry.AvailableItemIDs = []string{}
		}
		detail.Entries = append(detail.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return EpisodeRosterDetail{}, CatalogChange{}, err
	}
	return detail, change, nil
}

// ReplaceEpisodeRoster atomically replaces the one explicit roster authority
// for a series. Invalid imports leave the prior source and all facts untouched.
func (s *Store) ReplaceEpisodeRoster(ctx context.Context, actor identity.Principal, seriesID string, edit EpisodeRosterEdit) (EpisodeRosterDetail, error) {
	if s == nil || s.pool == nil {
		return EpisodeRosterDetail{}, ErrUnavailable
	}
	if !metadataIdentifier(seriesID) {
		return EpisodeRosterDetail{}, ErrInvalidInput
	}
	edit, payload, digest, err := normalizeEpisodeRosterEdit(edit)
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	return s.mutateEpisodeRoster(ctx, actor, seriesID, edit, payload, digest, false)
}

// WithdrawEpisodeRoster retires facts explicitly and retains a revision
// tombstone and immutable source history. It never deletes physical media.
func (s *Store) WithdrawEpisodeRoster(ctx context.Context, actor identity.Principal, seriesID, revision string) (EpisodeRosterDetail, error) {
	if s == nil || s.pool == nil {
		return EpisodeRosterDetail{}, ErrUnavailable
	}
	if !metadataIdentifier(seriesID) {
		return EpisodeRosterDetail{}, ErrInvalidInput
	}
	if _, err := episodeRosterRevision(revision); err != nil {
		return EpisodeRosterDetail{}, err
	}
	return s.mutateEpisodeRoster(ctx, actor, seriesID, EpisodeRosterEdit{Revision: revision}, nil, "", true)
}

func (s *Store) mutateEpisodeRoster(ctx context.Context, actor identity.Principal, seriesID string, edit EpisodeRosterEdit, payload []byte, digest string, withdraw bool) (EpisodeRosterDetail, error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	defer rollback(tx)
	if err := checkEpisodeRosterActor(ctx, tx, actor, true); err != nil {
		return EpisodeRosterDetail{}, err
	}
	// Lock the physical series first, then read the optional roster in a fresh
	// statement. Two initial Revision=0 requests cannot both create a roster.
	detail, change, err := readEpisodeRoster(ctx, tx, seriesID, true)
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	if detail.Revision != edit.Revision {
		return EpisodeRosterDetail{}, ErrRevisionConflict
	}
	revision, err := episodeRosterRevision(detail.Revision)
	if err != nil || revision == math.MaxInt64 {
		return EpisodeRosterDetail{}, ErrRevisionConflict
	}
	if withdraw && detail.State != "active" || !withdraw && detail.State == "active" && detail.Source.SHA256 == digest {
		if err := checkEpisodeRosterActor(ctx, tx, actor, false); err != nil {
			return EpisodeRosterDetail{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return EpisodeRosterDetail{}, err
		}
		return detail, nil
	}
	action := "replace"
	if withdraw {
		action = "withdraw"
		edit.Source = EpisodeRosterSourceInput{Key: detail.Source.Key, Label: detail.Source.Label, Revision: detail.Source.Revision}
		digest = detail.Source.SHA256
		if err := tx.QueryRow(ctx, `SELECT payload FROM episode_roster_imports WHERE series_id=$1 AND revision=$2`, seriesID, revision).Scan(&payload); err != nil {
			return EpisodeRosterDetail{}, err
		}
	} else {
		var conflicting bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM episode_roster_imports
			WHERE series_id=$1 AND source_key=$2 AND source_revision=$3 AND payload_sha256<>$4)`,
			seriesID, edit.Source.Key, edit.Source.Revision, digest).Scan(&conflicting); err != nil {
			return EpisodeRosterDetail{}, err
		}
		if conflicting {
			return EpisodeRosterDetail{}, ErrEpisodeRosterSourceRevision
		}
	}
	next, state := revision+1, "active"
	if withdraw {
		state = "withdrawn"
	}
	_, err = tx.Exec(ctx, `INSERT INTO series_episode_rosters
		(series_id,revision,state,source_key,source_label,source_revision,parser_version,payload_sha256,last_edited_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (series_id) DO UPDATE SET
		revision=EXCLUDED.revision,state=EXCLUDED.state,source_key=EXCLUDED.source_key,source_label=EXCLUDED.source_label,
		source_revision=EXCLUDED.source_revision,parser_version=EXCLUDED.parser_version,payload_sha256=EXCLUDED.payload_sha256,
		last_edited_by=EXCLUDED.last_edited_by,updated_at=clock_timestamp()`, seriesID, next, state,
		edit.Source.Key, edit.Source.Label, edit.Source.Revision, EpisodeRosterParserVersion, digest, actor.User.ID)
	if err != nil {
		return EpisodeRosterDetail{}, fmt.Errorf("write episode roster: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO episode_roster_imports
		(series_id,revision,action,source_key,source_label,source_revision,parser_version,payload,payload_sha256,actor_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, seriesID, next, action, edit.Source.Key, edit.Source.Label,
		edit.Source.Revision, EpisodeRosterParserVersion, payload, digest, actor.User.ID)
	if err != nil {
		return EpisodeRosterDetail{}, fmt.Errorf("retain episode roster import: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE expected_episodes SET active=false,retired_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE series_id=$1 AND active`, seriesID); err != nil {
		return EpisodeRosterDetail{}, err
	}
	if !withdraw {
		for _, entry := range edit.Entries {
			var date any
			if entry.PremiereDate != "" {
				value, _ := time.Parse(time.DateOnly, entry.PremiereDate)
				date = value
			}
			_, err := tx.Exec(ctx, `INSERT INTO expected_episodes
				(id,series_id,source_key,entry_key,season_number,episode_number,name,premiere_date,active,import_revision)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,true,$9) ON CONFLICT (series_id,source_key,entry_key) DO UPDATE SET
				season_number=EXCLUDED.season_number,episode_number=EXCLUDED.episode_number,name=EXCLUDED.name,
				premiere_date=EXCLUDED.premiere_date,active=true,import_revision=EXCLUDED.import_revision,
				updated_at=clock_timestamp(),retired_at=NULL`, expectedEpisodeID(seriesID, edit.Source.Key, entry.Key), seriesID,
				edit.Source.Key, entry.Key, entry.SeasonNumber, entry.EpisodeNumber, entry.Name, date, next)
			if err != nil {
				return EpisodeRosterDetail{}, fmt.Errorf("write expected episode: %w", err)
			}
		}
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		return EpisodeRosterDetail{}, err
	}
	detail, _, err = readEpisodeRoster(ctx, tx, seriesID, false)
	if err != nil {
		return EpisodeRosterDetail{}, err
	}
	if detail.Revision != strconv.FormatInt(next, 10) {
		return EpisodeRosterDetail{}, ErrUnavailable
	}
	if err := checkEpisodeRosterActor(ctx, tx, actor, false); err != nil {
		return EpisodeRosterDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EpisodeRosterDetail{}, err
	}
	return detail, nil
}
