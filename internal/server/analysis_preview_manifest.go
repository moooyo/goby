package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// acquirePreviewManifest binds durable preview metadata to the sealed generation
// that produced its BIFs. The caller owns a successful lease and must retain it
// until its final source/task check completes. This method performs no extraction.
func (r *mediaAnalysisRuntime) acquirePreviewManifest(ctx context.Context, references []library.AnalysisPreview, work *library.AnalysisWork) (result *analysiscache.Lease, resultErr error) {
	if ctx == nil {
		return nil, library.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.cache == nil {
		return nil, library.ErrUnavailable
	}
	if err := validatePreviewManifestReferences(references); err != nil {
		return nil, err
	}
	first := references[0]
	lease, err := r.cache.Acquire(ctx, first.CacheKey, first.Seal, "manifest.json")
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, lease.Close())
			result = nil
		}
	}()
	if lease.File == nil || lease.Seal != first.Seal || lease.Artifact.Name != "manifest.json" ||
		lease.Artifact.Size < 1 || lease.Artifact.Size > analysisPreviewManifestLimit || !analysisPreviewHash(lease.Artifact.SHA256) {
		return nil, invalidPreviewManifest("invalid manifest artifact")
	}
	// SectionReader leaves the independently owned descriptor at its original
	// offset. A bounded extra byte also rejects a changed file length.
	raw, err := io.ReadAll(io.NewSectionReader(lease.File, 0, lease.Artifact.Size+1))
	if err != nil {
		return nil, errors.Join(analysiscache.ErrUnsafe, err)
	}
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != lease.Artifact.Size || hex.EncodeToString(digest[:]) != lease.Artifact.SHA256 {
		return nil, invalidPreviewManifest("manifest bytes changed after acquisition")
	}
	if err := validatePreviewManifest(raw, references, work); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return lease, nil
}

func invalidPreviewManifest(reason string) error {
	return fmt.Errorf("%w: %s", analysiscache.ErrUnsafe, reason)
}

func previewManifestRevision(value string) bool {
	revision, err := strconv.ParseInt(value, 10, 64)
	return err == nil && revision > 0 && strconv.FormatInt(revision, 10) == value
}

func previewManifestIdentity(value string) bool {
	if !analysisPreviewBuildIdentity(value) || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validatePreviewManifestReferences(references []library.AnalysisPreview) error {
	if len(references) < 1 || len(references) > 3 {
		return invalidPreviewManifest("invalid reference count")
	}
	first := references[0]
	if !previewManifestIdentity(first.ItemID) || !previewManifestIdentity(first.SourceRevision) ||
		!analysisPreviewHash(first.CacheKey) || !analysisPreviewHash(first.Seal) ||
		!analysisPreviewHash(first.ProfileFingerprint) || !previewManifestRevision(first.ProfileRevision) || first.PublicationEpoch < 1 {
		return invalidPreviewManifest("invalid reference identity")
	}
	seen := make(map[int]bool, len(references))
	for _, reference := range references {
		if reference.ItemID != first.ItemID || reference.SourceRevision != first.SourceRevision ||
			reference.CacheKey != first.CacheKey || reference.Seal != first.Seal ||
			reference.ProfileFingerprint != first.ProfileFingerprint || reference.ProfileRevision != first.ProfileRevision ||
			reference.PublicationEpoch != first.PublicationEpoch || seen[reference.Width] ||
			reference.Width != 240 && reference.Width != 320 && reference.Width != 400 {
			return invalidPreviewManifest("references do not describe one generation")
		}
		seen[reference.Width] = true
	}
	return nil
}

func validatePreviewManifest(raw []byte, references []library.AnalysisPreview, work *library.AnalysisWork) error {
	if len(raw) == 0 || len(raw) > analysisPreviewManifestLimit {
		return invalidPreviewManifest("manifest size is outside its bound")
	}
	if err := validatePreviewManifestReferences(references); err != nil {
		return err
	}
	var manifest analysisPreviewBuildManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return invalidPreviewManifest("manifest is not a complete JSON record")
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, raw) {
		// The producer uses json.Marshal on this exact closed struct. Matching
		// those bytes rejects aliases, unknown/missing/duplicate keys, reordered
		// fields, trailing data and other representations the writer never emits.
		return invalidPreviewManifest("manifest is not canonical generation JSON")
	}
	first := references[0]
	if manifest.Version != 1 || manifest.ItemID != first.ItemID || manifest.SourceRevision != first.SourceRevision ||
		manifest.ProfileFingerprint != first.ProfileFingerprint || manifest.ConfigurationRevision != first.ProfileRevision ||
		manifest.PublicationEpoch != first.PublicationEpoch || manifest.PreviewProfile != media.PreviewAnalysisProfile ||
		!analysisPreviewHash(manifest.FFmpegSHA256) || !analysisPreviewHash(manifest.FFprobeSHA256) ||
		manifest.Quality < 40 || manifest.Quality > 95 ||
		manifest.IntervalTicks < 2*media.TicksPerSecond || manifest.IntervalTicks > 120*media.TicksPerSecond || manifest.IntervalTicks%media.TicksPerSecond != 0 ||
		len(manifest.Variants) != 3 {
		return invalidPreviewManifest("manifest identity or execution metadata does not match")
	}
	variants := make(map[int]analysisPreviewVariantManifest, 3)
	var firstVariant analysisPreviewVariantManifest
	for index, variant := range manifest.Variants {
		if variant.Width != 240 && variant.Width != 320 && variant.Width != 400 || variant.Height < 1 || variant.Height > 2048 ||
			variant.FrameCount < 1 || variant.FrameCount > analysisPreviewFrameLimit ||
			variant.Bytes < 72+12*int64(variant.FrameCount) || variant.Bytes > 128<<20 ||
			!analysisPreviewHash(variant.SHA256) || !analysisPreviewHash(variant.TimelineSHA256) {
			return invalidPreviewManifest("invalid manifest variant")
		}
		if _, duplicate := variants[variant.Width]; duplicate {
			return invalidPreviewManifest("duplicate manifest width")
		}
		if index == 0 {
			firstVariant = variant
		} else if variant.FrameCount != firstVariant.FrameCount || variant.TimelineSHA256 != firstVariant.TimelineSHA256 {
			return invalidPreviewManifest("width variants do not share one source timeline")
		}
		variants[variant.Width] = variant
	}
	for _, reference := range references {
		variant := variants[reference.Width]
		if reference.Height != variant.Height || reference.FrameCount != variant.FrameCount || reference.Bytes != variant.Bytes ||
			reference.SHA256 != variant.SHA256 || reference.IntervalTicks != manifest.IntervalTicks ||
			len(reference.NominalTicks) != variant.FrameCount || len(reference.ActualTicks) != variant.FrameCount {
			return invalidPreviewManifest("preview reference differs from sealed variant metadata")
		}
		packed, err := library.EncodeAnalysisPreviewTimeline(reference.NominalTicks, reference.ActualTicks)
		if err != nil {
			return invalidPreviewManifest("invalid reference timeline")
		}
		for index, nominal := range reference.NominalTicks {
			if nominal != int64(index)*manifest.IntervalTicks {
				return invalidPreviewManifest("reference timeline differs from its interval")
			}
		}
		digest := sha256.Sum256(packed)
		if hex.EncodeToString(digest[:]) != variant.TimelineSHA256 {
			return invalidPreviewManifest("reference timeline differs from sealed generation")
		}
	}
	if work != nil {
		if work.TaskKey != library.TaskPreviewGenerationKey || !work.Execution.Available ||
			library.ValidateAnalysisExecutionProfile(work.Execution) != nil || library.ValidateAnalysisProfile(work.Profile) != nil ||
			len(work.Sources) != 1 || !work.Sources[0].Target || work.Sources[0].ItemID != first.ItemID || work.Sources[0].SourceRevision != first.SourceRevision ||
			work.ConfigurationFingerprint != manifest.ProfileFingerprint || work.ConfigurationRevision != manifest.ConfigurationRevision || work.PublicationEpoch != manifest.PublicationEpoch ||
			work.Execution.PreviewProfile != manifest.PreviewProfile || work.Execution.FFmpegSHA256 != manifest.FFmpegSHA256 ||
			work.Execution.FFprobeSHA256 != manifest.FFprobeSHA256 || work.Profile.PreviewQuality != manifest.Quality ||
			len(work.Execution.PreviewWidths) != len(references) || len(references) != 3 {
			return invalidPreviewManifest("manifest differs from admitted preview execution")
		}
		interval, err := library.EffectiveAnalysisPreviewInterval(work.Sources[0].DurationTicks, work.Profile.PreviewIntervalSeconds)
		if err != nil || interval != manifest.IntervalTicks || int64(firstVariant.FrameCount) != (work.Sources[0].DurationTicks-1)/interval+1 {
			return invalidPreviewManifest("manifest does not cover the admitted source duration")
		}
		for _, reference := range references {
			for _, actual := range reference.ActualTicks {
				if actual >= work.Sources[0].DurationTicks {
					return invalidPreviewManifest("manifest timeline exceeds the admitted source")
				}
			}
		}
	}
	return nil
}
