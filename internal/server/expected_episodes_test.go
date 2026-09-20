package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

const episodeRosterRequestJSON = `{"Revision":"0","Source":{"Key":"local-manifest","Label":"Declared episodes","Revision":"v1"},"Entries":[{"Key":"episode-2","SeasonNumber":1,"EpisodeNumber":2,"Name":"Second episode","PremiereDate":"2020-01-02"}]}`

func episodeRosterParserRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/series/series/episode-roster", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "roster-request"))
}

func TestEpisodeRosterInputSupportsExplicitFactsAndExactCAS(t *testing.T) {
	for _, raw := range []string{episodeRosterRequestJSON,
		strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":"9007199254740993"`, 1),
		strings.Replace(episodeRosterRequestJSON, `,"PremiereDate":"2020-01-02"`, "", 1),
		strings.Replace(episodeRosterRequestJSON, `"SeasonNumber":1,"EpisodeNumber":2`, `"SeasonNumber":0,"EpisodeNumber":0`, 1),
		`{"Revision":"0","Source":{"Key":"local","Label":"","Revision":"v1"},"Entries":[]}`,
	} {
		response := httptest.NewRecorder()
		edit, ok := decodeEpisodeRosterEdit(response, episodeRosterParserRequest(raw), false)
		if !ok || edit.Entries == nil || edit.Source.Key == "" {
			t.Fatalf("valid roster input rejected: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	edit, ok := decodeEpisodeRosterEdit(response, episodeRosterParserRequest(`{"Revision":"9007199254740993"}`), true)
	if !ok || edit.Revision != "9007199254740993" {
		t.Fatal("withdrawal lost exact revision")
	}
}

func TestEpisodeRosterInputRejectsAmbiguityNullsLossyUnicodeAndBounds(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"root_null", `null`},
		{"root_duplicate", strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":"0","Revision":"0"`, 1)},
		{"unknown_field", strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":"0","ProviderUrl":"https://example.invalid"`, 1)},
		{"wrong_case", strings.Replace(episodeRosterRequestJSON, `"Source"`, `"source"`, 1)},
		{"numeric_revision", strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":0`, 1)},
		{"revision_null", strings.Replace(episodeRosterRequestJSON, `"Revision":"0"`, `"Revision":null`, 1)},
		{"source_null", `{"Revision":"0","Source":null,"Entries":[]}`},
		{"source_duplicate", strings.Replace(episodeRosterRequestJSON, `"Label":"Declared episodes"`, `"Label":"Declared episodes","Label":"Declared episodes"`, 1)},
		{"source_unknown", strings.Replace(episodeRosterRequestJSON, `"Key":"local-manifest"`, `"Key":"local-manifest","Kind":"remote"`, 1)},
		{"entry_duplicate", strings.Replace(episodeRosterRequestJSON, `"Name":"Second episode"`, `"Name":"Second episode","Name":"Second episode"`, 1)},
		{"entry_unknown", strings.Replace(episodeRosterRequestJSON, `"Name":"Second episode"`, `"Name":"Second episode","MediaSources":[]`, 1)},
		{"entries_null", `{"Revision":"0","Source":{"Key":"local","Label":"","Revision":"v1"},"Entries":null}`},
		{"number_null", strings.Replace(episodeRosterRequestJSON, `"SeasonNumber":1`, `"SeasonNumber":null`, 1)},
		{"number_string", strings.Replace(episodeRosterRequestJSON, `"SeasonNumber":1`, `"SeasonNumber":"1"`, 1)},
		{"number_fraction", strings.Replace(episodeRosterRequestJSON, `"SeasonNumber":1`, `"SeasonNumber":1.0`, 1)},
		{"number_negative", strings.Replace(episodeRosterRequestJSON, `"EpisodeNumber":2`, `"EpisodeNumber":-1`, 1)},
		{"date_null", strings.Replace(episodeRosterRequestJSON, `"PremiereDate":"2020-01-02"`, `"PremiereDate":null`, 1)},
		{"date_empty", strings.Replace(episodeRosterRequestJSON, `"PremiereDate":"2020-01-02"`, `"PremiereDate":""`, 1)},
		{"date_invalid", strings.Replace(episodeRosterRequestJSON, `2020-01-02`, `2023-02-29`, 1)},
		{"lone_surrogate", strings.Replace(episodeRosterRequestJSON, `Second episode`, `\ud800`, 1)},
		{"control", strings.Replace(episodeRosterRequestJSON, `Second episode`, `Second\nepisode`, 1)},
		{"whitespace", strings.Replace(episodeRosterRequestJSON, `local-manifest`, ` local-manifest`, 1)},
		{"body_limit", strings.Repeat(" ", library.MaxEpisodeRosterBytes) + episodeRosterRequestJSON},
		{"trailing_object", episodeRosterRequestJSON + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			_, ok := decodeEpisodeRosterEdit(response, episodeRosterParserRequest(test.raw), false)
			if ok || response.Code != http.StatusBadRequest {
				t.Fatalf("invalid request accepted: %d %s", response.Code, response.Body.String())
			}
			var body struct {
				Error struct {
					Code   string
					Fields map[string]string
				}
				RequestID string `json:"RequestId"`
			}
			if json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Error.Code != "invalid_input" || len(body.Error.Fields) == 0 || body.RequestID != "roster-request" {
				t.Fatal("native field error envelope lost")
			}
		})
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.URL.RawQuery = "ignored=true" }, func(r *http.Request) { r.URL.ForceQuery = true },
		func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
	} {
		r := episodeRosterParserRequest(episodeRosterRequestJSON)
		mutate(r)
		response := httptest.NewRecorder()
		if _, ok := decodeEpisodeRosterEdit(response, r, false); ok {
			t.Fatal("query or content-type ambiguity accepted")
		}
	}
}

func TestEpisodeRosterInputRejectsDuplicateEpisodeIdentities(t *testing.T) {
	for _, entries := range []string{
		`[{"Key":"same","SeasonNumber":1,"EpisodeNumber":1,"Name":""},{"Key":"same","SeasonNumber":1,"EpisodeNumber":2,"Name":""}]`,
		`[{"Key":"one","SeasonNumber":1,"EpisodeNumber":1,"Name":""},{"Key":"two","SeasonNumber":1,"EpisodeNumber":1,"Name":""}]`,
	} {
		raw := `{"Revision":"0","Source":{"Key":"local","Label":"","Revision":"v1"},"Entries":` + entries + `}`
		response := httptest.NewRecorder()
		if _, ok := decodeEpisodeRosterEdit(response, episodeRosterParserRequest(raw), false); ok {
			t.Fatal("ambiguous episode identities accepted")
		}
	}
}
