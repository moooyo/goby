package backuppg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

// Validate explicit roster authority without consulting media, numbering gaps,
// a clock, or external providers. Historical imports and retired identities are
// retained facts. A surviving owner may be dormant after catalog reclassification;
// read paths require a current ordinary Series before exposing active entries.
func validateSelectedPhase3State(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 46 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM series_episode_rosters roster
			LEFT JOIN items owner ON owner.id=roster.series_id
			LEFT JOIN episode_roster_imports accepted ON accepted.series_id=roster.series_id AND accepted.revision=roster.revision
			WHERE owner.id IS NULL
			OR accepted.series_id IS NULL OR roster.revision<>(SELECT max(history.revision) FROM episode_roster_imports history WHERE history.series_id=roster.series_id)
			OR roster.state IS DISTINCT FROM CASE accepted.action WHEN 'replace' THEN 'active' ELSE 'withdrawn' END
			OR (roster.source_key,roster.source_label,roster.source_revision,roster.parser_version,roster.payload_sha256,roster.last_edited_by)
			IS DISTINCT FROM (accepted.source_key,accepted.source_label,accepted.source_revision,accepted.parser_version,accepted.payload_sha256,accepted.actor_id))
		AND NOT EXISTS (SELECT 1 FROM episode_roster_imports history
			GROUP BY series_id,source_key,source_revision HAVING count(DISTINCT payload_sha256)>1)
		AND NOT EXISTS (SELECT 1 FROM (
			SELECT history.*,lag(action) OVER roster_history_window AS previous_action,
				lag(payload_sha256) OVER roster_history_window AS previous_hash
			FROM episode_roster_imports history WINDOW roster_history_window AS (PARTITION BY series_id ORDER BY revision)
		) history WHERE action='withdraw' AND (previous_action IS DISTINCT FROM 'replace' OR previous_hash IS DISTINCT FROM payload_sha256))
		AND NOT EXISTS (SELECT 1 FROM expected_episodes fact
			JOIN series_episode_rosters roster ON roster.series_id=fact.series_id
			JOIN episode_roster_imports history ON history.series_id=fact.series_id AND history.revision=fact.import_revision
			WHERE history.action<>'replace' OR fact.source_key<>history.source_key
			OR fact.active IS DISTINCT FROM (roster.state='active' AND fact.import_revision=roster.revision))`).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	// One bounded payload and at most one payload's facts are held at a time.
	// LIMIT includes a sentinel row, so excessive facts fail without a large
	// aggregate allocation. No nested query shares an open pgx result stream.
	rows, err := tx.Query(ctx, `SELECT history.series_id,history.action,history.source_key,history.source_label,
		history.source_revision,history.parser_version,history.payload,history.payload_sha256,
		(roster.state='active' AND roster.revision=history.revision),
		COALESCE((SELECT jsonb_agg(to_jsonb(fact) ORDER BY fact.id) FROM (
			SELECT id,source_key,entry_key,season_number,episode_number,name,
				COALESCE(to_char(premiere_date,'YYYY-MM-DD'),'') AS premiere_date
			FROM expected_episodes WHERE series_id=history.series_id AND import_revision=history.revision
			ORDER BY id LIMIT $1
		) fact),'[]'::jsonb)
		FROM episode_roster_imports history JOIN series_episode_rosters roster ON roster.series_id=history.series_id
		ORDER BY history.series_id,history.revision`, library.MaxEpisodeRosterEntries+1)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record selectedPhase3Import
		if err := rows.Scan(&record.seriesID, &record.action, &record.source.Key, &record.source.Label,
			&record.source.Revision, &record.parserVersion, &record.payload, &record.payloadHash,
			&record.current, &record.facts); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		validImport := validSelectedPhase3Import(record)
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validImport {
			return ErrSchema
		}
	}
	if err := classifyResourceStateError(ctx, rows.Err()); err != nil {
		return err
	}
	rows.Close()
	// The application keeps a stable row for every identity ever introduced.
	// Verify total historical coverage and its last accepted replacement after
	// every payload has passed the exact parser. PostgreSQL performs the set
	// operation without retaining an unbounded history map in the process.
	err = tx.QueryRow(ctx, `WITH declared AS (
		SELECT history.series_id,history.source_key,history.revision,entry->>'Key' AS entry_key
		FROM episode_roster_imports history
		CROSS JOIN LATERAL jsonb_array_elements(convert_from(history.payload,'UTF8')::jsonb->'Entries') entry
		WHERE history.action='replace'
	), latest AS (
		SELECT DISTINCT ON (series_id,source_key,entry_key) series_id,source_key,entry_key,revision
		FROM declared ORDER BY series_id,source_key,entry_key,revision DESC
	)
	SELECT NOT EXISTS(SELECT 1 FROM latest LEFT JOIN expected_episodes fact
		ON (fact.series_id,fact.source_key,fact.entry_key)=(latest.series_id,latest.source_key,latest.entry_key)
		WHERE fact.id IS NULL OR fact.import_revision<>latest.revision)`).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return nil
}

type selectedPhase3Import struct {
	seriesID, action, payloadHash string
	source                        library.EpisodeRosterSourceInput
	parserVersion                 int
	payload, facts                []byte
	current                       bool
}

type selectedPhase3Fact struct {
	ID            string `json:"id"`
	SourceKey     string `json:"source_key"`
	EntryKey      string `json:"entry_key"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	PremiereDate  string `json:"premiere_date"`
}

func validSelectedPhase3Import(record selectedPhase3Import) bool {
	edit, digest, err := library.ParseEpisodeRosterPayload(record.payload)
	if err != nil || record.parserVersion != library.EpisodeRosterParserVersion ||
		digest != record.payloadHash || edit.Source != record.source {
		return false
	}
	var facts []selectedPhase3Fact
	if json.Unmarshal(record.facts, &facts) != nil || facts == nil || len(facts) > library.MaxEpisodeRosterEntries ||
		(record.action != "replace" && record.action != "withdraw") ||
		(record.action == "withdraw" && (len(facts) != 0 || record.current)) ||
		(record.current && len(facts) != len(edit.Entries)) {
		return false
	}
	entries := make(map[string]library.EpisodeRosterEntryInput, len(edit.Entries))
	for _, entry := range edit.Entries {
		entries[entry.Key] = entry
	}
	for _, fact := range facts {
		entry, exists := entries[fact.EntryKey]
		if !exists || fact.SourceKey != edit.Source.Key ||
			fact.ID != library.ExpectedEpisodeID(record.seriesID, fact.SourceKey, fact.EntryKey) ||
			fact.SeasonNumber != entry.SeasonNumber || fact.EpisodeNumber != entry.EpisodeNumber ||
			fact.Name != entry.Name || fact.PremiereDate != entry.PremiereDate {
			return false
		}
		delete(entries, fact.EntryKey)
	}
	return true
}
