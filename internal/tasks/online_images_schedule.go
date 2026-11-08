package tasks

import (
	"errors"
	"fmt"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

// Installation never opts a library into network access and never replaces an
// administrator's schedule. Eligible children require actual missing artwork.
func (s *Store) installOnlineImageSchedule(tx library.OwnedTx) error {
	if _, present := s.executors.lookup(library.TaskOnlineImagesKey); !present {
		return nil
	}
	var definition Definition
	err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d
		WHERE key=$1 AND enabled AND revision=1
		AND NOT EXISTS(SELECT 1 FROM task_triggers t WHERE t.task_id=d.id) FOR UPDATE`, library.TaskOnlineImagesKey), &definition)
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
	const maxRuntime = 30 * 60 * ScheduleTicksPerSecond
	if _, err := tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,interval_ticks,anchor_at,next_fire_at,max_runtime_ticks,created_at,updated_at)
		VALUES($1,$2,$3,0,'interval',$4,$5,$6,$7,$5,$5)`, intervalID, definition.ID, definition.Revision+1,
		24*60*60*ScheduleTicksPerSecond, now, now.Add(24*time.Hour), maxRuntime); err != nil {
		return fmt.Errorf("install default image-download interval: %w", err)
	}
	// Start at zero to deliver a committed opt-in/scan before task installation.
	_, err = tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,system_event,last_event_sequence,max_runtime_ticks,created_at,updated_at)
		VALUES($1,$2,$3,1,'system_event',$4,0,$6,$5,$5)`, eventID, definition.ID, definition.Revision+1,
		string(systemevents.OnlineImagesRequested), now, maxRuntime)
	return err
}
