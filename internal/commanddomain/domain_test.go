package commanddomain

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestDomainAbsentOwnerCannotCertifyRetirement(t *testing.T) {
	var domain *Domain
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := domain.Retire(ctx); err == nil {
		t.Fatal("an absent native owner certified retirement")
	}
	if process, err := domain.Start(ctx, exec.Command("/unused")); err == nil || process != nil {
		t.Fatal("an absent native owner admitted a command")
	}
}

func TestDomainDisabledAndMalformedPrerequisitesCreateNoOwner(t *testing.T) {
	for _, config := range []Config{
		{},
		{Enabled: true},
		{Enabled: true, MaxCommands: MaxDomainCommands + 1},
		{Enabled: true, MaxCommands: 1, MaxTasks: 4097},
	} {
		domain, err := New(config)
		if err == nil || domain != nil {
			t.Fatalf("an absent or malformed prerequisite created a native owner: %v", err)
		}
	}
}

func TestDomainZeroValueCannotCertifyRetirement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Domain{}).Retire(ctx); err == nil {
		t.Fatal("a zero-value object certified native retirement")
	}
}

func TestDomainContextCarriesOnlyTheActualOwnerPointer(t *testing.T) {
	if FromContext(nil) != nil || FromContext(context.Background()) != nil {
		t.Fatal("an unrelated context supplied a native owner")
	}
	// Context propagation is an identity check, not a native readiness fixture.
	domain := &Domain{}
	if FromContext(WithDomain(context.Background(), domain)) != domain {
		t.Fatal("the exact owner pointer was not propagated")
	}
}

func TestDomainWaitRejectsMissingAndDuplicateOwnership(t *testing.T) {
	var process *Process
	if !errors.Is(process.Wait(), ErrWaitOwnership) {
		t.Fatal("a nil process accepted Wait")
	}
	process = &Process{domain: &Domain{}, command: exec.Command("/unused"), waitStarted: true}
	if !errors.Is(process.Wait(), ErrWaitOwnership) {
		t.Fatal("duplicate Wait ownership was accepted")
	}
	if _, joined := process.ExitCode(); joined {
		t.Fatal("an unjoined process reported an exit code")
	}
}
