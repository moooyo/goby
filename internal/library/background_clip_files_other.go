//go:build !linux

package library

import "os"

func lockBackgroundClipDirectory(*os.Root) (func() error, error)   { return nil, ErrUnavailable }
func backgroundClipRenameNoReplace(*os.Root, string, string) error { return ErrUnavailable }
func syncBackgroundClipDirectory(*os.Root) error                   { return ErrUnavailable }
