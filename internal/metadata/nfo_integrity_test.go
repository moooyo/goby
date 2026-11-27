package metadata

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestNFORecoverySeasonNumberUsesOriginalDocument(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       bool
	}{
		{"absent", `<season><title>Season</title></season>`, true},
		{"season", `<season><season>1</season></season>`, true},
		{"seasonnumber", `<season><seasonnumber>01</seasonnumber></season>`, true},
		{"matching aliases", `<season><season>1</season><SeasonNumber>&#49;</SeasonNumber></season>`, true},
		{"conflicting aliases", `<season><season>1</season><seasonnumber>2</seasonnumber></season>`, false},
		{"conflicting repeated alias", `<season><seasonnumber>1</seasonnumber><seasonnumber>2</seasonnumber></season>`, false},
		{"wrong season", `<season><season>2</season></season>`, false},
		{"empty explicit number", `<season><seasonnumber/></season>`, false},
		{"nested explicit number", `<season><seasonnumber><value>1</value></seasonnumber></season>`, false},
		{"invalid explicit number", `<season><seasonnumber>-1</seasonnumber></season>`, false},
		{"UTF-16", encodeNFOTestUTF16(binary.BigEndian, `<?xml version="1.0" encoding="UTF-16"?><season><seasonnumber>1</seasonnumber></season>`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseNFO(strings.NewReader(tc.data)); err != nil {
				t.Fatalf("ordinary import rejected the fixture: %v", err)
			}
			if got := NFOSeasonMatchesRecovery([]byte(tc.data), 1); got != tc.want {
				t.Fatalf("recovery match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNFORecoveryDistinguishesWireDamageFromRejectedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"zero bytes", "", "empty"},
		{"invalid UTF-8", "\x81\xff\x00\x03", "malformed"},
		{"truncated XML", "<movie><title>Short", "malformed"},
		{"NUL padded wrong episode", "<episodedetails><episode>9</episode></episodedetails>\x00\x00", "malformed"},
		{"truncated UTF-16", "\xff\xfe<\x00x", "malformed"},
		{"unknown declared encoding", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><movie>\xff</movie>", ""},
		{"UTF-32 BOM", "\xff\xfe\x00\x00<\x00\x00\x00", ""},
		{"unsupported directives", "<!DOCTYPE movie><movie/>", ""},
		{"structurally valid wrong kind", "<tvshow><title>Another item</title></tvshow>", ""},
		{"structurally valid wrong fields", "<episodedetails><episode>-3</episode></episodedetails>", ""},
		{"pair must still prove its binding", "<episodedetails><episode>3</episode></episodedetails><episodedetails><episode>1</episode></episodedetails>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NFOIntegrityFailure([]byte(tc.data)); got != tc.want {
				t.Fatalf("integrity classification = %q, want %q", got, tc.want)
			}
		})
	}
}
