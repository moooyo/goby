package metadata

import (
	"bytes"
	"encoding/xml"
	"errors"
	"math"
	"strings"
)

// NFOSeasonMatchesRecovery checks all explicit season numbers in the original
// document. Normal imports intentionally ignore the seasonnumber spelling.
func NFOSeasonMatchesRecovery(data []byte, expected int) bool {
	roots, err := readNFORoots(bytes.NewReader(data), false)
	if err != nil || roots[0].name.Space != "" || !strings.EqualFold(roots[0].name.Local, "season") {
		return false
	}
	for _, field := range roots[0].children {
		name := strings.ToLower(field.name.Local)
		if field.name.Space != "" || name != "season" && name != "seasonnumber" {
			continue
		}
		text, err := scalarText(field)
		if err != nil {
			return false
		}
		number, err := parseInteger(text, name, 0, math.MaxInt32)
		if err != nil || number == nil || *number != expected {
			return false
		}
	}
	return true
}

// NFOIntegrityFailure classifies only wire/structural damage, not every import
// rejection. Its caller must separately establish a stable, complete read.
// Unsupported encodings, resource limits, and metadata/identity disagreements
// are not permission to replace a document.
func NFOIntegrityFailure(data []byte) string {
	if len(data) == 0 {
		return "empty"
	}
	if len(data) > maxNFOBytes || bytes.HasPrefix(data, []byte{0xff, 0xfe, 0, 0}) ||
		bytes.HasPrefix(data, []byte{0, 0, 0xfe, 0xff}) ||
		bytes.HasPrefix(data, []byte{0, '<', 0}) || bytes.HasPrefix(data, []byte{'<', 0}) ||
		bytes.HasPrefix(data, []byte{0, 0, 0, '<'}) {
		return ""
	}
	decoded, enc, err := nfoUTF8Bytes(data)
	if err != nil {
		// The prefix of a UTF-8/ASCII declaration is available even when
		// later bytes are bad. Do not label a known unsupported encoding
		// as corruption merely because it failed UTF-8 validation.
		if enc == nfoUTF8 && !knownNFOIntegrityDeclaration(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), enc) {
			return ""
		}
		return "malformed"
	}
	if !knownNFOIntegrityDeclaration(decoded, enc) {
		return ""
	}
	// Structural integrity is independent of whether this file is entitled
	// to carry two episode roots, or has accepted fields and numbering.
	_, err = parseXML(decoded, enc, true)
	var syntax *xml.SyntaxError
	if errors.As(err, &syntax) {
		return "malformed"
	}
	return ""
}

func knownNFOIntegrityDeclaration(data []byte, enc nfoEncoding) bool {
	if !bytes.HasPrefix(data, []byte("<?xml")) || len(data) <= 5 || !xmlWhitespace(data[5:6]) {
		return true
	}
	declaration, _, found := bytes.Cut(data[5:min(len(data), maxFieldBytes+5)], []byte("?>"))
	return !found || checkDeclaration(declaration, enc) == nil
}
