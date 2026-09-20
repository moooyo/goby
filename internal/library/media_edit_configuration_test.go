package library

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
)

func TestMediaEditExecutionConsumesCapturedProfilesAndBudgets(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "ffmpeg")
	execution := mediaEditExecutionSnapshot{Configuration: config.MediaOperationsConfig{Enabled: true, MaxConcurrent: 1, MaxQueued: 1, MaxRuntimeSeconds: 7, MaxScratchBytes: 4096, ScratchDirectory: "/owned/scratch", WritableProfiles: []string{"matroska-v1"}}, FFmpegPath: tool, FFprobePath: tool, FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64)}
	op := MediaOperation{}
	op.ExecutionSnapshot, _ = json.Marshal(execution)
	_, duration, bytes, err := readMediaEditExecution(op, tool, tool, "mkv")
	if err != nil || duration != 7*time.Second || bytes != 4096 {
		t.Fatalf("captured budget not consumed: duration=%v bytes=%d err=%v", duration, bytes, err)
	}
	if _, _, _, err := readMediaEditExecution(op, tool, tool, "mp4"); err == nil {
		t.Fatal("disabled MP4 profile accepted")
	}
	if _, _, _, err := readMediaEditExecution(op, tool+"changed", tool, "mkv"); err == nil {
		t.Fatal("different executable accepted")
	}
	op.Parameters.Profile = "mp4-movtext-v1"
	if _, _, _, err := readMediaEditExecution(op, tool, tool, "mkv"); err == nil {
		t.Fatal("request profile differs from source")
	}
	op.Parameters.Profile = ""
	execution.Configuration.MaxRuntimeSeconds = 86400
	op.ExecutionSnapshot, _ = json.Marshal(execution)
	_, duration, _, err = readMediaEditExecution(op, tool, tool, "mka")
	if err != nil || duration != 2*time.Hour {
		t.Fatalf("hard runtime cap=%v %v", duration, err)
	}
	execution.Configuration.Enabled = false
	op.ExecutionSnapshot, _ = json.Marshal(execution)
	if _, _, _, err := readMediaEditExecution(op, tool, tool, "mkv"); err == nil {
		t.Fatal("disabled operations executed")
	}
}
