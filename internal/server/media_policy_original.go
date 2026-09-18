package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

type mediaPolicyScopeContextKey struct{}

func (s *Server) originalMediaPolicyScope(ctx context.Context, r *http.Request, principal identity.Principal, source library.MediaFile) (transcode.Scope, error) {
	scope := transcode.Scope{ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID,
		UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		ItemID: source.Item.ID, SourceID: source.SourceID}
	values, err := streamValues(r)
	if err != nil {
		return transcode.Scope{}, library.ErrInvalidInput
	}
	if reference := values["playsessionid"]; reference != "" && r.Method != http.MethodHead {
		// Original access is authorized by the current credential and source,
		// independently of playback state. Only an already live, owned source
		// correlation may deduplicate delivery with conversion output. Reads
		// never prepare, refresh, or revive a play or bind an unknown nonce.
		play, err := s.library.GetPlaybackSession(ctx, playbackOwner(principal), reference)
		if errors.Is(err, library.ErrNotFound) {
			return scope, nil
		}
		if err != nil {
			return transcode.Scope{}, err
		}
		if play.ItemID == source.Item.ID && play.MediaSourceID == source.SourceID && !play.IsDynamic {
			scope.PlaySessionID = play.ID
		}
	}
	return scope, nil
}

func (s *Server) completeOriginalMediaPolicy(ctx context.Context) {
	if scope, ok := ctx.Value(mediaPolicyScopeContextKey{}).(transcode.Scope); ok {
		s.completeMediaPolicy(ctx, scope)
	}
}

func (s *Server) touchOriginalMediaPolicy(ctx context.Context, principal identity.Principal) {
	if scope, ok := ctx.Value(mediaPolicyScopeContextKey{}).(transcode.Scope); ok {
		s.touchMediaPolicy(ctx, principal, scope)
	}
}

func (s *Server) failOriginalMediaPolicy(ctx context.Context) {
	if scope, ok := ctx.Value(mediaPolicyScopeContextKey{}).(transcode.Scope); ok {
		s.failMediaPolicy(ctx, scope)
	}
}
