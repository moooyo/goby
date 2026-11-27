package metadata

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func episodePairTestRoot(number int, body string) string {
	return fmt.Sprintf("<episodedetails><season>1</season><episode>%d</episode>%s</episodedetails>", number, body)
}

func TestParseEpisodeNFOPairMergesOnlyBoundDescription(t *testing.T) {
	pair := EpisodePair{Season: 1, First: 3, Last: 4}
	first := episodePairTestRoot(3, `<title>First</title><plot>Plot A</plot><sorttitle>Explicit sort</sorttitle><tmdbid>100</tmdbid><year>2020</year><mpaa>TV-PG</mpaa><tag>A</tag><tag>B</tag>`)
	second := episodePairTestRoot(4, `<title>Second</title><plot>Plot B</plot><sorttitle>Ignored sort</sorttitle><tmdbid>200</tmdbid><imdbid>tt22</imdbid><year>2021</year><mpaa>TV-PG</mpaa><tag>B</tag><tag>A</tag><actor><name>Other</name></actor>`)
	input := first + "\n<!-- pair separator -->\n" + second
	want := parseNFOString(t, first)
	want.Name, want.Overview = "First,  Second", "Plot A\n\nPlot B"
	for name, encoded := range map[string]string{
		"utf8":  "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?>` + input,
		"utf16": encodeNFOTestUTF16(binary.BigEndian, `<?xml version="1.0" encoding="UTF-16"?>`+input),
	} {
		t.Run(name, func(t *testing.T) {
			got, merged, err := ParseEpisodeNFO(strings.NewReader(encoded), pair)
			if err != nil || !merged {
				t.Fatalf("episode merge = %v, error = %v", merged, err)
			}
			assertNFOMetadata(t, got, want)
			assertNFOError(t, encoded) // The generic parser remains single-root.
		})
	}
	got, merged, err := ParseEpisodeNFO(strings.NewReader(second), pair)
	if err != nil || merged {
		t.Fatalf("single-root behavior changed: merged = %v, error = %v", merged, err)
	}
	assertNFOMetadata(t, got, parseNFOString(t, second))
	got, merged, err = ParseEpisodeNFO(strings.NewReader(episodePairTestRoot(3, `<plot>A</plot>`)+episodePairTestRoot(4, `<title>B</title>`)), pair)
	if err != nil || !merged || got.Name != "B" || got.Overview != "A" {
		t.Fatalf("added a separator for missing text: name=%q plot=%q merged=%v error=%v", got.Name, got.Overview, merged, err)
	}
}

func TestParseEpisodeNFOPairRejectsWholeInput(t *testing.T) {
	pair := EpisodePair{Season: 1, First: 3, Last: 4}
	first, second := episodePairTestRoot(3, `<title>First</title>`), episodePairTestRoot(4, `<title>Second</title>`)
	names := strings.Repeat("界", 200)
	plots := strings.Repeat("a", maxFieldBytes/2)
	var collections strings.Builder
	for i := 0; i <= maxEntries/2; i++ {
		fmt.Fprintf(&collections, "<genre>Genre %d</genre>", i)
	}
	manyElements := strings.Repeat("<unused/>", maxElements/2)
	cases := map[string]string{
		"reverse": second + first, "wrong last": first + episodePairTestRoot(8, ""),
		"wrong season":      first + strings.Replace(second, "<season>1", "<season>2", 1),
		"duplicate episode": first + episodePairTestRoot(4, `<episode>4</episode>`),
		"missing episode":   first + `<episodedetails><season>1</season></episodedetails>`,
		"third root":        first + second + second, "broken second": first + strings.TrimSuffix(second, "</episodedetails>"),
		"wrong root": first + `<tvshow/>`, "wrapper": `<episodes>` + first + second + `</episodes>`,
		"namespace":              first + strings.Replace(second, "<episodedetails>", `<episodedetails xmlns="urn:other">`, 1),
		"empty namespace prefix": first + strings.NewReplacer(`<episodedetails>`, `<p:episodedetails xmlns:p="">`, `</episodedetails>`, `</p:episodedetails>`).Replace(second),
		"CDATA separator":        first + `<![CDATA[ ]]>` + second,
		"entity epilogue":        first + second + `&#32;`,
		"between declaration":    first + `<?xml version="1.0"?>` + second,
		"between BOM":            first + "\xef\xbb\xbf" + second, "NUL tail": first + second + "\x00", "nonwhite tail": first + second + "tail",
		"DTD":                         `<!DOCTYPE episodedetails>` + first + second,
		"non-XML PI":                  first + `<?other anything?>` + second,
		"invalid second scalar":       first + episodePairTestRoot(4, `<year>invalid</year>`),
		"different rating":            episodePairTestRoot(3, `<mpaa>PG</mpaa>`) + episodePairTestRoot(4, `<mpaa>R</mpaa>`),
		"missing rating":              episodePairTestRoot(3, `<mpaa>PG</mpaa>`) + second,
		"second only tags":            first + episodePairTestRoot(4, `<tag>Restricted</tag>`),
		"different tags":              episodePairTestRoot(3, `<tag>A</tag>`) + episodePairTestRoot(4, `<tag>B</tag>`),
		"combined title":              episodePairTestRoot(3, `<title>`+names+`</title>`) + episodePairTestRoot(4, `<title>`+names+`</title>`),
		"combined plot":               episodePairTestRoot(3, `<plot>`+plots+`</plot>`) + episodePairTestRoot(4, `<plot>`+plots+`</plot>`),
		"collective element limit":    episodePairTestRoot(3, manyElements) + episodePairTestRoot(4, manyElements),
		"collective collection limit": episodePairTestRoot(3, collections.String()) + episodePairTestRoot(4, collections.String()),
		"raw size limit":              first + second + strings.Repeat(" ", maxNFOBytes),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got, merged, err := ParseEpisodeNFO(strings.NewReader(input), pair)
			if err == nil || merged || !reflect.DeepEqual(got, Metadata{}) {
				t.Fatalf("accepted or returned partial facts from invalid input: merged=%v error=%v", merged, err)
			}
		})
	}
}
