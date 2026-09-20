package server

import (
	"net/http"
	"net/url"
	"strings"
)

// compatibilityNamespace accepts the API with or without its /emby prefix.
// Only route literals are canonicalized; identifiers and escaped entity names
// retain their spelling and segment boundaries. Administrator paths are never
// translated into the token-authenticated namespace.
func compatibilityNamespace(r *http.Request) *http.Request {
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	if len(parts) == 0 {
		return r
	}
	first, err := url.PathUnescape(parts[0])
	if err != nil {
		return r
	}
	prefixed := strings.EqualFold(first, "emby")
	if prefixed {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return r
	}
	resources := []string{"System", "Users", "UserSettings", "DisplayPreferences", "Items", "Videos", "Audio", "Sessions", "Library", "Libraries", "Environment", "Shows", "Genres", "Tags", "Studios", "Persons", "Artists", "AlbumArtists", "MusicGenres", "Albums", "Songs", "Search", "Auth", "Devices", "ScheduledTasks", "Branding", "Playlists", "Collections", "LiveStreams", "LiveTv", "Providers", "Features", "Registrations"}
	resource := ""
	decoded, err := url.PathUnescape(parts[0])
	if err != nil {
		return r
	}
	for _, candidate := range resources {
		if strings.EqualFold(decoded, candidate) {
			resource = candidate
			break
		}
	}
	if resource == "" {
		return r
	}
	parts[0] = resource
	literal := func(index int, choices ...string) {
		if index >= len(parts) {
			return
		}
		value, err := url.PathUnescape(parts[index])
		if err != nil {
			return
		}
		for _, choice := range choices {
			if strings.EqualFold(value, choice) {
				parts[index] = choice
				return
			}
		}
	}
	switch resource {
	case "Playlists", "Collections":
		literal(2, "Items", "Delete", "Users")
		if resource == "Playlists" {
			literal(2, "InstantMix", "AddToPlaylistInfo")
		}
		literal(3, "Delete")
		literal(4, "Move", "Delete")
	case "LiveStreams":
		literal(1, "Open", "MediaInfo", "Close")
		literal(2, "hls")
	case "Providers":
		literal(1, "Subtitles")
		literal(2, "Subtitles")
	case "UserSettings":
		literal(2, "Partial")
	case "Environment":
		literal(1, "DefaultDirectoryBrowser", "DirectoryContents", "ParentPath", "ValidatePath")
	case "Artists", "AlbumArtists", "MusicGenres", "Genres", "Tags", "Studios", "Persons":
		if len(parts) == 2 && (resource == "Artists" || resource == "MusicGenres") {
			literal(1, "InstantMix")
			if resource == "Artists" {
				literal(1, "Prefixes")
			}
		}
		if resource == "MusicGenres" {
			literal(2, "InstantMix")
		}
		nestedArtists := false
		if resource == "Artists" {
			imagePath := false
			if len(parts) > 3 {
				next, _ := url.PathUnescape(parts[2])
				imagePath = strings.EqualFold(next, "Images")
			}
			if !imagePath {
				literal(1, "AlbumArtists")
				nestedArtists = len(parts) > 1 && parts[1] == "AlbumArtists"
			}
		}
		if !nestedArtists {
			literal(2, "Images")
			if resource == "Artists" {
				literal(2, "Similar")
			}
		}
	case "LiveTv":
		literal(1, "Programs")
	case "Search":
		literal(1, "Hints", "Entities")
	case "Albums":
		literal(2, "Similar", "InstantMix")
	case "Songs":
		literal(2, "InstantMix")
	case "Branding":
		literal(1, "Configuration", "Css", "Css.css")
	case "ScheduledTasks":
		literal(1, "Running")
		if len(parts) > 1 && parts[1] == "Running" {
			literal(3, "Delete")
		} else {
			literal(2, "Triggers")
		}
	case "Devices":
		literal(1, "Info", "Options", "Delete")
	case "Auth":
		literal(1, "Keys")
		if len(parts) > 1 && parts[1] == "Keys" {
			// The credential itself is opaque and case-sensitive.
			literal(3, "Delete")
		}
	case "System":
		literal(1, "Info", "Ping", "Endpoint", "Configuration", "ActivityLog", "Logs")
		if len(parts) > 1 && parts[1] == "Configuration" {
			literal(2, "Partial", "encoding", "subtitles", "tasks")
		} else if len(parts) > 1 && parts[1] == "ActivityLog" {
			literal(2, "Entries")
		} else if len(parts) > 1 && parts[1] == "Logs" {
			// Log names are opaque. Only known operation positions are folded.
			if len(parts) == 3 {
				literal(2, "Query")
			}
			literal(3, "Lines")
		} else {
			literal(2, "Public")
		}
	case "Users":
		literal(1, "Public", "Query", "New", "AuthenticateByName")
		literal(2, "Items", "Views", "Suggestions", "Authenticate", "PlayedItems", "FavoriteItems", "PlayingItems", "Password", "Policy", "Configuration", "Images", "Delete")
		if len(parts) > 2 && parts[2] == "Items" {
			literal(3, "Root", "Latest", "Resume")
			literal(4, "UserData", "HideFromResume", "Rating", "SpecialFeatures", "LocalTrailers")
		}
		if len(parts) > 2 && parts[2] == "Images" {
			literal(3, "Primary")
		}
		if len(parts) > 2 && parts[2] == "Configuration" {
			literal(3, "Partial")
		}
		literal(4, "Delete", "Progress")
		literal(5, "Delete")
	case "Items":
		if len(parts) == 2 {
			literal(1, "Counts", "Prefixes")
		}
		literal(2, "PlaybackInfo", "Ancestors", "UserData", "Images", "ThumbnailSet", "Refresh", "File", "Download", "Similar", "InstantMix", "ThemeMedia", "AddToPlaylistInfo", "Delete", "DeleteInfo", "RemoteSearch", "Subtitles")
		literal(3, "Subtitles", "Attachments")
		literal(4, "Delete")
		literal(5, "Stream")
		if len(parts) > 2 && parts[2] == "Images" {
			literal(3, "Thumbnail")
			literal(4, "Delete")
			literal(5, "Delete", "Index")
		}
	case "Videos", "Audio":
		literal(1, "ActiveEncodings")
		if resource == "Videos" {
			literal(2, "index.bif")
		}
		literal(2, "stream", "AdditionalParts", "master.m3u8", "main.m3u8", "live.m3u8", "subtitles.m3u8", "live_subtitles.m3u8", "hls1", "hls2", "Subtitles")
		if len(parts) > 1 && parts[1] == "ActiveEncodings" {
			literal(2, "Delete")
		}
		literal(3, "Subtitles", "Attachments")
		literal(4, "Delete")
		literal(5, "Stream")
	case "Sessions":
		literal(1, "Playing", "Logout", "Capabilities", "Notifications")
		if len(parts) > 1 && parts[1] == "Notifications" {
			literal(2, "Test")
		}
		literal(2, "Playing", "Progress", "Ping", "Stopped", "Full", "Command")
	case "Library":
		literal(1, "VirtualFolders", "Refresh")
		literal(2, "Query", "Delete", "LibraryOptions", "Name", "Paths")
		literal(3, "Delete")
	case "Libraries":
		literal(1, "AvailableOptions")
	case "Shows":
		literal(1, "NextUp")
		literal(2, "Seasons", "Episodes")
	}
	escaped := "/emby/" + strings.Join(parts, "/")
	if escaped == r.URL.EscapedPath() {
		return r
	}
	path, err := url.PathUnescape(escaped)
	if err != nil {
		return r
	}
	copy := r.Clone(r.Context())
	copy.URL.Path, copy.URL.RawPath = path, escaped
	return copy
}
