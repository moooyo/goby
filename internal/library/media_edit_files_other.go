//go:build !linux

package library

import "os"

func mediaEditTrustedToolPath(string) error { return ErrUnavailable }

func mediaEditSameFilesystem(os.FileInfo, os.FileInfo) bool      { return false }
func mediaEditExchange(*os.Root, string, *os.Root, string) error { return ErrUnavailable }
func mediaEditCopyAttributes(*os.File, *os.File) (string, error) { return "", ErrUnavailable }
func mediaEditAttributeDigest(*os.File) (string, error)          { return "", ErrUnavailable }
