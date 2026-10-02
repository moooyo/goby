//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/media"
)

// MeasureGeneratedAVPacketFrameTransportAssociation executes exactly one joined
// projection, replacing the raw-only Measure for this diagnostic. It reuses the
// frozen private borrowed-input/hash/capture helpers and governed decoder; it
// never calls the old Measure first or adds an output-frame/default-fill probe.
// The caller retains source, segment and all production siblings until its own
// final fences. Capture ownership transfers at entry on every outcome.
func MeasureGeneratedAVPacketFrameTransportAssociation(ctx context.Context, source, segment *os.File, options GeneratedAVAssociationProjectionOptions) (result GeneratedAVPacketFrameAssociationMeasurement, failure error) {
	result.Stage = "preflight"
	defer func() {
		if options.Capture != nil {
			if err := options.Capture.Close(); err != nil {
				failure = errors.Join(failure, err)
				result.RawProjection.Complete = false
				if result.Stage == "complete" {
					result.Stage = "capture_close"
				}
			}
		}
		if failure != nil {
			result.Complete = false
			result.NativeClockComplete = false
			result.Projection.Complete, result.Projection.MeasuredInputKnown = false, false
			result.Projection.Observation.Complete = false
			result.Projection.measurement = nil
			result.Association.SameHeldInput, result.Association.PacketUnitBindingComplete = false, false
		}
	}()
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
	finalFence := func() error {
		if err := fence(); err != nil {
			return err
		}
		return ValidateGeneratedMP4AVSourceEndpointIdentity(source, options.SourceCertificate)
	}
	if err := finalFence(); err != nil {
		return result, err
	}
	if _, err := generatedAVAssociationCaptureInfo(options.Capture, inputs); err != nil {
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
	if err := finalFence(); err != nil {
		return result, err
	}
	result.RawProjection.SourceBytes, result.RawProjection.SourceSHA256, result.RawProjection.SourceIdentity = sourceInfo.Size(), sourceHash, sourceIdentity
	result.RawProjection.InputIdentity = segmentIdentity
	result.Stage = "transport"
	result.Transport, err = MeasureGeneratedAVTransportDiagnostic(ctx, segment)
	if err != nil {
		return result, err
	}
	if !result.Transport.Complete || result.Transport.Qualified || result.Transport.Bytes != segmentInfo.Size() {
		return result, ErrInvalidInput
	}
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
	output := &generatedAVDiagnosticBuffer{limit: generatedAVEffectiveJSONBytes, cancel: cancel}
	// Keep every argument identical to the passed raw calibration projection.
	args := []string{"-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats, "-show_packets", "-show_frames", "-show_streams", "-show_data_hash", "sha256", "-show_entries",
		"packet=codec_type,stream_index,pts,dts,duration,pos,size,flags,data_hash:packet_side_data=side_data_type,skip_samples,discard_padding,skip_reason,discard_reason:frame=media_type,stream_index,pts,pkt_dts,duration,nb_samples,key_frame,pkt_pos,pkt_size:frame_side_data=side_data_type,skip_samples,discard_padding,skip_reason,discard_reason:stream=index,codec_type,codec_name,time_base,sample_rate,channels,sample_fmt,channel_layout:stream_tags=:stream_disposition=:stream_side_data=",
		"-of", "json", "-i", "pipe:0"}
	result.Stage = "joined_probe"
	count, hashes, err := generatedAVDiagnosticDecode(processCtx, options.FFprobePath, []*os.File{segment}, args, output)
	if err := errors.Join(err, output.err); err != nil {
		return result, err
	}
	if len(hashes) != 1 || count != result.Transport.Bytes || hashes[0] != result.Transport.SHA256 {
		return result, ErrInvalidInput
	}
	result.RawProjection.InputBytes, result.RawProjection.InputSHA256 = count, hashes[0]
	result.RawProjection.JSONBytes, result.RawProjection.JSONLimitBytes, result.RawProjection.JSONSHA256 = output.Len(), generatedAVEffectiveJSONBytes, sha256.Sum256(output.Bytes())
	if err := finalFence(); err != nil {
		return result, err
	}
	result.Stage = "raw_capture"
	if err := generatedAVWriteAssociationCapture(ctx, options.Capture, output.Bytes(), inputs); err != nil {
		return result, err
	}
	result.RawProjection.CaptureWritten = true
	if err := finalFence(); err != nil {
		return result, err
	}
	result.Stage = "raw_envelope"
	envelope, err := ParseGeneratedAVAssociationProjectionEnvelope(ctx, output.Bytes())
	if err != nil {
		return result, err
	}
	result.RawProjection.EnvelopeParsed, result.RawProjection.RootFields = envelope.EnvelopeParsed, envelope.RootFields
	if err := finalFence(); err != nil {
		return result, err
	}
	result.RawProjection.SourceIdentityUnchanged, result.RawProjection.InputIdentityUnchanged, result.RawProjection.Complete = true, true, true
	result.Stage = "strict_projection"
	result.Projection, err = ParseGeneratedAVPacketFrameProjection(ctx, output.Bytes())
	if err != nil {
		return result, err
	}
	if err := finalFence(); err != nil {
		return result, err
	}
	projection := &result.Projection
	projection.Complete, projection.MeasuredInputKnown = true, true
	projection.MeasuredInputBytes, projection.MeasuredInputSHA256, projection.MeasuredSourceSHA256 = count, hashes[0], sourceHash
	projection.MeasuredInputIdentity, projection.MeasuredSourceIdentity = segmentIdentity, sourceIdentity
	projection.Observation.Complete, projection.Observation.InputBytes, projection.Observation.InputSHA256 = true, count, [][32]byte{hashes[0]}
	result.Stage = "typed_seal"
	seal := &generatedAVPacketFrameMeasurementSeal{jsonSHA: projection.JSONSHA256, inputSHA: hashes[0], sourceSHA: sourceHash,
		inputBytes: count, inputIdentity: segmentIdentity, sourceIdentity: sourceIdentity}
	seal.factsSHA, err = generatedAVPacketFrameFactsFingerprint(*projection)
	if err != nil {
		return result, err
	}
	seal.transportFactsSHA, err = generatedAVPacketFrameFactsFingerprint(result.Transport)
	if err != nil {
		return result, err
	}
	projection.measurement = seal
	if err := finalFence(); err != nil {
		return result, err
	}
	result.Stage = "association"
	result.Association, err = AssociateGeneratedAVPacketFrameTransport(ctx, *projection, result.Transport)
	if err != nil {
		return result, err
	}
	result.Stage = "final_fence"
	if err := finalFence(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.NativeClockComplete = projection.Observation.NativeClockComplete
	result.Complete, result.Stage = true, "complete"
	return result, nil
}
