package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strings"
)

// Matroska does not expose TrackNumber/UID as ffprobe stream IDs. Raw entries
// retain encounter order and are independently bound to the complete probe
// inventory before nonzero AAC delay can be accepted. IDs may be regenerated.
type mediaEditMatroskaTrackProof struct {
	Number, UID, TrackType                     uint64
	CodecID                                    string
	CodecDelayNS, SeekPreRollNS                uint64
	DefaultDurationNS                          uint64
	SamplingFrequency, OutputSamplingFrequency uint64
	SampleRate, Channels                       uint64
	CodecPrivateBytes                          int64
	CodecPrivateSHA256                         string
	audioKnown                                 bool
	privateOffset                              int64
}

type mediaEditMatroskaAudioProof struct {
	SamplingFrequency, OutputSamplingFrequency uint64
	SampleRate, Channels                       uint64
	Known                                      bool
}

func mediaEditMatroskaAudioFromScope(scope mediaEditEBMLScope) *mediaEditMatroskaAudioProof {
	frequency := float64(8000)
	if scope.count[0xB5] != 0 {
		frequency = scope.floats[0xB5]
	}
	output := frequency
	if scope.count[0x78B5] != 0 {
		output = scope.floats[0x78B5]
	}
	channels := uint64(1)
	if scope.count[0x9F] != 0 {
		channels = scope.uints[0x9F]
	}
	result := &mediaEditMatroskaAudioProof{Channels: channels}
	if frequency < 1 || frequency > 768000 || frequency != math.Trunc(frequency) || output < 1 || output > 768000 || output != math.Trunc(output) || channels == 0 || channels > 64 {
		return result
	}
	result.SamplingFrequency, result.OutputSamplingFrequency = uint64(frequency), uint64(output)
	result.SampleRate, result.Known = uint64(output), true
	return result
}

// Positive values use the same nearest/ties-away rescale as av_rescale_q.
// Comparing only rounded initial_padding is insufficient: both conversions
// must reproduce the exact original integer nanoseconds.
func mediaEditAACDelaySamples(delayNS, sampleRate uint64) (int64, error) {
	if sampleRate == 0 || sampleRate > 768000 || delayNS > math.MaxInt64 {
		return 0, mediaEditContainerError("AAC CodecDelay has an unproven sample rate or extent")
	}
	round := func(numerator *big.Int, denominator uint64) *big.Int {
		divisor := new(big.Int).SetUint64(denominator)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(numerator, divisor, remainder)
		if new(big.Int).Lsh(remainder, 1).Cmp(divisor) >= 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		return quotient
	}
	samples := round(new(big.Int).Mul(new(big.Int).SetUint64(delayNS), new(big.Int).SetUint64(sampleRate)), 1_000_000_000)
	if !samples.IsInt64() || samples.Sign() < 0 || samples.Int64() > math.MaxInt32 || delayNS != 0 && samples.Sign() == 0 {
		return 0, mediaEditContainerError("AAC CodecDelay exceeds its exact sample budget")
	}
	restored := round(new(big.Int).Mul(new(big.Int).Set(samples), big.NewInt(1_000_000_000)), sampleRate)
	if !restored.IsUint64() || restored.Uint64() != delayNS {
		return 0, mediaEditContainerError("AAC CodecDelay does not round-trip at nanosecond precision")
	}
	return samples.Int64(), nil
}

func mediaEditHasMatroskaCodecDelay(proof mediaEditContainerProof) bool {
	for _, track := range proof.MatroskaTracks {
		if track.CodecDelayNS != 0 {
			return true
		}
	}
	return false
}

func mediaEditMatroskaPrivateDigest(ctx context.Context, file *os.File, offset, size int64) (string, error) {
	if offset < 0 || size <= 0 || size > 16<<20 {
		return "", mediaEditContainerError("invalid Matroska codec-private extent")
	}
	digest := sha256.New()
	reader := io.NewSectionReader(file, offset, size)
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(buffer)
		total += int64(n)
		_, _ = digest.Write(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			var pathError *os.PathError
			if errors.As(err, &pathError) {
				err = pathError.Err
			}
			return "", fmt.Errorf("%w: Matroska CodecPrivate read: %w", ErrSubtitleRemovalUnsupported, err)
		}
	}
	if total != size {
		return "", mediaEditContainerError("truncated Matroska codec-private bytes")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func mediaEditMatroskaCodecMatches(track mediaEditMatroskaTrackProof, codec, codecType string) bool {
	if track.TrackType == 1 && codecType != "video" || track.TrackType == 2 && codecType != "audio" || track.TrackType == 17 && codecType != "subtitle" ||
		track.TrackType != 1 && track.TrackType != 2 && track.TrackType != 17 {
		return false
	}
	if track.TrackType == 1 && !strings.HasPrefix(track.CodecID, "V_") || track.TrackType == 2 && !strings.HasPrefix(track.CodecID, "A_") || track.TrackType == 17 && !strings.HasPrefix(track.CodecID, "S_") {
		return false
	}
	known := map[string]string{
		"V_MPEG4/ISO/AVC": "h264", "V_MPEGH/ISO/HEVC": "hevc", "V_AV1": "av1", "V_VP8": "vp8", "V_VP9": "vp9",
		"A_AAC": "aac", "A_MPEG/L3": "mp3", "A_AC3": "ac3", "A_EAC3": "eac3", "A_DTS": "dts", "A_FLAC": "flac",
		"S_TEXT/UTF8": "subrip", "S_TEXT/ASS": "ass", "S_TEXT/SSA": "ass", "S_TEXT/WEBVTT": "webvtt",
		"S_HDMV/PGS": "hdmv_pgs_subtitle", "S_VOBSUB": "dvd_subtitle",
	}
	if expected, present := known[track.CodecID]; present {
		return codec == expected
	}
	switch track.CodecID {
	case "A_PCM/INT/LIT":
		return codec == "pcm_u8" || codec == "pcm_s16le" || codec == "pcm_s24le" || codec == "pcm_s32le"
	case "A_PCM/INT/BIG":
		return codec == "pcm_s16be" || codec == "pcm_s24be" || codec == "pcm_s32be"
	case "A_PCM/FLOAT/IEEE":
		return codec == "pcm_f32le" || codec == "pcm_f64le"
	}
	return false
}

func bindMediaEditMatroskaTracks(proof mediaEditContainerProof, document mediaEditDocument) (map[int]mediaEditMatroskaTrackProof, error) {
	if len(proof.MatroskaTracks) == 0 || len(proof.MatroskaTracks) > mediaEditMaxStreams || proof.MatroskaAttachmentCount < 0 || len(document.Streams) != len(proof.MatroskaTracks)+proof.MatroskaAttachmentCount {
		return nil, mediaEditContainerError("AAC delay requires a complete Matroska track/attachment inventory")
	}
	result := make(map[int]mediaEditMatroskaTrackProof, len(proof.MatroskaTracks))
	numbers, uids := map[uint64]bool{}, map[uint64]bool{}
	for position, stream := range document.Streams {
		index, err := mediaEditInteger(stream["index"])
		if err != nil || index != int64(position) {
			return nil, mediaEditContainerError("Matroska stream indexes do not bind encounter order")
		}
		codecType, _ := stream["codec_type"].(string)
		codec, _ := stream["codec_name"].(string)
		if position >= len(proof.MatroskaTracks) {
			disposition, _ := stream["disposition"].(map[string]any)
			attached, _ := mediaEditInteger(disposition["attached_pic"])
			if codecType != "attachment" && (codecType != "video" || attached != 1) {
				return nil, mediaEditContainerError("Matroska trailing stream is not an admitted attachment")
			}
			continue
		}
		track := proof.MatroskaTracks[position]
		if track.Number == 0 || track.UID == 0 || numbers[track.Number] || uids[track.UID] || !mediaEditMatroskaCodecMatches(track, codec, codecType) {
			return nil, mediaEditContainerError("Matroska raw track does not bind one known probe codec")
		}
		numbers[track.Number], uids[track.UID] = true, true
		size := int64(0)
		if value, present := stream["extradata_size"]; present {
			size, err = mediaEditInteger(value)
			if err != nil {
				return nil, err
			}
		}
		digest, _ := stream["extradata_hash"].(string)
		if size != track.CodecPrivateBytes || size > 0 && (len(track.CodecPrivateSHA256) != 64 || !strings.EqualFold(digest, "SHA256:"+track.CodecPrivateSHA256)) {
			return nil, mediaEditContainerError("Matroska CodecPrivate is not completely represented by the probe")
		}
		if track.TrackType == 2 {
			rate, rateErr := mediaEditInteger(stream["sample_rate"])
			channels, channelErr := mediaEditInteger(stream["channels"])
			if !track.audioKnown || rateErr != nil || channelErr != nil || rate <= 0 || uint64(rate) != track.SampleRate || channels <= 0 || uint64(channels) != track.Channels {
				return nil, mediaEditContainerError("Matroska audio sampling does not bind its probe stream")
			}
		}
		if track.SeekPreRollNS != 0 || track.CodecDelayNS != 0 && (track.TrackType != 2 || track.CodecID != "A_AAC") {
			return nil, mediaEditContainerError("non-AAC delay or nonzero seek preroll remains unsupported")
		}
		if track.CodecID == "A_AAC" {
			if track.CodecDelayNS != 0 && (track.CodecPrivateBytes <= 0 || len(track.CodecPrivateSHA256) != 64) {
				return nil, mediaEditContainerError("AAC CodecDelay has no bound CodecPrivate bytes")
			}
			expectedPadding, err := mediaEditAACDelaySamples(track.CodecDelayNS, track.SampleRate)
			if err != nil {
				return nil, err
			}
			padding := int64(0)
			if value, present := stream["initial_padding"]; present {
				padding, err = mediaEditInteger(value)
				if err != nil {
					return nil, err
				}
			}
			if padding != expectedPadding {
				return nil, mediaEditContainerError("AAC CodecDelay differs from probe initial_padding")
			}
		}
		result[position] = track
	}
	return result, nil
}

type mediaEditMatroskaDelaySemantics struct {
	OutputIndex             int    `json:"output_index"`
	CodecID                 string `json:"codec_id"`
	CodecDelayNS            uint64 `json:"codec_delay_ns"`
	SampleRate              uint64 `json:"sample_rate"`
	SamplingFrequency       uint64 `json:"sampling_frequency"`
	OutputSamplingFrequency uint64 `json:"output_sampling_frequency"`
	Channels                uint64 `json:"channels"`
	InitialPadding          int64  `json:"initial_padding"`
	CodecPrivateSHA256      string `json:"codec_private_sha256"`
}

func compareMediaEditMatroskaDelays(source, candidate map[int]mediaEditMatroskaTrackProof, removedIndex int, pairs []MediaEditStreamEvidence) (string, error) {
	if len(source) == 0 || len(candidate) != len(source)-1 {
		return "", mediaEditContainerError("Matroska delay bindings do not describe one track removal")
	}
	removed, exists := source[removedIndex]
	if !exists || removed.TrackType != 17 || removed.CodecDelayNS != 0 || removed.SeekPreRollNS != 0 {
		return "", mediaEditContainerError("Matroska delay comparison does not bind the selected subtitle")
	}
	beforeSeen, afterSeen := map[int]bool{}, map[int]bool{}
	semantics := []mediaEditMatroskaDelaySemantics{}
	for _, pair := range pairs {
		before, beforePresent := source[pair.SourceIndex]
		after, afterPresent := candidate[pair.CandidateIndex]
		if !beforePresent && !afterPresent && pair.SourceIndex >= len(source) && pair.CandidateIndex >= len(candidate) && (pair.CodecType == "attachment" || pair.CodecType == "video") {
			continue
		}
		if !beforePresent || !afterPresent || pair.SourceIndex == removedIndex || beforeSeen[pair.SourceIndex] || afterSeen[pair.CandidateIndex] {
			return "", mediaEditContainerError("Matroska retained delay track mapping is incomplete")
		}
		beforeSeen[pair.SourceIndex], afterSeen[pair.CandidateIndex] = true, true
		if before.CodecDelayNS != after.CodecDelayNS || before.SeekPreRollNS != after.SeekPreRollNS {
			return "", mediaEditContainerError("retained Matroska codec delay changed")
		}
		if before.CodecDelayNS != 0 {
			if before.CodecID != "A_AAC" || after.CodecID != "A_AAC" || before.SampleRate != after.SampleRate || before.SamplingFrequency != after.SamplingFrequency || before.OutputSamplingFrequency != after.OutputSamplingFrequency || before.Channels != after.Channels || before.CodecPrivateSHA256 != after.CodecPrivateSHA256 {
				return "", mediaEditContainerError("retained AAC delay sampling or codec parameters changed")
			}
			padding, err := mediaEditAACDelaySamples(before.CodecDelayNS, before.SampleRate)
			if err != nil {
				return "", err
			}
			semantics = append(semantics, mediaEditMatroskaDelaySemantics{OutputIndex: pair.CandidateIndex, CodecID: "A_AAC", CodecDelayNS: before.CodecDelayNS,
				SampleRate: before.SampleRate, SamplingFrequency: before.SamplingFrequency, OutputSamplingFrequency: before.OutputSamplingFrequency,
				Channels: before.Channels, InitialPadding: padding, CodecPrivateSHA256: before.CodecPrivateSHA256})
		}
	}
	if len(beforeSeen) != len(source)-1 || len(afterSeen) != len(candidate) {
		return "", mediaEditContainerError("not all retained Matroska tracks were bound")
	}
	digest, err := mediaEditJSONDigest(semantics)
	if err != nil {
		return "", fmt.Errorf("hash Matroska AAC delay preservation: %w", err)
	}
	return digest, nil
}
