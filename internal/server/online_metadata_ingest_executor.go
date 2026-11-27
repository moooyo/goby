package server

import (
	"context"
	"errors"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/tasks"
)

type onlineMetadataIngestExecutor struct{ server *Server }

func (e onlineMetadataIngestExecutor) Available() bool {
	s := e.server
	if s == nil || !s.cfg.FastLibraryIngest || !s.cfg.FastLibraryOnlineMetadata || s.library == nil || s.settings == nil {
		return false
	}
	for _, status := range s.onlineProviderConfig().Status() {
		if status.ID == "tmdb" && status.Configured {
			return true
		}
	}
	return false
}

func (e onlineMetadataIngestExecutor) Execute(ctx context.Context, work tasks.Work, report func(tasks.Progress) error) error {
	if !e.Available() || work.TaskKey != library.TaskOnlineMetadataIngestKey || report == nil {
		return tasks.ErrUnavailable
	}
	s := e.server
	work = work.WithContext(ctx)
	var revision string
	if err := s.db.QueryRow(ctx, `SELECT revision::text FROM provider_configuration WHERE id=1`).Scan(&revision); err != nil {
		return err
	}
	config := s.onlineProviderConfig()
	client := onlineMetadataIngestClient{server: s, config: config, client: providers.New(config)}
	fence := func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		var enabled bool
		var language, country, current string
		if err := tx.QueryRow(`SELECT COALESCE(management#>'{Metadata,EnableInternetProviders}'='true'::jsonb,false),
			COALESCE(management#>>'{Metadata,PreferredMetadataLanguage}',''),COALESCE(management#>>'{Metadata,MetadataCountryCode}','')
			FROM managed_settings WHERE id=1 FOR SHARE`).Scan(&enabled, &language, &country); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT revision::text FROM provider_configuration WHERE id=1 FOR SHARE`).Scan(&current); err != nil {
			return err
		}
		if !enabled || language != config.Language || country != config.Country || current != revision || s.onlineProviderConfig() != config {
			return tasks.ErrUnavailable
		}
		return ctx.Err()
	}
	err := s.library.EnrichIngestedOnlineMetadata(ctx, client, work.ChildID, work.LibraryID, fence, func(p library.ProviderProgress) error {
		return report(tasks.Progress{Processed: p.Processed, Updated: p.Updated})
	})
	if errors.Is(err, providers.ErrNotConfigured) || errors.Is(err, providers.ErrDisabled) || errors.Is(err, providers.ErrUnavailable) {
		return tasks.ErrUnavailable
	}
	return err
}

// A settings or credential change ends the current operation before its next
// request. Already-admitted provider responses still have to pass the SQL fence.
type onlineMetadataIngestClient struct {
	server *Server
	config providers.Config
	client *providers.Client
}

func (c onlineMetadataIngestClient) Status() []providers.Status {
	if c.server.onlineProviderConfig() != c.config {
		return nil
	}
	return c.client.Status()
}

func (c onlineMetadataIngestClient) Search(ctx context.Context, provider string, query providers.Query) ([]providers.Match, error) {
	if c.server.onlineProviderConfig() != c.config {
		return nil, providers.ErrDisabled
	}
	return c.client.Search(ctx, provider, query)
}

func (c onlineMetadataIngestClient) Lookup(ctx context.Context, selection providers.Selection) (providers.Metadata, error) {
	if c.server.onlineProviderConfig() != c.config {
		return providers.Metadata{}, providers.ErrDisabled
	}
	return c.client.Lookup(ctx, selection)
}
