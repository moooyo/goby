package library

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetadataJSONObjectOwnsEachDecodedValue(t *testing.T) {
	expected := map[string]string{
		"First":  `"first"`,
		"Second": `"second"`,
		"Number": `90071992547409931234`,
		"Nested": `{"Values":[null,true,{"Number":90071992547409931235}]}`,
		"Long":   `"` + strings.Repeat("long value ", 1024) + `"`,
	}
	raw := []byte(`{"First":` + expected["First"] + `,"Second":` + expected["Second"] +
		`,"Number":` + expected["Number"] + `,"Nested":` + expected["Nested"] + `,"Long":` + expected["Long"] + `}`)
	original := bytes.Clone(raw)
	object, err := metadataJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	for index := range raw {
		raw[index] = 'x'
	}
	for field, want := range expected {
		if got := string(object[field]); got != want {
			t.Fatalf("input mutation changed %s: got %q, want %q", field, got, want)
		}
	}
	for field := range expected {
		t.Run(field, func(t *testing.T) {
			input := bytes.Clone(original)
			object, err := metadataJSONObject(input)
			if err != nil {
				t.Fatal(err)
			}
			for index := range object[field] {
				object[field][index] = '!'
			}
			if !bytes.Equal(input, original) {
				t.Fatalf("%s aliases the input document", field)
			}
			for other, want := range expected {
				if other != field && string(object[other]) != want {
					t.Fatalf("mutating %s changed independent field %s", field, other)
				}
			}
		})
	}
}

func TestMetadataJSONObjectRetainsDocumentValidation(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"nonobject", `[1,2,3]`},
		{"null", `null`},
		{"duplicate", `{"Name":"first","Name":"second"}`},
		{"escaped-duplicate", `{"Name":"first","\u004eame":"second"}`},
		{"incomplete", `{"Name":"first"`},
		{"trailing-object", `{"Name":"first"} {}`},
		{"trailing-scalar", `{"Name":"first"} true`},
		{"invalid-value", `{"Number":01}`},
		{"invalid-utf8", "{\"Name\":\"" + string([]byte{0xff}) + "\"}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if object, err := metadataJSONObject([]byte(test.raw)); err == nil || object != nil {
				t.Fatalf("invalid document returned values: object=%v error=%v", object, err)
			}
		})
	}
}
