package media

import (
	"strings"
	"testing"
)

func TestDolbyVisionRPUValidationAllowsOnlyExactAnnexBStructuralWarnings(t *testing.T) {
	const warning = "[hevc_mp4toannexb @ 0x7f5abc123000] No parameter sets in the extradata\n"
	for _, test := range []struct {
		name, diagnostic string
	}{
		{"empty", ""},
		{"one complete warning", warning},
		{"repeated complete warnings", warning + warning},
		{"separate filter contexts", warning + strings.Replace(warning, "7f5abc123000", "abcdef1234567890", 1)},
		{"upper case pointer digits", strings.Replace(warning, "7f5abc123000", "ABCDEF01", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !dolbyVisionRPUValidationStderrAllowed([]byte(test.diagnostic)) {
				t.Fatalf("exact structural diagnostic was rejected: %q", test.diagnostic)
			}
		})
	}
	for name, diagnostic := range map[string]string{
		"only newline":              "\n",
		"only whitespace":           " \t\r\n",
		"unprefixed message":        "No parameter sets in the extradata\n",
		"different filter context":  strings.Replace(warning, "hevc_mp4toannexb", "hevc_metadata", 1),
		"decoder context":           strings.Replace(warning, "hevc_mp4toannexb", "hevc", 1),
		"native RPU context":        strings.Replace(warning, "hevc_mp4toannexb", "dovi_rpu", 1),
		"context suffix injection":  strings.Replace(warning, "hevc_mp4toannexb", "hevc_mp4toannexb_fake", 1),
		"prefixed text":             "error " + warning,
		"leading whitespace":        " " + warning,
		"missing pointer":           strings.Replace(warning, "0x7f5abc123000", "", 1),
		"missing hex prefix":        strings.Replace(warning, "0x7f5abc123000", "7f5abc123000", 1),
		"nonhex pointer":            strings.Replace(warning, "7f5abc123000", "7f5x123", 1),
		"oversized pointer":         strings.Replace(warning, "7f5abc123000", "12345678901234567", 1),
		"pointer text injection":    strings.Replace(warning, "7f5abc123000", "7f5abc] error", 1),
		"approximate message":       strings.Replace(warning, "parameter sets", "parameter set", 1),
		"message suffix":            strings.TrimSuffix(warning, "\n") + "; RPU CRC mismatch\n",
		"unterminated message":      strings.TrimSuffix(warning, "\n"),
		"carriage return":           strings.Replace(warning, "\n", "\r\n", 1),
		"terminal control sequence": "\x1b[31m" + warning,
		"trailing whitespace":       warning + " ",
		"trailing blank line":       warning + "\n",
		"unknown after known":       warning + "unknown decoder warning\n",
		"unknown before known":      "unknown decoder warning\n" + warning,
		"RPU warning after known":   warning + "[dovi_rpu @ 0x1234] RPU CRC mismatch\n",
		"RPU warning before known":  "[dovi_rpu @ 0x1234] RPU CRC mismatch\n" + warning,
		"known context other error": warning + "[hevc_mp4toannexb @ 0x1234] Invalid NAL unit\n",
		"repeat summary":            warning + "    Last message repeated 1 times\n",
		"second partial warning":    warning + strings.TrimSuffix(warning, "\n"),
		"embedded null":             strings.Replace(warning, "parameter sets", "parameter\x00 sets", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if dolbyVisionRPUValidationStderrAllowed([]byte(diagnostic)) {
				t.Fatalf("unknown or ambiguous validation diagnostic was accepted: %q", diagnostic)
			}
		})
	}
}
