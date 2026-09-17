package config

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
)

// MediaDiagnosticsConfig is opt-in process configuration, not an administrator
// request. Its zero value is disabled. Load captures only the explicit loader
// and supported hardware environment at startup, after both resource paths opt in.
type MediaDiagnosticsConfig struct {
	Enabled             bool
	CgroupParent        string
	ScratchDirectory    string
	LoaderDirectories   []string
	HardwareEnvironment map[string]string
}

func loadMediaDiagnostics() (MediaDiagnosticsConfig, error) {
	c := MediaDiagnosticsConfig{
		CgroupParent:     os.Getenv("GOBY_MEDIA_DIAGNOSTICS_CGROUP"),
		ScratchDirectory: os.Getenv("GOBY_MEDIA_DIAGNOSTICS_SCRATCH"),
	}
	if c.CgroupParent == "" && c.ScratchDirectory == "" {
		return c, nil
	}
	if c.CgroupParent == "" || c.ScratchDirectory == "" {
		return MediaDiagnosticsConfig{}, fmt.Errorf("GOBY_MEDIA_DIAGNOSTICS_CGROUP and GOBY_MEDIA_DIAGNOSTICS_SCRATCH must be set together")
	}
	c.Enabled = true
	if loader := os.Getenv("LD_LIBRARY_PATH"); loader != "" {
		if len(loader) > 16*1024+15 {
			return MediaDiagnosticsConfig{}, fmt.Errorf("LD_LIBRARY_PATH exceeds the media diagnostic directory budget")
		}
		c.LoaderDirectories = strings.Split(loader, ":")
	}
	for _, name := range []string{"LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH", "MESA_LOADER_DRIVER_OVERRIDE", "CUDA_VISIBLE_DEVICES"} {
		if value, present := os.LookupEnv(name); present {
			if c.HardwareEnvironment == nil {
				c.HardwareEnvironment = make(map[string]string)
			}
			// An explicit empty device mapping must not silently become an
			// omitted mapping that exposes all devices to a diagnostic child.
			c.HardwareEnvironment[name] = value
		}
	}
	if err := c.Validate(); err != nil {
		return MediaDiagnosticsConfig{}, err
	}
	return c, nil
}

// Validate uses Linux path syntax without opening paths, inspecting cgroups,
// or probing tools/devices. The executor separately admits actual ownership,
// controllers and an empty scratch directory the service cannot modify.
func (c MediaDiagnosticsConfig) Validate() error {
	if !c.Enabled {
		if c.CgroupParent != "" || c.ScratchDirectory != "" || len(c.LoaderDirectories) != 0 || len(c.HardwareEnvironment) != 0 {
			return fmt.Errorf("disabled media diagnostics must not retain execution configuration")
		}
		return nil
	}
	for _, field := range []struct{ name, value string }{
		{"GOBY_MEDIA_DIAGNOSTICS_CGROUP", c.CgroupParent},
		{"GOBY_MEDIA_DIAGNOSTICS_SCRATCH", c.ScratchDirectory},
	} {
		if !mediaDiagnosticDirectory(field.value, 4096) {
			return fmt.Errorf("%s must be a bounded canonical absolute Linux directory other than the filesystem root", field.name)
		}
	}
	if len(c.LoaderDirectories) > 16 {
		return fmt.Errorf("LD_LIBRARY_PATH exceeds the media diagnostic directory budget")
	}
	for _, directory := range c.LoaderDirectories {
		if !mediaDiagnosticDirectory(directory, 1024) || strings.Contains(directory, ":") {
			return fmt.Errorf("LD_LIBRARY_PATH must contain bounded canonical absolute Linux directories without empty entries")
		}
	}
	if len(c.HardwareEnvironment) > 4 {
		return fmt.Errorf("media diagnostic hardware environment exceeds its entry budget")
	}
	// Keep this small startup check aligned with media's closed execution
	// environment; the executor revalidates before creating its process session.
	for name, value := range c.HardwareEnvironment {
		if value == "" || len(value) > 1024 || !utf8.ValidString(value) ||
			strings.Contains(value, "\\") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("media diagnostic hardware environment contains an invalid value")
		}
		switch name {
		case "LIBVA_DRIVER_NAME", "MESA_LOADER_DRIVER_OVERRIDE":
			if len(value) > 64 || strings.IndexFunc(value, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
			}) >= 0 {
				return fmt.Errorf("media diagnostic driver selection must be a bounded driver identifier")
			}
		case "LIBVA_DRIVERS_PATH":
			if !mediaDiagnosticDirectory(value, 1024) || strings.Contains(value, ":") {
				return fmt.Errorf("LIBVA_DRIVERS_PATH must be a bounded canonical absolute Linux directory")
			}
		case "CUDA_VISIBLE_DEVICES":
			if len(value) > 85 {
				return fmt.Errorf("CUDA_VISIBLE_DEVICES exceeds the media diagnostic device budget")
			}
			var seen [32]bool
			for _, field := range strings.Split(value, ",") {
				device, err := strconv.Atoi(field)
				if err != nil || device < 0 || device > 31 || strconv.Itoa(device) != field || seen[device] {
					return fmt.Errorf("CUDA_VISIBLE_DEVICES must contain unique canonical device numbers from 0 through 31")
				}
				seen[device] = true
			}
		default:
			return fmt.Errorf("media diagnostic hardware environment contains an unsupported name")
		}
	}
	return nil
}

func mediaDiagnosticDirectory(value string, maximum int) bool {
	return value != "/" && len(value) <= maximum && utf8.ValidString(value) &&
		path.IsAbs(value) && path.Clean(value) == value && !strings.Contains(value, "\\") &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

// ExecutionOptions copies the captured configuration without consulting the
// current environment or accessing resources. Callers must first Validate and
// check Enabled; options alone do not admit a diagnostic run.
func (c MediaDiagnosticsConfig) ExecutionOptions(ffmpegPath string) media.DiagnosticExecutionOptions {
	options := media.DiagnosticExecutionOptions{
		FFmpegPath: ffmpegPath, CgroupParent: c.CgroupParent, ScratchDirectory: c.ScratchDirectory,
		LoaderDirectories: append([]string(nil), c.LoaderDirectories...),
	}
	if c.HardwareEnvironment != nil {
		options.HardwareEnvironment = make(map[string]string, len(c.HardwareEnvironment))
		for name, value := range c.HardwareEnvironment {
			options.HardwareEnvironment[name] = value
		}
	}
	return options
}
