package library

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

const (
	analysisFeaturesMagic           = "GAFB"
	analysisFeaturesVersion         = 3
	analysisFeaturesVersionV1       = 1
	analysisFeaturesVersionV2       = 2
	analysisFeaturesHeaderSizeV1V2  = 56
	analysisFeaturesHeaderSize      = 60
	analysisFeaturesAudioSize       = 20
	analysisFeaturesVisualSizeV1    = 18
	analysisFeaturesVisualSize      = 83
	analysisFeaturesRefinementSize  = 264
	analysisFeaturesMaxPayloadV1V2  = 256 << 10
	analysisFeaturesMaxPayloadBytes = 512 << 10
	analysisFeaturesMaxProfileBytes = 512
	analysisFeaturesMaxAudio        = 8192
	analysisFeaturesMaxVisual       = 4096
	analysisFeaturesMaxRefinement   = 1200
)

// EncodeAnalysisFeatures encodes a bounded feature snapshot for the supplied
// source duration. Version 3 appends a refinement count to the version 2 header,
// then stores the UTF-8 profile, audio bins, visual samples, and refinement
// samples in that order, with little-endian numeric fields. Refinement records
// contain an original source-relative timestamp and 256 raw grayscale bytes.
func EncodeAnalysisFeatures(value AnalysisFeatures, sourceDuration int64) ([]byte, error) {
	window, err := analysisFeaturesWindow(sourceDuration)
	if err != nil {
		return nil, err
	}
	if err := validateAnalysisFeaturesMetadata(value.AlgorithmProfile, value.AudioBoundaryUncertaintyTicks); err != nil {
		return nil, err
	}
	if len(value.ContentSHA256) != 64 || strings.ToLower(value.ContentSHA256) != value.ContentSHA256 {
		return nil, fmt.Errorf("%w: analysis content hash must be 64 lowercase hexadecimal characters", ErrInvalidInput)
	}
	var digest [32]byte
	if _, err := hex.Decode(digest[:], []byte(value.ContentSHA256)); err != nil {
		return nil, fmt.Errorf("%w: invalid analysis content hash", ErrInvalidInput)
	}
	if len(value.Audio) > analysisFeaturesMaxAudio || len(value.Visual) > analysisFeaturesMaxVisual || len(value.Refinement) > analysisFeaturesMaxRefinement {
		return nil, fmt.Errorf("%w: analysis feature counts exceed their limits", ErrInvalidInput)
	}
	size := analysisFeaturesHeaderSize + len(value.AlgorithmProfile) +
		len(value.Audio)*analysisFeaturesAudioSize + len(value.Visual)*analysisFeaturesVisualSize +
		len(value.Refinement)*analysisFeaturesRefinementSize
	if size > analysisFeaturesMaxPayloadBytes {
		return nil, fmt.Errorf("%w: analysis feature payload exceeds its limit", ErrInvalidInput)
	}
	var previousEnd int64
	for index, sample := range value.Audio {
		if !validAnalysisAudioSample(sample, previousEnd, window) {
			return nil, fmt.Errorf("%w: invalid analysis audio sample %d", ErrInvalidInput, index)
		}
		previousEnd = sample.EndTicks
	}
	var previousTick int64 = -1
	for index, sample := range value.Visual {
		if !validAnalysisVisualSample(sample, previousTick, window) {
			return nil, fmt.Errorf("%w: invalid analysis visual sample %d", ErrInvalidInput, index)
		}
		previousTick = sample.Ticks
	}
	previousTick = -1
	for index, sample := range value.Refinement {
		if !validAnalysisRefinementSample(sample, previousTick, min(sourceDuration, 120*introdetect.TicksPerSecond)) {
			return nil, fmt.Errorf("%w: invalid analysis refinement sample %d", ErrInvalidInput, index)
		}
		previousTick = sample.Ticks
	}

	payload := make([]byte, size)
	copy(payload[:4], analysisFeaturesMagic)
	binary.LittleEndian.PutUint16(payload[4:6], analysisFeaturesVersion)
	binary.LittleEndian.PutUint16(payload[6:8], uint16(len(value.AlgorithmProfile)))
	binary.LittleEndian.PutUint32(payload[8:12], uint32(len(value.Audio)))
	binary.LittleEndian.PutUint32(payload[12:16], uint32(len(value.Visual)))
	binary.LittleEndian.PutUint64(payload[16:24], uint64(value.AudioBoundaryUncertaintyTicks))
	copy(payload[24:56], digest[:])
	binary.LittleEndian.PutUint32(payload[56:60], uint32(len(value.Refinement)))
	offset := analysisFeaturesHeaderSize
	copy(payload[offset:], value.AlgorithmProfile)
	offset += len(value.AlgorithmProfile)
	for _, sample := range value.Audio {
		binary.LittleEndian.PutUint64(payload[offset:offset+8], uint64(sample.StartTicks))
		binary.LittleEndian.PutUint64(payload[offset+8:offset+16], uint64(sample.EndTicks))
		binary.LittleEndian.PutUint32(payload[offset+16:offset+20], sample.Fingerprint)
		offset += analysisFeaturesAudioSize
	}
	for _, sample := range value.Visual {
		binary.LittleEndian.PutUint64(payload[offset:offset+8], uint64(sample.Ticks))
		binary.LittleEndian.PutUint64(payload[offset+8:offset+16], sample.Hash)
		binary.LittleEndian.PutUint16(payload[offset+16:offset+18], sample.Contrast)
		if sample.LumaKnown {
			payload[offset+18] = 1
		}
		for index, cell := range sample.Luma {
			payload[offset+19+index] = byte(cell)
		}
		offset += analysisFeaturesVisualSize
	}
	for _, sample := range value.Refinement {
		binary.LittleEndian.PutUint64(payload[offset:offset+8], uint64(sample.Ticks))
		copy(payload[offset+8:offset+analysisFeaturesRefinementSize], sample.Raster[:])
		offset += analysisFeaturesRefinementSize
	}
	return payload, nil
}

// DecodeAnalysisFeatures validates and decodes a complete feature snapshot.
// Missing evidence is represented by nonnil empty audio and visual slices.
// Versions 1 and 2 remain readable without acquiring refinement evidence.
// Reading historical bytes never promotes their extraction profile.
func DecodeAnalysisFeatures(payload []byte, sourceDuration int64) (AnalysisFeatures, error) {
	return readAnalysisFeatures(payload, sourceDuration, true)
}

// ValidateStoredAnalysisFeatures checks a stored snapshot without allocating
// sample slices. Backups must use the duration associated with its source.
func ValidateStoredAnalysisFeatures(payload []byte, sourceDuration int64) error {
	_, err := readAnalysisFeatures(payload, sourceDuration, false)
	return err
}

func readAnalysisFeatures(payload []byte, sourceDuration int64, materialize bool) (AnalysisFeatures, error) {
	window, err := analysisFeaturesWindow(sourceDuration)
	if err != nil {
		return AnalysisFeatures{}, err
	}
	if len(payload) < analysisFeaturesHeaderSizeV1V2 || len(payload) > analysisFeaturesMaxPayloadBytes {
		return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis feature payload size", ErrInvalidInput)
	}
	if string(payload[:4]) != analysisFeaturesMagic {
		return AnalysisFeatures{}, fmt.Errorf("%w: unsupported analysis feature format", ErrInvalidInput)
	}
	version := binary.LittleEndian.Uint16(payload[4:6])
	visualSize := analysisFeaturesVisualSize
	headerSize := analysisFeaturesHeaderSizeV1V2
	maximumPayload := analysisFeaturesMaxPayloadV1V2
	var refinementCount uint32
	switch version {
	case analysisFeaturesVersionV1:
		visualSize = analysisFeaturesVisualSizeV1
	case analysisFeaturesVersionV2:
	case analysisFeaturesVersion:
		headerSize = analysisFeaturesHeaderSize
		maximumPayload = analysisFeaturesMaxPayloadBytes
		if len(payload) < headerSize {
			return AnalysisFeatures{}, fmt.Errorf("%w: incomplete analysis feature header", ErrInvalidInput)
		}
		refinementCount = binary.LittleEndian.Uint32(payload[56:60])
	default:
		return AnalysisFeatures{}, fmt.Errorf("%w: unsupported analysis feature format", ErrInvalidInput)
	}
	if len(payload) > maximumPayload {
		return AnalysisFeatures{}, fmt.Errorf("%w: analysis feature payload exceeds its version limit", ErrInvalidInput)
	}
	profileBytes := binary.LittleEndian.Uint16(payload[6:8])
	audioCount := binary.LittleEndian.Uint32(payload[8:12])
	visualCount := binary.LittleEndian.Uint32(payload[12:16])
	// Bound all declared lengths before converting counts, calculating a size,
	// slicing the profile, or allocating output arrays.
	if profileBytes == 0 || profileBytes > analysisFeaturesMaxProfileBytes ||
		audioCount > analysisFeaturesMaxAudio || visualCount > analysisFeaturesMaxVisual || refinementCount > analysisFeaturesMaxRefinement {
		return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis feature lengths", ErrInvalidInput)
	}
	size := headerSize + int(profileBytes) +
		int(audioCount)*analysisFeaturesAudioSize + int(visualCount)*visualSize +
		int(refinementCount)*analysisFeaturesRefinementSize
	if len(payload) != size {
		return AnalysisFeatures{}, fmt.Errorf("%w: analysis feature payload length does not match its header", ErrInvalidInput)
	}
	uncertainty := int64(binary.LittleEndian.Uint64(payload[16:24]))
	offset := headerSize
	profile := string(payload[offset : offset+int(profileBytes)])
	if err := validateAnalysisFeaturesMetadata(profile, uncertainty); err != nil {
		return AnalysisFeatures{}, err
	}
	offset += int(profileBytes)
	var value AnalysisFeatures
	if materialize {
		value = AnalysisFeatures{
			ContentSHA256:                 hex.EncodeToString(payload[24:56]),
			AlgorithmProfile:              profile,
			AudioBoundaryUncertaintyTicks: uncertainty,
			Audio:                         make([]introdetect.AudioSample, int(audioCount)),
			Visual:                        make([]introdetect.VisualSample, int(visualCount)),
		}
		if refinementCount > 0 {
			value.Refinement = make([]introdetect.RefinementSample, int(refinementCount))
		}
	}
	var previousEnd int64
	for index := 0; index < int(audioCount); index++ {
		sample := introdetect.AudioSample{
			StartTicks:  int64(binary.LittleEndian.Uint64(payload[offset : offset+8])),
			EndTicks:    int64(binary.LittleEndian.Uint64(payload[offset+8 : offset+16])),
			Fingerprint: binary.LittleEndian.Uint32(payload[offset+16 : offset+20]),
		}
		if !validAnalysisAudioSample(sample, previousEnd, window) {
			return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis audio sample %d", ErrInvalidInput, index)
		}
		previousEnd = sample.EndTicks
		if materialize {
			value.Audio[index] = sample
		}
		offset += analysisFeaturesAudioSize
	}
	var previousTick int64 = -1
	for index := 0; index < int(visualCount); index++ {
		sample := introdetect.VisualSample{
			Ticks:    int64(binary.LittleEndian.Uint64(payload[offset : offset+8])),
			Hash:     binary.LittleEndian.Uint64(payload[offset+8 : offset+16]),
			Contrast: binary.LittleEndian.Uint16(payload[offset+16 : offset+18]),
		}
		if version >= analysisFeaturesVersionV2 {
			if payload[offset+18] > 1 {
				return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis luminance flag %d", ErrInvalidInput, index)
			}
			sample.LumaKnown = payload[offset+18] == 1
			for cell := range sample.Luma {
				sample.Luma[cell] = int8(payload[offset+19+cell])
			}
		}
		if !validAnalysisVisualSample(sample, previousTick, window) {
			return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis visual sample %d", ErrInvalidInput, index)
		}
		previousTick = sample.Ticks
		if materialize {
			value.Visual[index] = sample
		}
		offset += visualSize
	}
	previousTick = -1
	refinementWindow := min(sourceDuration, 120*introdetect.TicksPerSecond)
	for index := 0; index < int(refinementCount); index++ {
		sample := introdetect.RefinementSample{Ticks: int64(binary.LittleEndian.Uint64(payload[offset : offset+8]))}
		if !validAnalysisRefinementSample(sample, previousTick, refinementWindow) {
			return AnalysisFeatures{}, fmt.Errorf("%w: invalid analysis refinement sample %d", ErrInvalidInput, index)
		}
		previousTick = sample.Ticks
		if materialize {
			copy(sample.Raster[:], payload[offset+8:offset+analysisFeaturesRefinementSize])
			value.Refinement[index] = sample
		}
		offset += analysisFeaturesRefinementSize
	}
	return value, nil
}

func validAnalysisRefinementSample(sample introdetect.RefinementSample, previousTick, window int64) bool {
	return sample.Ticks >= 0 && sample.Ticks > previousTick && sample.Ticks < window
}

func analysisFeaturesWindow(sourceDuration int64) (int64, error) {
	if sourceDuration <= 0 || sourceDuration > media.MaxAnalysisDurationTicks {
		return 0, fmt.Errorf("%w: analysis source duration must be positive and at most 12 hours", ErrInvalidInput)
	}
	return min(sourceDuration, 600*introdetect.TicksPerSecond), nil
}

func validateAnalysisFeaturesMetadata(profile string, uncertainty int64) error {
	if len(profile) == 0 || len(profile) > analysisFeaturesMaxProfileBytes ||
		!utf8.ValidString(profile) || strings.TrimSpace(profile) == "" {
		return fmt.Errorf("%w: invalid analysis algorithm profile", ErrInvalidInput)
	}
	for _, character := range profile {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: analysis algorithm profile contains a control character", ErrInvalidInput)
		}
	}
	if uncertainty < 0 || uncertainty > 30*introdetect.TicksPerSecond {
		return fmt.Errorf("%w: invalid analysis audio boundary uncertainty", ErrInvalidInput)
	}
	return nil
}

func validAnalysisAudioSample(sample introdetect.AudioSample, previousEnd, window int64) bool {
	return sample.StartTicks >= 0 && sample.StartTicks >= previousEnd &&
		sample.EndTicks > sample.StartTicks && sample.EndTicks <= window &&
		sample.EndTicks-sample.StartTicks <= 2*introdetect.TicksPerSecond
}

func validAnalysisVisualSample(sample introdetect.VisualSample, previousTick, window int64) bool {
	if !sample.LumaKnown && sample.Luma != ([64]int8{}) {
		return false
	}
	for _, cell := range sample.Luma {
		if cell == -128 {
			return false
		}
	}
	return sample.Ticks >= 0 && sample.Ticks > previousTick && sample.Ticks < window && sample.Contrast <= 1000
}

const analysisFeatureCacheMaxRows = 8192

func analysisFeatureCacheKey(source AnalysisSource, profileFingerprint string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("goby.analysis.feature-cache.v1"))
	for _, value := range []string{source.ItemID, source.SourceRevision, profileFingerprint} {
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// GetAnalysisFeatures admits only a current target or support source from the
// fenced child. Forced work cannot silently reuse an earlier extraction.
func (s *Store) GetAnalysisFeatures(ctx context.Context, childID, itemID string, fence AnalysisFence) (AnalysisFeatures, bool, error) {
	if err := analysisContext(ctx); err != nil {
		return AnalysisFeatures{}, false, err
	}
	if s == nil || s.pool == nil {
		return AnalysisFeatures{}, false, ErrUnavailable
	}
	if !metadataIdentifier(childID) || !metadataIdentifier(itemID) {
		return AnalysisFeatures{}, false, ErrInvalidInput
	}
	var result AnalysisFeatures
	var found bool
	err := s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		if work.TaskKey != TaskIntroAnalysisKey || !work.Execution.Available {
			return ErrInvalidInput
		}
		source, allowed := FindAnalysisSource(work, itemID)
		if !allowed {
			return ErrAnalysisSourceChanged
		}
		if work.Force {
			return nil
		}
		var err error
		result, found, err = loadAnalysisFeatureCache(tx, source, work.ConfigurationFingerprint)
		if err == nil && found && result.AlgorithmProfile != work.Execution.IntroProfile {
			_, err = tx.Exec(`DELETE FROM analysis_feature_cache WHERE cache_key=$1`, analysisFeatureCacheKey(source, work.ConfigurationFingerprint))
			result, found = AnalysisFeatures{}, false
		}
		return err
	})
	if err != nil {
		return AnalysisFeatures{}, false, err
	}
	return result, found, nil
}

func loadAnalysisFeatureCache(tx OwnedTx, source AnalysisSource, profileFingerprint string) (AnalysisFeatures, bool, error) {
	key := analysisFeatureCacheKey(source, profileFingerprint)
	var itemID, revision, storedProfile, content, algorithm string
	var payload []byte
	var size, duration int64
	// Avoid transferring an unexpectedly oversized stored bytea before checking
	// its format. Such cache corruption is recoverable by a fresh extraction.
	err := tx.QueryRow(`SELECT item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,
		CASE WHEN octet_length(payload)<=$2 THEN payload ELSE NULL END,bytes,duration_ticks
		FROM analysis_feature_cache WHERE cache_key=$1`, key, analysisFeaturesMaxPayloadBytes).
		Scan(&itemID, &revision, &storedProfile, &content, &algorithm, &payload, &size, &duration)
	if errors.Is(err, pgx.ErrNoRows) {
		return AnalysisFeatures{}, false, nil
	}
	if err != nil {
		return AnalysisFeatures{}, false, err
	}
	value, decodeErr := DecodeAnalysisFeatures(payload, duration)
	if decodeErr != nil || itemID != source.ItemID || revision != source.SourceRevision || storedProfile != profileFingerprint ||
		size != int64(len(payload)) || duration != source.DurationTicks ||
		value.ContentSHA256 != content || value.AlgorithmProfile != algorithm {
		if _, err := tx.Exec(`DELETE FROM analysis_feature_cache WHERE cache_key=$1`, key); err != nil {
			return AnalysisFeatures{}, false, err
		}
		return AnalysisFeatures{}, false, nil
	}
	if _, err := tx.Exec(`UPDATE analysis_feature_cache SET last_used_at=clock_timestamp() WHERE cache_key=$1`, key); err != nil {
		return AnalysisFeatures{}, false, err
	}
	return value, true, nil
}

// PutAnalysisFeatures preserves whole-content identity supplied by the bounded
// source reader. The cache key never substitutes a prefix fingerprint for it.
func (s *Store) PutAnalysisFeatures(ctx context.Context, childID, itemID string, fence AnalysisFence, value AnalysisFeatures) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if !metadataIdentifier(childID) || !metadataIdentifier(itemID) {
		return ErrInvalidInput
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		if work.TaskKey != TaskIntroAnalysisKey || !work.Execution.Available || value.AlgorithmProfile != work.Execution.IntroProfile {
			return ErrInvalidInput
		}
		source, allowed := FindAnalysisSource(work, itemID)
		if !allowed {
			return ErrAnalysisSourceChanged
		}
		return putAnalysisFeatureCache(tx, source, work.ConfigurationFingerprint, work.Profile, value)
	})
}

func putAnalysisFeatureCache(tx OwnedTx, source AnalysisSource, profileFingerprint string, profile AnalysisProfile, value AnalysisFeatures) error {
	if err := ValidateAnalysisProfile(profile); err != nil {
		return err
	}
	payload, err := EncodeAnalysisFeatures(value, source.DurationTicks)
	if err != nil {
		return err
	}
	key := analysisFeatureCacheKey(source, profileFingerprint)
	// Superseded revisions for this item no longer serve any current work.
	if _, err := tx.Exec(`DELETE FROM analysis_feature_cache WHERE item_id=$1 AND cache_key<>$2`, source.ItemID, key); err != nil {
		return err
	}
	tag, err := tx.Exec(`INSERT INTO analysis_feature_cache
		(cache_key,item_id,source_revision,profile_fingerprint,content_sha256,algorithm_profile,duration_ticks,payload,bytes,last_used_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,clock_timestamp()) ON CONFLICT(cache_key) DO UPDATE
		SET payload=EXCLUDED.payload,bytes=EXCLUDED.bytes,last_used_at=EXCLUDED.last_used_at
		WHERE analysis_feature_cache.item_id=EXCLUDED.item_id
		AND analysis_feature_cache.source_revision=EXCLUDED.source_revision
		AND analysis_feature_cache.profile_fingerprint=EXCLUDED.profile_fingerprint
		AND analysis_feature_cache.content_sha256=EXCLUDED.content_sha256
		AND analysis_feature_cache.algorithm_profile=EXCLUDED.algorithm_profile
		AND analysis_feature_cache.duration_ticks=EXCLUDED.duration_ticks`, key, source.ItemID,
		source.SourceRevision, profileFingerprint, value.ContentSHA256, value.AlgorithmProfile,
		source.DurationTicks, payload, len(payload))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAnalysisSourceChanged
	}
	return trimAnalysisFeatureCache(tx, profile.FeatureCacheMaxBytes)
}

func trimAnalysisFeatureCache(tx OwnedTx, maximumBytes int64) error {
	// The catalog owner serializes cache writes with configuration changes.
	// Deterministic ties make eviction reproducible without exceeding either
	// the current profile's byte budget or the independent hard row bound.
	_, err := tx.Exec(`WITH ranked AS MATERIALIZED (
		SELECT cache_key,row_number() OVER (ORDER BY last_used_at DESC,cache_key) AS position,
			sum(bytes) OVER (ORDER BY last_used_at DESC,cache_key ROWS UNBOUNDED PRECEDING) AS retained_bytes
		FROM analysis_feature_cache)
		DELETE FROM analysis_feature_cache cache USING ranked
		WHERE cache.cache_key=ranked.cache_key AND (ranked.position>$1 OR ranked.retained_bytes>$2)`,
		analysisFeatureCacheMaxRows, maximumBytes)
	return err
}
