package media

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func videoCopySeekAudioTestCandidate(t *testing.T) VideoCopySeekCandidate {
	t.Helper()
	_, candidate := videoCopySeekTestCandidate(t)
	audio := VideoCopySeekAudio{StreamIndex: 1, Codec: "aac", TimeBaseNumerator: 1, TimeBaseDenominator: 48000,
		SampleRate: 48000, Channels: 1, PTS: 96000, Duration: 1024, PacketSHA256: strings.Repeat("9", 64)}
	candidate.Index.Entries[0].Audio = []VideoCopySeekAudio{audio}
	candidate.Audio = &audio
	return candidate
}

func videoCopySeekAudioTestHash(candidate VideoCopySeekCandidate) string {
	a := candidate.Audio
	timestamp, _ := videoCopySeekOutputTimestamp(candidate, videoSeekTimeBase(a.TimeBaseNumerator, a.TimeBaseDenominator))
	return fmt.Sprintf("#format: frame checksums\n#version: 2\n#hash: SHA256\n#tb 0: %d/%d\n#media_type 0: audio\n#codec_id 0: aac\n#sample_rate 0: %d\n#channel_layout_name 0: mono\n0, %d, %d, %d, 235, %s\n",
		a.TimeBaseNumerator, a.TimeBaseDenominator, a.SampleRate, timestamp, timestamp, a.Duration, a.PacketSHA256)
}

func TestVideoCopySeekAudioProofRetainsExactIndependentPacketRequirements(t *testing.T) {
	candidate := videoCopySeekAudioTestCandidate(t)
	valid := videoCopySeekAudioTestHash(candidate)
	if err := parseVideoCopySeekAudioProof(strings.NewReader(valid), candidate); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"wrong codec":          strings.Replace(valid, "codec_id 0: aac", "codec_id 0: mp3", 1),
		"wrong clock":          strings.Replace(valid, "#tb 0: 1/48000", "#tb 0: 1/44100", 1),
		"wrong packet":         strings.Replace(valid, candidate.Audio.PacketSHA256, strings.Repeat("8", 64), 1),
		"different duration":   strings.Replace(valid, "0, 0, 0, 1024,", "0, 0, 0, 960,", 1),
		"presentation preroll": strings.Replace(valid, "0, 0, 0, 1024,", "0, 0, -1024, 1024,", 1),
		"decode preroll":       strings.Replace(valid, "0, 0, 0, 1024,", "0, -1024, 0, 1024,", 1),
		"side data":            strings.TrimSuffix(valid, "\n") + ", S=1, 10, " + strings.Repeat("0", 64) + "\n",
		"missing packet":       valid[:strings.LastIndex(valid, "\n0,")+1],
		"extra packet":         valid + "0, 1024, 1024, 1024, 235, " + candidate.Audio.PacketSHA256 + "\n",
		"truncation":           strings.TrimSuffix(valid, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := parseVideoCopySeekAudioProof(strings.NewReader(output), candidate); err == nil {
				t.Fatal("unproven audio packet authorized a restart")
			}
		})
	}
	candidate.CopyTimestamps = true
	if err := parseVideoCopySeekAudioProof(strings.NewReader(valid), candidate); err == nil {
		t.Fatal("source-global audio accepted a zero-normalized packet")
	}
	if err := parseVideoCopySeekAudioProof(strings.NewReader(videoCopySeekAudioTestHash(candidate)), candidate); err != nil {
		t.Fatal(err)
	}
}

func TestVideoCopySeekProofsShareSeekArgumentsWithoutCompetingFrameLimits(t *testing.T) {
	candidate := videoCopySeekAudioTestCandidate(t)
	candidate.CopyTimestamps = true
	data, _ := json.Marshal(candidate)
	video, err := BuildVideoCopySeekCommandArgs(string(data), 2)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := BuildVideoCopySeekAudioCommandArgs(string(data), 2)
	if err != nil {
		t.Fatal(err)
	}
	videoBoundary, audioBoundary := slices.Index(video, "-map"), slices.Index(audio, "-map")
	if videoBoundary < 0 || audioBoundary < 0 || !slices.Equal(video[:videoBoundary], audio[:audioBoundary]) {
		t.Fatal("separate packet proofs changed the production input or trim clock")
	}
	if slices.Contains(video, "-frames:a") || slices.Contains(audio, "-frames:v:0") || !strings.Contains(strings.Join(audio, " "), "-map 0:1") ||
		!strings.Contains(strings.Join(video, " "), "-output_ts_offset 2.0000000") || !strings.Contains(strings.Join(audio, " "), "-output_ts_offset 2.0000000") {
		t.Fatalf("proofs retained competing stream limits or lost the output clock: video=%v audio=%v", video, audio)
	}
}
