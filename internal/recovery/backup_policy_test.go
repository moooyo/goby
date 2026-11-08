package recovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func TestBackupPolicyScheduleUsesExistingCalendarSemantics(t *testing.T) {
	day := 0
	for _, test := range []struct {
		name        string
		schedule    BackupSchedule
		after, want time.Time
	}{
		{"daily", BackupSchedule{Kind: "daily", TimeOfDay: "03:00", Timezone: "Asia/Shanghai"}, time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 19, 0, 0, 0, time.UTC)},
		{"weekly", BackupSchedule{Kind: "weekly", DayOfWeek: &day, TimeOfDay: "04:15", Timezone: "UTC"}, time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC), time.Date(2026, 11, 8, 4, 15, 0, 0, time.UTC)},
	} {
		t.Run(test.name, func(t *testing.T) {
			rule, err := backupPolicyRule(test.schedule)
			if err != nil {
				t.Fatal(err)
			}
			next, err := tasks.Next(rule, test.after)
			if err != nil || !next.Equal(test.want) {
				t.Fatalf("next=%v err=%v want=%v", next, err, test.want)
			}
		})
	}
}

type unavailableBackupPolicyOwner struct{}

func (unavailableBackupPolicyOwner) WithOwnedTx(context.Context, func(library.OwnedTx) error) error {
	return ErrUnavailable
}

func TestBackupPolicyAvailabilityNeverWaitsForRecoveryManagerMutex(t *testing.T) {
	manager := &Manager{ctx: context.Background()}
	executor := &backupPolicyExecutor{manager: manager, owner: unavailableBackupPolicyOwner{}}
	manager.mu.Lock()
	finished := make(chan bool, 1)
	go func() { finished <- executor.Available() }()
	select {
	case available := <-finished:
		manager.mu.Unlock()
		if available {
			t.Fatal("missing backup dependencies were advertised as available")
		}
	case <-time.After(time.Second):
		manager.mu.Unlock()
		t.Fatal("task admission availability waited on the recovery manager mutex")
	}
}

func TestBackupPolicyScheduleRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	day := 7
	for _, schedule := range []BackupSchedule{
		{Kind: "interval", TimeOfDay: "03:00", Timezone: "UTC"},
		{Kind: "daily", TimeOfDay: "3:00", Timezone: "UTC"},
		{Kind: "daily", TimeOfDay: "03:ab", Timezone: "UTC"},
		{Kind: "daily", TimeOfDay: "24:00", Timezone: "UTC"},
		{Kind: "daily", TimeOfDay: "23:60", Timezone: "UTC"},
		{Kind: "daily", TimeOfDay: "03:00", Timezone: "Local"},
		{Kind: "daily", TimeOfDay: "03:00", Timezone: "../UTC"},
		{Kind: "daily", DayOfWeek: &day, TimeOfDay: "03:00", Timezone: "UTC"},
		{Kind: "weekly", TimeOfDay: "03:00", Timezone: "UTC"},
		{Kind: "weekly", DayOfWeek: &day, TimeOfDay: "03:00", Timezone: "UTC"},
		{},
	} {
		_, err := backupPolicyRule(schedule)
		var validation *BackupPolicyValidationError
		if !errors.As(err, &validation) || len(validation.Fields) == 0 {
			t.Fatalf("accepted invalid schedule: %#v err=%v", schedule, err)
		}
	}
}

func TestBackupPolicyExecutorDoesNotAdvertiseMissingDependencies(t *testing.T) {
	var manager *Manager
	registration := manager.BackupPolicyTaskRegistration(nil)
	if registration.Key != BackupTaskKey || !registration.Global || registration.Executor.Available() {
		t.Fatal("unconfigured automatic backups were advertised as available")
	}
	if err := registration.Executor.Execute(t.Context(), tasks.Work{TaskKey: BackupTaskKey}, nil); !errors.Is(err, tasks.ErrUnavailable) {
		t.Fatalf("unconfigured execution: %v", err)
	}
}
