package server

import (
	"context"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// External bitmap files are decoded only by the burn-in path. They must not
// enter text parsing or advertise a browser text-track delivery URL.
func (s *Server) validatePlannedExternalSubtitle(ctx context.Context, principal identity.Principal, scope transcode.Scope, plan transcode.Plan) error {
	if transcode.IsBitmapSubtitle(plan.Subtitle.Codec) {
		return s.withPlannedBitmapSubtitle(ctx, principal, scope, plan, nil)
	}
	_, err := s.readPlannedExternalSubtitle(ctx, principal, scope, plan)
	return err
}

func (s *Server) withPlannedBitmapSubtitle(ctx context.Context, principal identity.Principal, scope transcode.Scope, plan transcode.Plan, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
	selected := plan.Subtitle
	if selected.Mode != "burn" || selected.ExternalTag == "" ||
		(selected.Codec != "hdmv_pgs_subtitle" && selected.Codec != "dvd_subtitle") {
		return library.ErrInvalidInput
	}
	return s.library.WithBitmapSubtitleFor(ctx, librarySubject(principal, principal.User.ID), scope.ItemID, scope.SourceID,
		selected.StreamIndex, func(work context.Context, info library.BitmapSubtitle, input media.ExternalSubtitleTimelineInput) error {
			if info.Tag != selected.ExternalTag || info.Codec != selected.Codec || info.SourceStreamIndex != selected.ExternalStreamIndex {
				return library.ErrSourceChanged
			}
			if consume != nil {
				return consume(work, input)
			}
			return work.Err()
		})
}

// Borrow selected, authorized descriptors only while the library holds their
// root IO capability. The runner copies fixed-name private assets, and a final
// playback authorization rejects revocation or a rescan during that copy.
func (s *Server) readBurnBitmapSubtitleAsset(ctx context.Context, spec transcode.Spec, consume func(context.Context, media.ExternalSubtitleTimelineInput) error) error {
	if consume == nil {
		return library.ErrInvalidInput
	}
	principal, err := s.burnSubtitlePrincipal(spec)
	if err != nil {
		return err
	}
	input, _, err := s.authorizeHLS(ctx, principal, spec.Scope, spec.SourceStamp, spec.Plan)
	if err != nil {
		return err
	}
	if err := input.Close(); err != nil {
		return media.SourceReadRetirementError(err, input)
	}
	if err := s.withPlannedBitmapSubtitle(ctx, principal, spec.Scope, spec.Plan, consume); err != nil {
		return err
	}
	current, _, err := s.authorizeHLS(ctx, principal, spec.Scope, spec.SourceStamp, spec.Plan)
	if err != nil {
		return err
	}
	if err := current.Close(); err != nil {
		return media.SourceReadRetirementError(err, current)
	}
	return nil
}
