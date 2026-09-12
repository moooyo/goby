package storagebinding

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestStorageBindingDocumentCanonicalRoundTrip(t *testing.T) {
	snapshot := modelTestSnapshot()
	snapshot.Boundaries[0], snapshot.Boundaries[1] = snapshot.Boundaries[1], snapshot.Boundaries[0]
	raw, err := EncodeSnapshot(snapshot)
	if err != nil || string(raw) != modelSnapshotGolden {
		t.Fatalf("encoding did not use the fixed canonical document: %v", err)
	}
	if snapshot.Boundaries[0].RelativePath != "nested/z" {
		t.Fatal("encoding reordered the caller's boundaries")
	}
	digest := sha256.Sum256(raw)
	if fingerprint, err := snapshot.Fingerprint(); err != nil || fingerprint != hex.EncodeToString(digest[:]) {
		t.Fatalf("document bytes differ from the fingerprint input: %v", err)
	}
	reordered := `{
		"boundaries": [
			{"identity": ` + modelOtherIdentityGolden + `, "relative_path": "nested/z"},
			{"identity": ` + modelIdentityGolden + `, "relative_path": "nested/<a>"}
		],
		"registered_root": ` + modelOtherIdentityGolden + `,
		"anchor": {"handle":"AQID","handle_type":-2147483648,"filesystem_uuid":"0123456789abcdef0123456789abcdef","profile":"linux-fsuuid-filehandle-v1","version":1},
		"mapping": {"registered_path": "/approved/library", "approved_path": "/approved"},
		"version": 1
	}`
	decoded, err := DecodeSnapshot([]byte(" \n\t" + reordered + "\r\n "))
	if err != nil {
		t.Fatalf("a complete reordered document was rejected: %v", err)
	}
	if encoded, err := EncodeSnapshot(decoded); err != nil || !bytes.Equal(encoded, raw) {
		t.Fatalf("reordered document did not round trip canonically: %v", err)
	}
	for _, boundaries := range [][]Boundary{nil, {}} {
		empty := modelTestSnapshot()
		empty.Boundaries = boundaries
		encoded, err := EncodeSnapshot(empty)
		if err != nil || string(encoded) != modelSnapshotGoldenPrefix+`[]}` {
			t.Fatalf("empty boundaries did not encode as an explicit array: %v", err)
		}
		decoded, err := DecodeSnapshot(encoded)
		if err != nil || len(decoded.Boundaries) != 0 {
			t.Fatalf("an explicit empty boundary array was rejected: %v", err)
		}
	}
}

func TestStorageBindingDocumentRejectsNonExactObjectKeys(t *testing.T) {
	for _, object := range documentTestObjects() {
		t.Run(object.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(object.raw), &fields); err != nil {
				t.Fatal(err)
			}
			key := object.fields[0]
			member := `"` + key + `":` + string(fields[key])
			caseKey := strings.ToUpper(key[:1]) + key[1:]
			for _, test := range []struct {
				name string
				raw  string
			}{
				{"unknown", strings.TrimSuffix(object.raw, "}") + `,"unknown_field":true}`},
				{"case variant", strings.Replace(object.raw, `"`+key+`"`, `"`+caseKey+`"`, 1)},
				{"duplicate", strings.TrimSuffix(object.raw, "}") + "," + member + "}"},
				{"escaped duplicate", strings.TrimSuffix(object.raw, "}") + fmt.Sprintf(",\"\\u%04x%s\":%s}", key[0], key[1:], fields[key])},
			} {
				t.Run(test.name, func(t *testing.T) {
					documentTestRequireError(t, object.wrap(test.raw), ErrInvalidTopology)
				})
			}
		})
	}
}

func TestStorageBindingDocumentRequiresEveryTypedField(t *testing.T) {
	for _, object := range documentTestObjects() {
		t.Run(object.name, func(t *testing.T) {
			for _, field := range object.fields {
				t.Run(field, func(t *testing.T) {
					for _, test := range []struct {
						name  string
						value json.RawMessage
					}{
						{"missing", nil},
						{"null", json.RawMessage("null")},
						{"wrong primitive", json.RawMessage("false")},
					} {
						t.Run(test.name, func(t *testing.T) {
							raw := documentTestReplaceField(t, object.raw, field, test.value)
							documentTestRequireError(t, object.wrap(raw), ErrInvalidTopology)
						})
					}
				})
			}
		})
	}
}

func TestStorageBindingDocumentPreservesOpaqueHandlesAndRequiresCanonicalBase64(t *testing.T) {
	snapshot := modelTestSnapshot()
	snapshot.Anchor.HandleType = 0
	snapshot.Anchor.Handle = make([]byte, MaxHandleBytes)
	for index := range snapshot.Anchor.Handle {
		snapshot.Anchor.Handle[index] = byte(index * 17)
	}
	snapshot.Anchor.Handle[0], snapshot.Anchor.Handle[MaxHandleBytes-1] = 0, 0
	snapshot.RegisteredRoot.HandleType = -2147483648
	snapshot.RegisteredRoot.Handle = []byte{0, 255, 0, 128, 1, 0}
	snapshot.Boundaries[0].Identity.HandleType = 2147483647
	snapshot.Boundaries[0].Identity.Handle = []byte{255, 0, 255, 0}
	raw, err := EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSnapshot(raw)
	if err != nil || !decoded.Anchor.Equal(snapshot.Anchor) || !decoded.RegisteredRoot.Equal(snapshot.RegisteredRoot) ||
		len(decoded.Boundaries) != len(snapshot.Boundaries) {
		t.Fatalf("a complete opaque identity did not round trip: %v", err)
	}
	for index := range snapshot.Boundaries {
		if !decoded.Boundaries[index].Identity.Equal(snapshot.Boundaries[index].Identity) {
			t.Fatal("a nested opaque identity changed during round trip")
		}
	}
	for _, object := range documentTestObjects() {
		if !object.identity {
			continue
		}
		t.Run(object.name, func(t *testing.T) {
			for _, test := range []struct {
				name  string
				value string
			}{
				{"numeric array", `[1,2,3]`},
				{"empty array", `[]`},
				{"empty handle", `""`},
				{"line break", `"AQID\n"`},
				{"carriage return", `"AQID\r"`},
				{"missing padding", `"AQ"`},
				{"excess padding", `"AQ==="`},
				{"nonzero one byte pad bits", `"AR=="`},
				{"nonzero two byte pad bits", `"AQJ="`},
				{"URL alphabet", `"__8="`},
				{"oversized handle", `"` + base64.StdEncoding.EncodeToString(make([]byte, MaxHandleBytes+1)) + `"`},
			} {
				t.Run(test.name, func(t *testing.T) {
					raw := documentTestReplaceField(t, object.raw, "handle", json.RawMessage(test.value))
					documentTestRequireError(t, object.wrap(raw), ErrInvalidTopology)
				})
			}
		})
	}
}

func TestStorageBindingDocumentRejectsMalformedInputAndInvalidModelsPrivately(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"whitespace", []byte(" \t\r\n")},
		{"null", []byte("null")},
		{"array", []byte("[]")},
		{"truncated", []byte(modelSnapshotGolden[:len(modelSnapshotGolden)-1])},
		{"trailing object", []byte(modelSnapshotGolden + " {}")},
		{"trailing primitive", []byte(modelSnapshotGolden + " true")},
		{"invalid UTF8", bytes.ReplaceAll([]byte(modelSnapshotGolden), []byte("/approved"), []byte("/approved\xff"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			documentTestRequireError(t, test.raw, ErrInvalidTopology)
		})
	}
	for _, test := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"unsupported version", func(snapshot *Snapshot) { snapshot.Version++ }},
		{"invalid nested identity", func(snapshot *Snapshot) { snapshot.Boundaries[1].Identity.Profile = "future-profile" }},
		{"duplicate boundary", func(snapshot *Snapshot) { snapshot.Boundaries[1].RelativePath = snapshot.Boundaries[0].RelativePath }},
		{"noncanonical private path", func(snapshot *Snapshot) {
			snapshot.Mapping.RegisteredPath = "/approved/private-document-path/../outside"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := modelTestSnapshot()
			test.change(&snapshot)
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			decodeErr := documentTestRequireError(t, raw, ErrInvalidTopology)
			_, encodeErr := EncodeSnapshot(snapshot)
			if !errors.Is(encodeErr, ErrInvalidTopology) {
				t.Fatalf("encoding accepted an invalid model: %v", encodeErr)
			}
			for _, err := range []error{decodeErr, encodeErr} {
				if strings.Contains(err.Error(), "private-document-path") {
					t.Fatal("a codec error exposed a persisted path")
				}
			}
		})
	}
	privateHandle := "private-document-handle!"
	raw := []byte(strings.Replace(modelSnapshotGolden, `"handle":"AQID"`, `"handle":"`+privateHandle+`"`, 1))
	if err := documentTestRequireError(t, raw, ErrInvalidTopology); strings.Contains(err.Error(), privateHandle) {
		t.Fatal("a codec error exposed a persisted handle")
	}
}

func TestStorageBindingDocumentEnforcesRawCountAndCanonicalByteLimits(t *testing.T) {
	if MaxDocumentBytes != 4<<20 {
		t.Fatal("the persisted document byte limit changed")
	}
	atLimit := []byte(strings.Repeat(" ", MaxDocumentBytes-len(modelSnapshotGolden)) + modelSnapshotGolden)
	if _, err := DecodeSnapshot(atLimit); err != nil {
		t.Fatalf("a valid document at the raw byte limit was rejected: %v", err)
	}
	documentTestRequireError(t, append(atLimit, ' '), ErrTopologyLimit)
	maximum := modelTestSnapshot()
	maximum.Boundaries = make([]Boundary, MaxBoundaries)
	for index := range maximum.Boundaries {
		maximum.Boundaries[index] = Boundary{RelativePath: fmt.Sprintf("nested/%03d", index), Identity: modelTestIdentity()}
	}
	raw, err := EncodeSnapshot(maximum)
	if err != nil {
		t.Fatalf("encoding rejected the maximum complete boundary count: %v", err)
	}
	decoded, err := DecodeSnapshot(raw)
	if err != nil || len(decoded.Boundaries) != MaxBoundaries {
		t.Fatalf("decoding rejected or truncated the maximum complete boundary count: %v", err)
	}
	excessive := maximum.Clone()
	excessive.Boundaries = append(excessive.Boundaries, Boundary{RelativePath: "one-more", Identity: modelTestIdentity()})
	for index := range maximum.Boundaries {
		maximum.Boundaries[index].RelativePath = fmt.Sprintf("%03d", index) + strings.Repeat("\n", MaxPathBytes-3)
	}
	for _, test := range []struct {
		name     string
		snapshot Snapshot
	}{
		{"boundary count", excessive},
		{"canonical bytes", maximum},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.snapshot)
			if err != nil || len(raw) > MaxDocumentBytes {
				t.Fatalf("the model-limit fixture exceeds the independent raw byte limit: %v", err)
			}
			documentTestRequireError(t, raw, ErrTopologyLimit)
			if _, err := EncodeSnapshot(test.snapshot); !errors.Is(err, ErrTopologyLimit) {
				t.Fatalf("encoding bypassed a model limit: %v", err)
			}
		})
	}
}

type documentTestObject struct {
	name     string
	raw      string
	prefix   string
	fields   []string
	identity bool
}

func documentTestObjects() []documentTestObject {
	mapping := `{"approved_path":"/approved","registered_path":"/approved/library"}`
	firstBoundary := `{"relative_path":"nested/\u003ca\u003e","identity":` + modelIdentityGolden + `}`
	secondBoundary := `{"relative_path":"nested/z","identity":` + modelOtherIdentityGolden + `}`
	identityFields := []string{"version", "profile", "filesystem_uuid", "handle_type", "handle"}
	return []documentTestObject{
		{"snapshot", modelSnapshotGolden, "", []string{"version", "mapping", "anchor", "registered_root", "boundaries"}, false},
		{"mapping", mapping, `"mapping":`, []string{"approved_path", "registered_path"}, false},
		{"anchor", modelIdentityGolden, `"anchor":`, identityFields, true},
		{"registered root", modelOtherIdentityGolden, `"registered_root":`, identityFields, true},
		{"first boundary", firstBoundary, "", []string{"relative_path", "identity"}, false},
		{"second boundary", secondBoundary, "", []string{"relative_path", "identity"}, false},
		{"first boundary identity", modelIdentityGolden, `"identity":`, identityFields, true},
		{"second boundary identity", modelOtherIdentityGolden, `"identity":`, identityFields, true},
	}
}

func (object documentTestObject) wrap(raw string) []byte {
	return []byte(strings.Replace(modelSnapshotGolden, object.prefix+object.raw, object.prefix+raw, 1))
}

func documentTestReplaceField(t *testing.T, raw, field string, value json.RawMessage) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(fields, field)
	} else {
		fields[field] = value
	}
	replaced, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(replaced)
}

func documentTestRequireError(t *testing.T, raw []byte, want error) error {
	t.Helper()
	_, err := DecodeSnapshot(raw)
	if !errors.Is(err, want) {
		t.Fatalf("document error = %v; want %v", err, want)
	}
	return err
}
