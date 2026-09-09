//go:build !linux

package media

import "os/exec"

func configureMediaProcess(*exec.Cmd) func() error { return func() error { return nil } }
