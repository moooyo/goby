package metadata

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseNFOFixtures(t *testing.T) {
	cases := []struct {
		name string
		want Metadata
	}{
		{
			name: "movie",
			want: Metadata{
				Kind: "movie", Name: "The Example", SortName: "Example, The",
				OriginalTitle: "The Original Example", Overview: "A careful story about music & discovery.",
				OfficialRating: "PG-13", ProductionYear: nfoInt(2024),
				PremiereDate: nfoDate(2024, time.February, 29), CommunityRating: nfoFloat(8.25),
				ProviderIDs: map[string]string{"Imdb": "tt1234567", "Tmdb": "12345", "MusicBrainz": "release-123"},
				Genres:      []string{"Drama", "Music"}, Tags: []string{"Featured", "Library favorite"},
				Studios: []string{"Example Pictures"},
				People: []Person{
					{Name: "Ada Example", Role: "Lead", Type: "Actor", SortOrder: nfoInt(0)},
					{Name: "Sam Director", Type: "Director"},
					{Name: "Pat Writer", Type: "Writer"},
				},
			},
		},
		{
			name: "episode",
			want: Metadata{
				Kind: "episodedetails", Name: "A New Beginning", SortName: "New Beginning, A",
				Overview: "The first chapter.", ProductionYear: nfoInt(2025),
				IndexNumber: nfoInt(3), ParentIndexNumber: nfoInt(2),
				PremiereDate: nfoDate(2025, time.January, 2), CommunityRating: nfoFloat(7.5),
				ProviderIDs: map[string]string{"Tvdb": "76543"}, Genres: []string{"Science Fiction"},
				People: []Person{
					{Name: "Alex Example", Role: "Explorer", Type: "Actor", SortOrder: nfoInt(2)},
					{Name: "Taylor Director", Type: "Director"},
					{Name: "Jordan Writer", Type: "Writer"},
				},
			},
		},
		{
			name: "album",
			want: Metadata{
				Kind: "album", Name: "Night Signals", SortName: "Signals, Night",
				Overview: "A small instrumental collection.", ProductionYear: nfoInt(2023),
				ProviderIDs: map[string]string{"MusicBrainz": "album-123"},
				Genres:      []string{"Jazz", "Ambient/Electronic"}, Studios: []string{"Example Records"},
			},
		},
		{
			name: "artist",
			want: Metadata{
				Kind: "artist", Name: "The Example Quartet",
				Overview:    "Four musicians exploring quiet melodies.",
				ProviderIDs: map[string]string{"MusicBrainz": "artist-456"}, Genres: []string{"Jazz"},
			},
		},
		{name: "missing", want: Metadata{Kind: "tvshow", Name: "Minimal Show"}},
		{
			name: "utf8",
			want: Metadata{
				Kind: "movie", Name: "Café Cinema — 🐈", OriginalTitle: "A Curious Cat's Story",
				Overview: "Café, cinema, and a curious cat 🐈.", Genres: []string{"Comedy"},
				People: []Person{{Name: "Zoë Example", Role: "Narrator", Type: "Actor"}},
			},
		},
		{name: "empty", want: Metadata{Kind: "movie"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("testdata", tc.name+".nfo"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			got, err := ParseNFO(file)
			if err != nil {
				t.Fatalf("ParseNFO() error = %v", err)
			}
			assertNFOMetadata(t, got, tc.want)
		})
	}
}

func TestParseNFOIndexMapping(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Metadata
	}{
		{"movie", "<movie><season>1</season><episode>2</episode></movie>", Metadata{Kind: "movie"}},
		{"season", "<season><title>Specials</title><season>0</season></season>", Metadata{Kind: "season", Name: "Specials", IndexNumber: nfoInt(0)}},
		{"episode", "<episodedetails><season>0</season><episode>0</episode></episodedetails>", Metadata{Kind: "episodedetails", IndexNumber: nfoInt(0), ParentIndexNumber: nfoInt(0)}},
		{"maximum episode", "<episodedetails><season>2147483647</season><episode>2147483647</episode></episodedetails>", Metadata{Kind: "episodedetails", IndexNumber: nfoInt(2147483647), ParentIndexNumber: nfoInt(2147483647)}},
		{"empty episode", "<episodedetails><season> </season><episode/></episodedetails>", Metadata{Kind: "episodedetails"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNFOMetadata(t, parseNFOString(t, tc.input), tc.want)
		})
	}
}

func TestParseNFOScalarDuplicates(t *testing.T) {
	t.Run("matching values", func(t *testing.T) {
		got := parseNFOString(t, "<movie><title> Example </title><title>Example</title><year>2024</year><year>2024</year><plot/><plot>Story</plot></movie>")
		assertNFOMetadata(t, got, Metadata{Kind: "movie", Name: "Example", ProductionYear: nfoInt(2024), Overview: "Story"})
	})
	for _, field := range []struct{ tag, first, second string }{
		{"title", "First", "Second"},
		{"sorttitle", "First", "Second"},
		{"originaltitle", "First", "Second"},
		{"plot", "First", "Second"},
		{"mpaa", "PG", "R"},
		{"year", "2023", "2024"},
		{"rating", "7", "8"},
		{"premiered", "2024-01-01", "2024-01-02"},
	} {
		t.Run(field.tag, func(t *testing.T) {
			input := fmt.Sprintf("<movie><%s>%s</%s><%s>%s</%s></movie>", field.tag, field.first, field.tag, field.tag, field.second, field.tag)
			assertNFOError(t, input)
		})
	}
}

func TestParseNFOTitleTakesPriorityOverMusicName(t *testing.T) {
	for _, body := range []string{
		"<name>Fallback</name><title>Preferred</title>",
		"<title>Preferred</title><name>Fallback</name>",
	} {
		assertNFOMetadata(t, parseNFOString(t, "<album>"+body+"</album>"), Metadata{Kind: "album", Name: "Preferred"})
	}
	assertNFOMetadata(t, parseNFOString(t, "<artist><title> </title><name>Fallback</name></artist>"), Metadata{Kind: "artist", Name: "Fallback"})
}

func TestParseNFOCatalogNamesHaveBoundedUTF8Bytes(t *testing.T) {
	for _, field := range []struct{ root, tag string }{
		{"movie", "title"}, {"tvshow", "title"}, {"episodedetails", "title"},
		{"season", "title"}, {"album", "title"}, {"artist", "title"},
		{"movie", "sorttitle"}, {"album", "name"}, {"artist", "name"},
	} {
		t.Run(field.root+"/"+field.tag, func(t *testing.T) {
			for _, boundary := range []struct {
				name, encoded, decoded string
				accepted               bool
			}{
				{"ASCII at limit", strings.Repeat("a", 1024), strings.Repeat("a", 1024), true},
				{"ASCII over limit", strings.Repeat("a", 1025), "", false},
				{"two-byte at limit", strings.Repeat("\u00e9", 512), strings.Repeat("\u00e9", 512), true},
				{"two-byte over limit", strings.Repeat("\u00e9", 512) + "a", "", false},
				{"four-byte at limit", strings.Repeat("\U0001f431", 256), strings.Repeat("\U0001f431", 256), true},
				{"four-byte over limit", strings.Repeat("\U0001f431", 256) + "a", "", false},
				{"decoded entities at limit", strings.Repeat("&#233;", 512), strings.Repeat("\u00e9", 512), true},
				{"decoded entities over limit", strings.Repeat("&#233;", 512) + "a", "", false},
				{"trimmed at limit", " " + strings.Repeat("a", 1024) + " ", strings.Repeat("a", 1024), true},
			} {
				t.Run(boundary.name, func(t *testing.T) {
					body := "<" + field.tag + ">" + boundary.encoded + "</" + field.tag + ">"
					if !boundary.accepted {
						// Other extracted fields must not escape when a catalog name
						// fails its storage limit; assertNFOError checks zero output.
						body = `<genre>Drama</genre><uniqueid type="imdb">tt123</uniqueid>` + body
						assertNFOError(t, "<"+field.root+">"+body+"</"+field.root+">")
						return
					}
					got := parseNFOString(t, "<"+field.root+">"+body+"</"+field.root+">")
					value := got.Name
					if field.tag == "sorttitle" {
						value = got.SortName
					}
					if value != boundary.decoded || len(value) != 1024 {
						t.Errorf("catalog name was truncated or changed: got %d bytes, want 1024", len(value))
					}
				})
			}
		})
	}
}

func TestParseNFOCatalogNameLimitDoesNotRestrictOtherText(t *testing.T) {
	value := strings.Repeat("a", 64*1024)
	input := "<movie><title>Short title</title>" +
		"<originaltitle>" + value + "</originaltitle><plot>" + value + "</plot>" +
		"<genre>" + value + "</genre><tag>" + value + "</tag><studio>" + value + "</studio>" +
		"<actor><name>" + value + "</name><role>" + value + "</role></actor></movie>"
	got := parseNFOString(t, input)
	if got.Name != "Short title" || got.OriginalTitle != value || got.Overview != value ||
		len(got.Genres) != 1 || got.Genres[0] != value || len(got.Tags) != 1 || got.Tags[0] != value ||
		len(got.Studios) != 1 || got.Studios[0] != value || len(got.People) != 1 ||
		got.People[0].Name != value || got.People[0].Role != value {
		t.Error("catalog name storage limit changed another text field's 64 KiB contract")
	}
}

func TestParseNFOProviderIDs(t *testing.T) {
	t.Run("normalization and duplicates", func(t *testing.T) {
		input := `<movie><imdbid>tt123</imdbid><uniqueid type="IMDB"> tt123 </uniqueid><tmdbid>456</tmdbid><tvdbid>789</tvdbid><uniqueid type="Vendor42">abc-123._:v2</uniqueid><uniqueid type="Vendor42">abc-123._:v2</uniqueid></movie>`
		want := Metadata{Kind: "movie", ProviderIDs: map[string]string{"Imdb": "tt123", "Tmdb": "456", "Tvdb": "789", "Vendor42": "abc-123._:v2"}}
		assertNFOMetadata(t, parseNFOString(t, input), want)
	})
	t.Run("conflicting duplicate", func(t *testing.T) {
		assertNFOError(t, `<movie><imdbid>tt123</imdbid><uniqueid type="imdb">tt456</uniqueid></movie>`)
		assertNFOError(t, `<movie><uniqueid type="Vendor42">first</uniqueid><uniqueid type="Vendor42">second</uniqueid></movie>`)
	})
	for _, provider := range []string{"unsafe-key", "unsafe.key", "a/b", "a:b", "white space", "Müsic"} {
		t.Run("invalid key "+provider, func(t *testing.T) {
			assertNFOError(t, `<movie><uniqueid type="`+provider+`">123</uniqueid></movie>`)
		})
	}
	for _, value := range []string{"https://example.com/id", "two words", "a\nb", "a\tb", "id?query", strings.Repeat("a", 257)} {
		t.Run(fmt.Sprintf("invalid value %q", value), func(t *testing.T) {
			assertNFOError(t, `<movie><uniqueid type="imdb">`+value+`</uniqueid></movie>`)
		})
	}
	t.Run("maximum value length", func(t *testing.T) {
		value := strings.Repeat("a", 256)
		got := parseNFOString(t, `<movie><uniqueid type="Vendor42">`+value+`</uniqueid></movie>`)
		if got.ProviderIDs["Vendor42"] != value {
			t.Fatalf("provider value was not preserved")
		}
	})
}

func TestParseNFOCollectionsPreserveOrder(t *testing.T) {
	input := `<movie><genre>Rock/Pop</genre><genre> Jazz </genre><genre>Rock/Pop</genre><tag>A,B</tag><tag>A,B</tag><tag>Second</tag><studio> One </studio><studio>One</studio><studio>Two</studio><director>A/B</director><writer>C,D</writer></movie>`
	want := Metadata{
		Kind: "movie", Genres: []string{"Rock/Pop", "Jazz"}, Tags: []string{"A,B", "Second"}, Studios: []string{"One", "Two"},
		People: []Person{{Name: "A/B", Type: "Director"}, {Name: "C,D", Type: "Writer"}},
	}
	assertNFOMetadata(t, parseNFOString(t, input), want)
}

func TestParseNFOActorFields(t *testing.T) {
	input := `<movie><actor><name> Ada </name><role> Narrator </role><order>2147483647</order><unknown>ignored</unknown></actor><actor><name>Ben</name><role/><order> </order></actor></movie>`
	want := Metadata{Kind: "movie", People: []Person{
		{Name: "Ada", Role: "Narrator", Type: "Actor", SortOrder: nfoInt(2147483647)},
		{Name: "Ben", Type: "Actor"},
	}}
	assertNFOMetadata(t, parseNFOString(t, input), want)
}

func TestParseNFOInvalidNumbers(t *testing.T) {
	cases := []struct {
		name   string
		wrap   string
		values []string
	}{
		{"year", "<movie><year>%s</year></movie>", []string{"0", "-1", "10000", "1.5", "true", "999999999999999999999999"}},
		{"season", "<season><season>%s</season></season>", []string{"-1", "2147483648", "1.5", "NaN"}},
		{"episode", "<episodedetails><episode>%s</episode></episodedetails>", []string{"-1", "2147483648", "1.5", "NaN"}},
		{"parent season", "<episodedetails><season>%s</season></episodedetails>", []string{"-1", "2147483648"}},
		{"actor order", "<movie><actor><name>Ada</name><order>%s</order></actor></movie>", []string{"-1", "2147483648", "1.5", "NaN"}},
		{"rating", "<movie><rating>%s</rating></movie>", []string{"-0.1", "10.1", "NaN", "+Inf", "-Inf", "Infinity", "1e309", "unknown"}},
	}
	for _, tc := range cases {
		for _, value := range tc.values {
			t.Run(tc.name+"/"+value, func(t *testing.T) {
				assertNFOError(t, fmt.Sprintf(tc.wrap, value))
			})
		}
	}
}

func TestParseNFONumericBoundaries(t *testing.T) {
	for _, year := range []int{1, 9999} {
		input := fmt.Sprintf("<movie><year> %d </year></movie>", year)
		assertNFOMetadata(t, parseNFOString(t, input), Metadata{Kind: "movie", ProductionYear: nfoInt(year)})
	}
	for _, rating := range []float64{0, 10} {
		input := fmt.Sprintf("<movie><rating>%g</rating></movie>", rating)
		assertNFOMetadata(t, parseNFOString(t, input), Metadata{Kind: "movie", CommunityRating: nfoFloat(rating)})
	}
}

func TestParseNFODates(t *testing.T) {
	t.Run("RFC3339 normalized to UTC", func(t *testing.T) {
		got := parseNFOString(t, "<movie><premiered>2024-02-29T12:30:45+08:00</premiered></movie>")
		want := time.Date(2024, time.February, 29, 4, 30, 45, 0, time.UTC)
		assertNFOMetadata(t, got, Metadata{Kind: "movie", PremiereDate: &want})
		if got.PremiereDate.Location() != time.UTC {
			t.Fatalf("PremiereDate location = %v, want UTC", got.PremiereDate.Location())
		}
	})
	t.Run("premiered takes priority over aired", func(t *testing.T) {
		for _, body := range []string{
			"<aired>2024-01-01</aired><premiered>2024-02-29</premiered>",
			"<premiered>2024-02-29</premiered><aired>2024-01-01</aired>",
		} {
			assertNFOMetadata(t, parseNFOString(t, "<episodedetails>"+body+"</episodedetails>"), Metadata{Kind: "episodedetails", PremiereDate: nfoDate(2024, time.February, 29)})
		}
	})
	for _, value := range []string{
		"2023-02-29", "2024-13-01", "2024-02-30", "2024-1-01", "0000-01-01",
		"2024-01-01T12:00:00", "2024-01-01T1:00:00Z", "2024-01-01T12:00:00+24:00",
		"2024-01-01T12:00:00+00:60", "2024-01-01T12:00:00,123Z",
		"2024-01-01T12:00:00.1234567890Z", "01/02/2024", "invalid",
	} {
		t.Run(value, func(t *testing.T) {
			assertNFOError(t, "<movie><premiered>"+value+"</premiered></movie>")
		})
	}
	t.Run("invalid lower priority date is rejected", func(t *testing.T) {
		assertNFOError(t, "<episodedetails><premiered>2024-02-29</premiered><aired>invalid</aired></episodedetails>")
	})
	t.Run("nanosecond precision", func(t *testing.T) {
		got := parseNFOString(t, "<movie><premiered>2024-01-01T12:00:00.123456789Z</premiered></movie>")
		want := time.Date(2024, time.January, 1, 12, 0, 0, 123456789, time.UTC)
		assertNFOMetadata(t, got, Metadata{Kind: "movie", PremiereDate: &want})
	})
}

func TestParseNFOIgnoresUnknownMetadata(t *testing.T) {
	input := `<MoViE xmlns:foreign="urn:example"><foreign:title>Foreign title</foreign:title><foreign:year>invalid</foreign:year><foreign:uniqueid type="imdb">tt999</foreign:uniqueid><unknown><title>Nested title</title><year>invalid</year></unknown><title>Visible title</title><thumb>file:///etc/passwd</thumb><trailer>https://invalid.example/stream</trailer><path>/outside/library/movie.mkv</path></MoViE>`
	assertNFOMetadata(t, parseNFOString(t, input), Metadata{Kind: "movie", Name: "Visible title"})
}

func TestParseNFORejectsUnsafeOrMalformedXML(t *testing.T) {
	cases := map[string]string{
		"external entity":                `<!DOCTYPE movie [<!ENTITY local SYSTEM "file:///etc/passwd">]><movie><title>&local;</title></movie>`,
		"external DTD":                   `<!DOCTYPE movie SYSTEM "https://invalid.example/nfo.dtd"><movie/>`,
		"internal DTD":                   `<!DOCTYPE movie [<!ENTITY greeting "hello">]><movie><title>&greeting;</title></movie>`,
		"bare doctype":                   `<!DOCTYPE movie><movie/>`,
		"directive":                      `<!SOMETHING data><movie/>`,
		"undeclared entity":              `<movie><title>&custom;</title></movie>`,
		"non UTF8 declaration":           `<?xml version="1.0" encoding="ISO-8859-1"?><movie/>`,
		"UTF16 declaration":              `<?xml version="1.0" encoding="UTF-16"?><movie/>`,
		"duplicate declaration encoding": `<?xml version="1.0" encoding="UTF-8" encoding="UTF-8"?><movie/>`,
		"declaration missing version":    `<?xml encoding="UTF-8"?><movie/>`,
		"declaration inside root":        `<movie><?xml version="1.0" encoding="UTF-8"?></movie>`,
		"non XML processing instruction": `<?stylesheet href="file:///etc/passwd"?><movie/>`,
		"duplicate attributes":           `<movie example="first" example="second"/>`,
		"duplicate expanded attributes":  `<movie xmlns:a="urn:example" xmlns:b="urn:example" a:value="first" b:value="second"/>`,
		"invalid UTF8":                   "<movie><title>\xff</title></movie>",
		"control character":              "<movie><title>\x01</title></movie>",
		"unclosed root":                  `<movie><title>Example</title>`,
		"mismatched element":             `<movie><title>Example</plot></movie>`,
		"two roots":                      `<movie/><movie/>`,
		"text before root":               `unexpected<movie/>`,
		"text after root":                `<movie/>unexpected`,
		"unknown root":                   `<book><title>Example</title></book>`,
		"default namespace root":         `<movie xmlns="urn:example"/>`,
		"prefixed root":                  `<foreign:movie xmlns:foreign="urn:example"/>`,
		"empty document":                 "",
		"whitespace document":            " \n\t ",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) { assertNFOError(t, input) })
	}
}

func TestParseNFOAcceptsCommentsAndPredefinedEntities(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?><!-- Before --><movie><!-- Inside --><title>A &amp; B &lt; C &gt; D &quot;E&quot; &apos;F&apos;</title></movie><!-- After -->`
	assertNFOMetadata(t, parseNFOString(t, input), Metadata{Kind: "movie", Name: `A & B < C > D "E" 'F'`})
}

func TestParseNFOAcceptsUTF8BOM(t *testing.T) {
	input := "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?><movie><title>Example</title></movie>`
	assertNFOMetadata(t, parseNFOString(t, input), Metadata{Kind: "movie", Name: "Example"})
}

func TestParseNFORejectsNilReader(t *testing.T) {
	got, err := ParseNFO(nil)
	if err == nil {
		t.Fatal("ParseNFO(nil) succeeded, want error")
	}
	if !reflect.DeepEqual(got, Metadata{}) {
		t.Fatalf("ParseNFO(nil) returned partial metadata: %#v", got)
	}
}

func TestParseNFODiscardsMetadataOnError(t *testing.T) {
	assertNFOError(t, `<movie><title>Valid title</title><uniqueid type="imdb">tt123</uniqueid><genre>Drama</genre><year>invalid</year></movie>`)
	assertNFOError(t, `<movie><title>Valid title</title><uniqueid type="imdb">tt123</uniqueid><genre>Drama</genre><actor><name>Actor</name><order>-1</order></actor></movie>`)
}

func TestParseNFOResourceLimits(t *testing.T) {
	t.Run("maximum input size", func(t *testing.T) {
		parseNFOString(t, paddedNFO(2*1024*1024))
	})
	t.Run("input too large", func(t *testing.T) {
		assertNFOError(t, paddedNFO(2*1024*1024+1))
	})
	t.Run("maximum depth", func(t *testing.T) {
		parseNFOString(t, "<movie>"+strings.Repeat("<unknown>", 63)+strings.Repeat("</unknown>", 63)+"</movie>")
	})
	t.Run("depth exceeded", func(t *testing.T) {
		assertNFOError(t, "<movie>"+strings.Repeat("<unknown>", 64)+strings.Repeat("</unknown>", 64)+"</movie>")
	})
	t.Run("maximum field bytes", func(t *testing.T) {
		value := strings.Repeat("a", 64*1024)
		got := parseNFOString(t, "<movie><plot>"+value+"</plot></movie>")
		if got.Overview != value {
			t.Fatal("field content was truncated")
		}
	})
	t.Run("field too large", func(t *testing.T) {
		assertNFOError(t, "<movie><plot>"+strings.Repeat("a", 64*1024+1)+"</plot></movie>")
	})
	t.Run("UTF8 bytes rather than characters", func(t *testing.T) {
		assertNFOError(t, "<movie><plot>"+strings.Repeat("é", 32*1024+1)+"</plot></movie>")
	})
	t.Run("unknown field too large", func(t *testing.T) {
		assertNFOError(t, "<movie><unknown>"+strings.Repeat("a", 64*1024+1)+"</unknown></movie>")
	})
	t.Run("field limit accumulates text segments", func(t *testing.T) {
		assertNFOError(t, "<movie><plot>"+strings.Repeat("a", 32*1024)+"<!-- Separator --><![CDATA["+strings.Repeat("b", 32*1024+1)+"]]></plot></movie>")
	})
	t.Run("maximum attribute bytes", func(t *testing.T) {
		parseNFOString(t, `<movie><unknown value="`+strings.Repeat("a", 64*1024)+`"/></movie>`)
	})
	t.Run("unknown attribute too large", func(t *testing.T) {
		assertNFOError(t, `<movie><unknown value="`+strings.Repeat("a", 64*1024+1)+`"/></movie>`)
	})
	t.Run("maximum attribute count", func(t *testing.T) {
		var input strings.Builder
		input.WriteString("<movie><unknown")
		for i := 0; i < 64; i++ {
			fmt.Fprintf(&input, ` value%d="data"`, i)
		}
		input.WriteString("/></movie>")
		parseNFOString(t, input.String())
	})
	t.Run("attribute count exceeded", func(t *testing.T) {
		var input strings.Builder
		input.WriteString("<movie><unknown")
		for i := 0; i < 65; i++ {
			fmt.Fprintf(&input, ` value%d="data"`, i)
		}
		input.WriteString("/></movie>")
		assertNFOError(t, input.String())
	})
	for _, tag := range []string{"genre", "tag", "studio"} {
		t.Run(tag+" maximum collection", func(t *testing.T) {
			var input strings.Builder
			input.WriteString("<movie>")
			for i := 0; i < 1024; i++ {
				fmt.Fprintf(&input, "<%s>Value %d</%s>", tag, i, tag)
			}
			input.WriteString("</movie>")
			parseNFOString(t, input.String())
		})
		t.Run(tag+" collection exceeded", func(t *testing.T) {
			var input strings.Builder
			input.WriteString("<movie>")
			for i := 0; i < 1025; i++ {
				fmt.Fprintf(&input, "<%s>Value %d</%s>", tag, i, tag)
			}
			input.WriteString("</movie>")
			assertNFOError(t, input.String())
		})
	}
	t.Run("people collection exceeded", func(t *testing.T) {
		var input strings.Builder
		input.WriteString("<movie>")
		for i := 0; i < 1025; i++ {
			fmt.Fprintf(&input, "<actor><name>Person %d</name></actor>", i)
		}
		input.WriteString("</movie>")
		assertNFOError(t, input.String())
	})
	t.Run("provider collection exceeded", func(t *testing.T) {
		var input strings.Builder
		input.WriteString("<movie>")
		for i := 0; i < 1025; i++ {
			fmt.Fprintf(&input, `<uniqueid type="Vendor%d">id</uniqueid>`, i)
		}
		input.WriteString("</movie>")
		assertNFOError(t, input.String())
	})
	t.Run("node count exceeded", func(t *testing.T) {
		assertNFOError(t, "<movie>"+strings.Repeat("<unknown/>", 16384)+"</movie>")
	})
}

func TestParseNFOPropagatesReaderFailure(t *testing.T) {
	sentinel := errors.New("source read failed")
	reader := io.MultiReader(strings.NewReader("<movie><title>Example</title>"), nfoFailReader{sentinel})
	_, err := ParseNFO(reader)
	if !errors.Is(err, sentinel) {
		t.Fatalf("ParseNFO() error = %v, want wrapped source error", err)
	}
}

type nfoFailReader struct {
	err error
}

func (r nfoFailReader) Read([]byte) (int, error) {
	return 0, r.err
}

func parseNFOString(t *testing.T, input string) Metadata {
	t.Helper()
	got, err := ParseNFO(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseNFO() error = %v", err)
	}
	return got
}

func assertNFOError(t *testing.T, input string) {
	t.Helper()
	got, err := ParseNFO(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParseNFO() succeeded, want error")
	}
	if !reflect.DeepEqual(got, Metadata{}) {
		t.Fatalf("ParseNFO() returned partial metadata on error: %#v", got)
	}
}

func assertNFOMetadata(t *testing.T, got, want Metadata) {
	t.Helper()
	normalizeNFOCollections(&got)
	normalizeNFOCollections(&want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseNFO() = %#v, want %#v", got, want)
	}
}

func normalizeNFOCollections(value *Metadata) {
	if len(value.ProviderIDs) == 0 {
		value.ProviderIDs = nil
	}
	if len(value.Genres) == 0 {
		value.Genres = nil
	}
	if len(value.Tags) == 0 {
		value.Tags = nil
	}
	if len(value.Studios) == 0 {
		value.Studios = nil
	}
	if len(value.People) == 0 {
		value.People = nil
	}
}

func nfoInt(value int) *int { return &value }

func nfoFloat(value float64) *float64 { return &value }

func nfoDate(year int, month time.Month, day int) *time.Time {
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return &value
}

func paddedNFO(size int) string {
	const fieldSize = 64 * 1024
	const elementSize = len("<unknown></unknown>")
	var input strings.Builder
	input.Grow(size)
	input.WriteString("<movie>")
	remaining := size - len("<movie></movie>")
	for remaining > fieldSize+elementSize {
		input.WriteString("<unknown>")
		input.WriteString(strings.Repeat("a", fieldSize))
		input.WriteString("</unknown>")
		remaining -= fieldSize + elementSize
	}
	input.WriteString("<unknown>")
	input.WriteString(strings.Repeat("a", remaining-elementSize))
	input.WriteString("</unknown></movie>")
	return input.String()
}
