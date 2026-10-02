package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"sort"
)

// GeneratedAVAssociationProjectionOptions carries trusted diagnostic-only
// capabilities. ExpectedSourceSHA256 is the whole encoded source hash from the
// strict source observation, never the independently decoded PCM hash. Capture
// transfers ownership at entry, including every failure path. It must be a new
// private empty 0600 regular single-link descriptor, distinct from both inputs.
// AcquireProbe borrows the caller's existing bounded probe lane; it adds no pool.
type GeneratedAVAssociationProjectionOptions struct {
	FFprobePath          string
	SourceCertificate    GeneratedAVSourceCertificate
	ExpectedSourceSHA256 [32]byte
	AcquireProbe         func(context.Context) (func(), error) `json:"-"`
	Capture              *os.File                              `json:"-"`
}

// GeneratedAVAssociationJSONField is a bounded shape observation, not a packet
// or frame schema. ArrayElements counts only actual top-level array entries.
// A null or scalar value remains its observed Kind with ArrayCountKnown=false.
type GeneratedAVAssociationJSONField struct {
	Name, Kind      string
	ArrayCountKnown bool
	ArrayElements   int
}

// GeneratedAVAssociationProjection is a raw calibration receipt. Complete
// requires actual input copier EOF, governed process retirement/Wait joins,
// exact bounded capture, envelope parsing and final borrowed-input fences.
// EnvelopeParsed never means that a mixed packet/frame schema was accepted.
// Schema, payload representation, frame origin, decoded content and playback
// qualification remain unknown/false until separately proved against raw bytes.
type GeneratedAVAssociationProjection struct {
	Qualified, Complete, EnvelopeParsed, CaptureWritten bool
	SchemaCalibrated, PayloadRepresentationKnown        bool
	PacketFrameBound, ContentBound                      bool
	JSONBytes, JSONLimitBytes                           int
	JSONSHA256                                          [32]byte
	SourceBytes, InputBytes                             int64
	SourceSHA256, InputSHA256                           [32]byte
	SourceIdentity, InputIdentity                       string
	SourceIdentityUnchanged, InputIdentityUnchanged     bool
	RootFields                                          []GeneratedAVAssociationJSONField
}

// ParseGeneratedAVAssociationProjectionEnvelope accepts only a complete bounded
// unique-key JSON object and records its actual top-level kinds/counts. It does
// not assume packets_and_frames, record type tags or packet-origin fields. This
// deliberately uncalibrated API cannot supply a DecodedObservation or clock.
func ParseGeneratedAVAssociationProjectionEnvelope(ctx context.Context, data []byte) (GeneratedAVAssociationProjection, error) {
	var empty GeneratedAVAssociationProjection
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if len(data) == 0 || len(data) > generatedAVEffectiveJSONBytes {
		return empty, ErrTimelineLimit
	}
	if err := generatedUniqueJSON(data); err != nil {
		return empty, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return empty, ErrTimelineProbe
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || len(root) < 1 || len(root) > 16 {
		return empty, ErrTimelineProbe
	}
	result := GeneratedAVAssociationProjection{
		EnvelopeParsed: true, JSONBytes: len(data), JSONLimitBytes: generatedAVEffectiveJSONBytes, JSONSHA256: sha256.Sum256(data),
	}
	arrayElements := 0
	for name, raw := range root {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if len(name) < 1 || len(name) > 64 {
			return empty, ErrTimelineLimit
		}
		value := bytes.TrimSpace(raw)
		if len(value) == 0 {
			return empty, ErrTimelineProbe
		}
		field := GeneratedAVAssociationJSONField{Name: name}
		switch value[0] {
		case '[':
			decoder := json.NewDecoder(bytes.NewReader(value))
			opening, err := decoder.Token()
			if err != nil || opening != json.Delim('[') {
				return empty, ErrTimelineProbe
			}
			// Reuse the existing frame-record ceiling across all top-level
			// arrays, stopping before allocating an oversized record slice.
			// Actual record roles remain unknown; no cap is enlarged.
			for decoder.More() {
				if err := ctx.Err(); err != nil {
					return empty, err
				}
				if arrayElements >= generatedAVEffectiveFrames {
					return empty, ErrTimelineLimit
				}
				var record json.RawMessage
				if decoder.Decode(&record) != nil {
					return empty, ErrTimelineProbe
				}
				arrayElements++
				field.ArrayElements++
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return empty, ErrTimelineProbe
			}
			field.Kind, field.ArrayCountKnown = "array", true
		case '{':
			field.Kind = "object"
		case '"':
			field.Kind = "string"
		case 't', 'f':
			field.Kind = "boolean"
		case 'n':
			field.Kind = "null"
		default:
			field.Kind = "number"
		}
		result.RootFields = append(result.RootFields, field)
	}
	sort.Slice(result.RootFields, func(left, right int) bool { return result.RootFields[left].Name < result.RootFields[right].Name })
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
