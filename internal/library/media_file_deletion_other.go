//go:build !linux

package library

import "os"

func fileDeletionSupported() bool                                          { return false }
func fileDeletionPrivateDirectory(os.FileInfo) bool                        { return false }
func fileDeletionRenameNoReplace(*os.Root, string, *os.Root, string) error { return ErrUnavailable }
func fileDeletionUnlink(*os.Root, string) error                            { return ErrUnavailable }
