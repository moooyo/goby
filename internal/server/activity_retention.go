package server

import (
	"context"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/config"
)

func (s *Server) startActivityRetention() {
	days := s.cfg.ActivityRetentionDays
	if days == 0 {
		days = config.DefaultActivityRetentionDays
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.activityCancel, s.activityDone = cancel, make(chan struct{})
	go func() {
		defer close(s.activityDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := activity.PruneExpired(ctx, s.db, time.Duration(days)*24*time.Hour); err != nil && ctx.Err() == nil {
					s.log.Warn("activity retention will retry", "error", err)
				}
			}
		}
	}()
}

func (s *Server) cancelActivityRetention() {
	if s.activityCancel != nil {
		s.activityCancel()
	}
}

func (s *Server) waitActivityRetention(ctx context.Context) error {
	if s.activityDone == nil {
		return nil
	}
	select {
	case <-s.activityDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
