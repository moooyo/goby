package tasks

import (
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/systemevents"
)

func TestSystemEventRulesHaveNoCalendarAndRejectUnsupportedOSSignals(t *testing.T) {
	for _, event := range []systemevents.Event{systemevents.ServerStarted, systemevents.LibraryChanged, systemevents.ConfigurationChanged} {
		rule := ScheduleRule{Kind: ScheduleSystemEvent, SystemEvent: event}
		if err := ValidateSchedule(rule); err != nil {
			t.Fatal(err)
		}
		if _, err := Next(rule, time.Now()); !errors.Is(err, ErrScheduleEvent) {
			t.Fatal("system event invented a timed due")
		}
		if _, err := Preview(rule, time.Now(), 3); !errors.Is(err, ErrScheduleEvent) {
			t.Fatal("system event invented preview timestamps")
		}
		ticks := ScheduleTicksPerSecond
		rule.IntervalTicks = &ticks
		if err := ValidateSchedule(rule); err == nil {
			t.Fatal("mixed event/calendar shape accepted")
		}
	}
	for _, rule := range []ScheduleRule{{Kind: ScheduleSystemEvent}, {Kind: ScheduleSystemEvent, SystemEvent: "DisplayConfigurationChange"}, {Kind: ScheduleStartup, SystemEvent: systemevents.ServerStarted}} {
		if err := ValidateSchedule(rule); err == nil {
			t.Fatal("unsupported event rule accepted")
		}
	}
}
