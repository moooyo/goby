package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

var (
	errAdminPlaybackOffline     = errors.New("playback client is disconnected")
	errAdminPlaybackUnsupported = errors.New("playback client does not support this command")
	errAdminPlaybackNotFound    = errors.New("playback client was not found")
	errAdminPlaybackSingleItem  = errors.New("play now requires exactly one media item")
)

func (s *Server) registerAdminPlaybackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/playback/sessions", s.requireAdmin(s.adminPlaybackSessions))
	mux.HandleFunc("POST /admin/v1/playback/sessions/{id}/command", s.requireAdmin(s.adminPlaybackCommand))
	mux.HandleFunc("POST /admin/v1/playback/message", s.requireAdmin(s.adminPlaybackMessage))
}

func nativePlaybackControls(actor identity.Principal, session identity.ClientSession, dto map[string]any, connected bool) map[string]bool {
	allowed := connected && session.Capabilities.SupportsMediaControl && identity.CanControlClientSession(actor.User, session)
	_, playing := dto["NowPlayingItem"]
	state, _ := dto["PlayState"].(map[string]any)
	canSeek, _ := state["CanSeek"].(bool)
	message := false
	for _, command := range session.Capabilities.SupportedCommands {
		message = message || command == "DisplayMessage"
	}
	playable := false
	for _, mediaType := range session.Capabilities.PlayableMediaTypes {
		playable = playable || mediaType == "Audio" || mediaType == "Video"
	}
	return map[string]bool{
		"Stop": allowed && playing, "Pause": allowed && playing, "Unpause": allowed && playing,
		"Seek": allowed && playing && canSeek, "Message": allowed && message,
		"Play": allowed && playable,
	}
}

func (s *Server) adminPlaybackSnapshot(r *http.Request, actor identity.Principal) ([]map[string]any, error) {
	sessions, err := s.identity.ListManagedClientSessionSummaries(r.Context(), actor, identity.ClientSessionFilter{})
	if err != nil {
		return nil, err
	}
	result, items, err := s.clientSessionSnapshotForAudience(r.Context(), actor, sessions, true)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		// Indexed image lookup normally writes its own HTTP failure. The native
		// snapshot instead uses the store directly so no partial response escapes.
		if err := s.indexNativePlaybackArtwork(r.Context(), actor, items); err != nil {
			return nil, err
		}
	}
	for _, session := range result {
		item, _ := session["NowPlayingItem"].(map[string]any)
		if item != nil {
			if preview := nativePlaybackArtworkURL(item); preview != "" {
				item["ArtworkUrl"], session["ArtworkUrl"] = preview, preview
			}
		}
	}
	// Catalog and artwork reads may wait after the identity list has finished.
	// A revoked or demoted cookie must not receive the completed snapshot.
	current, err := s.identity.RevalidateManagedPlaybackController(r.Context(), actor)
	if err != nil {
		return nil, err
	}
	for index, session := range sessions {
		connected := s.hasClientControlTransport(session.SessionID)
		result[index]["TransportConnected"] = connected
		result[index]["Controls"] = nativePlaybackControls(current, session, result[index], connected)
	}
	return result, nil
}

func (s *Server) indexNativePlaybackArtwork(ctx context.Context, actor identity.Principal, items []map[string]any) error {
	// Resolve automatic and managed artwork in one bounded query rather than
	// fetching an image per session.
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, ok := item["Id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	byID, err := s.library.ImagesForItemsFor(ctx, librarySubject(actor, actor.User.ID), ids)
	if err != nil {
		return err
	}
	presentation := itemPresentation{enableImages: true, imageTypeLimit: 1}
	for _, item := range items {
		id, _ := item["Id"].(string)
		presentation.applyIndexedImageTags(item, byID[id], false)
	}
	return nil
}

func nativePlaybackArtworkURL(item map[string]any) string {
	id, _ := item["Id"].(string)
	if !validRemoteCommandText(id, 256, true) {
		return ""
	}
	imageType, tag := "", ""
	if tags, ok := item["ImageTags"].(map[string]string); ok {
		tag = tags["Primary"]
	} else if tags, ok := item["ImageTags"].(map[string]any); ok {
		tag, _ = tags["Primary"].(string)
	}
	if tag != "" {
		imageType = "Primary"
	}
	if tags, ok := item["BackdropImageTags"].([]string); ok && len(tags) > 0 && tags[0] != "" {
		imageType, tag = "Backdrop", tags[0]
	}
	if imageType == "" {
		return ""
	}
	return "/admin/v1/items/" + url.PathEscape(id) + "/images/" + imageType + "/0?Tag=" + url.QueryEscape(tag)
}

func (s *Server) adminPlaybackSessions(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		adminSessionInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	items, err := s.adminPlaybackSnapshot(r, actor)
	if err != nil {
		s.adminPlaybackError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"Items": items, "TotalRecordCount": len(items), "Limit": identity.MaxClientSessions,
		"AtCapacity": len(items) == identity.MaxClientSessions, "ObservedAt": time.Now().UTC(),
	})
}

type adminPlaybackCommand struct {
	name               string
	messageType        string
	data               map[string]any
	includeTargetID    bool
	requiresNowPlaying bool
	playSessionID      string
}

func nativePlaybackTicks(fields map[string]json.RawMessage, field string, required bool) (*int64, error) {
	value, supplied, err := remoteCommandString(fields, field)
	if err != nil || required && !supplied {
		return nil, errRemoteCommandInput
	}
	if !supplied {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
		return nil, errRemoteCommandInput
	}
	return &parsed, nil
}

func nativePlaybackMessage(fields map[string]json.RawMessage) (map[string]any, error) {
	text, _, err := remoteCommandString(fields, "text")
	if err != nil || !validRemoteCommandText(text, maxRemoteArgumentBytes, true) {
		return nil, errRemoteCommandInput
	}
	header, supplied, err := remoteCommandString(fields, "header")
	if err != nil || supplied && !validRemoteCommandText(header, 128, false) {
		return nil, errRemoteCommandInput
	}
	timeout := 10000
	if raw, supplied := fields["timeoutms"]; supplied {
		if json.Unmarshal(raw, &timeout) != nil || timeout < 0 || timeout > 60000 || strings.TrimSpace(string(raw)) == "null" {
			return nil, errRemoteCommandInput
		}
	}
	return map[string]any{"Name": "DisplayMessage", "Arguments": map[string]string{
		"Header": header, "Text": text, "TimeoutMs": strconv.Itoa(timeout),
	}}, nil
}

func parseNativePlaybackCommand(r *http.Request) (adminPlaybackCommand, error) {
	if r.URL.RawQuery != "" {
		return adminPlaybackCommand{}, errRemoteCommandInput
	}
	fields, err := readRemoteCommandObject(r, true)
	if err != nil {
		return adminPlaybackCommand{}, err
	}
	name, _, err := remoteCommandString(fields, "command")
	if err != nil {
		return adminPlaybackCommand{}, err
	}
	command := adminPlaybackCommand{name: name, includeTargetID: true}
	allowed := map[string]bool{"command": true}
	switch name {
	case "Stop", "Pause", "Unpause":
		command.messageType, command.data, command.requiresNowPlaying = "Playstate", map[string]any{"Command": name}, true
	case "Seek":
		allowed["seekpositionticks"] = true
		position, err := nativePlaybackTicks(fields, "seekpositionticks", true)
		if err != nil {
			return adminPlaybackCommand{}, err
		}
		command.messageType, command.requiresNowPlaying = "Playstate", true
		command.data = map[string]any{"Command": name, "SeekPositionTicks": *position}
	case "DisplayMessage":
		allowed["text"], allowed["header"], allowed["timeoutms"] = true, true, true
		command.messageType = "GeneralCommand"
		command.data, err = nativePlaybackMessage(fields)
		if err != nil {
			return adminPlaybackCommand{}, err
		}
	case "PlayNow", "PlayNext", "PlayLast":
		allowed["itemids"], allowed["startpositionticks"] = true, true
		var ids []string
		if json.Unmarshal(fields["itemids"], &ids) != nil {
			return adminPlaybackCommand{}, errRemoteCommandInput
		}
		if name == "PlayNow" && len(ids) != 1 {
			return adminPlaybackCommand{}, errAdminPlaybackSingleItem
		}
		if !validRemoteItemIDs(ids) {
			return adminPlaybackCommand{}, errRemoteCommandInput
		}
		command.messageType, command.includeTargetID = "Play", false
		command.data = map[string]any{"PlayCommand": name, "ItemIds": ids}
		position, err := nativePlaybackTicks(fields, "startpositionticks", false)
		if err != nil || position != nil && name != "PlayNow" {
			return adminPlaybackCommand{}, errRemoteCommandInput
		}
		if position != nil {
			command.data["StartPositionTicks"] = *position
		}
	default:
		return adminPlaybackCommand{}, errRemoteCommandInput
	}
	if command.requiresNowPlaying {
		allowed["playsessionid"] = true
		playID, supplied, err := remoteCommandString(fields, "playsessionid")
		if err != nil || supplied && (!validRemoteCommandText(playID, 256, true) || strings.TrimSpace(playID) != playID) {
			return adminPlaybackCommand{}, errRemoteCommandInput
		}
		if supplied {
			command.playSessionID = playID
			command.data["PlaySessionId"] = playID
		}
	}
	for field := range fields {
		if !allowed[field] {
			return adminPlaybackCommand{}, errRemoteCommandInput
		}
	}
	return command, nil
}

type nativePlaybackReceipt struct {
	SessionID          string    `json:"SessionId"`
	Command            string    `json:"Command"`
	Status             string    `json:"Status"`
	DeliveryCount      int       `json:"DeliveryCount"`
	ExecutionConfirmed bool      `json:"ExecutionConfirmed"`
	AcceptedAt         time.Time `json:"AcceptedAt"`
}

func (s *Server) queueNativePlaybackCommand(ctx context.Context, actor identity.Principal, id string, command adminPlaybackCommand) (nativePlaybackReceipt, error) {
	receipt := nativePlaybackReceipt{SessionID: id, Command: command.name}
	if !validRemoteCommandText(id, 256, true) || strings.TrimSpace(id) != id {
		return receipt, errRemoteCommandInput
	}
	actor, err := s.identity.RevalidateManagedPlaybackController(ctx, actor)
	if err != nil {
		return receipt, err
	}
	noPresenceFilter := 0
	targets, err := s.identity.ListManagedClientSessionSummaries(ctx, actor, identity.ClientSessionFilter{
		SessionID: id, ActiveWithinSeconds: &noPresenceFilter, Limit: 1,
	})
	if err != nil {
		return receipt, err
	}
	if len(targets) != 1 {
		return receipt, errAdminPlaybackNotFound
	}
	target := targets[0]
	if !identity.CanControlClientSession(actor.User, target) {
		return receipt, identity.ErrClientSessionForbidden
	}
	if !target.Capabilities.SupportsMediaControl {
		return receipt, errAdminPlaybackUnsupported
	}
	if command.name == "DisplayMessage" {
		supported := false
		for _, name := range target.Capabilities.SupportedCommands {
			supported = supported || name == "DisplayMessage"
		}
		if !supported {
			return receipt, errAdminPlaybackUnsupported
		}
	}
	if command.requiresNowPlaying {
		playing, err := s.library.ListNowPlayingSessionsForSubject(ctx, librarySubject(actor, actor.User.ID), true, []string{id})
		if err != nil {
			return receipt, err
		}
		if len(playing) == 0 {
			return receipt, errAdminPlaybackNotFound
		}
		if command.playSessionID != "" && playing[0].ID != command.playSessionID {
			return receipt, errAdminPlaybackNotFound
		}
		if command.name == "Seek" && (playing[0].PlayerState.CanSeek == nil || !*playing[0].PlayerState.CanSeek) {
			return receipt, errAdminPlaybackUnsupported
		}
	}
	if command.messageType == "Play" {
		if err := s.authorizeRemotePlayItems(ctx, librarySubject(actor, actor.User.ID), clientSessionLibrarySubject(target), command.data["ItemIds"].([]string)); err != nil {
			return receipt, err
		}
		if allowed, err := s.nativePlaybackMediaTypesAllowed(ctx, target, command.data); err != nil {
			return receipt, err
		} else if !allowed {
			return receipt, errAdminPlaybackUnsupported
		}
	}
	if !s.hasClientControlTransport(id) || s.eventHub == nil {
		return receipt, errAdminPlaybackOffline
	}
	// A fresh credential check follows all catalog queries, which may wait.
	actor, err = s.identity.RevalidateManagedPlaybackController(ctx, actor)
	if err != nil {
		return receipt, err
	}
	data := make(map[string]any, len(command.data))
	for key, value := range command.data {
		data[key] = value
	}
	envelope, err := remoteCommandEnvelope(command.messageType, data, actor, target, command.includeTargetID)
	if err != nil {
		return receipt, err
	}
	receipt.DeliveryCount, err = s.eventHub.PublishScope(targetEventScope(target), envelope)
	if err != nil {
		return receipt, err
	}
	if receipt.DeliveryCount == 0 {
		return receipt, errAdminPlaybackOffline
	}
	// Queue admission never changes a playback row. A receiver must report its
	// resulting state through its existing authenticated playback API.
	receipt.Status, receipt.AcceptedAt = "queued", time.Now().UTC()
	return receipt, nil
}

func (s *Server) adminPlaybackCommand(w http.ResponseWriter, r *http.Request) {
	command, err := parseNativePlaybackCommand(r)
	if err != nil {
		s.adminPlaybackError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	receipt, err := s.queueNativePlaybackCommand(r.Context(), actor, r.PathValue("id"), command)
	if err != nil {
		s.adminPlaybackError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, receipt)
}

func (s *Server) adminPlaybackMessage(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		s.adminPlaybackError(w, r, errRemoteCommandInput)
		return
	}
	fields, err := readRemoteCommandObject(r, true)
	if err == nil {
		for name := range fields {
			if name != "text" && name != "header" && name != "timeoutms" {
				err = errRemoteCommandInput
			}
		}
	}
	var data map[string]any
	if err == nil {
		data, err = nativePlaybackMessage(fields)
	}
	if err != nil {
		s.adminPlaybackError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	items, err := s.adminPlaybackSnapshot(r, actor)
	if err != nil {
		s.adminPlaybackError(w, r, err)
		return
	}
	command := adminPlaybackCommand{name: "DisplayMessage", messageType: "GeneralCommand", data: data, includeTargetID: true}
	results := make([]nativePlaybackReceipt, 0, len(items))
	for _, item := range items {
		if item["NowPlayingItem"] == nil {
			continue
		}
		id, _ := item["Id"].(string)
		receipt, err := s.queueNativePlaybackCommand(r.Context(), actor, id, command)
		if err != nil {
			switch {
			case errors.Is(err, errAdminPlaybackOffline), errors.Is(err, errAdminPlaybackNotFound):
				receipt.Status = "offline"
			case errors.Is(err, errAdminPlaybackUnsupported), errors.Is(err, identity.ErrClientSessionForbidden):
				receipt.Status = "unsupported"
			default:
				receipt.Status = "unavailable"
			}
		}
		results = append(results, receipt)
		if r.Context().Err() != nil || remoteControllerDenied(err) {
			break
		}
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"Results": results, "AcceptedAt": time.Now().UTC()})
}

func (s *Server) adminPlaybackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errAdminPlaybackSingleItem):
		apiError(w, r, http.StatusBadRequest, "single_playback_item_required", "Choose exactly one media item for Play now.")
	case errors.Is(err, errAdminPlaybackOffline):
		apiError(w, r, http.StatusConflict, "playback_client_offline", "The playback client is disconnected. Refresh before sending another command.")
	case errors.Is(err, errAdminPlaybackUnsupported):
		apiError(w, r, http.StatusConflict, "playback_command_unsupported", "This playback client does not support the requested command.")
	case errors.Is(err, errAdminPlaybackNotFound):
		apiError(w, r, http.StatusNotFound, "playback_session_not_found", "The requested playback session is no longer available.")
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrClientSessionForbidden), errors.Is(err, identity.ErrInvalidInput):
		s.clientSessionError(w, r, err)
	case errors.Is(err, library.ErrForbidden), errors.Is(err, library.ErrNotFound), errors.Is(err, library.ErrInvalidInput):
		s.libraryError(w, r, err)
	default:
		s.remoteCommandError(w, r, err)
	}
}

func (s *Server) authorizeNativePlaybackDelivery(ctx context.Context, controller identity.Principal, target identity.ClientSession, messageType string, fields map[string]json.RawMessage) (bool, error) {
	if messageType == "GeneralCommand" {
		name, _, err := remoteCommandString(fields, "name")
		if err != nil || name != "DisplayMessage" {
			return false, nil
		}
		for _, supported := range target.Capabilities.SupportedCommands {
			if supported == name {
				return true, nil
			}
		}
		return false, nil
	}
	if messageType == "Play" {
		var ids []string
		if json.Unmarshal(fields["itemids"], &ids) != nil || !validRemoteItemIDs(ids) {
			return false, nil
		}
		name, _, err := remoteCommandString(fields, "playcommand")
		if err != nil {
			return false, nil
		}
		return s.nativePlaybackMediaTypesAllowed(ctx, target, map[string]any{"ItemIds": ids, "PlayCommand": name})
	}
	name, _, err := remoteCommandString(fields, "command")
	if err != nil || (name != "Stop" && name != "Pause" && name != "Unpause" && name != "Seek") {
		return false, nil
	}
	playID, supplied, err := remoteCommandString(fields, "playsessionid")
	if err != nil {
		return false, nil
	}
	playing, err := s.library.ListNowPlayingSessionsForSubject(ctx, librarySubject(controller, controller.User.ID), true, []string{target.SessionID})
	if errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(playing) == 0 || supplied && playing[0].ID != playID {
		return false, nil
	}
	return name != "Seek" || playing[0].PlayerState.CanSeek != nil && *playing[0].PlayerState.CanSeek, nil
}

func (s *Server) nativePlaybackMediaTypesAllowed(ctx context.Context, target identity.ClientSession, data map[string]any) (bool, error) {
	command, _ := data["PlayCommand"].(string)
	if command != "PlayNow" {
		declared := false
		for _, supported := range target.Capabilities.SupportedCommands {
			declared = declared || supported == command
		}
		if !declared {
			return false, nil
		}
	}
	ids, _ := data["ItemIds"].([]string)
	if !validRemoteItemIDs(ids) || command == "PlayNow" && len(ids) != 1 {
		return false, nil
	}
	items, err := s.library.GetNowPlayingItemsByIDFor(ctx, clientSessionLibrarySubject(target), ids)
	if errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(items) != len(ids) {
		return false, nil
	}
	for _, item := range items {
		if item.Media == nil || item.IsFolder {
			return false, nil
		}
		mediaType := "Video"
		if item.Type == "Audio" {
			mediaType = "Audio"
		}
		declared := false
		for _, supported := range target.Capabilities.PlayableMediaTypes {
			declared = declared || supported == mediaType
		}
		if !declared {
			return false, nil
		}
	}
	return true, nil
}
