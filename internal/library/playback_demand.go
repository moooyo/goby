package library

import "strings"

// Emby progress event hints supplement the existing authoritative report.
// Unknown hints retain their historical behavior and do not create a new
// client sequence or change the report's database-lock processing order.
func playbackReportPaused(event, hint string, paused bool) bool {
	if event != "Progress" {
		return paused
	}
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "pause":
		return true
	case "unpause":
		return false
	case "timeupdate":
		return paused
	default:
		return paused
	}
}
