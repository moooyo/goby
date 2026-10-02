//go:build linux

package commanddomain

import "golang.org/x/sys/unix"

func checkLimitsPlatform(class LimitsClass) error {
	if err := validateLimitsClass(class); err != nil {
		return err
	}
	if class == LimitsDefault {
		return nil
	}
	if _, err := executableIssuerNow(); err != nil {
		return err
	}
	for _, expected := range launcherAnalysisRlimits() {
		var actual unix.Rlimit
		if unix.Getrlimit(expected.resource, &actual) != nil || actual.Max < expected.value {
			return ErrUnavailable
		}
		if expected.resource == unix.RLIMIT_NOFILE && actual.Cur < 1025 {
			return ErrUnavailable
		}
	}
	return nil
}

type launcherRlimit struct {
	resource int
	value    uint64
}

func launcherAnalysisRlimits() [3]launcherRlimit {
	return [3]launcherRlimit{{unix.RLIMIT_AS, launcherAnalysisAddressSpace}, {unix.RLIMIT_NOFILE, launcherAnalysisOpenFiles}, {unix.RLIMIT_FSIZE, launcherAnalysisFileSize}}
}

func applyLauncherLimits(config LauncherConfig) error {
	if err := validateLauncherLimits(config.Version, config.Limits); err != nil {
		return err
	}
	if config.Version == LauncherConfigVersion {
		return nil
	}
	for _, expected := range launcherAnalysisRlimits() {
		limit := unix.Rlimit{Cur: expected.value, Max: expected.value}
		if unix.Setrlimit(expected.resource, &limit) != nil {
			return ErrLauncherConfig
		}
		var actual unix.Rlimit
		if unix.Getrlimit(expected.resource, &actual) != nil || actual != limit {
			return ErrLauncherConfig
		}
	}
	return nil
}
