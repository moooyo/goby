package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

const (
	analysisPreviewWorkspaceOverhead int64 = 1 << 20
	analysisPreviewManifestLimit           = 64 << 10
	analysisPreviewFrameLimit              = 4096
)

type analysisPreviewBuildPlan struct {
	interval, reservation, variantBytes, jpegBytes int64
	frames                                         int
	widths                                         []int
}

type analysisPreviewExtract func(context.Context, media.PreviewAnalysisOptions, func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error)

// buildPreview returns a live publication pin only on success. It does not
// publish a database reference. The caller must Keep after a committed or
// uncertain database transaction, or Discard after a definite failure.
func (r *mediaAnalysisRuntime) buildPreview(ctx context.Context, work library.AnalysisWork, source library.AnalysisSource, file *os.File, info media.Info) (*analysiscache.Publication, []library.AnalysisPreviewPublication, error) {
	if r == nil || r.cache == nil || ctx == nil || file == nil {
		return nil, nil, library.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	plan, err := planAnalysisPreviewBuild(r.configuration, work, source, info)
	if err != nil {
		return nil, nil, err
	}
	before, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() || before.Size() != source.Size {
		return nil, nil, library.ErrAnalysisSourceChanged
	}
	stream, present := analysisStream(info, "video")
	if !present {
		return nil, nil, media.ErrAnalysisUnproven
	}
	extractor, err := bindAnalysisPreviewExtractor(r.extractor, work, plan)
	if err != nil {
		return nil, nil, err
	}
	return generateAnalysisPreview(ctx, r.cache, work, source, plan,
		func(ctx context.Context, options media.PreviewAnalysisOptions, emit func(media.PreviewFrame) error) (media.PreviewAnalysisSummary, error) {
			return extractor.ExtractPreviews(ctx, file, info, stream, options, emit)
		})
}

func planAnalysisPreviewBuild(configuration config.MediaAnalysisConfig, work library.AnalysisWork, source library.AnalysisSource, info media.Info) (analysisPreviewBuildPlan, error) {
	if work.TaskKey != library.TaskPreviewGenerationKey || !work.Execution.Available || work.Execution.Version != library.AnalysisProfileVersion ||
		work.Execution.PreviewProfile != media.PreviewAnalysisProfile || !analysisPreviewHash(work.Execution.FFmpegSHA256) || !analysisPreviewHash(work.Execution.FFprobeSHA256) ||
		!source.Target || !analysisPreviewBuildIdentity(source.ItemID) || !analysisPreviewBuildIdentity(source.SourceRevision) ||
		!analysisPreviewHash(work.ConfigurationFingerprint) || !analysisPreviewBuildIdentity(work.ConfigurationRevision) || work.PublicationEpoch < 1 ||
		library.ValidateAnalysisProfile(work.Profile) != nil || len(work.Execution.PreviewWidths) != 3 || len(work.Sources) != 1 || work.Sources[0] != source {
		return analysisPreviewBuildPlan{}, library.ErrInvalidInput
	}
	widths := append([]int(nil), work.Execution.PreviewWidths...)
	for index, width := range []int{240, 320, 400} {
		if widths[index] != width {
			return analysisPreviewBuildPlan{}, library.ErrInvalidInput
		}
	}
	if source.DurationTicks != info.DurationTicks || source.Size <= 0 || info.Size > 0 && source.Size != info.Size {
		return analysisPreviewBuildPlan{}, library.ErrAnalysisSourceChanged
	}
	if source.Size > work.Profile.MaxSourceBytes {
		return analysisPreviewBuildPlan{}, media.ErrAnalysisBudget
	}
	interval, err := library.EffectiveAnalysisPreviewInterval(source.DurationTicks, work.Profile.PreviewIntervalSeconds)
	if err != nil {
		return analysisPreviewBuildPlan{}, err
	}
	count := (source.DurationTicks-1)/interval + 1
	if count < 1 || count > analysisPreviewFrameLimit {
		return analysisPreviewBuildPlan{}, media.ErrAnalysisBudget
	}
	if configuration.MaxEntryBytes <= analysisPreviewWorkspaceOverhead || configuration.MaxEntryBytes > 1<<30 || configuration.MaxFileBytes < 1 || configuration.MaxFileBytes > 128<<20 {
		return analysisPreviewBuildPlan{}, media.ErrAnalysisBudget
	}
	// Three completed BIFs and one current JPEG scratch set can coexist. Each
	// BIF allowance includes its header/index; overhead covers both manifests,
	// the owner/seal records and rounding, rather than ignoring control bytes.
	variant := min(configuration.MaxFileBytes, int64(128<<20), (configuration.MaxEntryBytes-analysisPreviewWorkspaceOverhead)/4)
	indexBytes := int64(64) + 8*(count+1)
	if variant <= indexBytes {
		return analysisPreviewBuildPlan{}, media.ErrAnalysisBudget
	}
	return analysisPreviewBuildPlan{interval: interval, frames: int(count), variantBytes: variant, jpegBytes: variant - indexBytes,
		reservation: 4*variant + analysisPreviewWorkspaceOverhead, widths: widths}, nil
}

func bindAnalysisPreviewExtractor(extractor media.AnalysisExtractor, work library.AnalysisWork, plan analysisPreviewBuildPlan) (media.AnalysisExtractor, error) {
	if extractor.ExpectedFFmpegSHA256 != "" && extractor.ExpectedFFmpegSHA256 != work.Execution.FFmpegSHA256 ||
		extractor.ExpectedFFprobeSHA256 != "" && extractor.ExpectedFFprobeSHA256 != work.Execution.FFprobeSHA256 {
		return media.AnalysisExtractor{}, media.ErrAnalysisUnavailable
	}
	extractor.ExpectedFFmpegSHA256, extractor.ExpectedFFprobeSHA256 = work.Execution.FFmpegSHA256, work.Execution.FFprobeSHA256
	defaults := media.DefaultAnalysisLimits()
	limit := func(current, fallback, maximum int64) int64 {
		if current == 0 {
			current = fallback
		}
		return min(current, maximum)
	}
	extractor.Limits.MaxPreviewFrames = int(limit(int64(extractor.Limits.MaxPreviewFrames), int64(defaults.MaxPreviewFrames), analysisPreviewFrameLimit))
	extractor.Limits.MaxOutputBytes = limit(extractor.Limits.MaxOutputBytes, defaults.MaxOutputBytes, plan.jpegBytes)
	extractor.Limits.MaxJPEGBytes = limit(extractor.Limits.MaxJPEGBytes, defaults.MaxJPEGBytes, min(plan.jpegBytes, int64(bif.DefaultLimits().MaxFrameBytes)))
	extractor.Limits.MaxFramePixels = limit(extractor.Limits.MaxFramePixels, defaults.MaxFramePixels, int64(bif.DefaultLimits().MaxPixels))
	maximumRuntime := time.Duration(work.Profile.MaxItemRuntimeSeconds) * time.Second
	if extractor.Limits.Timeout == 0 || extractor.Limits.Timeout > maximumRuntime {
		extractor.Limits.Timeout = maximumRuntime
	}
	if extractor.Limits.MaxPreviewFrames < plan.frames || extractor.Limits.MaxOutputBytes < 1 || extractor.Limits.MaxJPEGBytes < 1 || extractor.Limits.Timeout < time.Second {
		return media.AnalysisExtractor{}, media.ErrAnalysisBudget
	}
	return extractor, nil
}

type analysisPreviewFrameRecord struct {
	index                 int
	size, nominal, actual int64
}

type analysisPreviewVariantManifest struct {
	Width, Height, FrameCount int
	Bytes                     int64
	SHA256, TimelineSHA256    string
}

type analysisPreviewBuildManifest struct {
	Version                                                           int
	ItemID, SourceRevision, ProfileFingerprint, ConfigurationRevision string
	PublicationEpoch                                                  int64
	PreviewProfile, FFmpegSHA256, FFprobeSHA256                       string
	Quality                                                           int
	IntervalTicks                                                     int64
	Variants                                                          []analysisPreviewVariantManifest
}

// The dependency is a synchronous extractor, not a second scheduler. Its
// callback cannot outlive this call; source/process completion precedes BIF
// assembly. Tests can exercise disk/pin failures without decoding fake media.
func generateAnalysisPreview(ctx context.Context, cache *analysiscache.Store, work library.AnalysisWork, source library.AnalysisSource, plan analysisPreviewBuildPlan, extract analysisPreviewExtract) (publication *analysiscache.Publication, values []library.AnalysisPreviewPublication, resultErr error) {
	if ctx == nil || cache == nil || extract == nil {
		return nil, nil, library.ErrUnavailable
	}
	if plan.frames < 1 || plan.frames > analysisPreviewFrameLimit || len(plan.widths) != 3 || plan.interval < 2*media.TicksPerSecond || plan.interval%media.TicksPerSecond != 0 ||
		source.DurationTicks <= 0 || source.DurationTicks > media.MaxAnalysisDurationTicks || int64(plan.frames) != (source.DurationTicks-1)/plan.interval+1 ||
		plan.variantBytes < 1 || plan.variantBytes > 128<<20 || plan.jpegBytes != plan.variantBytes-64-8*int64(plan.frames+1) || plan.jpegBytes < 1 ||
		plan.reservation != 4*plan.variantBytes+analysisPreviewWorkspaceOverhead {
		return nil, nil, media.ErrAnalysisBudget
	}
	operation, cancel := context.WithTimeout(ctx, time.Duration(work.Profile.MaxItemRuntimeSeconds)*time.Second)
	defer cancel()
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, nil, err
	}
	key := hex.EncodeToString(token[:])
	builder, err := cache.Begin(operation, key, plan.reservation)
	if err != nil {
		return nil, nil, analysisPreviewBuildError(err)
	}
	builderOwned := true
	defer func() {
		if builderOwned {
			// A canceled request does not release an in-flight filesystem call or
			// a temporary reader. Keep the caller's execution slot until it ends.
			resultErr = errors.Join(resultErr, builder.Abort(context.Background()))
		}
		if resultErr != nil {
			publication, values = nil, nil
			resultErr = analysisPreviewBuildError(resultErr)
		}
	}()
	manifest := analysisPreviewBuildManifest{Version: 1, ItemID: source.ItemID, SourceRevision: source.SourceRevision,
		ProfileFingerprint: work.ConfigurationFingerprint, ConfigurationRevision: work.ConfigurationRevision, PublicationEpoch: work.PublicationEpoch,
		PreviewProfile: work.Execution.PreviewProfile, FFmpegSHA256: work.Execution.FFmpegSHA256, FFprobeSHA256: work.Execution.FFprobeSHA256,
		Quality: work.Profile.PreviewQuality, IntervalTicks: plan.interval}
	var firstNominal, firstActual []int64
	artifacts := make(map[string]analysiscache.Artifact, 4)
	for _, width := range plan.widths {
		records := make([]analysisPreviewFrameRecord, 0, plan.frames)
		var jpegBytes int64
		height := 0
		summary, err := extract(operation, media.PreviewAnalysisOptions{Width: width, IntervalTicks: plan.interval, Quality: work.Profile.PreviewQuality}, func(frame media.PreviewFrame) error {
			if err := operation.Err(); err != nil {
				return err
			}
			index := len(records)
			if index >= plan.frames || index >= analysisPreviewFrameLimit || int64(len(frame.JPEG)) > plan.jpegBytes-jpegBytes || len(frame.JPEG) > int(bif.DefaultLimits().MaxFrameBytes) ||
				frame.Height > int(bif.DefaultLimits().MaxDimension) {
				return media.ErrAnalysisBudget
			}
			nominal := int64(index) * plan.interval
			// The extractor proves a displayed source frame, which can span
			// several slots or remain visible after video EOF. A slot before
			// the first video PTS holds that same first frame backward.
			if frame.Width != width || frame.Height <= 0 || height != 0 && height != frame.Height || len(frame.JPEG) == 0 ||
				frame.NominalTicks != nominal || frame.ActualTicks < 0 || frame.ActualTicks >= source.DurationTicks ||
				index > 0 && frame.ActualTicks < records[index-1].actual ||
				index > 0 && frame.ActualTicks > nominal && frame.ActualTicks != records[0].actual || sha256.Sum256(frame.JPEG) != frame.SHA256 {
				return media.ErrAnalysisUnproven
			}
			if firstNominal != nil && (index >= len(firstNominal) || frame.NominalTicks != firstNominal[index] || frame.ActualTicks != firstActual[index]) {
				return fmt.Errorf("%w: preview variants selected different source frames", media.ErrAnalysisUnproven)
			}
			decoded, err := jpeg.DecodeConfig(bytes.NewReader(frame.JPEG))
			if err != nil || decoded.Width != frame.Width || decoded.Height != frame.Height {
				return fmt.Errorf("%w: preview JPEG geometry differs from its frame facts", media.ErrAnalysisUnproven)
			}
			name := fmt.Sprintf("frame-%06d.jpg", index)
			artifact, err := builder.WriteTemporary(operation, name, func(writeCtx context.Context, writer io.Writer) error {
				if err := writeCtx.Err(); err != nil {
					return err
				}
				n, err := writer.Write(frame.JPEG)
				if err == nil && n != len(frame.JPEG) {
					err = io.ErrShortWrite
				}
				return err
			})
			if err != nil {
				return err
			}
			if artifact.Size != int64(len(frame.JPEG)) || artifact.SHA256 != hex.EncodeToString(frame.SHA256[:]) {
				return media.ErrAnalysisUnproven
			}
			height, jpegBytes = frame.Height, jpegBytes+artifact.Size
			// Do not retain PreviewFrame: it owns the callback's JPEG buffer.
			records = append(records, analysisPreviewFrameRecord{index: index, size: artifact.Size, nominal: nominal, actual: frame.ActualTicks})
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		if summary.Profile != work.Execution.PreviewProfile || summary.FFmpegSHA256 != work.Execution.FFmpegSHA256 || summary.FFprobeSHA256 != work.Execution.FFprobeSHA256 ||
			summary.Quality != work.Profile.PreviewQuality || summary.IntervalTicks != plan.interval || summary.Width != width || summary.Height != height ||
			summary.FrameCount != plan.frames || len(records) != plan.frames || summary.JPEGBytes != jpegBytes {
			return nil, nil, fmt.Errorf("%w: preview summary differs from its admitted frames", media.ErrAnalysisUnproven)
		}
		nominal, actual := make([]int64, len(records)), make([]int64, len(records))
		frames := make([]bif.FrameSource, len(records))
		for index, record := range records {
			nominal[index], actual[index] = record.nominal, record.actual
			name, size := fmt.Sprintf("frame-%06d.jpg", record.index), record.size
			frames[index] = bif.FrameSource{Timestamp: uint32(index), Size: size,
				Open: func(readCtx context.Context) (io.ReadCloser, error) {
					lease, err := builder.OpenTemporary(readCtx, name)
					if err != nil {
						return lease, err
					}
					if lease.Artifact.Size != size {
						return lease, media.ErrAnalysisUnproven
					}
					return lease, nil
				}}
		}
		limits := bif.DefaultLimits()
		limits.MaxTotalBytes = uint64(plan.variantBytes)
		name := fmt.Sprintf("%d.bif", width)
		artifact, err := builder.WriteFile(operation, name, func(writeCtx context.Context, writer io.Writer) error {
			// Uniform frames use ordinal wire timestamps and their actual slot
			// interval as the multiplier. This also supports fixed-interval BIF
			// consumers that derive the frame index directly from that header.
			multiplier := uint32(plan.interval / (media.TicksPerSecond / 1000))
			_, err := bif.Write(writeCtx, writer, frames, multiplier, limits)
			return err
		})
		if err != nil {
			return nil, nil, err
		}
		if artifact.Size != jpegBytes+64+8*int64(len(records)+1) || artifact.Size > plan.variantBytes || !analysisPreviewHash(artifact.SHA256) {
			return nil, nil, media.ErrAnalysisUnproven
		}
		// Delete after WriteFile returns: deleting from its producer would try
		// to reacquire Builder's write mutex. BIF has closed each full lease.
		for _, record := range records {
			if err := builder.DeleteTemporary(operation, fmt.Sprintf("frame-%06d.jpg", record.index)); err != nil {
				return nil, nil, err
			}
		}
		timeline, err := library.EncodeAnalysisPreviewTimeline(nominal, actual)
		if err != nil {
			return nil, nil, err
		}
		timelineSHA := sha256.Sum256(timeline)
		manifest.Variants = append(manifest.Variants, analysisPreviewVariantManifest{Width: width, Height: height, FrameCount: len(records), Bytes: artifact.Size,
			SHA256: artifact.SHA256, TimelineSHA256: hex.EncodeToString(timelineSHA[:])})
		values = append(values, library.AnalysisPreviewPublication{ItemID: source.ItemID, CacheKey: key, Width: width, Height: height, SHA256: artifact.SHA256,
			Bytes: artifact.Size, FrameCount: len(records), IntervalTicks: plan.interval, NominalTicks: nominal, ActualTicks: actual})
		artifacts[name] = artifact
		if firstNominal == nil {
			firstNominal, firstActual = nominal, actual
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, nil, err
	}
	if len(encoded) > analysisPreviewManifestLimit {
		return nil, nil, media.ErrAnalysisBudget
	}
	manifestArtifact, err := builder.WriteFile(operation, "manifest.json", func(writeCtx context.Context, writer io.Writer) error {
		if err := writeCtx.Err(); err != nil {
			return err
		}
		n, err := writer.Write(encoded)
		if err == nil && n != len(encoded) {
			return io.ErrShortWrite
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	artifacts["manifest.json"] = manifestArtifact
	publication, err = builder.Publish(operation)
	if publication != nil {
		builderOwned = false
	}
	if err != nil {
		return nil, nil, discardAnalysisPreviewPublication(publication, err)
	}
	if publication == nil {
		return nil, nil, analysiscache.ErrUnsafe
	}
	if err := validateAnalysisPreviewEntry(publication.Entry, key, artifacts, plan.reservation); err != nil {
		return nil, nil, discardAnalysisPreviewPublication(publication, err)
	}
	if err := operation.Err(); err != nil {
		return nil, nil, discardAnalysisPreviewPublication(publication, err)
	}
	for index := range values {
		values[index].Seal = publication.Entry.Seal
	}
	return publication, values, nil
}

func validateAnalysisPreviewEntry(entry analysiscache.Entry, key string, expected map[string]analysiscache.Artifact, reservation int64) error {
	if entry.Key != key || !analysisPreviewHash(entry.Seal) || entry.Bytes < 1 || entry.Bytes > reservation || len(entry.Artifacts) != len(expected) {
		return analysiscache.ErrUnsafe
	}
	seen := make(map[string]bool, len(expected))
	for _, artifact := range entry.Artifacts {
		want, exists := expected[artifact.Name]
		if !exists || seen[artifact.Name] || artifact != want {
			return analysiscache.ErrUnsafe
		}
		seen[artifact.Name] = true
	}
	return nil
}

// Before the caller has any database reference, a failed filesystem publication
// is definitely unreferenced. Keep is the conservative pin-release fallback if
// deletion itself fails; it preserves charged bytes for later reconciliation.
func discardAnalysisPreviewPublication(publication *analysiscache.Publication, cause error) error {
	if publication == nil {
		return cause
	}
	if err := publication.Discard(context.Background()); err != nil {
		return errors.Join(cause, err, publication.Keep())
	}
	return cause
}

func analysisPreviewBuildError(err error) error {
	if errors.Is(err, analysiscache.ErrLimit) || errors.Is(err, bif.ErrLimit) || errors.Is(err, bif.ErrInvalidLimits) {
		return errors.Join(media.ErrAnalysisBudget, err)
	}
	return err
}

func analysisPreviewHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func analysisPreviewBuildIdentity(value string) bool {
	return len(value) > 0 && len(value) <= 256 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n/\\")
}
