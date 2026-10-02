package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// WithPlaybackControlPool enables a generation-owned reserved database lane.
// Legacy direct constructors may omit it and retain their single-pool behavior.
func WithPlaybackControlPool(pool *pgxpool.Pool) Option {
	return func(server *Server) { server.playbackControlDB = pool }
}

// requirePlaybackControl is registered only for fixed POST Ping and Stopped
// handlers and the DELETE/POST ActiveEncodings stop aliases. It selects the lane
// before parsing credentials; untrusted metadata never changes pool selection
// or bypasses the existing authentication checks.
func (s *Server) requirePlaybackControl(next http.HandlerFunc) http.HandlerFunc {
	authorized := s.requireEmby(next)
	if s.playbackControlDB == nil {
		return authorized
	}
	return func(w http.ResponseWriter, r *http.Request) {
		authorized(w, r.WithContext(database.WithPlaybackControl(r.Context())))
	}
}

// Dynamic heartbeat Info reads retain the same current catalog and playback
// authorization, while inheriting only the fixed control route's trusted lane.
func (s *Server) authorizeDynamicSource(ctx context.Context, owner dynamicsource.Owner, itemID, playID string) error {
	subject := library.Subject{UserID: owner.UserID}
	if owner.ApplicationKey {
		subject.ApplicationCredentialID = owner.SessionID
	}
	item, err := s.library.GetPlaybackControlItemFor(ctx, subject, itemID)
	if err != nil {
		return err
	}
	if !item.CanPlay || item.IsFolder {
		return library.ErrForbidden
	}
	if playID != "" {
		play, err := s.library.GetPlaybackSession(ctx, library.PlaybackOwner{UserID: owner.UserID, SessionID: owner.SessionID,
			DeviceID: owner.DeviceID, PeerIP: owner.PeerIP, ApplicationKey: owner.ApplicationKey, ApplicationClientID: owner.ApplicationClientID}, playID)
		if err != nil {
			return err
		}
		if play.ItemID != itemID || play.MediaSourceID != media.SourceID(itemID) || !time.Now().Before(play.ExpiresAt) ||
			(play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
			return library.ErrNotFound
		}
	}
	return nil
}
