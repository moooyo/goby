import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton, LinearProgress, MenuItem, Paper, Skeleton, Stack, Switch, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Tooltip, Typography } from './ui';
import { AddRounded } from './icons';
import { DownloadRounded } from './icons';
import { DeleteOutlineRounded } from './icons';
import { UploadFileRounded } from './icons';
import { RefreshRounded } from './icons';
import { RestoreRounded } from './icons';
import { ShieldOutlined } from './icons';
import { CheckRounded, MoreVertRounded, SaveOutlined, SettingsOutlined } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { TaskDefinition } from './api';
import { backupsApi, newBackupRequestId, validateBackupPassphrase } from './backupsApi';
import type { BackupPage, BackupView, OperationPage, OperationView, SourceView, StatusView } from './backupsApi';
import { backupAttemptMatchesOperation, backupAttemptResolvedByOperation, readBackupAttempt, removeBackupAttempt, sameBackupAttempt, storeBackupAttempt } from './backupReceipts';
import type { BackupAttempt, BackupReceiptScope } from './backupReceipts';
import { ErrorNotice } from './components';
import { colors } from './theme';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { adminLocale, confirmAction, useTranslation } from './i18n';
import { backupPolicyApi, backupPolicyDraft, parseBackupPolicyDraft } from './backupPolicyApi';
import type { BackupPolicy, BackupPolicyDraft } from './backupPolicyApi';
import { fieldError, PasswordField } from './formFields';
import { scheduleDate } from './taskSchedule';
import { taskScheduleSummary, taskTriggerLabel } from './taskTriggerPresentation';
import './backup-notifications.css';
import './backup-policy.css';

const pageSize = 25;
const sectionSpacing = { p: { xs: 2, sm: 2.5 } };
const paginationStyles = {
  borderTop: 1, borderColor: 'divider',
  '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: 2 },
  '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } },
};
const recoveryMessages: Record<string, string> = {
  storage_unavailable: 'Backup storage is unavailable. Ask the server operator to check the backup storage configuration.',
  tools_unavailable: 'Backup tools are unavailable. Ask the server operator to install the required recovery tools.',
  database_unavailable: 'The server cannot access its data. Ask the server operator to check the database connection.',
  recovery_database_not_configured: 'Restore needs a separate recovery database. Ask the server operator to configure it before planning a restore.',
  recovery_required: 'The server needs recovery attention. Ask the server operator to resolve the recovery state before continuing.',
  invalid_archive: 'This backup could not be verified. Check the passphrase and use a complete Goby backup file.',
  capacity_exceeded: 'There is not enough backup capacity. Download and delete an older backup, or ask the server operator to increase the limit.',
  target_not_ready: 'The server is not ready for this change. Refresh the page and check the current job.',
  source_changed: 'The backup or restore plan changed. Refresh and review the latest version before continuing.',
  authority_changed: 'Your administrator access changed. Sign in again before continuing.',
  audit_unavailable: 'The server cannot record this change. Ask the server operator to check activity storage.',
  operation_cancelled: 'This job was cancelled.',
  operation_interrupted: 'This job was interrupted. Review its state before starting a new attempt.',
  activation_failed: 'The recovered server did not pass startup checks. Check the current server and rollback availability before continuing.',
  operation_busy: 'Another recovery job is in progress. Wait for it to finish or cancel it if cancellation is available.',
  invalid_response: 'The server returned an unexpected response. Refresh to check the actual job state.',
  network_error: 'The server could not be reached. Reconnect to check the actual job state.',
  secure_random_unavailable: 'Secure request IDs are unavailable. Open the administrator page over HTTPS before starting a job.',
  backup_receipt_unavailable: 'The pending recovery receipt could not be stored or read. Allow session storage and reconnect before starting another recovery request.',
};
const kindLabels: Record<OperationView['Kind'], string> = { create: 'Create backup', import: 'Import backup', delete: 'Delete backup', restore: 'Restore backup', rollback: 'Roll back' };
const phaseLabels: Record<OperationView['Phase'], string> = {
  admission: 'Queued', upload: 'Receiving file', snapshot: 'Collecting server data', encryption: 'Encrypting backup',
  publication: 'Saving backup', validation: 'Checking backup', staging: 'Preparing restored data', ready: 'Ready for review',
  activation: 'Starting recovered server', rollback: 'Returning to the retained copy', cleanup: 'Finishing up', finished: 'Finished',
};

type AdmissionAttempt = BackupAttempt;
interface Snapshot { status: StatusView; backups: BackupPage; operations: OperationPage }
type DialogState = { kind: 'create'; includeImages: boolean }
  | { kind: 'import' }
  | { kind: 'policy' }
  | { kind: 'plan'; backup: BackupView; status: StatusView }
  | { kind: 'delete'; backup: BackupView }
  | { kind: 'inspect'; operationId: string }
  | { kind: 'rollback'; status: StatusView };
interface AdmissionCallbacks {
  onAttempt: (attempt: AdmissionAttempt) => void;
  onRejected: () => void;
  onAccepted: (operation: OperationView) => void;
  onUnknown: (attempt: AdmissionAttempt) => void;
  onClose: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

function readableState(state: string): string { return state.charAt(0).toUpperCase() + state.slice(1); }
function dateTime(value: string): string { return new Date(value).toLocaleString(document.documentElement.lang || 'zh-CN', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }); }
function backupListTime(value: string): string { return new Date(value).toLocaleString(adminLocale(), { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
function checkedBackupVersion(backup: BackupView): string | undefined { return backup.Verified && backup.Source?.GobyVersion ? backup.Source.GobyVersion : undefined; }
function count(value: string): string { return BigInt(value).toLocaleString(adminLocale()); }
function bytes(value: string): string {
  const amount = BigInt(value);
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
  let divisor = 1n;
  let unit = 0;
  while (unit < units.length - 1 && amount >= divisor * 1024n) { divisor *= 1024n; unit += 1; }
  if (unit === 0) return `${amount.toLocaleString(adminLocale())} B`;
  const decimal = (amount % divisor) * 10n / divisor;
  return `${(amount / divisor).toLocaleString(adminLocale())}${decimal ? `.${decimal}` : ''} ${units[unit]}`;
}
function activeOperation(operation: OperationView): boolean { return ['pending', 'running', 'applying'].includes(operation.State); }
function recoverySwitchAvailable(status: StatusView): boolean {
  // Configuration and health failures take precedence over missing tools in
  // status. Switching a checked candidate does not decode an archive again.
  return status.UnavailableReason !== 'storage_unavailable' && status.UnavailableReason !== 'recovery_required'
    && (status.RestoreAvailable || status.RestoreUnavailableReason === 'tools_unavailable');
}
function uncertain(error: unknown): boolean {
  return !(error instanceof ApiError) || ['network_error', 'invalid_response', 'session_changed'].includes(error.code) || error.status >= 500;
}
function safeError(error: unknown): Error {
  return error instanceof ApiError
    ? new ApiError(recoveryMessages[error.code] || 'The change could not be confirmed. Refresh the current backup and job details before trying again.', { code: error.code, status: error.status, requestId: error.requestId && /^[A-Za-z0-9_-]{1,128}$/.test(error.requestId) ? error.requestId : undefined })
    : new Error('The server could not be reached. Reconnect to check the actual job state.');
}
function passphraseIssue(value: string): string | undefined {
  try { validateBackupPassphrase(value); return undefined; }
  catch { return 'Use 12–1024 UTF-8 bytes. Some characters use more than one byte. Invalid text characters are not allowed.'; }
}
function StateChip({ state }: { state: string }) {
  return <Chip size="small" label={readableState(state)} color={state === 'completed' || state === 'ready' ? 'success' : state === 'failed' || state === 'interrupted' ? 'error' : state === 'running' || state === 'applying' ? 'primary' : 'default'} />;
}
function Digest({ value }: { value: string }) {
  return <Typography component="div" variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>SHA-256: <span className="mono">{value || 'Available when the backup is ready'}</span></Typography>;
}
function SourceDetails({ source }: { source: SourceView | null }) {
  if (!source) return <Typography variant="body2" color="text.secondary">Source details are available after the backup has been checked.</Typography>;
  return <Stack spacing={2}>
    <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
      {[
        ['Source server', source.ServerName || 'Unnamed server'], ['Server ID', source.ServerId],
        ['Goby version', source.GobyVersion], ['Data format version', source.SchemaVersion], ['Backup created', dateTime(source.CreatedAt)],
      ].map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{value || 'Not recorded'}</Typography></Box>)}
    </Box>
    <Box><Typography variant="h4" component="h3" sx={{ mb: 1 }}>Included records</Typography><Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', columnGap: 2, rowGap: 0.6 }}>
      {source.Tables.map((table) => <Box key={table.Name} sx={{ display: 'contents' }}><Typography component="dt" variant="body2" sx={{ overflowWrap: 'anywhere' }}>{table.Name}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, fontVariantNumeric: 'tabular-nums' }}>{count(table.Rows)}</Typography></Box>)}
    </Box></Box>
  </Stack>;
}

function PassphraseFields({ passphrase, confirmation, disabled, onPassphrase, onConfirmation }: { passphrase: string; confirmation?: string; disabled: boolean; onPassphrase: (value: string) => void; onConfirmation?: (value: string) => void }) {
  const issue = passphrase ? passphraseIssue(passphrase) : undefined;
  return <>
    <TextField label="Passphrase" type="password" value={passphrase} onChange={(event) => onPassphrase(event.target.value)} disabled={disabled} required autoFocus fullWidth
      error={Boolean(issue)} helperText={issue || 'Use 12–1024 UTF-8 bytes. Spaces and letter case are preserved exactly.'}
      slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false, autoCapitalize: 'none', maxLength: 1024 } }} />
    {onConfirmation && <TextField label="Confirm passphrase" type="password" value={confirmation ?? ''} onChange={(event) => onConfirmation(event.target.value)} disabled={disabled} required fullWidth
      error={Boolean(confirmation && confirmation !== passphrase)} helperText={confirmation && confirmation !== passphrase ? 'The passphrases do not match.' : 'Keep this passphrase somewhere safe. Goby cannot recover it.'}
      slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false, autoCapitalize: 'none', maxLength: 1024 } }} />}
  </>;
}

const backupPolicyStates: Record<string, string> = {
  pending: 'Queued', running: 'Running', publishing: 'Saving backup', completed: 'Completed', failed: 'Failed',
  interrupted: 'Interrupted', deleting: 'Deleting', deleted: 'Deleted', restored: 'Restored', stopping: 'Stopping', cancelled: 'Cancelled',
};
const backupPolicyFocus: Record<string, string> = {
  KeepCount: 'backup-policy-keep', IncludeImages: 'backup-policy-images',
  Passphrase: 'backup-policy-passphrase', Confirmation: 'backup-policy-confirmation',
};

function backupTaskURL(taskId?: string): string {
  return taskId ? `/admin/system/tasks?task=${encodeURIComponent(taskId)}` : '/admin/system/tasks';
}

function BackupTaskSummary({ task, loading, error, linked, expanded = false }: { task?: TaskDefinition; loading: boolean; error?: unknown; linked: boolean; expanded?: boolean }) {
  const { t } = useTranslation();
  if (loading) return <span className="backup-schedule-summary">{t('Loading backup trigger rules...')}</span>;
  if (error != null) return <span className="backup-schedule-summary is-unavailable">{t('Backup trigger rules could not be loaded.')}</span>;
  if (!task) return <span className="backup-schedule-summary">{t(linked ? 'Backup trigger rules are unavailable.' : 'No linked backup task. View Tasks to manage backup triggers.')}</span>;
  return <div className="backup-schedule-summary">
    {expanded && task.Triggers.length ? <ul>{task.Triggers.map((trigger) => <li key={trigger.Id}><span>{taskTriggerLabel(trigger, t)}</span>{trigger.CalculationError && <span className="backup-policy-field-error">{t('The server could not calculate this trigger. Review it in Tasks.')}</span>}</li>)}</ul>
      : <span>{taskScheduleSummary(task, t)}</span>}
    {!task.Enabled && <span className="backup-schedule-disabled">{t('Task disabled')}</span>}
    {expanded && <span className="backup-schedule-next">{t('Next scheduled backup')}: {task.NextRunAt ? scheduleDate(task.NextRunAt, task.ScheduleTimezone) : t('No scheduled backup')}</span>}
  </div>;
}

function BackupPolicyDrawer({ onClose, onSaved, onNavigationGuardChange }: {
  onClose: () => void; onSaved: (policy: BackupPolicy) => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t } = useTranslation();
  const [saved, setSaved] = useState<BackupPolicy>();
  const [task, setTask] = useState<TaskDefinition>();
  const [taskError, setTaskError] = useState<unknown>();
  const [taskLoading, setTaskLoading] = useState(false);
  const [draft, setDraft] = useState<BackupPolicyDraft>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  const [review, setReview] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState('');
  const [revision, setRevision] = useState(0);
  const mounted = useRef(true);
  const mutation = useRef<AbortController | null>(null);
  const focusFrame = useRef<number | undefined>(undefined);
  const form = useRef<HTMLFormElement>(null);
  const dirty = Boolean(saved && draft && JSON.stringify(draft) !== JSON.stringify(backupPolicyDraft(saved)));
  const parsed = saved && draft ? parseBackupPolicyDraft(draft, saved) : undefined;
  const disabled = loading || busy || stale || review;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved backup settings and leave this page?');
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; mutation.current?.abort(); if (focusFrame.current !== undefined) cancelAnimationFrame(focusFrame.current); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setNotice(''); setTask(undefined); setTaskError(undefined); setTaskLoading(true);
    void backupPolicyApi.get({ signal: controller.signal }).then(async (policy) => {
      if (controller.signal.aborted) return;
      setSaved(policy); setDraft(backupPolicyDraft(policy)); setStale(false); setReview(false); setLoading(false);
      if (policy.TaskId) {
        try {
          const response = await adminApi.getTask(policy.TaskId, { signal: controller.signal });
          if (!controller.signal.aborted) setTask(response.Task);
        } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setTaskError(cause); }
      }
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); setStale(true); }
    }).finally(() => { if (!controller.signal.aborted) { setLoading(false); setTaskLoading(false); } });
    return () => controller.abort();
  }, [revision]);

  function change(patch: Partial<BackupPolicyDraft>) {
    if (disabled) return;
    setDraft((value) => value ? { ...value, ...patch } : value); setError(null); setNotice('');
  }
  function close() {
    if (mutation.current || busy || dirty && !confirmAction('Discard unsaved backup settings?')) return;
    setDraft(undefined); onClose();
  }
  function reload() {
    if (mutation.current || loading || busy || dirty && !confirmAction('Discard your draft and reload saved backup settings?')) return;
    setRevision((value) => value + 1);
  }
  function focusIssue(issues: Record<string, string>) {
    if (focusFrame.current !== undefined) cancelAnimationFrame(focusFrame.current);
    focusFrame.current = requestAnimationFrame(() => {
      const field = Object.keys(issues).find((key) => backupPolicyFocus[key]);
      const id = field ? backupPolicyFocus[field] : 'backup-policy-error';
      (document.getElementById(id) ?? (field === 'Passphrase' ? document.getElementById('backup-policy-secret-mode') : null)
        ?? form.current?.querySelector<HTMLElement>('[aria-invalid="true"]'))?.focus();
    });
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (mutation.current || disabled || !dirty || !draft || !saved) return;
    const result = parseBackupPolicyDraft(draft, saved);
    if (!result.input) { focusIssue(result.errors); return; }
    const controller = new AbortController(); mutation.current = controller;
    setBusy(true); setError(null); setNotice('');
    try {
      const policy = await backupPolicyApi.update(result.input, { signal: controller.signal });
      if (!mounted.current || controller.signal.aborted) return;
      setSaved(policy); setDraft(backupPolicyDraft(policy)); setReview(false); setStale(false);
      setNotice('Backup settings saved.'); onSaved(policy);
    } catch (cause) {
      if (!mounted.current || controller.signal.aborted || isAbortError(cause)) return;
      setError(cause);
      const fields: Record<string, string> = {};
      for (const key of Object.keys(backupPolicyFocus)) { const issue = fieldError(cause, key); if (issue) fields[key] = issue; }
      focusIssue(fields);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500
        || ['network_error', 'invalid_response', 'session_changed'].includes(cause.code)) {
        setReview(true);
        setDraft((value) => value ? { ...value, passphrase: '', confirmation: '' } : value);
      }
    } finally { if (mutation.current === controller) mutation.current = null; if (mounted.current && !controller.signal.aborted) setBusy(false); }
  }
  const issue = (name: string) => fieldError(error, name) ?? parsed?.errors[name];

  return <Dialog open centered className="backup-policy-modal" onClose={close} disableEscapeKeyDown={busy} fullWidth maxWidth="sm"
    sx={{ width: 'min(480px, calc(100vw - 24px))' }} aria-labelledby="backup-policy-title" aria-describedby="backup-policy-description">
    <form ref={form} onSubmit={(event) => void save(event)} noValidate className="backup-policy-form" aria-busy={loading || busy}>
      <DialogTitle id="backup-policy-title">Backup settings</DialogTitle>
      <DialogContent>
        <p id="backup-policy-description" className="backup-policy-description">{t('Choose backup contents, retention, and encryption. Manage automatic trigger rules in Tasks.')}</p>
        {loading && <div role="status" aria-label={t('Loading backup settings')} className="backup-policy-loading"><Skeleton height={90} /><Skeleton height={150} /></div>}
        {error != null && <div id="backup-policy-error" tabIndex={-1}><ErrorNotice error={error} retry={!saved ? reload : undefined} /></div>}
        {review && <Alert severity="warning">{t('The settings changed or the save result could not be confirmed. Reload the saved settings before making another change. This request is not submitted again automatically.')}</Alert>}
        {stale && saved && <Alert severity="warning">{t('Showing the last confirmed backup settings. Reload before editing.')}</Alert>}
        {notice && <Alert severity="success" role="status">{t(notice)}</Alert>}
        {saved && draft && <>
          {!saved.Available && <Alert severity="warning">{t(recoveryMessages[saved.UnavailableReason] ?? 'Automatic backup creation is unavailable. Check recovery readiness in Backups before running this task.')}</Alert>}
          <section className="backup-policy-section" aria-labelledby="backup-policy-schedule-heading">
            <div className="backup-policy-section-heading"><h3 id="backup-policy-schedule-heading">{t('Automatic backups')}</h3>
              <Button size="small" variant="outlined" href={backupTaskURL(saved.TaskId)} disabled={disabled || dirty} onClick={(event) => { if (disabled || dirty) event.preventDefault(); }}>{saved.TaskId ? 'Trigger rules' : 'View tasks'}</Button>
            </div>
            <BackupTaskSummary task={task} loading={taskLoading} error={taskError} linked={Boolean(saved.TaskId)} expanded />
            {taskError != null && <ErrorNotice error={taskError} retry={reload} />}
            {saved.TaskId && <p className="backup-policy-note">{t('Backup task')}: <a href={backupTaskURL(saved.TaskId)} onClick={(event) => { if (disabled || dirty) event.preventDefault(); }} aria-disabled={disabled || dirty} tabIndex={disabled || dirty ? -1 : undefined}><span className="mono">{saved.TaskId}</span></a></p>}
            <p className="backup-policy-note">{t(dirty ? 'Save or reload this settings draft before editing trigger rules.' : 'Trigger rules and their time zone are edited in Tasks. Saving these settings preserves every trigger rule.')}</p>
          </section>
          <section className="backup-policy-section" aria-labelledby="backup-policy-retention-heading">
            <h3 id="backup-policy-retention-heading">{t('Retention and contents')}</h3>
            <div className="backup-policy-retention-presets" role="group" aria-label={t('Automatic backup retention presets')}>{[7, 14, 30].map((keep) => <Button key={keep} type="button" size="small" variant={draft.keepCount === String(keep) ? 'contained' : 'outlined'} aria-pressed={draft.keepCount === String(keep)} disabled={disabled} onClick={() => change({ keepCount: String(keep) })}>{t('Keep {count}', { count: keep })}</Button>)}</div>
            <TextField id="backup-policy-keep" fullWidth type="number" label="Automatic backups to keep" value={draft.keepCount} disabled={disabled}
              onChange={(event) => change({ keepCount: event.target.value })} error={Boolean(issue('KeepCount'))} helperText={issue('KeepCount') ?? 'Keep between 1 and 30 completed automatic backups.'}
              slotProps={{ htmlInput: { min: 1, max: 30, step: 1, inputMode: 'numeric' } }} />
            <p className="backup-policy-note">{t('Retention removes only completed automatic backups created by this deployment. Manual and imported backups are kept.')}</p>
            <FormControlLabel control={<Switch id="backup-policy-images" checked={draft.includeImages} disabled={disabled} onChange={(event) => change({ includeImages: event.target.checked })}
              inputProps={{ 'aria-describedby': 'backup-policy-images-note' }} />} label="Include library images and avatars" />
            <p id="backup-policy-images-note" className="backup-policy-note">{t(draft.includeImages
              ? 'Include uploaded and downloaded library images, cached embedded covers, user avatars, and manual image overrides.'
              : 'Library images, cached covers, user avatars, and manual image overrides will be excluded. Restore will use source-folder images when available.')}</p>
            <p className="backup-policy-note">{t('Media files and images stored beside media files are never copied. File references remain included.')}</p>
          </section>
          <section className="backup-policy-section" aria-labelledby="backup-policy-encryption-heading">
            <h3 id="backup-policy-encryption-heading">{t('Backup encryption')}</h3>
            <p className="backup-policy-note">{t('Keep a separate copy of the passphrase. You will need it to restore an automatic backup.')}</p>
            <TextField id="backup-policy-secret-mode" fullWidth select label="Saved passphrase action" value={draft.passphraseMode} disabled={disabled}
              onChange={(event) => change({ passphraseMode: event.target.value as BackupPolicyDraft['passphraseMode'], passphrase: '', confirmation: '' })}
              helperText={saved.HasPassphrase ? 'An encryption passphrase is saved. Its contents are never returned.' : 'No automatic backup passphrase has been saved.'}>
              <MenuItem value="keep">Keep saved passphrase</MenuItem><MenuItem value="replace">Set a new passphrase</MenuItem><MenuItem value="clear">Clear saved passphrase</MenuItem>
            </TextField>
            {draft.passphraseMode === 'replace' ? <>
              <PasswordField id="backup-policy-passphrase" fullWidth label="New backup passphrase" value={draft.passphrase} disabled={disabled}
                onChange={(event) => change({ passphrase: event.target.value })} error={Boolean(issue('Passphrase'))}
                helperText={issue('Passphrase') ?? 'Use 12–1024 UTF-8 bytes. Spaces and letter case are preserved exactly.'}
                slotProps={{ htmlInput: { autoComplete: 'new-password', spellCheck: false, autoCapitalize: 'none', maxLength: 1024 } }} />
              <PasswordField id="backup-policy-confirmation" fullWidth label="Confirm new backup passphrase" value={draft.confirmation} disabled={disabled}
                onChange={(event) => change({ confirmation: event.target.value })} error={Boolean(issue('Confirmation'))} helperText={issue('Confirmation')}
                slotProps={{ htmlInput: { autoComplete: 'new-password', spellCheck: false, autoCapitalize: 'none', maxLength: 1024 } }} />
            </> : issue('Passphrase') && <Alert severity="error">{t(issue('Passphrase')!)}</Alert>}
            {draft.passphraseMode === 'clear' && <Alert severity="warning">{t(task?.Triggers.length ? 'Remove all automatic backup trigger rules in Tasks before clearing the saved passphrase.' : 'Saving will remove the stored automatic backup passphrase.')}</Alert>}
            <p className="backup-policy-note">{t('Manual backups always ask for their own passphrase. The saved automatic backup passphrase is never reused in this form.')}</p>
          </section>
          <section className="backup-policy-section backup-policy-current" aria-labelledby="backup-policy-current-heading">
            <h3 id="backup-policy-current-heading">{t('Last automatic backup')}</h3>
            <dl>
              <div><dt>{t('Last automatic run')}</dt><dd>{saved.LastRunAt ? dateTime(saved.LastRunAt) : t('Not recorded')}</dd></div>
              {saved.LastRunState && <div><dt>{t('Last run status')}</dt><dd>{t(backupPolicyStates[saved.LastRunState] ?? 'Not recorded')}</dd></div>}</dl>
            <p className="backup-policy-note">{t('Status times use your current time zone. Changes apply after saving.')}</p>
          </section>
        </>}
      </DialogContent>
      <DialogActions>
        <Button type="button" onClick={reload} disabled={loading || busy} startIcon={<RefreshRounded />}>Reload saved settings</Button>
        <span className="backup-policy-actions-spacer" />
        <Button type="button" onClick={close} disabled={busy}>Close</Button>
        <Button type="submit" variant="contained" disabled={disabled || !dirty || !saved || !draft} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving backup settings...' : 'Save backup settings'}</Button>
      </DialogActions>
    </form>
  </Dialog>;
}

function BackupAdmissionDialog({ kind, status, includeImages, onAttempt, onRejected, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { kind: 'create' | 'import'; status: StatusView; includeImages?: boolean }) {
  const { t } = useTranslation();
  const [passphrase, setPassphrase] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [file, setFile] = useState<File>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const controller = useRef<AbortController | undefined>(undefined);
  const requestId = useRef<string | undefined>(undefined);
  const fileInput = useRef<HTMLInputElement>(null);
  const dirty = Boolean(passphrase || confirmation || file);
  const fileIssue = file && (file.size === 0 ? 'Choose a complete, nonempty Goby backup file.' : BigInt(file.size) > BigInt(status.Limits.MaxBackupBytes) ? `The file exceeds the ${bytes(status.Limits.MaxBackupBytes)} backup limit.` : undefined);
  const valid = kind === 'create' ? typeof includeImages === 'boolean' && Boolean(passphrase && !passphraseIssue(passphrase) && passphrase === confirmation) : Boolean(file && !fileIssue);
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard this backup form and leave this page?');
  useEffect(() => () => { controller.current?.abort(); if (fileInput.current) fileInput.current.value = ''; }, []);
  function close() {
    if (controller.current || (dirty && !confirmAction('Discard this backup form?'))) return;
    setPassphrase(''); setConfirmation(''); setFile(undefined); onClose();
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!valid || controller.current) return;
    const pending = new AbortController(); controller.current = pending; setBusy(true); setError(undefined);
    let recorded = false;
    try {
      requestId.current ??= newBackupRequestId();
      onAttempt({ RequestId: requestId.current, Kind: kind });
      recorded = true;
      const operation = kind === 'create'
        ? await backupsApi.createBackup({ RequestId: requestId.current, Passphrase: passphrase, IncludeImages: includeImages }, { signal: pending.signal })
        : await backupsApi.importBackup(file!, requestId.current, { signal: pending.signal });
      if (pending.signal.aborted) return;
      setPassphrase(''); setConfirmation(''); setFile(undefined);
      onAccepted(operation);
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) {
        setPassphrase(''); setConfirmation(''); setFile(undefined);
        onUnknown({ RequestId: requestId.current, Kind: kind });
      } else { if (recorded) onRejected(); setError(safeError(cause)); requestId.current = undefined; }
    } finally { if (controller.current === pending) controller.current = undefined; if (!pending.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="backup-admission-title" aria-describedby="backup-admission-description"
    slotProps={{ transition: { onEntered: () => document.getElementById('backup-admission-title')?.closest('[role="dialog"]')?.querySelector<HTMLInputElement>('input')?.focus() } }}>
    <Box component="form" onSubmit={submit} aria-busy={busy}>
      <DialogTitle id="backup-admission-title" sx={{ pt: 3 }}>{kind === 'create' ? 'Create backup' : 'Import backup'}</DialogTitle>
      <DialogContent><Stack spacing={2.5} sx={{ pt: 1 }}>
        <Typography id="backup-admission-description" variant="body2" color="text.secondary">{kind === 'create' ? 'Save an encrypted copy of this server’s data and settings. Media files are not included.' : 'Upload an encrypted Goby backup. Imported files remain unverified until you plan a restore and provide the passphrase.'}</Typography>
        {error != null && <ErrorNotice error={error} />}
        {kind === 'create' ? <>
          <Alert severity="info">{t(includeImages ? 'This manual backup includes library images and avatars, using the saved backup contents setting.' : 'This manual backup excludes library images and avatars, using the saved backup contents setting.')}</Alert>
          <Typography variant="body2" color="text.secondary">Manual backups use the passphrase entered below and are not removed by automatic backup retention.</Typography>
          <PassphraseFields passphrase={passphrase} confirmation={confirmation} disabled={busy} onPassphrase={setPassphrase} onConfirmation={setConfirmation} />
        </> : <>
          <TextField label="Backup file" type="file" inputRef={fileInput} fullWidth required disabled={busy} onChange={(event) => setFile((event.target as HTMLInputElement).files?.[0])}
            error={Boolean(fileIssue)} helperText={fileIssue || `Maximum file size: ${bytes(status.Limits.MaxBackupBytes)}. An interrupted upload must start again.`}
            slotProps={{ inputLabel: { shrink: true }, htmlInput: { accept: '.age,application/octet-stream' } }} />
          {file && <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{file.name} · {bytes(String(file.size))}</Typography>}
        </>}
        {busy && <Alert severity="info" role="status">{kind === 'import' ? 'Uploading the file. Keep this page open until the server acknowledges the job.' : 'Waiting for the server to acknowledge the job.'} Admission does not mean the backup is complete.</Alert>}
      </Stack></DialogContent>
      <DialogActions sx={{ px: 3, pb: 3 }}><Button onClick={close} disabled={busy} color="secondary">Close</Button><Button type="submit" variant="contained" disabled={busy || !valid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{kind === 'create' ? 'Create backup' : 'Import backup'}</Button></DialogActions>
    </Box>
  </Dialog>;
}

function RestorePlanDialog({ backup, status, onAttempt, onRejected, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { backup: BackupView; status: StatusView }) {
  const [passphrase, setPassphrase] = useState('');
  const [restoreDefaults, setRestoreDefaults] = useState(false);
  const [replaceRollback, setReplaceRollback] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const controller = useRef<AbortController | undefined>(undefined);
  const requestId = useRef<string | undefined>(undefined);
  const dirty = Boolean(passphrase || restoreDefaults || replaceRollback);
  const valid = Boolean(passphrase && !passphraseIssue(passphrase) && (!status.Rollback.MustReplace || replaceRollback));
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard the restore form and leave this page?');
  useEffect(() => () => controller.current?.abort(), []);
  function close() { if (!controller.current && (!dirty || confirmAction('Discard the restore form?'))) { setPassphrase(''); onClose(); } }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!valid || controller.current) return;
    const pending = new AbortController(); controller.current = pending; setBusy(true); setError(undefined);
    let recorded = false;
    try {
      requestId.current ??= newBackupRequestId();
      onAttempt({ RequestId: requestId.current, Kind: 'restore' });
      recorded = true;
      const operation = await backupsApi.planRestore({ RequestId: requestId.current, BackupId: backup.Id, SHA256: backup.SHA256, Passphrase: passphrase, RestoreDefaults: restoreDefaults, ReplaceRollback: status.Rollback.MustReplace && replaceRollback, GenerationRevision: status.GenerationRevision }, { signal: pending.signal });
      if (!pending.signal.aborted) { setPassphrase(''); onAccepted(operation); }
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) { setPassphrase(''); onUnknown({ RequestId: requestId.current, Kind: 'restore' }); }
      else { if (recorded) onRejected(); setError(safeError(cause)); requestId.current = undefined; }
    } finally { if (controller.current === pending) controller.current = undefined; if (!pending.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="restore-plan-title" aria-describedby="restore-plan-description">
    <Box component="form" onSubmit={submit} aria-busy={busy}>
      <DialogTitle id="restore-plan-title" sx={{ pt: 3 }}>Plan restore</DialogTitle>
      <DialogContent><Stack spacing={2.5} sx={{ pt: 1 }}>
        <Typography id="restore-plan-description" variant="body2" color="text.secondary">Check this backup and prepare its data for review. Your current server stays active until you explicitly apply the ready plan.</Typography>
        <Box sx={{ p: 2, bgcolor: 'background.default', borderRadius: 1 }}><Typography variant="body2">Backup from {dateTime(backup.CreatedAt)}</Typography><Typography variant="caption" className="mono" sx={{ overflowWrap: 'anywhere' }}>{backup.Id}</Typography><Digest value={backup.SHA256} /></Box>
        {error != null && <ErrorNotice error={error} />}
        <PassphraseFields passphrase={passphrase} disabled={busy} onPassphrase={setPassphrase} />
        <Box><FormControlLabel sx={{ alignItems: 'flex-start', ml: -1 }} control={<Checkbox checked={restoreDefaults} onChange={(event) => setRestoreDefaults(event.target.checked)} disabled={busy} />} label="Restore saved defaults" /><Typography variant="body2" color="text.secondary">Restore the backup’s server name and default output settings. Network, storage, hardware, security settings, and approved media folders follow this deployment.</Typography></Box>
        {status.Rollback.MustReplace && <Alert severity="warning"><Typography variant="body2">Preparing another restore removes the previous rollback copy{status.Rollback.CreatedAt ? ` from ${dateTime(status.Rollback.CreatedAt)}` : ''}. This cannot be undone.</Typography><FormControlLabel sx={{ alignItems: 'flex-start', mt: 1 }} control={<Checkbox checked={replaceRollback} onChange={(event) => setReplaceRollback(event.target.checked)} disabled={busy} />} label="Replace the previous rollback copy" /></Alert>}
        <Typography variant="caption" color="text.secondary">Server revision: <span className="mono">{status.GenerationRevision}</span>. If the server changes, refresh and plan again.</Typography>
        {busy && <Alert severity="info" role="status">Waiting for the server to acknowledge the planning job. The backup still needs validation.</Alert>}
      </Stack></DialogContent>
      <DialogActions sx={{ px: 3, pb: 3 }}><Button onClick={close} disabled={busy} color="secondary">Close</Button><Button type="submit" variant="contained" disabled={busy || !valid}>Plan restore</Button></DialogActions>
    </Box>
  </Dialog>;
}

function DeleteBackupDialog({ backup, onAttempt, onRejected, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { backup: BackupView }) {
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const controller = useRef<AbortController | undefined>(undefined);
  const requestId = useRef<string | undefined>(undefined);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => controller.current?.abort(), []);
  async function remove() {
    if (!confirmed || controller.current) return;
    const pending = new AbortController(); controller.current = pending; setBusy(true); setError(undefined);
    let recorded = false;
    try {
      requestId.current ??= newBackupRequestId();
      onAttempt({ RequestId: requestId.current, Kind: 'delete' });
      recorded = true;
      const operation = await backupsApi.deleteBackup(backup.Id, { RequestId: requestId.current, SHA256: backup.SHA256 }, { signal: pending.signal });
      if (!pending.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) onUnknown({ RequestId: requestId.current, Kind: 'delete' });
      else { if (recorded) onRejected(); setError(safeError(cause)); requestId.current = undefined; }
    } finally { if (controller.current === pending) controller.current = undefined; if (!pending.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={() => { if (!busy) onClose(); }} fullWidth maxWidth="sm" aria-labelledby="delete-backup-title">
    <DialogTitle id="delete-backup-title" sx={{ pt: 3 }}>Delete backup</DialogTitle><DialogContent><Stack spacing={2} sx={{ pt: 1 }}>
      <Typography variant="body2">Permanently delete the backup from {dateTime(backup.CreatedAt)} and free {bytes(backup.SizeBytes)}. Download a copy first if you need to keep it.</Typography>
      <Typography variant="caption" className="mono" sx={{ overflowWrap: 'anywhere' }}>{backup.Id}</Typography><Digest value={backup.SHA256} />
      {error != null && <ErrorNotice error={error} />}
      <FormControlLabel control={<Checkbox checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy} />} label="I understand this backup will be permanently deleted" />
    </Stack></DialogContent><DialogActions sx={{ px: 3, pb: 3 }}><Button onClick={onClose} disabled={busy} color="secondary">Keep backup</Button><Button onClick={() => void remove()} variant="contained" color="error" disabled={busy || !confirmed}>Delete backup</Button></DialogActions>
  </Dialog>;
}

function InspectPlanDialog({ operationId, applicationUncertain, onAttempt, onRejected, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { operationId: string; applicationUncertain: boolean }) {
  const [details, setDetails] = useState<{ operation: OperationView; status: StatusView }>();
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(true);
  const [confirming, setConfirming] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const mutation = useRef<AbortController | undefined>(undefined);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setConfirmed(false); setConfirming(false);
    void Promise.all([backupsApi.getOperation(operationId, { signal: controller.signal }), backupsApi.getStatus({ signal: controller.signal })]).then(([operation, status]) => {
      if (!controller.signal.aborted) setDetails({ operation, status });
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(safeError(cause)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [operationId, refresh]);
  const canApply = Boolean(!applicationUncertain && !loading && !error && details?.operation.CanApply && details.operation.State === 'ready' && details.operation.Source && details.operation.GenerationRevision === details.status.GenerationRevision && recoverySwitchAvailable(details.status)
    && (!details.status.Busy || details.status.ActiveOperationId === details.operation.Id));
  async function apply() {
    if (!details || !canApply || !confirmed || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy(true); setError(undefined);
    let recorded = false;
    try {
      onAttempt({ RequestId: details.operation.RequestId, Kind: 'restore', OperationId: operationId, ApplyingRevision: details.operation.Revision });
      recorded = true;
      const operation = await backupsApi.applyRestore(operationId, { Revision: details.operation.Revision, GenerationRevision: details.status.GenerationRevision }, { signal: controller.signal });
      if (!controller.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (controller.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause)) onUnknown({ RequestId: details.operation.RequestId, Kind: 'restore', OperationId: operationId, ApplyingRevision: details.operation.Revision });
      else { if (recorded) onRejected(); setError(safeError(cause)); setConfirmed(false); }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={() => { if (!busy) onClose(); }} fullWidth maxWidth="sm" aria-labelledby="inspect-plan-title">
    <DialogTitle id="inspect-plan-title" sx={{ pt: 3 }}>{confirming ? 'Apply restore' : 'Inspect plan'}</DialogTitle>
    <DialogContent><Stack spacing={2.5} sx={{ pt: 1 }}>
      {loading && <Box role="status" aria-label="Loading restore plan"><Skeleton height={100} /><Skeleton height={160} /></Box>}
      {error != null && <ErrorNotice error={error} retry={busy ? undefined : () => setRefresh((value) => value + 1)} />}
      {!loading && details && <>
        <Stack direction="row" sx={{ gap: 1, alignItems: 'center', flexWrap: 'wrap' }}><StateChip state={details.operation.State} /><Typography variant="body2" color="text.secondary">{phaseLabels[details.operation.Phase]}</Typography></Stack>
        <SourceDetails source={details.operation.Source} />
        <Divider />
        <Typography variant="body2">Saved defaults: <strong>{details.operation.RestoreDefaults ? 'Restore from backup' : 'Keep this deployment’s defaults'}</strong></Typography>
        <Typography variant="body2">Previous rollback copy: <strong>{details.operation.ReplaceRollback ? 'Replacement was explicitly requested' : 'No replacement requested'}</strong></Typography>
        <Box sx={{ p: 2, bgcolor: 'background.default', borderRadius: 1 }}><Typography variant="body2">Plan revision: <span className="mono">{details.operation.Revision}</span></Typography><Typography variant="body2">Server revision: <span className="mono">{details.status.GenerationRevision}</span></Typography><Typography variant="caption" className="mono" sx={{ overflowWrap: 'anywhere' }}>Job {details.operation.Id}</Typography></Box>
        {!canApply && !error && <Alert severity="warning">{applicationUncertain ? 'An earlier activation request still has an unknown outcome. Check the jobs before submitting another request.' : 'This plan cannot be applied now. Refresh the details and check the current job and server state.'}</Alert>}
        {confirming && <><Alert severity="warning">Applying this plan stops active work and starts the server using the checked backup. Your current server data will be kept as a rollback copy. Everyone must sign in again using their restored account passwords.</Alert><FormControlLabel sx={{ alignItems: 'flex-start' }} control={<Checkbox checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy} />} label="I understand that everyone must sign in again" /><Typography variant="body2">Confirm applying plan revision <strong>{details.operation.Revision}</strong> to server revision <strong>{details.status.GenerationRevision}</strong>.</Typography></>}
        {busy && <Alert severity="info" role="status">Waiting for activation to be acknowledged. A disconnected session or accepted request does not confirm completion.</Alert>}
      </>}
    </Stack></DialogContent>
    <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button onClick={onClose} disabled={busy} color="secondary">Close</Button><Button onClick={() => setRefresh((value) => value + 1)} disabled={busy || loading} startIcon={<RefreshRounded />}>Refresh details</Button>{confirming && <Button onClick={() => { setConfirming(false); setConfirmed(false); }} disabled={busy}>Back to review</Button>}<Button variant="contained" color={confirming ? 'warning' : 'primary'} disabled={!canApply || busy || (confirming && !confirmed)} onClick={() => confirming ? void apply() : setConfirming(true)}>Apply restore</Button></DialogActions>
  </Dialog>;
}

function RollbackDialog({ status, onAttempt, onRejected, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { status: StatusView }) {
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const requestId = useRef<string | undefined>(undefined);
  useUserDraftNavigation(false, busy, onNavigationGuardChange);
  useEffect(() => () => mutation.current?.abort(), []);
  async function rollback() {
    if (!confirmed || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy(true); setError(undefined);
    let recorded = false;
    try {
      requestId.current ??= newBackupRequestId();
      onAttempt({ RequestId: requestId.current, Kind: 'rollback' });
      recorded = true;
      const operation = await backupsApi.rollback({ RequestId: requestId.current, GenerationRevision: status.GenerationRevision }, { signal: controller.signal });
      if (!controller.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (controller.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) onUnknown({ RequestId: requestId.current, Kind: 'rollback' });
      else { if (recorded) onRejected(); setError(safeError(cause)); requestId.current = undefined; setConfirmed(false); }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={() => { if (!busy) onClose(); }} fullWidth maxWidth="sm" aria-labelledby="rollback-title">
    <DialogTitle id="rollback-title" sx={{ pt: 3 }}>Roll back</DialogTitle><DialogContent><Stack spacing={2.5} sx={{ pt: 1 }}>
      <Typography variant="body2">Return to the retained copy of <strong>{status.Rollback.ServerName || 'this server'}</strong>{status.Rollback.CreatedAt ? ` from ${dateTime(status.Rollback.CreatedAt)}` : ''}.</Typography>
      <Typography variant="body2">Copy generation: <span className="mono">{status.Rollback.Generation}</span>. Current server revision: <span className="mono">{status.GenerationRevision}</span>.</Typography>
      <Alert severity="warning">Active work will stop. Changes since this copy was saved will no longer be active. Everyone must sign in again with the passwords from the retained copy.</Alert>
      {error != null && <ErrorNotice error={error} />}
      <FormControlLabel sx={{ alignItems: 'flex-start' }} control={<Checkbox checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} disabled={busy} />} label="I understand that everyone must sign in again" />
      {busy && <Alert severity="info" role="status">Waiting for rollback to be acknowledged. Check the completed job after reconnecting.</Alert>}
    </Stack></DialogContent><DialogActions sx={{ px: 3, pb: 3 }}><Button onClick={onClose} disabled={busy} color="secondary">Close</Button><Button onClick={() => void rollback()} variant="contained" color="warning" disabled={busy || !confirmed}>Roll back</Button></DialogActions>
  </Dialog>;
}

async function findAttempt(attempt: AdmissionAttempt, first: OperationPage, signal: AbortSignal): Promise<OperationView | undefined> {
  const current = first.Items.find((operation) => backupAttemptMatchesOperation(attempt, operation));
  if (current) return current;
  const initial = await backupsApi.getOperations({ StartIndex: 0, Limit: 100 }, { signal });
  const found = initial.Items.find((operation) => backupAttemptMatchesOperation(attempt, operation));
  if (found) return found;
  const seen = new Set(initial.Items.map((operation) => operation.Id));
  if (initial.Items.length !== Math.min(initial.Limit, initial.TotalRecordCount)) throw new ApiError('The job list changed during reconciliation.', { code: 'source_changed' });
  for (let start = 100; start < initial.TotalRecordCount; start += 100) {
    const page = await backupsApi.getOperations({ StartIndex: start, Limit: 100 }, { signal });
    const operation = page.Items.find((item) => backupAttemptMatchesOperation(attempt, item));
    if (operation) return operation;
    if (page.TotalRecordCount !== initial.TotalRecordCount || page.Items.length !== Math.min(page.Limit, initial.TotalRecordCount - start)
      || page.Items.some((item) => seen.has(item.Id))) throw new ApiError('The job list changed during reconciliation.', { code: 'source_changed' });
    page.Items.forEach((item) => seen.add(item.Id));
  }
  return undefined;
}

export function BackupsPage({ currentUserId, onNavigationGuardChange }: { currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t } = useTranslation();
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [loadError, setLoadError] = useState<unknown>();
  const [actionError, setActionError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [backupStart, setBackupStart] = useState(0);
  const [operationStart, setOperationStart] = useState(0);
  const [selectedBackupId, setSelectedBackupId] = useState<string>();
  const [dialog, setDialog] = useState<DialogState>();
  const [tracked, setTracked] = useState<OperationView>();
  const [unknownAttempt, setUnknownAttempt] = useState<AdmissionAttempt>();
  const [receiptScope, setReceiptScope] = useState<BackupReceiptScope>();
  const [identityRevision, setIdentityRevision] = useState(0);
  const [serverVersion, setServerVersion] = useState<string>();
  const [policy, setPolicy] = useState<BackupPolicy>();
  const [policyError, setPolicyError] = useState<unknown>();
  const [policyLoading, setPolicyLoading] = useState(true);
  const [policyRevision, setPolicyRevision] = useState(0);
  const [backupTask, setBackupTask] = useState<TaskDefinition>();
  const [backupTaskError, setBackupTaskError] = useState<unknown>();
  const [backupTaskLoading, setBackupTaskLoading] = useState(true);
  const pendingAttempt = useRef<AdmissionAttempt | undefined>(undefined);
  const [attemptNotFound, setAttemptNotFound] = useState(false);
  const [notice, setNotice] = useState('');
  const [focusVersion, setFocusVersion] = useState(0);
  const [actionBusy, setActionBusy] = useState('');
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  const action = useRef<AbortController | undefined>(undefined);
  const readController = useRef<AbortController | undefined>(undefined);
  const snapshotVersion = useRef(0);
  const trackedId = tracked?.Id;
  const reload = () => {
    readController.current?.abort(); snapshotVersion.current += 1; setLoading(true);
    setPolicyRevision((value) => value + 1);
    if (receiptScope) setRefresh((value) => value + 1);
    else setIdentityRevision((value) => value + 1);
  };
  useEffect(() => () => action.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    // Resolve the server identity before enabling any recovery action. A receipt
    // from another administrator or another server at this origin is independent.
    void adminApi.getOverview({ signal: controller.signal }).then((overview) => {
      if (controller.signal.aborted) return;
      const scope = { ActorId: currentUserId, ServerId: overview.Server?.Id };
      setServerVersion(typeof overview.Server?.Version === 'string' && overview.Server.Version ? overview.Server.Version : undefined);
      const attempt = readBackupAttempt(scope);
      pendingAttempt.current = attempt;
      setUnknownAttempt(attempt);
      setReceiptScope(scope);
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted && !isAbortError(cause)) { setLoadError(safeError(cause)); setLoading(false); }
    });
    return () => controller.abort();
  }, [currentUserId, identityRevision]);
  useEffect(() => {
    const controller = new AbortController();
    setPolicyLoading(true); setPolicyError(undefined); setBackupTask(undefined); setBackupTaskError(undefined); setBackupTaskLoading(true);
    void backupPolicyApi.get({ signal: controller.signal }).then(async (savedPolicy) => {
      if (controller.signal.aborted) return;
      setPolicy(savedPolicy); setPolicyLoading(false);
      if (savedPolicy.TaskId) {
        try {
          const response = await adminApi.getTask(savedPolicy.TaskId, { signal: controller.signal });
          if (!controller.signal.aborted) setBackupTask(response.Task);
        } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setBackupTaskError(cause); }
      }
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setPolicyError(cause); })
      .finally(() => { if (!controller.signal.aborted) { setPolicyLoading(false); setBackupTaskLoading(false); } });
    return () => controller.abort();
  }, [currentUserId, policyRevision]);
  useEffect(() => {
    if (focusVersion === 0) return;
    const frame = requestAnimationFrame(() => {
      if (!document.querySelector('[role="dialog"]')) (document.getElementById('backup-admission-notice') || document.getElementById('backup-jobs-heading'))?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [focusVersion]);
  useEffect(() => {
    const changed = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, []);
  useEffect(() => {
    if (!receiptScope) return;
    const scope: BackupReceiptScope = receiptScope;
    const controller = new AbortController(); setLoading(true);
    readController.current = controller;
    const version = ++snapshotVersion.current;
    async function read() {
      try {
        const [receivedStatus, backups, operations] = await Promise.all([
          backupsApi.getStatus({ signal: controller.signal }),
          backupsApi.getBackups({ StartIndex: backupStart, Limit: pageSize }, { signal: controller.signal }),
          backupsApi.getOperations({ StartIndex: operationStart, Limit: pageSize }, { signal: controller.signal }),
        ]);
        let status = receivedStatus;
        if (controller.signal.aborted || version !== snapshotVersion.current) return;
        if (backupStart && backupStart >= backups.TotalRecordCount) { setBackupStart(Math.max(0, Math.ceil(backups.TotalRecordCount / pageSize) - 1) * pageSize); return; }
        if (operationStart && operationStart >= operations.TotalRecordCount) { setOperationStart(Math.max(0, Math.ceil(operations.TotalRecordCount / pageSize) - 1) * pageSize); return; }
        const followId = status.ActiveOperationId || trackedId;
        let selected = followId ? operations.Items.find((operation) => operation.Id === followId) : undefined;
        if (!selected && followId) selected = await backupsApi.getOperation(followId, { signal: controller.signal });
        if (unknownAttempt) {
          const admitted = await findAttempt(unknownAttempt, operations, controller.signal);
          if (!admitted) status = await backupsApi.getStatus({ signal: controller.signal });
          if (controller.signal.aborted || version !== snapshotVersion.current) return;
          if (admitted && backupAttemptResolvedByOperation(unknownAttempt, admitted)) {
            removeBackupAttempt(scope, unknownAttempt);
            if (pendingAttempt.current && sameBackupAttempt(pendingAttempt.current, unknownAttempt)) pendingAttempt.current = undefined;
            selected = admitted; setUnknownAttempt(undefined); setAttemptNotFound(false); setNotice('The server acknowledged the job. Follow its current state below.');
          }
          else setAttemptNotFound(!admitted && !status.Busy && unknownAttempt.ApplyingRevision === undefined);
        }
        if (controller.signal.aborted || version !== snapshotVersion.current) return;
        if (selected) setTracked(selected);
        setSnapshot({ status, backups, operations }); setLoadError(undefined);
      } catch (cause) { if (!controller.signal.aborted && version === snapshotVersion.current && !isAbortError(cause)) { setLoadError(safeError(cause)); setAttemptNotFound(false); } }
      finally { if (!controller.signal.aborted && version === snapshotVersion.current) setLoading(false); }
    }
    void read();
    return () => { controller.abort(); if (readController.current === controller) readController.current = undefined; };
  }, [backupStart, operationStart, refresh, trackedId, unknownAttempt, receiptScope]);
  const knownActive = snapshot?.operations.Items.some(activeOperation) || Boolean(tracked && activeOperation(tracked));
  // Parallel responses can observe completion before the backup list catches up.
  const changingBackup = snapshot?.backups.Items.some((backup) => backup.State === 'writing' || backup.State === 'deleting');
  const deletedBackupStillListed = [tracked, ...(snapshot?.operations.Items ?? [])].some((operation) => operation?.Kind === 'delete'
    && operation.State === 'completed' && snapshot?.backups.Items.some((backup) => backup.Id === operation.BackupId));
  const waitingOnReadyPlan = Boolean(snapshot?.status.ActiveOperationId && [tracked, ...(snapshot?.operations.Items ?? [])].some((operation) => operation?.Id === snapshot.status.ActiveOperationId && operation.State === 'ready'));
  const polling = visible && (knownActive || changingBackup || deletedBackupStillListed || Boolean(snapshot?.status.Busy && !waitingOnReadyPlan) || Boolean(unknownAttempt));
  useEffect(() => {
    if (!polling || loading) return;
    const timer = window.setTimeout(() => setRefresh((value) => value + 1), unknownAttempt ? 5000 : 3000);
    return () => window.clearTimeout(timer);
  }, [polling, loading, refresh, unknownAttempt]);
  function clearAttempt(): boolean {
    if (!pendingAttempt.current) return true;
    if (!receiptScope) return false;
    try {
      removeBackupAttempt(receiptScope, pendingAttempt.current);
      pendingAttempt.current = undefined;
      setUnknownAttempt(undefined);
      return true;
    } catch (cause) {
      setUnknownAttempt(pendingAttempt.current);
      setLoadError(safeError(cause));
      return false;
    }
  }
  function beginAttempt(attempt: AdmissionAttempt) {
    if (!receiptScope) throw new ApiError('Recovery identity is not available. Reconnect before continuing.', { code: 'backup_receipt_unavailable' });
    storeBackupAttempt(receiptScope, attempt);
    pendingAttempt.current = attempt;
  }
  function accepted(operation: OperationView) {
    snapshotVersion.current += 1;
    const attempt = pendingAttempt.current;
    if (attempt && backupAttemptMatchesOperation(attempt, operation)) {
      if (!backupAttemptResolvedByOperation(attempt, operation)) { unknown(attempt); return; }
      if (!clearAttempt()) { setDialog(undefined); reload(); return; }
    }
    setTracked(operation); setDialog(undefined); setActionError(undefined);
    setFocusVersion((value) => value + 1);
    setAttemptNotFound(false);
    setNotice('The server acknowledged the job. Follow its current state below.'); reload();
  }
  function unknown(attempt: AdmissionAttempt) {
    snapshotVersion.current += 1;
    setAttemptNotFound(false);
    setDialog(undefined); setUnknownAttempt(attempt); setNotice(''); setActionError(undefined); reload();
    setFocusVersion((value) => value + 1);
  }
  function startNewAttempt() {
    if (!unknownAttempt || !attemptNotFound || unknownAttempt.ApplyingRevision !== undefined || snapshot?.status.Busy || loading || loadError) return;
    if (unknownAttempt.Kind === 'create' && (!policy || policyLoading || policyError != null)) return;
    snapshotVersion.current += 1;
    const kind = unknownAttempt.Kind;
    if (!clearAttempt()) return;
    setAttemptNotFound(false);
    setNotice('A new attempt will use a new request ID. The earlier request may still appear in the job list.');
    if (kind === 'create' && policy) setDialog({ kind, includeImages: policy.IncludeImages });
    else if (kind === 'import') setDialog({ kind });
    reload();
  }
  async function cancel(operation: OperationView) {
    if (!operation.CanCancel || action.current) return;
    const controller = new AbortController(); action.current = controller; setActionBusy(operation.Id); setActionError(undefined);
    try {
      const result = await backupsApi.cancelOperation(operation.Id, { Revision: operation.Revision }, { signal: controller.signal });
      if (!controller.signal.aborted) accepted(result);
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setActionError(safeError(cause)); reload(); } }
    finally { if (action.current === controller) action.current = undefined; if (!controller.signal.aborted) setActionBusy(''); }
  }
  async function download(backup: BackupView) {
    if (action.current) return;
    const controller = new AbortController(); action.current = controller; setActionBusy(backup.Id); setActionError(undefined);
    try {
      const url = await backupsApi.prepareDownload(backup.Id, { signal: controller.signal });
      if (controller.signal.aborted) return;
      const anchor = document.createElement('a'); anchor.href = url; anchor.rel = 'noopener'; document.body.append(anchor); anchor.click(); anchor.remove();
      setNotice('The browser download was requested. Check your browser’s downloads for completion.');
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setActionError(safeError(cause)); }
    finally { if (action.current === controller) action.current = undefined; if (!controller.signal.aborted) setActionBusy(''); }
  }
  // Tool availability gates creation and restore planning. Managing stored
  // archives only requires the common service and interaction guards.
  const unavailable = !receiptScope || !snapshot || snapshot.status.UnavailableReason === 'storage_unavailable' || snapshot.status.UnavailableReason === 'recovery_required' || Boolean(loadError) || Boolean(actionBusy) || Boolean(unknownAttempt) || loading;
  const admissionBusy = unavailable || Boolean(snapshot?.status.Busy);
  const createBusy = admissionBusy || policyLoading || policyError != null || !policy;
  const operations = snapshot ? [ ...(tracked && !snapshot.operations.Items.some((operation) => operation.Id === tracked.Id) ? [tracked] : []), ...snapshot.operations.Items.map((operation) => tracked?.Id === operation.Id ? tracked : operation) ] : [];
  const recovering = operations.some((operation) => (operation.Kind === 'restore' || operation.Kind === 'rollback') && (operation.State === 'applying' || operation.Phase === 'activation' || operation.Phase === 'rollback')) || unknownAttempt?.Kind === 'restore' || unknownAttempt?.Kind === 'rollback';
  const latestUsableBackup = snapshot?.backups.Items.filter((backup) => backup.State === 'ready' && backup.Verified)
    .reduce<BackupView | undefined>((latest, backup) => !latest || Date.parse(backup.CreatedAt) > Date.parse(latest.CreatedAt) ? backup : latest, undefined);
  const partialBackupList = Boolean(snapshot && snapshot.backups.TotalRecordCount > snapshot.backups.Items.length);
  const latestBackup = snapshot?.backups.Items.reduce<BackupView | undefined>((latest, backup) => !latest || Date.parse(backup.CreatedAt) > Date.parse(latest.CreatedAt) ? backup : latest, undefined);
  const selectedBackup = snapshot?.backups.Items.find((backup) => backup.Id === selectedBackupId)
    ?? snapshot?.backups.Items.find((backup) => backup.State === 'ready') ?? snapshot?.backups.Items[0];
  const selectedVersion = selectedBackup ? checkedBackupVersion(selectedBackup) : undefined;
  const differentVersion = Boolean(selectedVersion && serverVersion && selectedVersion !== serverVersion);
  const callbacks: AdmissionCallbacks = { onAttempt: beginAttempt, onRejected: clearAttempt, onAccepted: accepted, onUnknown: unknown, onClose: () => setDialog(undefined), onNavigationGuardChange };
  return <Box className="backup-page">
    <div className="page-heading backup-page-heading"><div className="page-heading-copy"><h1>{t('Backups & recovery')}</h1>
      <div className="backup-page-summary">
        <span>{t(partialBackupList ? 'Latest backup on this page' : 'Latest backup')}: <span className="mono">{latestBackup ? backupListTime(latestBackup.CreatedAt) : t(snapshot ? 'No backups yet' : 'Loading backups')}</span>{latestBackup && <> · {t(latestBackup.Verified ? 'Verified' : 'Pending verification')}</>}</span>
        <BackupTaskSummary task={backupTask} loading={backupTaskLoading} error={backupTaskError ?? policyError} linked={Boolean(policy?.TaskId)} />
        {policy && <span>{t('Keep {count} automatic backups', { count: policy.KeepCount })}{!policy.IncludeImages && <> · {t('Images excluded')}</>}</span>}
      </div>
    </div><div className="page-heading-actions"><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
      <Tooltip title="Reconnect"><span><IconButton aria-label="Reconnect" onClick={reload} disabled={loading || policyLoading}><RefreshRounded /></IconButton></span></Tooltip>
      <Button startIcon={<UploadFileRounded />} disabled={admissionBusy} onClick={() => setDialog({ kind: 'import' })}>Import backup</Button>
      <Button variant="outlined" startIcon={<SettingsOutlined />} disabled={unavailable || recovering} onClick={() => setDialog({ kind: 'policy' })}>Backup settings</Button>
      <Button variant="contained" startIcon={<AddRounded />} disabled={createBusy || !snapshot?.status.Available} onClick={() => { if (policy) setDialog({ kind: 'create', includeImages: policy.IncludeImages }); }}>Create backup</Button>
    </Stack></div></div>
    <Stack className="backup-workspace" spacing={2.5}>
      {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
      {actionError != null && <ErrorNotice error={actionError} retry={reload} />}
      {policyError != null && <Box className="backup-policy-load-error"><ErrorNotice error={policyError} retry={() => setPolicyRevision((value) => value + 1)} /><Typography variant="body2" color="text.secondary">Reload saved backup contents before creating a manual backup.</Typography></Box>}
      {notice && <Alert severity="info" role="status" onClose={() => setNotice('')}>{notice}</Alert>}
      {unknownAttempt && <Alert id="backup-admission-notice" tabIndex={-1} severity="warning"><Typography variant="body2">{unknownAttempt.ApplyingRevision === undefined ? 'Admission could not be confirmed.' : 'Activation could not be confirmed. A ready plan alone does not confirm that it was applied.'} Check the jobs before starting another attempt. The request is not automatically submitted again.</Typography><Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere', mt: 1 }}>Attempt ID: <span className="mono">{unknownAttempt.RequestId}</span></Typography>{attemptNotFound && <Typography variant="body2" sx={{ mt: 1 }}>A complete check found no matching job and the server is idle. The earlier request could still appear later. You can explicitly start a new attempt with a new request ID.</Typography>}<Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 1 }}><Button color="inherit" onClick={reload} disabled={loading}>Check jobs again</Button>{attemptNotFound && <Button color="inherit" variant="outlined" onClick={startNewAttempt} disabled={loading || Boolean(loadError) || unknownAttempt.Kind === 'create' && (policyLoading || policyError != null || !policy)}>Start a new attempt</Button>}</Stack></Alert>}
      {recovering && <Alert severity="warning">Recovery is in progress or awaiting confirmation. If the connection drops, reconnect and sign in with the restored account passwords when asked. Check the completed job after signing in; admission and disconnection do not confirm a successful restore.</Alert>}
      {!snapshot && loading && <Stack role="status" aria-label="Loading backups" spacing={2}><Skeleton variant="rounded" height={140} /><Skeleton variant="rounded" height={250} /></Stack>}
      {snapshot && <>
        {(!snapshot.status.Available || !snapshot.status.RestoreAvailable || snapshot.status.Busy || loadError != null) && <Paper component="section" className="backup-readiness" aria-labelledby="backup-status-heading" sx={{ p: 1.75, borderRadius: '14px', bgcolor: colors.paper }}>
          <Stack direction="row" sx={{ gap: '10px 24px', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap' }}>
            <Box><Typography id="backup-status-heading" variant="h3" component="h2" sx={{ fontSize: 13, fontWeight: 600 }}>Recovery readiness</Typography><Stack direction="row" sx={{ gap: 0.75, alignItems: 'center', flexWrap: 'wrap', mt: 0.75 }}><Chip size="small" color={snapshot.status.Available ? 'success' : 'warning'} label={snapshot.status.Available ? 'Backup creation available' : 'Backup creation unavailable'} /><Chip size="small" color={snapshot.status.RestoreAvailable ? 'success' : 'warning'} label={snapshot.status.RestoreAvailable ? 'Restore planning available' : 'Restore planning unavailable'} />{snapshot.status.Busy && <Chip size="small" color="primary" label="Recovery job in progress" />}{loadError != null && <Chip size="small" color="warning" label="Last confirmed data" />}</Stack></Box>
            <Box sx={{ minWidth: 0, flex: '1 1 200px', maxWidth: 300 }}><Typography variant="caption" color="text.secondary">{partialBackupList ? 'Latest verified on this page' : 'Latest verified backup'}</Typography><Typography variant="body2" className="mono" sx={{ mt: 0.5 }}>{latestUsableBackup ? dateTime(latestUsableBackup.CreatedAt) : 'No verified backup'}</Typography></Box>
          </Stack>
          {!snapshot.status.Available && <Alert severity="warning" sx={{ mt: 1.5 }}>{recoveryMessages[snapshot.status.UnavailableReason] || 'Backup creation is unavailable. Ask the server operator to check its configuration.'}</Alert>}
          {!snapshot.status.RestoreAvailable && <Alert severity="info" sx={{ mt: 1.5 }}>{recoveryMessages[snapshot.status.RestoreUnavailableReason] || 'Restore planning is currently unavailable. Ask the server operator to check recovery setup.'}</Alert>}
        </Paper>}
        <Paper component="section" className="backup-list" aria-labelledby="backups-heading" variant="outlined" aria-busy={loading} sx={{ overflow: 'hidden' }}>
          <Stack className="delivery-card-heading" direction="row" sx={{ gap: 1, alignItems: 'center' }}><Typography id="backups-heading" component="h2">Backups</Typography><span className="backup-list-count mono">{snapshot.backups.TotalRecordCount.toLocaleString(adminLocale())}</span></Stack>
          {snapshot.backups.Items.length === 0 ? <Box sx={{ ...sectionSpacing, pt: 0 }}><Typography variant="body2" color="text.secondary">No backups yet. Create an encrypted backup of this server or import a Goby backup file.</Typography></Box> : <TableContainer sx={{ borderRadius: 0, position: 'relative' }}>
            <Table className="backup-table" aria-label="Backups">
              <TableHead><TableRow>
                <TableCell>Time</TableCell><TableCell>Type</TableCell><TableCell>Size</TableCell><TableCell>Version</TableCell><TableCell>Verification</TableCell>
              </TableRow></TableHead>
              <TableBody>
                {snapshot.backups.Items.map((backup) => <TableRow key={backup.Id} className={`backup-row${selectedBackup?.Id === backup.Id ? ' is-selected' : ''}`} aria-selected={selectedBackup?.Id === backup.Id} data-testid={`backup-${backup.Id}`}>
                  <TableCell className="backup-time-cell">
                    <Button className="backup-date-button" aria-pressed={selectedBackup?.Id === backup.Id} aria-label={`Select backup from ${dateTime(backup.CreatedAt)}`} title={dateTime(backup.CreatedAt)} onClick={() => setSelectedBackupId(backup.Id)}>{backupListTime(backup.CreatedAt)}</Button>
                  </TableCell>
                  <TableCell className="backup-type-cell">
                    <Typography variant="body2">{backup.Kind === 'generated' ? 'Created here' : 'Imported'}</Typography>
                    <Typography variant="caption" color="text.secondary" component="div">{backup.Verified && backup.Source ? backup.Source.ServerName || 'Unnamed server' : 'Source awaiting verification'}</Typography>
                    <details className="backup-row-details">
                      <summary aria-label={`Details for backup ${backup.Id}`} title={t('Details and actions')}><MoreVertRounded aria-hidden="true" /><span className="backup-row-details-label">{t('Details and actions')}</span></summary>
                      <div className="backup-row-detail-content"><Typography component="div" variant="caption" className="mono" sx={{ overflowWrap: 'anywhere' }}>{backup.Id}</Typography><Digest value={backup.SHA256} />
                        <Typography variant="caption" color="text.secondary">Downloads are available when a backup is ready. Planning a restore verifies imported files.</Typography>
                        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                          <Button size="small" startIcon={<DownloadRounded />} disabled={unavailable || backup.State !== 'ready'} onClick={() => void download(backup)}>Download</Button>
                          <Button size="small" startIcon={<RestoreRounded />} disabled={admissionBusy || !snapshot.status.RestoreAvailable || backup.State !== 'ready'} onClick={() => setDialog({ kind: 'plan', backup, status: snapshot.status })}>Plan restore</Button>
                          <Button size="small" color="error" startIcon={<DeleteOutlineRounded />} disabled={admissionBusy || ['writing', 'deleting'].includes(backup.State)} onClick={() => setDialog({ kind: 'delete', backup })}>Delete backup</Button>
                        </Stack>
                        {backup.Verified && backup.Source && <Typography variant="caption" color="text.secondary">{t('Data format version')}: <span className="mono">{backup.Source.SchemaVersion}</span></Typography>}
                      </div>
                    </details>
                    {backup.ErrorCode && <Alert severity={backup.State === 'cancelled' ? 'info' : 'error'} sx={{ mt: 1.5 }}>{recoveryMessages[backup.ErrorCode] || 'This backup could not be completed. Check its job before trying again.'}</Alert>}
                  </TableCell>
                  <TableCell className="backup-size-cell"><span className="backup-mobile-label">{t('Size')}</span><span className="mono">{bytes(backup.SizeBytes)}</span></TableCell>
                  <TableCell className="backup-version-cell"><span className="backup-mobile-label">{t('Version')}</span><span className={`mono${!checkedBackupVersion(backup) ? ' backup-version-unverified' : checkedBackupVersion(backup) && serverVersion && checkedBackupVersion(backup) !== serverVersion ? ' is-different-version' : ''}`} data-testid={`backup-version-${backup.Id}`}>{checkedBackupVersion(backup) || t('Pending verification')}</span></TableCell>
                  <TableCell className="backup-verification-cell"><span className="backup-mobile-label">{t('Verification')}</span>{backup.State === 'ready' ? <span className={`backup-verification${backup.Verified ? ' is-verified' : ''}`}>{backup.Verified && <CheckRounded aria-hidden="true" />}{t(backup.Verified ? 'Verified' : 'Pending verification')}</span> : <Stack spacing={0.75} sx={{ alignItems: 'flex-start' }}><StateChip state={backup.State} /><span className="backup-verification">{t(backup.Verified ? 'Verified' : 'Pending verification')}</span></Stack>}</TableCell>
                </TableRow>)}
              </TableBody>
            </Table>
          </TableContainer>}
          <TablePagination component="div" count={snapshot.backups.TotalRecordCount} page={Math.floor(backupStart / pageSize)} rowsPerPage={pageSize} rowsPerPageOptions={[pageSize]} onPageChange={(_, page) => setBackupStart(page * pageSize)} labelRowsPerPage="Backups per page" sx={paginationStyles} />
          <details className="backup-storage-details"><summary>{t('Backup storage usage')}</summary><Box component="dl" sx={{ display: 'flex', flexWrap: 'wrap', gap: '8px 24px', m: 0 }}>
            {[
              ['Storage used', `${bytes(snapshot.status.Storage.Bytes)} of ${bytes(snapshot.status.Limits.MaxStoredBytes)}`],
              ['Stored backups', `${snapshot.status.Storage.Objects.toLocaleString(adminLocale())} of ${snapshot.status.Limits.MaxBackups.toLocaleString(adminLocale())}`],
              ['Maximum backup size', bytes(snapshot.status.Limits.MaxBackupBytes)],
            ].map(([label, value]) => <Box key={label} sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}><Typography component="dt" variant="caption" color="text.secondary">{label}:</Typography><Typography component="dd" variant="caption" sx={{ m: 0, fontVariantNumeric: 'tabular-nums' }}>{value}</Typography></Box>)}
          </Box></details>
          <Box className="backup-encryption-note"><ShieldOutlined sx={{ fontSize: 14, color: 'text.secondary' }} /><Typography variant="caption" color="text.secondary">Backups are encrypted. Restore requires the backup passphrase. Media files are not included.</Typography></Box>
        </Paper>
        <Paper component="section" className="backup-inspector" aria-labelledby="backup-inspector-heading" variant="outlined">
          <Stack className="delivery-card-heading" direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><Typography component="h2" id="backup-inspector-heading">Restore backup</Typography>{selectedBackup && <StateChip state={selectedBackup.State} />}</Stack>
          {selectedBackup ? <>
            <Box className="backup-inspector-step"><span className="backup-step-number is-ready"><CheckRounded aria-hidden="true" /></span><Box><Typography sx={{ fontWeight: 600 }}>Select backup</Typography><Typography className="mono" variant="body2" sx={{ mt: 0.5 }}>{dateTime(selectedBackup.CreatedAt)}</Typography><Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>{selectedBackup.Verified && selectedBackup.Source ? selectedBackup.Source.ServerName || 'Unnamed server' : selectedBackup.Kind === 'generated' ? 'Created here' : 'Imported'} · {bytes(selectedBackup.SizeBytes)}</Typography></Box></Box>
            <Box className="backup-inspector-step"><span className="backup-step-number">2</span><Box><Typography sx={{ fontWeight: 600 }}>Unlock and verify</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>{selectedBackup.Verified ? 'The archive is verified. Enter its passphrase in a new restore plan to check compatibility with this server.' : 'This backup is not verified. Planning a restore checks the uploaded archive.'}</Typography></Box></Box>
            <Box className="backup-inspector-step backup-version-review" data-testid="backup-version-review"><span className={`backup-step-number${differentVersion ? ' is-warning' : ''}`}>3</span><Box><Typography sx={{ fontWeight: 600 }}>Check application version</Typography>
              {!selectedVersion ? <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>The application version is pending verification. Plan a restore to check this backup.</Typography>
                : !serverVersion ? <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>{t('Backup Goby version: {version}. The current server version is unavailable.', { version: selectedVersion })}</Typography>
                  : differentVersion ? <Alert className="backup-version-warning" severity="warning">{t('This backup was created with Goby {backupVersion}; this server runs {serverVersion}. Cross-version restore is not guaranteed. Prefer a backup from the same version.', { backupVersion: selectedVersion, serverVersion })}</Alert>
                    : <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>{t('Backup and server both use Goby {version}. A restore plan must still pass the server compatibility checks.', { version: selectedVersion })}</Typography>}
            </Box></Box>
            <Box className="backup-inspector-step"><span className="backup-step-number">4</span><Box><Typography sx={{ fontWeight: 600 }}>Preview restored data</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>Create a restore plan to review the checked source server, included records, and deployment defaults.</Typography></Box></Box>
            <Box className="backup-inspector-step"><span className="backup-step-number">5</span><Box><Typography sx={{ fontWeight: 600 }}>Activate restore</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, fontSize: 12.5, lineHeight: 1.65 }}>Confirm the checked plan. Everyone must sign in again using their restored account passwords.</Typography></Box></Box>
            <Box className="backup-inspector-tools"><Button size="small" startIcon={<DownloadRounded />} disabled={unavailable || selectedBackup.State !== 'ready'} onClick={() => void download(selectedBackup)}>Download</Button><Button size="small" color="error" startIcon={<DeleteOutlineRounded />} disabled={admissionBusy || ['writing', 'deleting'].includes(selectedBackup.State)} onClick={() => setDialog({ kind: 'delete', backup: selectedBackup })}>Delete backup</Button></Box>
            <Box className="backup-inspector-actions"><Typography variant="caption" color="text.secondary">Media files remain in their current directories.</Typography><Button variant="contained" startIcon={<RestoreRounded />} disabled={admissionBusy || !snapshot.status.RestoreAvailable || selectedBackup.State !== 'ready'} onClick={() => setDialog({ kind: 'plan', backup: selectedBackup, status: snapshot.status })}>Plan restore</Button></Box>
          </> : <Box sx={{ p: 2, pt: 0 }}><Typography variant="body2" color="text.secondary">Select a backup to review the recovery steps. Create a backup or import one to get started.</Typography></Box>}
        </Paper>
        <Box className="backup-support" sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 380px), 1fr))', gap: 2, alignItems: 'start' }}>
          <Paper component="section" aria-labelledby="backup-jobs-heading" variant="outlined" aria-busy={loading} sx={{ minWidth: 0, overflow: 'hidden' }}>
            <Box sx={sectionSpacing}>
              <Stack direction="row" sx={{ gap: 1, justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' }}><Typography id="backup-jobs-heading" tabIndex={-1} variant="h3" component="h2">Backup jobs</Typography><Typography variant="caption" color="text.secondary" role="status">{polling ? 'Checking active jobs automatically' : 'Showing the last confirmed state'}</Typography></Stack>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>An accepted request starts a job. Review its state to confirm the outcome.</Typography>
            </Box>
            {operations.length === 0 ? <Box sx={{ ...sectionSpacing, pt: 0 }}><Typography variant="body2" color="text.secondary">No backup or recovery jobs have been recorded.</Typography></Box> : <Box component="ul" aria-label="Backup jobs" sx={{ listStyle: 'none', m: 0, p: 0 }}>
              {operations.map((operation) => <Box component="li" key={operation.Id} data-testid={`operation-${operation.Id}`} sx={{ ...sectionSpacing, borderTop: 1, borderColor: 'divider' }}>
                <Stack direction="row" sx={{ gap: 1, justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' }}><Typography variant="h4" component="h3" sx={{ fontSize: 14 }}>{kindLabels[operation.Kind]}</Typography><StateChip state={operation.State} /></Stack>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{phaseLabels[operation.Phase]}</Typography>
                {activeOperation(operation) && <LinearProgress aria-label={`${kindLabels[operation.Kind]} in progress`} sx={{ mt: 1.25, mb: 1.5, '@media (prefers-reduced-motion: reduce)': { '& .MuiLinearProgress-bar': { animation: 'none' } } }} />}
                <Typography variant="caption" component="div" color="text.secondary" sx={{ mt: 0.5 }}>Updated {dateTime(operation.UpdatedAt)}</Typography>
                <Typography variant="caption" component="div" className="mono" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{operation.Id}</Typography>
                {(operation.Kind === 'restore' || operation.CanCancel) && <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, mt: 1, mx: -1.25, '& .MuiButton-root': { px: 1.25 } }}>
                  {operation.Kind === 'restore' && <Button size="small" disabled={Boolean(actionBusy)} onClick={() => setDialog({ kind: 'inspect', operationId: operation.Id })}>Inspect plan</Button>}
                  {operation.CanCancel && <Button size="small" color="error" disabled={Boolean(actionBusy) || Boolean(loadError) || loading} onClick={() => void cancel(operation)}>Cancel job</Button>}
                </Stack>}
                {operation.State === 'ready' && operation.CanApply && <Alert severity="info" sx={{ mt: 2 }}>The backup was checked and the restore plan is ready. Inspect its source and settings before applying it.</Alert>}
                {operation.ErrorCode && <Alert severity={operation.State === 'cancelled' ? 'info' : 'error'} sx={{ mt: 2 }}>{recoveryMessages[operation.ErrorCode] || 'This job did not complete. Refresh its state and review the current server before trying again.'}</Alert>}
                {operation.State === 'completed' && (operation.Kind === 'restore' || operation.Kind === 'rollback') && <Alert severity="success" sx={{ mt: 2 }}>The server records this recovery job as completed. Reconnect to confirm access and review your server data before resuming normal work.</Alert>}
              </Box>)}
            </Box>}
            <TablePagination component="div" count={snapshot.operations.TotalRecordCount} page={Math.floor(operationStart / pageSize)} rowsPerPage={pageSize} rowsPerPageOptions={[pageSize]} onPageChange={(_, page) => setOperationStart(page * pageSize)} labelRowsPerPage="Jobs per page" sx={paginationStyles} />
          </Paper>
          <Paper component="section" aria-labelledby="rollback-heading" variant="outlined" sx={{ ...sectionSpacing, minWidth: 0 }}>
            <Stack direction="row" sx={{ gap: 1, alignItems: 'center' }}><ShieldOutlined color="primary" sx={{ fontSize: 20 }} /><Typography id="rollback-heading" variant="h3" component="h2">Retained rollback copy</Typography></Stack>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{snapshot.status.Rollback.Available ? `A retained copy of ${snapshot.status.Rollback.ServerName || 'this server'}${snapshot.status.Rollback.CreatedAt ? ` from ${dateTime(snapshot.status.Rollback.CreatedAt)}` : ''} is available. Rolling back requires confirmation and signs everyone out.` : recoveryMessages[snapshot.status.Rollback.UnavailableReason] || 'No verified rollback copy is available. Applying a restore preserves the previously active server for recovery.'}</Typography>
            {snapshot.status.Rollback.Available && <Box sx={{ p: 2, mt: 2, borderRadius: '12px', bgcolor: colors.surface }}>
              <Stack direction="row" sx={{ gap: 1, justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' }}><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{snapshot.status.Rollback.ServerName || 'This server'}</Typography><Chip size="small" color="success" label="Available" /></Stack>
              {snapshot.status.Rollback.CreatedAt && <Typography variant="caption" component="div" color="text.secondary" sx={{ mt: 0.75 }}>{dateTime(snapshot.status.Rollback.CreatedAt)}</Typography>}
              <Typography variant="caption" component="div" className="mono" color="text.secondary" sx={{ mt: 0.75, overflowWrap: 'anywhere' }}>{snapshot.status.Rollback.Generation}</Typography>
            </Box>}
            <Button variant="outlined" startIcon={<RestoreRounded />} disabled={admissionBusy || !snapshot.status.Rollback.Available || !recoverySwitchAvailable(snapshot.status)} onClick={() => setDialog({ kind: 'rollback', status: snapshot.status })} sx={{ mt: 2 }}>Roll back</Button>
          </Paper>
        </Box>
      </>}
    </Stack>
    {(dialog?.kind === 'create' || dialog?.kind === 'import') && snapshot && <BackupAdmissionDialog {...callbacks} kind={dialog.kind} status={snapshot.status} includeImages={dialog.kind === 'create' ? dialog.includeImages : undefined} />}
    {dialog?.kind === 'plan' && <RestorePlanDialog {...callbacks} backup={dialog.backup} status={dialog.status} />}
    {dialog?.kind === 'delete' && <DeleteBackupDialog {...callbacks} backup={dialog.backup} />}
    {dialog?.kind === 'inspect' && <InspectPlanDialog {...callbacks} operationId={dialog.operationId} applicationUncertain={unknownAttempt?.ApplyingRevision !== undefined} />}
    {dialog?.kind === 'rollback' && <RollbackDialog {...callbacks} status={dialog.status} />}
    {dialog?.kind === 'policy' && <BackupPolicyDrawer onClose={() => setDialog(undefined)} onSaved={(savedPolicy) => { setPolicy(savedPolicy); setPolicyError(undefined); setPolicyRevision((value) => value + 1); setNotice('Backup settings saved.'); }} onNavigationGuardChange={onNavigationGuardChange} />}
  </Box>;
}
