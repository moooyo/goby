package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestLibraryStorageDirectoryWirePreservesExactBytesAndUnknowns(t *testing.T) {
	readable, writable := true, false
	total, used, available := "18446744073709551615", "9007199254740993", "9007199254740991"
	for _, test := range []struct {
		name string
		item library.LibraryStorageDirectory
		want map[string]any
	}{
		{
			name: "exact decimal bytes",
			item: library.LibraryStorageDirectory{
				Path: "/media/registered", Available: true, Readable: &readable, Writable: &writable,
				TotalBytes: &total, UsedBytes: &used, AvailableBytes: &available,
			},
			want: map[string]any{
				"Path": "/media/registered", "Available": true, "Readable": true, "Writable": false,
				"TotalBytes": total, "UsedBytes": used, "AvailableBytes": available,
			},
		},
		{
			name: "unknown bytes remain null",
			item: library.LibraryStorageDirectory{Path: "/media/unknown"},
			want: map[string]any{
				"Path": "/media/unknown", "Available": false, "Readable": nil, "Writable": nil,
				"TotalBytes": nil, "UsedBytes": nil, "AvailableBytes": nil,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.item)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("directory capacity DTO changed exact bytes, unknown values, or public fields: %s", encoded)
			}
		})
	}
}

func TestLibraryStorageDirectoriesRejectsSelectorsAndMissingCatalog(t *testing.T) {
	s := &Server{}
	for _, suffix := range []string{"?", "?Path=/private-marker", "?Limit=1", "?api_key=private-marker"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/admin/v1/storage/library-directories"+suffix, nil)
		s.libraryStorageDirectories(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("directory selector reached catalog access: %q %d %s", suffix, response.Code, response.Body.String())
		}
		var result struct{ Error struct{ Code string } }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Error.Code != "invalid_query" {
			t.Fatalf("directory selector did not retain the closed query contract: %q %s", suffix, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/storage/library-directories", nil)
	request = request.WithContext(context.WithValue(request.Context(), principalKey, identity.Principal{Kind: "admin"}))
	s.libraryStorageDirectories(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing catalog fabricated directory observations: %d %s", response.Code, response.Body.String())
	}
}
