package server

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

// Playback planning combines primary probe facts with separately indexed
// sidecars. The cached primary probe and original-file byte stream stay intact.
func playbackMediaInfo(item library.Item) media.Info {
	if item.Media == nil {
		return media.Info{}
	}
	info := *item.Media
	info.Streams = append([]media.Stream(nil), item.Media.Streams...)
	for _, source := range item.Subtitles {
		codec := source.Codec
		if codec == "vtt" {
			codec = "webvtt"
		}
		info.Streams = append(info.Streams, media.Stream{Index: source.Index, Codec: codec, CodecType: "subtitle",
			Language: source.Language, Title: source.Title, IsDefault: source.IsDefault, IsForced: source.IsForced,
			IsExternal: true, IsTextSubtitleStream: true})
	}
	return info
}

func externalSubtitleURL(itemID string, index int, format, token string) string {
	path := "/Videos/" + url.PathEscape(itemID) + "/" + url.PathEscape(media.SourceID(itemID)) +
		"/Subtitles/" + strconv.Itoa(index) + "/0/Stream." + format
	if token != "" {
		path += "?" + url.Values{"api_key": {token}}.Encode()
	}
	return path
}

func externalSubtitleDisplayLanguage(language string) string {
	if strings.TrimSpace(language) == "" {
		return ""
	}
	switch strings.ToLower(language) {
	case "en", "eng":
		return "English"
	default:
		// Keep unrecognized identifiers instead of guessing a language name.
		return language
	}
}

func externalSubtitleDisplayTitle(source library.Subtitle, language string) string {
	base := source.Title
	if strings.TrimSpace(base) == "" || base == source.Language {
		// Existing scans store the language identifier as a synthetic Title.
		// Handle those rows here without rewriting metadata or requiring a scan.
		base = language
	}
	qualifiers := make([]string, 0, 3)
	if source.IsForced {
		qualifiers = append(qualifiers, "Forced")
	}
	// Custom titles remain the base, and SDH remains visible when recorded.
	// Their combination is Goby's display policy, not a claimed reference rule.
	if source.IsHearingImpaired {
		qualifiers = append(qualifiers, "SDH")
	}
	if source.Codec != "" {
		qualifiers = append(qualifiers, strings.ToUpper(source.Codec))
	}
	if len(qualifiers) == 0 {
		return base
	}
	suffix := "(" + strings.Join(qualifiers, " ") + ")"
	if base == "" {
		return suffix
	}
	return base + " " + suffix
}

func itemMediaStreamsDTO(item library.Item) []map[string]any {
	if item.Media == nil {
		return []map[string]any{}
	}
	streams := mediaStreamsDTO(item.Media.Streams)
	for _, source := range item.Subtitles {
		language := externalSubtitleDisplayLanguage(source.Language)
		stream := map[string]any{
			"Index": source.Index, "Type": "Subtitle", "Codec": source.Codec,
			"Language": source.Language, "Title": source.Title, "DisplayTitle": externalSubtitleDisplayTitle(source, language),
			"IsDefault": source.IsDefault, "IsForced": source.IsForced, "IsHearingImpaired": source.IsHearingImpaired,
			"IsExternal": true, "IsTextSubtitleStream": true, "SupportsExternalStream": true,
			"Protocol": "File", "Path": filepath.Join(filepath.Dir(item.Path), source.Filename),
			"DeliveryMethod": "External", "DeliveryUrl": externalSubtitleURL(item.ID, source.Index, source.Codec, ""),
		}
		if language != "" {
			stream["DisplayLanguage"] = language
		}
		streams = append(streams, stream)
	}
	return streams
}

func addSubtitleDeliveryCredentials(dto map[string]any, itemID, token string, formats map[int]string) {
	if streams, ok := dto["MediaStreams"].([]map[string]any); ok {
		for _, stream := range streams {
			if stream["IsExternal"] != true || stream["Type"] != "Subtitle" {
				continue
			}
			index, ok := stream["Index"].(int)
			if !ok {
				continue
			}
			format, _ := stream["Codec"].(string)
			if selected := formats[index]; selected != "" {
				format = selected
			}
			stream["DeliveryUrl"] = externalSubtitleURL(itemID, index, format, token)
		}
	}
	if sources, ok := dto["MediaSources"].([]map[string]any); ok {
		for _, source := range sources {
			addSubtitleDeliveryCredentials(source, itemID, token, formats)
		}
	}
}

func (s *Server) itemDTOForRequest(r *http.Request, item library.Item, fields []string, detail bool) map[string]any {
	dto := s.itemDTO(item, fields, detail)
	token, _, _ := parseEmbyCredentials(r)
	addSubtitleDeliveryCredentials(dto, item.ID, token, nil)
	return dto
}
