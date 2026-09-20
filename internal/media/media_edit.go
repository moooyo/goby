package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	MaxSubtitleRemovalInputBytes int64 = 1 << 40
	MaxSubtitleRemovalTimeout          = 2 * time.Hour
	mediaEditOutputAllowance     int64 = 64 << 20
	mediaEditMaxPackets          int64 = 100_000_000
	mediaEditProofVersion              = 1
)

var (
	ErrSubtitleRemovalUnsupported = errors.New("subtitle removal cannot prove preservation")
	ErrSubtitleRemovalBudget      = errors.New("subtitle removal exceeded its resource budget")
	mediaEditSlots                = make(chan struct{}, 2)
)

// SubtitleRemovalOptions accepts trusted server configuration, never HTTP
// paths or arguments. Container is the original, authorized file extension.
type SubtitleRemovalOptions struct {
	FFmpegPath  string
	FFprobePath string
	Container   string
	StreamIndex int
	// Zero uses the source size plus 64 MiB. RLIMIT_FSIZE enforces the ceiling
	// before a write, including sparse writes and trailer seeks.
	MaxOutputBytes int64
	// Zero uses the two-hour maximum for the entire operation, including its
	// concurrency wait, complete packet scans and repeated file hashing.
	Timeout time.Duration
}

// MediaEditStreamEvidence proves the ordered packet payloads and presentation
// clocks of one retained stream. Attachment bytes use ExtradataSHA256 instead.
type MediaEditStreamEvidence struct {
	SourceIndex     int    `json:"source_index"`
	CandidateIndex  int    `json:"candidate_index"`
	CodecType       string `json:"codec_type"`
	Packets         int64  `json:"packets"`
	PayloadSHA256   string `json:"payload_sha256"`
	TimingSHA256    string `json:"timing_sha256"`
	ExtradataSHA256 string `json:"extradata_sha256,omitempty"`
}

// MediaEditWriterChange records only regenerated container-writer identity.
// User metadata and per-stream encoder tags are never covered by this exception.
type MediaEditWriterChange struct {
	Field         string `json:"field"`
	Before        string `json:"before"`
	After         string `json:"after"`
	BeforePresent bool   `json:"before_present"`
	AfterPresent  bool   `json:"after_present"`
}

// SubtitleRemovalEvidence concerns a staged file only. It grants no authority
// to replace a source; the library must revalidate its journal, root binding,
// source identity and digest before atomically publishing the candidate.
type SubtitleRemovalEvidence struct {
	Version         int                       `json:"version"`
	Container       string                    `json:"container"`
	RemovedIndex    int                       `json:"removed_index"`
	SourceBytes     int64                     `json:"source_bytes"`
	CandidateBytes  int64                     `json:"candidate_bytes"`
	SourceSHA256    string                    `json:"source_sha256"`
	CandidateSHA256 string                    `json:"candidate_sha256"`
	MetadataSHA256  string                    `json:"metadata_sha256"`
	RetainedStreams []MediaEditStreamEvidence `json:"retained_streams"`
	WriterChanges   []MediaEditWriterChange   `json:"writer_changes,omitempty"`
}

// RemuxSubtitleRemoval borrows two distinct regular descriptors without
// changing their offsets. The candidate must be empty, opened read/write and
// caller-owned. The function removes one absolute embedded subtitle index,
// copies all other streams, then independently proves their packet payloads,
// clocks, metadata, chapters, dispositions and codec extradata. Unknown proof
// cases fail closed and leave an unpublished candidate for caller cleanup.
// Supported profiles are single-segment Matroska with plain metadata and
// ordinary self-contained AVC/AAC/mov_text MP4 without nontrivial edit lists.
// Container writer identities are the only permitted metadata changes and
// appear explicitly in WriterChanges; no user or stream tags are exempted.
// Linux util-linux /usr/bin/prlimit is required to enforce a hard file-size
// ceiling on the seekable candidate, including MP4 trailer rewrites.
func RemuxSubtitleRemoval(ctx context.Context, input, candidate *os.File, options SubtitleRemovalOptions) (evidence SubtitleRemovalEvidence, resultErr error) {
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if runtime.GOOS != "linux" || input == nil || candidate == nil || options.StreamIndex < 0 || options.StreamIndex > 4095 {
		return evidence, ErrSubtitleRemovalUnsupported
	}
	for _, executable := range []string{options.FFmpegPath, options.FFprobePath} {
		if strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
			return evidence, ErrSubtitleRemovalUnsupported
		}
	}
	if options.Container != "mkv" && options.Container != "mka" && options.Container != "mp4" {
		return evidence, ErrSubtitleRemovalUnsupported
	}
	if options.Timeout == 0 {
		options.Timeout = MaxSubtitleRemovalTimeout
	}
	if options.Timeout < time.Second || options.Timeout > MaxSubtitleRemovalTimeout {
		return evidence, ErrSubtitleRemovalBudget
	}
	operationContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	select {
	case mediaEditSlots <- struct{}{}:
		defer func() { <-mediaEditSlots }()
	case <-operationContext.Done():
		return evidence, operationContext.Err()
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxSubtitleRemovalInputBytes {
		return evidence, ErrSubtitleRemovalUnsupported
	}
	outputBefore, err := candidate.Stat()
	if err != nil || !outputBefore.Mode().IsRegular() || outputBefore.Size() != 0 || os.SameFile(before, outputBefore) {
		return evidence, ErrSubtitleRemovalUnsupported
	}
	if options.MaxOutputBytes == 0 {
		options.MaxOutputBytes = before.Size() + mediaEditOutputAllowance
	}
	if options.MaxOutputBytes <= 0 || options.MaxOutputBytes > MaxSubtitleRemovalInputBytes+mediaEditOutputAllowance {
		return evidence, ErrSubtitleRemovalBudget
	}
	defer func() {
		if err := mediaEditCheckUnchanged(input, before); err != nil {
			evidence, resultErr = SubtitleRemovalEvidence{}, err
		}
		if err := operationContext.Err(); err != nil {
			evidence, resultErr = SubtitleRemovalEvidence{}, err
		}
	}()
	sourceWriter, err := mediaEditContainerAdmission(operationContext, input, before.Size(), options.Container)
	if err != nil {
		return evidence, err
	}
	source, err := probeMediaEditDocument(operationContext, options.FFprobePath, input)
	if err != nil {
		return evidence, err
	}
	if err := source.admit(options.Container, options.StreamIndex); err != nil {
		return evidence, err
	}
	sourceDigest, err := mediaEditFileDigest(operationContext, input, before.Size())
	if err != nil {
		return evidence, err
	}
	sourcePackets, err := probeMediaEditPackets(operationContext, options.FFprobePath, input, source.timeBases())
	if err != nil {
		return evidence, err
	}
	args, err := buildMediaEditRemuxArgs(source, options)
	if err != nil {
		return evidence, err
	}
	if err := runMediaEditRemux(operationContext, options.FFmpegPath, input, candidate, options.MaxOutputBytes, args); err != nil {
		return evidence, err
	}
	if err := candidate.Sync(); err != nil {
		return evidence, fmt.Errorf("sync subtitle removal candidate: %w", err)
	}
	outputAfter, err := candidate.Stat()
	if err != nil || !os.SameFile(outputBefore, outputAfter) || outputAfter.Size() <= 0 || outputAfter.Size() > options.MaxOutputBytes {
		return evidence, ErrSubtitleRemovalBudget
	}
	candidateWriter, err := mediaEditContainerAdmission(operationContext, candidate, outputAfter.Size(), options.Container)
	if err != nil {
		return evidence, fmt.Errorf("subtitle removal candidate profile: %w", err)
	}
	stagedDigest, err := mediaEditFileDigest(operationContext, candidate, outputAfter.Size())
	if err != nil {
		return evidence, err
	}
	staged, err := probeMediaEditDocument(operationContext, options.FFprobePath, candidate)
	if err != nil {
		return evidence, err
	}
	metadataDigest, pairs, err := compareMediaEditDocuments(source, staged, options.Container, options.StreamIndex)
	if err != nil {
		return evidence, err
	}
	writerChanges, err := mediaEditWriterChanges(source, staged, sourceWriter, candidateWriter)
	if err != nil {
		return evidence, err
	}
	stagedPackets, err := probeMediaEditPackets(operationContext, options.FFprobePath, candidate, staged.timeBases())
	if err != nil {
		return evidence, err
	}
	for index := range pairs {
		pair := &pairs[index]
		original, originalOK := sourcePackets[pair.SourceIndex]
		remuxed, remuxedOK := stagedPackets[pair.CandidateIndex]
		if !originalOK || !remuxedOK || original.Packets != remuxed.Packets || original.PayloadSHA256 != remuxed.PayloadSHA256 || original.TimingSHA256 != remuxed.TimingSHA256 {
			return evidence, fmt.Errorf("%w: retained stream %d packet evidence differs", ErrSubtitleRemovalUnsupported, pair.SourceIndex)
		}
		if pair.CodecType != "attachment" && original.Packets == 0 {
			return evidence, fmt.Errorf("%w: retained stream %d has no packet evidence", ErrSubtitleRemovalUnsupported, pair.SourceIndex)
		}
		pair.Packets, pair.PayloadSHA256, pair.TimingSHA256 = original.Packets, original.PayloadSHA256, original.TimingSHA256
	}
	finalDigest, err := mediaEditFileDigest(operationContext, candidate, outputAfter.Size())
	if err != nil || finalDigest != stagedDigest {
		return evidence, fmt.Errorf("%w: candidate changed during proof", ErrSubtitleRemovalUnsupported)
	}
	if err := mediaEditCheckUnchanged(candidate, outputAfter); err != nil {
		return evidence, err
	}
	return SubtitleRemovalEvidence{Version: mediaEditProofVersion, Container: options.Container, RemovedIndex: options.StreamIndex,
		SourceBytes: before.Size(), CandidateBytes: outputAfter.Size(), SourceSHA256: sourceDigest,
		CandidateSHA256: stagedDigest, MetadataSHA256: metadataDigest, RetainedStreams: pairs, WriterChanges: writerChanges}, nil
}

func mediaEditCheckUnchanged(file *os.File, before os.FileInfo) error {
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || FileChangeTime(before) != FileChangeTime(after) {
		return fmt.Errorf("%w: descriptor changed during subtitle removal", ErrSubtitleRemovalUnsupported)
	}
	return nil
}

func mediaEditFileDigest(ctx context.Context, file *os.File, size int64) (string, error) {
	reader := io.NewSectionReader(file, 0, size)
	digest := sha256.New()
	buffer := make([]byte, 128<<10)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(buffer)
		read += int64(n)
		_, _ = digest.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if read != size {
		return "", fmt.Errorf("%w: descriptor length changed during hashing", ErrSubtitleRemovalUnsupported)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func mediaEditJSONDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
