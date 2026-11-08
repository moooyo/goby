//go:build linux

package recovery

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/tasks"
)

type backupPolicyScheduleSnapshot struct {
	timezone   string
	triggers   string
	configured string
}

func snapshotBackupPolicySchedule(t *testing.T, f *managerIntegrationFixture, taskID string) backupPolicyScheduleSnapshot {
	t.Helper()
	var result backupPolicyScheduleSnapshot
	err := f.seed.source.QueryRow(f.seed.ctx, `SELECT d.schedule_timezone,
		COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.position,t.id) FROM task_triggers t WHERE t.task_id=d.id),'[]'::jsonb)::text,
		jsonb_build_array(p.configured_kind,p.configured_day_of_week,p.configured_time_of_day_ticks,p.configured_timezone)::text
		FROM task_definitions d CROSS JOIN backup_policy p WHERE d.id=$1 AND p.id=1`, taskID).
		Scan(&result.timezone, &result.triggers, &result.configured)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBackupPolicySettingsPreserveCustomTriggersAndCredentialActions(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	_, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	// This test edits rules without dispatching their startup occurrence.
	if err := taskManager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("settings-only-backup-passphrase")
	defer clear(passphrase)
	saved, err := f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{
		Revision: policy.Revision, KeepCount: 14, IncludeImages: false, Passphrase: &passphrase,
	})
	if err != nil || saved.Enabled || !saved.HasPassphrase || saved.IncludeImages || saved.Revision == policy.Revision {
		t.Fatalf("save settings before adding rules: %#v %v", saved, err)
	}
	definition, err := store.Get(ctx, saved.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	daily := int64(3*3600+25*60) * tasks.ScheduleTicksPerSecond
	weekly := int64(4*3600+30*60) * tasks.ScheduleTicksPerSecond
	interval := int64(3600) * tasks.ScheduleTicksPerSecond
	runtime := int64(300) * tasks.ScheduleTicksPerSecond
	day := 2
	definition, err = store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.ReplaceTriggersRequest{
		TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "Asia/Shanghai",
		Triggers: []tasks.ScheduleRule{
			{Kind: tasks.ScheduleDaily, TimeOfDayTicks: &daily},
			{Kind: tasks.ScheduleWeekly, TimeOfDayTicks: &weekly, DayOfWeek: &day},
			{Kind: tasks.ScheduleInterval, IntervalTicks: &interval, MaxRuntimeTicks: &runtime},
			{Kind: tasks.ScheduleStartup},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotBackupPolicySchedule(t, f, definition.ID)
	policy, err = f.manager.BackupPolicy(ctx, actor)
	if err != nil || policy.Schedule.Kind != "custom" || !policy.Enabled {
		t.Fatalf("custom backup schedule: %#v %v", policy, err)
	}
	var generation int64
	var ciphertext []byte
	if err := f.seed.source.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1`).Scan(&generation, &ciphertext); err != nil {
		t.Fatal(err)
	}
	request := BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 7, IncludeImages: true}
	saved, err = f.manager.UpdateBackupPolicySettings(ctx, actor, request)
	if err != nil || saved.Revision == policy.Revision || saved.KeepCount != 7 || !saved.IncludeImages || !saved.HasPassphrase || saved.Schedule.Kind != "custom" {
		t.Fatalf("patch custom backup settings: %#v %v", saved, err)
	}
	if after := snapshotBackupPolicySchedule(t, f, definition.ID); after != before {
		t.Fatal("settings patch changed a trigger, occurrence, interval anchor, timezone, or preferred legacy schedule")
	}
	var retainedGeneration int64
	var retainedCiphertext []byte
	if err := f.seed.source.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1`).Scan(&retainedGeneration, &retainedCiphertext); err != nil || retainedGeneration != generation || !bytes.Equal(retainedCiphertext, ciphertext) {
		t.Fatal("omitted passphrase changed the sealed credential")
	}
	if _, err := f.manager.UpdateBackupPolicySettings(ctx, actor, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale settings patch was not rejected: %v", err)
	}
	if _, err := store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC"}); !errors.Is(err, tasks.ErrRevisionConflict) {
		t.Fatalf("settings patch did not invalidate a stale task editor: %v", err)
	}
	empty := []byte{}
	_, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{
		Revision: saved.Revision, KeepCount: 7, IncludeImages: true, Passphrase: &empty,
	})
	var validation *BackupPolicyValidationError
	if !errors.As(err, &validation) || validation.Fields["Passphrase"] == "" {
		t.Fatalf("active rules allowed clearing their credential: %v", err)
	}
	current, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil || current.Revision != saved.Revision || !current.HasPassphrase || snapshotBackupPolicySchedule(t, f, definition.ID) != before {
		t.Fatal("rejected credential clear changed saved authority or schedule")
	}
	replacement := []byte("replacement-settings-passphrase")
	defer clear(replacement)
	saved, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{
		Revision: current.Revision, KeepCount: 7, IncludeImages: true, Passphrase: &replacement,
	})
	if err != nil || !saved.HasPassphrase || snapshotBackupPolicySchedule(t, f, definition.ID) != before {
		t.Fatalf("credential replacement changed the custom schedule: %v", err)
	}
	if err := f.seed.source.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1`).Scan(&retainedGeneration, &retainedCiphertext); err != nil || retainedGeneration != generation+1 || bytes.Equal(retainedCiphertext, ciphertext) || bytes.Contains(retainedCiphertext, replacement) {
		t.Fatal("replacement did not rotate the sealed credential generation")
	}
	opened, err := f.seed.vault.OpenBackupPolicyPassphrase(ctx, retainedGeneration, retainedCiphertext)
	if err != nil || !bytes.Equal(opened, replacement) {
		clear(opened)
		t.Fatal("replacement credential could not be authenticated")
	}
	clear(opened)
	definition, err = store.Get(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	definition, err = store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.ReplaceTriggersRequest{
		TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: definition.ScheduleTimezone, Triggers: []tasks.ScheduleRule{},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err = f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	before = snapshotBackupPolicySchedule(t, f, definition.ID)
	cleared, err := f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{
		Revision: policy.Revision, KeepCount: 7, IncludeImages: false, Passphrase: &empty,
	})
	if err != nil || cleared.Enabled || cleared.HasPassphrase || cleared.NextRunAt != nil || snapshotBackupPolicySchedule(t, f, definition.ID) != before {
		t.Fatalf("clear after explicit trigger removal: %#v %v", cleared, err)
	}
	var settingsEvents, falseScheduleEvents int
	if err := f.seed.source.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM activity_entries WHERE action='settings.updated' AND resource_id='backup_policy'),
		(SELECT count(*) FROM activity_entries WHERE action='task.schedule_updated' AND resource_id=$1 AND revision::text=$2)`, definition.ID, cleared.Revision).Scan(&settingsEvents, &falseScheduleEvents); err != nil || settingsEvents != 4 || falseScheduleEvents != 0 {
		t.Fatal("settings-only mutations claimed a schedule change or lost their audit records")
	}
}

func TestBackupTaskTriggersRequireCredentialAndAvailableExecutor(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	_, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	if err := taskManager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := store.Get(ctx, policy.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	request := tasks.ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []tasks.ScheduleRule{{Kind: tasks.ScheduleStartup}}}
	before := snapshotBackupPolicySchedule(t, f, definition.ID)
	_, err = store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, request)
	var validation *tasks.ValidationError
	if !errors.As(err, &validation) || validation.Fields["Triggers"] == "" || snapshotBackupPolicySchedule(t, f, definition.ID) != before {
		t.Fatalf("backup triggers accepted a missing credential or changed rejected rows: %v", err)
	}
	passphrase := []byte("task-trigger-guard-passphrase")
	defer clear(passphrase)
	policy, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 7, IncludeImages: true, Passphrase: &passphrase})
	if err != nil {
		t.Fatal(err)
	}
	definition, err = store.Get(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	request.Revision = definition.Revision
	owner := f.manager.backupPolicy.owner
	f.manager.backupPolicy.owner = nil
	defer func() { f.manager.backupPolicy.owner = owner }()
	if _, err := store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, request); !errors.Is(err, tasks.ErrUnavailable) {
		t.Fatalf("backup triggers accepted an unavailable executor: %v", err)
	}
	request.Triggers = []tasks.ScheduleRule{}
	if _, err := store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, request); err != nil {
		t.Fatalf("unavailable executor prevented removing its automatic rules: %v", err)
	}
}

func TestBackupPolicyCredentialClearAndTriggerAdditionShareRevisionAuthority(t *testing.T) {
	f := newManagerIntegrationFixture(t)
	_, store, taskManager := backupPolicyRepository(t, f)
	ctx, actor := f.seed.ctx, f.seed.actor
	if err := taskManager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	policy, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("concurrent-backup-trigger-passphrase")
	defer clear(passphrase)
	policy, err = f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 7, IncludeImages: true, Passphrase: &passphrase})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := store.Get(ctx, policy.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	clearResult, triggerResult := make(chan error, 1), make(chan error, 1)
	go func() {
		<-ready
		empty := []byte{}
		_, err := f.manager.UpdateBackupPolicySettings(ctx, actor, BackupPolicySettingsRequest{Revision: policy.Revision, KeepCount: 7, IncludeImages: true, Passphrase: &empty})
		clearResult <- err
	}()
	go func() {
		<-ready
		_, err := store.ReplaceTriggers(ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative}, tasks.ReplaceTriggersRequest{
			TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []tasks.ScheduleRule{{Kind: tasks.ScheduleStartup}},
		})
		triggerResult <- err
	}()
	close(ready)
	var clearErr, triggerErr error
	for index := 0; index < 2; index++ {
		select {
		case clearErr = <-clearResult:
			clearResult = nil
		case triggerErr = <-triggerResult:
			triggerResult = nil
		case <-time.After(30 * time.Second):
			t.Fatal("credential clear and trigger addition did not finish their lock order")
		}
	}
	if clearErr == nil {
		if !errors.Is(triggerErr, tasks.ErrRevisionConflict) {
			t.Fatalf("credential clear did not fence the stale trigger edit: %v", triggerErr)
		}
	} else if triggerErr != nil || !errors.Is(clearErr, ErrConflict) {
		t.Fatalf("concurrent changes did not produce one revision winner: clear=%v triggers=%v", clearErr, triggerErr)
	}
	current, err := f.manager.BackupPolicy(ctx, actor)
	if err != nil || current.Enabled && !current.HasPassphrase || !current.Enabled && current.HasPassphrase {
		t.Fatalf("concurrent edits left mismatched scheduling and credential authority: %#v %v", current, err)
	}
}
