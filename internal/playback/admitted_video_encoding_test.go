package playback

import "testing"

func TestAdmittedVideoEncodingKeepsOnlyTheOfferedSDRorHDRTuple(t *testing.T) {
	profile := videoProfilesTestProfile("hls", "ts,mp4")
	profile.VideoCodec, profile.AudioCodec = "h264,hevc", "aac"
	candidates := conversionEncodingCandidates(profile, DlnaProfileTypeVideo, false, true)
	for _, admitted := range []AdmittedVideoEncoding{
		{Codec: "h264", Profile: "high", BitDepth: 8, Container: "mp4", Width: 746, Height: 420},
		{Codec: "hevc", Profile: "main10", BitDepth: 10, Container: "mp4", VideoRange: "HDR10", Width: 1280, Height: 720},
	} {
		if err := validateAdmittedVideoEncoding(admitted); err != nil {
			t.Fatalf("a valid retained output tuple was rejected: %+v, %v", admitted, err)
		}
		selected := conversionAdmissionCandidates(candidates, ConversionLimits{AdmittedVideoEncoding: admitted})
		if len(selected) != 1 || selected[0].profile.Container != admitted.Container || selected[0].video.codec != admitted.Codec ||
			selected[0].video.profile != admitted.Profile || selected[0].video.bitDepth != admitted.BitDepth || selected[0].video.videoRange != admitted.VideoRange {
			t.Fatalf("retained tuple selected another codec, profile, depth, range, or container: %+v", selected)
		}
	}
	admitted := AdmittedVideoEncoding{Codec: "av1", Profile: "main", BitDepth: 8, Container: "mp4", Width: 1280, Height: 720}
	if selected := conversionAdmissionCandidates(candidates, ConversionLimits{AdmittedVideoEncoding: admitted}); len(selected) != 0 {
		t.Fatal("retained state introduced a video encoder the client did not offer")
	}
}
