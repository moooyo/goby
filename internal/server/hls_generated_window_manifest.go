package server

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const hlsGeneratedWindowGraphQuery = "GobyHlsWindowGraphId"

// Logical URIs admit an immutable source slot. Each successful admission
// redirects to a separately bound exact producer URI; it does not advertise
// incomplete bytes or confuse a private window's ENDLIST with this presentation.
func hlsGeneratedWindowManifest(session *hlsSession, graph *hlsGeneratedWindowGraph, resource, token string, start int64, variants ...int) ([]byte, error) {
	if session == nil || graph == nil || graph.fallback || len(graph.timeline.Segments) == 0 || start < 0 || start >= graph.endpoint.DurationTicks {
		return nil, errInvalidHLSManifest
	}
	variant := 0
	if len(variants) > 1 {
		return nil, errInvalidHLSManifest
	}
	if len(variants) == 1 {
		variant = variants[0]
	}
	plan := session.key.plan
	if variant < 0 || variant >= max(1, plan.HLS.RenditionCount) {
		return nil, errInvalidHLSManifest
	}
	version := 3
	switch plan.HLS.SegmentType {
	case "mpegts":
	case "fmp4":
		version = 7
	default:
		return nil, errInvalidHLSManifest
	}
	var body strings.Builder
	fmt.Fprintf(&body, "#EXTM3U\n#EXT-X-VERSION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n", version)
	fmt.Fprintf(&body, "#EXT-X-TARGETDURATION:%d\n", graph.timeline.TargetDuration)
	if start > 0 {
		fmt.Fprintf(&body, "#EXT-X-START:TIME-OFFSET=%d.%07d,PRECISE=YES\n", start/media.TicksPerSecond, start%media.TicksPerSecond)
	}
	var end int64
	for number, slot := range graph.timeline.Segments {
		if slot.Number != number || slot.StartTicks != end || slot.DurationTicks <= 0 || slot.DurationTicks > graph.endpoint.DurationTicks-end {
			return nil, errInvalidHLSManifest
		}
		logical := hlsGeneratedWindowResource{number: number, variant: variant}
		name := hlsGeneratedWindowResourceName(plan, logical)
		if name == "" {
			return nil, errInvalidHLSManifest
		}
		child := hlsArtifactURL(session, resource, name, token, start) + "&" + hlsGeneratedWindowGraphQuery + "=" + url.QueryEscape(session.id)
		if !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		// Every slot starts a separately closed encoder/mux epoch. Declaring
		// the discontinuity in the immutable VOD avoids later URI rewrites.
		body.WriteString("#EXT-X-DISCONTINUITY\n")
		if plan.HLS.SegmentType == "fmp4" {
			logical.initialization = true
			initialization := hlsGeneratedWindowResourceName(plan, logical)
			if initialization == "" {
				return nil, errInvalidHLSManifest
			}
			initializationURL := hlsArtifactURL(session, resource, initialization, token, start) + "&" + hlsGeneratedWindowGraphQuery + "=" + url.QueryEscape(session.id)
			if !validHLSManifestURL(initializationURL) {
				return nil, errInvalidHLSManifest
			}
			// The map and media share this exact logical slot and rendition.
			// A neighboring slot's initialization cannot supply its epoch.
			fmt.Fprintf(&body, "#EXT-X-MAP:URI=\"%s\"\n", initializationURL)
		}
		fmt.Fprintf(&body, "#EXTINF:%d.%07d,\n%s\n", slot.DurationTicks/media.TicksPerSecond, slot.DurationTicks%media.TicksPerSecond, child)
		end += slot.DurationTicks
		if body.Len() > maxHLSManifestBytes-len("#EXT-X-ENDLIST\n") {
			return nil, errHLSManifestLimit
		}
	}
	if end != graph.endpoint.DurationTicks || len(graph.timeline.Segments) > transcode.MaxPlaylistSegments {
		return nil, errInvalidHLSManifest
	}
	body.WriteString("#EXT-X-ENDLIST\n")
	return []byte(body.String()), nil
}

func (h *hlsRuntime) publishGeneratedWindowGraph(session *hlsSession, graph *hlsGeneratedWindowGraph) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.ctx.Err() != nil || session.windowGraph != graph || graph == nil || graph.fallback {
		return false
	}
	graph.published = true
	return true
}
