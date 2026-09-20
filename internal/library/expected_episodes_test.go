package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func episodeRosterTestEdit() EpisodeRosterEdit {
	return EpisodeRosterEdit{Revision: "0", Source: EpisodeRosterSourceInput{Key: "declared-local-roster", Label: "Explicit episode list", Revision: "v1"},
		Entries: []EpisodeRosterEntryInput{{Key: "episode-two", SeasonNumber: 1, EpisodeNumber: 2, Name: "Second episode", PremiereDate: "2020-01-02"},
			{Key: "episode-three", SeasonNumber: 1, EpisodeNumber: 3, Name: "Unknown premiere"}}}
}

func TestEpisodeRosterCanonicalPayloadAndStableIdentity(t *testing.T) {
	input := episodeRosterTestEdit()
	input.Entries[0], input.Entries[1] = input.Entries[1], input.Entries[0]
	normalized, payload, digest, err := normalizeEpisodeRosterEdit(input)
	if err != nil {
		t.Fatal(err)
	}
	if input.Entries[0].Key != "episode-three" || normalized.Entries[0].Key != "episode-two" {
		t.Fatal("normalization mutated input or omitted canonical ordering")
	}
	parsed, hash, err := ParseEpisodeRosterPayload(payload)
	if err != nil || hash != digest || parsed.Revision != "0" || len(parsed.Entries) != 2 {
		t.Fatalf("canonical import round trip: %v", err)
	}
	_, secondPayload, secondDigest, err := normalizeEpisodeRosterEdit(episodeRosterTestEdit())
	if err != nil || !bytes.Equal(payload, secondPayload) || digest != secondDigest {
		t.Fatal("source order changed content identity")
	}
	id := ExpectedEpisodeID("series", "source", "entry")
	if !IsExpectedEpisodeID(id) || id != ExpectedEpisodeID("series", "source", "entry") ||
		id == ExpectedEpisodeID("series", "other-source", "entry") || id == ExpectedEpisodeID("other-series", "source", "entry") ||
		ExpectedEpisodeID("ab", "c", "d") == ExpectedEpisodeID("a", "bc", "d") {
		t.Fatal("expected identity is unstable or ambiguous")
	}
	for _, bad := range []string{"", "missing-", strings.ToUpper(id), "physical-" + id[8:], id + "0"} {
		if IsExpectedEpisodeID(bad) {
			t.Errorf("accepted malformed identity %q", bad)
		}
	}
}

func TestEpisodeRosterRejectsInvalidOrAmbiguousFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*EpisodeRosterEdit)
	}{
		{"noncanonical_revision", func(e *EpisodeRosterEdit) { e.Revision = "01" }},
		{"overflow_revision", func(e *EpisodeRosterEdit) { e.Revision = "9223372036854775808" }},
		{"negative_revision", func(e *EpisodeRosterEdit) { e.Revision = "-1" }},
		{"empty_source", func(e *EpisodeRosterEdit) { e.Source.Key = "" }},
		{"source_whitespace", func(e *EpisodeRosterEdit) { e.Source.Key = " source" }},
		{"source_control", func(e *EpisodeRosterEdit) { e.Source.Label = "label\n" }},
		{"source_oversize", func(e *EpisodeRosterEdit) { e.Source.Revision = strings.Repeat("x", 129) }},
		{"null_entries", func(e *EpisodeRosterEdit) { e.Entries = nil }},
		{"too_many_entries", func(e *EpisodeRosterEdit) { e.Entries = make([]EpisodeRosterEntryInput, MaxEpisodeRosterEntries+1) }},
		{"duplicate_key", func(e *EpisodeRosterEdit) { e.Entries[1].Key = e.Entries[0].Key }},
		{"duplicate_number", func(e *EpisodeRosterEdit) { e.Entries[1].EpisodeNumber = e.Entries[0].EpisodeNumber }},
		{"negative_season", func(e *EpisodeRosterEdit) { e.Entries[0].SeasonNumber = -1 }},
		{"large_episode", func(e *EpisodeRosterEdit) { e.Entries[0].EpisodeNumber = 10000 }},
		{"name_control", func(e *EpisodeRosterEdit) { e.Entries[0].Name = "title\x00" }},
		{"name_oversize", func(e *EpisodeRosterEdit) { e.Entries[0].Name = strings.Repeat("x", 513) }},
		{"invalid_utf8", func(e *EpisodeRosterEdit) { e.Entries[0].Key = string([]byte{0xff}) }},
		{"unknown_calendar_date", func(e *EpisodeRosterEdit) { e.Entries[0].PremiereDate = "2023-02-29" }},
		{"time_instead_of_date", func(e *EpisodeRosterEdit) { e.Entries[0].PremiereDate = "2020-01-01T00:00:00Z" }},
		{"year_zero", func(e *EpisodeRosterEdit) { e.Entries[0].PremiereDate = "0000-01-01" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			edit := episodeRosterTestEdit()
			test.change(&edit)
			if _, _, _, err := normalizeEpisodeRosterEdit(edit); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid fact accepted: %v", err)
			}
		})
	}
	large := episodeRosterTestEdit()
	large.Entries = make([]EpisodeRosterEntryInput, 1000)
	for index := range large.Entries {
		large.Entries[index] = EpisodeRosterEntryInput{Key: strings.Repeat("k", 120) + string(rune(0x4e00+index)), SeasonNumber: 1, EpisodeNumber: index, Name: strings.Repeat("n", 512)}
	}
	if _, _, _, err := normalizeEpisodeRosterEdit(large); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("canonical payload byte budget was ignored")
	}
	empty := episodeRosterTestEdit()
	empty.Entries = []EpisodeRosterEntryInput{}
	if _, _, _, err := normalizeEpisodeRosterEdit(empty); err != nil {
		t.Fatalf("explicit empty roster rejected: %v", err)
	}
	zero := episodeRosterTestEdit()
	zero.Entries = []EpisodeRosterEntryInput{{Key: "special", SeasonNumber: 0, EpisodeNumber: 0, Name: "", PremiereDate: "0001-01-01"}}
	if _, _, _, err := normalizeEpisodeRosterEdit(zero); err != nil {
		t.Fatalf("explicit special or early valid date rejected: %v", err)
	}
}

func TestEpisodeRosterArchiveParserRejectsNoncanonicalOrTamperedPayload(t *testing.T) {
	_, raw, _, err := normalizeEpisodeRosterEdit(episodeRosterTestEdit())
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["ParserVersion"] = 2
	wrongVersion, _ := json.Marshal(object)
	for _, bad := range [][]byte{
		nil, append([]byte(" "), raw...), append(append([]byte{}, raw...), []byte(" {}")...),
		bytes.Replace(raw, []byte(`"ParserVersion":1`), []byte(`"ParserVersion":1,"ParserVersion":1`), 1),
		bytes.Replace(raw, []byte(`"ParserVersion":1`), []byte(`"ParserVersion":1,"Extra":false`), 1),
		bytes.Replace(raw, []byte(`"Name":"Second episode"`), []byte(`"Name":"\ud800"`), 1),
		wrongVersion, bytes.Repeat([]byte("x"), MaxEpisodeRosterBytes+1),
	} {
		if _, _, err := ParseEpisodeRosterPayload(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("archive accepted noncanonical evidence")
		}
	}
}
