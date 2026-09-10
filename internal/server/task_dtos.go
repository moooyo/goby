package server

import (
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/tasks"
)

func taskTickString(value *int64) any {
	if value == nil {
		return nil
	}
	return strconv.FormatInt(*value, 10)
}

func taskOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func taskTriggerDTO(trigger tasks.Trigger) map[string]any {
	return map[string]any{"Id": trigger.ID, "Kind": trigger.Kind,
		"IntervalTicks": taskTickString(trigger.IntervalTicks), "TimeOfDayTicks": taskTickString(trigger.TimeOfDayTicks),
		"MaxRuntimeTicks": taskTickString(trigger.MaxRuntimeTicks), "DayOfWeek": trigger.DayOfWeek, "NextFireAt": taskOptionalTime(trigger.NextFireAt),
		"CalculationError": trigger.CalculationError}
}

func taskRunDTO(run tasks.Run) map[string]any {
	return map[string]any{"Id": run.ID, "TaskId": run.TaskID, "TaskName": run.TaskName, "State": run.State,
		"Source": run.Source, "RequestId": run.RequestID, "ScheduledFor": taskOptionalTime(run.ScheduledFor),
		"MaxRuntimeTicks": taskTickString(run.MaxRuntimeTicks), "CreatedAt": run.CreatedAt.UTC(),
		"StartedAt": taskOptionalTime(run.StartedAt), "DeadlineAt": taskOptionalTime(run.DeadlineAt), "StopRequestedAt": taskOptionalTime(run.StopRequestedAt),
		"StopReason": run.StopReason, "FinishedAt": taskOptionalTime(run.FinishedAt), "ErrorCode": run.ErrorCode, "ErrorMessage": run.ErrorMessage,
		"TotalChildren": run.TotalChildren, "TerminalChildren": run.TerminalChildren, "CompletedChildren": run.CompletedChildren,
		"FailedChildren": run.FailedChildren, "CancelledChildren": run.CancelledChildren, "InterruptedChildren": run.InterruptedChildren,
		"UnavailableChildren": run.UnavailableChildren, "Scanned": run.Scanned, "Added": run.Added, "Updated": run.Updated}
}

func taskChildDTO(child tasks.Child) map[string]any {
	return map[string]any{"Id": child.ID, "RunId": child.RunID, "LibraryId": child.LibraryID, "LibraryName": child.LibraryName,
		"Ordinal": child.Ordinal, "State": child.State, "ScanJobId": child.ScanJobID, "Scanned": child.Scanned,
		"Added": child.Added, "Updated": child.Updated, "ErrorCode": child.ErrorCode, "ErrorMessage": child.ErrorMessage,
		"CreatedAt": child.CreatedAt.UTC(), "StartedAt": taskOptionalTime(child.StartedAt), "FinishedAt": taskOptionalTime(child.FinishedAt)}
}

func taskDefinitionDTO(definition tasks.Definition) map[string]any {
	triggers := make([]map[string]any, 0, len(definition.Triggers))
	var next *time.Time
	for _, trigger := range definition.Triggers {
		triggers = append(triggers, taskTriggerDTO(trigger))
		if definition.Enabled && trigger.CalculationError == "" && trigger.NextFireAt != nil && (next == nil || trigger.NextFireAt.Before(*next)) {
			value := trigger.NextFireAt.UTC()
			next = &value
		}
	}
	var current, last any
	if definition.CurrentRun != nil {
		current = taskRunDTO(*definition.CurrentRun)
	}
	if definition.LastRun != nil {
		last = taskRunDTO(*definition.LastRun)
	}
	return map[string]any{"Id": definition.ID, "Key": definition.Key, "Name": definition.Name, "Description": definition.Description,
		"Category": definition.Category, "IsHidden": definition.IsHidden, "Enabled": definition.Enabled,
		"Revision": strconv.FormatInt(definition.Revision, 10), "ScheduleTimezone": definition.ScheduleTimezone,
		"Triggers": triggers, "CurrentRun": current, "LastRun": last, "NextRunAt": taskOptionalTime(next)}
}

func taskRunPageDTO(page tasks.RunPage) map[string]any {
	items := make([]map[string]any, 0, len(page.Items))
	for _, run := range page.Items {
		items = append(items, taskRunDTO(run))
	}
	return map[string]any{"Items": items, "TotalRecordCount": page.TotalRecordCount, "StartIndex": page.StartIndex, "Limit": page.Limit}
}

func taskChildPageDTO(page tasks.ChildPage) map[string]any {
	items := make([]map[string]any, 0, len(page.Items))
	for _, child := range page.Items {
		items = append(items, taskChildDTO(child))
	}
	return map[string]any{"Items": items, "TotalRecordCount": page.TotalRecordCount, "StartIndex": page.StartIndex, "Limit": page.Limit}
}
