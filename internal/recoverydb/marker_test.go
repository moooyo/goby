package recoverydb

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/lifecycle"
)

const markerDeploymentFixture = "0123456789abcdef0123456789abcdef"
const markerGenerationFixture = "fedcba9876543210fedcba9876543210"
const markerInitialFixture = `{"version":1,"deploymentId":"0123456789abcdef0123456789abcdef","generationId":"","slot":"primary"}`
const markerRecoveryFixture = `{"version":1,"deploymentId":"0123456789abcdef0123456789abcdef","generationId":"fedcba9876543210fedcba9876543210","slot":"recovery"}`

func TestMarkerRoundTripIncludesInitialEmptyGeneration(t *testing.T) {
	for name, marker := range map[string]Marker{
		"initial_primary":     {Version: 1, DeploymentID: markerDeploymentFixture, Slot: lifecycle.DatabasePrimary},
		"primary_generation":  {Version: 1, DeploymentID: markerDeploymentFixture, GenerationID: markerGenerationFixture, Slot: lifecycle.DatabasePrimary},
		"recovery_generation": {Version: 1, DeploymentID: markerDeploymentFixture, GenerationID: markerGenerationFixture, Slot: lifecycle.DatabaseRecovery},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := EncodeMarker(marker)
			if err != nil {
				t.Fatalf("encode valid marker: %v", err)
			}
			decoded, err := DecodeMarker(encoded)
			if err != nil || decoded != marker {
				t.Fatal("valid marker did not round-trip exactly")
			}
			if name == "initial_primary" && encoded != markerInitialFixture {
				t.Fatal("initial marker omitted or changed the explicit empty generation")
			}
			if name == "recovery_generation" && encoded != markerRecoveryFixture {
				t.Fatal("local encoding did not emit the canonical four-field representation")
			}
		})
	}
}

func TestMarkerDecodeAcceptsEquivalentOrderWhitespaceAndEscapes(t *testing.T) {
	for name, value := range map[string]string{
		"reordered":               `{"slot":"recovery","generationId":"fedcba9876543210fedcba9876543210","deploymentId":"0123456789abcdef0123456789abcdef","version":1}`,
		"whitespace":              " \n\t{\n  \"slot\": \"recovery\", \"generationId\": \"fedcba9876543210fedcba9876543210\",\n  \"deploymentId\": \"0123456789abcdef0123456789abcdef\", \"version\": 1\n}\r\n ",
		"escaped_key":             strings.Replace(markerRecoveryFixture, `"version"`, `"\u0076ersion"`, 1),
		"escaped_value":           strings.Replace(markerRecoveryFixture, `"recovery"`, `"\u0072ecovery"`, 1),
		"escaped_identifier_byte": strings.Replace(markerRecoveryFixture, `"0123456789abcdef0123456789abcdef"`, `"\u0030123456789abcdef0123456789abcdef"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			before := value
			decoded, err := DecodeMarker(value)
			want := Marker{Version: 1, DeploymentID: markerDeploymentFixture, GenerationID: markerGenerationFixture, Slot: lifecycle.DatabaseRecovery}
			if err != nil || decoded != want {
				t.Fatalf("equivalent marker JSON rejected: %v", err)
			}
			raw := RawMarker{Present: true, Value: value}
			if err := ValidateRawMarker(raw); err != nil || raw.Value != before {
				t.Fatal("raw compare-and-swap observation was normalized")
			}
		})
	}
}

func TestMarkerDecodeRequiresEveryFieldExactlyOnce(t *testing.T) {
	for _, field := range []string{
		`"version":1,`,
		`"deploymentId":"0123456789abcdef0123456789abcdef",`,
		`"generationId":"",`,
		`,"slot":"primary"`,
	} {
		t.Run("missing_"+field, func(t *testing.T) {
			value := strings.Replace(markerInitialFixture, field, "", 1)
			assertInvalidMarker(t, value)
		})
	}
	for name, member := range map[string]string{
		"version":             `"version":1`,
		"deployment":          `"deploymentId":"0123456789abcdef0123456789abcdef"`,
		"generation":          `"generationId":""`,
		"slot":                `"slot":"primary"`,
		"escaped_version":     `"\u0076ersion":1`,
		"escaped_deployment":  `"deployment\u0049d":"0123456789abcdef0123456789abcdef"`,
		"escaped_generation":  `"generation\u0049d":""`,
		"escaped_slot":        `"\u0073lot":"primary"`,
		"conflicting_version": `"version":2`,
		"unknown":             `"authority":true`,
		"case_variant":        `"Version":1`,
	} {
		t.Run("additional_"+name, func(t *testing.T) {
			assertInvalidMarker(t, strings.TrimSuffix(markerInitialFixture, "}")+","+member+"}")
		})
	}
	assertInvalidMarker(t, strings.Replace(markerInitialFixture, `"version"`, `"Version"`, 1))
}

func TestMarkerDecodeRejectsWrongTypesAndNonintegerVersions(t *testing.T) {
	for _, value := range []string{"0", "2", "-1", "1.0", "1e0", "1E+0", "9007199254740993", "18446744073709551616", "null", "true", `"1"`, "[]", "{}"} {
		t.Run("version_"+value, func(t *testing.T) {
			assertInvalidMarker(t, strings.Replace(markerInitialFixture, `"version":1`, `"version":`+value, 1))
		})
	}
	for name, original := range map[string]string{
		"deploymentId": `"0123456789abcdef0123456789abcdef"`,
		"generationId": `""`,
		"slot":         `"primary"`,
	} {
		for _, value := range []string{"null", "1", "false", "[]", "{}"} {
			t.Run(name+"_"+value, func(t *testing.T) {
				assertInvalidMarker(t, strings.Replace(markerInitialFixture, `"`+name+`":`+original, `"`+name+`":`+value, 1))
			})
		}
	}
}

func TestMarkerRejectsInvalidBindingClaims(t *testing.T) {
	valid := Marker{Version: 1, DeploymentID: markerDeploymentFixture, GenerationID: markerGenerationFixture, Slot: lifecycle.DatabaseRecovery}
	for name, mutate := range map[string]func(*Marker){
		"zero_version":              func(m *Marker) { m.Version = 0 },
		"future_version":            func(m *Marker) { m.Version = 2 },
		"empty_deployment":          func(m *Marker) { m.DeploymentID = "" },
		"short_deployment":          func(m *Marker) { m.DeploymentID = strings.Repeat("a", 31) },
		"long_deployment":           func(m *Marker) { m.DeploymentID = strings.Repeat("a", 33) },
		"uppercase_deployment":      func(m *Marker) { m.DeploymentID = strings.ToUpper(markerDeploymentFixture) },
		"non_hex_deployment":        func(m *Marker) { m.DeploymentID = strings.Repeat("g", 32) },
		"empty_recovery_generation": func(m *Marker) { m.GenerationID = "" },
		"short_generation":          func(m *Marker) { m.GenerationID = strings.Repeat("a", 31) },
		"long_generation":           func(m *Marker) { m.GenerationID = strings.Repeat("a", 33) },
		"uppercase_generation":      func(m *Marker) { m.GenerationID = strings.ToUpper(markerGenerationFixture) },
		"non_hex_generation":        func(m *Marker) { m.GenerationID = strings.Repeat("g", 32) },
		"empty_slot":                func(m *Marker) { m.Slot = "" },
		"unknown_slot":              func(m *Marker) { m.Slot = "archive" },
		"uppercase_slot":            func(m *Marker) { m.Slot = "Recovery" },
		"slot_whitespace":           func(m *Marker) { m.Slot = " recovery " },
	} {
		t.Run(name, func(t *testing.T) {
			marker := valid
			mutate(&marker)
			if value, err := EncodeMarker(marker); err != ErrInvalid || value != "" {
				t.Fatal("invalid binding claim produced an encoded marker")
			}
		})
	}
	for name, value := range map[string]string{
		"uppercase_deployment":      strings.Replace(markerRecoveryFixture, markerDeploymentFixture, strings.ToUpper(markerDeploymentFixture), 1),
		"invalid_generation":        strings.Replace(markerRecoveryFixture, markerGenerationFixture, strings.Repeat("g", 32), 1),
		"empty_recovery_generation": strings.Replace(markerRecoveryFixture, markerGenerationFixture, "", 1),
		"invalid_slot":              strings.Replace(markerRecoveryFixture, `"recovery"`, `"archive"`, 1),
	} {
		t.Run("decode_"+name, func(t *testing.T) { assertInvalidMarker(t, value) })
	}
}

func TestMarkerDecodeRejectsMalformedUnicodeWithoutReplacement(t *testing.T) {
	for name, value := range map[string]string{
		"invalid_utf8_key":               strings.Replace(markerInitialFixture, "version", "vers\xffion", 1),
		"invalid_utf8_value":             strings.Replace(markerInitialFixture, "primary", "prim\xffary", 1),
		"truncated_utf8":                 markerInitialFixture + "\xe4\xb8",
		"lone_high_surrogate_key":        strings.Replace(markerInitialFixture, `"version"`, `"vers\ud800ion"`, 1),
		"lone_low_surrogate_key":         strings.Replace(markerInitialFixture, `"version"`, `"vers\udc00ion"`, 1),
		"lone_high_surrogate_generation": strings.Replace(markerInitialFixture, `"generationId":""`, `"generationId":"\ud800"`, 1),
		"lone_low_surrogate_generation":  strings.Replace(markerInitialFixture, `"generationId":""`, `"generationId":"\udc00"`, 1),
		"surrogate_pair_slot":            strings.Replace(markerInitialFixture, `"primary"`, `"prim\ud800\udc00ary"`, 1),
		"reversed_surrogate_pair":        strings.Replace(markerInitialFixture, `"generationId":""`, `"generationId":"\udc00\ud800"`, 1),
		"replacement_character":          strings.Replace(markerInitialFixture, `"primary"`, `"prim\ufffdary"`, 1),
		"embedded_nul":                   strings.Replace(markerInitialFixture, `"primary"`, `"pri\u0000mary"`, 1),
	} {
		t.Run(name, func(t *testing.T) { assertInvalidMarker(t, value) })
	}
}

func TestMarkerDecodeRejectsIncompleteAndTrailingDocuments(t *testing.T) {
	for name, value := range map[string]string{
		"empty": "", "spaces": " \n\t", "null": "null", "array": "[]", "string": `"marker"`, "empty_object": "{}",
		"second_object":    markerInitialFixture + markerRecoveryFixture,
		"trailing_null":    markerInitialFixture + " null",
		"trailing_false":   markerInitialFixture + " false",
		"trailing_garbage": markerInitialFixture + "x",
		"trailing_comment": markerInitialFixture + " // comment",
		"trailing_comma":   strings.TrimSuffix(markerInitialFixture, "}") + ",}",
	} {
		t.Run(name, func(t *testing.T) { assertInvalidMarker(t, value) })
	}
	for length := 0; length < len(markerInitialFixture); length++ {
		assertInvalidMarker(t, markerInitialFixture[:length])
	}
}

func TestMarkerBoundsCountUTF8BytesAndAcceptExactLimit(t *testing.T) {
	atLimit := markerInitialFixture + strings.Repeat(" ", MaxMarkerBytes-len(markerInitialFixture))
	if _, err := DecodeMarker(atLimit); err != nil {
		t.Fatal("marker at exact byte limit was rejected")
	}
	assertInvalidMarker(t, atLimit+" ")
	if err := ValidateRawMarker(RawMarker{Present: true, Value: strings.Repeat("a", MaxMarkerBytes)}); err != nil {
		t.Fatal("raw marker at exact byte limit was rejected")
	}
	if err := ValidateRawMarker(RawMarker{Present: true, Value: strings.Repeat("a", MaxMarkerBytes+1)}); err != ErrInvalid {
		t.Fatal("oversized raw marker was accepted")
	}
	if err := ValidateRawMarker(RawMarker{Present: true, Value: strings.Repeat("\u00e9", MaxMarkerBytes/2)}); err != nil {
		t.Fatal("UTF-8 raw marker at exact byte limit was rejected")
	}
	if err := ValidateRawMarker(RawMarker{Present: true, Value: strings.Repeat("\u00e9", MaxMarkerBytes/2) + "a"}); err != ErrInvalid {
		t.Fatal("raw marker limit counted characters instead of bytes")
	}
}

func TestRawMarkerKeepsAbsentEmptyNullAndForeignBytesDistinct(t *testing.T) {
	observations := []RawMarker{
		{},
		{Present: true, Value: ""},
		{Present: true, Value: "null"},
		{Present: true, Value: "not JSON"},
		{Present: true, Value: " \n" + markerInitialFixture + "\n "},
		{Present: true, Value: markerInitialFixture},
		{Present: true, Value: strings.Replace(markerInitialFixture, markerDeploymentFixture, strings.Repeat("b", 32), 1)},
		{Present: true, Value: strings.Replace(markerInitialFixture, `"version":1`, `"version":2`, 1)},
		{Present: true, Value: `{"version":1,"version":2}`},
		{Present: true, Value: `"\ud800"`},
	}
	for i, observation := range observations {
		before := observation
		if err := ValidateRawMarker(observation); err != nil || observation != before {
			t.Fatalf("raw observation %d was rejected or normalized", i)
		}
		for j := 0; j < i; j++ {
			if observations[j] == observation {
				t.Fatalf("different storage observations %d and %d collapsed", j, i)
			}
		}
	}
	for _, observation := range observations[1:4] {
		assertInvalidMarker(t, observation.Value)
	}
	for _, raw := range []RawMarker{
		{Present: false, Value: "null"},
		{Present: false, Value: markerInitialFixture},
		{Present: true, Value: "invalid\xffUTF8"},
	} {
		if err := ValidateRawMarker(raw); err != ErrInvalid {
			t.Fatal("invalid raw storage observation was accepted")
		}
	}
}

func TestMarkerDecodeDoesNotAuthorizeForeignClaims(t *testing.T) {
	foreign := strings.Replace(markerRecoveryFixture, markerDeploymentFixture, strings.Repeat("b", 32), 1)
	marker, err := DecodeMarker(foreign)
	if err != nil || marker.DeploymentID != strings.Repeat("b", 32) {
		t.Fatal("codec rejected or rewrote a structurally valid foreign claim")
	}
	// The codec deliberately cannot choose a local owner. The caller must bind
	// this foreign claim to protected local state before any database mutation.
	if marker.DeploymentID == markerDeploymentFixture {
		t.Fatal("foreign claim was converted into the local deployment identity")
	}
}

func assertInvalidMarker(t *testing.T, value string) {
	t.Helper()
	marker, err := DecodeMarker(value)
	if err != ErrInvalid || marker != (Marker{}) {
		t.Fatal("invalid JSON returned a usable or partially populated marker")
	}
}
