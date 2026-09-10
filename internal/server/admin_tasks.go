package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

const maxAdminTaskBodyBytes = 32 * 1024

func (s *Server) registerAdminTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/tasks", s.requireAdmin(s.adminTasks))
	mux.HandleFunc("GET /admin/v1/tasks/{id}", s.requireAdmin(s.adminTask))
	mux.HandleFunc("POST /admin/v1/tasks/{id}/runs", s.requireAdmin(s.startAdminTask))
	mux.HandleFunc("GET /admin/v1/tasks/{id}/runs", s.requireAdmin(s.adminTaskRuns))
	mux.HandleFunc("GET /admin/v1/task-runs/{id}", s.requireAdmin(s.adminTaskRun))
	mux.HandleFunc("POST /admin/v1/task-runs/{id}/cancel", s.requireAdmin(s.cancelAdminTaskRun))
	mux.HandleFunc("PUT /admin/v1/tasks/{id}/triggers", s.requireAdmin(s.replaceAdminTaskTriggers))
	mux.HandleFunc("POST /admin/v1/tasks/{id}/triggers/preview", s.requireAdmin(s.previewAdminTaskTriggers))
}

func adminTaskInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the task request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func adminTaskNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		adminTaskInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

func adminTaskID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	valid := len(id) == 32
	if valid {
		decoded, err := hex.DecodeString(id)
		valid = err == nil && hex.EncodeToString(decoded) == id
	}
	if !valid {
		adminTaskInputError(w, r, map[string]string{"Id": "Supply a lowercase 32-character hexadecimal task identifier."})
		return "", false
	}
	return id, true
}

func adminTaskPage(w http.ResponseWriter, r *http.Request) (tasks.Page, bool) {
	page := tasks.Page{Limit: tasks.DefaultPageLimit}
	if len(r.URL.RawQuery) > 4096 {
		adminTaskInputError(w, r, map[string]string{"Query": "Supply a page query no longer than 4 KiB."})
		return page, false
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		adminTaskInputError(w, r, map[string]string{"Query": "Supply a valid bounded page query."})
		return page, false
	}
	for name, entries := range values {
		if (name != "StartIndex" && name != "Limit") || len(entries) != 1 {
			adminTaskInputError(w, r, map[string]string{"Query": "Supply each supported pagination parameter at most once."})
			return page, false
		}
		value, err := strconv.ParseInt(entries[0], 10, 32)
		if err != nil || value < 0 || strconv.FormatInt(value, 10) != entries[0] || name == "Limit" && (value < 1 || value > tasks.MaxPageLimit) {
			adminTaskInputError(w, r, map[string]string{name: "Use a nonnegative StartIndex or a Limit between 1 and 200."})
			return page, false
		}
		if name == "StartIndex" {
			page.StartIndex = int(value)
		} else {
			page.Limit = int(value)
		}
	}
	return page, true
}

func adminTaskObject(data []byte, allowed, required []string, path string) (map[string]json.RawMessage, map[string]string) {
	fieldPath := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	objectPath := path
	if objectPath == "" {
		objectPath = "Body"
	}
	invalid := func(message string) (map[string]json.RawMessage, map[string]string) {
		return nil, map[string]string{objectPath: message}
	}
	fields := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		fields[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return invalid("Supply one JSON object.")
	}
	values := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		field, err := decoder.Token()
		name, isName := field.(string)
		if err != nil || !isName || !fields[name] {
			return invalid("The object contains an unsupported field.")
		}
		if _, exists := values[name]; exists {
			return nil, map[string]string{fieldPath(name): "Supply each field at most once."}
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return invalid("Supply a valid JSON object.")
		}
		values[name] = raw
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return invalid("Supply a valid JSON object.")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid("Supply exactly one JSON object.")
	}
	missing := map[string]string{}
	for _, field := range required {
		if _, exists := values[field]; !exists {
			missing[fieldPath(field)] = "This field is required."
		}
	}
	if len(missing) > 0 {
		return nil, missing
	}
	return values, nil
}

func adminTaskBody(w http.ResponseWriter, r *http.Request, allowed, required []string) (map[string]json.RawMessage, bool) {
	if !adminTaskNoQuery(w, r) {
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminTaskBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		adminTaskInputError(w, r, map[string]string{"Body": "Supply a UTF-8 JSON object no larger than 32 KiB."})
		return nil, false
	}
	values, invalid := adminTaskObject(data, allowed, required, "")
	if invalid != nil {
		adminTaskInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func adminTaskActor(r *http.Request) tasks.Actor {
	return tasks.Actor{Principal: r.Context().Value(principalKey).(identity.Principal), Audience: identity.AdministratorNative}
}

func (s *Server) adminTasks(w http.ResponseWriter, r *http.Request) {
	if !adminTaskNoQuery(w, r) {
		return
	}
	if s.taskStore == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definitions, err := s.taskStore.List(r.Context(), tasks.ListOptions{})
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, taskDefinitionDTO(definition))
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) adminTask(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok || !adminTaskNoQuery(w, r) {
		return
	}
	if s.taskStore == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.Get(r.Context(), id)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Task": taskDefinitionDTO(definition)})
}

func (s *Server) startAdminTask(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	values, ok := adminTaskBody(w, r, []string{"RequestId"}, nil)
	if !ok {
		return
	}
	var requestID string
	if raw, present := values["RequestId"]; present {
		if string(bytes.TrimSpace(raw)) == "null" || json.Unmarshal(raw, &requestID) != nil || len(requestID) > tasks.MaxRequestIDBytes ||
			!utf8.ValidString(requestID) || strings.TrimSpace(requestID) != requestID || strings.IndexFunc(requestID, unicode.IsControl) >= 0 {
			adminTaskInputError(w, r, map[string]string{"RequestId": "Supply a request identifier of at most 128 UTF-8 bytes without controls or surrounding whitespace."})
			return
		}
	}
	result, err := s.taskManager.Start(r.Context(), adminTaskActor(r), tasks.StartRequest{TaskID: id, RequestID: requestID})
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"Run": taskRunDTO(result.Run), "Admitted": result.Admitted})
}

func (s *Server) adminTaskRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	page, ok := adminTaskPage(w, r)
	if !ok {
		return
	}
	if s.taskStore == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	result, err := s.taskStore.ListRuns(r.Context(), id, page)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, taskRunPageDTO(result))
}

func (s *Server) adminTaskRun(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	page, ok := adminTaskPage(w, r)
	if !ok {
		return
	}
	if s.taskStore == nil {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	run, err := s.taskStore.GetRun(r.Context(), id)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	children, err := s.taskStore.ListChildren(r.Context(), id, page)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Run": taskRunDTO(run), "Children": taskChildPageDTO(children)})
}

func (s *Server) cancelAdminTaskRun(w http.ResponseWriter, r *http.Request) {
	id, ok := adminTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := adminTaskBody(w, r, nil, nil); !ok {
		return
	}
	run, err := s.taskManager.Stop(r.Context(), adminTaskActor(r), id)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"Run": taskRunDTO(run)})
}

func (s *Server) taskError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *tasks.ValidationError
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.As(err, &validation):
		adminTaskInputError(w, r, validation.Fields)
	case errors.Is(err, tasks.ErrInvalidInput), errors.Is(err, tasks.ErrInvalidSchedule), errors.Is(err, identity.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the task identifiers and schedule fields.")
	case errors.Is(err, tasks.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The requested task resource was not found.")
	case errors.Is(err, tasks.ErrRequestConflict):
		apiError(w, r, http.StatusConflict, "request_conflict", "The request identifier belongs to a different task request.")
	case errors.Is(err, tasks.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "The task changed. Refresh it before trying again.")
	case errors.Is(err, tasks.ErrDisabled):
		apiError(w, r, http.StatusConflict, "task_disabled", "This task is disabled.")
	case errors.Is(err, tasks.ErrUnavailable), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "task_unavailable", "Task execution is currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		s.log.Error("task operation failed", "request_id", r.Context().Value(requestIDKey))
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The task operation could not be completed.")
	}
}
