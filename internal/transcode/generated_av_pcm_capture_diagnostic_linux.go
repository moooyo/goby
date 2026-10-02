//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"syscall"

	"github.com/moooyo/goby/internal/media"
)

// Both fields are named and private. Promoting os.File.ReadFrom or a backing
// buffer fast path would bypass Write's byte/cancellation checks during exec copy.
type generatedAVPCMCaptureWriter struct {
	pcm     *generatedAVPCMWriter
	file    *os.File
	limit   int64
	written int64
	cancel  context.CancelFunc
	err     error
}

func (writer *generatedAVPCMCaptureWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	if int64(len(data)) > writer.limit-writer.written {
		writer.err = ErrTimelineLimit
	} else {
		count, err := writer.pcm.Write(data)
		if err != nil {
			writer.err = err
		} else if count != len(data) {
			writer.err = io.ErrShortWrite
		}
	}
	if writer.err != nil {
		writer.cancel()
		return 0, writer.err
	}
	count, err := writer.file.Write(data)
	writer.written += int64(count)
	if err != nil {
		writer.err = err
	} else if count != len(data) {
		writer.err = io.ErrShortWrite
	}
	if writer.err != nil {
		writer.cancel()
	}
	return count, writer.err
}

func generatedAVPCMCaptureFinalFile(ctx context.Context, file *os.File, before os.FileInfo, written int64) error {
	if err := file.Sync(); err != nil {
		return err
	}
	after, err := file.Stat()
	offset, offsetErr := file.Seek(0, io.SeekCurrent)
	if err != nil || offsetErr != nil || !os.SameFile(before, after) || after.Size() != written || offset != written || after.Mode().Perm() != 0600 {
		return ErrInvalidInput
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return ErrInvalidInput
	}
	return ctx.Err()
}

// MeasureGeneratedAVPCMCaptureDiagnostic replaces the former PCM measurement
// with one governed streaming decoder. Source role allows source itself as its
// unique copied input; other roles require distinct ordered held output parts.
// All parts and the encoded source remain borrowed, offset-stable and held.
func MeasureGeneratedAVPCMCaptureDiagnostic(ctx context.Context, source *os.File, files []*os.File, options GeneratedAVPCMCaptureOptions) (result GeneratedAVPCMCaptureDiagnostic, failure error) {
	result.Role, result.Stage = options.Role, "preflight"
	if generatedAVPCMCaptureAliasesBorrowed(source, files, options.Capture) {
		return result, ErrInvalidInput
	}
	defer func() {
		if options.Capture != nil {
			if err := options.Capture.Close(); err != nil {
				failure = errors.Join(failure, err)
				if result.Stage == "complete" {
					result.Stage = "capture_close"
				}
			}
		}
		if failure != nil {
			result.Complete, result.PCM.Complete = false, false
			result.SourceIdentityUnchanged, result.InputIdentitiesUnchanged = false, false
		}
	}()
	if ctx == nil || options.Capture == nil || options.AcquireProbe == nil || options.ExpectedSourceSHA256 == ([32]byte{}) || options.Channels < 1 || options.Channels > 2 ||
		options.SourceCertificate.Audio.SampleRate != 48000 || options.SourceCertificate.Audio.Channels != options.Channels {
		return result, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	limit, err := generatedAVPCMCaptureLimit(options.Role, len(files))
	if err != nil {
		return result, err
	}
	result.LimitBytes = limit
	// Preserve each actual borrowed descriptor's offset fence, including a
	// separate source-role descriptor naming the same source inode.
	inputs := []*os.File{source}
	for _, file := range files {
		if file != source {
			inputs = append(inputs, file)
		}
	}
	fence, err := generatedAVDiagnosticOuterFence(inputs)
	if err != nil {
		return result, err
	}
	sourceInfo, sourceIdentity, err := generatedAVAssociationInputInfo(source)
	if err != nil {
		return result, err
	}
	finalFence := func() error {
		if err := fence(); err != nil {
			return err
		}
		return ValidateGeneratedMP4AVSourceEndpointIdentity(source, options.SourceCertificate)
	}
	if err := finalFence(); err != nil {
		return result, err
	}
	var partInfo []os.FileInfo
	var total int64
	for _, file := range files {
		info, identity, err := generatedAVAssociationInputInfo(file)
		if err != nil {
			return result, err
		}
		if os.SameFile(sourceInfo, info) != (options.Role == GeneratedAVPCMCaptureSource) {
			return result, ErrInvalidInput
		}
		for _, prior := range partInfo {
			if os.SameFile(prior, info) {
				return result, ErrInvalidInput
			}
		}
		if info.Size() > generatedAVTransportBytes-total {
			return result, ErrTimelineLimit
		}
		total += info.Size()
		partInfo = append(partInfo, info)
		result.InputIdentities = append(result.InputIdentities, identity)
	}
	captureInfo, err := generatedAVAssociationCaptureInfo(options.Capture, inputs)
	if err != nil {
		return result, err
	}
	result.Stage = "source_hash"
	sourceHash, err := generatedAVAssociationSourceHash(ctx, source, sourceInfo.Size())
	if err != nil {
		return result, err
	}
	if sourceHash != options.ExpectedSourceSHA256 {
		return result, ErrInvalidInput
	}
	result.SourceBytes, result.SourceSHA256, result.SourceIdentity = sourceInfo.Size(), sourceHash, sourceIdentity
	if err := finalFence(); err != nil {
		return result, err
	}
	result.Stage = "probe_admission"
	release, err := options.AcquireProbe(ctx)
	if release != nil {
		defer release()
	}
	if err != nil {
		return result, err
	}
	if release == nil {
		return result, ErrInvalidOptions
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pcm, err := newGeneratedAVPCMWriter(options.Channels, limit)
	if err != nil {
		return result, err
	}
	pcm.cancel = cancel
	writer := &generatedAVPCMCaptureWriter{pcm: pcm, file: options.Capture, limit: limit, cancel: cancel}
	// Identical media arguments to the former streaming PCM measurement. No
	// -ar/-ac, nominal clipping, generated timestamps or extra output is added.
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats, "-i", "pipe:0", "-map", "0:a:0", "-vn", "-sn", "-dn", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1"}
	result.Stage = "joined_pcm"
	count, hashes, err := generatedAVDiagnosticDecode(processCtx, options.FFmpegPath, files, args, writer)
	if err := errors.Join(err, writer.err, pcm.err); err != nil {
		return result, err
	}
	if count != total || len(hashes) != len(files) || options.Role == GeneratedAVPCMCaptureSource && hashes[0] != sourceHash {
		return result, ErrInvalidInput
	}
	result.InputBytes, result.InputSHA256 = count, hashes
	if err := finalFence(); err != nil {
		return result, err
	}
	result.Stage = "pcm_finish"
	result.PCM, err = pcm.finish()
	if err != nil {
		return result, err
	}
	if result.PCM.Bytes != writer.written || result.PCM.Bytes > limit {
		return result, ErrInvalidInput
	}
	result.Stage = "capture_finish"
	if err := generatedAVPCMCaptureFinalFile(ctx, options.Capture, captureInfo, writer.written); err != nil {
		return result, err
	}
	result.CaptureWritten = true
	result.Stage = "final_fence"
	if err := finalFence(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.PCM.Complete, result.Complete, result.SourceIdentityUnchanged, result.InputIdentitiesUnchanged = true, true, true, true
	result.Stage = "complete"
	return result, nil
}
