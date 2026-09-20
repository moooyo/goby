package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompatibilityNamespacePreservesIdentifiersAndEncodedNames(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"/videos/AbC123/original.mp4?api_key=opaque", "/emby/Videos/AbC123/original.mp4"},
		{"/EMBY/users/AbC/items/ReS123", "/emby/Users/AbC/Items/ReS123"},
		{"/users/authenticatebyname", "/emby/Users/AuthenticateByName"},
		{"/users/AbC/configuration/partial", "/emby/Users/AbC/Configuration/Partial"},
		{"/features?FeatureType=User", "/emby/Features"},
		{"/playlists/AbC/items/9007199254740993/move/0", "/emby/Playlists/AbC/Items/9007199254740993/Move/0"},
		{"/collections/AbC/items/delete", "/emby/Collections/AbC/Items/Delete"},
		{"/items/AbC/download", "/emby/Items/AbC/Download"},
		{"/items/AbC/deleteinfo", "/emby/Items/AbC/DeleteInfo"},
		{"/search/hints?SearchTerm=music", "/emby/Search/Hints"},
		{"/EMBY/search/entities/9007199254740993", "/emby/Search/Entities/9007199254740993"},
		{"/items/prefixes", "/emby/Items/Prefixes"},
		{"/items/AbC/instantmix", "/emby/Items/AbC/InstantMix"},
		{"/artists/prefixes", "/emby/Artists/Prefixes"},
		{"/artists/instantmix?Id=12", "/emby/Artists/InstantMix"},
		{"/artists/12/similar", "/emby/Artists/12/Similar"},
		{"/artists/albumartists/similar", "/emby/Artists/AlbumArtists/similar"},
		{"/artists/prefixes/images/Primary", "/emby/Artists/prefixes/Images/Primary"},
		{"/artists/albumartists/images/Primary", "/emby/Artists/albumartists/Images/Primary"},
		{"/musicgenres/AC%2FDC/instantmix", "/emby/MusicGenres/AC%2FDC/InstantMix"},
		{"/musicgenres/instantmix?Id=12", "/emby/MusicGenres/InstantMix"},
		{"/albums/AbC/similar", "/emby/Albums/AbC/Similar"},
		{"/songs/AbC/instantmix", "/emby/Songs/AbC/InstantMix"},
		{"/playlists/AbC/instantmix", "/emby/Playlists/AbC/InstantMix"},
		{"/users/AbC/suggestions", "/emby/Users/AbC/Suggestions"},
		{"/libraries/availableoptions", "/emby/Libraries/AvailableOptions"},
		{"/playlists/AbC/addtoplaylistinfo", "/emby/Playlists/AbC/AddToPlaylistInfo"},
		{"/system/activitylog/entries", "/emby/System/ActivityLog/Entries"},
		{"/system/logs/query", "/emby/System/Logs/Query"},
		{"/system/logs/MiXeD%2Fname/lines", "/emby/System/Logs/MiXeD%2Fname/Lines"},
		{"/SystemActivity/Entries", "/SystemActivity/Entries"},
		{"/sessions/notifications", "/emby/Sessions/Notifications"},
		{"/EMBY/sessions/notifications/test", "/emby/Sessions/Notifications/Test"},
		{"/items/AbC/remotesearch/subtitles/en", "/emby/Items/AbC/RemoteSearch/Subtitles/en"},
		{"/videos/AbC/SrC/attachments/2/stream", "/emby/Videos/AbC/SrC/Attachments/2/Stream"},
		{"/livestreams/open", "/emby/LiveStreams/Open"},
		{"/livestreams/LiVeID/hls/v1.m3u8", "/emby/LiveStreams/LiVeID/hls/v1.m3u8"},
		{"/genres/AC%2FDC", "/emby/Genres/AC%2FDC"},
		{"/persons/Playing", "/emby/Persons/Playing"},
		{"/admin/v1/users", "/admin/v1/users"},
		{"/admin/", "/admin/"},
		{"/healthz", "/healthz"},
		{"/unknown/path", "/unknown/path"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		before := request.URL.String()
		result := compatibilityNamespace(request)
		if result.URL.EscapedPath() != test.want || result.URL.RawQuery != request.URL.RawQuery {
			t.Errorf("namespace %q = %q, want %q", test.path, result.URL.String(), test.want)
		}
		if request.URL.String() != before {
			t.Error("namespace translation mutated the original request")
		}
	}
}

func TestRootCompatibilityAliasesSharePingAndMediaCORS(t *testing.T) {
	handler := (&Server{}).Handler()
	for _, path := range []string{"/System/Ping", "/system/ping", "/Emby/system/ping"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Body.String() != "Emby Server" {
			t.Errorf("root ping alias failed: %s, %d", path, response.Code)
		}
	}
	for _, path := range []string{"/Videos/id/original.mp4", "/audio/id/stream"} {
		request := httptest.NewRequest(http.MethodOptions, path, nil)
		request.Header.Set("Origin", "https://client.example")
		request.Header.Set("Access-Control-Request-Method", "HEAD")
		request.Header.Set("Access-Control-Request-Headers", "X-Emby-Token,If-Range")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "https://client.example" ||
			!strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "If-Range") {
			t.Errorf("media alias did not share compatibility CORS: %s", path)
		}
	}
}
