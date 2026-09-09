package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

const adminMetadataEditJSON = `{"Revision":"1","Overrides":{"Overview":"Manual overview","ProductionYear":null,"Genres":[],"ProviderIds":{}},"LockedFields":["Overview"]}`

func adminMetadataRequestForTest(method, query, body, contentType string) *http.Request {
	r := httptest.NewRequest(method, "/admin/v1/items/item-id/metadata", strings.NewReader(body))
	r.URL.RawQuery = query
	r.SetPathValue("id", "item-id")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "metadata-request-id"))
}

func assertAdminMetadataInputError(t *testing.T, response *httptest.ResponseRecorder, status int, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("metadata parser status = %d, want %d", response.Code, status)
	}
	var body struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode metadata error: %v", err)
	}
	wantCode := "invalid_input"
	if status == http.StatusUnsupportedMediaType {
		wantCode = "unsupported_media_type"
	}
	if body.Error.Code != wantCode || body.RequestID != "metadata-request-id" {
		t.Fatal("metadata parser lost the native error envelope or request identifier")
	}
	if status == http.StatusBadRequest && (len(body.Error.Fields) == 0 || field != "" && body.Error.Fields[field] == "") {
		t.Fatalf("metadata parser omitted the expected field error for %q", field)
	}
}

func TestParseAdminMetadataQueryDefaultsAndCanonicalValues(t *testing.T) {
	for _, test := range []struct {
		query string
		want  library.MetadataItemQuery
	}{
		{"", library.MetadataItemQuery{Limit: 50}},
		{"SearchTerm=&Types=", library.MetadataItemQuery{Limit: 50}},
		{"SearchTerm=++Caf%C3%A9+Alpha++&Types=Movie,Episode&StartIndex=0&Limit=200", library.MetadataItemQuery{SearchTerm: "Caf\u00e9 Alpha", Types: []string{"Movie", "Episode"}, Limit: 200}},
		{"StartIndex=2147483647&Limit=1", library.MetadataItemQuery{StartIndex: 2147483647, Limit: 1}},
		{"Types=Movie,Video,Series,Season,Episode,Folder,MusicArtist,MusicAlbum,Audio", library.MetadataItemQuery{Types: []string{"Movie", "Video", "Series", "Season", "Episode", "Folder", "MusicArtist", "MusicAlbum", "Audio"}, Limit: 50}},
		{"SearchTerm=" + url.QueryEscape(strings.Repeat("\u00e9", 512)), library.MetadataItemQuery{SearchTerm: strings.Repeat("\u00e9", 512), Limit: 50}},
	} {
		request := adminMetadataRequestForTest(http.MethodGet, test.query, "", "")
		result, err := parseAdminMetadataQuery(request)
		if err != nil || !reflect.DeepEqual(result, test.want) || request.URL.RawQuery != test.query {
			t.Fatalf("canonical metadata query changed or was rejected: %v", err)
		}
	}
}

func TestParseAdminMetadataQueryRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	for _, test := range []struct{ query, field string }{
		{"searchterm=Alpha", "Query"}, {"SearchTerm=Alpha&searchterm=Alpha", "Query"}, {"UserId=other", "Query"},
		{"SearchTerm=Alpha&SearchTerm=Alpha", "SearchTerm"}, {"Limit=10&Limit=10", "Limit"},
		{"Types=Movie&Types=Movie", "Types"}, {"SearchTerm=%ff", "SearchTerm"}, {"Types=%ff", "Types"},
		{"SearchTerm=%zz", "Query"}, {"SearchTerm=a;b", "Query"}, {"SearchTerm=a%00b", "SearchTerm"},
		{"SearchTerm=a%0Ab", "SearchTerm"}, {"SearchTerm=" + url.QueryEscape(strings.Repeat("\u00e9", 513)), "SearchTerm"},
		{"StartIndex=-1", "StartIndex"}, {"StartIndex=2147483648", "StartIndex"}, {"StartIndex=01", "StartIndex"},
		{"StartIndex=%2B1", "StartIndex"}, {"StartIndex=", "StartIndex"}, {"StartIndex=1.0", "StartIndex"},
		{"Limit=0", "Limit"}, {"Limit=201", "Limit"}, {"Limit=01", "Limit"}, {"Limit=", "Limit"},
		{"Types=movie", "Types"}, {"Types=Movie,Movie", "Types"}, {"Types=Movie,", "Types"},
		{"Types=Movie,%20Episode", "Types"}, {"Types=Unknown", "Types"}, {"Types=CollectionFolder", "Types"},
	} {
		_, err := parseAdminMetadataQuery(adminMetadataRequestForTest(http.MethodGet, test.query, "", ""))
		var invalid *library.MetadataValidationError
		if !errors.As(err, &invalid) || invalid.Fields[test.field] == "" {
			t.Fatalf("invalid metadata query lacks the expected %s error: %v", test.field, err)
		}
	}
}

func TestAdminMetadataPathAndUndeclaredQueryBoundaries(t *testing.T) {
	for _, id := range []string{"opaque:metadata-id", "item with spaces", strings.Repeat("\u00e9", 128)} {
		request := adminMetadataRequestForTest(http.MethodGet, "", "", "")
		request.SetPathValue("id", id)
		response := httptest.NewRecorder()
		got, ok := adminMetadataID(response, request)
		if !ok || got != id || response.Body.Len() != 0 || !adminMetadataNoQuery(response, request) {
			t.Fatal("valid opaque metadata path was changed or rejected")
		}
	}
	for _, id := range []string{"", " item", "item ", "item\x00id", "item\xff", strings.Repeat("a", 257)} {
		request := adminMetadataRequestForTest(http.MethodGet, "", "", "")
		request.SetPathValue("id", id)
		response := httptest.NewRecorder()
		if _, ok := adminMetadataID(response, request); ok {
			t.Fatal("invalid metadata path was accepted")
		}
		assertAdminMetadataInputError(t, response, http.StatusBadRequest, "Id")
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, query := range []string{"UserId=other", "Revision=1", "api_key=not-authority", "%zz", "SearchTerm="} {
			response := httptest.NewRecorder()
			if adminMetadataNoQuery(response, adminMetadataRequestForTest(method, query, "", "")) {
				t.Fatal("metadata detail/write accepted an undeclared query")
			}
			assertAdminMetadataInputError(t, response, http.StatusBadRequest, "Query")
		}
	}
}

func TestDecodeAdminMetadataEditPreservesSparseReplacementAndExplicitClear(t *testing.T) {
	for _, revision := range []string{"1", "9007199254740993", "9223372036854775807"} {
		body := strings.Replace(adminMetadataEditJSON, `"Revision":"1"`, `"Revision":"`+revision+`"`, 1)
		response := httptest.NewRecorder()
		edit, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", body, "application/json; charset=utf-8"))
		if !ok || response.Body.Len() != 0 || edit.Revision != revision || len(edit.Overrides) != 4 || !reflect.DeepEqual(edit.LockedFields, []string{"Overview"}) {
			t.Fatal("metadata parser changed a revision or sparse replacement")
		}
		for field, expected := range map[string]string{"Overview": `"Manual overview"`, "ProductionYear": "null", "Genres": "[]", "ProviderIds": "{}"} {
			if string(edit.Overrides[field]) != expected {
				t.Fatalf("sparse override %s lost explicit empty/null semantics", field)
			}
		}
		if _, present := edit.Overrides["Name"]; present {
			t.Fatal("missing override was expanded into an unintended clear")
		}
	}
	response := httptest.NewRecorder()
	edit, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", `{"Revision":"1","Overrides":{},"LockedFields":[]}`, "application/json"))
	if !ok || edit.Overrides == nil || edit.LockedFields == nil || len(edit.Overrides) != 0 || len(edit.LockedFields) != 0 {
		t.Fatal("empty override/lock replacement did not retain non-null collections")
	}
}

func TestDecodeAdminMetadataEditRejectsMalformedTopLevelAndNestedDuplicates(t *testing.T) {
	for _, test := range []struct{ body, field string }{
		{"", "Body"}, {"null", "Body"}, {"[]", "Body"}, {"{}", "Revision"},
		{`{"Revision":"1","Overrides":{},"LockedFields":[],"Path":"secret-value"}`, "Body"},
		{`{"revision":"1","Overrides":{},"LockedFields":[]}`, "Body"},
		{`{"Revision":"1","Overrides":null,"LockedFields":[]}`, "Overrides"},
		{`{"Revision":"1","Overrides":[],"LockedFields":[]}`, "Overrides"},
		{`{"Revision":"1","Overrides":{},"LockedFields":null}`, "LockedFields"},
		{`{"Revision":"1","Overrides":{},"LockedFields":[null]}`, "LockedFields"},
		{`{"Revision":"1","Overrides":{},"LockedFields":[1]}`, "LockedFields"},
		{`{"Revision":"1","Revision":"2","Overrides":{},"LockedFields":[]}`, "Body"},
		{`{"Revision":"1","Overrides":{"Name":"one","\u004eame":"two"},"LockedFields":[]}`, "Body"},
		{`{"Revision":"1","Overrides":{"ProviderIds":{"Imdb":"one","Imdb":"two"}},"LockedFields":[]}`, "Body"},
		{`{"Revision":"1","Overrides":{"People":[{"Name":"one","Name":"two"}]},"LockedFields":[]}`, "Body"},
		{adminMetadataEditJSON + `{}`, "Body"},
		{strings.Replace(adminMetadataEditJSON, "Manual overview", "invalid\xfftext", 1), "Body"},
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", test.body, "application/json")); ok {
			t.Fatal("malformed or ambiguous metadata edit was accepted")
		}
		assertAdminMetadataInputError(t, response, http.StatusBadRequest, test.field)
		if strings.Contains(response.Body.String(), "secret-value") {
			t.Fatal("metadata validation reflected a rejected protected value")
		}
	}
	for _, revision := range []string{`0`, `null`, `true`, `"0"`, `"01"`, `"+1"`, `"1.0"`, `"1e3"`, `"-1"`, `"9223372036854775808"`} {
		body := `{"Revision":` + revision + `,"Overrides":{},"LockedFields":[]}`
		response := httptest.NewRecorder()
		if _, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", body, "application/json")); ok {
			t.Fatal("noncanonical metadata revision was accepted")
		}
		assertAdminMetadataInputError(t, response, http.StatusBadRequest, "Revision")
	}
}

func TestAdminMetadataJSONAndBodyResourceBounds(t *testing.T) {
	for _, contentType := range []string{"", "text/plain", "application/json; broken"} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", adminMetadataEditJSON, contentType)); ok {
			t.Fatal("metadata edit accepted an unsupported content type")
		}
		assertAdminMetadataInputError(t, response, http.StatusUnsupportedMediaType, "")
	}
	for _, size := range []int{1 << 20, 1<<20 + 1} {
		body := adminMetadataEditJSON + strings.Repeat(" ", size-len(adminMetadataEditJSON))
		response := httptest.NewRecorder()
		_, ok := decodeAdminMetadataEdit(response, adminMetadataRequestForTest(http.MethodPut, "", body, "application/json"))
		if ok != (size == 1<<20) {
			t.Fatalf("metadata body size boundary changed at %d bytes", size)
		}
	}
	for _, test := range []struct {
		body string
		ok   bool
	}{
		{`{"Name":"\ud83d\ude00"}`, true},
		{`{"a":{"Name":1},"b":{"Name":2}}`, true},
		{strings.Repeat("[", 16) + "0" + strings.Repeat("]", 16), true},
		{strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17), false},
		{"[" + strings.Repeat("0,", 65534) + "0]", true},
		{"[" + strings.Repeat("0,", 65535) + "0]", false},
		{`{"a":1,"\u0061":2}`, false},
		{`{"a":1} true`, false},
		{`{"a":`, false},
	} {
		if got := metadataUniqueJSON([]byte(test.body)); got != test.ok {
			t.Fatal("metadata JSON uniqueness/depth/node boundary changed")
		}
	}
}

func TestNativeItemMetadataPreservesCompleteAndSparseValueShapes(t *testing.T) {
	var detail library.ItemMetadataDetail
	const input = `{"ItemId":"item-id","LibraryId":"library-id","ParentId":"parent-id","ParentName":"Parent","Name":"Manual title","Type":"Movie","Path":"/media/movie.mp4","IsFolder":false,"Revision":"9007199254740993","Automatic":{"Name":"Automatic title","SortName":"automatic","ProviderIds":{},"Genres":[],"Tags":[],"Studios":[],"People":[]},"Effective":{"Name":"Manual title","SortName":"automatic","ProductionYear":null,"CommunityRating":0,"ProviderIds":{},"Genres":[],"Tags":[],"Studios":[],"People":[]},"Overrides":{"Name":"Manual title","ProductionYear":null},"LockedValues":{"Overview":"Locked overview"},"LockedFields":["Overview"],"EditableFields":["Name","Overview","ProductionYear"],"LastEditedBy":"actor-id","LastEditedAt":"2026-09-09T01:02:03Z"}`
	if err := json.Unmarshal([]byte(input), &detail); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(nativeItemMetadata(detail))
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if len(output) != 11 || output["Revision"] != "9007199254740993" || output["LastEditedBy"] != "actor-id" || output["LastEditedAt"] != "2026-09-09T01:02:03Z" {
		t.Fatal("native metadata envelope or revision types changed")
	}
	if inactive, ok := output["InactiveFields"].([]any); !ok || len(inactive) != 0 {
		t.Fatal("absent inactive fields must serialize as a non-null empty array")
	}
	item := output["Item"].(map[string]any)
	if len(item) != 8 || item["Id"] != "item-id" || item["ParentName"] != "Parent" || item["Path"] != "/media/movie.mp4" {
		t.Fatal("native item context was expanded or omitted")
	}
	for _, layer := range []string{"Automatic", "Effective"} {
		values := output[layer].(map[string]any)
		if len(values) != 15 || values["PremiereDate"] != nil || values["IndexNumber"] != nil || values["ParentIndexNumber"] != nil {
			t.Fatal("complete metadata values omitted an absent scalar or added internal fields")
		}
		if _, ok := values["ProviderIds"].(map[string]any); !ok {
			t.Fatal("provider identifiers must remain a non-null object with public casing")
		}
		for _, field := range []string{"Genres", "Tags", "Studios", "People"} {
			if entries, ok := values[field].([]any); !ok || len(entries) != 0 {
				t.Fatal("empty complete metadata collections must remain arrays")
			}
		}
	}
	if output["Effective"].(map[string]any)["CommunityRating"] != float64(0) || output["Overrides"].(map[string]any)["ProductionYear"] != nil ||
		len(output["Overrides"].(map[string]any)) != 2 || len(output["LockedValues"].(map[string]any)) != 1 {
		t.Fatal("explicit zero/null or sparse metadata layers were lost")
	}
	for _, field := range []string{"UserData", "Media", "Kind", "ProviderIDs", "SourcePath", "LocalMetadata"} {
		if _, exists := output[field]; exists {
			t.Fatal("native metadata exposed an unrelated internal field")
		}
	}
}

func TestNativeItemMetadataUsesUTCWithoutMutatingAuditInput(t *testing.T) {
	zone := time.FixedZone("metadata-source-zone", 5*60*60+30*60)
	stamp := time.Date(2026, time.September, 9, 6, 32, 3, 123456789, zone)
	originalStamp := stamp
	detail := library.ItemMetadataDetail{LastEditedAt: &stamp, InactiveFields: []string{"IndexNumber"}}
	originalDetail, originalPointer := detail, detail.LastEditedAt
	encoded, err := json.Marshal(nativeItemMetadata(detail))
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output["LastEditedAt"] != "2026-09-09T01:02:03.123456789Z" {
		t.Fatal("metadata audit timestamp did not use the UTC wire contract")
	}
	if inactive, ok := output["InactiveFields"].([]any); !ok || len(inactive) != 1 || inactive[0] != "IndexNumber" {
		t.Fatal("saved inactive fields were lost during serialization")
	}
	if detail.LastEditedAt != originalPointer || *detail.LastEditedAt != originalStamp || detail.LastEditedAt.Location() != zone || !reflect.DeepEqual(detail, originalDetail) {
		t.Fatal("serializing UTC metadata changed the caller's detail or timestamp")
	}
}

func TestNativeMetadataItemsPreservesPagingAndLightweightProjection(t *testing.T) {
	var result library.MetadataItemResult
	if err := json.Unmarshal([]byte(`{"Library":{"Id":"library-id","Name":"Movies","CollectionType":"movies","Paths":["/private/root"]},"Items":[{"ItemId":"item-id","LibraryId":"library-id","ParentId":"parent-id","ParentName":"Parent","Name":"Title","Type":"Movie","Path":"/media/movie.mp4","IsFolder":false,"IndexNumber":0,"ParentIndexNumber":0,"ProductionYear":2020,"HasOverrides":true,"LockedFieldCount":2}],"TotalRecordCount":7}`), &result); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(nativeMetadataItems(result, library.MetadataItemQuery{StartIndex: 3, Limit: 1}))
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if len(output) != 5 || output["TotalRecordCount"] != float64(7) || output["StartIndex"] != float64(3) || output["Limit"] != float64(1) {
		t.Fatal("native metadata paging envelope changed")
	}
	if len(output["Library"].(map[string]any)) != 3 || strings.Contains(string(encoded), "/private/root") {
		t.Fatal("library context exposed configuration paths")
	}
	items := output["Items"].([]any)
	item := items[0].(map[string]any)
	if len(item) != 13 || item["Id"] != "item-id" || item["ProductionYear"] != float64(2020) || item["HasOverrides"] != true || item["LockedFieldCount"] != float64(2) {
		t.Fatal("metadata list summary changed its lightweight fields")
	}
	empty, err := json.Marshal(nativeMetadataItems(library.MetadataItemResult{}, library.MetadataItemQuery{Limit: 50}))
	if err != nil || !strings.Contains(string(empty), `"Items":[]`) {
		t.Fatal("empty native item listing must contain a non-null array")
	}
}
