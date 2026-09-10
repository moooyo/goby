// Package diagnostics stores bounded, sanitized service diagnostics.
package diagnostics

import (
	"errors"
	"time"
)

const (
	DefaultDirectory     = "/var/log/goby"
	DefaultMaxFileBytes  = int64(4 << 20)
	DefaultMaxFiles      = 16
	DefaultRetentionDays = 7
	DefaultMinFreeBytes  = int64(32 << 20)
	MaxRecordBytes       = 8 << 10
	MaxReadBytes         = int64(64 << 20)
	MaxReaders           = 8
)

var (
	ErrUnavailable = errors.New("diagnostics unavailable")
	ErrNotFound    = errors.New("diagnostic file not found")
	ErrInvalid     = errors.New("invalid diagnostics request")
	ErrBusy        = errors.New("diagnostics busy")
)

type Config struct {
	Directory     string
	MaxFileBytes  int64
	MaxFiles      int
	RetentionDays int
	MinFreeBytes  int64
}

// WithDefaults returns effective policy values without accessing the filesystem.
func (c Config) WithDefaults() Config {
	if c.Directory == "" {
		c.Directory = DefaultDirectory
	}
	if c.MaxFileBytes == 0 {
		c.MaxFileBytes = DefaultMaxFileBytes
	}
	if c.MaxFiles == 0 {
		c.MaxFiles = DefaultMaxFiles
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = DefaultRetentionDays
	}
	if c.MinFreeBytes == 0 {
		c.MinFreeBytes = DefaultMinFreeBytes
	}
	return c
}

type Status struct {
	Healthy       bool   `json:"Healthy"`
	Degraded      bool   `json:"Degraded"`
	Closed        bool   `json:"Closed"`
	MaxFileBytes  int64  `json:"MaxFileBytes"`
	MaxFiles      int    `json:"MaxFiles"`
	RetentionDays int    `json:"RetentionDays"`
	MinFreeBytes  int64  `json:"MinFreeBytes"`
	Format        string `json:"Format"`
}

type File struct {
	Name         string    `json:"Name"`
	DateCreated  time.Time `json:"DateCreated"`
	DateModified time.Time `json:"DateModified"`
	Size         int64     `json:"Size"`
}

type ListOptions struct {
	StartIndex int
	Limit      int
}

type Page struct {
	Items            []File `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

type LinesOptions struct {
	StartIndex int
	Limit      int
}

// LinesPage describes one immutable byte snapshot. TotalRecordCount is the
// number of complete JSONL lines in that snapshot, not a count of later writes.
type LinesPage struct {
	Items            []string `json:"Items"`
	StartIndex       int      `json:"StartIndex"`
	NextIndex        int      `json:"NextIndex"`
	TotalRecordCount int      `json:"TotalRecordCount"`
	SnapshotSize     int64    `json:"SnapshotSize"`
}
