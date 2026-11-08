package transcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCodecConfigurations = errors.New("invalid codec configurations")

const (
	H264SoftwareCodecID = "V-E-libx264"
	HEVCSoftwareCodecID = "V-E-libx265"
	MinCodecPriority    = -1000
	MaxCodecPriority    = 1000
)

// CodecConfiguration identifies an implemented encoder, rather than an output
// format. These IDs are bound explicitly to the observed native implementations.
type CodecConfiguration struct {
	CodecId   string `json:"CodecId"`
	IsEnabled bool   `json:"IsEnabled"`
	Priority  int    `json:"Priority"`
}

// CodecConfigurations is a complete, comparable policy for the two supported
// native software encoder identities. A nil managed override selects defaults;
// additional native encoders require an implemented output path before adoption.
type CodecConfigurations [2]CodecConfiguration

// DefaultCodecConfigurations preserves client ordering when priorities tie.
func DefaultCodecConfigurations() CodecConfigurations {
	return CodecConfigurations{
		{CodecId: H264SoftwareCodecID, IsEnabled: true},
		{CodecId: HEVCSoftwareCodecID, IsEnabled: true},
	}
}

func ValidateCodecConfigurations(value CodecConfigurations) error {
	for _, configuration := range value {
		if configuration.CodecId != H264SoftwareCodecID && configuration.CodecId != HEVCSoftwareCodecID {
			return fmt.Errorf("%w: use each supported encoder identity exactly once", ErrInvalidCodecConfigurations)
		}
		if configuration.Priority < MinCodecPriority || configuration.Priority > MaxCodecPriority {
			return fmt.Errorf("%w: encoder priority is outside the managed bound", ErrInvalidCodecConfigurations)
		}
	}
	if value[0].CodecId == value[1].CodecId {
		return fmt.Errorf("%w: use each supported encoder identity exactly once", ErrInvalidCodecConfigurations)
	}
	return nil
}

// CanonicalCodecConfigurations removes array ordering as an unrelated source
// of settings changes. The managed priority bound is a Goby input constraint.
func CanonicalCodecConfigurations(value CodecConfigurations) (CodecConfigurations, error) {
	if err := ValidateCodecConfigurations(value); err != nil {
		return CodecConfigurations{}, err
	}
	if value[0].CodecId != H264SoftwareCodecID {
		value[0], value[1] = value[1], value[0]
	}
	return value, nil
}

// ResolveCodecConfigurations preserves the absence in legacy planner callers.
// Managers and settings snapshots otherwise supply a complete validated policy.
func ResolveCodecConfigurations(value CodecConfigurations) (CodecConfigurations, error) {
	if value == (CodecConfigurations{}) {
		return DefaultCodecConfigurations(), nil
	}
	return CanonicalCodecConfigurations(value)
}

// UnmarshalJSON does not use the default fixed-array decoder, which would ignore
// excess entries. Every object member is required, typed, and present only once.
func (value *CodecConfigurations) UnmarshalJSON(data []byte) error {
	invalid := func() error { return ErrInvalidCodecConfigurations }
	if value == nil || len(data) == 0 || len(data) > 2048 {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
		return invalid()
	}
	var result CodecConfigurations
	count := 0
	for decoder.More() {
		if count == len(result) {
			return invalid()
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
			return invalid()
		}
		fields := make(map[string]json.RawMessage, 3)
		for decoder.More() {
			token, err := decoder.Token()
			field, ok := token.(string)
			if err != nil || !ok || field != "CodecId" && field != "IsEnabled" && field != "Priority" || fields[field] != nil {
				return invalid()
			}
			var raw json.RawMessage
			if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return invalid()
			}
			fields[field] = raw
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 3 {
			return invalid()
		}
		configuration := &result[count]
		if json.Unmarshal(fields["CodecId"], &configuration.CodecId) != nil ||
			json.Unmarshal(fields["IsEnabled"], &configuration.IsEnabled) != nil ||
			json.Unmarshal(fields["Priority"], &configuration.Priority) != nil {
			return invalid()
		}
		count++
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim(']') || count != len(result) {
		return invalid()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid()
	}
	canonical, err := CanonicalCodecConfigurations(result)
	if err != nil {
		return err
	}
	*value = canonical
	return nil
}

// SoftwareCodecConfiguration looks up the actual selected implementation.
// A disabled software encoder does not disable a different hardware encoder
// for the same format, or a format whose native identity has not been adopted.
func SoftwareCodecConfiguration(value CodecConfigurations, codec, backend string) (CodecConfiguration, bool) {
	var id string
	switch VideoEncoder(codec, backend) {
	case "libx264":
		id = H264SoftwareCodecID
	case "libx265":
		id = HEVCSoftwareCodecID
	default:
		return CodecConfiguration{}, false
	}
	resolved, err := ResolveCodecConfigurations(value)
	if err != nil {
		return CodecConfiguration{}, false
	}
	for _, configuration := range resolved {
		if configuration.CodecId == id {
			return configuration, true
		}
	}
	return CodecConfiguration{}, false
}

// ConfiguredVideoEncodingAllowed is an admission gate for the final encoder.
// A hardware fallback must call it again after selecting its software backend.
func ConfiguredVideoEncodingAllowed(value CodecConfigurations, plan Plan) bool {
	if !VideoEncodingSupported(plan.VideoCodec) {
		return true
	}
	if _, err := ResolveCodecConfigurations(value); err != nil {
		return false
	}
	_, backend := hardwareSelection(plan.Hardware)
	configuration, found := SoftwareCodecConfiguration(value, plan.VideoCodec, backend)
	return !found || configuration.IsEnabled
}
