//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"strconv"
	"syscall"

	"github.com/moooyo/goby/internal/media"
)

func generatedAVAssociationInputInfo(file *os.File) (os.FileInfo, string, error) {
	if file == nil {
		return nil, "", ErrInvalidInput
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 {
		return nil, "", ErrInvalidInput
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, "", ErrInvalidInput
	}
	if info.Size() > generatedAVTransportBytes {
		return nil, "", ErrTimelineLimit
	}
	identity, err := media.VideoSeekSourceIdentity(info)
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	return info, identity, nil
}

func generatedAVAssociationSourceHash(ctx context.Context, file *os.File, size int64) ([32]byte, error) {
	var empty [32]byte
	if ctx == nil || file == nil || size < 1 || size > generatedAVTransportBytes {
		return empty, ErrInvalidInput
	}
	hash := sha256.New()
	var buffer [64 << 10]byte
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		length := min(int64(len(buffer)), size-offset)
		n, err := file.ReadAt(buffer[:int(length)], offset)
		if err != nil || int64(n) != length {
			return empty, ErrInvalidInput
		}
		_, _ = hash.Write(buffer[:n])
		offset += int64(n)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	copy(empty[:], hash.Sum(nil))
	return empty, nil
}

func generatedAVAssociationCaptureInfo(file *os.File, inputs []*os.File) (os.FileInfo, error) {
	if file == nil {
		return nil, ErrInvalidOptions
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 0 {
		return nil, ErrInvalidInput
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return nil, ErrInvalidInput
	}
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil || offset != 0 {
		return nil, ErrInvalidInput
	}
	for _, input := range inputs {
		if input == nil {
			return nil, ErrInvalidInput
		}
		prior, err := input.Stat()
		if err != nil || os.SameFile(info, prior) {
			return nil, ErrInvalidInput
		}
	}
	return info, nil
}

func generatedAVWriteAssociationCapture(ctx context.Context, file *os.File, data []byte, inputs []*os.File) error {
	if ctx == nil {
		return ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) < 1 || len(data) > generatedAVEffectiveJSONBytes {
		return ErrTimelineLimit
	}
	before, err := generatedAVAssociationCaptureInfo(file, inputs)
	if err != nil {
		return err
	}
	// The named bounded process writer has already joined. Direct Write keeps
	// its exact output bytes and cannot select an io.Copy ReaderFrom fast path.
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	after, err := file.Stat()
	offset, offsetErr := file.Seek(0, io.SeekCurrent)
	if err != nil || offsetErr != nil || !os.SameFile(before, after) || after.Size() != int64(len(data)) || offset != int64(len(data)) || after.Mode().Perm() != 0600 {
		return ErrInvalidInput
	}
	stat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return ErrInvalidInput
	}
	return ctx.Err()
}

// MeasureGeneratedAVAssociationProjection borrows source and one real physical
// segment. It replaces an output-frame observation for calibration, rather
// than running a second default-filled decoder. A single joined FFprobe process
// returns packets and frames using the original nofillin/genpts exclusion and
// decoder error policy. No mixed JSON shape or packet-origin field is assumed.
func MeasureGeneratedAVAssociationProjection(ctx context.Context, source, segment *os.File, options GeneratedAVAssociationProjectionOptions) (result GeneratedAVAssociationProjection, failure error) {
	// A transferred artifact descriptor is consumed even when options, context,
	// input preflight, admission or process start fails.
	if options.Capture != nil {
		defer func() {
			if err := options.Capture.Close(); err != nil {
				result.Complete = false
				failure = errors.Join(failure, err)
			}
		}()
	}
	if ctx == nil || options.Capture == nil || options.AcquireProbe == nil || options.ExpectedSourceSHA256 == ([32]byte{}) {
		return result, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	inputs := []*os.File{source, segment}
	fence, err := generatedAVDiagnosticOuterFence(inputs)
	if err != nil {
		return result, err
	}
	sourceInfo, sourceIdentity, err := generatedAVAssociationInputInfo(source)
	if err != nil {
		return result, err
	}
	segmentInfo, segmentIdentity, err := generatedAVAssociationInputInfo(segment)
	if err != nil {
		return result, err
	}
	if os.SameFile(sourceInfo, segmentInfo) {
		return result, ErrInvalidInput
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, options.SourceCertificate); err != nil {
		return result, err
	}
	if _, err := generatedAVAssociationCaptureInfo(options.Capture, inputs); err != nil {
		return result, err
	}
	sourceHash, err := generatedAVAssociationSourceHash(ctx, source, sourceInfo.Size())
	if err != nil {
		return result, err
	}
	if sourceHash != options.ExpectedSourceSHA256 {
		return result, ErrInvalidInput
	}
	if err := fence(); err != nil {
		return result, err
	}
	result.SourceBytes, result.SourceSHA256, result.SourceIdentity = sourceInfo.Size(), sourceHash, sourceIdentity
	result.InputIdentity = segmentIdentity
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
	output := &generatedAVDiagnosticBuffer{limit: generatedAVEffectiveJSONBytes, cancel: cancel}
	args := []string{"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats, "-show_packets", "-show_frames", "-show_streams", "-show_data_hash", "sha256", "-show_entries",
		"packet=codec_type,stream_index,pts,dts,duration,pos,size,flags,data_hash:packet_side_data=side_data_type,skip_samples,discard_padding,skip_reason,discard_reason:frame=media_type,stream_index,pts,pkt_dts,duration,nb_samples,key_frame,pkt_pos,pkt_size:frame_side_data=side_data_type,skip_samples,discard_padding,skip_reason,discard_reason:stream=index,codec_type,codec_name,time_base,sample_rate,channels,sample_fmt,channel_layout:stream_tags=:stream_disposition=:stream_side_data=",
		"-of", "json", "-i", "pipe:0"}
	count, hashes, err := generatedAVDiagnosticDecode(processCtx, options.FFprobePath, []*os.File{segment}, args, output)
	if err := errors.Join(err, output.err); err != nil {
		return result, err
	}
	if len(hashes) != 1 || count != segmentInfo.Size() {
		return result, ErrInvalidInput
	}
	result.InputBytes, result.InputSHA256 = count, hashes[0]
	result.JSONBytes, result.JSONLimitBytes, result.JSONSHA256 = output.Len(), generatedAVEffectiveJSONBytes, sha256.Sum256(output.Bytes())
	// Retirement and all exec copiers joined inside Decode. Fence both borrowed
	// inputs before writing any diagnostic artifact, then again after capture.
	if err := fence(); err != nil {
		return result, err
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, options.SourceCertificate); err != nil {
		return result, err
	}
	if err := generatedAVWriteAssociationCapture(ctx, options.Capture, output.Bytes(), inputs); err != nil {
		return result, err
	}
	result.CaptureWritten = true
	if err := fence(); err != nil {
		return result, err
	}
	envelope, err := ParseGeneratedAVAssociationProjectionEnvelope(ctx, output.Bytes())
	if err != nil {
		return result, err
	}
	result.EnvelopeParsed, result.RootFields = envelope.EnvelopeParsed, envelope.RootFields
	if err := fence(); err != nil {
		return result, err
	}
	if err := ValidateGeneratedMP4AVSourceEndpointIdentity(source, options.SourceCertificate); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.SourceIdentityUnchanged, result.InputIdentityUnchanged, result.Complete = true, true, true
	return result, nil
}
