package server

import (
	"net/url"
	"strconv"

	"github.com/moooyo/goby/internal/transcode"
)

// audioPlaybackURL serializes the constructible output, including exact encoder
// settings, rather than the client's preferences. Source measurements remain in
// the authorized catalog and are never accepted from query parameters. A client
// can change StartTimeTicks without retaining a server-private conversion ID.
func audioPlaybackURL(itemID, sourceID, playID, deviceID, token string, plan transcode.Plan) string {
	values := url.Values{
		"DeviceId": {deviceID}, "MediaSourceId": {sourceID}, "PlaySessionId": {playID}, "api_key": {token},
		"Static": {"false"}, "StartTimeTicks": {strconv.FormatInt(plan.StartTicks, 10)},
		"AudioStreamIndex": {strconv.Itoa(plan.AudioStreamIndex)}, "AudioCodec": {plan.AudioCodec},
		"AllowAudioStreamCopy": {strconv.FormatBool(plan.AudioCodec == "copy")},
	}
	if plan.AudioCodec != "copy" {
		if plan.AudioBitrate > 0 {
			values.Set("AudioBitrate", strconv.FormatInt(plan.AudioBitrate, 10))
		}
		values.Set("AudioChannels", strconv.Itoa(plan.AudioChannels))
		values.Set("AudioSampleRate", strconv.Itoa(plan.AudioSampleRate))
		if plan.AudioCodec == "flac" {
			values.Set("AudioBitDepth", strconv.Itoa(plan.AudioBitDepth))
		}
	}
	return "/emby/Audio/" + url.PathEscape(itemID) + "/stream." + plan.Container + "?" + values.Encode()
}
