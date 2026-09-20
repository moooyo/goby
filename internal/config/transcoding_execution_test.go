package config

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestTranscodingExecutionDefaultsPreserveConfiguredThreads(t *testing.T) {
	transcodingConfigEnvironment(t)
	t.Setenv("GOBY_TRANSCODE_THREADS", "4")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transcoding.Execution != transcode.DefaultExecutionOptions(4) {
		t.Fatalf("startup execution defaults lost configured threads: %+v", cfg.Transcoding.Execution)
	}
	legacy := cfg.Transcoding
	legacy.Execution = transcode.ExecutionOptions{}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy direct fixture requires premature execution materialization: %v", err)
	}
	partial := legacy
	partial.Execution.Threads = 4
	if err := partial.Validate(); err == nil || !strings.Contains(err.Error(), "execution options") {
		t.Fatalf("partially specified execution settings bypassed strict validation: %v", err)
	}
}
