package transcode

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// CanonicalX264Options accepts only bounded encoder analysis and quality
// parameters. File access, output format, rate control, profiles, threads and
// GOP scheduling remain owned by the validated execution plan. In particular,
// libx264's warning-only handling of unknown parameters is not validation.
func CanonicalX264Options(value string) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("%w: x264 options", ErrInvalidExecution)
	}
	if value == "" {
		return "", nil
	}
	if len(value) > 512 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "'\"\\\r\n\t ") {
		return invalid()
	}
	items := strings.Split(value, ":")
	if len(items) > 7 {
		return invalid()
	}
	seen := make(map[string]bool, len(items))
	canonical := make([]string, 0, len(items))
	for _, item := range items {
		key, setting, found := strings.Cut(item, "=")
		if !found || key == "" || setting == "" || strings.Contains(setting, "=") {
			return invalid()
		}
		key = strings.ReplaceAll(key, "_", "-")
		if key == "merange" {
			key = "me-range"
		}
		if seen[key] {
			return invalid()
		}
		seen[key] = true
		integer := func(low, high int) bool {
			parsed, err := strconv.Atoi(setting)
			if err != nil || parsed < low || parsed > high {
				return false
			}
			setting = strconv.Itoa(parsed)
			return true
		}
		switch key {
		case "subme":
			// Higher subme modes require separate trellis and AQ prerequisites.
			if !integer(0, 9) {
				return invalid()
			}
		case "me-range":
			// The retained preset motion-search methods clamp higher values.
			if !integer(4, 16) {
				return invalid()
			}
		case "rc-lookahead":
			if !integer(0, 250) {
				return invalid()
			}
		case "aq-mode":
			if !integer(0, 3) {
				return invalid()
			}
		case "aq-strength":
			parsed, err := strconv.ParseFloat(setting, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 3 {
				return invalid()
			}
			setting = strconv.FormatFloat(parsed, 'f', -1, 64)
		case "deblock":
			parts := strings.Split(setting, ",")
			if len(parts) != 2 {
				return invalid()
			}
			for index, part := range parts {
				parsed, err := strconv.Atoi(part)
				if err != nil || parsed < -6 || parsed > 6 {
					return invalid()
				}
				parts[index] = strconv.Itoa(parsed)
			}
			setting = strings.Join(parts, ",")
		case "partitions":
			if setting != "none" && setting != "all" {
				return invalid()
			}
		default:
			return invalid()
		}
		canonical = append(canonical, key+"="+setting)
	}
	sort.Strings(canonical)
	return strings.Join(canonical, ":"), nil
}
