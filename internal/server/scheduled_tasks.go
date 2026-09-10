package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

var errUnsupportedTaskTrigger = errors.New("unsupported compatibility task trigger")

var embyTaskWeekdays = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func (s *Server) registerScheduledTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/ScheduledTasks", s.requireEmby(s.embyScheduledTasks))
	mux.HandleFunc("GET /emby/ScheduledTasks/{id}", s.requireEmby(s.embyScheduledTask))
	// A single-segment trigger wildcard conflicts with Running/{id} at
	// Running/Triggers. The bounded dispatcher is strictly less specific than
	// the explicit execution routes, so the complete ServeMux stays unambiguous.
	mux.HandleFunc("POST /emby/ScheduledTasks/{path...}", s.dispatchScheduledTaskPost)
	mux.HandleFunc("POST /emby/ScheduledTasks/Running/{id}", s.requireEmby(s.startEmbyScheduledTask))
	mux.HandleFunc("DELETE /emby/ScheduledTasks/Running/{id}", s.requireEmby(s.stopEmbyScheduledTask))
	mux.HandleFunc("POST /emby/ScheduledTasks/Running/{id}/Delete", s.requireEmby(s.stopEmbyScheduledTask))
}

func (s *Server) dispatchScheduledTaskPost(w http.ResponseWriter, r *http.Request) {
	const prefix = "/emby/ScheduledTasks/"
	escaped := r.URL.EscapedPath()
	if !strings.HasPrefix(escaped, prefix) {
		embyTextError(w, r, http.StatusNotFound, "Task not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(escaped, prefix), "/")
	if len(parts) != 2 {
		embyTextError(w, r, http.StatusNotFound, "Task not found")
		return
	}
	id, idErr := url.PathUnescape(parts[0])
	action, actionErr := url.PathUnescape(parts[1])
	if idErr != nil || actionErr != nil || !strings.EqualFold(action, "Triggers") || id == "" || len(id) > 128 ||
		!utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.ContainsAny(id, `/\`) || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		embyTextError(w, r, http.StatusNotFound, "Task not found")
		return
	}
	r.SetPathValue("id", id)
	s.requireEmby(s.replaceEmbyScheduledTaskTriggers)(w, r)
}

func embyTaskActor(r *http.Request) tasks.Actor {
	return tasks.Actor{Principal: r.Context().Value(principalKey).(identity.Principal), Audience: identity.AdministratorEmby}
}

func (s *Server) scheduledTaskManager(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	return s.keyManager(w, r)
}

func parseEmbyTaskQuery(r *http.Request, list bool) (tasks.ListOptions, error) {
	options := tasks.ListOptions{}
	if len(r.URL.RawQuery) > 4096 || r.URL.ForceQuery && r.URL.RawQuery == "" {
		return options, tasks.ErrInvalidInput
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return options, tasks.ErrInvalidInput
	}
	seen := make(map[string]bool)
	for name, entries := range values {
		field := strings.ToLower(name)
		if seen[field] || len(entries) != 1 || !utf8.ValidString(entries[0]) {
			return options, tasks.ErrInvalidInput
		}
		seen[field] = true
		if field == "api_key" {
			// Authentication query transport is not a task-management parameter.
			continue
		}
		if !list || field != "ishidden" && field != "isenabled" || entries[0] != "true" && entries[0] != "false" {
			return options, tasks.ErrInvalidInput
		}
		value := entries[0] == "true"
		if field == "ishidden" {
			options.IsHidden = &value
		} else {
			options.IsEnabled = &value
		}
	}
	return options, nil
}

func embyTaskID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if id == "" || len(id) > 128 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return "", tasks.ErrInvalidInput
	}
	return id, nil
}

func embyTaskTriggerDTO(trigger tasks.Trigger) map[string]any {
	result := make(map[string]any)
	switch tasks.ScheduleKind(trigger.Kind) {
	case tasks.ScheduleInterval:
		result["Type"] = "IntervalTrigger"
		if trigger.IntervalTicks != nil {
			result["IntervalTicks"] = *trigger.IntervalTicks
		}
	case tasks.ScheduleDaily:
		result["Type"] = "DailyTrigger"
	case tasks.ScheduleWeekly:
		result["Type"] = "WeeklyTrigger"
		if trigger.DayOfWeek != nil && *trigger.DayOfWeek >= 0 && *trigger.DayOfWeek < len(embyTaskWeekdays) {
			result["DayOfWeek"] = embyTaskWeekdays[*trigger.DayOfWeek]
		}
	case tasks.ScheduleStartup:
		result["Type"] = "StartupTrigger"
	}
	if trigger.TimeOfDayTicks != nil {
		result["TimeOfDayTicks"] = *trigger.TimeOfDayTicks
	}
	if trigger.MaxRuntimeTicks != nil {
		result["MaxRuntimeTicks"] = *trigger.MaxRuntimeTicks
	}
	return result
}

func embyTaskResultDTO(definition tasks.Definition, run tasks.Run) map[string]any {
	started := run.CreatedAt.UTC()
	if run.StartedAt != nil {
		started = run.StartedAt.UTC()
	}
	status := "Failed"
	switch run.State {
	case tasks.RunCompleted:
		status = "Completed"
	case tasks.RunCancelled:
		status = "Cancelled"
	case tasks.RunInterrupted:
		status = "Aborted"
	}
	result := map[string]any{"Id": definition.ID, "Name": definition.Name, "Key": definition.EmbyKey,
		"StartTimeUtc": started, "Status": status}
	if run.FinishedAt != nil {
		result["EndTimeUtc"] = run.FinishedAt.UTC()
	}
	if run.ErrorMessage != "" && (run.State == tasks.RunFailed || run.State == tasks.RunInterrupted) {
		result["ErrorMessage"] = run.ErrorMessage
	}
	return result
}

func embyTaskDTO(definition tasks.Definition) map[string]any {
	triggers := make([]map[string]any, 0, len(definition.Triggers))
	for _, trigger := range definition.Triggers {
		triggers = append(triggers, embyTaskTriggerDTO(trigger))
	}
	result := map[string]any{"Id": definition.ID, "Name": definition.Name, "Key": definition.EmbyKey,
		"Description": definition.Description, "Category": definition.Category, "IsHidden": definition.IsHidden,
		"State": "Idle", "Triggers": triggers}
	if run := definition.CurrentRun; run != nil && run.State.Active() {
		result["State"] = "Running"
		if run.State == tasks.RunStopping {
			result["State"] = "Cancelling"
		}
		progress := float64(0)
		if run.TotalChildren > 0 {
			progress = float64(run.TerminalChildren) / float64(run.TotalChildren) * 100
			progress = min(100, max(0, progress))
		}
		// This is completed-library progress, not an estimate of unscanned files.
		result["CurrentProgressPercentage"] = progress
	}
	if run := definition.LastRun; run != nil && run.FinishedAt != nil && !run.State.Active() {
		result["LastExecutionResult"] = embyTaskResultDTO(definition, *run)
	}
	return result
}

func executableEmbyTask(definition tasks.Definition) bool {
	return definition.Key == tasks.LibraryScanKey && definition.EmbyKey == tasks.LibraryScanEmbyKey
}

func (s *Server) scheduledTaskDefinition(ctx context.Context, id string) (tasks.Definition, error) {
	if s.taskStore == nil {
		return tasks.Definition{}, tasks.ErrUnavailable
	}
	definition, err := s.taskStore.Get(ctx, id)
	if err != nil {
		return tasks.Definition{}, err
	}
	if !executableEmbyTask(definition) {
		return tasks.Definition{}, tasks.ErrNotFound
	}
	return definition, nil
}

func (s *Server) embyScheduledTasks(w http.ResponseWriter, r *http.Request) {
	if !s.scheduledTaskManager(w, r) {
		return
	}
	options, err := parseEmbyTaskQuery(r, true)
	if err != nil || s.taskStore == nil {
		if err == nil {
			err = tasks.ErrUnavailable
		}
		s.scheduledTaskError(w, r, err)
		return
	}
	definitions, err := s.taskStore.List(r.Context(), options)
	if err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	result := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		if executableEmbyTask(definition) {
			result = append(result, embyTaskDTO(definition))
		}
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) embyScheduledTask(w http.ResponseWriter, r *http.Request) {
	if !s.scheduledTaskManager(w, r) {
		return
	}
	definition, ok := s.loadScheduledTask(w, r)
	if ok {
		jsonResponse(w, http.StatusOK, embyTaskDTO(definition))
	}
}

func (s *Server) loadScheduledTask(w http.ResponseWriter, r *http.Request) (tasks.Definition, bool) {
	id, err := embyTaskID(r)
	if err == nil {
		_, err = parseEmbyTaskQuery(r, false)
	}
	if err != nil {
		s.scheduledTaskError(w, r, err)
		return tasks.Definition{}, false
	}
	definition, err := s.scheduledTaskDefinition(r.Context(), id)
	if err != nil {
		s.scheduledTaskError(w, r, err)
		return tasks.Definition{}, false
	}
	return definition, true
}

func embyTaskTicks(raw json.RawMessage) (*int64, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, tasks.ErrInvalidInput
	}
	var ticks int64
	if err := json.Unmarshal(raw, &ticks); err != nil {
		return nil, tasks.ErrInvalidInput
	}
	if _, err := tasks.ScheduleTicksDuration(ticks); err != nil {
		return nil, err
	}
	return &ticks, nil
}

func embyTaskWeekday(raw json.RawMessage) (*int, error) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		for day, candidate := range embyTaskWeekdays {
			if name == candidate {
				return &day, nil
			}
		}
		return nil, tasks.ErrInvalidInput
	}
	// Numeric weekdays are a Goby input tolerance based on the SDK enum; the
	// fresh reference matrix did not exercise weekly rules or numeric weekdays.
	var day int
	if err := json.Unmarshal(raw, &day); err != nil || day < 0 || day > 6 {
		return nil, tasks.ErrInvalidInput
	}
	return &day, nil
}

func parseEmbyTaskTriggers(data []byte, timezone string) ([]tasks.ScheduleRule, error) {
	if len(data) > maxAdminTaskBodyBytes || !utf8.Valid(data) {
		return nil, tasks.ErrInvalidInput
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		return nil, tasks.ErrInvalidInput
	}
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil || len(values) > tasks.MaxTriggers {
		return nil, tasks.ErrInvalidInput
	}
	rules := make([]tasks.ScheduleRule, 0, len(values))
	for _, raw := range values {
		fields, err := remoteCommandObject(raw, true)
		if err != nil {
			return nil, tasks.ErrInvalidInput
		}
		var kind string
		if json.Unmarshal(fields["type"], &kind) != nil {
			return nil, tasks.ErrInvalidInput
		}
		rule := tasks.ScheduleRule{}
		switch kind {
		case "IntervalTrigger":
			rule.Kind = tasks.ScheduleInterval
		case "DailyTrigger":
			rule.Kind = tasks.ScheduleDaily
		case "WeeklyTrigger":
			rule.Kind = tasks.ScheduleWeekly
		case "StartupTrigger":
			rule.Kind = tasks.ScheduleStartup
		default:
			// No Linux event executor exists for SystemEventTrigger. Accepting
			// it would persist a schedule that cannot execute as requested.
			return nil, errUnsupportedTaskTrigger
		}
		for name, value := range fields {
			switch name {
			case "type":
			case "intervalticks":
				rule.IntervalTicks, err = embyTaskTicks(value)
			case "timeofdayticks":
				rule.TimeOfDayTicks, err = embyTaskTicks(value)
			case "maxruntimeticks":
				rule.MaxRuntimeTicks, err = embyTaskTicks(value)
			case "dayofweek":
				rule.DayOfWeek, err = embyTaskWeekday(value)
			default:
				err = tasks.ErrInvalidInput
			}
			if err != nil {
				return nil, err
			}
		}
		validation := rule
		if rule.Kind == tasks.ScheduleInterval {
			anchor := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			validation.AnchorAt = &anchor
		}
		if rule.Kind == tasks.ScheduleDaily || rule.Kind == tasks.ScheduleWeekly {
			validation.Timezone = timezone
		}
		if err := tasks.ValidateSchedule(validation); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (s *Server) replaceEmbyScheduledTaskTriggers(w http.ResponseWriter, r *http.Request) {
	if !s.scheduledTaskManager(w, r) {
		return
	}
	definition, ok := s.loadScheduledTask(w, r)
	if !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminTaskBodyBytes)
	data, err := io.ReadAll(r.Body)
	zone := definition.ScheduleTimezone
	if zone == "" {
		zone = "UTC"
	}
	var rules []tasks.ScheduleRule
	if err == nil {
		rules, err = parseEmbyTaskTriggers(data, zone)
	} else {
		err = tasks.ErrInvalidInput
	}
	if err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	if !s.taskManager.Available() {
		s.scheduledTaskError(w, r, tasks.ErrUnavailable)
		return
	}
	_, err = s.taskStore.ReplaceTriggers(r.Context(), embyTaskActor(r), tasks.ReplaceTriggersRequest{
		TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: zone, Triggers: rules})
	if err != nil {
		// The compatibility request has no revision field. A concurrent native
		// edit must produce a conflict, never an automatic overwrite retry.
		s.scheduledTaskError(w, r, err)
		return
	}
	s.taskManager.Wake()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startEmbyScheduledTask(w http.ResponseWriter, r *http.Request) {
	if !s.scheduledTaskManager(w, r) {
		return
	}
	definition, ok := s.loadScheduledTask(w, r)
	if !ok {
		return
	}
	if _, err := s.taskManager.Start(r.Context(), embyTaskActor(r), tasks.StartRequest{TaskID: definition.ID}); err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopEmbyScheduledTask(w http.ResponseWriter, r *http.Request) {
	if !s.scheduledTaskManager(w, r) {
		return
	}
	definition, ok := s.loadScheduledTask(w, r)
	if !ok {
		return
	}
	if _, err := s.taskManager.StopByDefinition(r.Context(), embyTaskActor(r), definition.ID); err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scheduledTaskError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *tasks.ValidationError
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		embyTextError(w, r, http.StatusNotFound, "Task not found")
	case errors.Is(err, tasks.ErrNotRunning):
		embyTextError(w, r, http.StatusInternalServerError, "Cannot cancel a Task unless it is in the Running state.")
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		actor, _ := r.Context().Value(principalKey).(identity.Principal)
		embyTextError(w, r, http.StatusForbidden, fmt.Sprintf("User %s does not have access to ManageServer feature.", actor.User.Name))
	case errors.Is(err, errUnsupportedTaskTrigger):
		apiError(w, r, http.StatusBadRequest, "unsupported_trigger", "The requested trigger type has no supported task executor.")
	case errors.As(err, &validation), errors.Is(err, tasks.ErrInvalidInput), errors.Is(err, tasks.ErrInvalidSchedule):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the task filters and trigger fields.")
	case errors.Is(err, tasks.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "The task schedule changed. Refresh it before trying again.")
	case errors.Is(err, tasks.ErrDisabled):
		apiError(w, r, http.StatusConflict, "task_disabled", "This task is disabled.")
	case errors.Is(err, tasks.ErrUnavailable), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "task_unavailable", "Task execution is currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		s.log.Error("compatibility task operation failed", "request_id", r.Context().Value(requestIDKey))
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The task operation could not be completed.")
	}
}
