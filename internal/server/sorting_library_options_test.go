package server

import (
	"encoding/json"
	"fmt"
	"github.com/moooyo/goby/internal/notificationjournal"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSortingNativeInputIsClosedAndPreservesEmptyArrays(t *testing.T) {
	for _, raw := range []string{`{"SortRemoveWords":[]}`, `{"SortRemoveWords":["The","Ä"]}`} {
		response := httptest.NewRecorder()
		request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsExtendedUpdateBodyForTest(`"Sorting":`+raw), "application/json"))
		if !ok || request.Sorting == nil || request.Sorting.SortRemoveWords == nil {
			t.Fatalf("valid sorting input=%s status%d", raw, response.Code)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"SortRemoveWords":null}`, `{"SortRemoveWords":["Ä","ä"]}`, `{"SortRemoveWords":["the " ]}`, `{"SortRemoveWords":[],"private-marker":true}`} {
		response := httptest.NewRecorder()
		_, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsExtendedUpdateBodyForTest(`"Sorting":`+raw), "application/json"))
		if ok || response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-marker") {
			t.Fatalf("invalid sorting input accepted or reflected: %s", raw)
		}
	}
}

func TestSortingJournalBackpressureHasRetryableHTTPStatus(t *testing.T) {
	for _, failure := range []error{notificationjournal.ErrCapacity, notificationjournal.ErrJournal} {
		request := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", nil)
		response := httptest.NewRecorder()
		(&Server{}).settingsError(response, request, failure)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("native backpressure status=%d", response.Code)
		}
		response = httptest.NewRecorder()
		(&Server{}).configurationError(response, request, failure, "ManageServer")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("compatibility backpressure status=%d", response.Code)
		}
	}
}

func TestImageFetcherSelectorUsesAdvertisedDynamicProviderAndPreservesOtherOptions(t *testing.T) {
	for _, test := range []struct {
		raw     string
		enabled bool
	}{
		{`{"TypeOptions":[]}`, true}, {`{"TypeOptions":[{"Type":"Audio","ImageFetchers":[]}]}`, false},
		{`{"TypeOptions":[{"Type":"Audio","ImageFetchers":["Goby Embedded Artwork"],"ImageFetcherOrder":["Goby Embedded Artwork"]}]}`, true},
		{`{"typeoptions":[{"TYPE":"Audio","IMAGEFETCHERS":[]}]}`, false},
	} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(test.raw), &wire); err != nil {
			t.Fatal(err)
		}
		native, err := wire.native()
		if err != nil || native.EnableEmbeddedArtwork == nil || *native.EnableEmbeddedArtwork != test.enabled || native.EnableLocalImages != nil || native.EnableLocalMetadata != nil {
			t.Fatalf("selector changed another importer: %s %v", test.raw, err)
		}
	}
	for _, raw := range []string{`{}`, `{"TypeOptions":null}`, `{"TypeOptions":[{"Type":"Audio"}]}`, `{"TypeOptions":[{"Type":"Movie","ImageFetchers":[]}]}`, `{"TypeOptions":[{"Type":"Audio","ImageFetchers":["Image Extractor"]}]}`, `{"TypeOptions":[{"Type":"Audio","ImageFetchers":[],"MetadataFetchers":[]}]}`, `{"DisabledLocalMetadataReaders":null,"TypeOptions":[]}`} {
		var wire embyLibraryOptionsUpdate
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		if _, err := wire.native(); err == nil {
			t.Fatalf("unsupported selector accepted: %s", raw)
		}
	}
	var legacy embyLibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"DisabledLocalMetadataReaders":["Nfo"]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	native, err := legacy.native()
	if err != nil || native.EnableEmbeddedArtwork != nil || native.EnableLocalMetadata == nil || *native.EnableLocalMetadata {
		t.Fatal("legacy reader update changed artwork selection")
	}
}

func TestHTTPImageFetcherDuplicateAliasesRejectWithoutChangingLibrary(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Dir(writeAPIMediaFile(t, root, "sorting-options/Movie.mp4")), "movies")
	headers := http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}}
	before := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/libraries/"+id, nil, nil, cookie)), "Library")
	for _, options := range []string{
		`{"TypeOptions":[{"Type":"Audio","ImageFetchers":[],"ImageFetchers":["Goby Embedded Artwork"]}]}`,
		`{"TypeOptions":[{"Type":"Audio","type":"Audio","ImageFetchers":[]}]}`,
		`{"TypeOptions":[{"Type":"Audio","ImageFetchers":[],"imagefetchers":["Goby Embedded Artwork"]}]}`,
		`{"TypeOptions":[{"Type":"Audio","ImageFetchers":[],"ImageFetcherOrder":[],"imagefetcherorder":["Goby Embedded Artwork"]}]}`,
		`{"TypeOptions":[],"typeoptions":[{"Type":"Audio","ImageFetchers":[]}]}`,
	} {
		body := json.RawMessage(fmt.Sprintf(`{"Id":%q,"LibraryOptions":%s}`, id, options))
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Library/VirtualFolders/LibraryOptions", body, headers), http.StatusBadRequest)
		after := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/libraries/"+id, nil, nil, cookie)), "Library")
		if after["Revision"] != before["Revision"] || !reflect.DeepEqual(after["LibraryOptions"], before["LibraryOptions"]) {
			t.Fatalf("ambiguous selector partially changed library configuration: %s", options)
		}
	}
}
