package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type mediaEditMatroskaDurationTag struct {
	TrackUID      uint64
	Value         string
	Offset, Bytes int64
}

type mediaEditDurationPatch struct {
	Offset        int64
	Before, After []byte
}

// Snapshot and full hash deliberately survive the return to the caller. A
// later candidate Stat must not adopt changed padding or appended Void bytes
// as a new baseline after the narrow restoration proof has completed.
type mediaEditDurationRestoration struct {
	Snapshot        os.FileInfo
	SHA256          string
	PreservedSHA256 string
	Tags            int
}

func mediaEditCanonicalDurationNS(value string) (int64, error) {
	if len(value) != 18 || value[2] != ':' || value[5] != ':' || value[8] != '.' {
		return 0, mediaEditContainerError("DURATION is outside the canonical bounded time format")
	}
	for index, character := range value {
		if index != 2 && index != 5 && index != 8 && (character < '0' || character > '9') {
			return 0, mediaEditContainerError("DURATION contains an invalid time field")
		}
	}
	hours, _ := strconv.ParseInt(value[:2], 10, 64)
	minutes, _ := strconv.ParseInt(value[3:5], 10, 64)
	seconds, _ := strconv.ParseInt(value[6:8], 10, 64)
	fraction, _ := strconv.ParseInt(value[9:], 10, 64)
	if minutes >= 60 || seconds >= 60 {
		return 0, mediaEditContainerError("DURATION time fields are out of range")
	}
	return (hours*3600+minutes*60+seconds)*1_000_000_000 + fraction, nil
}

func mediaEditDurationTags(proof mediaEditContainerProof) (map[uint64]mediaEditMatroskaDurationTag, error) {
	result := map[uint64]mediaEditMatroskaDurationTag{}
	for _, tag := range proof.MatroskaDurationTags {
		if tag.TrackUID == 0 || tag.Offset < 0 || tag.Bytes < 1 || tag.Bytes > 1<<20 {
			return nil, mediaEditContainerError("DURATION tag has an invalid bounded extent")
		}
		if _, exists := result[tag.TrackUID]; exists {
			return nil, mediaEditContainerError("DURATION track target is ambiguous")
		}
		result[tag.TrackUID] = tag
	}
	return result, nil
}

func mediaEditDurationCRCScopes(proof mediaEditContainerProof, tags []mediaEditMatroskaDurationTag) []mediaEditMatroskaCRC {
	result := []mediaEditMatroskaCRC{}
	for _, scope := range proof.matroskaCRCs {
		for _, tag := range tags {
			if scope.ParentStart <= tag.Offset && tag.Offset+tag.Bytes <= scope.ParentEnd {
				result = append(result, scope)
				break
			}
		}
	}
	return result
}

// This repair preserves the source's actual tag, not a relaxed comparison.
// It is limited to the observed one-millisecond video DefaultDuration loss,
// with unchanged raw DefaultDuration and exact raw/probe UID/index bindings.
func restoreMediaEditMatroskaDurations(ctx context.Context, sourceFile, candidate *os.File, sourceProof, candidateProof mediaEditContainerProof,
	source, staged mediaEditDocument, removedIndex int, baseline os.FileInfo) (*mediaEditDurationRestoration, error) {
	if ctx == nil || sourceFile == nil || candidate == nil {
		return nil, mediaEditContainerError("invalid DURATION restoration descriptor or context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Avoid imposing the delayed-AAC codec-private binding profile on files
	// whose generated duration metadata already matches exactly.
	changed := false
	position := 0
	for _, stream := range source.Streams {
		index, _ := mediaEditInteger(stream["index"])
		if int(index) == removedIndex {
			continue
		}
		if position >= len(staged.Streams) {
			return nil, mediaEditContainerError("DURATION candidate inventory is incomplete")
		}
		before, err := mediaEditTags(stream["tags"])
		if err != nil {
			return nil, err
		}
		after, err := mediaEditTags(staged.Streams[position]["tags"])
		if err != nil {
			return nil, err
		}
		bv, bp := before["duration"]
		av, ap := after["duration"]
		changed = changed || bv != av || bp != ap
		position++
	}
	if !changed {
		return nil, nil
	}
	if baseline == nil || baseline.Size() <= 0 || sourceProof.MatroskaTimestampScaleNS != 1_000_000 || candidateProof.MatroskaTimestampScaleNS != 1_000_000 {
		return nil, mediaEditContainerError("DURATION restoration requires the proven millisecond container profile")
	}
	beforeBinding, err := bindMediaEditMatroskaTracks(sourceProof, source)
	if err != nil {
		return nil, err
	}
	afterBinding, err := bindMediaEditMatroskaTracks(candidateProof, staged)
	if err != nil {
		return nil, err
	}
	beforeTags, err := mediaEditDurationTags(sourceProof)
	if err != nil {
		return nil, err
	}
	afterTags, err := mediaEditDurationTags(candidateProof)
	if err != nil {
		return nil, err
	}
	predicted := staged
	predicted.Streams = append([]map[string]any(nil), staged.Streams...)
	patches := []mediaEditDurationPatch{}
	sourceTargets, candidateTargets := []mediaEditMatroskaDurationTag{}, []mediaEditMatroskaDurationTag{}
	position = 0
	for _, stream := range source.Streams {
		index, _ := mediaEditInteger(stream["index"])
		if int(index) == removedIndex {
			continue
		}
		before, _ := mediaEditTags(stream["tags"])
		after, _ := mediaEditTags(staged.Streams[position]["tags"])
		originalValue, originalPresent := before["duration"]
		candidateValue, candidatePresent := after["duration"]
		if originalValue != candidateValue || originalPresent != candidatePresent {
			rawBefore, beforeOK := beforeBinding[int(index)]
			rawAfter, afterOK := afterBinding[position]
			original, originalOK := beforeTags[rawBefore.UID]
			target, targetOK := afterTags[rawAfter.UID]
			if !beforeOK || !afterOK || rawBefore.TrackType != 1 || rawAfter.TrackType != 1 || rawBefore.CodecID != rawAfter.CodecID ||
				rawBefore.DefaultDurationNS == 0 || rawBefore.DefaultDurationNS != rawAfter.DefaultDurationNS || rawBefore.DefaultDurationNS%1_000_000 == 0 ||
				!originalOK || !targetOK || !originalPresent || !candidatePresent || original.Value != originalValue || target.Value != candidateValue ||
				original.Bytes != target.Bytes || target.Bytes != 19 || target.Offset > baseline.Size()-target.Bytes {
				return nil, mediaEditContainerError("DURATION cannot be restored through an exact video tag binding")
			}
			beforeNS, err := mediaEditCanonicalDurationNS(original.Value)
			if err != nil {
				return nil, err
			}
			afterNS, err := mediaEditCanonicalDurationNS(target.Value)
			if err != nil || beforeNS-afterNS != 1_000_000 {
				return nil, mediaEditContainerError("DURATION differs beyond the proven default-duration rounding case")
			}
			oldBytes, newBytes := make([]byte, target.Bytes), make([]byte, target.Bytes)
			copy(oldBytes, target.Value)
			copy(newBytes, original.Value)
			patches = append(patches, mediaEditDurationPatch{Offset: target.Offset, Before: oldBytes, After: newBytes})
			sourceTargets, candidateTargets = append(sourceTargets, original), append(candidateTargets, target)
			clonedStream := map[string]any{}
			for key, value := range staged.Streams[position] {
				clonedStream[key] = value
			}
			clonedTags := map[string]any{}
			stagedTags, ok := staged.Streams[position]["tags"].(map[string]any)
			if !ok {
				return nil, mediaEditContainerError("DURATION candidate tags are not completely represented")
			}
			for key, value := range stagedTags {
				if strings.EqualFold(key, "duration") {
					clonedTags[key] = original.Value
				} else {
					clonedTags[key] = value
				}
			}
			clonedStream["tags"] = clonedTags
			predicted.Streams[position] = clonedStream
		}
		position++
	}
	if len(patches) == 0 || len(patches) > mediaEditMaxStreams {
		return nil, mediaEditContainerError("DURATION restoration has no bounded target")
	}
	if _, _, err := compareMediaEditDocuments(source, predicted, "mkv", removedIndex); err != nil {
		return nil, err
	}
	if err := mediaEditCheckUnchanged(candidate, baseline); err != nil {
		return nil, err
	}
	sourceCRCs := mediaEditDurationCRCScopes(sourceProof, sourceTargets)
	candidateCRCs := mediaEditDurationCRCScopes(candidateProof, candidateTargets)
	for _, scopes := range [][]mediaEditMatroskaCRC{sourceCRCs, candidateCRCs} {
		for _, scope := range scopes {
			if scope.Depth == 0 {
				return nil, mediaEditContainerError("DURATION restoration cannot rewrite a root-level CRC without a parent element")
			}
		}
	}
	if err := verifyMediaEditMatroskaCRCs(ctx, sourceFile, sourceCRCs); err != nil {
		return nil, err
	}
	if err := verifyMediaEditMatroskaCRCs(ctx, candidate, candidateCRCs); err != nil {
		return nil, err
	}
	for index, target := range sourceTargets {
		actual := make([]byte, target.Bytes)
		if _, err := sourceFile.ReadAt(actual, target.Offset); err != nil {
			return nil, mediaEditDurationIOError(err)
		}
		if !bytes.Equal(actual, patches[index].After) {
			return nil, mediaEditContainerError("source DURATION bytes changed")
		}
	}
	for _, patch := range patches {
		actual := make([]byte, len(patch.Before))
		if _, err := candidate.ReadAt(actual, patch.Offset); err != nil {
			return nil, mediaEditDurationIOError(err)
		}
		if !bytes.Equal(actual, patch.Before) {
			return nil, mediaEditContainerError("candidate DURATION bytes changed")
		}
	}
	ranges := make([]mediaEditDurationRange, 0, len(patches)+len(candidateCRCs))
	for _, patch := range patches {
		ranges = append(ranges, mediaEditDurationRange{patch.Offset, patch.Offset + int64(len(patch.After))})
	}
	for _, scope := range candidateCRCs {
		ranges = append(ranges, mediaEditDurationRange{scope.ValueOffset, scope.ValueOffset + 4})
	}
	preserved, err := mediaEditDurationUnchangedDigest(ctx, candidate, baseline.Size(), ranges)
	if err != nil {
		return nil, err
	}
	if err := mediaEditCheckUnchanged(candidate, baseline); err != nil {
		return nil, err
	}
	for _, patch := range patches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n, err := candidate.WriteAt(patch.After, patch.Offset); err != nil || n != len(patch.After) {
			return nil, mediaEditDurationIOError(err)
		}
	}
	if err := rewriteMediaEditMatroskaCRCs(ctx, candidate, candidateCRCs); err != nil {
		return nil, err
	}
	if err := candidate.Sync(); err != nil {
		return nil, mediaEditDurationIOError(err)
	}
	after, err := candidate.Stat()
	if err != nil {
		return nil, mediaEditDurationIOError(err)
	}
	if !os.SameFile(baseline, after) || after.Size() != baseline.Size() {
		return nil, mediaEditContainerError("DURATION restoration changed candidate identity or size")
	}
	return proveMediaEditMatroskaDurationRestoration(ctx, candidate, after, patches, candidateCRCs, ranges, preserved)
}

func proveMediaEditMatroskaDurationRestoration(ctx context.Context, candidate *os.File, after os.FileInfo,
	patches []mediaEditDurationPatch, candidateCRCs []mediaEditMatroskaCRC, ranges []mediaEditDurationRange, preserved string) (*mediaEditDurationRestoration, error) {
	// The rewrite's earlier CRC check precedes this snapshot. Recheck inside
	// the returned proof baseline so a changed checksum cannot be adopted as
	// an excluded byte range before the final full-file hash is established.
	if err := verifyMediaEditMatroskaCRCs(ctx, candidate, candidateCRCs); err != nil {
		return nil, err
	}
	for _, patch := range patches {
		actual := make([]byte, len(patch.After))
		if _, err := candidate.ReadAt(actual, patch.Offset); err != nil {
			return nil, mediaEditDurationIOError(err)
		}
		if !bytes.Equal(actual, patch.After) {
			return nil, mediaEditContainerError("DURATION restoration did not retain the exact source bytes")
		}
	}
	got, err := mediaEditDurationUnchangedDigest(ctx, candidate, after.Size(), ranges)
	if err != nil {
		return nil, err
	}
	if got != preserved {
		return nil, mediaEditContainerError("DURATION restoration changed bytes outside its declared tag and CRC ranges")
	}
	full, err := mediaEditFileDigest(ctx, candidate, after.Size())
	if err != nil {
		return nil, err
	}
	if err := mediaEditCheckUnchanged(candidate, after); err != nil {
		return nil, err
	}
	return &mediaEditDurationRestoration{Snapshot: after, SHA256: full, PreservedSHA256: preserved, Tags: len(patches)}, nil
}

type mediaEditDurationRange struct{ Start, End int64 }

func mediaEditDurationUnchangedDigest(ctx context.Context, file *os.File, size int64, excluded []mediaEditDurationRange) (string, error) {
	if ctx == nil || file == nil || size <= 0 || size > MaxSubtitleRemovalInputBytes+mediaEditOutputAllowance || len(excluded) > mediaEditMaxStreams+mediaEditMatroskaCRCMaxScopes {
		return "", mediaEditContainerError("invalid DURATION preservation digest extent")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ranges := append([]mediaEditDurationRange(nil), excluded...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	previous := int64(0)
	for _, interval := range ranges {
		if interval.Start < previous || interval.End <= interval.Start || interval.End > size {
			return "", mediaEditContainerError("invalid DURATION restoration range")
		}
		previous = interval.End
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("goby-matroska-duration-restoration-v1\x00"))
	var frame [16]byte
	binary.BigEndian.PutUint64(frame[:8], uint64(size))
	binary.BigEndian.PutUint64(frame[8:], uint64(len(ranges)))
	_, _ = digest.Write(frame[:])
	buffer := make([]byte, 128<<10)
	position := int64(0)
	ranges = append(ranges, mediaEditDurationRange{size, size})
	for _, interval := range ranges {
		binary.BigEndian.PutUint64(frame[:8], uint64(position))
		binary.BigEndian.PutUint64(frame[8:], uint64(interval.Start))
		_, _ = digest.Write(frame[:])
		for position < interval.Start {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			chunk := buffer[:min(int64(len(buffer)), interval.Start-position)]
			if _, err := file.ReadAt(chunk, position); err != nil {
				return "", mediaEditDurationIOError(err)
			}
			_, _ = digest.Write(chunk)
			position += int64(len(chunk))
		}
		position = interval.End
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func mediaEditDurationIOError(err error) error {
	if err == nil {
		err = errors.New("short candidate write")
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}
	return fmt.Errorf("%w: Matroska DURATION restoration I/O: %w", ErrSubtitleRemovalUnsupported, err)
}
