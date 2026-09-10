//go:build !linux

package main

import (
	"context"
	"os"
)

func readCLIPassphrase(context.Context, string, bool) ([]byte, error) { return nil, errCLIInput }
func openCLIArchive(string) (*os.File, error)                         { return nil, errCLIInput }
