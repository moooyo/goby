package metadata

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestParseNFOUTF16(t *testing.T) {
	const document = `<movie><title>Café, 🐈 &amp; Trees</title><plot>Explicit � is valid text.</plot></movie>`
	want := Metadata{Kind: "movie", Name: "Café, 🐈 & Trees", Overview: "Explicit � is valid text."}
	for _, endianness := range []struct {
		name  string
		order binary.ByteOrder
		other string
	}{
		{"UTF-16LE", binary.LittleEndian, "UTF-16BE"},
		{"UTF-16BE", binary.BigEndian, "UTF-16LE"},
	} {
		t.Run(endianness.name, func(t *testing.T) {
			encode := func(text string) string { return encodeNFOTestUTF16(endianness.order, text) }
			for _, declaration := range []string{"", `<?xml version="1.0"?>`, `<?xml version="1.0" encoding="utf-16" standalone="yes"?>`,
				`<?xml version="1.0" encoding="` + endianness.name + `"?>`} {
				assertNFOMetadata(t, parseNFOString(t, encode(declaration+document)), want)
			}

			for name, text := range map[string]string{
				"UTF-8 declaration":  `<?xml version="1.0" encoding="UTF-8"?>` + document,
				"wrong byte order":   `<?xml version="1.0" encoding="` + endianness.other + `"?>` + document,
				"unsupported label":  `<?xml version="1.0" encoding="ISO-8859-1"?>` + document,
				"duplicate encoding": `<?xml version="1.0" encoding="UTF-16" encoding="UTF-16"?>` + document,
				"version 1.1":        `<?xml version="1.1" encoding="UTF-16"?>` + document,
				"directive":          `<!DOCTYPE movie>` + document,
				"undeclared entity":  `<movie><title>&unknown;</title></movie>`,
				"root namespace":     `<movie xmlns="urn:foreign"/>`,
				"two roots":          document + `<movie/>`,
				"truncated XML":      `<movie><title>Unfinished</title>`,
				"UTF-8 field limit":  `<movie><plot>` + strings.Repeat("界", maxFieldBytes/3+1) + `</plot></movie>`,
			} {
				t.Run(name, func(t *testing.T) { assertNFOError(t, encode(text)) })
			}
			t.Run("no BOM", func(t *testing.T) { assertNFOError(t, encode(document)[2:]) })
			t.Run("odd byte count", func(t *testing.T) { assertNFOError(t, encode(document)+"\x00") })
			for _, unit := range []uint16{0xd800, 0xdfff} {
				// Decode the entire input strictly, including ignored XML comments.
				var bad [2]byte
				endianness.order.PutUint16(bad[:], unit)
				assertNFOError(t, encode(`<!--`)+string(bad[:])+encode(`-->` + document)[2:])
				assertNFOError(t, encode(document)+string(bad[:]))
			}
		})
	}
	t.Run("raw byte limit includes BOM", func(t *testing.T) {
		text := paddedNFO((maxNFOBytes - 2) / 2)
		assertNFOMetadata(t, parseNFOString(t, encodeNFOTestUTF16(binary.LittleEndian, text)), Metadata{Kind: "movie"})
		assertNFOError(t, encodeNFOTestUTF16(binary.LittleEndian, text+" "))
	})
	t.Run("UTF-32 BOM is not UTF-16", func(t *testing.T) {
		assertNFOError(t, "\xff\xfe\x00\x00"+encodeNFOTestUTF16(binary.LittleEndian, document)[2:])
		assertNFOError(t, "\x00\x00\xfe\xff"+encodeNFOTestUTF16(binary.BigEndian, document)[2:])
	})
}

func encodeNFOTestUTF16(order binary.ByteOrder, text string) string {
	units := utf16.Encode([]rune(text))
	data := make([]byte, 2+2*len(units))
	order.PutUint16(data, 0xfeff)
	for i, unit := range units {
		order.PutUint16(data[2+i*2:], unit)
	}
	return string(data)
}
