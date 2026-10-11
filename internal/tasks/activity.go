package tasks

import (
	"strconv"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// A nil actor is used only by explicit scheduler and lifecycle operations.
// User-controlled request receipts and task display text never enter activity.
func taskActivityActor(actor *Actor) (activity.Actor, activity.Source) {
	if actor == nil {
		return activity.Actor{Kind: activity.ActorSystem}, activity.SourceSystem
	}
	source := activity.SourceNative
	if actor.Audience == identity.AdministratorEmby {
		source = activity.SourceEmby
	}
	if actor.Principal.IsApplicationKey() {
		return activity.Actor{Kind: activity.ActorApplicationKey,
			ID: strconv.FormatInt(actor.Principal.ApplicationKeyID, 10), CredentialID: actor.Principal.SessionID}, source
	}
	return activity.Actor{Kind: activity.ActorUser,
		ID: actor.Principal.User.ID, CredentialID: actor.Principal.SessionID}, source
}

func recordTaskActivity(tx library.OwnedTx, actor *Actor, action activity.Action, runID string,
	revision, count int64, state activity.State) error {
	principal, source := taskActivityActor(actor)
	event := activity.Event{Action: action, Source: source,
		Actor: principal, Resource: activity.Resource{Kind: activity.ResourceTaskRun, ID: runID},
		Revision: revision, Count: count, State: state}
	switch state {
	case activity.StateFailed:
		event.Severity = activity.SeverityError
	case activity.StateInterrupted:
		event.Severity = activity.SeverityWarning
	}
	return activity.RecordOwned(tx, event)
}

func recordTaskScheduleActivity(tx library.OwnedTx, actor Actor, definition Definition, changed []activity.Field) error {
	principal, source := taskActivityActor(&actor)
	return activity.RecordOwned(tx, activity.Event{Action: activity.ActionTaskScheduleUpdated,
		Source: source, Actor: principal, Resource: activity.Resource{Kind: activity.ResourceTask, ID: definition.ID},
		Revision: definition.Revision, Count: int64(len(definition.Triggers)), ChangedFields: changed})
}

// Every successful replacement creates a new rule revision and may reset
// anchors or suspended calculations, even when the input configuration matches.
func changedTaskScheduleFields(definition Definition, timezone string) []activity.Field {
	changed := []activity.Field{activity.FieldTriggers}
	if definition.ScheduleTimezone != timezone {
		changed = append(changed, activity.FieldScheduleTimezone)
	}
	return changed
}

// The caller already holds the parent, child, and scan locks. Return only the
// first flag transitions, in stable order, for the later cancellation audit.
func cancelTaskScans(tx library.OwnedTx, runID string) ([]string, error) {
	rows, err := tx.Query(`WITH cancelled AS (
		UPDATE scan_jobs j SET cancel_requested = true
		FROM task_run_children c WHERE c.run_id = $1 AND c.scan_job_id = j.id
			AND j.task_child_id = c.id AND j.status IN ('Queued','Running') AND NOT j.cancel_requested
		RETURNING j.id)
		SELECT id FROM cancelled ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	return collectIDs(rows)
}

func recordTaskScanCancellations(tx library.OwnedTx, scanIDs []string) error {
	for _, id := range scanIDs {
		if err := activity.RecordOwned(tx, activity.Event{Action: activity.ActionScanCancelRequested,
			Source: activity.SourceSystem, Actor: activity.Actor{Kind: activity.ActorSystem},
			Resource: activity.Resource{Kind: activity.ResourceScan, ID: id}}); err != nil {
			return err
		}
	}
	return nil
}
