package tasks

import (
	"errors"
	"fmt"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// Definition revision and retained trigger history distinguish an untouched
// definition from a schedule the administrator replaced or deliberately cleared.
func (s *Store) installIntroSchedule(tx library.OwnedTx) error {
	if _, present := s.executors.lookup(library.TaskIntroAnalysisKey); !present {
		return nil
	}
	var definition Definition
	err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d
		WHERE key=$1 AND enabled AND revision=1
		AND NOT EXISTS(SELECT 1 FROM task_triggers t WHERE t.task_id=d.id) FOR UPDATE`, library.TaskIntroAnalysisKey), &definition)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var now time.Time
	if err := tx.QueryRow(`SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	now = now.UTC()
	intervalID, err := randomID()
	if err != nil {
		return err
	}
	eventID, err := randomID()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE task_definitions SET revision=revision+1,updated_at=$2 WHERE id=$1`, definition.ID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,interval_ticks,anchor_at,next_fire_at,created_at,updated_at)
		VALUES($1,$2,$3,0,'interval',$4,$5,$6,$5,$5)`, intervalID, definition.ID, definition.Revision+1,
		24*60*60*ScheduleTicksPerSecond, now, now.Add(24*time.Hour)); err != nil {
		return fmt.Errorf("install default intro interval: %w", err)
	}
	// Start at zero so a committed enable/scan request preceding installation is
	// still delivered. Later administrator replacements use the current cursor.
	_, err = tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,system_event,last_event_sequence,created_at,updated_at)
		VALUES($1,$2,$3,1,'system_event',$4,0,$5,$5)`, eventID, definition.ID, definition.Revision+1,
		string(systemevents.IntroAnalysisRequested), now)
	if err != nil {
		return fmt.Errorf("install default intro event: %w", err)
	}
	return nil
}
