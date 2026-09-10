package recoverycontrol

import (
	"bytes"
	"strings"
	"testing"
)

func TestPayloadIsAnOpaqueBoundedObjectWithoutAmbiguousKeys(t *testing.T) {
	input := []byte(" { \"phase\" : \"prepared\", \"amount\": 9007199254740993, \"ratio\": 1e+1000, \"nested\": [true, null, {\"ok\": false}] } ")
	normalized, err := normalizePayload(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(normalized, []byte("9007199254740993")) || !bytes.Contains(normalized, []byte("1e+1000")) {
		t.Fatal("normalization changed numeric scalar representations")
	}
	input[2] = 'x'
	if !bytes.HasPrefix(normalized, []byte(`{"phase":"prepared"`)) {
		t.Fatal("normalization retained input backing storage")
	}
	for _, value := range []string{"", "null", "[]", `"object"`, "42", `{"a":1,"a":2}`, `{"nested":{"a":1,"a":1}}`, `{"a":[{"b":1,"\u0062":2}]}`, `{} {}`, `{"a":}`, `{"a":` + strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66) + "}", "{\"x\":\"\xff\"}"} {
		if _, err := normalizePayload([]byte(value)); err == nil {
			t.Fatalf("accepted invalid payload %q", value)
		}
	}
	if _, err := normalizePayload([]byte(`{"x":"` + strings.Repeat("a", MaxPayloadBytes) + `"}`)); err == nil {
		t.Fatal("accepted payload above its byte limit")
	}
}

func TestMetadataEncodingRejectsAmbiguousAndForeignFields(t *testing.T) {
	value := ownerMarker{Version: 1, DeploymentID: strings.Repeat("1", 32), StoreID: strings.Repeat("2", 32), Lock: identity{Device: 1, Inode: 2}}
	encoded, err := encode(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ownerMarker
	if err := decode(encoded, &decoded); err != nil || decoded != value {
		t.Fatal("canonical metadata did not round-trip")
	}
	for _, data := range [][]byte{bytes.TrimSpace(encoded), append(append([]byte(nil), encoded...), encoded...), bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":1,"password":"untrusted"`), 1)} {
		if decode(data, new(ownerMarker)) == nil {
			t.Fatal("ambiguous metadata was accepted")
		}
	}
}
