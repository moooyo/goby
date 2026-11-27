package library

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/metadata"
)

func TestSeriesNFOAuditWarnings(t *testing.T) {
	parse := func(item, directory, leaf, document string) seriesNFOAuditClaim {
		t.Helper()
		claim, ok := parseSeriesNFOAuditClaim([]byte(document), item, directory, item+"-file", leaf)
		if !ok {
			t.Fatal("valid series fixture did not parse")
		}
		return claim
	}
	const wrong = `<tvshow><title>Glass Harbor</title><originaltitle>玻璃港</originaltitle><year>2019</year><uniqueid type="TMDB-TV">00042</uniqueid></tvshow>`
	const peer = `<tvshow><title>玻璃港</title><year>2019</year><uniqueid type="tmdb">42</uniqueid></tvshow>`
	a := parse("a", "a-dir", "Frost Lanterns (2004)", wrong)
	b := parse("b", "b-dir", "玻璃港 (2019)", peer)
	c := parse("c", "c-dir", "玻璃港 (2019)", peer)
	warning := SeriesNFOAuditWarning{ItemID: "a", Code: "title_year_disagreement"}
	strong := SeriesNFOAuditWarning{ItemID: "a", Code: "shared_declaration_disagreement", CorroboratingItemID: "b"}
	if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{a}); !slices.Equal(got, []SeriesNFOAuditWarning{warning}) {
		t.Fatalf("without a peer: %+v", got)
	}
	for _, order := range [][]seriesNFOAuditClaim{{a, b, c}, {b, c, a}, {c, a, b}, {c, b, a}} {
		if got := seriesNFOAuditWarnings(order); !slices.Equal(got, []SeriesNFOAuditWarning{strong}) {
			t.Fatalf("peer selection or warning direction changed with scan order: %+v", got)
		}
	}
	if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{b, c}); len(got) != 0 || got == nil {
		t.Fatalf("legitimate copies are not failures: %+v", got)
	}
	conflicting := strings.Replace(wrong, `<uniqueid type="TMDB-TV">`, `<tmdbid>41</tmdbid><uniqueid type="TMDB-TV">`, 1)
	contradictory := parse("a", "a-dir", "Frost Lanterns (2004)", conflicting)
	if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{contradictory, b}); !slices.Equal(got, []SeriesNFOAuditWarning{warning}) {
		t.Fatalf("a superseded, conflicting declaration was mistaken for corroboration: %+v", got)
	}
	shadowed := parse("a", "a-dir", "Frost Lanterns (2004)", strings.Replace(wrong, "</tvshow>",
		`<tmdbid>local-only</tmdbid><imdbid>tt1234567</imdbid></tvshow>`, 1))
	conflictingPeer := parse("b", "b-dir", "玻璃港 (2019)", strings.Replace(peer, "42</uniqueid></tvshow>",
		`41</uniqueid><imdbid>tt1234567</imdbid></tvshow>`, 1))
	if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{shadowed, conflictingPeer}); !slices.Equal(got, []SeriesNFOAuditWarning{warning}) {
		t.Fatalf("an opaque late declaration hid a comparable conflict: %+v", got)
	}
	ordinary, err := metadata.ParseNFO(strings.NewReader(conflicting))
	if err != nil || ordinary.ProviderIDs["Tmdb"] != "00042" {
		t.Fatalf("ordinary last-value compatibility changed: %v, %v", ordinary.ProviderIDs, err)
	}
	for _, fixture := range []struct {
		name   string
		change func(*seriesNFOAuditClaim, *seriesNFOAuditClaim)
	}{
		{"same directory", func(a, b *seriesNFOAuditClaim) { b.directoryID = a.directoryID }},
		{"one hardlinked file", func(a, b *seriesNFOAuditClaim) { b.fileID = a.fileID }},
		{"different namespaces", func(_, b *seriesNFOAuditClaim) { b.ids = [3]string{"", "", "42"} }},
		{"other shared provider disagrees", func(a, b *seriesNFOAuditClaim) { a.ids[0], b.ids[0] = "tt1234567", "tt1234568" }},
		{"peer is not self-consistent", func(_, b *seriesNFOAuditClaim) { b.directoryYear = 2018 }},
		{"peer has no shared title", func(_, b *seriesNFOAuditClaim) { b.titles[0], b.directoryTitle = "Harbor Glass", "Harbor Glass" }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			left, right := a, b
			fixture.change(&left, &right)
			if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{right, left}); !slices.Equal(got, []SeriesNFOAuditWarning{warning}) {
				t.Fatalf("unusable peer strengthened warning: %+v", got)
			}
		})
	}
}

func TestSeriesNFOAuditSilenceDoesNotAssertIdentity(t *testing.T) {
	const leaf = "Blue Hill (2004)"
	for _, fixture := range []struct {
		name, leaf, title, original string
		year, premiere              int
	}{
		{"year only", leaf, "Blue Hill", "", 2019, 0},
		{"title only", leaf, "Glass Harbor", "", 2004, 0},
		{"regional one-year difference", leaf, "Glass Harbor", "", 2005, 0},
		{"original title is an alias", leaf, "Glass Harbor", "BLUE HILL", 2019, 0},
		{"Unicode and spacing", "Café de Paris (2004)", "CAFÉ de Paris", "", 2019, 0},
		{"original title with XML whitespace", leaf, "Cerro Azul", "Blue\tHill", 2019, 0},
		{"no titles", leaf, "", "", 2019, 0},
		{"no year", leaf, "Glass Harbor", "", 0, 0},
		{"only premiere", leaf, "Glass Harbor", "", 0, 2019},
		{"different declared premiere year", leaf, "Glass Harbor", "", 2019, 2018},
		{"generic directory", "TV Shows (2004)", "Glass Harbor", "", 2019, 0},
		{"no directory year", "Blue Hill", "Glass Harbor", "", 2019, 0},
		{"ambiguous directory year", "Blue Hill (2004) (2003)", "Glass Harbor", "", 2019, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value := metadata.Metadata{Name: fixture.title, OriginalTitle: fixture.original}
			if fixture.year != 0 {
				value.ProductionYear = &fixture.year
			}
			if fixture.premiere != 0 {
				date := time.Date(fixture.premiere, 12, 31, 0, 0, 0, 0, time.UTC)
				value.PremiereDate = &date
			}
			claim := newSeriesNFOAuditClaim("show", "directory", "file", fixture.leaf, &value)
			if got := seriesNFOAuditWarnings([]seriesNFOAuditClaim{claim}); len(got) != 0 {
				t.Fatalf("inconclusive evidence produced warnings: %+v", got)
			}
		})
	}
}

func TestSeriesNFOAuditProviderIDsDoNotPromoteOpaqueValues(t *testing.T) {
	value := &metadata.Metadata{ProviderIDs: map[string]string{
		"Imdb": "tt0000000", "Tmdb": "https://example.invalid/42", "Tvdb": "٤٢", "official website": "42",
	}}
	if got := newSeriesNFOAuditClaim("series", "directory", "file", "Series", value).ids; got != [3]string{} {
		t.Fatalf("opaque IDs entered the corroboration key: %v", got)
	}
	for raw, want := range map[string]string{"00042": "42", "9223372036854775807": "9223372036854775807", "9223372036854775808": "", "0": "", "+42": "", "42:3": "", "42.0": ""} {
		if got := seriesNFOAuditNumericID(raw); got != want {
			t.Errorf("numeric ID %q: got %q, want %q", raw, got, want)
		}
	}
}
