package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerRootBindingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/libraries/{id}/roots", s.requireAdmin(s.listRegisteredRoots))
	mux.HandleFunc("GET /admin/v1/libraries/{id}/roots/{rootId}/binding", s.requireAdmin(s.getRootBinding))
	mux.HandleFunc("PUT /admin/v1/libraries/{id}/roots/{rootId}/binding", s.requireAdmin(s.updateRootBinding))
}

func nativeRegisteredRoot(root library.RegisteredRootInfo) map[string]any {
	return map[string]any{
		"Id": root.RootID, "LibraryId": root.LibraryID, "Path": root.Path,
		"AllowedPath": root.AllowedPath, "RelativePath": root.RelativePath, "Revision": root.Revision,
	}
}

func nativeRootBindingIdentity(value library.RootBindingIdentityInfo) map[string]any {
	return map[string]any{"Profile": value.Profile, "FilesystemUUID": value.FilesystemUUID, "Digest": value.Digest}
}

func nativeRootBindingTopology(value *library.RootBindingTopologyInfo) map[string]any {
	boundaries := make([]map[string]any, 0, len(value.Boundaries))
	for _, boundary := range value.Boundaries {
		boundaries = append(boundaries, map[string]any{
			"RelativePath": boundary.RelativePath, "Identity": nativeRootBindingIdentity(boundary.Identity),
		})
	}
	return map[string]any{
		"Anchor":         nativeRootBindingIdentity(value.Anchor),
		"RegisteredRoot": nativeRootBindingIdentity(value.RegisteredRoot), "Boundaries": boundaries,
	}
}

// Project the bounded display model explicitly. The native API never serializes
// a storage document, opaque directory handle, or live filesystem witness.
func nativeRootBinding(binding library.RootBindingInfo) map[string]any {
	result := nativeRegisteredRoot(binding.RegisteredRootInfo)
	result["Status"] = binding.Status
	if binding.ApprovedFingerprint != "" {
		result["ApprovedFingerprint"] = binding.ApprovedFingerprint
	}
	if binding.ObservedFingerprint != "" {
		result["ObservedFingerprint"] = binding.ObservedFingerprint
	}
	if binding.Approved != nil {
		result["Approved"] = nativeRootBindingTopology(binding.Approved)
	}
	if binding.Observed != nil {
		result["Observed"] = nativeRootBindingTopology(binding.Observed)
	}
	if binding.BoundAt != nil {
		result["BoundAt"] = binding.BoundAt.UTC()
	}
	if binding.BoundBy != "" {
		result["BoundBy"] = binding.BoundBy
	}
	return result
}

func rootBindingNoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func rootBindingNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation does not accept query parameters.")
		return false
	}
	return true
}

func (s *Server) listRegisteredRoots(w http.ResponseWriter, r *http.Request) {
	rootBindingNoCache(w)
	if !rootBindingNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	roots, err := s.library.ListRegisteredRoots(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		s.rootBindingError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		items = append(items, nativeRegisteredRoot(root))
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) getRootBinding(w http.ResponseWriter, r *http.Request) {
	rootBindingNoCache(w)
	if !rootBindingNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	binding, err := s.library.GetRootBinding(r.Context(), actor, r.PathValue("id"), r.PathValue("rootId"))
	if err != nil {
		s.rootBindingError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Binding": nativeRootBinding(binding)})
}

func rootBindingInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the storage binding request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func decodeRootBindingUpdate(w http.ResponseWriter, r *http.Request) (library.RootBindingUpdate, bool) {
	var input library.RootBindingUpdate
	if !rootBindingNoQuery(w, r) {
		return input, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return input, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		rootBindingInputError(w, r, map[string]string{"Body": "Supply a UTF-8 JSON object no larger than 4 KiB."})
		return input, false
	}
	values, invalid := managedUserObject(data, []string{"Revision", "ObservedFingerprint", "AcknowledgeMissingRemoval"}, "")
	if invalid != nil {
		rootBindingInputError(w, r, invalid)
		return input, false
	}
	invalid = make(map[string]string)
	managedUserValue(values["Revision"], "Revision", &input.Revision, invalid)
	managedUserValue(values["ObservedFingerprint"], "ObservedFingerprint", &input.ObservedFingerprint, invalid)
	managedUserValue(values["AcknowledgeMissingRemoval"], "AcknowledgeMissingRemoval", &input.AcknowledgeMissingRemoval, invalid)
	if input.Revision == "" {
		invalid["Revision"] = "Supply the current decimal revision string."
	}
	if input.ObservedFingerprint == "" {
		invalid["ObservedFingerprint"] = "Supply the fingerprint of the reviewed observation."
	}
	if !input.AcknowledgeMissingRemoval {
		invalid["AcknowledgeMissingRemoval"] = "Explicitly acknowledge removal of missing records after a subsequent complete scan."
	}
	if len(invalid) != 0 {
		rootBindingInputError(w, r, invalid)
		return library.RootBindingUpdate{}, false
	}
	return input, true
}

func (s *Server) updateRootBinding(w http.ResponseWriter, r *http.Request) {
	rootBindingNoCache(w)
	input, ok := decodeRootBindingUpdate(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	binding, err := s.library.UpdateRootBinding(r.Context(), actor, r.PathValue("id"), r.PathValue("rootId"), input)
	if err != nil {
		s.rootBindingError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Binding": nativeRootBinding(binding)})
}

func (s *Server) rootBindingError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrRootBindingConflict) {
		apiError(w, r, http.StatusConflict, "root_binding_conflict", "The storage binding or observed storage changed. Reload it before trying again.")
		return
	}
	s.libraryError(w, r, err)
}
