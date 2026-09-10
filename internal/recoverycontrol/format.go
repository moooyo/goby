package recoverycontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"unicode/utf8"
)

const (
	markerName          = ".goby-recovery-control.json"
	lockName            = ".goby-recovery-control.lock"
	currentName         = "current.json"
	proofName           = "cas-proof.json"
	maxRecordBytes      = MaxPayloadBytes + 4096
	maxMetadataBytes    = 16 << 10
	maxDirectoryEntries = 128
)

type identity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type ownerMarker struct {
	Version      int      `json:"version"`
	DeploymentID string   `json:"deploymentId"`
	StoreID      string   `json:"storeId"`
	Lock         identity `json:"lock"`
}

type record struct {
	Version        int             `json:"version"`
	DeploymentID   string          `json:"deploymentId"`
	StoreID        string          `json:"storeId"`
	Revision       uint64          `json:"revision"`
	PreviousDigest string          `json:"previousDigest"`
	Payload        json.RawMessage `json:"payload"`
}

type recordReference struct {
	Revision       uint64   `json:"revision"`
	Digest         string   `json:"digest"`
	PreviousDigest string   `json:"previousDigest"`
	Identity       identity `json:"identity"`
}

type publicationProof struct {
	Version      int              `json:"version"`
	DeploymentID string           `json:"deploymentId"`
	StoreID      string           `json:"storeId"`
	Before       *recordReference `json:"before"`
	Candidate    recordReference  `json:"candidate"`
}

func hash(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func validHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func encode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, ErrInvalid
	}
	return buffer.Bytes(), nil
}

func decode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrUnavailable
	}
	canonical, err := encode(value)
	if err != nil || !bytes.Equal(data, canonical) {
		return ErrUnavailable
	}
	return nil
}

// normalizePayload preserves JSON scalar representations and member order,
// removes insignificant whitespace, and rejects duplicate object keys at every
// nesting level. The business schema remains opaque to this package.
func normalizePayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > MaxPayloadBytes || !utf8.Valid(payload) {
		return nil, ErrInvalid
	}
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeJSON(decoder, 0); err != nil {
		return nil, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, payload); err != nil {
		return nil, ErrInvalid
	}
	return buffer.Bytes(), nil
}

func consumeJSON(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return ErrInvalid
			}
			key, ok := token.(string)
			if !ok {
				return ErrInvalid
			}
			if _, exists := keys[key]; exists {
				return ErrInvalid
			}
			keys[key] = struct{}{}
			if err := consumeJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for decoder.More() {
			if err := consumeJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validReference(reference recordReference) bool {
	return validHex(reference.Digest, 64) && reference.Identity != (identity{}) &&
		(reference.Revision == 0 && reference.PreviousDigest == "" || reference.Revision > 0 && validHex(reference.PreviousDigest, 64))
}

func recordSnapshot(value record, data []byte) Snapshot {
	result := Snapshot{Revision: value.Revision, Digest: hash(data)}
	if value.Revision > 0 {
		result.Payload = append([]byte(nil), value.Payload...)
	}
	return result
}
