package library

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/moooyo/goby/internal/introskipper"
)

const (
	analysisFeaturesVersionV4     = 4
	analysisFeaturesRawHeaderSize = 72
	analysisFeaturesRawPointSize  = 4
)

// AnalysisFeaturesCodecVersion identifies a supported feature header. Callers
// must still use ValidateStoredAnalysisFeatures to validate its complete payload
// and source duration. Historical backup schemas can reject newer formats before
// accepting a snapshot that the current reader understands.
func AnalysisFeaturesCodecVersion(payload []byte) (uint16, error) {
	if len(payload) < analysisFeaturesHeaderSizeV1V2 || string(payload[:4]) != analysisFeaturesMagic {
		return 0, fmt.Errorf("%w: invalid analysis feature header", ErrInvalidInput)
	}
	version := binary.LittleEndian.Uint16(payload[4:6])
	headerSize := analysisFeaturesHeaderSizeV1V2
	switch version {
	case analysisFeaturesVersionV1, analysisFeaturesVersionV2:
	case analysisFeaturesVersion:
		headerSize = analysisFeaturesHeaderSize
	case analysisFeaturesVersionV4:
		headerSize = analysisFeaturesRawHeaderSize
	default:
		return 0, fmt.Errorf("%w: unsupported analysis feature format", ErrInvalidInput)
	}
	if len(payload) < headerSize {
		return 0, fmt.Errorf("%w: incomplete analysis feature header", ErrInvalidInput)
	}
	return version, nil
}

// Version 4 preserves the legacy 60-byte header but requires its audio, visual,
// refinement, and uncertainty fields to be zero. It appends a uint32 raw count
// and the exact IEEE 754 extraction horizon, followed by the profile and uint32
// fingerprint words. Raw positions remain contiguous and receive no PTS offset.
func encodeRawAnalysisFeatures(value AnalysisFeatures, sourceDuration int64) ([]byte, error) {
	if _, err := analysisFeaturesWindow(sourceDuration); err != nil {
		return nil, err
	}
	if len(value.Audio) != 0 || len(value.Visual) != 0 || len(value.Refinement) != 0 || value.AudioBoundaryUncertaintyTicks != 0 {
		return nil, fmt.Errorf("%w: raw analysis fingerprints cannot contain legacy features", ErrInvalidInput)
	}
	if err := validateAnalysisFeaturesMetadata(value.AlgorithmProfile, 0); err != nil {
		return nil, err
	}
	if err := validateRawAnalysisFeatures(len(value.RawFingerprint), value.FingerprintEndSeconds, sourceDuration); err != nil {
		return nil, err
	}
	if len(value.ContentSHA256) != 64 || strings.ToLower(value.ContentSHA256) != value.ContentSHA256 {
		return nil, fmt.Errorf("%w: analysis content hash must be 64 lowercase hexadecimal characters", ErrInvalidInput)
	}
	var digest [32]byte
	if _, err := hex.Decode(digest[:], []byte(value.ContentSHA256)); err != nil {
		return nil, fmt.Errorf("%w: invalid analysis content hash", ErrInvalidInput)
	}
	size := analysisFeaturesRawHeaderSize + len(value.AlgorithmProfile) + len(value.RawFingerprint)*analysisFeaturesRawPointSize
	if size > analysisFeaturesMaxPayloadBytes {
		return nil, fmt.Errorf("%w: analysis feature payload exceeds its limit", ErrInvalidInput)
	}
	payload := make([]byte, size)
	copy(payload[:4], analysisFeaturesMagic)
	binary.LittleEndian.PutUint16(payload[4:6], analysisFeaturesVersionV4)
	binary.LittleEndian.PutUint16(payload[6:8], uint16(len(value.AlgorithmProfile)))
	copy(payload[24:56], digest[:])
	binary.LittleEndian.PutUint32(payload[60:64], uint32(len(value.RawFingerprint)))
	binary.LittleEndian.PutUint64(payload[64:72], math.Float64bits(value.FingerprintEndSeconds))
	offset := analysisFeaturesRawHeaderSize
	copy(payload[offset:], value.AlgorithmProfile)
	offset += len(value.AlgorithmProfile)
	for _, point := range value.RawFingerprint {
		binary.LittleEndian.PutUint32(payload[offset:offset+analysisFeaturesRawPointSize], point)
		offset += analysisFeaturesRawPointSize
	}
	return payload, nil
}

func readRawAnalysisFeatures(payload []byte, sourceDuration int64, materialize bool) (AnalysisFeatures, error) {
	if len(payload) < analysisFeaturesRawHeaderSize || len(payload) > analysisFeaturesMaxPayloadBytes {
		return AnalysisFeatures{}, fmt.Errorf("%w: invalid raw analysis feature payload size", ErrInvalidInput)
	}
	if binary.LittleEndian.Uint32(payload[8:12]) != 0 || binary.LittleEndian.Uint32(payload[12:16]) != 0 ||
		binary.LittleEndian.Uint64(payload[16:24]) != 0 || binary.LittleEndian.Uint32(payload[56:60]) != 0 {
		return AnalysisFeatures{}, fmt.Errorf("%w: raw analysis fingerprints cannot contain legacy features", ErrInvalidInput)
	}
	profileBytes := binary.LittleEndian.Uint16(payload[6:8])
	rawCount := binary.LittleEndian.Uint32(payload[60:64])
	// Bound declarations before integer conversion, size arithmetic, or allocation.
	if profileBytes == 0 || profileBytes > analysisFeaturesMaxProfileBytes || rawCount == 0 || rawCount > introskipper.MaxFingerprintPoints {
		return AnalysisFeatures{}, fmt.Errorf("%w: invalid raw analysis feature lengths", ErrInvalidInput)
	}
	size := analysisFeaturesRawHeaderSize + int(profileBytes) + int(rawCount)*analysisFeaturesRawPointSize
	if len(payload) != size {
		return AnalysisFeatures{}, fmt.Errorf("%w: raw analysis feature payload length does not match its header", ErrInvalidInput)
	}
	horizon := math.Float64frombits(binary.LittleEndian.Uint64(payload[64:72]))
	if err := validateRawAnalysisFeatures(int(rawCount), horizon, sourceDuration); err != nil {
		return AnalysisFeatures{}, err
	}
	offset := analysisFeaturesRawHeaderSize
	profile := string(payload[offset : offset+int(profileBytes)])
	if err := validateAnalysisFeaturesMetadata(profile, 0); err != nil {
		return AnalysisFeatures{}, err
	}
	if !materialize {
		return AnalysisFeatures{}, nil
	}
	offset += int(profileBytes)
	value := AnalysisFeatures{
		ContentSHA256:         hex.EncodeToString(payload[24:56]),
		AlgorithmProfile:      profile,
		RawFingerprint:        make([]uint32, int(rawCount)),
		FingerprintEndSeconds: horizon,
	}
	for index := range value.RawFingerprint {
		value.RawFingerprint[index] = binary.LittleEndian.Uint32(payload[offset : offset+analysisFeaturesRawPointSize])
		offset += analysisFeaturesRawPointSize
	}
	return value, nil
}

func validateRawAnalysisFeatures(count int, horizon float64, sourceDuration int64) error {
	if count == 0 || count > introskipper.MaxFingerprintPoints {
		return fmt.Errorf("%w: raw analysis fingerprint count exceeds its bounds", ErrInvalidInput)
	}
	maximumHorizon := min(float64(sourceDuration)/float64(introskipper.TicksPerSecond), 600)
	if math.IsNaN(horizon) || math.IsInf(horizon, 0) || horizon <= 0 || horizon > maximumHorizon {
		return fmt.Errorf("%w: invalid raw analysis fingerprint horizon", ErrInvalidInput)
	}
	return nil
}

func analysisFeaturesMatchWork(value AnalysisFeatures, source AnalysisSource, work AnalysisWork) bool {
	if value.AlgorithmProfile != work.Execution.IntroProfile {
		return false
	}
	if work.Execution.DetectorVersion == introskipper.Version {
		return len(value.RawFingerprint) > 0 &&
			value.FingerprintEndSeconds == introskipper.FingerprintEndSeconds(source.DurationTicks, work.Execution.IntroSkipperOptions) &&
			len(value.Audio) == 0 && len(value.Visual) == 0 && len(value.Refinement) == 0 && value.AudioBoundaryUncertaintyTicks == 0
	}
	return len(value.RawFingerprint) == 0 && value.FingerprintEndSeconds == 0
}
