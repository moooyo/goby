package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// expectedEpisodeItemsSQL supplies an items-compatible virtual relation for a
// caller-owned authorized query transaction. Nothing is inserted into items.
// Availability is derived inside the same snapshot as counts and pagination.
// An ambiguous structural season never grants a fallback route around a denied
// season: zero seasons uses the series, exactly one uses that season, and two or
// more require an administrator to resolve the physical catalog ambiguity.
func expectedEpisodeItemsSQL(access libraryAccess) string {
	name := `COALESCE(NULLIF(e.name,''),'Episode ' || e.episode_number::text)`
	return `SELECT (jsonb_populate_record(NULL::items,jsonb_build_object(
		'id',e.id,'library_id',s.library_id,'root_id',NULL,
		'parent_id',COALESCE(roster_season.id,s.id),'name',` + name + `,'sort_name',` + name + `,
		'type','Episode','path','','relative_path','','overview','','is_folder',false,
		'index_number',e.episode_number,'parent_index_number',e.season_number,'media',NULL,
		'file_identity','','file_size',0,'modified_at',NULL,'created_at',e.created_at,'updated_at',e.updated_at,
		'local_metadata',jsonb_strip_nulls(jsonb_build_object('Kind','Episode','Name',` + name + `,
			'IndexNumber',e.episode_number,'ParentIndexNumber',e.season_number,
			'PremiereDate',to_char(e.premiere_date,'YYYY-MM-DD') || 'T00:00:00Z',
			'ProductionYear',extract(year FROM e.premiere_date)::integer)),
		'local_metadata_hash','','local_metadata_path',''))).*,
		jsonb_build_object('PremiereDateKnown',e.premiere_date IS NOT NULL,
			'IsUnaired',COALESCE(e.premiere_date > (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,false)) AS expected_episode
		FROM expected_episodes e JOIN series_episode_rosters roster ON roster.series_id=e.series_id
		JOIN items s ON s.id=e.series_id
		JOIN LATERAL (SELECT count(*) AS count,min(season.id) AS id FROM items season
			WHERE season.parent_id=s.id AND season.library_id=s.library_id AND season.type='Season' AND season.is_folder
			AND season.index_number=e.season_number AND ` + ordinaryItemSQL("season") + `) season_match ON season_match.count<=1
		LEFT JOIN items roster_season ON roster_season.id=season_match.id
		WHERE e.active AND roster.state='active' AND e.source_key=roster.source_key
			AND s.type='Series' AND s.is_folder AND ` + access.ordinarySQL("s") + `
			AND (season_match.count=0 OR ` + access.ordinarySQL("roster_season") + `)
			AND NOT EXISTS (SELECT 1 FROM items physical LEFT JOIN items physical_parent ON physical_parent.id=physical.parent_id
				WHERE ` + episodeRosterPhysicalMatchSQL("e", "physical", "physical_parent") + `)`
}

func readExpectedEpisode(ctx context.Context, tx pgx.Tx, access libraryAccess, id string) (Item, error) {
	if !IsExpectedEpisodeID(id) {
		return Item{}, ErrNotFound
	}
	var fact []byte
	item, err := scanItem(tx.QueryRow(ctx, `SELECT `+access.itemColumnsSQL()+`,i.expected_episode FROM (`+
		expectedEpisodeItemsSQL(access)+`) i WHERE i.id=$1 AND `+access.itemPolicySQL("i"), id), &fact)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read expected episode: %w", err)
	}
	var info ExpectedEpisodeInfo
	if len(fact) == 0 || json.Unmarshal(fact, &info) != nil {
		return Item{}, ErrUnavailable
	}
	item.ExpectedEpisode = &info
	item.CanPlay = false
	return item, nil
}
