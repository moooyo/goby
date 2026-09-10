package config

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/diagnostics"
)

const DefaultActivityRetentionDays = 30

func loadObservability() (diagnostics.Config, int, error) {
	value := diagnostics.Config{Directory: env("GOBY_LOG_DIR", diagnostics.DefaultDirectory)}
	fields := []struct {
		name                       string
		fallback, minimum, maximum int64
	}{
		{"GOBY_LOG_MAX_FILE_BYTES", diagnostics.DefaultMaxFileBytes, 8192, 64 << 20},
		{"GOBY_LOG_MAX_FILES", diagnostics.DefaultMaxFiles, 1, 256},
		{"GOBY_LOG_RETENTION_DAYS", diagnostics.DefaultRetentionDays, 1, 365},
		{"GOBY_LOG_MIN_FREE_BYTES", diagnostics.DefaultMinFreeBytes, 1, 1 << 40},
		{"GOBY_ACTIVITY_RETENTION_DAYS", DefaultActivityRetentionDays, 1, 365},
	}
	values := make([]int64, len(fields))
	for index, field := range fields {
		parsed, err := strconv.ParseInt(env(field.name, strconv.FormatInt(field.fallback, 10)), 10, 64)
		if err != nil || parsed < field.minimum || parsed > field.maximum {
			return diagnostics.Config{}, 0, fmt.Errorf("%s must be a decimal integer between %d and %d", field.name, field.minimum, field.maximum)
		}
		values[index] = parsed
	}
	value.MaxFileBytes, value.MaxFiles = values[0], int(values[1])
	value.RetentionDays, value.MinFreeBytes = int(values[2]), values[3]
	days := int(values[4])
	if err := validateObservability(value, days); err != nil {
		return diagnostics.Config{}, 0, err
	}
	return value, days, nil
}

// Policy validation performs no filesystem access. Open verifies ownership and
// the dedicated directory's live identity before any diagnostic file is used.
func validateObservability(value diagnostics.Config, activityDays int) error {
	if strings.ContainsRune(value.Directory, '\x00') || len(value.Directory) > 4096 ||
		!path.IsAbs(value.Directory) || path.Clean(value.Directory) != value.Directory || value.Directory == "/" {
		return fmt.Errorf("GOBY_LOG_DIR must be a clean absolute dedicated directory")
	}
	if value.MaxFileBytes < 8192 || value.MaxFileBytes > 64<<20 || value.MaxFiles < 1 || value.MaxFiles > 256 ||
		value.RetentionDays < 1 || value.RetentionDays > 365 || value.MinFreeBytes < 1 || value.MinFreeBytes > 1<<40 {
		return fmt.Errorf("diagnostic log policy exceeds the supported bounds")
	}
	if activityDays < 0 || activityDays > 365 {
		return fmt.Errorf("GOBY_ACTIVITY_RETENTION_DAYS must be between 1 and 365")
	}
	return nil
}
