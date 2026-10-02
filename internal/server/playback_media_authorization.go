package server

import (
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

// Delivery policy is checked after the authority transaction commits and before
// source I/O. Policy retirement may reenter HLS or the manager, so this callback
// must never run under database row locks.
func (s *Server) checkHLSPlaybackAuthorization(current library.PlaybackMediaAuthorization, scope transcode.Scope, plan transcode.Plan) error {
	fresh, play := current.Principal, current.Play
	if fresh.IsApplicationKey() != scope.ApplicationKey || fresh.User.ID != scope.UserID ||
		fresh.ClientSessionID != scope.ApplicationClientID || fresh.SessionID != scope.AuthSessionID || fresh.Client.DeviceID != scope.DeviceID {
		return library.ErrNotFound
	}
	if err := s.checkMediaPolicy(fresh, scope); err != nil {
		return err
	}
	// Registered outputs retain their captured planning limits; current policy
	// revalidation checks conversion support without negotiating a new plan.
	if !hlsPlanAllowed(plan, hlsPrincipalLimits(config.TranscodingConfig{Enabled: s.cfg.Transcoding.Enabled}, fresh)) {
		return library.ErrForbidden
	}
	if play.IsDynamic {
		return library.ErrSourceChanged
	}
	if play.ItemID != scope.ItemID || play.MediaSourceID != scope.SourceID || !time.Now().Before(play.ExpiresAt) ||
		(play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
		return library.ErrNotFound
	}
	s.hls.applyPlaybackSnapshot(play)
	return nil
}
