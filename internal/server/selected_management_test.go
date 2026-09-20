package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestSelectedManagementDeletionFolderQueryIsClosedAndPresenceAware(t *testing.T) {
	query, err := parseDeletionFolderQuery(httptest.NewRequest(http.MethodGet, "/admin/v1/policy/deletion-folders?SearchTerm=Music&LibraryId=library&StartIndex=4&Limit=50", nil))
	if err != nil || query.SearchTerm != "Music" || query.LibraryID != "library" || query.StartIndex != 4 || query.Limit != 50 {
		t.Fatalf("folder query: %+v %v", query, err)
	}
	for _, raw := range []string{"Limit=0", "Limit=201", "Limit=01", "StartIndex=-1", "StartIndex=2147483648", "Limit=2&Limit=2", "searchterm=wrong", "UserId=other", "LibraryId=a&LibraryId=b", "SearchTerm=%ff"} {
		if _, err := parseDeletionFolderQuery(httptest.NewRequest(http.MethodGet, "/admin/v1/policy/deletion-folders?"+raw, nil)); err == nil {
			t.Errorf("accepted query %q", raw)
		}
	}
}

func TestSelectedManagementLibraryOptionsDescribeActualLocalConsumers(t *testing.T) {
	defaults := library.DefaultLibraryOptions()
	want := map[string]any{
		"MetadataSavers": []any{}, "MetadataReaders": []map[string]any{{"Name": "Nfo", "DefaultEnabled": defaults.EnableLocalMetadata, "Features": []string{}}},
		"SubtitleFetchers": []any{}, "LyricsFetchers": []any{},
		"TypeOptions":           []map[string]any{{"Type": "Audio", "MetadataFetchers": []any{}, "ImageFetchers": []map[string]any{{"Name": "Goby Embedded Artwork", "DefaultEnabled": true, "Features": []string{}}}, "SupportedImageTypes": []string{"Primary"}, "DefaultImageOptions": []any{}}},
		"DefaultLibraryOptions": embyEditableLibraryOptions(library.Library{Options: &defaults}),
	}
	if !reflect.DeepEqual(supportedLibraryOptionsDTO(), want) {
		t.Fatal("capabilities diverged from the closed scanner default contract")
	}
	request := httptest.NewRequest(http.MethodGet, "/emby/Libraries/AvailableOptions", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	if selectedManagementEmptyBody(response, request) || response.Code != http.StatusBadRequest {
		t.Fatal("capability read accepted a request body")
	}
}
