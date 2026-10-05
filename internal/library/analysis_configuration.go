package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/systemevents"
)

// DefaultAnalysisProfile is independent of playback and general server settings.
func DefaultAnalysisProfile() AnalysisProfile {
	return AnalysisProfile{
		AutoPublishIntros: true, PreviewIntervalSeconds: 10, PreviewQuality: 80,
		MaxSourceBytes: 128 << 30, MaxItemRuntimeSeconds: 1200, FeatureCacheMaxBytes: 128 << 20,
		IntroSkipper: introskipper.DefaultOptions(),
	}
}

// ValidateAnalysisProfile is shared by configuration writes and archive validation.
// The complete profile is required; zero values never imply omitted defaults.
func ValidateAnalysisProfile(profile AnalysisProfile) error {
	if err := validateAnalysisProfileLimits(profile); err != nil {
		return err
	}
	if err := introskipper.ValidateOptions(profile.IntroSkipper); err != nil {
		return fmt.Errorf("%w: invalid Intro Skipper options: %v", ErrInvalidInput, err)
	}
	return nil
}

// ValidateLegacyAnalysisProfile validates the frozen pre-Intro-Skipper profile.
// Historical archives must not acquire defaults that change admission hashes.
func ValidateLegacyAnalysisProfile(profile AnalysisProfile) error {
	if profile.IntroSkipper != (introskipper.Options{}) {
		return fmt.Errorf("%w: unexpected Intro Skipper options in a historical profile", ErrInvalidInput)
	}
	return validateAnalysisProfileLimits(profile)
}

func validateAnalysisProfileLimits(profile AnalysisProfile) error {
	if profile.PreviewIntervalSeconds < 2 || profile.PreviewIntervalSeconds > 120 ||
		profile.PreviewQuality < 40 || profile.PreviewQuality > 95 ||
		profile.MaxSourceBytes < 1 || profile.MaxSourceBytes > 1<<40 ||
		profile.MaxItemRuntimeSeconds < 1 || profile.MaxItemRuntimeSeconds > 7200 ||
		profile.FeatureCacheMaxBytes < 1<<20 || profile.FeatureCacheMaxBytes > 512<<20 {
		return fmt.Errorf("%w: invalid media analysis profile", ErrInvalidInput)
	}
	return nil
}

// DecodeAnalysisIntroSkipperOptions preserves the exact closed settings shape
// when reading durable JSONB or validating an archive outside the live store.
func DecodeAnalysisIntroSkipperOptions(raw []byte) (introskipper.Options, error) {
	var options introskipper.Options
	if len(raw) == 0 || len(raw) > 4096 || analysisStrictJSON(raw, &options) != nil ||
		introskipper.ValidateOptions(options) != nil {
		return introskipper.Options{}, ErrInvalidInput
	}
	return options, nil
}

const analysisConfigurationColumns = `revision,auto_publish_intros,preview_interval_seconds,preview_quality,
	max_source_bytes,max_item_runtime_seconds,feature_cache_max_bytes,intro_skipper_options,updated_at`

func scanAnalysisConfiguration(row rowScanner) (AnalysisConfiguration, error) {
	var result AnalysisConfiguration
	var revision int64
	var optionsRaw []byte
	err := row.Scan(&revision, &result.Profile.AutoPublishIntros, &result.Profile.PreviewIntervalSeconds,
		&result.Profile.PreviewQuality, &result.Profile.MaxSourceBytes, &result.Profile.MaxItemRuntimeSeconds,
		&result.Profile.FeatureCacheMaxBytes, &optionsRaw, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisConfiguration{}, fmt.Errorf("%w: media analysis configuration is missing", ErrUnavailable)
	}
	if err != nil {
		return AnalysisConfiguration{}, err
	}
	result.Profile.IntroSkipper, err = DecodeAnalysisIntroSkipperOptions(optionsRaw)
	if err != nil ||
		revision < 1 || ValidateAnalysisProfile(result.Profile) != nil || result.UpdatedAt.IsZero() {
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
	// Retain the historical wire field and immutable admission fingerprints.
	// Current publication is selected per library, so a legacy false value
	// cannot silently disable an explicitly enabled library's new analysis.
	input.Profile.AutoPublishIntros = true
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
			optionsRaw, err := json.Marshal(profile.IntroSkipper)
			if err != nil {
				return fmt.Errorf("%w: invalid Intro Skipper options", ErrInvalidInput)
			}
			result, err = scanAnalysisConfiguration(tx.QueryRow(`UPDATE analysis_settings SET
				revision=revision+1,auto_publish_intros=$2,preview_interval_seconds=$3,preview_quality=$4,
				max_source_bytes=$5,max_item_runtime_seconds=$6,feature_cache_max_bytes=$7,intro_skipper_options=$8,updated_at=clock_timestamp()
				WHERE id=1 AND revision=$1 RETURNING `+analysisConfigurationColumns,
				revision, profile.AutoPublishIntros, profile.PreviewIntervalSeconds, profile.PreviewQuality,
				profile.MaxSourceBytes, profile.MaxItemRuntimeSeconds, profile.FeatureCacheMaxBytes, optionsRaw))
			if err != nil {
				return err
			}
			// Keep detection evidence and administrator decisions. Only references
			// whose authority depended on the previous profile are withdrawn.
			if _, err := tx.Exec(`UPDATE analysis_detections SET auto_published=false WHERE auto_published`); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE analysis_credits_detections SET auto_published=false WHERE auto_published`); err != nil {
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
			if err := requestAnalysisProfileRebuild(tx); err != nil {
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

func requestAnalysisProfileRebuild(tx OwnedTx) error {
	var intros, previews, credits bool
	if err := tx.QueryRow(`SELECT
		EXISTS(SELECT 1 FROM libraries WHERE collection_type='tvshows'
			AND COALESCE((options->>'EnableIntroDetection')::boolean,false)),
		EXISTS(SELECT 1 FROM libraries WHERE collection_type IN ('movies','tvshows','mixed')
			AND COALESCE((options->>'EnablePreviewGeneration')::boolean,false)),
		EXISTS(SELECT 1 FROM libraries WHERE collection_type IN ('movies','tvshows','mixed')
			AND COALESCE((options->>'EnableCreditsDetection')::boolean,false))`).Scan(&intros, &previews, &credits); err != nil {
		return err
	}
	for _, request := range []struct {
		enabled bool
		event   systemevents.Event
	}{{intros, systemevents.IntroAnalysisRequested}, {previews, systemevents.PreviewGenerationRequested}, {credits, systemevents.CreditsAnalysisRequested}} {
		if request.enabled {
			if err := systemevents.Record(tx.Exec, request.event); err != nil {
				return err
			}
		}
	}
	return nil
}
