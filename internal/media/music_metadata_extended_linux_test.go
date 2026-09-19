//go:build linux

package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMusicMetadataActualFLACTagsPreserveExplicitCreditsAndNumbering(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual music tag verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "credits.flac")
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=1", "-c:a", "flac", "-threads:a", "1",
		"-metadata", "title=Tagged title", "-metadata", "album=Tagged album", "-metadata", "artist=Display A & B; C / D",
		"-metadata", `artists=["Solo A","Solo B"]`, "-metadata", "album_artist=Ensemble", "-metadata", "composers=Composer One;Composer Two",
		"-metadata", "genres=Classical;Live", "-metadata", "track=3/12", "-metadata", "disc=2/3", "-metadata", "date=2024-02-29",
		"-metadata", "musicbrainz_trackid=abcdef01-2345-6789-abcd-0123456789ab", path)
	if output, err := command.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("author music fixture: %v %s", err, output)
	}
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 15 * time.Second}).Probe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	facts := info.EmbeddedMusic
	if facts == nil || facts.Version != CurrentMusicMetadataVersion || facts.Artist != "Display A & B; C / D" ||
		!reflect.DeepEqual(facts.Artists, []string{"Solo A", "Solo B"}) || !reflect.DeepEqual(facts.Composers, []string{"Composer One", "Composer Two"}) ||
		facts.TrackNumber != 3 || facts.DiscNumber != 2 || facts.TrackTotal != 12 || facts.DiscTotal != 3 || facts.Date != "2024-02-29" ||
		facts.ProviderIDs["MusicBrainzRecording"] != "abcdef01-2345-6789-abcd-0123456789ab" {
		t.Fatalf("actual local tags were lost or reinterpreted: %+v", facts)
	}
}
