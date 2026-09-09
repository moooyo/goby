package config

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

func transcodingConfigEnvironment(t *testing.T) {
	t.Helper()
	startupConfigEnvironment(t)
	for _, name := range []string{
		"GOBY_STARTUP_TIMEOUT", "GOBY_TRANSCODING_ENABLED", "GOBY_TRANSCODE_CACHE",
		"GOBY_TRANSCODE_THREADS", "GOBY_TRANSCODE_MAX_JOBS", "GOBY_TRANSCODE_MAX_USER_JOBS",
		"GOBY_TRANSCODE_MAX_SESSION_JOBS", "GOBY_TRANSCODE_MAX_QUEUE_JOBS", "GOBY_TRANSCODE_MAX_RETAINED_JOBS",
		"GOBY_TRANSCODE_MAX_CACHE_BYTES", "GOBY_TRANSCODE_MAX_JOB_BYTES", "GOBY_TRANSCODE_MIN_FREE_BYTES",
		"GOBY_TRANSCODE_MAX_BITRATE", "GOBY_TRANSCODE_MAX_WIDTH", "GOBY_TRANSCODE_MAX_HEIGHT",
		"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS", "GOBY_HW_DECODER", "GOBY_HW_ENCODER", "GOBY_HW_DEVICE",
	} {
		t.Setenv(name, "")
	}
}

func TestTranscodingEnvironmentDefaultsAndExplicitDisable(t *testing.T) {
	transcodingConfigEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Transcoding
	if !c.Enabled || c.CacheDirectory != "/var/cache/goby/transcodes" ||
		c.Threads != 2 || c.MaxJobs != 2 || c.MaxUserJobs != 1 || c.MaxSessionJobs != 1 ||
		c.MaxQueueJobs != 16 || c.MaxRetainedJobs != 128 || c.MaxCacheBytes != 20<<30 ||
		c.MaxJobBytes != 8<<30 || c.MinFreeBytes != 512<<20 || c.MaxBitrate != 20_000_000 ||
		c.MaxWidth != 1920 || c.MaxHeight != 1080 || c.MaxAudioChannels != 8 ||
		c.Hardware != (transcode.Hardware{Decode: "software", Encode: "software"}) {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	t.Setenv("GOBY_TRANSCODING_ENABLED", "false")
	disabled, err := Load()
	if err != nil || disabled.Transcoding.Enabled {
		t.Fatalf("disable conversion: enabled=%v, error=%v", disabled.Transcoding.Enabled, err)
	}
	disabled.Transcoding.Enabled = true
	if disabled.Transcoding != c {
		t.Fatal("disabling conversion discarded configured limits")
	}
	base := Config{
		DatabaseURL: "postgres://goby:fixture@localhost:5432/goby", PublicURL: "http://localhost:8096",
		ServerName: "Goby", StartupTimeout: time.Minute,
	}
	if err := base.Validate(); err != nil || base.Transcoding.Enabled {
		t.Fatalf("direct configuration must retain opt-in behavior: %v", err)
	}
	base.Transcoding.Enabled = true
	if err := base.Validate(); err == nil {
		t.Fatal("direct enabled configuration accepted missing resource limits")
	}
}

func TestTranscodingEnvironmentRejectsMalformedAndOutOfRangeSettings(t *testing.T) {
	transcodingConfigEnvironment(t)
	for _, test := range []struct {
		name   string
		values []string
	}{
		{"GOBY_TRANSCODING_ENABLED", []string{"yes", "enabled", " true "}},
		{"GOBY_TRANSCODE_CACHE", []string{".", "relative/cache", "/", "/var/cache/../transcodes", "/cache/", "//cache", `C:\cache`, "/cache\\child", "/cache\nchild", strings.Repeat("/a", 2049)}},
		{"GOBY_TRANSCODE_THREADS", []string{"0", "-1", "65", "2.0", " 2 ", "9223372036854775808"}},
		{"GOBY_TRANSCODE_MAX_JOBS", []string{"0", "65"}},
		{"GOBY_TRANSCODE_MAX_USER_JOBS", []string{"0", "3"}},
		{"GOBY_TRANSCODE_MAX_SESSION_JOBS", []string{"0", "3"}},
		{"GOBY_TRANSCODE_MAX_QUEUE_JOBS", []string{"0", "1025"}},
		{"GOBY_TRANSCODE_MAX_RETAINED_JOBS", []string{"1", "4097"}},
		{"GOBY_TRANSCODE_MAX_CACHE_BYTES", []string{"0", "-1", "8GiB", "1125899906842625", "9223372036854775808"}},
		{"GOBY_TRANSCODE_MAX_JOB_BYTES", []string{"0", "21474836481"}},
		{"GOBY_TRANSCODE_MIN_FREE_BYTES", []string{"0", "-1", "1125899906842625"}},
		{"GOBY_TRANSCODE_MAX_BITRATE", []string{"0", "1000000001"}},
		{"GOBY_TRANSCODE_MAX_WIDTH", []string{"0", "8193"}},
		{"GOBY_TRANSCODE_MAX_HEIGHT", []string{"0", "8193"}},
		{"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS", []string{"0", "9"}},
	} {
		for index, value := range test.values {
			t.Run(fmt.Sprintf("%s/%d", test.name, index), func(t *testing.T) {
				t.Setenv(test.name, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), test.name) {
					t.Fatalf("configuration error = %v, want named setting %s", err, test.name)
				}
			})
		}
	}
	t.Setenv("GOBY_TRANSCODING_ENABLED", "false")
	t.Setenv("GOBY_TRANSCODE_THREADS", "-1")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_TRANSCODE_THREADS") {
		t.Fatalf("disabled conversion concealed invalid explicit settings: %v", err)
	}
}

func TestTranscodingEnvironmentHardwareSelections(t *testing.T) {
	transcodingConfigEnvironment(t)
	for _, test := range []struct {
		decode string
		encode string
		device string
		valid  bool
	}{
		{"software", "software", "", true},
		{"software", "vaapi", "/dev/dri/renderD128", true},
		{"vaapi", "software", "/dev/dri/renderD255", true},
		{"vaapi", "vaapi", "", true},
		{"software", "qsv", "/dev/dri/renderD128", true},
		{"qsv", "qsv", "/dev/dri/renderD129", true},
		{"qsv", "software", "", true},
		{"cuda", "software", "31", true},
		{"software", "nvenc", "0", true},
		{"cuda", "nvenc", "", true},
		{"vaapi", "nvenc", "", false},
		{"cuda", "qsv", "", false},
		{"qsv", "vaapi", "", false},
		{"nvenc", "software", "", false},
		{"software", "cuda", "", false},
		{"auto", "software", "", false},
		{"software", "software", "/dev/dri/renderD128", false},
		{"cuda", "nvenc", "32", false},
		{"cuda", "nvenc", "00", false},
		{"vaapi", "vaapi", "/dev/dri/renderD127", false},
		{"vaapi", "vaapi", "/dev/dri/renderD128/../renderD129", false},
	} {
		t.Run(test.decode+"/"+test.encode+"/"+test.device, func(t *testing.T) {
			t.Setenv("GOBY_HW_DECODER", test.decode)
			t.Setenv("GOBY_HW_ENCODER", test.encode)
			t.Setenv("GOBY_HW_DEVICE", test.device)
			_, err := Load()
			if test.valid && err != nil || !test.valid && (err == nil || !strings.Contains(err.Error(), "GOBY_HW_")) {
				t.Fatalf("hardware validity %v returned %v", test.valid, err)
			}
		})
	}
}

func TestTranscodingEnvironmentPreservesExplicitManagerAndPlannerLimits(t *testing.T) {
	transcodingConfigEnvironment(t)
	for name, value := range map[string]string{
		"GOBY_TRANSCODE_CACHE":   "/dev/shm/goby-config-test",
		"GOBY_TRANSCODE_THREADS": "4", "GOBY_TRANSCODE_MAX_JOBS": "8",
		"GOBY_TRANSCODE_MAX_USER_JOBS": "3", "GOBY_TRANSCODE_MAX_SESSION_JOBS": "2",
		"GOBY_TRANSCODE_MAX_QUEUE_JOBS": "24", "GOBY_TRANSCODE_MAX_RETAINED_JOBS": "256",
		"GOBY_TRANSCODE_MAX_CACHE_BYTES": "134217728", "GOBY_TRANSCODE_MAX_JOB_BYTES": "33554432",
		"GOBY_TRANSCODE_MIN_FREE_BYTES": "16777216", "GOBY_TRANSCODE_MAX_BITRATE": "900000",
		"GOBY_TRANSCODE_MAX_WIDTH": "640", "GOBY_TRANSCODE_MAX_HEIGHT": "480",
		"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS": "1", "GOBY_HW_DECODER": "cuda",
		"GOBY_HW_ENCODER": "nvenc", "GOBY_HW_DEVICE": "1",
	} {
		t.Setenv(name, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Transcoding
	repository := transcode.NewRepository(nil)
	options := c.ManagerOptions("/opt/ffmpeg/bin/ffmpeg", repository)
	if options.Root != "/dev/shm/goby-config-test" || options.FFmpegPath != "/opt/ffmpeg/bin/ffmpeg" ||
		options.Repository != repository || options.Threads != 4 || options.MaxJobs != 8 ||
		options.MaxUserJobs != 3 || options.MaxSessionJobs != 2 || options.MaxQueueJobs != 24 ||
		options.MaxRetainedJobs != 256 || options.MaxBytes != 128<<20 || options.MaxJobBytes != 32<<20 ||
		options.MinFreeBytes != 16<<20 {
		t.Fatalf("explicit manager policy was altered: %+v", options)
	}
	if c.MaxBitrate != 900_000 || c.MaxWidth != 640 || c.MaxHeight != 480 || c.MaxAudioChannels != 1 ||
		c.Hardware != (transcode.Hardware{Decode: "cuda", Encode: "nvenc", Device: "1"}) {
		t.Fatalf("explicit planner policy was altered: %+v", c)
	}
}
