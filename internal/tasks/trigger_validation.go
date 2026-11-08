package tasks

import "github.com/moooyo/goby/internal/library"

// TriggerValidator lets a registered executor validate durable prerequisites
// under the task definition lock before its automatic rules are replaced.
// Implementations must use the supplied bounded transaction, perform no external
// work, and allow an empty rule set to remove unusable automatic schedules.
type TriggerValidator interface {
	ValidateTaskTriggers(library.OwnedTx, []ScheduleRule) error
}
