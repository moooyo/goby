package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestRootBindingWriteUnitRejectsInvalidApprovalBeforeDatabaseAccess(t *testing.T) {
	valid := RootBindingUpdate{Revision: "1", ObservedFingerprint: strings.Repeat("a0", 32), AcknowledgeMissingRemoval: true}
	for _, test := range []struct {
		name   string
		change func(*RootBindingUpdate)
	}{
		{"missing acknowledgement", func(input *RootBindingUpdate) { input.AcknowledgeMissingRemoval = false }},
		{"empty revision", func(input *RootBindingUpdate) { input.Revision = "" }},
		{"zero revision", func(input *RootBindingUpdate) { input.Revision = "0" }},
		{"negative revision", func(input *RootBindingUpdate) { input.Revision = "-1" }},
		{"signed revision", func(input *RootBindingUpdate) { input.Revision = "+1" }},
		{"padded revision", func(input *RootBindingUpdate) { input.Revision = "01" }},
		{"fractional revision", func(input *RootBindingUpdate) { input.Revision = "1.0" }},
		{"exponent revision", func(input *RootBindingUpdate) { input.Revision = "1e2" }},
		{"whitespace revision", func(input *RootBindingUpdate) { input.Revision = "1 " }},
		{"unbounded revision", func(input *RootBindingUpdate) { input.Revision = strings.Repeat("1", 1000) }},
		{"empty fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = "" }},
		{"uppercase fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("A", 64) }},
		{"nonhex fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("g", 64) }},
		{"short fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("a", 63) }},
		{"long fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("a", 65) }},
		{"control fingerprint", func(input *RootBindingUpdate) { input.ObservedFingerprint = strings.Repeat("a", 63) + "\n" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.change(&input)
			if _, err := (&Store{}).UpdateRootBinding(context.Background(), identity.Principal{}, "library", "root", input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid approval reached database access: %v", err)
			}
		})
	}
	for _, value := range []string{"", " leading", "trailing ", "control\n", string([]byte{0xff}), strings.Repeat("x", 257)} {
		t.Run(fmt.Sprintf("identifier %q", value), func(t *testing.T) {
			for _, ids := range [][2]string{{value, "root"}, {"library", value}} {
				if _, err := (&Store{}).UpdateRootBinding(context.Background(), identity.Principal{}, ids[0], ids[1], valid); !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("invalid scope reached database access: %v", err)
				}
			}
		})
	}
	if _, err := (&Store{}).UpdateRootBinding(nil, identity.Principal{}, "library", "root", valid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil context reached database access: %v", err)
	}
	var unavailable *Store
	if _, err := unavailable.UpdateRootBinding(context.Background(), identity.Principal{}, "library", "root", valid); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil store did not fail closed: %v", err)
	}
}

func TestRootBindingWriteUnitPreservesExactRevisionAndRejectsOverflow(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int64
	}{
		{"1", 1},
		{"9007199254740993", 9007199254740993},
		{"9223372036854775807", math.MaxInt64},
	} {
		input := RootBindingUpdate{Revision: test.input, ObservedFingerprint: strings.Repeat("0f", 32), AcknowledgeMissingRemoval: true}
		if value, err := rootBindingUpdateRevision(input); err != nil || value != test.want {
			t.Fatalf("revision lost integer precision: got = %d, error = %v", value, err)
		}
	}
	for _, value := range []string{"9223372036854775808", "9999999999999999999"} {
		input := RootBindingUpdate{Revision: value, ObservedFingerprint: strings.Repeat("0f", 32), AcknowledgeMissingRemoval: true}
		if _, err := (&Store{}).UpdateRootBinding(context.Background(), identity.Principal{}, "library", "root", input); !errors.Is(err, ErrRootBindingConflict) {
			t.Fatalf("revision overflow reached database access: %v", err)
		}
	}
}
