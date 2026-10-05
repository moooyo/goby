package library

import (
	"context"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
)

const backgroundConfigurationColumns = `revision::text,duration_seconds,max_width,video_bitrate,max_item_runtime_seconds,updated_at`

func readBackgroundPreviewConfiguration(row rowScanner) (BackgroundPreviewConfiguration, error) {
	var value BackgroundPreviewConfiguration
	err := row.Scan(&value.Revision, &value.Profile.DurationSeconds, &value.Profile.MaxWidth, &value.Profile.VideoBitrate, &value.Profile.MaxItemRuntimeSeconds, &value.UpdatedAt)
	if err != nil {
		return value, err
	}
	if ValidateBackgroundPreviewProfile(value.Profile) != nil {
		return value, ErrUnavailable
	}
	value.Defaults = DefaultBackgroundPreviewProfile()
	return value, nil
}

func (s *Store) GetBackgroundPreviewConfiguration(ctx context.Context, actor identity.Principal) (BackgroundPreviewConfiguration, error) {
	var result BackgroundPreviewConfiguration
	err := s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		var err error
		result, err = readBackgroundPreviewConfiguration(tx.QueryRow(`SELECT ` + backgroundConfigurationColumns + ` FROM background_preview_settings WHERE id=1`))
		return err
	})
	return result, err
}

func (s *Store) UpdateBackgroundPreviewConfiguration(ctx context.Context, actor identity.Principal, input BackgroundPreviewConfigurationUpdate) (BackgroundPreviewConfiguration, error) {
	if _, err := libraryEditRevision(input.Revision); err != nil {
		return BackgroundPreviewConfiguration{}, err
	}
	if err := ValidateBackgroundPreviewProfile(input.Profile); err != nil {
		return BackgroundPreviewConfiguration{}, err
	}
	var result BackgroundPreviewConfiguration
	err := s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		current, err := readBackgroundPreviewConfiguration(tx.QueryRow(`SELECT ` + backgroundConfigurationColumns + ` FROM background_preview_settings WHERE id=1 FOR UPDATE`))
		if err != nil {
			return err
		}
		if current.Revision != input.Revision {
			return ErrBackgroundPreviewConflict
		}
		result = current
		if current.Profile == input.Profile {
			return nil
		}
		revision, _ := strconv.ParseInt(current.Revision, 10, 64)
		if revision == int64(^uint64(0)>>1) {
			return ErrBackgroundPreviewConflict
		}
		result, err = readBackgroundPreviewConfiguration(tx.QueryRow(`UPDATE background_preview_settings SET revision=revision+1,duration_seconds=$1,
   max_width=$2,video_bitrate=$3,max_item_runtime_seconds=$4,updated_at=clock_timestamp() WHERE id=1 RETURNING `+backgroundConfigurationColumns,
			input.Profile.DurationSeconds, input.Profile.MaxWidth, input.Profile.VideoBitrate, input.Profile.MaxItemRuntimeSeconds))
		// Configuration affects new work only. Existing files and completed queue
		// entries deliberately remain untouched, with no regeneration event.
		return err
	})
	return result, err
}

func (s *Store) withBackgroundAdministrator(ctx context.Context, actor identity.Principal, callback func(OwnedTx) error) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		if err := callback(tx); err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
}
