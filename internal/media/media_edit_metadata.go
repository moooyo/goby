package media

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const mediaEditMaxStreams = 256

type mediaEditDocument struct {
	Streams  []map[string]any `json:"streams"`
	Chapters []map[string]any `json:"chapters"`
	Programs []map[string]any `json:"programs"`
	Format   map[string]any   `json:"format"`
}

func probeMediaEditDocument(ctx context.Context, executable string, file *os.File) (mediaEditDocument, error) {
	limiter, args, err := mediaEditLimitedTool(executable, 0)
	if err != nil {
		return mediaEditDocument{}, err
	}
	args = append(args, "-v", "error", "-max_alloc", "268435456", "-show_format", "-show_streams", "-show_chapters", "-show_programs", "-show_data_hash", "sha256",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2", "-i", "/proc/self/fd/3")
	output, err := runLimitedFilesOutput(ctx, MaxSubtitleRemovalTimeout, maxProbeOutput, limiter, []*os.File{file}, args...)
	if err != nil {
		if ctx.Err() != nil {
			return mediaEditDocument{}, ctx.Err()
		}
		if errors.Is(err, ErrOutputLimit) || mediaEditProcessHitFileLimit(err) {
			return mediaEditDocument{}, fmt.Errorf("%w: %w", ErrSubtitleRemovalBudget, err)
		}
		return mediaEditDocument{}, fmt.Errorf("probe subtitle removal metadata: %w", err)
	}
	if len(output.stderr) != 0 {
		return mediaEditDocument{}, fmt.Errorf("%w: metadata probe did not finish with clean diagnostics", ErrSubtitleRemovalUnsupported)
	}
	return parseMediaEditDocument(output.stdout)
}

func parseMediaEditDocument(data []byte) (mediaEditDocument, error) {
	var document mediaEditDocument
	if len(data) > maxProbeOutput {
		return document, ErrSubtitleRemovalBudget
	}
	if _, err := mediaEditDecodeJSON(data); err != nil {
		return document, fmt.Errorf("%w: invalid or ambiguous metadata JSON", ErrSubtitleRemovalUnsupported)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF || len(document.Programs) != 0 || len(document.Streams) < 1 || len(document.Streams) > mediaEditMaxStreams || len(document.Chapters) > 10000 || document.Format == nil {
		return document, fmt.Errorf("%w: unsupported metadata document", ErrSubtitleRemovalUnsupported)
	}
	indexes := make(map[int]bool, len(document.Streams))
	for _, stream := range document.Streams {
		index, err := mediaEditInteger(stream["index"])
		if err != nil || index < 0 || index > 4095 || indexes[int(index)] {
			return document, fmt.Errorf("%w: invalid stream index", ErrSubtitleRemovalUnsupported)
		}
		indexes[int(index)] = true
		if _, err := mediaEditStreamSignature(stream); err != nil {
			return document, err
		}
	}
	if _, err := mediaEditFormatSignature(document.Format); err != nil {
		return document, err
	}
	if _, err := mediaEditChapterSignatures(document.Chapters); err != nil {
		return document, err
	}
	return document, nil
}

func (document mediaEditDocument) admit(container string, removedIndex int) error {
	format, _ := document.Format["format_name"].(string)
	matroska := false
	mp4 := false
	for _, alias := range strings.Split(format, ",") {
		matroska = matroska || alias == "matroska"
		mp4 = mp4 || alias == "mp4"
	}
	if (container == "mkv" || container == "mka") && !matroska || container == "mp4" && !mp4 {
		return fmt.Errorf("%w: container does not match the authorized extension", ErrSubtitleRemovalUnsupported)
	}
	removed, retainedMedia := false, false
	for _, stream := range document.Streams {
		index, _ := mediaEditInteger(stream["index"])
		codecType, _ := stream["codec_type"].(string)
		codec, _ := stream["codec_name"].(string)
		if int(index) == removedIndex {
			if codecType != "subtitle" {
				return fmt.Errorf("%w: selected stream is not an embedded subtitle", ErrSubtitleRemovalUnsupported)
			}
			removed = true
		}
		attachedPicture := false
		if disposition, ok := stream["disposition"].(map[string]any); ok {
			flag, _ := mediaEditInteger(disposition["attached_pic"])
			attachedPicture = flag == 1
		}
		if codecType == "audio" || codecType == "video" && !attachedPicture {
			retainedMedia = true
		}
		if codec == "" || codecType != "audio" && codecType != "video" && codecType != "subtitle" && codecType != "attachment" {
			return fmt.Errorf("%w: unsupported stream type", ErrSubtitleRemovalUnsupported)
		}
		if container == "mka" && codecType == "video" && !attachedPicture {
			return fmt.Errorf("%w: MKA profile contains video", ErrSubtitleRemovalUnsupported)
		}
		if container == "mp4" {
			allowed := map[string]string{"h264": "video", "hevc": "video", "av1": "video", "aac": "audio", "alac": "audio", "ac3": "audio", "eac3": "audio", "mp3": "audio", "mov_text": "subtitle"}
			if allowed[codec] != codecType {
				return fmt.Errorf("%w: unsupported MP4 stream profile", ErrSubtitleRemovalUnsupported)
			}
		}
	}
	if !removed || !retainedMedia || len(document.Streams) < 2 {
		return fmt.Errorf("%w: selected subtitle or retained media is absent", ErrSubtitleRemovalUnsupported)
	}
	return nil
}

func (document mediaEditDocument) timeBases() map[int]*big.Rat {
	result := make(map[int]*big.Rat, len(document.Streams))
	for _, stream := range document.Streams {
		index, _ := mediaEditInteger(stream["index"])
		base, _ := mediaEditTimeBase(stream["time_base"])
		result[int(index)] = base
	}
	return result
}

func buildMediaEditRemuxArgs(source mediaEditDocument, options SubtitleRemovalOptions) ([]string, error) {
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-y", "-threads", "1", "-max_alloc", "268435456", "-copyts",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2", "-i", "/proc/self/fd/3",
		"-map", "0", "-map", "-0:" + strconv.Itoa(options.StreamIndex), "-map_metadata", "0", "-map_chapters", "0", "-c", "copy",
		"-avoid_negative_ts", "disabled", "-max_interleave_delta", "1000000"}
	// Explicit dispositions prevent FFmpeg from silently making a previously
	// non-default first audio/subtitle stream default after the removal.
	outputIndex := 0
	for _, stream := range source.Streams {
		index, _ := mediaEditInteger(stream["index"])
		if int(index) == options.StreamIndex {
			continue
		}
		disposition, err := mediaEditDispositionArgument(stream["disposition"])
		if err != nil {
			return nil, err
		}
		args = append(args, "-map_metadata:s:"+strconv.Itoa(outputIndex), "0:s:"+strconv.FormatInt(index, 10), "-disposition:"+strconv.Itoa(outputIndex), disposition)
		outputIndex++
	}
	if options.Container == "mp4" {
		// Retain arbitrary format metadata in an mdta atom. The independent
		// signature still rejects any source key the muxer cannot round-trip.
		args = append(args, "-movflags", "use_metadata_tags", "-use_editlist", "0", "-f", "mp4")
	} else {
		args = append(args, "-f", "matroska")
	}
	return append(args, "/proc/self/fd/4"), nil
}

func mediaEditDispositionArgument(value any) (string, error) {
	disposition, ok := value.(map[string]any)
	if !ok || len(disposition) == 0 || len(disposition) > 64 {
		return "", fmt.Errorf("%w: missing stream dispositions", ErrSubtitleRemovalUnsupported)
	}
	var enabled []string
	for name, value := range disposition {
		if len(name) == 0 || len(name) > 64 || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz_") != "" {
			return "", fmt.Errorf("%w: invalid disposition name", ErrSubtitleRemovalUnsupported)
		}
		number, err := mediaEditInteger(value)
		if err != nil || number < 0 || number > 1 {
			return "", fmt.Errorf("%w: invalid disposition value", ErrSubtitleRemovalUnsupported)
		}
		if number == 1 {
			enabled = append(enabled, name)
		}
	}
	if len(enabled) == 0 {
		return "0", nil
	}
	sort.Strings(enabled)
	return strings.Join(enabled, "+"), nil
}

func compareMediaEditDocuments(source, candidate mediaEditDocument, container string, removedIndex int) (string, []MediaEditStreamEvidence, error) {
	if err := source.admit(container, removedIndex); err != nil {
		return "", nil, err
	}
	if len(candidate.Streams) != len(source.Streams)-1 || len(candidate.Programs) != 0 {
		return "", nil, fmt.Errorf("%w: candidate stream count changed", ErrSubtitleRemovalUnsupported)
	}
	sourceFormat, err := mediaEditFormatSignature(source.Format)
	if err != nil {
		return "", nil, err
	}
	candidateFormat, err := mediaEditFormatSignature(candidate.Format)
	if err != nil || !reflect.DeepEqual(sourceFormat, candidateFormat) {
		return "", nil, fmt.Errorf("%w: container metadata changed", ErrSubtitleRemovalUnsupported)
	}
	sourceChapters, err := mediaEditChapterSignatures(source.Chapters)
	if err != nil {
		return "", nil, err
	}
	candidateChapters, err := mediaEditChapterSignatures(candidate.Chapters)
	if err != nil || !reflect.DeepEqual(sourceChapters, candidateChapters) {
		return "", nil, fmt.Errorf("%w: chapters changed", ErrSubtitleRemovalUnsupported)
	}
	var pairs []MediaEditStreamEvidence
	var signatures []map[string]any
	for _, stream := range source.Streams {
		index, _ := mediaEditInteger(stream["index"])
		if int(index) == removedIndex {
			continue
		}
		output := candidate.Streams[len(pairs)]
		outputIndex, err := mediaEditInteger(output["index"])
		if err != nil || outputIndex != int64(len(pairs)) {
			return "", nil, fmt.Errorf("%w: candidate stream mapping differs", ErrSubtitleRemovalUnsupported)
		}
		originalSignature, err := mediaEditStreamSignature(stream)
		if err != nil {
			return "", nil, err
		}
		outputSignature, err := mediaEditStreamSignature(output)
		if err != nil || !reflect.DeepEqual(originalSignature, outputSignature) {
			return "", nil, fmt.Errorf("%w: retained stream %d metadata differs", ErrSubtitleRemovalUnsupported, index)
		}
		codecType, _ := stream["codec_type"].(string)
		extradata, _ := stream["extradata_hash"].(string)
		pairs = append(pairs, MediaEditStreamEvidence{SourceIndex: int(index), CandidateIndex: int(outputIndex), CodecType: codecType, ExtradataSHA256: strings.TrimPrefix(extradata, "SHA256:")})
		signatures = append(signatures, originalSignature)
	}
	digest, err := mediaEditJSONDigest(map[string]any{"version": mediaEditProofVersion, "format": sourceFormat, "chapters": sourceChapters, "streams": signatures})
	return digest, pairs, err
}

func mediaEditStreamSignature(stream map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(stream))
	// These fields are container-local identifiers or recomputable demuxer
	// statistics. Every retained packet's exact clock is proven separately.
	ignored := map[string]bool{"index": true, "id": true, "time_base": true, "start_pts": true, "start_time": true,
		"duration_ts": true, "duration": true, "bit_rate": true, "max_bit_rate": true, "nb_frames": true,
		"nb_read_frames": true, "nb_read_packets": true, "avg_frame_rate": true, "r_frame_rate": true}
	for key, value := range stream {
		if !ignored[key] {
			result[key] = value
		}
	}
	codecType, _ := stream["codec_type"].(string)
	if codecType != "attachment" {
		if _, err := mediaEditTimeBase(stream["time_base"]); err != nil {
			return nil, err
		}
	}
	if _, err := mediaEditDispositionArgument(stream["disposition"]); err != nil {
		return nil, err
	}
	size := int64(0)
	if value, ok := stream["extradata_size"]; ok {
		var err error
		size, err = mediaEditInteger(value)
		if err != nil || size < 0 || size > 256<<20 {
			return nil, fmt.Errorf("%w: invalid codec extradata size", ErrSubtitleRemovalUnsupported)
		}
	}
	if codecType == "attachment" && size == 0 {
		return nil, fmt.Errorf("%w: attachment bytes are not proven", ErrSubtitleRemovalUnsupported)
	}
	if size > 0 {
		digest, _ := stream["extradata_hash"].(string)
		decoded, err := hex.DecodeString(strings.TrimPrefix(digest, "SHA256:"))
		if !strings.HasPrefix(digest, "SHA256:") || err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("%w: missing codec extradata hash", ErrSubtitleRemovalUnsupported)
		}
	}
	tags, err := mediaEditTags(stream["tags"])
	if err != nil {
		return nil, err
	}
	result["tags"] = tags
	if err := mediaEditValidateStreamSideData(stream["side_data_list"]); err != nil {
		return nil, err
	}
	return result, nil
}

func mediaEditFormatSignature(format map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(format))
	for key, value := range format {
		switch key {
		case "filename", "nb_streams", "size", "bit_rate", "probe_score", "start_time", "duration":
		default:
			result[key] = value
		}
	}
	tags, err := mediaEditTags(format["tags"])
	if err != nil {
		return nil, err
	}
	// libavformat rewrites this generator identity even with map_metadata.
	// The change is separately recorded in the evidence, never silently lost.
	delete(tags, "encoder")
	result["tags"] = tags
	return result, nil
}

func mediaEditWriterChanges(source, candidate mediaEditDocument, sourceContainer, candidateContainer map[string]string) ([]MediaEditWriterChange, error) {
	before, err := mediaEditTags(source.Format["tags"])
	if err != nil {
		return nil, err
	}
	after, err := mediaEditTags(candidate.Format["tags"])
	if err != nil {
		return nil, err
	}
	var changes []MediaEditWriterChange
	add := func(field, before, after string, beforePresent, afterPresent bool) {
		if before != after || beforePresent != afterPresent {
			changes = append(changes, MediaEditWriterChange{Field: field, Before: before, After: after, BeforePresent: beforePresent, AfterPresent: afterPresent})
		}
	}
	original, originalPresent := before["encoder"]
	rewritten, rewrittenPresent := after["encoder"]
	add("GlobalEncoder", original, rewritten, originalPresent, rewrittenPresent)
	for _, field := range []string{"MuxingApp", "WritingApp"} {
		original, originalPresent := sourceContainer[field]
		rewritten, rewrittenPresent := candidateContainer[field]
		add(field, original, rewritten, originalPresent, rewrittenPresent)
	}
	return changes, nil
}

func mediaEditTags(value any) (map[string]string, error) {
	result := map[string]string{}
	if value == nil {
		return result, nil
	}
	tags, ok := value.(map[string]any)
	if !ok || len(tags) > 4096 {
		return nil, fmt.Errorf("%w: invalid metadata tags", ErrSubtitleRemovalUnsupported)
	}
	for key, raw := range tags {
		text, ok := raw.(string)
		key = strings.ToLower(key)
		if _, duplicate := result[key]; duplicate || !ok || len(key) > 4096 || len(text) > 1<<20 {
			return nil, fmt.Errorf("%w: invalid or ambiguous metadata tag", ErrSubtitleRemovalUnsupported)
		}
		result[key] = text
	}
	return result, nil
}

func mediaEditChapterSignatures(chapters []map[string]any) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(chapters))
	for _, chapter := range chapters {
		base, err := mediaEditTimeBase(chapter["time_base"])
		if err != nil {
			return nil, err
		}
		start, err := mediaEditInteger(chapter["start"])
		if err != nil {
			return nil, err
		}
		end, err := mediaEditInteger(chapter["end"])
		if err != nil || start < 0 || end < start {
			return nil, fmt.Errorf("%w: invalid chapter interval", ErrSubtitleRemovalUnsupported)
		}
		normalized := make(map[string]any, len(chapter))
		for key, value := range chapter {
			switch key {
			case "id", "time_base", "start", "end", "start_time", "end_time":
			default:
				normalized[key] = value
			}
		}
		normalized["start"] = new(big.Rat).Mul(new(big.Rat).SetInt64(start), base).RatString()
		normalized["end"] = new(big.Rat).Mul(new(big.Rat).SetInt64(end), base).RatString()
		tags, err := mediaEditTags(chapter["tags"])
		if err != nil {
			return nil, err
		}
		normalized["tags"] = tags
		result = append(result, normalized)
	}
	return result, nil
}

func mediaEditInteger(value any) (int64, error) {
	var text string
	switch value := value.(type) {
	case json.Number:
		text = value.String()
	case string:
		text = value
	default:
		return 0, fmt.Errorf("%w: invalid integer fact", ErrSubtitleRemovalUnsupported)
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid integer fact", ErrSubtitleRemovalUnsupported)
	}
	return number, nil
}

func mediaEditTimeBase(value any) (*big.Rat, error) {
	text, ok := value.(string)
	if !ok || len(text) > 64 || strings.Trim(text, "0123456789/") != "" || strings.Count(text, "/") != 1 {
		return nil, fmt.Errorf("%w: invalid media time base", ErrSubtitleRemovalUnsupported)
	}
	base, ok := new(big.Rat).SetString(text)
	if !ok || base.Sign() <= 0 || !base.Num().IsInt64() || !base.Denom().IsInt64() {
		return nil, fmt.Errorf("%w: invalid media time base", ErrSubtitleRemovalUnsupported)
	}
	return base, nil
}

func mediaEditValidateStreamSideData(value any) error {
	if value == nil {
		return nil
	}
	records, ok := value.([]any)
	if !ok || len(records) > 64 {
		return fmt.Errorf("%w: invalid stream side data", ErrSubtitleRemovalUnsupported)
	}
	// Unknown side-data records may hide bytes that ffprobe never renders.
	// Only records with a complete, explicit public representation are admitted.
	fields := map[string][]string{
		"Display Matrix":               {"displaymatrix", "rotation"},
		"Mastering display metadata":   {"red_x", "red_y", "green_x", "green_y", "blue_x", "blue_y", "white_point_x", "white_point_y", "min_luminance", "max_luminance"},
		"Content light level metadata": {"max_content", "max_average"},
		"DOVI configuration record":    {"dv_version_major", "dv_version_minor", "dv_profile", "dv_level", "rpu_present_flag", "el_present_flag", "bl_present_flag", "dv_bl_signal_compatibility_id"},
		"Stereo 3D":                    {"type", "inverted"},
	}
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: invalid stream side data", ErrSubtitleRemovalUnsupported)
		}
		kind, _ := record["side_data_type"].(string)
		expected, known := fields[kind]
		if kind == "DOVI configuration record" {
			if _, present := record["dv_md_compression"]; present {
				expected = append(append([]string(nil), expected...), "dv_md_compression")
			}
			data, err := json.Marshal(record)
			parsed, parseErr := parseDolbyVisionRecord(data)
			if err != nil || parseErr != nil || parsed == nil {
				return fmt.Errorf("%w: invalid Dolby Vision configuration", ErrSubtitleRemovalUnsupported)
			}
			for _, field := range []string{"dv_version_major", "dv_version_minor"} {
				version, err := mediaEditInteger(record[field])
				if err != nil || version < 0 || version > 255 {
					return fmt.Errorf("%w: invalid Dolby Vision version", ErrSubtitleRemovalUnsupported)
				}
			}
		}
		if !known || len(record) != len(expected)+1 {
			return fmt.Errorf("%w: stream side data cannot be completely proven", ErrSubtitleRemovalUnsupported)
		}
		for _, field := range expected {
			if value, ok := record[field]; !ok || value == nil {
				return fmt.Errorf("%w: incomplete stream side data", ErrSubtitleRemovalUnsupported)
			}
		}
	}
	return nil
}
