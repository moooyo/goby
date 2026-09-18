package identity

import "slices"

// These are Goby's installed user features. They are discovered through the
// FeatureService contract; they are not claimed to be Emby's built-in IDs.
const (
	FeaturePlayback           = "goby_playback"
	FeatureDownloads          = "goby_downloads"
	FeaturePlaylists          = "goby_playlists"
	FeatureCollections        = "goby_collections"
	FeatureSubtitleDownloads  = "goby_subtitle_downloads"
	FeatureSubtitleManagement = "goby_subtitle_management"
	FeatureRemoteControl      = "goby_remote_control"
	FeaturePreferences        = "goby_preferences"
)

type FeatureInfo struct {
	ID          string `json:"Id"`
	Name        string `json:"Name"`
	FeatureType string `json:"FeatureType"`
}

// UserFeatures returns a fresh descriptor slice so callers cannot mutate the
// authorization registry through a discovery response.
func UserFeatures() []FeatureInfo {
	return []FeatureInfo{
		{FeaturePlayback, "Play media", "User"},
		{FeatureDownloads, "Download original media", "User"},
		{FeaturePlaylists, "Access and manage playlists", "User"},
		{FeatureCollections, "Access and manage collections", "User"},
		{FeatureSubtitleDownloads, "Download subtitles from providers", "User"},
		{FeatureSubtitleManagement, "Manage external subtitles", "User"},
		{FeatureRemoteControl, "Control player sessions", "User"},
		{FeaturePreferences, "Manage personal preferences", "User"},
	}
}

func IsUserFeature(id string) bool {
	return slices.ContainsFunc(UserFeatures(), func(feature FeatureInfo) bool { return feature.ID == id })
}

// AllowsFeature is an additional deny-only gate. It never grants an operation
// denied by role, ownership, device, library, or another explicit policy flag.
// Unknown retained IDs are inert until a corresponding feature is installed.
func (policy ManagedPolicy) AllowsFeature(id string) bool {
	return IsUserFeature(id) && !slices.Contains(policy.RestrictedFeatures, id)
}
