package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const mediaEditMP4CopyBufferBytes = 128 << 10

// runMediaEditMP4StructuralCopy stages the scanner's admitted removal plan
// without moving either borrowed descriptor's offset. Only the selected trak
// type and body change: the type becomes free and every body byte becomes zero.
// All sizes, offsets, other boxes and media payloads retain their original bytes.
// The returned digest binds the source's unchanged ranges and is independently
// reproduced from the candidate. Publication and final source identity checks
// remain the caller's responsibility.
func runMediaEditMP4StructuralCopy(ctx context.Context, input, candidate *os.File, plan mediaEditMP4RemovalPlan, maxBytes int64) (digest string, resultErr error) {
	if ctx == nil || input == nil || candidate == nil {
		return "", mediaEditMP4CopyReject("missing context or descriptor")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if plan.SourceBytes > MaxSubtitleRemovalInputBytes {
		return "", ErrSubtitleRemovalBudget
	}
	if plan.SourceBytes <= 0 || plan.RemovedTrackID == 0 || plan.RemovedTrackID > uint64(^uint32(0)) ||
		plan.TrackTypeOffset < 4 || plan.TrackTypeOffset > plan.SourceBytes-4 ||
		plan.TrackBodyOffset <= plan.TrackTypeOffset || plan.TrackBodyOffset >= plan.TrackEnd || plan.TrackEnd > plan.SourceBytes {
		return "", mediaEditMP4CopyReject("invalid removal plan")
	}
	headerTail := plan.TrackBodyOffset - plan.TrackTypeOffset
	if headerTail != 4 && headerTail != 12 {
		return "", mediaEditMP4CopyReject("invalid track header extent")
	}
	if maxBytes < plan.SourceBytes {
		return "", ErrSubtitleRemovalBudget
	}
	sourceBefore, err := input.Stat()
	if err != nil {
		return "", mediaEditMP4CopyIOError("source stat", err)
	}
	if sourceBefore.Size() > MaxSubtitleRemovalInputBytes {
		return "", ErrSubtitleRemovalBudget
	}
	if !sourceBefore.Mode().IsRegular() || sourceBefore.Size() != plan.SourceBytes {
		return "", mediaEditMP4CopyReject("source does not match removal plan")
	}
	candidateBefore, err := candidate.Stat()
	if err != nil {
		return "", mediaEditMP4CopyIOError("candidate stat", err)
	}
	if !candidateBefore.Mode().IsRegular() || candidateBefore.Size() != 0 || os.SameFile(sourceBefore, candidateBefore) {
		return "", mediaEditMP4CopyReject("candidate is not an independent empty regular file")
	}
	var emptyCheck [1]byte
	if n, err := candidate.ReadAt(emptyCheck[:], 0); err != io.EOF || n != 0 {
		if err != nil {
			return "", mediaEditMP4CopyIOError("candidate read admission", err)
		}
		return "", mediaEditMP4CopyReject("candidate is no longer empty")
	}
	defer func() {
		if err := ctx.Err(); err != nil {
			digest, resultErr = "", err
		}
		if err := mediaEditCheckUnchanged(input, sourceBefore); err != nil && resultErr == nil {
			digest, resultErr = "", err
		}
	}()
	var header [16]byte
	headerSize := headerTail + 4
	if err := mediaEditMP4CopyRead(input, header[:headerSize], plan.TrackTypeOffset-4, "source track header"); err != nil {
		return "", err
	}
	if string(header[4:8]) != "trak" {
		return "", mediaEditMP4CopyReject("selected track type changed")
	}
	trackSize := uint64(plan.TrackEnd - (plan.TrackTypeOffset - 4))
	if headerSize == 8 {
		declared := binary.BigEndian.Uint32(header[:4])
		if declared <= 1 || uint64(declared) != trackSize {
			return "", mediaEditMP4CopyReject("selected track size changed")
		}
	} else if binary.BigEndian.Uint32(header[:4]) != 1 || binary.BigEndian.Uint64(header[8:16]) != trackSize {
		return "", mediaEditMP4CopyReject("selected extended track size changed")
	}
	buffer := make([]byte, mediaEditMP4CopyBufferBytes)
	sourceDigest, err := mediaEditMP4CopyPass(ctx, input, candidate, plan, buffer)
	if err != nil {
		return "", err
	}
	candidateAfterCopy, err := candidate.Stat()
	if err != nil {
		return "", mediaEditMP4CopyIOError("candidate copied stat", err)
	}
	if !os.SameFile(candidateBefore, candidateAfterCopy) || !candidateAfterCopy.Mode().IsRegular() || candidateAfterCopy.Size() != plan.SourceBytes {
		return "", mediaEditMP4CopyReject("candidate copy has an unexpected extent")
	}
	candidateDigest, err := mediaEditMP4CopyPass(ctx, candidate, nil, plan, buffer)
	if err != nil {
		return "", err
	}
	if candidateDigest != sourceDigest {
		return "", mediaEditMP4CopyReject("candidate changed a preserved byte range")
	}
	if err := mediaEditCheckUnchanged(candidate, candidateAfterCopy); err != nil {
		return "", err
	}
	return sourceDigest, nil
}

// The caller invokes this again inside its final candidate-proof baseline.
// A successful copy alone does not authorize a later candidate snapshot: even
// a changed unreferenced payload or appended free box must invalidate the
// source-preserved digest associated with that snapshot's complete file hash.
func verifyMediaEditMP4StructuralCandidate(ctx context.Context, candidate *os.File, plan mediaEditMP4RemovalPlan, expectedDigest string) error {
	if ctx == nil || candidate == nil || len(expectedDigest) != 64 {
		return mediaEditMP4CopyReject("final preservation proof is missing")
	}
	if plan.SourceBytes <= 0 || plan.SourceBytes > MaxSubtitleRemovalInputBytes || plan.RemovedTrackID == 0 || plan.RemovedTrackID > uint64(^uint32(0)) ||
		plan.TrackTypeOffset < 4 || plan.TrackTypeOffset > plan.SourceBytes-4 || plan.TrackBodyOffset <= plan.TrackTypeOffset ||
		plan.TrackBodyOffset >= plan.TrackEnd || plan.TrackEnd > plan.SourceBytes ||
		(plan.TrackBodyOffset-plan.TrackTypeOffset != 4 && plan.TrackBodyOffset-plan.TrackTypeOffset != 12) {
		return mediaEditMP4CopyReject("final preservation plan is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return mediaEditMP4CopyReject("final preservation digest is invalid")
	}
	before, err := candidate.Stat()
	if err != nil {
		return mediaEditMP4CopyIOError("final candidate stat", err)
	}
	if !before.Mode().IsRegular() || before.Size() != plan.SourceBytes {
		return mediaEditMP4CopyReject("final candidate extent differs from the source")
	}
	digest, err := mediaEditMP4CopyPass(ctx, candidate, nil, plan, make([]byte, mediaEditMP4CopyBufferBytes))
	if err != nil {
		return err
	}
	if digest != expectedDigest {
		return mediaEditMP4CopyReject("final candidate changed a source-preserved byte range")
	}
	return mediaEditCheckUnchanged(candidate, before)
}

type mediaEditMP4CopyRegion struct {
	start, end int64
	kind       byte
}

// The digest is a domain string, source size, removed track ID and range count,
// then each preserved range's start, end and bytes. All integers are unsigned
// 64-bit big-endian. Empty ranges are framed too, so distinct layouts cannot
// share a digest merely because their concatenated preserved bytes are equal.
func mediaEditMP4CopyPass(ctx context.Context, input, output *os.File, plan mediaEditMP4RemovalPlan, buffer []byte) (string, error) {
	digest := sha256.New()
	_, _ = digest.Write([]byte("goby-media-edit-mp4-structural-copy-v1\x00"))
	var frame [24]byte
	binary.BigEndian.PutUint64(frame[0:8], uint64(plan.SourceBytes))
	binary.BigEndian.PutUint64(frame[8:16], plan.RemovedTrackID)
	binary.BigEndian.PutUint64(frame[16:24], 3)
	_, _ = digest.Write(frame[:])
	regions := [...]mediaEditMP4CopyRegion{
		{0, plan.TrackTypeOffset, 'p'},
		{plan.TrackTypeOffset, plan.TrackTypeOffset + 4, 't'},
		{plan.TrackTypeOffset + 4, plan.TrackBodyOffset, 'p'},
		{plan.TrackBodyOffset, plan.TrackEnd, 'z'},
		{plan.TrackEnd, plan.SourceBytes, 'p'},
	}
	readStage := "candidate proof read"
	if output != nil {
		readStage = "source copy read"
	}
	for _, region := range regions {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if region.kind == 'p' {
			binary.BigEndian.PutUint64(frame[:8], uint64(region.start))
			binary.BigEndian.PutUint64(frame[8:16], uint64(region.end))
			_, _ = digest.Write(frame[:16])
		}
		for offset := region.start; offset < region.end; {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			chunk := buffer[:min(int64(len(buffer)), region.end-offset)]
			if err := mediaEditMP4CopyRead(input, chunk, offset, readStage); err != nil {
				return "", err
			}
			switch region.kind {
			case 'p':
				_, _ = digest.Write(chunk)
			case 't':
				if output != nil {
					if !bytes.Equal(chunk, []byte("trak")) {
						return "", mediaEditMP4CopyReject("selected track type changed during copy")
					}
					copy(chunk, "free")
				} else if !bytes.Equal(chunk, []byte("free")) {
					return "", mediaEditMP4CopyReject("candidate free type changed")
				}
			case 'z':
				if output != nil {
					clear(chunk)
				} else {
					for _, value := range chunk {
						if value != 0 {
							return "", mediaEditMP4CopyReject("candidate removed track body is not zero")
						}
					}
				}
			}
			if output != nil {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				n, err := output.WriteAt(chunk, offset)
				if err != nil {
					return "", mediaEditMP4CopyIOError("candidate copy write", err)
				}
				if n != len(chunk) {
					return "", mediaEditMP4CopyIOError("candidate copy write", io.ErrShortWrite)
				}
			}
			offset += int64(len(chunk))
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func mediaEditMP4CopyRead(file *os.File, data []byte, offset int64, stage string) error {
	n, err := file.ReadAt(data, offset)
	if err != nil {
		return mediaEditMP4CopyIOError(stage, err)
	}
	if n != len(data) {
		return mediaEditMP4CopyIOError(stage, io.ErrUnexpectedEOF)
	}
	return nil
}

func mediaEditMP4CopyIOError(stage string, err error) error {
	for {
		var pathError *os.PathError
		if !errors.As(err, &pathError) {
			break
		}
		err = pathError.Err
	}
	return fmt.Errorf("%w: MP4 structural copy %s: %w", ErrSubtitleRemovalUnsupported, stage, err)
}

func mediaEditMP4CopyReject(reason string) error {
	return fmt.Errorf("%w: MP4 structural copy %s", ErrSubtitleRemovalUnsupported, reason)
}
