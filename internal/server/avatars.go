package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerAvatarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/users/{id}/image", s.requireAdmin(s.avatarCollection))
	mux.HandleFunc("PUT /admin/v1/users/{id}/image", s.requireAdmin(s.putAvatar))
	mux.HandleFunc("DELETE /admin/v1/users/{id}/image", s.requireAdmin(s.deleteAvatar))
	mux.HandleFunc("GET /admin/v1/users/{id}/image/content", s.requireAdmin(s.avatarPreview))
	mux.HandleFunc("GET /emby/Users/{Id}/Images/{Type}", s.embyAvatar)
	mux.HandleFunc("GET /emby/Users/{Id}/Images/{Type}/{Index}", s.embyAvatar)
	mux.HandleFunc("POST /emby/Users/{Id}/Images/{Type}", s.requireEmby(s.embyPutAvatar))
	mux.HandleFunc("POST /emby/Users/{Id}/Images/{Type}/{Index}", s.requireEmby(s.embyPutAvatar))
	mux.HandleFunc("DELETE /emby/Users/{Id}/Images/{Type}", s.requireEmby(s.embyDeleteAvatar))
	mux.HandleFunc("DELETE /emby/Users/{Id}/Images/{Type}/{Index}", s.requireEmby(s.embyDeleteAvatar))
	mux.HandleFunc("POST /emby/Users/{Id}/Images/{Type}/Delete", s.requireEmby(s.embyDeleteAvatar))
	mux.HandleFunc("POST /emby/Users/{Id}/Images/{Type}/{Index}/Delete", s.requireEmby(s.embyDeleteAvatar))
}

func avatarCollectionDTO(userID string, value identity.AvatarCollection) map[string]any {
	items := make([]map[string]any, 0, len(value.Images))
	for _, image := range value.Images {
		items = append(items, map[string]any{"ImageType": "Primary", "ImageIndex": 0, "Tag": image.Tag,
			"Width": image.Width, "Height": image.Height, "Size": strconv.FormatInt(image.Size, 10),
			"PreviewUrl": "/admin/v1/users/" + url.PathEscape(userID) + "/image/content?Tag=" + url.QueryEscape(image.Tag), "Source": "managed"})
	}
	return map[string]any{"Revision": value.Revision, "Items": items}
}

func avatarMatchRevision(w http.ResponseWriter, r *http.Request, required bool) (*string, bool) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 && !required {
		return nil, true
	}
	if len(values) == 0 {
		apiError(w, r, http.StatusPreconditionRequired, "precondition_required", "Reload the avatar and supply its current If-Match revision.")
		return nil, false
	}
	var revision string
	valid := false
	if len(values) == 1 {
		// An HTTP entity tag is opaque text, not a JSON string. In particular,
		// a backslash escape must never alias the current decimal revision.
		value := strings.Trim(values[0], " \t")
		valid = len(value) >= 3 && len(value) <= 80 && value[0] == '"' && value[len(value)-1] == '"'
		if valid {
			revision = value[1 : len(value)-1]
		}
	}
	if valid {
		valid = revision == "0" || revision[0] >= '1' && revision[0] <= '9'
		for _, digit := range revision {
			valid = valid && digit >= '0' && digit <= '9'
		}
	}
	if !valid {
		apiError(w, r, http.StatusBadRequest, "invalid_revision", "If-Match must contain one quoted avatar revision.")
		return nil, false
	}
	return &revision, true
}

func (s *Server) avatarError(w http.ResponseWriter, r *http.Request, err error) {
	s.artworkManagementError(w, r, err)
}

func (s *Server) avatarCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.identity.GetAvatar(r.Context(), actor, id)
	if err != nil {
		s.avatarError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, avatarCollectionDTO(id, value))
}

func (s *Server) putAvatar(w http.ResponseWriter, r *http.Request) {
	s.mutateAvatar(w, r, false, false)
}
func (s *Server) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	s.mutateAvatar(w, r, true, false)
}
func (s *Server) embyPutAvatar(w http.ResponseWriter, r *http.Request) {
	s.mutateAvatar(w, r, false, true)
}
func (s *Server) embyDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	s.mutateAvatar(w, r, true, true)
}

func avatarRouteTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	request, err := parseImageRequest(r)
	if err != nil || request.typeName != "Primary" || request.index != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_image_request", "User avatars support only Primary image index zero.")
		return "", false
	}
	copy := r.Clone(r.Context())
	copy.SetPathValue("id", r.PathValue("Id"))
	return managedUserID(w, copy)
}

func readAvatarUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" {
		apiError(w, r, http.StatusUnsupportedMediaType, "invalid_image", "Upload JPEG, PNG, or GIF bytes with their matching Content-Type.")
		return nil, false
	}
	if r.ContentLength > artwork.ManagedUploadBytes {
		apiError(w, r, http.StatusRequestEntityTooLarge, "artwork_limit", "The avatar must be no larger than 20 MiB.")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, artwork.ManagedUploadBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		apiError(w, r, http.StatusRequestEntityTooLarge, "artwork_limit", "The avatar must be no larger than 20 MiB.")
		return nil, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		apiError(w, r, http.StatusRequestTimeout, "request_timeout", "The avatar upload exceeded its body deadline.")
		return nil, false
	}
	if err != nil || len(data) == 0 || http.DetectContentType(data) != contentType {
		apiError(w, r, http.StatusUnsupportedMediaType, "invalid_image", "The upload does not contain the declared image format.")
		return nil, false
	}
	return data, true
}

func (s *Server) mutateAvatar(w http.ResponseWriter, r *http.Request, remove, compatibility bool) {
	id, ok := "", false
	if compatibility {
		id, ok = avatarRouteTarget(w, r)
	} else {
		id, ok = managedUserID(w, r)
	}
	if !ok {
		return
	}
	revision, ok := avatarMatchRevision(w, r, !compatibility)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	actor := r.Context().Value(principalKey).(identity.Principal)
	// A private target check precedes body consumption, including cross-user
	// attempts by ordinary clients. The store repeats it at commit authority.
	if _, err := s.identity.GetAvatar(ctx, actor, id); err != nil {
		s.avatarError(w, r, err)
		return
	}
	var value identity.AvatarCollection
	var err error
	if remove {
		value, err = s.identity.DeleteAvatar(ctx, actor, id, revision)
	} else {
		select {
		case s.images.slots <- struct{}{}:
			defer func() { <-s.images.slots }()
		case <-ctx.Done():
			s.avatarError(w, r, ctx.Err())
			return
		}
		data, ok := readAvatarUpload(w, r)
		if !ok {
			return
		}
		value, err = s.identity.PutAvatar(ctx, actor, id, revision, data)
	}
	if err != nil {
		s.avatarError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", `"`+value.Revision+`"`)
	if compatibility {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	jsonResponse(w, http.StatusOK, avatarCollectionDTO(id, value))
}

func avatarImageSource(image artwork.StoredImage, err error) (io.ReadCloser, library.Image, error) {
	if errors.Is(err, identity.ErrNotFound) {
		err = library.ErrNotFound
	}
	if err != nil {
		return nil, library.Image{}, err
	}
	return io.NopCloser(bytes.NewReader(image.Content)), library.Image{ImageType: "Primary", ImageIndex: 0,
		Tag: image.Tag, MIMEType: image.MIMEType, Width: image.Width, Height: image.Height,
		Size: image.Size, ModifiedAt: image.ModifiedAt}, nil
}

func (s *Server) avatarPreview(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	copy := r.Clone(r.Context())
	copy.SetPathValue("Type", "Primary")
	copy.SetPathValue("Index", "0")
	s.serveArtworkImage(w, copy, func(ctx context.Context) (io.ReadCloser, library.Image, error) {
		image, err := s.identity.ReadAvatar(ctx, actor, id)
		return avatarImageSource(image, err)
	})
}

func (s *Server) embyAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := avatarRouteTarget(w, r)
	if !ok {
		return
	}
	token, client, err := parseEmbyCredentials(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_client", "Supply unambiguous client metadata.")
		return
	}
	if token != "" {
		// An invalid supplied credential never falls back to anonymous access.
		s.requireEmby(func(w http.ResponseWriter, r *http.Request) {
			actor := r.Context().Value(principalKey).(identity.Principal)
			s.serveArtworkImage(w, r, func(ctx context.Context) (io.ReadCloser, library.Image, error) {
				image, err := s.identity.ReadAvatar(ctx, actor, id)
				return avatarImageSource(image, err)
			})
		})(w, r)
		return
	}
	remote := !s.endpointInfo(r).IsInNetwork
	s.serveArtworkImage(w, r, func(ctx context.Context) (io.ReadCloser, library.Image, error) {
		image, err := s.identity.ReadPublicAvatar(ctx, id, remote, client.DeviceID)
		return avatarImageSource(image, err)
	})
}

// Projection is best effort and cannot turn an otherwise valid login into a
// failed authentication response. Payload routes independently reauthorize;
// a displayed tag is never a bearer credential or a cache authorization key.
func (s *Server) attachAvatarDTOs(ctx context.Context, users []map[string]any) {
	for start := 0; start < len(users); start += 256 {
		end := min(start+256, len(users))
		ids := make([]string, 0, end-start)
		for _, user := range users[start:end] {
			if id, ok := user["Id"].(string); ok {
				ids = append(ids, id)
			}
		}
		projections, err := s.identity.AvatarProjections(ctx, ids)
		if err != nil {
			return
		}
		for _, user := range users[start:end] {
			id, _ := user["Id"].(string)
			if image, exists := projections[id]; exists {
				user["PrimaryImageTag"], user["PrimaryImageAspectRatio"] = image.Tag, image.AspectRatio
			}
		}
	}
}

func (s *Server) avatarUserDTO(ctx context.Context, user map[string]any) map[string]any {
	s.attachAvatarDTOs(ctx, []map[string]any{user})
	return user
}
