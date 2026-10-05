package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/creditsskipper"
)

func creditsVisualTestTool(t *testing.T, operation string) string {
	t.Helper()
	inventory := " ... blackframe V->V\n ... blackdetect V->V\n ... silencedetect A->A\n ... showinfo V->V\n ... format V->V\n ... entropy V->V\n ... signalstats V->V\n ... metadata V->V\n"
	body := "case \"$1\" in\n-version) printf 'ffmpeg version 9.0.1 Copyright\\n';;\n-hide_banner)\n if [ \"$2\" = '-filters' ]; then\ncat <<'FILTERS'\n" + inventory + "FILTERS\nelse\n" + operation + "\nfi;;\nesac"
	return analysisProcessTestTool(t, body)
}

func TestCreditsVisualFailedDecoderCannotPublishExistingChapterCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info := Info{DurationTicks: 40 * TicksPerSecond, Streams: []Stream{{Index: 0, CodecType: "video", Width: 64, Height: 64}}}
	request := CreditsVisualRequest{VideoStreamIndex: 0, AudioStreamIndex: -1, IsMovie: true, Chapters: []creditsskipper.Chapter{{Name: "Credits", StartSeconds: 20}}}
	tool := creditsVisualTestTool(t, "printf '[Parsed_blackframe_0 @ x] frame:0 pblack:100 pts:0 t:0.0 type:I last_keyframe:0\\n' >&2; exit 1")
	result, err := (AnalysisExtractor{FFmpegPath: tool}).ExtractCreditsVisual(context.Background(), file, info, request)
	if err == nil || len(result.Result.Segments) != 0 || result.SourceIdentity != "" {
		t.Fatalf("decoder failure published a partial pass: %+v %v", result, err)
	}
}

func TestCreditsVisualRunningProbeCancellationReturnsNoEvidence(t *testing.T) {
	directory := t.TempDir()
	path, marker := filepath.Join(directory, "source"), filepath.Join(directory, "entered")
	if err := os.WriteFile(path, []byte{1}, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	tool := creditsVisualTestTool(t, "touch '"+strings.ReplaceAll(marker, "'", "'\\''")+"'; sleep 60")
	info := Info{DurationTicks: 40 * TicksPerSecond, Streams: []Stream{{Index: 0, CodecType: "video", Width: 64, Height: 64}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		value CreditsVisualEvidence
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		value, err := (AnalysisExtractor{FFmpegPath: tool}).ExtractCreditsVisual(ctx, file, info, CreditsVisualRequest{VideoStreamIndex: 0, AudioStreamIndex: -1, IsMovie: true})
		done <- outcome{value, err}
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	started := false
	for !started {
		select {
		case <-ticker.C:
			_, err := os.Stat(marker)
			started = err == nil
		case result := <-done:
			t.Fatalf("probe exited before the cancellation gate: %v", result.err)
		case <-deadline.C:
			cancel()
			<-done
			t.Fatal("probe did not start")
		}
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.value.Result.Version != "" || result.value.SourceIdentity != "" {
			t.Fatalf("cancelled probe retained evidence: %+v %v", result.value, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled probe did not join")
	}
}
