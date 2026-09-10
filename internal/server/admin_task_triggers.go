package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/tasks"
)

func adminTaskTicks(raw json.RawMessage, field string, invalid map[string]string) *int64 {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) != nil {
		invalid[field] = "Supply a decimal tick string or null."
		return nil
	}
	value, err := strconv.ParseInt(encoded, 10, 64)
	if err != nil || value < 0 || value > tasks.ScheduleMaxDurationTicks || strconv.FormatInt(value, 10) != encoded {
		invalid[field] = "Supply a canonical nonnegative tick string within the supported duration range."
		return nil
	}
	return &value
}

func decodeAdminTaskTriggers(w http.ResponseWriter, r *http.Request, withRevision bool) (tasks.ReplaceTriggersRequest, bool) {
	fields := []string{"ScheduleTimezone", "Triggers"}
	if withRevision {
		fields = append(fields, "Revision")
	}
	values, ok := adminTaskBody(w, r, fields, fields)
	if !ok {
		return tasks.ReplaceTriggersRequest{}, false
	}
	invalid := map[string]string{}
	var result tasks.ReplaceTriggersRequest
	if withRevision {
		result.Revision = managedUserRevision(values["Revision"], invalid)
	}
	managedUserValue(values["ScheduleTimezone"], "ScheduleTimezone", &result.ScheduleTimezone, invalid)
	if len(result.ScheduleTimezone) == 0 || len(result.ScheduleTimezone) > 255 || !utf8.ValidString(result.ScheduleTimezone) || strings.IndexFunc(result.ScheduleTimezone, unicode.IsControl) >= 0 {
		invalid["ScheduleTimezone"] = "Supply a named schedule timezone of at most 255 UTF-8 bytes."
	}
	var encoded []json.RawMessage
	if bytes.Equal(bytes.TrimSpace(values["Triggers"]), []byte("null")) || json.Unmarshal(values["Triggers"], &encoded) != nil || len(encoded) > tasks.MaxTriggers {
		invalid["Triggers"] = "Supply an array containing at most 32 triggers."
	} else {
		result.Triggers = make([]tasks.ScheduleRule, 0, len(encoded))
		for index, raw := range encoded {
			path := fmt.Sprintf("Triggers.%d", index)
			fields, fieldErrors := adminTaskObject(raw, []string{"Kind", "IntervalTicks", "TimeOfDayTicks", "DayOfWeek", "MaxRuntimeTicks"}, []string{"Kind"}, path)
			if fieldErrors != nil {
				for field, message := range fieldErrors {
					invalid[field] = message
				}
				continue
			}
			var kind string
			managedUserValue(fields["Kind"], path+".Kind", &kind, invalid)
			rule := tasks.ScheduleRule{Kind: tasks.ScheduleKind(kind),
				IntervalTicks:   adminTaskTicks(fields["IntervalTicks"], path+".IntervalTicks", invalid),
				TimeOfDayTicks:  adminTaskTicks(fields["TimeOfDayTicks"], path+".TimeOfDayTicks", invalid),
				MaxRuntimeTicks: adminTaskTicks(fields["MaxRuntimeTicks"], path+".MaxRuntimeTicks", invalid)}
			if day, exists := fields["DayOfWeek"]; exists && !bytes.Equal(bytes.TrimSpace(day), []byte("null")) {
				var value int
				if json.Unmarshal(day, &value) != nil || value < 0 || value > 6 {
					invalid[path+".DayOfWeek"] = "Supply an integer from Sunday 0 through Saturday 6, or null."
				} else {
					rule.DayOfWeek = &value
				}
			}
			result.Triggers = append(result.Triggers, rule)
		}
	}
	if len(invalid) != 0 {
		adminTaskInputError(w, r, invalid)
		return tasks.ReplaceTriggersRequest{}, false
	}
	return result, true
}

func (s *Server) replaceAdminTaskTriggers(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	request, ok := decodeAdminTaskTriggers(w, r, true)
	if !ok {
		return
	}
	request.TaskID = id
	if s.taskStore == nil || !s.taskManager.Available() {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.ReplaceTriggers(r.Context(), adminTaskActor(r), request)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	s.taskManager.Wake()
	jsonResponse(w, http.StatusOK, map[string]any{"Task": taskDefinitionDTO(definition)})
}

func (s *Server) previewAdminTaskTriggers(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	request, ok := decodeAdminTaskTriggers(w, r, false)
	if !ok {
		return
	}
	if s.taskStore == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	result, err := s.taskStore.PreviewTriggers(r.Context(), id, request.ScheduleTimezone, request.Triggers)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for _, preview := range result.Items {
		occurrences := make([]string, 0, len(preview.Occurrences))
		for _, occurrence := range preview.Occurrences {
			occurrences = append(occurrences, occurrence.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
		}
		items = append(items, map[string]any{"Index": preview.Index, "Occurrences": occurrences, "Event": preview.Event})
	}
	jsonResponse(w, http.StatusOK, map[string]any{"ServerTime": result.ServerTime.UTC(), "Items": items})
}
