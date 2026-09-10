package recovery

import (
	"strings"
	"testing"
)

func TestRestoredRootMustMatchApprovedIdentityAndExactRelativePath(t *testing.T) {
	approved := map[string]bool{"/media": true, "/media/archive": true}
	for name, row := range map[string][3]string{
		"library_root":    {"/media", "/media", "."},
		"child":           {"/media/movies", "/media", "movies"},
		"nested_root":     {"/media/archive/music", "/media/archive", "music"},
		"unicode":         {"/media/\u97f3\u6a02", "/media", "\u97f3\u6a02"},
		"legal_newline":   {"/media/a\nb", "/media", "a\nb"},
		"legal_backslash": {"/media/a\\b", "/media", "a\\b"},
	} {
		t.Run(name, func(t *testing.T) {
			if !validRestoredRoot(row[0], row[1], row[2], approved) {
				t.Fatal("valid exact Linux path was rejected")
			}
		})
	}
	for name, row := range map[string][3]string{
		"unapproved":          {"/other/movie", "/other", "movie"},
		"prefix_escape":       {"/media-other/movie", "/media", "../media-other/movie"},
		"different_full":      {"/media/tv", "/media", "movies"},
		"absolute_relative":   {"/media/movie", "/media", "/media/movie"},
		"traversal":           {"/media/movie", "/media", "series/../movie"},
		"backslash_traversal": {"/media/a\\..\\b", "/media", "a\\..\\b"},
		"noncanonical":        {"/media//movie", "/media", "movie"},
		"empty_relative":      {"/media", "/media", ""},
		"nul":                 {"/media/a\x00b", "/media", "a\x00b"},
		"invalid_utf8":        {"/media/\xff", "/media", "\xff"},
		"oversized":           {"/media/" + strings.Repeat("x", 4097), "/media", strings.Repeat("x", 4097)},
	} {
		t.Run(name, func(t *testing.T) {
			if validRestoredRoot(row[0], row[1], row[2], approved) {
				t.Fatal("unsafe or mismatched restored root was accepted")
			}
		})
	}
}
