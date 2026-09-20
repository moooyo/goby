package server

import (
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestNavigationQueryParsesActualSelectorsAndRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{"Years=2025,2026&MinCommunityRating=8.5&IsHD=true&HasSubtitles=false", "ExcludeItemTypes=Folder,Season&NameStartsWith=100%25_", "MinPremiereDate=2025-01-01T00:00:00Z&MaxPremiereDate=2026-01-01T00:00:00Z", "isstandalonespecial=true"} {
		query := library.Query{}
		if !readNavigationFilters(httptest.NewRecorder(), httptest.NewRequest("GET", "/emby/Items?"+raw, nil), &query) || !hasNavigationFilters(query) {
			t.Fatalf("valid selector was not parsed: %q", raw)
		}
	}
	for _, raw := range []string{"Years=", "Years=2025,,2026", "Years=0", "Years=10000", "Years=2025&years=2026", "MinCommunityRating=NaN", "MinCommunityRating=11", "IsHD=perhaps", "ExcludeItemTypes=Movie,", "MinDateCreated=yesterday", "MinPremiereDate=2026-01-01T00:00:00Z&MaxPremiereDate=2025-01-01T00:00:00Z", "Years=%zz", "IsStandaloneSpecial=true&isstandalonespecial=false", "IsStandaloneSpecial="} {
		query := library.Query{}
		request := httptest.NewRequest("GET", "/emby/Items", nil)
		request.URL.RawQuery = raw
		if readNavigationFilters(httptest.NewRecorder(), request, &query) {
			t.Fatalf("invalid selector accepted: %q", raw)
		}
	}
}

func TestPhase3NamespacePreservesOpaqueIdsAndNamedEntitySegments(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"/items/counts", "/emby/Items/Counts"}, {"/items/AbC/ancestors", "/emby/Items/AbC/Ancestors"},
		{"/items/counts/images/Primary", "/emby/Items/counts/Images/Primary"},
		{"/videos/AbC/additionalparts", "/emby/Videos/AbC/AdditionalParts"},
		{"/artists/albumartists/Alice%2FBob", "/emby/Artists/AlbumArtists/Alice%2FBob"},
		{"/artists/albumartists/images", "/emby/Artists/AlbumArtists/images"},
		{"/artists/albumartists/images/Primary", "/emby/Artists/albumartists/Images/Primary"},
		{"/users/AbC/images/primary/0/delete", "/emby/Users/AbC/Images/Primary/0/Delete"},
		{"/users/AbC/playingitems/XyZ/progress", "/emby/Users/AbC/PlayingItems/XyZ/Progress"},
		{"/displaypreferences/aBc", "/emby/DisplayPreferences/aBc"},
		{"/environment/directorycontents", "/emby/Environment/DirectoryContents"},
		{"/library/virtualfolders/paths/delete", "/emby/Library/VirtualFolders/Paths/Delete"},
	} {
		request := httptest.NewRequest("GET", test.path, nil)
		result := compatibilityNamespace(request)
		if result.URL.EscapedPath() != test.want {
			t.Fatalf("alias changed a literal or opaque identity: %q -> %q, want %q", test.path, result.URL.EscapedPath(), test.want)
		}
	}
}
