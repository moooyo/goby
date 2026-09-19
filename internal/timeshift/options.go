package timeshift

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultWindow            = 600 * time.Second
	defaultWindowBytes int64 = 512 << 20
	defaultGlobalBytes int64 = 2 << 30
	hardWindows              = 128
	hardArtifacts            = 65536
	hardVariants             = 16
	hardSegments             = 4096
	hardEpochs               = 64
)

func normalizeOptions(options Options) (Options, error) {
	if options.MaxBytes == 0 {
		options.MaxBytes = defaultGlobalBytes
	}
	if options.MaxWindows == 0 {
		options.MaxWindows = 32
	}
	if options.MaxOwnerWindows == 0 {
		options.MaxOwnerWindows = min(4, options.MaxWindows)
	}
	if options.MaxReaders == 0 {
		options.MaxReaders = 64
	}
	if options.MaxWindowReaders == 0 {
		options.MaxWindowReaders = min(8, options.MaxReaders)
	}
	if options.MaxPublishing == 0 {
		options.MaxPublishing = min(4, options.MaxWindows)
	}
	if options.MaxArtifactBytes == 0 {
		options.MaxArtifactBytes = min(64<<20, options.MaxBytes)
	}
	if options.MaxSegments == 0 {
		options.MaxSegments = 2048
	}
	if options.MaxEpochs == 0 {
		options.MaxEpochs = 32
	}
	if options.MaxVariants == 0 {
		options.MaxVariants = hardVariants
	}
	if options.MaxArtifacts == 0 {
		options.MaxArtifacts = hardArtifacts
	}
	if options.ReadTimeout == 0 {
		options.ReadTimeout = 30 * time.Second
	}
	if options.IdleTimeout == 0 {
		options.IdleTimeout = 5 * time.Minute
	}
	if options.SweepInterval == 0 {
		options.SweepInterval = time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Root == "" || options.MaxBytes < 1 || options.MaxBytes > 1<<40 ||
		options.MaxWindows < 1 || options.MaxWindows > hardWindows || options.MaxOwnerWindows < 1 || options.MaxOwnerWindows > options.MaxWindows ||
		options.MaxReaders < 1 || options.MaxReaders > 1024 || options.MaxWindowReaders < 1 || options.MaxWindowReaders > options.MaxReaders ||
		options.MaxPublishing < 1 || options.MaxPublishing > 16 || options.MaxPublishing > options.MaxWindows ||
		options.MaxArtifactBytes < 1 || options.MaxArtifactBytes > 256<<20 || options.MaxArtifactBytes > options.MaxBytes ||
		options.MaxSegments < 1 || options.MaxSegments > hardSegments || options.MaxEpochs < 1 || options.MaxEpochs > hardEpochs ||
		options.MaxVariants < 1 || options.MaxVariants > hardVariants || options.MaxArtifacts < 1 || options.MaxArtifacts > hardArtifacts ||
		options.ReadTimeout < time.Millisecond || options.ReadTimeout > 2*time.Minute ||
		options.IdleTimeout < time.Millisecond || options.IdleTimeout > time.Hour ||
		options.SweepInterval < time.Millisecond || options.SweepInterval > time.Minute {
		return options, ErrInvalid
	}
	return options, nil
}

func normalizeWindow(options WindowOptions, limits Options) (WindowOptions, error) {
	if options.Window == 0 {
		options.Window = defaultWindow
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = min(defaultWindowBytes, limits.MaxBytes)
	}
	if options.Window < time.Millisecond || options.Window > 24*time.Hour || options.MaxBytes < 1 || options.MaxBytes > limits.MaxBytes ||
		len(options.Variants) < 1 || len(options.Variants) > limits.MaxVariants || options.TargetDurationTicks < 0 ||
		options.TargetDurationTicks > 600*TicksPerSecond || options.TargetDurationTicks%TicksPerSecond != 0 ||
		options.TargetDurationTicks > 0 && int64(options.Window/(100*time.Nanosecond)) < 3*options.TargetDurationTicks {
		return options, ErrInvalid
	}
	seen := make(map[string]bool, len(options.Variants))
	for _, variant := range options.Variants {
		if len(variant.ID) < 1 || len(variant.ID) > 32 || strings.Trim(variant.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" || seen[variant.ID] {
			return options, ErrInvalid
		}
		seen[variant.ID] = true
		if variant.Kind == "subtitle" {
			if variant.Format != "vtt" {
				return options, ErrInvalid
			}
		} else if (variant.Kind != "video" && variant.Kind != "audio") || (variant.Format != "ts" && variant.Format != "fmp4") {
			return options, ErrInvalid
		}
	}
	options.Variants = append([]Variant(nil), options.Variants...)
	return options, nil
}

func validScope(scope Scope) bool {
	valid := func(value string, optional bool) bool {
		return (optional || value != "") && len(value) <= 256 && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
			strings.IndexFunc(value, unicode.IsControl) < 0
	}
	if !valid(scope.AuthSessionID, false) || !valid(scope.DeviceID, true) || !valid(scope.PlaySessionID, false) ||
		!valid(scope.ItemID, false) || !valid(scope.SourceID, false) {
		return false
	}
	if scope.ApplicationKey {
		return scope.UserID == "" && valid(scope.ApplicationClientID, false)
	}
	return valid(scope.UserID, false) && scope.ApplicationClientID == ""
}

func sameOwner(first, second Scope) bool {
	if first.ApplicationKey != second.ApplicationKey {
		return false
	}
	if first.ApplicationKey {
		return first.AuthSessionID == second.AuthSessionID
	}
	return first.UserID == second.UserID
}
