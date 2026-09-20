package library

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

const (
	analysisPreviewMaxFrames               = 4096
	analysisPreviewTimelinePairBytes       = 16
	analysisPreviewMaxTimelineBytes        = analysisPreviewMaxFrames * analysisPreviewTimelinePairBytes
	analysisPreviewMaxBytes          int64 = 128 << 20
)

func analysisPreviewHex(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	var digest [32]byte
	_, err := hex.Decode(digest[:], []byte(value))
	return err == nil
}

func validAnalysisPreviewWidth(width int) bool { return width == 240 || width == 320 || width == 400 }

// EffectiveAnalysisPreviewInterval keeps a complete timeline within 4096 frames.
// The adaptive interval is an integral number of seconds, never a partial set.
func EffectiveAnalysisPreviewInterval(durationTicks int64, configuredSeconds int) (int64, error) {
	if durationTicks <= 0 || durationTicks > media.MaxAnalysisDurationTicks || configuredSeconds < 2 || configuredSeconds > 120 {
		return 0, fmt.Errorf("%w: invalid preview interval input", ErrInvalidInput)
	}
	minimumSeconds := (durationTicks-1)/(analysisPreviewMaxFrames*media.TicksPerSecond) + 1
	return max(int64(configuredSeconds), minimumSeconds) * media.TicksPerSecond, nil
}

func validateAnalysisPreviewTimeline(nominal, actual []int64) error {
	if len(nominal) < 1 || len(nominal) > analysisPreviewMaxFrames || len(nominal) != len(actual) || nominal[0] != 0 {
		return fmt.Errorf("%w: invalid analysis preview timeline count", ErrInvalidInput)
	}
	for index := range nominal {
		if nominal[index] < 0 || nominal[index] >= media.MaxAnalysisDurationTicks || actual[index] < 0 || actual[index] >= media.MaxAnalysisDurationTicks ||
			index > 0 && (nominal[index] <= nominal[index-1] || actual[index] < actual[index-1]) {
			return fmt.Errorf("%w: invalid analysis preview timestamp", ErrInvalidInput)
		}
	}
	return nil
}

// EncodeAnalysisPreviewTimeline stores exact nominal/actual int64 tick pairs.
// The containing preview row binds the interval, frame count and source duration.
func EncodeAnalysisPreviewTimeline(nominal, actual []int64) ([]byte, error) {
	if err := validateAnalysisPreviewTimeline(nominal, actual); err != nil {
		return nil, err
	}
	result := make([]byte, len(nominal)*analysisPreviewTimelinePairBytes)
	for index := range nominal {
		offset := index * analysisPreviewTimelinePairBytes
		binary.LittleEndian.PutUint64(result[offset:offset+8], uint64(nominal[index]))
		binary.LittleEndian.PutUint64(result[offset+8:offset+16], uint64(actual[index]))
	}
	return result, nil
}

// DecodeAnalysisPreviewTimeline rejects partial pairs and bounds allocation
// before reading attacker-controlled archival or cached bytes.
func DecodeAnalysisPreviewTimeline(payload []byte) ([]int64, []int64, error) {
	if len(payload) == 0 || len(payload) > analysisPreviewMaxTimelineBytes || len(payload)%analysisPreviewTimelinePairBytes != 0 {
		return nil, nil, fmt.Errorf("%w: invalid analysis preview timeline size", ErrInvalidInput)
	}
	count := len(payload) / analysisPreviewTimelinePairBytes
	nominal, actual := make([]int64, count), make([]int64, count)
	for index := range nominal {
		offset := index * analysisPreviewTimelinePairBytes
		nominal[index] = int64(binary.LittleEndian.Uint64(payload[offset : offset+8]))
		actual[index] = int64(binary.LittleEndian.Uint64(payload[offset+8 : offset+16]))
	}
	if err := validateAnalysisPreviewTimeline(nominal, actual); err != nil {
		return nil, nil, err
	}
	return nominal, actual, nil
}

func validateAnalysisPreviewContent(value AnalysisPreview, duration int64) error {
	if !metadataIdentifier(value.ItemID) || duration <= 0 || duration > media.MaxAnalysisDurationTicks ||
		!validAnalysisPreviewWidth(value.Width) || value.Height <= 0 || value.Height > 2048 ||
		!analysisPreviewHex(value.CacheKey) || !analysisPreviewHex(value.Seal) || !analysisPreviewHex(value.SHA256) ||
		value.Bytes < 1 || value.Bytes > analysisPreviewMaxBytes || value.FrameCount < 1 || value.FrameCount > analysisPreviewMaxFrames ||
		value.IntervalTicks < 2*media.TicksPerSecond || value.IntervalTicks > 120*media.TicksPerSecond || value.IntervalTicks%media.TicksPerSecond != 0 ||
		int64(value.FrameCount) != (duration-1)/value.IntervalTicks+1 ||
		len(value.NominalTicks) != value.FrameCount || len(value.ActualTicks) != value.FrameCount {
		return fmt.Errorf("%w: invalid analysis preview metadata", ErrInvalidInput)
	}
	// A BIF requires its 64-byte header, N+1 eight-byte index entries and at
	// least the four SOI/EOI marker bytes for each JPEG. This necessary bound
	// does not replace verification of the actual sealed BIF and image bytes.
	if value.Bytes < 72+12*int64(value.FrameCount) {
		return fmt.Errorf("%w: preview size cannot contain its declared frames", ErrInvalidInput)
	}
	interval, err := EffectiveAnalysisPreviewInterval(duration, int(value.IntervalTicks/media.TicksPerSecond))
	if err != nil || interval != value.IntervalTicks {
		return fmt.Errorf("%w: incomplete analysis preview interval", ErrInvalidInput)
	}
	if err := validateAnalysisPreviewTimeline(value.NominalTicks, value.ActualTicks); err != nil {
		return err
	}
	for index, nominal := range value.NominalTicks {
		if nominal != int64(index)*value.IntervalTicks || nominal >= duration || value.ActualTicks[index] >= duration {
			return fmt.Errorf("%w: preview timeline does not cover the source slots", ErrInvalidInput)
		}
	}
	return nil
}

// ValidateStoredAnalysisPreview validates the complete durable reference without
// opening its derivative. Archives must use the reference's source duration.
func ValidateStoredAnalysisPreview(value AnalysisPreview, duration int64) error {
	if _, err := libraryEditRevision(value.Revision); err != nil {
		return err
	}
	if _, err := libraryEditRevision(value.ProfileRevision); err != nil {
		return err
	}
	if !metadataIdentifier(value.SourceRevision) || !analysisPreviewHex(value.ProfileFingerprint) ||
		value.PublicationEpoch < 1 || value.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: invalid stored analysis preview identity", ErrInvalidInput)
	}
	return validateAnalysisPreviewContent(value, duration)
}

func prepareAnalysisPreviewPublications(work AnalysisWork, values []AnalysisPreviewPublication) (AnalysisSource, []AnalysisPreview, error) {
	var source AnalysisSource
	targets := 0
	for _, candidate := range work.Sources {
		if candidate.Target {
			source = candidate
			targets++
		}
	}
	if work.TaskKey != TaskPreviewGenerationKey || len(work.Sources) != 1 || targets != 1 || len(values) < 1 || len(values) > 3 || len(values) != len(work.Execution.PreviewWidths) ||
		work.PublicationEpoch < 1 || !analysisPreviewHex(work.ConfigurationFingerprint) {
		return AnalysisSource{}, nil, ErrInvalidInput
	}
	if _, err := libraryEditRevision(work.ConfigurationRevision); err != nil {
		return AnalysisSource{}, nil, err
	}
	if err := ValidateAnalysisProfile(work.Profile); err != nil {
		return AnalysisSource{}, nil, err
	}
	interval, err := EffectiveAnalysisPreviewInterval(source.DurationTicks, work.Profile.PreviewIntervalSeconds)
	if err != nil {
		return AnalysisSource{}, nil, err
	}
	expected := make(map[int]bool, len(work.Execution.PreviewWidths))
	for _, width := range work.Execution.PreviewWidths {
		if !validAnalysisPreviewWidth(width) || expected[width] {
			return AnalysisSource{}, nil, ErrInvalidInput
		}
		expected[width] = true
	}
	result := make([]AnalysisPreview, 0, len(values))
	for _, value := range values {
		if !expected[value.Width] || value.ItemID != source.ItemID || value.IntervalTicks != interval {
			return AnalysisSource{}, nil, ErrInvalidInput
		}
		delete(expected, value.Width)
		preview := AnalysisPreview{ItemID: value.ItemID, SourceRevision: source.SourceRevision,
			ProfileFingerprint: work.ConfigurationFingerprint, ProfileRevision: work.ConfigurationRevision, PublicationEpoch: work.PublicationEpoch,
			CacheKey: value.CacheKey, Seal: value.Seal, Width: value.Width, Height: value.Height, SHA256: value.SHA256,
			Bytes: value.Bytes, FrameCount: value.FrameCount, IntervalTicks: value.IntervalTicks,
			NominalTicks: value.NominalTicks, ActualTicks: value.ActualTicks}
		if err := validateAnalysisPreviewContent(preview, source.DurationTicks); err != nil {
			return AnalysisSource{}, nil, err
		}
		preview.NominalTicks = append([]int64(nil), value.NominalTicks...)
		preview.ActualTicks = append([]int64(nil), value.ActualTicks...)
		result = append(result, preview)
	}
	return source, result, nil
}

// PublishAnalysisPreview publishes all admitted width variants atomically after
// source-byte revalidation. Bytes remain in the caller's owned derivative cache.
func (s *Store) PublishAnalysisPreview(ctx context.Context, childID string, fence AnalysisFence, values []AnalysisPreviewPublication) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if !metadataIdentifier(childID) || len(values) < 1 || len(values) > 3 {
		return ErrInvalidInput
	}
	if _, err := s.RevalidateAnalysisWork(ctx, childID, fence); err != nil {
		return err
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		source, previews, err := prepareAnalysisPreviewPublications(work, values)
		if err != nil {
			return err
		}
		for _, preview := range previews {
			timeline, err := EncodeAnalysisPreviewTimeline(preview.NominalTicks, preview.ActualTicks)
			if err != nil {
				return err
			}
			profileRevision, _ := libraryEditRevision(preview.ProfileRevision)
			tag, err := tx.Exec(`INSERT INTO analysis_previews
				(item_id,width,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,
				cache_key,seal,height,content_sha256,bytes,frame_count,interval_ticks,timeline,updated_at)
				VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,clock_timestamp())
				ON CONFLICT(item_id,width) DO UPDATE SET revision=analysis_previews.revision+1,
				source_revision=EXCLUDED.source_revision,profile_fingerprint=EXCLUDED.profile_fingerprint,
				profile_revision=EXCLUDED.profile_revision,publication_epoch=EXCLUDED.publication_epoch,child_id=EXCLUDED.child_id,
				cache_key=EXCLUDED.cache_key,seal=EXCLUDED.seal,height=EXCLUDED.height,content_sha256=EXCLUDED.content_sha256,
				bytes=EXCLUDED.bytes,frame_count=EXCLUDED.frame_count,interval_ticks=EXCLUDED.interval_ticks,
				timeline=EXCLUDED.timeline,updated_at=EXCLUDED.updated_at WHERE analysis_previews.revision<$16`,
				preview.ItemID, preview.Width, preview.SourceRevision, preview.ProfileFingerprint, profileRevision,
				preview.PublicationEpoch, childID, preview.CacheKey, preview.Seal, preview.Height, preview.SHA256,
				preview.Bytes, preview.FrameCount, preview.IntervalTicks, timeline, int64(math.MaxInt64))
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrAnalysisConflict
			}
		}
		if _, err := tx.Exec(`DELETE FROM analysis_previews WHERE item_id=$1 AND NOT(width=ANY($2::integer[]))`, source.ItemID, work.Execution.PreviewWidths); err != nil {
			return err
		}
		return analysisRecordChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: source.ItemID, LibraryID: source.LibraryID})
	})
}

const analysisPreviewColumns = `p.item_id,p.revision,p.source_revision,p.profile_fingerprint,p.profile_revision,p.publication_epoch,
	p.cache_key,p.seal,p.width,p.height,p.content_sha256,p.bytes,p.frame_count,p.interval_ticks,
	CASE WHEN octet_length(p.timeline)<=65536 THEN p.timeline ELSE NULL END,p.updated_at`

func scanAnalysisPreview(row rowScanner, duration int64) (AnalysisPreview, error) {
	var result AnalysisPreview
	var revision, profileRevision int64
	var timeline []byte
	if err := row.Scan(&result.ItemID, &revision, &result.SourceRevision, &result.ProfileFingerprint, &profileRevision, &result.PublicationEpoch,
		&result.CacheKey, &result.Seal, &result.Width, &result.Height, &result.SHA256, &result.Bytes, &result.FrameCount, &result.IntervalTicks,
		&timeline, &result.UpdatedAt); err != nil {
		return AnalysisPreview{}, err
	}
	result.Revision, result.ProfileRevision = strconv.FormatInt(revision, 10), strconv.FormatInt(profileRevision, 10)
	var err error
	result.NominalTicks, result.ActualTicks, err = DecodeAnalysisPreviewTimeline(timeline)
	if err != nil || ValidateStoredAnalysisPreview(result, duration) != nil {
		return AnalysisPreview{}, fmt.Errorf("%w: invalid stored analysis preview reference", ErrUnavailable)
	}
	return result, nil
}

// GetAnalysisWorkPreviews returns a complete reusable variant set only for the
// currently fenced task. The runtime separately verifies the sealed cache bytes
// and revalidates the actual source before skipping extraction.
func (s *Store) GetAnalysisWorkPreviews(ctx context.Context, childID string, fence AnalysisFence) ([]AnalysisPreview, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	if !metadataIdentifier(childID) {
		return nil, ErrInvalidInput
	}
	result := []AnalysisPreview{}
	err := s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		var source AnalysisSource
		targets := 0
		for _, candidate := range work.Sources {
			if candidate.Target {
				source = candidate
				targets++
			}
		}
		if work.TaskKey != TaskPreviewGenerationKey || len(work.Sources) != 1 || targets != 1 {
			return ErrInvalidInput
		}
		if work.Force {
			return nil
		}
		if len(work.Execution.PreviewWidths) < 1 || len(work.Execution.PreviewWidths) > 3 {
			return ErrInvalidInput
		}
		expected := make(map[int]bool, len(work.Execution.PreviewWidths))
		for _, width := range work.Execution.PreviewWidths {
			if !validAnalysisPreviewWidth(width) || expected[width] {
				return ErrInvalidInput
			}
			expected[width] = true
		}
		interval, err := EffectiveAnalysisPreviewInterval(source.DurationTicks, work.Profile.PreviewIntervalSeconds)
		if err != nil {
			return err
		}
		revision, err := libraryEditRevision(work.ConfigurationRevision)
		if err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT `+analysisPreviewColumns+` FROM analysis_previews p
			WHERE p.item_id=$1 AND p.source_revision=$2 AND p.profile_fingerprint=$3
			AND p.profile_revision=$4 AND p.publication_epoch=$5 ORDER BY p.width LIMIT 4`,
			source.ItemID, source.SourceRevision, work.ConfigurationFingerprint, revision, work.PublicationEpoch)
		if err != nil {
			return err
		}
		previews := make([]AnalysisPreview, 0, len(expected))
		for rows.Next() {
			preview, err := scanAnalysisPreview(rows, source.DurationTicks)
			if err != nil {
				rows.Close()
				return err
			}
			if !expected[preview.Width] || preview.IntervalTicks != interval {
				rows.Close()
				return fmt.Errorf("%w: stored preview differs from its execution profile", ErrUnavailable)
			}
			delete(expected, preview.Width)
			previews = append(previews, preview)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(expected) == 0 {
			result = previews
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ClearAnalysisPreviews withdraws references and advances a persistent item
// tombstone, fencing work admitted before this administrator action.
func (s *Store) ClearAnalysisPreviews(ctx context.Context, actor identity.Principal, itemID, expectedSource string) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if !metadataIdentifier(itemID) || !metadataIdentifier(expectedSource) {
		return ErrInvalidInput
	}
	opened, err := s.analysisCurrentSourceAsAdministrator(ctx, actor, itemID)
	if err != nil {
		return err
	}
	if opened.SourceRevision != expectedSource {
		return ErrAnalysisSourceChanged
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		current, err := readAnalysisSource(tx, itemID, true)
		if err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		if current.SourceRevision != expectedSource || current.MediaSourceID != opened.MediaSourceID {
			return ErrAnalysisSourceChanged
		}
		tag, err := tx.Exec(`INSERT INTO analysis_preview_state(item_id,revision) VALUES($1,1)
			ON CONFLICT(item_id) DO UPDATE SET revision=analysis_preview_state.revision+1
			WHERE analysis_preview_state.revision<$2`, itemID, int64(math.MaxInt64))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrAnalysisConflict
		}
		if _, err := tx.Exec(`DELETE FROM analysis_previews WHERE item_id=$1`, itemID); err != nil {
			return err
		}
		if err := analysisRecordChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: current.LibraryID}); err != nil {
			return err
		}
		if err := analysisContext(ctx); err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
}

func (s *Store) GetAnalysisPreviewsFor(ctx context.Context, subject Subject, itemID, sourceID string) ([]AnalysisPreview, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	opened, err := s.analysisCurrentSourceFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return nil, err
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if !access.canPlay {
		return nil, ErrForbidden
	}
	current, err := readAnalysisSourceUsing(ctx, tx, access, itemID, false)
	if err != nil {
		return nil, err
	}
	if current.SourceRevision != opened.SourceRevision || current.MediaSourceID != opened.MediaSourceID {
		return nil, ErrAnalysisSourceChanged
	}
	rows, err := tx.Query(ctx, `SELECT `+analysisPreviewColumns+` FROM analysis_previews p CROSS JOIN analysis_settings settings
		WHERE settings.id=1 AND p.item_id=$1 AND p.source_revision=$2 AND p.profile_revision=settings.revision
		AND p.publication_epoch=settings.publication_epoch ORDER BY p.width LIMIT 4`, itemID, current.SourceRevision)
	if err != nil {
		return nil, err
	}
	result := make([]AnalysisPreview, 0, 3)
	for rows.Next() {
		preview, err := scanAnalysisPreview(rows, current.DurationTicks)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, preview)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(result) > 3 {
		return nil, ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// CurrentAnalysisPreviewCacheKeys returns the complete currently effective set.
// The cache owner serializes pruning with publication and consumes no partial
// list on a database or authorization failure.
func (s *Store) CurrentAnalysisPreviewCacheKeys(ctx context.Context, actor identity.Principal) ([]string, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	if err := administrator.check(ctx, tx, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT p.cache_key FROM analysis_previews p JOIN items i ON i.id=p.item_id
		JOIN library_roots root ON root.id=i.root_id AND root.library_id=i.library_id CROSS JOIN analysis_settings settings
		WHERE settings.id=1 AND p.profile_revision=settings.revision AND p.publication_epoch=settings.publication_epoch
		AND p.source_revision=`+introSourceRevisionSQL+` AND `+analysisPhysicalSQL+` ORDER BY p.cache_key`)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		if !analysisPreviewHex(key) {
			rows.Close()
			return nil, ErrUnavailable
		}
		result = append(result, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
