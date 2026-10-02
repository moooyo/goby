//go:build !linux

package commanddomain

func checkLimitsPlatform(LimitsClass) error { return ErrUnavailable }
