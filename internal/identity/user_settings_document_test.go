package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestUserSettingsDocumentCanonicalAndIndependent(t *testing.T) {
	for _, test := range []struct {
		name   string
		data   string
		rowErr error
		values map[string]string
	}{
		{name: "missing", rowErr: pgx.ErrNoRows, values: map[string]string{}},
		{name: "empty", data: " { } ", values: map[string]string{}},
		{
			name:   "strings and escapes",
			data:   ` { "z": "<>&", "a": "", "line": "\u2028" } `,
			values: map[string]string{"a": "", "line": "\u2028", "z": "<>&"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := userSettingsDocumentTestTx{data: []byte(test.data), err: test.rowErr}
			first, err := readUserSettings(context.Background(), tx, "user", false)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(test.values)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first.values, test.values) || !bytes.Equal(first.canonical, want) {
				t.Fatalf("unexpected validated document: %#v %s", first.values, first.canonical)
			}
			second, err := readUserSettings(context.Background(), tx, "user", true)
			if err != nil {
				t.Fatal(err)
			}
			first.values["changed"] = "local"
			if !bytes.Equal(first.canonical, want) {
				t.Fatal("mutating the map changed its canonical before-image")
			}
			first.canonical[0] = '['
			for index := range tx.data {
				tx.data[index] = 'x'
			}
			if !reflect.DeepEqual(second.values, test.values) || !bytes.Equal(second.canonical, want) {
				t.Fatal("another document shared mutable input, values, or canonical bytes")
			}
		})
	}
}

func TestUserSettingsDocumentRejectsMalformedStoredState(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "empty input", data: ""},
		{name: "malformed", data: `{"key":`},
		{name: "null root", data: "null"},
		{name: "array root", data: "[]"},
		{name: "null member", data: `{"key":null}`},
		{name: "number member", data: `{"key":1}`},
		{name: "boolean member", data: `{"key":true}`},
		{name: "object member", data: `{"key":{}}`},
		{name: "array member", data: `{"key":[]}`},
		{name: "empty key", data: `{"":"value"}`},
		{name: "control key", data: `{"bad\nkey":"value"}`},
		{name: "null character", data: `{"key":"bad\u0000value"}`},
		{name: "case aliases", data: `{"Theme":"dark","theme":"light"}`},
		{name: "unicode aliases", data: `{"K":"one","\u212a":"two"}`},
	}
	tooMany := make(map[string]string, MaxUserSettingsEntries+1)
	for index := 0; index <= MaxUserSettingsEntries; index++ {
		tooMany[fmt.Sprintf("key-%03d", index)] = "value"
	}
	tooLarge := make(map[string]string, 9)
	for index := 0; index < 9; index++ {
		tooLarge[fmt.Sprintf("key-%03d", index)] = strings.Repeat("v", MaxUserSettingsValueBytes)
	}
	for name, values := range map[string]map[string]string{
		"key limit":      {strings.Repeat("k", MaxUserSettingsKeyBytes+1): "value"},
		"value limit":    {"key": strings.Repeat("v", MaxUserSettingsValueBytes+1)},
		"entry limit":    tooMany,
		"document limit": tooLarge,
	} {
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct {
			name string
			data string
		}{name: name, data: string(data)})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			document, err := readUserSettings(context.Background(), userSettingsDocumentTestTx{data: []byte(test.data)}, "user", false)
			if !errors.Is(err, ErrStoredUserSettings) || document.values != nil || document.canonical != nil {
				t.Fatalf("invalid stored document escaped validation: %#v %q %v", document.values, document.canonical, err)
			}
		})
	}
}

type userSettingsDocumentTestTx struct {
	pgx.Tx
	data []byte
	err  error
}

func (tx userSettingsDocumentTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return userSettingsDocumentTestRow{data: tx.data, err: tx.err}
}

type userSettingsDocumentTestRow struct {
	data []byte
	err  error
}

func (row userSettingsDocumentTestRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*[]byte) = row.data
	return nil
}
