package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestGeneratedAVAssociationProjectionEnvelopeRecordsOnlyActualShape(t *testing.T) {
	data := []byte(`{"packets_and_frames":[{"type":"packet","stream_index":1,"data_hash":"SHA256:uninterpreted"},{"type":"frame","media_type":"audio","nb_samples":1024}],"programs":null,"stream_groups":[],"streams":[{"index":0},{"index":1}]}`)
	result, err := ParseGeneratedAVAssociationProjectionEnvelope(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if !result.EnvelopeParsed || result.Complete || result.Qualified || result.SchemaCalibrated || result.PayloadRepresentationKnown || result.PacketFrameBound || result.ContentBound || result.CaptureWritten {
		t.Fatal("raw shape observation acquired a schema, clock, content or completion claim")
	}
	if result.JSONBytes != len(data) || result.JSONLimitBytes != 4<<20 || result.JSONSHA256 != sha256.Sum256(data) || len(result.RootFields) != 4 {
		t.Fatal("raw byte receipt or bounded field summary changed")
	}
	first, second := result.RootFields[0], result.RootFields[1]
	if first.Name != "packets_and_frames" || first.Kind != "array" || !first.ArrayCountKnown || first.ArrayElements != 2 || second.Name != "programs" || second.Kind != "null" || second.ArrayCountKnown {
		t.Fatal("calibration invented record roles or converted actual null to an empty array")
	}
	if result.SourceSHA256 != ([32]byte{}) || result.InputSHA256 != ([32]byte{}) || result.SourceIdentityUnchanged || result.InputIdentityUnchanged {
		t.Fatal("pure envelope acquired borrowed-file evidence")
	}
}

func TestGeneratedAVAssociationProjectionEnvelopeRejectsMalformedOrUnboundedJSON(t *testing.T) {
	cases := map[string][]byte{
		"null_root":         []byte(`null`),
		"array_root":        []byte(`[]`),
		"empty_root":        []byte(`{}`),
		"incomplete":        []byte(`{"records":[`),
		"trailing":          []byte(`{"records":[]} {}`),
		"duplicate_root":    []byte(`{"records":[],"records":[]}`),
		"duplicate_nested":  []byte(`{"records":[{"pos":1,"pos":2}]}`),
		"bad_unicode":       []byte(`{"records":["\ud800"]}`),
		"field_name_bound":  []byte(`{"` + strings.Repeat("x", 65) + `":[]}`),
		"field_count_bound": []byte(`{"a":0,"b":0,"c":0,"d":0,"e":0,"f":0,"g":0,"h":0,"i":0,"j":0,"k":0,"l":0,"m":0,"n":0,"o":0,"p":0,"q":0}`),
		"depth_bound":       []byte(`{"records":[[[[[[[[[0]]]]]]]]]}`),
		"record_bound":      []byte(`{"records":[` + strings.Repeat("0,", generatedAVEffectiveFrames) + `0]}`),
		"aggregate_arrays":  []byte(`{"first":[` + strings.Repeat("0,", generatedAVEffectiveFrames-1) + `0],"second":[0]}`),
		"stdout_bound":      bytes.Repeat([]byte{' '}, (4<<20)+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := ParseGeneratedAVAssociationProjectionEnvelope(context.Background(), data)
			if err == nil || result.EnvelopeParsed || result.Complete || result.Qualified {
				t.Fatal("malformed or unbounded evidence acquired a successful envelope")
			}
		})
	}
	if _, err := ParseGeneratedAVAssociationProjectionEnvelope(nil, []byte(`{"records":[]}`)); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseGeneratedAVAssociationProjectionEnvelope(ctx, []byte(`{"records":[]}`)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled envelope accepted")
	}
}
