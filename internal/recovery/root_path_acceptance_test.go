package recovery

import "testing"

func TestRestoredRootPathAcceptance(t *testing.T) {
	approved := map[string]bool{"/media": true, `/media\archive`: true, `/media\..\outside`: true}
	for _, test := range []struct {
		name, full, allowed, relative string
		want                          bool
	}{
		{name: "literal-backslash", full: `/media/a\b`, allowed: "/media", relative: `a\b`, want: true},
		{name: "exact-anchor", full: `/media\archive`, allowed: `/media\archive`, relative: ".", want: true},
		{name: "nested-root", full: `/media\archive/Shows\2026/Season 01`, allowed: `/media\archive`, relative: `Shows\2026/Season 01`, want: true},
		{name: "different-literal-name", full: "/media/a/b", allowed: "/media", relative: `a\b`},
		{name: "relative-traversal", full: `/media/a\..\b`, allowed: "/media", relative: `a\..\b`},
		{name: "mixed-traversal", full: `/media/safe\../movie`, allowed: "/media", relative: `safe\../movie`},
		{name: "anchor-traversal", full: `/media\..\outside/movie`, allowed: `/media\..\outside`, relative: "movie"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validRestoredRoot(test.full, test.allowed, test.relative, approved); got != test.want {
				t.Fatalf("restored root acceptance = %t, want %t", got, test.want)
			}
		})
	}
}
