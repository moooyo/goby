package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

// PrepareMediaOCR produces reviewable observations without changing the media
// source or publishing any subtitle. The caller owns the durable job claim and
// must persist this result only while that claim remains current.
func (s *Store) PrepareMediaOCR(ctx context.Context, work MediaOperationWork, configuration media.SubtitleOCRConfig) (MediaOperationResult, error) {
	operation := work.Operation
	if operation.Kind != MediaOperationOCR || work.Apply || work.Discard || !validMediaOperationWork(work) ||
		operation.SourceRevision == "" || operation.StreamIndex < 0 {
		return MediaOperationResult{}, ErrInvalidInput
	}
	snapshot, err := s.readMediaOCRSnapshot(ctx, operation)
	if err != nil {
		return MediaOperationResult{}, err
	}
	var selected media.Stream
	found := false
	for _, stream := range snapshot.mediaFile.Item.Media.Streams {
		if stream.Index == operation.StreamIndex && stream.CodecType == "subtitle" {
			selected, found = stream, true
			break
		}
	}
	if !found {
		return MediaOperationResult{}, ErrNotFound
	}
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openMediaSource(ctx, snapshot)
		return file, snapshot.mediaFile, err
	})
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer file.Close()
	result, err := media.RecognizeBitmapSubtitles(ctx, configuration, file, selected, *snapshot.mediaFile.Item.Media, operation.Parameters.ModelIDs)
	if err != nil {
		return MediaOperationResult{}, err
	}
	info, err := file.Stat()
	if err != nil || !snapshot.matches(info) {
		return MediaOperationResult{}, errors.Join(ErrSourceChanged, err)
	}
	// Re-read both authority and complete catalog identity after expensive work.
	// A new descriptor also detects pathname replacement during recognition.
	current, err := s.readMediaOCRSnapshot(ctx, operation)
	if err != nil {
		return MediaOperationResult{}, err
	}
	check, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openMediaSource(ctx, current)
		return file, current.mediaFile, err
	})
	if err != nil {
		return MediaOperationResult{}, err
	}
	defer check.Close()
	currentInfo, err := check.Stat()
	if err != nil || !sameMediaSourceFile(info, currentInfo) {
		return MediaOperationResult{}, errors.Join(ErrSourceChanged, err)
	}
	return mediaOCRReviewResult(operation, result)
}

func (s *Store) readMediaOCRSnapshot(ctx context.Context, operation MediaOperation) (indexedMediaSource, error) {
	tx, err := s.beginMetadataRead(ctx, operation.RequestActor)
	if err != nil {
		return indexedMediaSource{}, err
	}
	defer rollback(tx)
	var revision string
	err = tx.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1`, operation.ItemID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexedMediaSource{}, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, err
	}
	if revision != operation.SourceRevision {
		return indexedMediaSource{}, ErrSourceChanged
	}
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), operation.ItemID, operation.MediaSourceID)
	if err != nil {
		return indexedMediaSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return indexedMediaSource{}, err
	}
	return snapshot, nil
}

func mediaOCRReviewResult(operation MediaOperation, result media.SubtitleOCRResult) (MediaOperationResult, error) {
	if len(result.Cues) < 1 || len(result.Cues) > media.MaxSubtitleOCRCues {
		return MediaOperationResult{}, fmt.Errorf("%w: OCR cue count is outside the supported range", ErrInvalidInput)
	}
	cues := make([]MediaOperationCue, len(result.Cues))
	var imageBytes, textBytes int
	for index, source := range result.Cues {
		imageBytes += len(source.ImagePNG)
		textBytes += len(source.Text)
		if imageBytes > media.MaxSubtitleOCRImageBytes || textBytes > media.MaxSubtitleOCRTextBytes {
			return MediaOperationResult{}, fmt.Errorf("%w: OCR review exceeds its resource limit", ErrInvalidInput)
		}
		confidence := source.Confidence
		cues[index] = MediaOperationCue{Ordinal: index, OriginalStartTicks: source.StartTicks, OriginalEndTicks: source.EndTicks,
			OriginalText: source.Text, StartTicks: source.StartTicks, EndTicks: source.EndTicks, Text: source.Text,
			Included: strings.TrimSpace(source.Text) != "", Confidence: &confidence, Warnings: []string{},
			ImageSHA256: source.ImageSHA256, ImagePNG: source.ImagePNG, IsForced: source.Forced, IsHearingImpaired: source.HearingImpaired}
		if !cues[index].Included {
			cues[index].Warnings = append(cues[index].Warnings, "No text was recognized. Review the source image before including this cue.")
		}
	}
	// Per-cue diagnostics belong with the paginated review rows. Keeping tens
	// of thousands of those strings in one summary would exceed its document
	// limit even when the captions and images remain within their budgets.
	globalWarnings := []string{}
	for _, warning := range result.Warnings {
		if value, ok := strings.CutPrefix(warning, "cue_"); ok {
			ordinalText, code, split := strings.Cut(value, "_")
			ordinal, err := strconv.Atoi(ordinalText)
			if split && err == nil && ordinal > 0 && ordinal <= len(cues) {
				switch code {
				case "empty_text":
					continue
				case "low_confidence":
					cues[ordinal-1].Warnings = append(cues[ordinal-1].Warnings, "Low confidence recognition. Review this cue before applying it.")
					continue
				case "engine_diagnostics":
					cues[ordinal-1].Warnings = append(cues[ordinal-1].Warnings, "The OCR engine reported diagnostics for this cue.")
					continue
				}
			}
		}
		globalWarnings = append(globalWarnings, warning)
	}
	summary, err := json.Marshal(struct {
		EngineSHA256 string
		ModelID      string
		ModelSHA256  string
		Models       []media.SubtitleOCRModelIdentity
		CueCount     int
		WarningCount int
		Warnings     []string
	}{result.EngineSHA256, result.ModelID, result.ModelSHA256, result.Models, len(cues), len(result.Warnings), globalWarnings})
	if err != nil {
		return MediaOperationResult{}, err
	}
	// Images are already covered by their content hashes in the review record.
	operation.ResultSummary = summary
	digest, err := mediaOperationReviewHash(operation, cues)
	if err != nil {
		return MediaOperationResult{}, err
	}
	return MediaOperationResult{Summary: summary, ResultHash: digest, Cues: cues}, nil
}

// renderMediaOCRCues treats recognized and corrected text as plain caption text,
// not markup or subtitle syntax. Overlapping cues remain independent; a stable
// time order prevents correction order from changing presentation semantics.
func renderMediaOCRCues(cues []MediaOperationCue, format string, duration int64) ([]byte, error) {
	if format != "srt" && format != "vtt" || duration <= 0 || len(cues) > MaxMediaOperationCues {
		return nil, ErrInvalidInput
	}
	ordered := append([]MediaOperationCue(nil), cues...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].StartTicks == ordered[j].StartTicks {
			return ordered[i].Ordinal < ordered[j].Ordinal
		}
		return ordered[i].StartTicks < ordered[j].StartTicks
	})
	document := subtitle.Document{Format: subtitle.FormatWebVTT}
	textBytes := 0
	for _, cue := range ordered {
		if !cue.Included {
			continue
		}
		if cue.StartTicks < 0 || cue.EndTicks <= cue.StartTicks || cue.EndTicks > duration ||
			strings.TrimSpace(cue.Text) == "" || !utf8.ValidString(cue.Text) || len(cue.Text) > subtitle.MaxCueTextBytes ||
			strings.IndexFunc(cue.Text, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
			return nil, fmt.Errorf("%w: an included OCR cue needs valid timing and printable text", ErrInvalidInput)
		}
		textBytes += len(cue.Text)
		if textBytes > maxOwnedSubtitleBytes {
			return nil, fmt.Errorf("%w: corrected OCR text exceeds its resource limit", ErrInvalidInput)
		}
		document.Cues = append(document.Cues, subtitle.Cue{StartTicks: cue.StartTicks, EndTicks: cue.EndTicks, Text: html.EscapeString(cue.Text)})
	}
	if len(document.Cues) == 0 {
		return nil, fmt.Errorf("%w: include at least one reviewed OCR cue", ErrInvalidInput)
	}
	rendered, err := subtitle.Render(document, subtitle.Options{Format: subtitle.Format(format), CopyTimestamps: true})
	if err != nil {
		return nil, fmt.Errorf("%w: reviewed OCR captions cannot be rendered: %w", ErrInvalidInput, err)
	}
	if len(rendered.Data) > maxOwnedSubtitleBytes {
		return nil, fmt.Errorf("%w: rendered OCR captions exceed their resource limit", ErrInvalidInput)
	}
	if _, err := subtitle.Parse(rendered.Data, subtitle.Format(format)); err != nil {
		return nil, fmt.Errorf("%w: rendered OCR captions are invalid: %w", ErrInvalidInput, err)
	}
	return rendered.Data, nil
}
