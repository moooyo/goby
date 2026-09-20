package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// DefaultAnalysisProfile is independent of playback and general server settings.
func DefaultAnalysisProfile() AnalysisProfile {
	return AnalysisProfile{
		AutoPublishIntros: true, PreviewIntervalSeconds: 10, PreviewQuality: 80,
		MaxSourceBytes: 128 << 30, MaxItemRuntimeSeconds: 1200, FeatureCacheMaxBytes: 128 << 20,
	}
}

// ValidateAnalysisProfile is shared by configuration writes and archive validation.
// The complete profile is required; zero values never imply omitted defaults.
func ValidateAnalysisProfile(profile AnalysisProfile) error {
	if profile.PreviewIntervalSeconds < 2 || profile.PreviewIntervalSeconds > 120 ||
		profile.PreviewQuality < 40 || profile.PreviewQuality > 95 ||
		profile.MaxSourceBytes < 1 || profile.MaxSourceBytes > 1<<40 ||
		profile.MaxItemRuntimeSeconds < 1 || profile.MaxItemRuntimeSeconds > 7200 ||
		profile.FeatureCacheMaxBytes < 1<<20 || profile.FeatureCacheMaxBytes > 512<<20 {
		return fmt.Errorf("%w: invalid media analysis profile", ErrInvalidInput)
	}
	return nil
}

const analysisConfigurationColumns = `revision,auto_publish_intros,preview_interval_seconds,preview_quality,
	max_source_bytes,max_item_runtime_seconds,feature_cache_max_bytes,updated_at`

func scanAnalysisConfiguration(row rowScanner) (AnalysisConfiguration, error) {
	var result AnalysisConfiguration
	var revision int64
	err := row.Scan(&revision, &result.Profile.AutoPublishIntros, &result.Profile.PreviewIntervalSeconds,
		&result.Profile.PreviewQuality, &result.Profile.MaxSourceBytes, &result.Profile.MaxItemRuntimeSeconds,
		&result.Profile.FeatureCacheMaxBytes, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisConfiguration{}, fmt.Errorf("%w: media analysis configuration is missing", ErrUnavailable)
	}
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	if revision < 1 || ValidateAnalysisProfile(result.Profile) != nil || result.UpdatedAt.IsZero() {
		return AnalysisConfiguration{}, fmt.Errorf("%w: invalid stored media analysis configuration", ErrUnavailable)
	}
	result.Revision = strconv.FormatInt(revision, 10)
	result.Defaults = DefaultAnalysisProfile()
	return result, nil
}

func (s *Store) GetAnalysisConfiguration(ctx context.Context, actor identity.Principal) (AnalysisConfiguration, error) {
	if err := analysisContext(ctx); err != nil {
		return AnalysisConfiguration{}, err
	}
	if s == nil || s.pool == nil {
		return AnalysisConfiguration{}, ErrUnavailable
	}
	// Read committed gives the final authorization check a fresh credential view.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	defer rollback(tx)
	administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	if err := administrator.check(ctx, tx, false); err != nil {
		return AnalysisConfiguration{}, err
	}
	result, err := scanAnalysisConfiguration(tx.QueryRow(ctx, `SELECT `+analysisConfigurationColumns+` FROM analysis_settings WHERE id=1`))
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return AnalysisConfiguration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisConfiguration{}, err
	}
	return result, nil
}

func (s *Store) UpdateAnalysisConfiguration(ctx context.Context, actor identity.Principal, input AnalysisConfigurationUpdate) (AnalysisConfiguration, error) {
	if err := analysisContext(ctx); err != nil {
		return AnalysisConfiguration{}, err
	}
	if s == nil || s.pool == nil {
		return AnalysisConfiguration{}, ErrUnavailable
	}
	revision, err := libraryEditRevision(input.Revision)
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	if err := ValidateAnalysisProfile(input.Profile); err != nil {
		return AnalysisConfiguration{}, err
	}
	var result AnalysisConfiguration
	err = s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		previous, err := scanAnalysisConfiguration(tx.QueryRow(`SELECT ` + analysisConfigurationColumns + ` FROM analysis_settings WHERE id=1 FOR UPDATE`))
		if err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		if previous.Revision != input.Revision {
			return ErrAnalysisConflict
		}
		result = previous
		if previous.Profile != input.Profile {
			if revision == math.MaxInt64 {
				return ErrAnalysisConflict
			}
			profile := input.Profile
			result, err = scanAnalysisConfiguration(tx.QueryRow(`UPDATE analysis_settings SET
				revision=revision+1,auto_publish_intros=$2,preview_interval_seconds=$3,preview_quality=$4,
				max_source_bytes=$5,max_item_runtime_seconds=$6,feature_cache_max_bytes=$7,updated_at=clock_timestamp()
				WHERE id=1 AND revision=$1 RETURNING `+analysisConfigurationColumns,
				revision, profile.AutoPublishIntros, profile.PreviewIntervalSeconds, profile.PreviewQuality,
				profile.MaxSourceBytes, profile.MaxItemRuntimeSeconds, profile.FeatureCacheMaxBytes))
			if err != nil {
				return err
			}
			// Keep detection evidence and administrator decisions. Only references
			// whose authority depended on the previous profile are withdrawn.
			if _, err := tx.Exec(`UPDATE analysis_detections SET auto_published=false WHERE auto_published`); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM analysis_previews`); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM analysis_feature_cache`); err != nil {
				return err
			}
			if err := analysisInvalidateCatalog(tx); err != nil {
				return err
			}
		}
		if err := analysisContext(ctx); err != nil {
			return err
		}
		// This must be the final database operation before the owner commits.
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	return result, nil
}
