package backuppg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

func analysisStateIdentifier(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func analysisStateDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	var digest [32]byte
	_, err := hex.Decode(digest[:], []byte(value))
	return err == nil
}

func decodeAnalysisStateSelection(raw []byte) (library.AnalysisSelection, bool) {
	var input library.AnalysisSelection
	if len(raw) == 0 || len(raw) > 65536 {
		return input, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return input, false
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, false
		}
		switch key {
		case "LibraryIds", "ItemIds", "Force":
		default:
			return input, false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return input, false
	}
	normalized, err := library.NormalizeAnalysisSelection(input)
	if err != nil || !slices.Equal(input.LibraryIDs, normalized.LibraryIDs) || !slices.Equal(input.ItemIDs, normalized.ItemIDs) {
		return input, false
	}
	return normalized, true
}

type analysisStateActor struct {
	source, kind, user, session, client, peer string
	application                               int64
}

func validAnalysisStateActor(actor analysisStateActor, analysis bool) bool {
	if actor.application < 0 || !analysisStateIdentifier(actor.client, 256, true) || !analysisStateIdentifier(actor.peer, 256, true) {
		return false
	}
	if actor.peer != "" {
		addr, err := netip.ParseAddr(actor.peer)
		if err != nil || addr.Zone() != "" || addr.Is4In6() || addr.String() != actor.peer {
			return false
		}
	}
	if !analysis {
		return true
	}
	if !analysisStateIdentifier(actor.user, 256, true) || !analysisStateIdentifier(actor.session, 256, true) {
		return false
	}
	switch actor.source {
	case "manual":
		return actor.kind == "admin" && actor.user != "" && actor.session != "" && actor.application == 0 && actor.client == ""
	case "compatibility":
		return actor.session != "" && (actor.kind == "emby" && actor.user != "" && actor.application == 0 && actor.client == "" || actor.kind == "application_key" && actor.user == "" && actor.application > 0 && actor.client != "")
	case "schedule", "startup", "system_event":
		return actor.kind == "system" && actor.user == "" && actor.session == "" && actor.application == 0 && actor.client == "" && actor.peer == ""
	}
	return false
}

func validateAnalysisTaskState(ctx context.Context, tx pgx.Tx) error {
	return analysisStateRows(ctx, tx, `SELECT task_key,analysis_input,analysis_config_fingerprint,source,actor_kind,
		actor_user_id,actor_session_id,actor_application_key_id,actor_client_session_id,actor_peer_ip FROM task_runs ORDER BY id`, func(rows pgx.Rows) error {
		var key, fingerprint string
		var raw []byte
		var actor analysisStateActor
		if err := rows.Scan(&key, &raw, &fingerprint, &actor.source, &actor.kind, &actor.user, &actor.session, &actor.application, &actor.client, &actor.peer); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		analysis := key == library.TaskIntroAnalysisKey || key == library.TaskPreviewGenerationKey
		if !validAnalysisStateActor(actor, analysis) {
			return ErrSchema
		}
		if !analysis {
			if raw != nil || fingerprint != "" {
				return ErrSchema
			}
			return nil
		}
		if _, ok := decodeAnalysisStateSelection(raw); !ok || !analysisStateDigest(fingerprint) {
			return ErrSchema
		}
		return nil
	})
}

func validateAnalysisAdmissionState(ctx context.Context, tx pgx.Tx) error {
	if err := analysisStateRows(ctx, tx, `SELECT profile,execution,configuration_revision,publication_epoch,fingerprint FROM analysis_run_profiles ORDER BY run_id`, func(rows pgx.Rows) error {
		var profile, execution []byte
		var revision, epoch int64
		var fingerprint string
		if err := rows.Scan(&profile, &execution, &revision, &epoch, &fingerprint); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if library.ValidateStoredAnalysisAdmission(profile, execution, revision, epoch, fingerprint) != nil {
			return ErrSchema
		}
		return nil
	}); err != nil {
		return err
	}
	return analysisStateRows(ctx, tx, `SELECT item_id,library_id,root_id,series_id,season_id,episode_key,item_type,
		source_revision,hierarchy_revision FROM analysis_work_sources ORDER BY child_id,position`, func(rows pgx.Rows) error {
		var item, lib, root, series, season, episode, itemType, source, hierarchy string
		if err := rows.Scan(&item, &lib, &root, &series, &season, &episode, &itemType, &source, &hierarchy); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		for _, value := range []string{item, lib, root, source, hierarchy} {
			if !analysisStateIdentifier(value, 256, false) {
				return ErrSchema
			}
		}
		if !analysisStateIdentifier(series, 256, true) || !analysisStateIdentifier(season, 256, true) || !analysisStateIdentifier(episode, 512, true) {
			return ErrSchema
		}
		// Unsupported hierarchy is an explicit retained admission outcome. Its
		// missing episode key must not become fabricated source authority.
		return nil
	})
}

func analysisStateFeatureKey(item, source, profile string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("goby.analysis.feature-cache.v1"))
	for _, value := range []string{item, source, profile} {
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func validateAnalysisFeatureState(ctx context.Context, tx pgx.Tx) error {
	return analysisStateRows(ctx, tx, `SELECT cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,
		duration_ticks,CASE WHEN octet_length(payload)<=262144 THEN payload END,bytes FROM analysis_feature_cache ORDER BY cache_key`, func(rows pgx.Rows) error {
		var key, item, source, profile, content, algorithm string
		var duration, size int64
		var payload []byte
		if err := rows.Scan(&key, &item, &source, &profile, &content, &algorithm, &duration, &payload, &size); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if !analysisStateIdentifier(item, 256, false) || !analysisStateIdentifier(source, 256, false) || !analysisStateDigest(profile) || key != analysisStateFeatureKey(item, source, profile) || size != int64(len(payload)) {
			return ErrSchema
		}
		decoded, err := library.DecodeAnalysisFeatures(payload, duration)
		if err != nil || decoded.ContentSHA256 != content || decoded.AlgorithmProfile != algorithm {
			return ErrSchema
		}
		return nil
	})
}

func validateAnalysisPreviewState(ctx context.Context, tx pgx.Tx) error {
	return analysisStateRows(ctx, tx, `SELECT preview.item_id,preview.revision,preview.source_revision,preview.profile_fingerprint,preview.profile_revision,
		preview.publication_epoch,preview.cache_key,preview.seal,preview.width,preview.height,preview.content_sha256,preview.bytes,
		preview.frame_count,preview.interval_ticks,CASE WHEN octet_length(preview.timeline)<=65536 THEN preview.timeline END,
		preview.updated_at,source.duration_ticks,(profile.profile->>'PreviewIntervalSeconds')::integer FROM analysis_previews preview
		JOIN analysis_work_sources source ON source.child_id=preview.child_id AND source.item_id=preview.item_id
		JOIN analysis_work work ON work.child_id=preview.child_id JOIN analysis_run_profiles profile ON profile.run_id=work.run_id
		ORDER BY preview.item_id,preview.width`, func(rows pgx.Rows) error {
		var value library.AnalysisPreview
		var revision, profileRevision, duration int64
		var configuredInterval int
		var timeline []byte
		if err := rows.Scan(&value.ItemID, &revision, &value.SourceRevision, &value.ProfileFingerprint, &profileRevision, &value.PublicationEpoch,
			&value.CacheKey, &value.Seal, &value.Width, &value.Height, &value.SHA256, &value.Bytes, &value.FrameCount, &value.IntervalTicks, &timeline, &value.UpdatedAt, &duration, &configuredInterval); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		value.Revision, value.ProfileRevision = strconv.FormatInt(revision, 10), strconv.FormatInt(profileRevision, 10)
		var err error
		value.NominalTicks, value.ActualTicks, err = library.DecodeAnalysisPreviewTimeline(timeline)
		expectedInterval, intervalErr := library.EffectiveAnalysisPreviewInterval(duration, configuredInterval)
		if err != nil || intervalErr != nil || expectedInterval != value.IntervalTicks || library.ValidateStoredAnalysisPreview(value, duration) != nil {
			return ErrSchema
		}
		return nil
	})
}
