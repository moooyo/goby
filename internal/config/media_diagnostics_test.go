package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func mediaDiagnosticsConfigEnvironment(t *testing.T) {
	t.Helper()
	transcodingConfigEnvironment(t)
	for _, name := range []string{"GOBY_MEDIA_DIAGNOSTICS_CGROUP", "GOBY_MEDIA_DIAGNOSTICS_SCRATCH", "LD_LIBRARY_PATH"} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH", "MESA_LOADER_DRIVER_OVERRIDE", "CUDA_VISIBLE_DEVICES"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func mediaDiagnosticsTestConfig() MediaDiagnosticsConfig {
	return MediaDiagnosticsConfig{Enabled: true,
		CgroupParent: "/unopened-diagnostic-fixture/cgroup", ScratchDirectory: "/unopened-diagnostic-fixture/scratch"}
}

func TestMediaDiagnosticsDefaultsDisabledAndIgnoresUnselectedEnvironment(t *testing.T) {
	mediaDiagnosticsConfigEnvironment(t)
	t.Setenv("LD_LIBRARY_PATH", ".:relative")
	t.Setenv("CUDA_VISIBLE_DEVICES", "")
	t.Setenv("LIBVA_DRIVER_NAME", "../not-selected")
	cfg, err := Load()
	if err != nil || !reflect.DeepEqual(cfg.MediaDiagnostics, MediaDiagnosticsConfig{}) {
		t.Fatalf("default diagnostics must be disabled without captured execution environment: %v", err)
	}
	if err := (MediaDiagnosticsConfig{}).Validate(); err != nil {
		t.Fatalf("zero-value diagnostics configuration was rejected: %v", err)
	}
	cfg.MediaDiagnostics.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_MEDIA_DIAGNOSTICS_CGROUP") {
		t.Fatal("Config.Validate did not enforce media diagnostic prerequisites")
	}
}

func TestMediaDiagnosticsLoadsOnlyPairedResourcePaths(t *testing.T) {
	mediaDiagnosticsConfigEnvironment(t)
	for _, pair := range [][2]string{{"/unopened/cgroup", ""}, {"", "/unopened/scratch"}} {
		t.Setenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP", pair[0])
		t.Setenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH", pair[1])
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_MEDIA_DIAGNOSTICS_") {
			t.Fatal("a single diagnostic resource path enabled execution")
		}
	}
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP", "/unopened/cgroup")
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH", "/unopened/scratch")
	cfg, err := Load()
	if err != nil || !cfg.MediaDiagnostics.Enabled || cfg.MediaDiagnostics.CgroupParent != "/unopened/cgroup" ||
		cfg.MediaDiagnostics.ScratchDirectory != "/unopened/scratch" {
		t.Fatalf("paired syntax-only paths did not enable diagnostics: %v", err)
	}
}

func TestMediaDiagnosticsRejectsUnsafeAndInconsistentPaths(t *testing.T) {
	for _, value := range []string{"", ".", "/", "relative", "//parent/child", "/parent/../child", "/parent/child/",
		`C:\diagnostics`, `/parent\child`, "/parent\x00child", "/parent\nchild", "/parent\tchild", "/parent\x7fchild",
		"/parent\u0085child", "/parent\xffchild", "/" + strings.Repeat("a", 4096)} {
		for _, field := range []string{"cgroup", "scratch"} {
			cfg := mediaDiagnosticsTestConfig()
			if field == "cgroup" {
				cfg.CgroupParent = value
			} else {
				cfg.ScratchDirectory = value
			}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("accepted invalid %s path syntax", field)
			}
		}
	}
	for _, cfg := range []MediaDiagnosticsConfig{
		{CgroupParent: "/cgroup", ScratchDirectory: "/scratch"},
		{LoaderDirectories: []string{"/loader"}},
		{HardwareEnvironment: map[string]string{"LIBVA_DRIVER_NAME": "iHD"}},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatal("disabled diagnostics concealed execution configuration")
		}
	}
}

func TestMediaDiagnosticsCapturesClosedStartupEnvironmentAndCopiesOptions(t *testing.T) {
	mediaDiagnosticsConfigEnvironment(t)
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP", "/unopened/cgroup")
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH", "/unopened/scratch")
	t.Setenv("LD_LIBRARY_PATH", "/opt/ffmpeg/lib:/usr/local/lib")
	wantHardware := map[string]string{"LIBVA_DRIVER_NAME": "iHD", "LIBVA_DRIVERS_PATH": "/opt/drivers",
		"MESA_LOADER_DRIVER_OVERRIDE": "iris", "CUDA_VISIBLE_DEVICES": "1,0"}
	for name, value := range wantHardware {
		t.Setenv(name, value)
	}
	t.Setenv("LD_PRELOAD", "/not-inherited/preload")
	t.Setenv("FFREPORT", "file=not-inherited")
	t.Setenv("HOME", "/not-inherited/home")
	cfg, err := Load()
	if err != nil || !reflect.DeepEqual(cfg.MediaDiagnostics.HardwareEnvironment, wantHardware) ||
		!reflect.DeepEqual(cfg.MediaDiagnostics.LoaderDirectories, []string{"/opt/ffmpeg/lib", "/usr/local/lib"}) {
		t.Fatalf("startup environment was not captured exactly: %v", err)
	}
	t.Setenv("LD_LIBRARY_PATH", "/later/loader")
	t.Setenv("LIBVA_DRIVER_NAME", "later_driver")
	first := cfg.MediaDiagnostics.ExecutionOptions("/opt/ffmpeg/bin/ffmpeg")
	second := cfg.MediaDiagnostics.ExecutionOptions("/opt/ffmpeg/bin/ffmpeg")
	if first.FFmpegPath != "/opt/ffmpeg/bin/ffmpeg" || first.CgroupParent != "/unopened/cgroup" ||
		first.ScratchDirectory != "/unopened/scratch" || !reflect.DeepEqual(first.HardwareEnvironment, wantHardware) ||
		!reflect.DeepEqual(first.LoaderDirectories, []string{"/opt/ffmpeg/lib", "/usr/local/lib"}) {
		t.Fatal("execution options consulted later environment or lost explicit resource paths")
	}
	first.LoaderDirectories[0] = "/changed/first"
	first.HardwareEnvironment["LIBVA_DRIVER_NAME"] = "changed_first"
	cfg.MediaDiagnostics.LoaderDirectories[1] = "/changed/config"
	cfg.MediaDiagnostics.HardwareEnvironment["CUDA_VISIBLE_DEVICES"] = "2"
	if second.LoaderDirectories[0] != "/opt/ffmpeg/lib" || second.LoaderDirectories[1] != "/usr/local/lib" ||
		!reflect.DeepEqual(second.HardwareEnvironment, wantHardware) || cfg.MediaDiagnostics.LoaderDirectories[0] != "/opt/ffmpeg/lib" ||
		cfg.MediaDiagnostics.HardwareEnvironment["LIBVA_DRIVER_NAME"] != "iHD" {
		t.Fatal("execution options share mutable map or slice storage with another owner")
	}
}

func TestMediaDiagnosticsRejectsMalformedLoaderEnvironment(t *testing.T) {
	mediaDiagnosticsConfigEnvironment(t)
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP", "/unopened/cgroup")
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH", "/unopened/scratch")
	for index, value := range []string{":/lib", "/lib:", "/lib::/other", ".", "/", "/lib/../other", "/lib/", `/lib\child`,
		"/lib\tchild", "/" + strings.Repeat("a", 1024), strings.Repeat("/lib:", 16) + "/lib"} {
		t.Run(fmt.Sprintf("loader_%d", index), func(t *testing.T) {
			t.Setenv("LD_LIBRARY_PATH", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LD_LIBRARY_PATH") {
				t.Fatal("malformed loader environment was accepted")
			}
		})
	}
	for _, value := range []string{"/lib\x00child", "/lib\u0085child", "/lib\xffchild"} {
		cfg := mediaDiagnosticsTestConfig()
		cfg.LoaderDirectories = []string{value}
		if err := cfg.Validate(); err == nil {
			t.Fatal("loader path accepted non-environment-safe bytes")
		}
	}
}

func TestMediaDiagnosticsRejectsInvalidHardwareEnvironmentWithoutValueDisclosure(t *testing.T) {
	mediaDiagnosticsConfigEnvironment(t)
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP", "/unopened/cgroup")
	t.Setenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH", "/unopened/scratch")
	for _, test := range []struct {
		name   string
		values []string
	}{
		{"LIBVA_DRIVER_NAME", []string{"", "../private-marker", "name with spaces", strings.Repeat("x", 65)}},
		{"MESA_LOADER_DRIVER_OVERRIDE", []string{"", "../private-marker", "name\nprivate-marker"}},
		{"LIBVA_DRIVERS_PATH", []string{"", ".", "/", "/private-marker/../driver", "/driver:", `/driver\private-marker`, "/" + strings.Repeat("x", 1024)}},
		{"CUDA_VISIBLE_DEVICES", []string{"", "-1", "32", "00", "1,1", "0,", ",0", "0,,1", " 0", "+1", "GPU-private-marker", strings.Repeat("0,", 43)}},
	} {
		for index, value := range test.values {
			t.Run(fmt.Sprintf("%s/%d", test.name, index), func(t *testing.T) {
				t.Setenv(test.name, value)
				if _, err := Load(); err == nil || strings.Contains(err.Error(), "private-marker") {
					t.Fatal("invalid hardware environment was accepted or disclosed its value")
				}
			})
		}
	}
	for _, environment := range []map[string]string{
		{"PRIVATE_UNSUPPORTED_NAME": "private-marker"},
		{"LD_PRELOAD": "/private-marker"},
		{"LIBVA_DRIVER_NAME": "private\x00marker"},
		{"LIBVA_DRIVERS_PATH": "/private\u0085marker"},
		{"LIBVA_DRIVERS_PATH": "/private\xffmarker"},
	} {
		cfg := mediaDiagnosticsTestConfig()
		cfg.HardwareEnvironment = environment
		if err := cfg.Validate(); err == nil || strings.Contains(err.Error(), "private-marker") || strings.Contains(err.Error(), "PRIVATE_UNSUPPORTED_NAME") {
			t.Fatal("direct hardware configuration bypassed the closed environment policy")
		}
	}
}
