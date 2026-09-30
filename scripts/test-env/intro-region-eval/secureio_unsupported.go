//go:build !linux

package main

import (
	"errors"
	"os"
)

var errUnsupportedIO = errors.New("this private evaluator requires Linux test-env for descriptor-relative no-symlink IO")

func openRegular(string) (*os.File, error) { return nil, errUnsupportedIO }

type reportWriter struct{}

func newReportWriter(string) (*reportWriter, error) { return nil, errUnsupportedIO }
func (*reportWriter) Publish([]byte) error          { return errUnsupportedIO }
func (*reportWriter) Close()                        {}
