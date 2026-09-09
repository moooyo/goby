package server

import (
	"bytes"
	"context"
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

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const (
	maxRemoteCommandBytes     = 64 * 1024
	maxRemoteCommandNameBytes = 128
	maxRemoteCommandArguments = 64
	maxRemoteArgumentBytes    = 2048
	maxRemotePlayItems        = 128
)

var (
	errRemoteCommandInput = errors.New("invalid remote command")
	errRemoteCommandLimit = errors.New("remote command exceeds limits")
	errRemoteCommandMedia = errors.New("remote command requires JSON")
)

func (s *Server) registerRemoteCommandRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /emby/Sessions/{Id}/Playing", s.requireEmby(s.remotePlayCommand))
	mux.HandleFunc("POST /emby/Sessions/{Id}/Playing/{Command}", s.requireEmby(s.remotePlaystateCommand))
	mux.HandleFunc("POST /emby/Sessions/{Id}/Command", s.requireEmby(s.remoteGeneralCommand(false)))
	mux.HandleFunc("POST /emby/Sessions/{Id}/Command/{Command}", s.requireEmby(s.remoteGeneralCommand(true)))
}

func readRemoteCommandBytes(r *http.Request) ([]byte, error) {
	if r.ContentLength > maxRemoteCommandBytes {
		return nil, errRemoteCommandLimit
	}
	if r.Body == nil {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRemoteCommandBytes+1))
	if err != nil {
		return nil, errRemoteCommandInput
	}
	if len(data) > maxRemoteCommandBytes {
		return nil, errRemoteCommandLimit
	}
	return data, nil
}

// Parse top-level fields separately so duplicate aliases cannot change the
// result depending on JSON decoder order. Unknown fields stay bounded and are
// discarded; only explicitly copied command fields reach the event envelope.
func remoteCommandObject(data []byte, foldNames bool) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errRemoteCommandInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errRemoteCommandInput
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, errRemoteCommandInput
		}
		name, ok := token.(string)
		if !ok || !validRemoteCommandText(name, maxRemoteCommandNameBytes, true) {
			return nil, errRemoteCommandInput
		}
		if foldNames {
			name = strings.ToLower(name)
		}
		if _, exists := fields[name]; exists || len(fields) >= maxRemoteCommandArguments {
			return nil, errRemoteCommandInput
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errRemoteCommandInput
		}
		fields[name] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errRemoteCommandInput
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errRemoteCommandInput
	}
	return fields, nil
}

func readRemoteCommandObject(r *http.Request, required bool) (map[string]json.RawMessage, error) {
	data, err := readRemoteCommandBytes(r)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 && !required {
		return map[string]json.RawMessage{}, nil
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errRemoteCommandMedia
	}
	return remoteCommandObject(data, true)
}

func validRemoteCommandText(value string, limit int, required bool) bool {
	return (!required || strings.TrimSpace(value) != "") && len(value) <= limit &&
		utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func remoteCommandString(fields map[string]json.RawMessage, name string) (string, bool, error) {
	raw, exists := fields[name]
	if !exists {
		return "", false, nil
	}
	var value string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return "", true, errRemoteCommandInput
	}
	return value, true, nil
}

func remoteCommandTicks(fields map[string]json.RawMessage, name string, allowNegative bool) (*int64, error) {
	raw, exists := fields[name]
	if !exists {
		return nil, nil
	}
	var value int64
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || (!allowNegative && value < 0) {
		return nil, errRemoteCommandInput
	}
	return &value, nil
}

func remoteCommandIndex(fields map[string]json.RawMessage, query map[string]string, name string, minimum int64) (*int64, error) {
	var index *int64
	if raw, exists := fields[name]; exists {
		var value int64
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
			return nil, errRemoteCommandInput
		}
		index = &value
	}
	if raw, exists := query[name]; exists {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || (index != nil && *index != value) {
			return nil, errRemoteCommandInput
		}
		index = &value
	}
	if index != nil && (*index < minimum || *index > 1<<31-1) {
		return nil, errRemoteCommandInput
	}
	return index, nil
}

func parseRemotePlay(r *http.Request) (map[string]any, error) {
	if len(r.URL.RawQuery) > maxRemoteCommandBytes {
		return nil, errRemoteCommandLimit
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, errRemoteCommandInput
	}
	var queryIDs []string
	idsSupplied := false
	itemIDsKey := ""
	for key, entries := range values {
		if strings.EqualFold(key, "ItemIds") {
			if itemIDsKey != "" && itemIDsKey != key {
				return nil, errRemoteCommandInput
			}
			itemIDsKey = key
			queryIDs = append(queryIDs, queryValues(entries)...)
			idsSupplied = true
			delete(values, key)
		}
	}
	query, err := imageQuery(values)
	if err != nil {
		return nil, errRemoteCommandInput
	}
	fields, err := readRemoteCommandObject(r, true)
	if err != nil {
		return nil, err
	}
	var ids []string
	if raw, exists := fields["itemids"]; exists {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &ids) != nil {
			return nil, errRemoteCommandInput
		}
		if idsSupplied && !equalRemoteItemIDs(ids, queryIDs) {
			return nil, errRemoteCommandInput
		}
	}
	if idsSupplied {
		ids = queryIDs
	}
	if !validRemoteItemIDs(ids) {
		return nil, errRemoteCommandInput
	}
	command, supplied, err := remoteCommandString(fields, "playcommand")
	if err != nil {
		return nil, err
	}
	queryCommand, exists := query["playcommand"]
	command, _, err = mergeRemoteCommandString(command, supplied, queryCommand, exists)
	if err != nil {
		return nil, err
	}
	switch command {
	case "PlayNow", "PlayNext", "PlayLast", "PlayInstantMix", "PlayShuffle":
	default:
		return nil, errRemoteCommandInput
	}
	result := map[string]any{"ItemIds": ids, "PlayCommand": command}
	start, err := remoteCommandTicks(fields, "startpositionticks", false)
	if err != nil {
		return nil, err
	}
	start, err = mergeRemoteCommandTicks(start, query, "startpositionticks", false)
	if err != nil {
		return nil, err
	}
	if start != nil {
		result["StartPositionTicks"] = *start
	}
	for _, field := range []struct {
		name, key string
		minimum   int64
	}{{"audiostreamindex", "AudioStreamIndex", -1}, {"subtitlestreamindex", "SubtitleStreamIndex", -1}, {"startindex", "StartIndex", 0}} {
		value, err := remoteCommandIndex(fields, query, field.name, field.minimum)
		if err != nil {
			return nil, err
		}
		if value != nil {
			if field.key == "StartIndex" && *value >= int64(len(ids)) {
				return nil, errRemoteCommandInput
			}
			result[field.key] = *value
		}
	}
	source, supplied, err := remoteCommandString(fields, "mediasourceid")
	if err != nil {
		return nil, err
	}
	querySource, exists := query["mediasourceid"]
	source, supplied, err = mergeRemoteCommandString(source, supplied, querySource, exists)
	if err != nil || (supplied && !validRemoteCommandText(source, 256, true)) {
		return nil, errRemoteCommandInput
	}
	if supplied {
		if len(ids) != 1 || source != media.SourceID(ids[0]) {
			return nil, errRemoteCommandInput
		}
		result["MediaSourceId"] = source
	}
	return result, nil
}

func equalRemoteItemIDs(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func validRemoteItemIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > maxRemotePlayItems {
		return false
	}
	for _, id := range ids {
		if !validRemoteCommandText(id, 256, true) || strings.TrimSpace(id) != id {
			return false
		}
	}
	return true
}

func mergeRemoteCommandString(value string, supplied bool, other string, otherSupplied bool) (string, bool, error) {
	if !otherSupplied {
		return value, supplied, nil
	}
	if supplied && value != other {
		return "", false, errRemoteCommandInput
	}
	return other, true, nil
}

func mergeRemoteCommandTicks(value *int64, query map[string]string, name string, allowNegative bool) (*int64, error) {
	raw, exists := query[name]
	if !exists {
		return value, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || (!allowNegative && parsed < 0) || (value != nil && *value != parsed) {
		return nil, errRemoteCommandInput
	}
	return &parsed, nil
}

func parseRemotePlaystate(r *http.Request, query map[string]string) (map[string]any, error) {
	fields, err := readRemoteCommandObject(r, false)
	if err != nil {
		return nil, err
	}
	command, supplied, err := remoteCommandString(fields, "command")
	if err != nil {
		return nil, err
	}
	command, supplied, err = mergeRemoteCommandString(command, supplied, r.PathValue("Command"), r.PathValue("Command") != "")
	if err != nil {
		return nil, err
	}
	value, exists := query["command"]
	command, _, err = mergeRemoteCommandString(command, supplied, value, exists)
	if err != nil {
		return nil, err
	}
	switch command {
	case "Stop", "Pause", "Unpause", "NextTrack", "PreviousTrack", "Seek", "Rewind", "FastForward", "PlayPause", "SeekRelative":
	default:
		return nil, errRemoteCommandInput
	}
	relative := command == "SeekRelative"
	position, err := remoteCommandTicks(fields, "seekpositionticks", relative)
	if err != nil {
		return nil, err
	}
	position, err = mergeRemoteCommandTicks(position, query, "seekpositionticks", relative)
	if err != nil || ((command == "Seek" || command == "SeekRelative") && position == nil) {
		return nil, errRemoteCommandInput
	}
	result := map[string]any{"Command": command}
	if position != nil {
		result["SeekPositionTicks"] = *position
	}
	return result, nil
}

func parseRemoteGeneralCommand(r *http.Request, named bool) (map[string]any, error) {
	arguments := map[string]string{}
	name := r.PathValue("Command")
	if named {
		// The named reference route ignores JSON arguments. Consume a bounded
		// body without interpreting even its Name, Id, or controller fields.
		if _, err := readRemoteCommandBytes(r); err != nil {
			return nil, err
		}
	} else {
		fields, err := readRemoteCommandObject(r, true)
		if err != nil {
			return nil, err
		}
		name, _, err = remoteCommandString(fields, "name")
		if err != nil {
			return nil, err
		}
		if raw, exists := fields["arguments"]; exists {
			values, err := remoteCommandObject(raw, false)
			if err != nil {
				return nil, err
			}
			for key := range values {
				value, _, err := remoteCommandString(values, key)
				if err != nil || !validRemoteCommandText(value, maxRemoteArgumentBytes, false) {
					return nil, errRemoteCommandInput
				}
				arguments[key] = value
			}
		}
	}
	if !validRemoteCommandText(name, maxRemoteCommandNameBytes, true) || strings.TrimSpace(name) != name {
		return nil, errRemoteCommandInput
	}
	return map[string]any{"Name": name, "Arguments": arguments}, nil
}

func (s *Server) remotePlaystateCommand(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.RawQuery) > maxRemoteCommandBytes {
		s.remoteCommandError(w, r, errRemoteCommandLimit)
		return
	}
	query, err := streamValues(r)
	if err != nil {
		s.remoteCommandError(w, r, errRemoteCommandInput)
		return
	}
	data, err := parseRemotePlaystate(r, query)
	if err != nil {
		s.remoteCommandError(w, r, err)
		return
	}
	s.acceptRemoteCommand(w, r, "Playstate", data, true)
}

func (s *Server) remotePlayCommand(w http.ResponseWriter, r *http.Request) {
	data, err := parseRemotePlay(r)
	if err != nil {
		s.remoteCommandError(w, r, err)
		return
	}
	s.acceptRemoteCommand(w, r, "Play", data, false)
}

func (s *Server) remoteGeneralCommand(named bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.RawQuery) > maxRemoteCommandBytes {
			s.remoteCommandError(w, r, errRemoteCommandLimit)
			return
		}
		if _, err := streamValues(r); err != nil {
			s.remoteCommandError(w, r, errRemoteCommandInput)
			return
		}
		data, err := parseRemoteGeneralCommand(r, named)
		if err != nil {
			s.remoteCommandError(w, r, err)
			return
		}
		s.acceptRemoteCommand(w, r, "GeneralCommand", data, !named)
	}
}

func remoteCommandEnvelope(messageType string, data map[string]any, actor identity.Principal, target identity.ClientSession, includeTargetID bool) (events.Envelope, error) {
	// IDs in client bodies never establish authority. The Data.Id seen in the
	// captured full-command and Playstate messages is the target session ID.
	data["ControllingUserId"] = actor.User.ID
	if includeTargetID {
		data["Id"] = target.SessionID
	} else {
		delete(data, "Id")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return events.Envelope{}, errRemoteCommandInput
	}
	if len(encoded) > maxRemoteCommandBytes {
		return events.Envelope{}, errRemoteCommandLimit
	}
	return events.Envelope{MessageType: messageType, Data: encoded}, nil
}

func (s *Server) acceptRemoteCommand(w http.ResponseWriter, r *http.Request, messageType string, data map[string]any, includeTargetID bool) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	actor, err := s.identity.RevalidateSession(r.Context(), actor)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	noPresenceFilter := 0
	targets, err := s.identity.ListClientSessions(r.Context(), actor, identity.ClientSessionFilter{
		SessionID: r.PathValue("Id"), ActiveWithinSeconds: &noPresenceFilter, Limit: 1,
	})
	if err != nil {
		s.clientSessionError(w, r, err)
		return
	}
	if len(targets) != 1 {
		apiError(w, r, http.StatusNotFound, "session_not_found", "The target client session is not available to this account.")
		return
	}
	target := targets[0]
	if messageType == "Play" {
		if err := s.authorizeRemotePlayItems(r.Context(), actor.User.ID, target.UserID, data["ItemIds"].([]string)); err != nil {
			s.libraryError(w, r, err)
			return
		}
	}
	envelope, err := remoteCommandEnvelope(messageType, data, actor, target, includeTargetID)
	if err != nil {
		s.remoteCommandError(w, r, err)
		return
	}
	if s.eventHub != nil && s.hasClientControlTransport(target.SessionID) {
		if _, err := s.eventHub.PublishSession(target.UserID, target.SessionID, envelope); err != nil {
			s.remoteCommandError(w, r, err)
			return
		}
	}
	// Acceptance is independent of connectivity and is never a player ACK.
	// Only subsequent authenticated playback reports may alter stored state.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizeRemotePlayItems(ctx context.Context, actorUserID, targetUserID string, ids []string) error {
	if !validRemoteItemIDs(ids) {
		return errRemoteCommandInput
	}
	// A controller's administrator role never substitutes for target access.
	checked := map[string]bool{}
	for _, itemID := range ids {
		if checked[itemID] {
			continue
		}
		checked[itemID] = true
		if _, err := s.library.GetItem(ctx, actorUserID, itemID); err != nil {
			return err
		}
		item, err := s.library.GetItem(ctx, targetUserID, itemID)
		if err != nil {
			return err
		}
		if !item.CanPlay {
			return library.ErrForbidden
		}
	}
	return nil
}

// authorizeRemoteSocketEvent closes the gap between queueing a command and
// transport delivery. No raw token or controller login session is retained;
// the controller account and receiver's current session are checked afresh.
func (s *Server) authorizeRemoteSocketEvent(ctx context.Context, receiver identity.Principal, event events.Event) (bool, error) {
	switch event.MessageType() {
	case "Play", "Playstate", "GeneralCommand":
	default:
		return true, nil
	}
	var envelope events.Envelope
	if json.Unmarshal(event.Bytes(), &envelope) != nil || len(envelope.Data) > maxRemoteCommandBytes {
		return false, nil
	}
	fields, err := remoteCommandObject(envelope.Data, true)
	if err != nil {
		return false, nil
	}
	id, supplied, err := remoteCommandString(fields, "id")
	if err != nil || (supplied && id != receiver.SessionID) {
		return false, nil
	}
	controllerID, _, err := remoteCommandString(fields, "controllinguserid")
	if err != nil || !validRemoteCommandText(controllerID, 256, true) {
		return false, nil
	}
	controller, err := s.identity.GetUser(ctx, controllerID)
	if errors.Is(err, identity.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if controller.IsDisabled || (controller.ID != receiver.User.ID && !controller.IsAdministrator) {
		return false, nil
	}
	noPresenceFilter := 0
	targets, err := s.identity.ListClientSessions(ctx, receiver, identity.ClientSessionFilter{
		SessionID: receiver.SessionID, ActiveWithinSeconds: &noPresenceFilter, Limit: 1,
	})
	if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrClientSessionForbidden) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(targets) != 1 || !targets[0].Capabilities.SupportsMediaControl {
		return false, nil
	}
	if event.MessageType() == "Play" {
		var ids []string
		if json.Unmarshal(fields["itemids"], &ids) != nil || !validRemoteItemIDs(ids) {
			return false, nil
		}
		if err := s.authorizeRemotePlayItems(ctx, controller.ID, receiver.User.ID, ids); err != nil {
			if errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

func (s *Server) remoteCommandError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errRemoteCommandInput):
		apiError(w, r, http.StatusBadRequest, "invalid_remote_command", "Check command names, argument types, and matching tick values.")
	case errors.Is(err, errRemoteCommandMedia):
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this command.")
	case errors.Is(err, errRemoteCommandLimit), errors.Is(err, events.ErrMessageTooLarge):
		apiError(w, r, http.StatusRequestEntityTooLarge, "remote_command_limit", "The remote command exceeds its resource limits.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "remote_command_unavailable", "The command transport is currently unavailable.")
	}
}
