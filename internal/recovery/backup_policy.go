package recovery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

const BackupTaskKey = "backup.create"

type BackupSchedule struct {
	Kind      string
	DayOfWeek *int
	TimeOfDay string
	Timezone  string
}

type BackupPolicyView struct {
	Revision          string
	Enabled           bool
	Schedule          BackupSchedule
	KeepCount         int
	IncludeImages     bool
	HasPassphrase     bool
	NextRunAt         *time.Time
	Available         bool
	UnavailableReason string
	TaskId            string
	LastRunAt         *time.Time
	LastRunState      string
	LastBackupId      string
}

type BackupPolicyRequest struct {
	Revision      string
	Enabled       bool
	Schedule      BackupSchedule
	KeepCount     int
	IncludeImages bool
	Passphrase    *[]byte `json:"-"`
}

// BackupPolicySettingsRequest changes backup contents and credentials without
// replacing the task's independently managed trigger set.
type BackupPolicySettingsRequest struct {
	Revision      string
	KeepCount     int
	IncludeImages bool
	Passphrase    *[]byte `json:"-"`
}

type BackupPolicyValidationError struct{ Fields map[string]string }

func (*BackupPolicyValidationError) Error() string { return "invalid automatic backup settings" }

type backupPolicyExecutor struct {
	manager *Manager
	owner   library.OwnedTransactions
}

// BackupPolicyTaskRegistration attaches a global executor to the existing
// durable task scheduler. No timer or independent background worker is added.
func (m *Manager) BackupPolicyTaskRegistration(owner library.OwnedTransactions) tasks.ExecutorRegistration {
	executor := &backupPolicyExecutor{manager: m, owner: owner}
	if m != nil {
		m.mu.Lock()
		m.backupPolicy = executor
		m.mu.Unlock()
	}
	return tasks.ExecutorRegistration{Key: BackupTaskKey, Name: "Create automatic backup", Description: "Create an encrypted backup using the saved backup policy, then retain only its completed automatic backups.", Category: "Backup", Global: true, Executor: executor}
}

func (executor *backupPolicyExecutor) Available() bool {
	if executor == nil || executor.manager == nil || executor.owner == nil {
		return false
	}
	m := executor.manager
	// Task admission calls Available while holding database locks. Never wait
	// for the recovery manager's mutex here: recovery mutations can hold that
	// mutex while waiting for the same database authority/definition locks.
	// This is an availability hint; Execute still checks healthyLocked before
	// allocating an object or acquiring its sealed publication authority.
	if m.ctx == nil || m.ctx.Err() != nil || m.runtime == nil || m.runtime.backups == nil {
		return false
	}
	storage := m.runtime.backups.Status()
	return storage.Healthy && m.engine.canCreate() && m.lease.Protects(m.pool)
}

// ValidateTaskTriggers runs under the task definition lock. Settings mutations
// take the same lock before changing the credential, so a clear cannot race a
// successful automatic-trigger replacement. Removing every rule remains possible
// while backup tools, storage, or the saved credential are unavailable.
func (executor *backupPolicyExecutor) ValidateTaskTriggers(tx library.OwnedTx, rules []tasks.ScheduleRule) error {
	if len(rules) == 0 {
		return nil
	}
	if !executor.Available() {
		return tasks.ErrUnavailable
	}
	var hasPassphrase bool
	var keep int
	if err := tx.QueryRow(`SELECT passphrase_ciphertext IS NOT NULL,keep_count FROM backup_policy WHERE id=1`).Scan(&hasPassphrase, &keep); err != nil {
		return tasks.ErrUnavailable
	}
	if !hasPassphrase {
		return &tasks.ValidationError{Fields: map[string]string{"Triggers": "Save an encryption passphrase in backup settings before adding automatic backup triggers."}}
	}
	if keep >= executor.manager.cfg.Recovery.WithDefaults().Backups.MaxObjects {
		return &tasks.ValidationError{Fields: map[string]string{"Triggers": "Keep fewer automatic backups than the deployment object limit before adding backup triggers."}}
	}
	return nil
}

func backupPolicyRule(schedule BackupSchedule) (tasks.ScheduleRule, error) {
	fields := map[string]string{}
	if len(schedule.Timezone) > 128 {
		fields["Schedule.Timezone"] = "Use an IANA time zone name of at most 128 bytes."
	}
	if schedule.Kind != "daily" && schedule.Kind != "weekly" {
		fields["Schedule.Kind"] = "Choose a daily or weekly backup schedule."
	}
	if len(schedule.TimeOfDay) != 5 || schedule.TimeOfDay[2] != ':' {
		fields["Schedule.TimeOfDay"] = "Use a time in HH:mm format."
	}
	hour, hourErr := strconv.Atoi(strings.SplitN(schedule.TimeOfDay, ":", 2)[0])
	minute := -1
	if len(schedule.TimeOfDay) == 5 {
		minute, _ = strconv.Atoi(schedule.TimeOfDay[3:])
	}
	if hourErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || fmt.Sprintf("%02d:%02d", hour, minute) != schedule.TimeOfDay {
		fields["Schedule.TimeOfDay"] = "Use a time in HH:mm format."
	}
	ticks := int64(hour*60+minute) * 60 * tasks.ScheduleTicksPerSecond
	rule := tasks.ScheduleRule{Kind: tasks.ScheduleKind(schedule.Kind), TimeOfDayTicks: &ticks, Timezone: schedule.Timezone}
	if schedule.Kind == "weekly" {
		if schedule.DayOfWeek == nil || *schedule.DayOfWeek < 0 || *schedule.DayOfWeek > 6 {
			fields["Schedule.DayOfWeek"] = "Choose a weekday between Sunday and Saturday."
		} else {
			day := *schedule.DayOfWeek
			rule.DayOfWeek = &day
		}
	} else if schedule.DayOfWeek != nil {
		fields["Schedule.DayOfWeek"] = "A daily backup does not have a weekday."
	}
	if err := tasks.ValidateSchedule(rule); err != nil && fields["Schedule.TimeOfDay"] == "" && fields["Schedule.Kind"] == "" && fields["Schedule.DayOfWeek"] == "" {
		fields["Schedule.Timezone"] = "Use UTC or an available IANA time zone."
	}
	if len(fields) > 0 {
		return tasks.ScheduleRule{}, &BackupPolicyValidationError{Fields: fields}
	}
	return rule, nil
}

func (m *Manager) BackupPolicy(ctx context.Context, actor identity.Principal) (BackupPolicyView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return BackupPolicyView{}, err
	}
	return m.readBackupPolicy(ctx)
}

func (m *Manager) readBackupPolicy(ctx context.Context) (BackupPolicyView, error) {
	view := BackupPolicyView{Schedule: BackupSchedule{Kind: "daily", TimeOfDay: "03:00", Timezone: "UTC"}, KeepCount: 7, IncludeImages: true}
	if m == nil || m.pool == nil {
		return view, ErrUnavailable
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return view, ErrUnavailable
	}
	defer rollbackRestore(tx)
	var revision int64
	var configuredTicks int64
	err = tx.QueryRow(ctx, `SELECT d.id,d.revision,p.configured_kind,p.configured_day_of_week,p.configured_time_of_day_ticks,p.configured_timezone,p.keep_count,p.include_images,p.passphrase_ciphertext IS NOT NULL
		FROM task_definitions d CROSS JOIN backup_policy p WHERE d.key=$1 AND p.id=1`, BackupTaskKey).Scan(&view.TaskId, &revision, &view.Schedule.Kind, &view.Schedule.DayOfWeek, &configuredTicks, &view.Schedule.Timezone, &view.KeepCount, &view.IncludeImages, &view.HasPassphrase)
	if err != nil {
		return view, ErrUnavailable
	}
	view.Revision = strconv.FormatInt(revision, 10)
	configuredMinutes := configuredTicks / (60 * tasks.ScheduleTicksPerSecond)
	view.Schedule.TimeOfDay = fmt.Sprintf("%02d:%02d", configuredMinutes/60, configuredMinutes%60)
	rows, err := tx.Query(ctx, `SELECT kind,time_of_day_ticks,day_of_week,timezone,next_fire_at FROM task_triggers WHERE task_id=$1 AND retired_at IS NULL ORDER BY position,id`, view.TaskId)
	if err != nil {
		return view, ErrUnavailable
	}
	count := 0
	for rows.Next() {
		var kind string
		var ticks *int64
		var day *int
		var zone *string
		var next *time.Time
		if rows.Scan(&kind, &ticks, &day, &zone, &next) != nil {
			rows.Close()
			return view, ErrUnavailable
		}
		count++
		if next != nil && (view.NextRunAt == nil || next.Before(*view.NextRunAt)) {
			at := next.UTC()
			view.NextRunAt = &at
		}
		if count == 1 && (kind == "daily" || kind == "weekly") && ticks != nil && *ticks >= 0 && *ticks < tasks.ScheduleTicksPerDay && *ticks%(60*tasks.ScheduleTicksPerSecond) == 0 && zone != nil {
			view.Schedule.Kind = kind
			view.Schedule.DayOfWeek = day
			view.Schedule.Timezone = *zone
			minutes := *ticks / (60 * tasks.ScheduleTicksPerSecond)
			view.Schedule.TimeOfDay = fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
		} else {
			view.Schedule.Kind = "custom"
			view.Schedule.DayOfWeek = nil
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return view, ErrUnavailable
	}
	rows.Close()
	view.Enabled = count > 0
	if count > 1 {
		view.Schedule.Kind = "custom"
		view.Schedule.DayOfWeek = nil
	}
	var at time.Time
	m.mu.Lock()
	deploymentID := m.data.DeploymentID
	m.mu.Unlock()
	err = tx.QueryRow(ctx, `SELECT created_at,state,backup_id FROM backup_policy_runs WHERE deployment_id=$1 ORDER BY created_at DESC,run_id DESC LIMIT 1`, deploymentID).Scan(&at, &view.LastRunState, &view.LastBackupId)
	if err == nil {
		at = at.UTC()
		view.LastRunAt = &at
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return view, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return view, ErrUnavailable
	}
	m.mu.Lock()
	executor := m.backupPolicy
	m.mu.Unlock()
	view.Available = executor != nil && executor.Available()
	if !view.Available {
		view.UnavailableReason = "backup_tools_or_storage_unavailable"
	}
	return view, nil
}

func (m *Manager) UpdateBackupPolicy(ctx context.Context, actor identity.Principal, request BackupPolicyRequest) (BackupPolicyView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return BackupPolicyView{}, err
	}
	rule, err := backupPolicyRule(request.Schedule)
	if err != nil {
		return BackupPolicyView{}, err
	}
	revision, err := strconv.ParseInt(request.Revision, 10, 64)
	if err != nil || revision < 1 || revision == math.MaxInt64 || strconv.FormatInt(revision, 10) != request.Revision {
		return BackupPolicyView{}, ErrInvalid
	}
	if request.KeepCount < 1 || request.KeepCount > 30 {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"KeepCount": "Keep between 1 and 30 automatic backups."}}
	}
	if request.Enabled && request.KeepCount >= m.cfg.Recovery.WithDefaults().Backups.MaxObjects {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"KeepCount": "Keep fewer backups than the deployment object limit so a new backup has a free slot."}}
	}
	if request.Passphrase != nil && len(*request.Passphrase) > 0 && ValidatePassphrase(*request.Passphrase) != nil {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Use an encryption passphrase of 12 to 1,024 UTF-8 bytes."}}
	}
	m.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.mu.Unlock()
		}
	}()
	if err = m.healthyLocked(ctx); err != nil {
		return BackupPolicyView{}, err
	}
	if m.backupPolicy == nil || request.Enabled && !m.engine.canCreate() {
		return BackupPolicyView{}, ErrUnavailable
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	defer rollbackRestore(tx)
	if err = identity.LockBackupPolicyMutation(ctx, tx, actor); err != nil {
		return BackupPolicyView{}, err
	}
	var taskID string
	var current int64
	if err = tx.QueryRow(ctx, `SELECT id,revision FROM task_definitions WHERE key=$1 FOR UPDATE`, BackupTaskKey).Scan(&taskID, &current); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return BackupPolicyView{}, err
	}
	if revision != current {
		return BackupPolicyView{}, ErrConflict
	}
	var generation int64
	var sealed []byte
	if err = tx.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1 FOR UPDATE`).Scan(&generation, &sealed); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if request.Passphrase != nil {
		if generation == math.MaxInt64 {
			return BackupPolicyView{}, ErrConflict
		}
		generation++
		sealed = nil
		if len(*request.Passphrase) > 0 {
			store := identity.NewWithApplicationKeyVault(m.pool, m.vault)
			sealed, err = store.SealBackupPolicyPassphrase(ctx, tx, generation, *request.Passphrase)
			if err != nil {
				return BackupPolicyView{}, err
			}
		}
	}
	if request.Enabled && len(sealed) == 0 {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Save an encryption passphrase before enabling automatic backups."}}
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE task_triggers SET retired_at=$2,updated_at=$2 WHERE task_id=$1 AND retired_at IS NULL`, taskID, now); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE task_definitions SET revision=revision+1,schedule_timezone=$2,updated_at=$3 WHERE id=$1`, taskID, request.Schedule.Timezone, now); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE backup_policy SET keep_count=$1,include_images=$2,secret_generation=$3,passphrase_ciphertext=$4,configured_kind=$5,configured_day_of_week=$6,configured_time_of_day_ticks=$7,configured_timezone=$8,updated_at=$9 WHERE id=1`, request.KeepCount, request.IncludeImages, generation, sealed, request.Schedule.Kind, rule.DayOfWeek, rule.TimeOfDayTicks, request.Schedule.Timezone, now); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if request.Enabled {
		next, calcErr := tasks.Next(rule, now.UTC())
		if calcErr != nil {
			return BackupPolicyView{}, ErrInvalid
		}
		id, idErr := randomOperationID()
		if idErr != nil {
			return BackupPolicyView{}, ErrUnavailable
		}
		maxTicks := int64(m.cfg.Recovery.WithDefaults().OperationTimeout / 100)
		if _, err = tx.Exec(ctx, `INSERT INTO task_triggers(id,task_id,schedule_revision,position,kind,time_of_day_ticks,day_of_week,timezone,max_runtime_ticks,next_fire_at,created_at,updated_at) VALUES($1,$2,$3,0,$4,$5,$6,$7,$8,$9,$10,$10)`, id, taskID, current+1, request.Schedule.Kind, rule.TimeOfDayTicks, rule.DayOfWeek, request.Schedule.Timezone, maxTicks, next, now); err != nil {
			return BackupPolicyView{}, ErrUnavailable
		}
	}
	count := int64(0)
	if request.Enabled {
		count = 1
	}
	if err = activity.Record(ctx, tx, activity.Event{Action: activity.ActionTaskScheduleUpdated, Source: activity.SourceNative, Actor: activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID}, Resource: activity.Resource{Kind: activity.ResourceTask, ID: taskID}, Revision: current + 1, Count: count, ChangedFields: []activity.Field{activity.FieldTriggers, activity.FieldScheduleTimezone}}); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return BackupPolicyView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	// Avoid calling the read helper while holding the manager's state lock.
	locked = false
	m.mu.Unlock()
	return m.readBackupPolicy(ctx)
}

// UpdateBackupPolicySettings shares the task revision with trigger editing but
// leaves every trigger, interval anchor, next occurrence, and timezone untouched.
// An uncertain commit is returned to the caller without replaying secret writes.
func (m *Manager) UpdateBackupPolicySettings(ctx context.Context, actor identity.Principal, request BackupPolicySettingsRequest) (BackupPolicyView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return BackupPolicyView{}, err
	}
	revision, err := strconv.ParseInt(request.Revision, 10, 64)
	if err != nil || revision < 1 || revision == math.MaxInt64 || strconv.FormatInt(revision, 10) != request.Revision {
		return BackupPolicyView{}, ErrInvalid
	}
	if request.KeepCount < 1 || request.KeepCount > 30 {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"KeepCount": "Keep between 1 and 30 automatic backups."}}
	}
	if request.Passphrase != nil && len(*request.Passphrase) > 0 && ValidatePassphrase(*request.Passphrase) != nil {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Use an encryption passphrase of 12 to 1,024 UTF-8 bytes."}}
	}
	m.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.mu.Unlock()
		}
	}()
	if err = m.healthyLocked(ctx); err != nil {
		return BackupPolicyView{}, err
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	defer rollbackRestore(tx)
	if err = identity.LockBackupPolicyMutation(ctx, tx, actor); err != nil {
		return BackupPolicyView{}, err
	}
	var taskID string
	var current int64
	if err = tx.QueryRow(ctx, `SELECT id,revision FROM task_definitions WHERE key=$1 FOR UPDATE`, BackupTaskKey).Scan(&taskID, &current); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return BackupPolicyView{}, err
	}
	if revision != current {
		return BackupPolicyView{}, ErrConflict
	}
	var generation int64
	var sealed []byte
	if err = tx.QueryRow(ctx, `SELECT secret_generation,passphrase_ciphertext FROM backup_policy WHERE id=1 FOR UPDATE`).Scan(&generation, &sealed); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	var automatic bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_triggers WHERE task_id=$1 AND retired_at IS NULL)`, taskID).Scan(&automatic); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if automatic && request.KeepCount >= m.cfg.Recovery.WithDefaults().Backups.MaxObjects {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"KeepCount": "Keep fewer backups than the deployment object limit so a new backup has a free slot."}}
	}
	if automatic && request.Passphrase != nil && len(*request.Passphrase) == 0 {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Remove all automatic backup triggers on Tasks before clearing the saved passphrase."}}
	}
	if request.Passphrase != nil {
		if generation == math.MaxInt64 {
			return BackupPolicyView{}, ErrConflict
		}
		generation++
		sealed = nil
		if len(*request.Passphrase) > 0 {
			store := identity.NewWithApplicationKeyVault(m.pool, m.vault)
			sealed, err = store.SealBackupPolicyPassphrase(ctx, tx, generation, *request.Passphrase)
			if err != nil {
				return BackupPolicyView{}, err
			}
		}
	}
	if automatic && len(sealed) == 0 {
		return BackupPolicyView{}, &BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Save an encryption passphrase before using automatic backup triggers."}}
	}
	if _, err = tx.Exec(ctx, `UPDATE backup_policy SET keep_count=$1,include_images=$2,secret_generation=$3,passphrase_ciphertext=$4,updated_at=clock_timestamp() WHERE id=1`, request.KeepCount, request.IncludeImages, generation, sealed); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE task_definitions SET revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, taskID); err != nil {
		return BackupPolicyView{}, ErrUnavailable
	}
	if err = activity.Record(ctx, tx, activity.Event{Action: activity.ActionSettingsUpdated, Source: activity.SourceNative, Actor: activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID}, Resource: activity.Resource{Kind: activity.ResourceSettings, ID: "backup_policy"}, Revision: current + 1}); err != nil {
		return BackupPolicyView{}, err
	}
	if err = identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return BackupPolicyView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return BackupPolicyView{}, err
	}
	locked = false
	m.mu.Unlock()
	return m.readBackupPolicy(ctx)
}

// NormalizeBackupPolicyRestore disables restored automatic schedules before
// startup can admit work. Retained run rows are evidence, never authority to
// delete objects from the importing deployment's backup directory.
func NormalizeBackupPolicyRestore(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('backup_policy') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE task_triggers t SET retired_at=clock_timestamp(),updated_at=clock_timestamp() FROM task_definitions d WHERE t.task_id=d.id AND d.key=$1 AND t.retired_at IS NULL`, BackupTaskKey); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE backup_policy_runs SET state='restored',error_code='backup_restored',updated_at=clock_timestamp() WHERE state<>'deleted'`)
	return err
}
