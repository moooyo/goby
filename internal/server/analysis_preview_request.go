package server

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/library"
)

type analysisPreviewRequest struct {
	width         int
	sourceID      string
	positionTicks int64
	tag           string
	maxWidth      int
	quality       int
}

func analysisPreviewWidth(width int) bool { return width == 240 || width == 320 || width == 400 }

func analysisPreviewOpaque(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0 && !strings.ContainsAny(value, "/\\") && strings.TrimSpace(value) == value
}

func parseAnalysisPreviewRequest(r *http.Request, image bool) (analysisPreviewRequest, error) {
	var request analysisPreviewRequest
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return request, library.ErrInvalidInput
	}
	values, err := streamValues(r)
	if err != nil {
		return request, library.ErrInvalidInput
	}
	for key, value := range values {
		switch key {
		case "api_key", "x-emby-token", "x-emby-client", "x-emby-client-version", "x-emby-device-id", "x-emby-device-name":
			// The authentication middleware has already checked all carriers.
		case "mediasourceid":
			if !analysisPreviewOpaque(value, 256) {
				return request, library.ErrInvalidInput
			}
			request.sourceID = value
		case "width":
			width, err := strconv.Atoi(value)
			if image || err != nil || !analysisPreviewWidth(width) || strconv.Itoa(width) != value {
				return request, library.ErrInvalidInput
			}
			request.width = width
		case "positionticks":
			position, err := strconv.ParseInt(value, 10, 64)
			if !image || err != nil || position < 0 || strconv.FormatInt(position, 10) != value {
				return request, library.ErrInvalidInput
			}
			request.positionTicks = position
		case "tag":
			if !image {
				return request, library.ErrInvalidInput
			}
			width, ok := analysisPreviewTagWidth(value)
			if !ok {
				return request, library.ErrInvalidInput
			}
			request.tag, request.width = value, width
		case "maxwidth", "quality":
			limit := 4096
			if key == "quality" {
				limit = 100
			}
			number, err := strconv.Atoi(value)
			if !image || err != nil || number < 0 || number > limit || strconv.Itoa(number) != value {
				return request, library.ErrInvalidInput
			}
			if key == "maxwidth" {
				request.maxWidth = number
			} else {
				request.quality = number
			}
		default:
			return request, library.ErrInvalidInput
		}
	}
	if _, present := values["positionticks"]; image && !present || !image && !analysisPreviewWidth(request.width) {
		return request, library.ErrInvalidInput
	}
	return request, nil
}

func analysisPreviewDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func analysisPreviewTagWidth(tag string) (int, bool) {
	rest, ok := strings.CutPrefix(tag, "goby-preview-")
	if !ok {
		return 0, false
	}
	widthText, digest, ok := strings.Cut(rest, "-")
	width, err := strconv.Atoi(widthText)
	return width, ok && err == nil && strconv.Itoa(width) == widthText && analysisPreviewWidth(width) && analysisPreviewDigest(digest)
}
