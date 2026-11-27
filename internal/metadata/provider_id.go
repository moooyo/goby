package metadata

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxProviderKey = 64
	maxProviderID  = 256
)

// ProviderKeyEqual matches a known ASCII provider name without allowing a
// Unicode lookalike from an imported identifier to select an adapter.
func ProviderKeyEqual(key, name string) bool {
	return strings.EqualFold(key, name) && !strings.ContainsFunc(key, func(ch rune) bool { return ch > unicode.MaxASCII })
}

// ValidateLocalProviderID bounds opaque metadata in imported or locked values.
// It does not validate an identifier for an online request.
func ValidateLocalProviderID(key, value string) error {
	if !validProviderText(key, maxProviderKey) {
		return fmt.Errorf("provider key must be bounded nonblank UTF-8 without control characters")
	}
	if !validProviderText(value, maxProviderID) {
		return fmt.Errorf("provider ID must be bounded nonblank UTF-8 without control characters")
	}
	return nil
}

func validProviderText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) != "" && !strings.ContainsFunc(value, unicode.IsControl)
}
