package notificationjournal

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeReferencesPreservesOrderNullFieldsAndOwnership(t *testing.T) {
	raw := []byte(`[{"Kind":"Item","Id":"003","LibraryId":null,"SourceId":null},{"Kind":"Entity","Id":"42"},{"Kind":"Item","Id":"001"}]`)
	want := []Reference{{Kind: "Item", ID: "003"}, {Kind: "Entity", ID: "42"}, {Kind: "Item", ID: "001"}}
	refs, err := DecodeReferences("UserDataInvalidated", "owner", raw, false)
	if err != nil || !reflect.DeepEqual(refs, want) {
		t.Fatalf("decoded references = %+v, %v; want %+v", refs, err, want)
	}
	clear(raw)
	if !reflect.DeepEqual(refs, want) {
		t.Fatal("decoded references retained caller-owned JSON bytes")
	}
	for _, kind := range []string{"CatalogInvalidated", "UserDataInvalidated", "ResyncRequired", "Test"} {
		refs, err := DecodeReferences(kind, "", []byte("[]"), true)
		if err != nil || refs == nil || len(refs) != 0 {
			t.Fatalf("empty %s delivery = %+v, %v", kind, refs, err)
		}
	}
}

func TestDecodeReferencesRejectsInvalidTailWithoutPartialResults(t *testing.T) {
	const valid = `{"Kind":"Item","Id":"item","LibraryId":"library","SourceId":"source"}`
	for name, tail := range map[string]string{
		"duplicate":       valid,
		"unknown field":   `{"Kind":"Item","Id":"other","LibraryId":"library","Secret":"private"}`,
		"case variant":    `{"kind":"Item","Id":"other","LibraryId":"library"}`,
		"wrong type":      `{"Kind":"Item","Id":7,"LibraryId":"library"}`,
		"missing scope":   `{"Kind":"Item","Id":"other"}`,
		"wrong namespace": `{"Kind":"Entity","Id":"42"}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte("[" + valid + "," + tail + "]")
			refs, err := DecodeReferences("CatalogInvalidated", "", raw, false)
			if !errors.Is(err, ErrJournal) || refs != nil {
				t.Fatalf("invalid tail returned partial references: %+v, %v", refs, err)
			}
			if !errors.Is(ValidateReferences("CatalogInvalidated", "", raw, false), ErrJournal) {
				t.Fatal("validation-only consumer accepted an invalid tail")
			}
		})
	}
	for _, raw := range []string{"null", "[]", "[" + valid + "] {}"} {
		if refs, err := DecodeReferences("CatalogInvalidated", "", []byte(raw), false); !errors.Is(err, ErrJournal) || refs != nil {
			t.Fatalf("invalid source framing returned references: %+v, %v", refs, err)
		}
	}
}

func TestDecodeReferencesPreservesReferenceAndByteLimits(t *testing.T) {
	want := make([]Reference, 4096)
	for index := range want {
		want[index] = Reference{Kind: "Item", ID: fmt.Sprintf("item-%d", index), LibraryID: "library"}
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := DecodeReferences("CatalogInvalidated", "", raw, false)
	if err != nil || !reflect.DeepEqual(refs, want) {
		t.Fatalf("maximum reference set was changed: count=%d, error=%v", len(refs), err)
	}
	excess, err := json.Marshal(append(want, Reference{Kind: "Item", ID: "one-more", LibraryID: "library"}))
	if err != nil {
		t.Fatal(err)
	}
	if refs, err := DecodeReferences("CatalogInvalidated", "", excess, false); !errors.Is(err, ErrJournal) || refs != nil {
		t.Fatalf("excessive reference set returned data: count=%d, error=%v", len(refs), err)
	}
	atLimit := append(raw, []byte(strings.Repeat(" ", 524288-len(raw)))...)
	if refs, err := DecodeReferences("CatalogInvalidated", "", atLimit, false); err != nil || len(refs) != len(want) {
		t.Fatalf("valid byte-limit document failed: count=%d, error=%v", len(refs), err)
	}
	if refs, err := DecodeReferences("CatalogInvalidated", "", append(atLimit, ' '), false); !errors.Is(err, ErrJournal) || refs != nil {
		t.Fatalf("excessive byte document returned data: count=%d, error=%v", len(refs), err)
	}
}
