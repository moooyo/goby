package recoverydb

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/lifecycle"
)

// EncodeMarker writes the four-field version-1 representation. An empty
// generation is reserved for an initial primary slot and is never omitted.
// Encoding a database claim does not establish local ownership or authority.
func EncodeMarker(marker Marker) (string, error) {
	if !validMarker(marker) {
		return "", ErrInvalid
	}
	encoded, err := json.Marshal(marker)
	if err != nil || len(encoded) > MaxMarkerBytes {
		return "", ErrInvalid
	}
	return string(encoded), nil
}

// DecodeMarker accepts equivalent JSON field order, whitespace, and escapes,
// while requiring exactly the four known fields, with no duplicates or nulls.
// Invalid input never returns a partially populated marker or its raw content.
//
// A valid marker may have arrived in an archive. The caller must independently
// compare its claim with protected local control state before using it. Keep
// the original RawMarker separately for compare-and-swap; do not re-encode it.
func DecodeMarker(value string) (Marker, error) {
	if len(value) == 0 || len(value) > MaxMarkerBytes || !utf8.ValidString(value) {
		return Marker{}, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return Marker{}, ErrInvalid
	}
	var marker Marker
	var seen uint8
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return Marker{}, ErrInvalid
		}
		key, ok := keyToken.(string)
		if !ok {
			return Marker{}, ErrInvalid
		}
		var bit uint8
		switch key {
		case "version":
			bit = 1
		case "deploymentId":
			bit = 2
		case "generationId":
			bit = 4
		case "slot":
			bit = 8
		default:
			return Marker{}, ErrInvalid
		}
		if seen&bit != 0 {
			return Marker{}, ErrInvalid
		}
		seen |= bit
		field, err := decoder.Token()
		if err != nil {
			return Marker{}, ErrInvalid
		}
		if key == "version" {
			number, ok := field.(json.Number)
			if !ok || number.String() != "1" {
				return Marker{}, ErrInvalid
			}
			marker.Version = 1
			continue
		}
		text, ok := field.(string)
		if !ok {
			return Marker{}, ErrInvalid
		}
		switch key {
		case "deploymentId":
			marker.DeploymentID = text
		case "generationId":
			marker.GenerationID = text
		case "slot":
			marker.Slot = lifecycle.DatabaseSlot(text)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || seen != 15 {
		return Marker{}, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF || !validMarker(marker) {
		return Marker{}, ErrInvalid
	}
	return marker, nil
}

// ValidateRawMarker checks only the exact, bounded storage observation. Every
// present UTF-8 value is retained, including empty strings and invalid JSON.
// An absent row is distinct from all present values and has no stored bytes.
func ValidateRawMarker(raw RawMarker) error {
	if !raw.Present {
		if raw.Value != "" {
			return ErrInvalid
		}
		return nil
	}
	if len(raw.Value) > MaxMarkerBytes || !utf8.ValidString(raw.Value) {
		return ErrInvalid
	}
	return nil
}

func validMarker(marker Marker) bool {
	if marker.Version != 1 || !markerID(marker.DeploymentID) || marker.Slot != lifecycle.DatabasePrimary && marker.Slot != lifecycle.DatabaseRecovery {
		return false
	}
	if marker.GenerationID == "" {
		return marker.Slot == lifecycle.DatabasePrimary
	}
	return markerID(marker.GenerationID)
}

func markerID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for i := range value {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
