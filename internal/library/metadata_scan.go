package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type scannedMetadataBase struct {
	Name, SortName, Overview string
}

type scannedMetadataOptions struct {
	Base          *scannedMetadataBase
	ForceEntities bool
}

// syncScannedMetadata runs after the scanner has written its newly accepted
// automatic fields, inside that same owned transaction. It reads administrator
// state only now, never from a stale snapshot taken before probing a file.
func syncScannedMetadata(ctx context.Context, tx pgx.Tx, itemID string, options ...scannedMetadataOptions) error {
	var automatic, sourceKey, localSource, musicSource, rawOverrides, rawLocks []byte
	var itemType, name, sortName, overview string
	var indexNumber, parentIndexNumber int
	err := tx.QueryRow(ctx, `SELECT i.name, i.sort_name, i.overview, i.type, i.index_number, i.parent_index_number,
		i.local_metadata, ms.music_source, ms.overrides, ms.locked_values, catalog_metadata_source_key(i)
		FROM items i JOIN item_metadata_state ms ON ms.item_id = i.id
		WHERE i.id = $1 FOR UPDATE OF ms`, itemID).
		Scan(&name, &sortName, &overview, &itemType, &indexNumber, &parentIndexNumber,
			&localSource, &musicSource, &rawOverrides, &rawLocks, &sourceKey)
	if err != nil {
		return fmt.Errorf("read scanned metadata state: %w", err)
	}
	var selected scannedMetadataOptions
	if len(options) != 0 {
		selected = options[0]
	}
	if selected.Base != nil {
		name, sortName, overview = selected.Base.Name, selected.Base.SortName, selected.Base.Overview
	}
	musicHash, err := acceptedMusicSourceHash(musicSource)
	if err != nil {
		return err
	}
	if musicHash != "" {
		chosenName, err := acceptedMusicName(localSource, musicSource, name)
		if err != nil {
			return err
		}
		if chosenName != name {
			local, err := metadataSourceObject(localSource)
			if err != nil {
				return err
			}
			var localSortName string
			if raw, found := local["SortName"]; found {
				if err := json.Unmarshal(raw, &localSortName); err != nil {
					return fmt.Errorf("read local music sort name: %w", err)
				}
			}
			if localSortName == "" {
				sortName = strings.ToLower(chosenName)
			}
			name = chosenName
		}
		key, err := metadataSourceObject(sourceKey)
		if err != nil {
			return err
		}
		key["MusicSourceHash"], err = json.Marshal(musicHash)
		if err != nil {
			return err
		}
		sourceKey, err = json.Marshal(key)
		if err != nil {
			return err
		}
	}
	mergedSource, err := mergeAcceptedMusicSource(localSource, musicSource)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT catalog_metadata_automatic_values($1, $2, $3, $4, $5, $6, $7::jsonb)",
		name, sortName, overview, itemType, indexNumber, parentIndexNumber, mergedSource).Scan(&automatic); err != nil {
		return fmt.Errorf("compose scanned automatic metadata: %w", err)
	}
	overrides, err := metadataSourceObject(rawOverrides)
	if err != nil {
		return err
	}
	locks, err := metadataSourceObject(rawLocks)
	if err != nil {
		return err
	}
	activeOverrides := activeMetadataControls(itemType, overrides)
	activeLocks := activeMetadataControls(itemType, locks)
	effective, _, err := composeMetadataValues(automatic, activeOverrides, activeLocks)
	if err != nil {
		return err
	}
	projection, err := buildMetadataProjection(mergedSource, activeOverrides, activeLocks, effective)
	if err != nil {
		return err
	}
	var changed, projectionChanged bool
	if err := tx.QueryRow(ctx, `SELECT automatic IS DISTINCT FROM $2::jsonb OR source_key IS DISTINCT FROM $3::jsonb,
		effective IS DISTINCT FROM $4::jsonb FROM item_metadata_state WHERE item_id = $1`,
		itemID, automatic, sourceKey, projection).Scan(&changed, &projectionChanged); err != nil {
		return fmt.Errorf("compare scanned metadata state: %w", err)
	}
	if changed || projectionChanged {
		if _, err := tx.Exec(ctx, `UPDATE item_metadata_state SET automatic = $2, source_key = $3,
			effective = $4, revision = revision + CASE WHEN $5 THEN 1 ELSE 0 END,
			updated_at = CASE WHEN $5 THEN clock_timestamp() ELSE updated_at END WHERE item_id = $1`,
			itemID, automatic, sourceKey, projection, changed); err != nil {
			return fmt.Errorf("update scanned metadata state: %w", err)
		}
	}
	return applyEffectiveMetadata(ctx, tx, itemID, effective, projection, projectionChanged || selected.ForceEntities)
}
