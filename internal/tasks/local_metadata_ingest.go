package tasks

import (
	"errors"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

func (s *Store) installScanIngestSchedules(tx library.OwnedTx) error {
	for _, policy := range []struct {
		key                  string
		event                systemevents.Event
		interval, maxRuntime time.Duration
	}{
		{library.TaskLocalMetadataIngestKey, systemevents.LocalMetadataIngestRequested, time.Hour, time.Hour},
		{library.TaskOnlineMetadataIngestKey, systemevents.OnlineMetadataIngestRequested, time.Hour, time.Hour},
		{library.TaskVideoSeekIngestKey, systemevents.VideoSeekIngestRequested, time.Hour, time.Hour},
		{library.TaskDolbyVisionVerificationKey, systemevents.DolbyVisionVerificationRequested, 15 * time.Minute, 20 * time.Minute},
		{library.TaskNFORecoveryKey, systemevents.NFORecoveryRequested, 15 * time.Minute, 20 * time.Minute},
	} {
		if err := s.installScanIngestSchedule(tx, policy.key, policy.event, policy.interval, policy.maxRuntime); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) installScanIngestSchedule(tx library.OwnedTx, key string, event systemevents.Event, interval, maxRuntime time.Duration) error {
	if entry, present := s.executors.lookup(key); !present || entry.AutomaticDisabled {
		return nil
	}
	var definition Definition
	err := decodeRow(tx.QueryRow(`SELECT to_jsonb(d) FROM task_definitions d
		WHERE key=$1 AND enabled AND revision=1
		AND NOT EXISTS(SELECT 1 FROM task_triggers t WHERE t.task_id=d.id) FOR UPDATE`, key), &definition)
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
	eventID, err := randomID()
	if err != nil {
		return err
	}
	startupID, err := randomID()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE task_definitions SET revision=revision+1,updated_at=$2 WHERE id=$1`, definition.ID, now); err != nil {
		return err
	}
	// Startup discovers pending work after interruption, even if the previous
	// process consumed the event before it stopped processing the backlog.
	runtimeTicks := int64(maxRuntime / scheduleNanosecondsPerTick)
	if _, err := tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,system_event,last_event_sequence,max_runtime_ticks,created_at,updated_at)
		VALUES($1,$2,$3,0,'system_event',$4,0,$5,$6,$6),($7,$2,$3,1,'startup',NULL,0,$5,$6,$6)`,
		eventID, definition.ID, definition.Revision+1, string(event), runtimeTicks, now, startupID); err != nil {
		return err
	}
	intervalID, err := randomID()
	if err != nil {
		return err
	}
	intervalTicks := int64(interval / scheduleNanosecondsPerTick)
	_, err = tx.Exec(`INSERT INTO task_triggers
		(id,task_id,schedule_revision,position,kind,interval_ticks,anchor_at,next_fire_at,max_runtime_ticks,created_at,updated_at)
		VALUES($1,$2,$3,2,'interval',$4,$5,$6,$7,$5,$5)`, intervalID, definition.ID, definition.Revision+1,
		intervalTicks, now, now.Add(interval), runtimeTicks)
	return err
}
