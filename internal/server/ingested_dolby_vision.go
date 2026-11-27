package server

import (
	"context"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// withIngestedDolbyVision augments only a new plan's input. The authorized
// source snapshot still supplies the original facts and identity for delivery.
func (s *Server) withIngestedDolbyVision(ctx context.Context, source library.MediaFile, info media.Info) media.Info {
	return media.ApplyDolbyVisionRPUEvidence(info, s.library.IngestedDolbyVisionEvidence(ctx, source))
}
