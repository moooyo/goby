package storagebinding

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// MaxDocumentBytes includes transport whitespace and PostgreSQL jsonb spacing.
// The decoded model retains the smaller canonical fingerprint byte limit.
const MaxDocumentBytes = 4 << 20

// DecodeSnapshot accepts one complete, closed snapshot document. Field names
// are exact and unique at every object level; absent fields are not defaults.
// Invalid input errors never include paths, opaque handles, or source bytes.
func DecodeSnapshot(raw []byte) (Snapshot, error) {
	if len(raw) > MaxDocumentBytes {
		return Snapshot{}, ErrTopologyLimit
	}
	if len(raw) == 0 || !utf8.Valid(raw) {
		return Snapshot{}, ErrInvalidTopology
	}
	fields, err := decodeDocumentObject(raw, "version", "mapping", "anchor", "registered_root", "boundaries")
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := decodeDocumentValue(fields["version"], &snapshot.Version); err != nil {
		return Snapshot{}, err
	}
	mapping, err := decodeDocumentObject(fields["mapping"], "approved_path", "registered_path")
	if err != nil {
		return Snapshot{}, err
	}
	if err := decodeDocumentValue(mapping["approved_path"], &snapshot.Mapping.ApprovedPath); err != nil {
		return Snapshot{}, err
	}
	if err := decodeDocumentValue(mapping["registered_path"], &snapshot.Mapping.RegisteredPath); err != nil {
		return Snapshot{}, err
	}
	snapshot.Anchor, err = decodeDocumentIdentity(fields["anchor"])
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.RegisteredRoot, err = decodeDocumentIdentity(fields["registered_root"])
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Boundaries, err = decodeDocumentBoundaries(fields["boundaries"])
	if err != nil {
		return Snapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// EncodeSnapshot writes the same canonical representation used by Fingerprint.
// Input order and nil boundary slices do not create alternative stored forms.
// This operation validates structure only and never approves live storage.
func EncodeSnapshot(snapshot Snapshot) ([]byte, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	raw, err := snapshot.canonicalBytes()
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDocumentBytes {
		return nil, ErrTopologyLimit
	}
	return raw, nil
}

func decodeDocumentObject(raw []byte, required ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, ErrInvalidTopology
	}
	allowed := make(map[string]bool, len(required))
	for _, name := range required {
		allowed[name] = true
	}
	fields := make(map[string]json.RawMessage, len(required))
	for decoder.More() {
		token, err := decoder.Token()
		name, isName := token.(string)
		if err != nil || !isName || !allowed[name] || fields[name] != nil {
			return nil, ErrInvalidTopology
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrInvalidTopology
		}
		fields[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(fields) != len(required) || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidTopology
	}
	return fields, nil
}

func decodeDocumentValue(raw []byte, destination any) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, destination) != nil {
		return ErrInvalidTopology
	}
	return nil
}

func decodeDocumentIdentity(raw []byte) (Identity, error) {
	fields, err := decodeDocumentObject(raw, "version", "profile", "filesystem_uuid", "handle_type", "handle")
	if err != nil {
		return Identity{}, err
	}
	var identity Identity
	for _, field := range []struct {
		name        string
		destination any
	}{
		{"version", &identity.Version},
		{"profile", &identity.Profile},
		{"filesystem_uuid", &identity.FilesystemUUID},
		{"handle_type", &identity.HandleType},
	} {
		if err := decodeDocumentValue(fields[field.name], field.destination); err != nil {
			return Identity{}, err
		}
	}
	var encoded string
	if err := decodeDocumentValue(fields["handle"], &encoded); err != nil {
		return Identity{}, err
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxHandleBytes) {
		return Identity{}, ErrInvalidTopology
	}
	identity.Handle, err = base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(identity.Handle) != encoded || identity.Validate() != nil {
		return Identity{}, ErrInvalidTopology
	}
	return identity, nil
}

func decodeDocumentBoundaries(raw []byte) ([]Boundary, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return nil, ErrInvalidTopology
	}
	boundaries := make([]Boundary, 0)
	for decoder.More() {
		if len(boundaries) >= MaxBoundaries {
			return nil, ErrTopologyLimit
		}
		var rawBoundary json.RawMessage
		if err := decoder.Decode(&rawBoundary); err != nil {
			return nil, ErrInvalidTopology
		}
		fields, err := decodeDocumentObject(rawBoundary, "relative_path", "identity")
		if err != nil {
			return nil, err
		}
		var boundary Boundary
		if err := decodeDocumentValue(fields["relative_path"], &boundary.RelativePath); err != nil {
			return nil, err
		}
		boundary.Identity, err = decodeDocumentIdentity(fields["identity"])
		if err != nil {
			return nil, err
		}
		boundaries = append(boundaries, boundary)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidTopology
	}
	return boundaries, nil
}
