package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestEntityFiltersPreserveDocumentedNameSeparators(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/emby/Items?Genres=Science+Fiction%7CDrama&Tags=local,comma%7Creference&Studios=North,+Studio%7CSouth+Studio&Person=Alex+Example&PersonTypes=Actor,Director&Limit=0", nil)
	response := httptest.NewRecorder()
	query, ok := readItemQuery(response, request, "viewer-id")
	if !ok {
		t.Fatalf("valid entity filters rejected: %s", response.Body.String())
	}
	for _, field := range []struct {
		name string
		actual, expected []string
	}{
		{name: "Genres", actual: query.Genres, expected: []string{"Science Fiction", "Drama"}},
		{name: "Tags", actual: query.Tags, expected: []string{"local,comma", "reference"}},
		{name: "Studios", actual: query.Studios, expected: []string{"North, Studio", "South Studio"}},
		{name: "PersonTypes", actual: query.PersonTypes, expected: []string{"Actor", "Director"}},
	} {
		if !reflect.DeepEqual(field.actual, field.expected) {
			t.Errorf("%s = %#v, want %#v", field.name, field.actual, field.expected)
		}
	}
	if query.Person != "Alex Example" || query.Limit != 0 || query.UserID != "viewer-id" {
		t.Errorf("entity filters changed person, pagination, or authorized user: %+v", query)
	}
}

func TestEntityIdentifierFiltersAcceptBothDelimitersAndRejectInvalidValues(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/emby/Items?GenreIds=1%7C2,3&TagIds=4,5&StudioIds=6%7C7&PersonIds=08%7C9,10", nil)
	query, ok := readItemQuery(httptest.NewRecorder(), request, "viewer-id")
	if !ok || !reflect.DeepEqual(query.GenreIds, []int64{1, 2, 3}) || !reflect.DeepEqual(query.TagIds, []int64{4, 5}) ||
		!reflect.DeepEqual(query.StudioIds, []int64{6, 7}) || !reflect.DeepEqual(query.PersonIds, []string{"8", "9", "10"}) {
		t.Fatalf("valid entity identifiers parsed incorrectly: %+v, valid = %v", query, ok)
	}
	for _, field := range []string{"GenreIds", "TagIds", "StudioIds", "PersonIds"} {
		for _, invalid := range []string{"0", "-1", "%2B1", "not-a-number", "1.2", "9223372036854775808", "1,,2", "1%7C"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, "/emby/Items?"+field+"="+invalid, nil)
				response := httptest.NewRecorder()
				if _, ok := readItemQuery(response, request, "viewer-id"); ok {
					t.Fatalf("invalid %s identifier was accepted: %s", field, invalid)
				}
				expectAPIError(t, response, http.StatusBadRequest, "invalid_input", true)
			})
		}
	}
}
