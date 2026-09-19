package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type PlaybackPreferences struct {
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
	ResumeTicks         int64
}

// Include active sidecar identity and bytes in the non-authority invalidation
// stamp. Reusing an external index after replacing its file must not silently
// inherit a remembered selection from the previous caption asset.
const playbackSelectionStampSQL = `md5(COALESCE(i.media::text,'null') || COALESCE((
	SELECT jsonb_agg(jsonb_build_object('Index',s.stream_index,'Root',s.root_id,'Codec',s.codec,
		'Hash',s.source_hash,'Identity',s.file_identity,'Size',s.file_size,'Modified',s.modified_at,'Changed',s.change_time_ns)
		ORDER BY s.stream_index)::text FROM item_subtitles s
	WHERE s.item_id=i.id AND s.root_id=i.root_id AND s.active),'[]'))`

// GetPlaybackPreferencesFor reads only the currently visible item and current
// media snapshot. The stamp is an invalidation key, not an authorization token.
func (s *Store) GetPlaybackPreferencesFor(ctx context.Context, subject Subject, itemID, sourceID string) (PlaybackPreferences, error) {
	if subject.UserID == "" {
		return PlaybackPreferences{}, nil
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return PlaybackPreferences{}, err
	}
	defer rollback(tx)
	var result PlaybackPreferences
	var storedSource, storedStamp, currentStamp string
	err = tx.QueryRow(ctx, `SELECT COALESCE(d.playback_position_ticks,0),COALESCE(d.remembered_media_source_id,''),
		COALESCE(d.remembered_media_stamp,''),`+playbackSelectionStampSQL+`,d.remembered_audio_stream_index,d.remembered_subtitle_stream_index
		FROM items i LEFT JOIN user_item_data d ON d.item_id=i.id AND d.user_id=$1
		WHERE i.id=$2 AND `+access.directSQL("i"), subject.UserID, itemID).
		Scan(&result.ResumeTicks, &storedSource, &storedStamp, &currentStamp, &result.AudioStreamIndex, &result.SubtitleStreamIndex)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaybackPreferences{}, ErrNotFound
	}
	if err != nil {
		return PlaybackPreferences{}, err
	}
	if sourceID != storedSource || currentStamp == "" || currentStamp != storedStamp {
		result.AudioStreamIndex, result.SubtitleStreamIndex = nil, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return PlaybackPreferences{}, err
	}
	return result, nil
}

// rememberPlaybackSelections runs inside the already authorized playback
// transaction. It writes only selection state, never account configuration.
// Invalid/obsolete display hints stay hints; they cannot become future defaults.
func rememberPlaybackSelections(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, itemID, sourceID string, update *PlayerStateUpdate) error {
	if owner.ApplicationKey || update == nil || update.AudioStreamIndex == nil && update.SubtitleStreamIndex == nil {
		return nil
	}
	var configuration, policy, rawMedia json.RawMessage
	var stamp string
	if err := tx.QueryRow(ctx, `SELECT u.configuration,u.policy,i.media,`+playbackSelectionStampSQL+`
		FROM users u JOIN items i ON i.id=$2 WHERE u.id=$1`, owner.UserID, itemID).
		Scan(&configuration, &policy, &rawMedia, &stamp); err != nil {
		return err
	}
	permissions, err := identity.ParseRuntimePolicy(policy)
	if err != nil || !permissions.EnableUserPreferenceAccess || !permissions.AllowsFeature(identity.FeaturePreferences) {
		return nil
	}
	preferences := identity.ProjectUserConfiguration(configuration)
	var info media.Info
	if stamp == "" || json.Unmarshal(rawMedia, &info) != nil {
		return nil
	}
	items := []Item{{ID: itemID, Media: &info}}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return err
	}
	validIndex := func(index int, kind string) bool {
		if kind == "subtitle" && index == -1 {
			return true
		}
		for _, stream := range info.Streams {
			if stream.Index == index && stream.CodecType == kind {
				return true
			}
		}
		if kind == "subtitle" {
			for _, stream := range items[0].Subtitles {
				if stream.Index == index {
					return true
				}
			}
		}
		return false
	}
	setAudio := preferences.RememberAudioSelections && update.AudioStreamIndex != nil && validIndex(*update.AudioStreamIndex, "audio")
	setSubtitle := preferences.RememberSubtitleSelections && update.SubtitleStreamIndex != nil && validIndex(*update.SubtitleStreamIndex, "subtitle")
	if !setAudio && !setSubtitle {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE user_item_data SET
		remembered_audio_stream_index=CASE WHEN $5 THEN $6::integer WHEN remembered_media_source_id=$3 AND remembered_media_stamp=$4 THEN remembered_audio_stream_index ELSE NULL END,
		remembered_subtitle_stream_index=CASE WHEN $7 THEN $8::integer WHEN remembered_media_source_id=$3 AND remembered_media_stamp=$4 THEN remembered_subtitle_stream_index ELSE NULL END,
		remembered_media_source_id=$3,remembered_media_stamp=$4 WHERE user_id=$1 AND item_id=$2`,
		owner.UserID, itemID, sourceID, stamp, setAudio, update.AudioStreamIndex, setSubtitle, update.SubtitleStreamIndex)
	if err != nil {
		return fmt.Errorf("remember validated playback selections: %w", err)
	}
	return nil
}
