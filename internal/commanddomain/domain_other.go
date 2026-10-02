//go:build !linux

package commanddomain

import (
	"context"
	"os/exec"
)

type Domain struct{}

func New(Config) (*Domain, error) { return nil, ErrUnavailable }
func NewWithExecutableCapabilities(Config, *ExecutableCapability, []*ExecutableCapability) (*Domain, error) {
	return nil, ErrUnavailable
}
func (*Domain) Start(context.Context, *exec.Cmd) (*Process, error) { return nil, ErrUnavailable }
func (*Domain) Retire(context.Context) error                       { return ErrUnavailable }
func (*Domain) Snapshot() Snapshot                                 { return Snapshot{Closed: true, Quarantined: true} }
func (*Domain) abnormalWait(*Process)                              {}
func (*Domain) joined(*Process)                                    {}
