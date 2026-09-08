package media

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	capabilityProbeTimeout = 10 * time.Second
	capabilityOutputLimit  = 1 << 20
)

// Capabilities describes interfaces compiled into the configured FFmpeg binary.
// Hardware availability, driver compatibility, and media support require real
// decode and encode jobs on the deployment host.
type Capabilities struct {
	Version              string
	HardwareAccelerators []string
	Decoders             []string
	Encoders             []string
	Filters              []string
	HardwareVerified     bool
}

// ProbeCapabilities enumerates compiled support without opening media or GPU
// devices. HardwareVerified is always false, including when hardware codecs are
// listed successfully. Each command has a timeout and bounded output.
func ProbeCapabilities(ctx context.Context, ffmpegPath string) (Capabilities, error) {
	if strings.TrimSpace(ffmpegPath) == "" {
		return Capabilities{}, fmt.Errorf("FFmpeg executable path is empty")
	}
	var capabilities Capabilities
	queries := []struct {
		option string
		parse  func([]byte) error
	}{
		{"-version", func(output []byte) (err error) {
			capabilities.Version, err = parseFFmpegVersion(output)
			return err
		}},
		{"-hwaccels", func(output []byte) (err error) {
			capabilities.HardwareAccelerators, err = parseHardwareAccelerators(output)
			return err
		}},
		{"-decoders", func(output []byte) (err error) {
			capabilities.Decoders, err = parseCodecList(output, "Decoders")
			return err
		}},
		{"-encoders", func(output []byte) (err error) {
			capabilities.Encoders, err = parseCodecList(output, "Encoders")
			return err
		}},
		{"-filters", func(output []byte) (err error) {
			capabilities.Filters, err = parseFilterList(output)
			return err
		}},
	}
	for _, query := range queries {
		output, err := runLimited(ctx, capabilityProbeTimeout, capabilityOutputLimit, ffmpegPath, "-hide_banner", query.option)
		if err != nil {
			return Capabilities{}, fmt.Errorf("probe FFmpeg %s: %w", query.option, err)
		}
		if err := query.parse(output); err != nil {
			return Capabilities{}, fmt.Errorf("parse FFmpeg %s: %w", query.option, err)
		}
	}
	return capabilities, nil
}

func parseFFmpegVersion(output []byte) (string, error) {
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "ffmpeg" && fields[1] == "version" {
			return fields[2], nil
		}
	}
	return "", fmt.Errorf("version header is missing")
}

func parseHardwareAccelerators(output []byte) ([]string, error) {
	var found bool
	names := make([]string, 0)
	seen := make(map[string]bool)
	for index, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !found {
			found = line == "Hardware acceleration methods:"
			continue
		}
		if line == "" {
			continue
		}
		if !isCapabilityName(line) {
			return nil, fmt.Errorf("invalid hardware accelerator on line %d", index+1)
		}
		if !seen[line] {
			names = append(names, line)
			seen[line] = true
		}
	}
	if !found {
		return nil, fmt.Errorf("hardware acceleration header is missing")
	}
	return names, nil
}

func parseCodecList(output []byte, heading string) ([]string, error) {
	return parseCapabilityTable(output, heading, false)
}

func parseFilterList(output []byte) ([]string, error) {
	if strings.TrimSpace(string(output)) == "No filters available: libavfilter disabled" {
		return []string{}, nil
	}
	return parseCapabilityTable(output, "Filters", true)
}

func parseCapabilityTable(output []byte, heading string, filters bool) ([]string, error) {
	var foundHeader, foundSeparator bool
	names := make([]string, 0)
	seen := make(map[string]bool)
	for index, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !foundHeader {
			foundHeader = line == heading+":"
			continue
		}
		if !foundSeparator {
			foundSeparator = line == "------"
			continue
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		valid := len(fields) >= 2
		if valid && filters {
			valid = len(fields) >= 3 && isFilterFlags(fields[0]) && isFilterIO(fields[2])
		} else if valid {
			valid = isCodecFlags(fields[0])
		}
		if !valid || !isCapabilityName(fields[1]) {
			return nil, fmt.Errorf("invalid %s row on line %d", strings.ToLower(heading), index+1)
		}
		name := fields[1]
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	if !foundHeader || !foundSeparator {
		return nil, fmt.Errorf("%s table header or separator is missing", strings.ToLower(heading))
	}
	return names, nil
}

func isCodecFlags(flags string) bool {
	return len(flags) == 6 && strings.ContainsRune("VASDT?", rune(flags[0])) &&
		(flags[1] == '.' || flags[1] == 'F') &&
		(flags[2] == '.' || flags[2] == 'S') &&
		(flags[3] == '.' || flags[3] == 'X') &&
		(flags[4] == '.' || flags[4] == 'B') &&
		(flags[5] == '.' || flags[5] == 'D')
}

func isFilterFlags(flags string) bool {
	// FFmpeg 9 emits two flags; earlier versions included a command-support flag.
	return (len(flags) == 2 || len(flags) == 3 && (flags[2] == '.' || flags[2] == 'C')) &&
		(flags[0] == '.' || flags[0] == 'T') &&
		(flags[1] == '.' || flags[1] == 'S')
}

func isFilterIO(value string) bool {
	input, output, found := strings.Cut(value, "->")
	if !found || input == "" || output == "" {
		return false
	}
	for _, side := range []string{input, output} {
		for _, kind := range side {
			if !strings.ContainsRune("AVN|", kind) {
				return false
			}
		}
	}
	return true
}

func isCapabilityName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}
