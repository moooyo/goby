import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, Paper, Skeleton, Stack, TablePagination, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DownloadRounded from '@mui/icons-material/DownloadRounded';
import UploadFileRounded from '@mui/icons-material/UploadFileRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import RestoreRounded from '@mui/icons-material/RestoreRounded';
import ShieldOutlined from '@mui/icons-material/ShieldOutlined';
import { ApiError, isAbortError } from './api';
import { backupsApi, newBackupRequestId, validateBackupPassphrase } from './backupsApi';
import type { BackupPage, BackupView, OperationPage, OperationView, SourceView, StatusView } from './backupsApi';
import { ErrorNotice, PageHeading } from './components';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const pageSize = 25;
const sectionSpacing = { p: { xs: 2.5, sm: 3 } };
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
};
const kindLabels: Record<OperationView['Kind'], string> = { create: 'Create backup', import: 'Import backup', delete: 'Delete backup', restore: 'Restore backup', rollback: 'Roll back' };
const phaseLabels: Record<OperationView['Phase'], string> = {
  admission: 'Queued', upload: 'Receiving file', snapshot: 'Collecting server data', encryption: 'Encrypting backup',
  publication: 'Saving backup', validation: 'Checking backup', staging: 'Preparing restored data', ready: 'Ready for review',
  activation: 'Starting recovered server', rollback: 'Returning to the retained copy', cleanup: 'Finishing up', finished: 'Finished',
};

interface AdmissionAttempt { RequestId: string; Kind: OperationView['Kind']; ApplyingRevision?: string }
interface Snapshot { status: StatusView; backups: BackupPage; operations: OperationPage }
type DialogState = { kind: 'create' | 'import' }
  | { kind: 'plan'; backup: BackupView; status: StatusView }
  | { kind: 'delete'; backup: BackupView }
  | { kind: 'inspect'; operationId: string }
  | { kind: 'rollback'; status: StatusView };
interface AdmissionCallbacks {
  onAccepted: (operation: OperationView) => void;
  onUnknown: (attempt: AdmissionAttempt) => void;
  onClose: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

function readableState(state: string): string { return state.charAt(0).toUpperCase() + state.slice(1); }
function dateTime(value: string): string { return new Date(value).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }); }
function count(value: string): string { return BigInt(value).toLocaleString(); }
function bytes(value: string): string {
  const amount = BigInt(value);
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
  let divisor = 1n;
  let unit = 0;
  while (unit < units.length - 1 && amount >= divisor * 1024n) { divisor *= 1024n; unit += 1; }
  if (unit === 0) return `${amount.toLocaleString()} B`;
  const decimal = (amount % divisor) * 10n / divisor;
  return `${(amount / divisor).toLocaleString()}${decimal ? `.${decimal}` : ''} ${units[unit]}`;
}
function activeOperation(operation: OperationView): boolean { return ['pending', 'running', 'applying'].includes(operation.State); }
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
  return <Chip size="small" variant="outlined" label={readableState(state)} color={state === 'completed' || state === 'ready' ? 'success' : state === 'failed' || state === 'interrupted' ? 'error' : state === 'running' || state === 'applying' ? 'primary' : 'default'} />;
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

function BackupAdmissionDialog({ kind, status, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { kind: 'create' | 'import'; status: StatusView }) {
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
  const valid = kind === 'create' ? Boolean(passphrase && !passphraseIssue(passphrase) && passphrase === confirmation) : Boolean(file && !fileIssue);
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard this backup form and leave this page?');
  useEffect(() => () => { controller.current?.abort(); if (fileInput.current) fileInput.current.value = ''; }, []);
  function close() {
    if (controller.current || (dirty && !window.confirm('Discard this backup form?'))) return;
    setPassphrase(''); setConfirmation(''); setFile(undefined); onClose();
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!valid || controller.current) return;
    const pending = new AbortController(); controller.current = pending; setBusy(true); setError(undefined);
    try {
      requestId.current ??= newBackupRequestId();
      const operation = kind === 'create'
        ? await backupsApi.createBackup({ RequestId: requestId.current, Passphrase: passphrase }, { signal: pending.signal })
        : await backupsApi.importBackup(file!, requestId.current, { signal: pending.signal });
      if (pending.signal.aborted) return;
      setPassphrase(''); setConfirmation(''); setFile(undefined);
      onAccepted(operation);
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) {
        setPassphrase(''); setConfirmation(''); setFile(undefined);
        onUnknown({ RequestId: requestId.current, Kind: kind });
      } else { setError(safeError(cause)); requestId.current = undefined; }
    } finally { if (controller.current === pending) controller.current = undefined; if (!pending.signal.aborted) setBusy(false); }
  }
  return <Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="backup-admission-title" aria-describedby="backup-admission-description"
    slotProps={{ transition: { onEntered: () => document.getElementById('backup-admission-title')?.closest('[role="dialog"]')?.querySelector<HTMLInputElement>('input')?.focus() } }}>
    <Box component="form" onSubmit={submit} aria-busy={busy}>
      <DialogTitle id="backup-admission-title" sx={{ pt: 3 }}>{kind === 'create' ? 'Create backup' : 'Import backup'}</DialogTitle>
      <DialogContent><Stack spacing={2.5} sx={{ pt: 1 }}>
        <Typography id="backup-admission-description" variant="body2" color="text.secondary">{kind === 'create' ? 'Save an encrypted copy of this server’s data and settings. Media files are not included.' : 'Upload an encrypted Goby backup. Imported files remain unverified until you plan a restore and provide the passphrase.'}</Typography>
        {error != null && <ErrorNotice error={error} />}
        {kind === 'create' ? <PassphraseFields passphrase={passphrase} confirmation={confirmation} disabled={busy} onPassphrase={setPassphrase} onConfirmation={setConfirmation} /> : <>
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

function RestorePlanDialog({ backup, status, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { backup: BackupView; status: StatusView }) {
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
  function close() { if (!controller.current && (!dirty || window.confirm('Discard the restore form?'))) { setPassphrase(''); onClose(); } }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!valid || controller.current) return;
    const pending = new AbortController(); controller.current = pending; setBusy(true); setError(undefined);
    try {
      requestId.current ??= newBackupRequestId();
      const operation = await backupsApi.planRestore({ RequestId: requestId.current, BackupId: backup.Id, SHA256: backup.SHA256, Passphrase: passphrase, RestoreDefaults: restoreDefaults, ReplaceRollback: status.Rollback.MustReplace && replaceRollback, GenerationRevision: status.GenerationRevision }, { signal: pending.signal });
      if (!pending.signal.aborted) { setPassphrase(''); onAccepted(operation); }
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) { setPassphrase(''); onUnknown({ RequestId: requestId.current, Kind: 'restore' }); }
      else { setError(safeError(cause)); requestId.current = undefined; }
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

function DeleteBackupDialog({ backup, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { backup: BackupView }) {
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
    try {
      requestId.current ??= newBackupRequestId();
      const operation = await backupsApi.deleteBackup(backup.Id, { RequestId: requestId.current, SHA256: backup.SHA256 }, { signal: pending.signal });
      if (!pending.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (pending.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) onUnknown({ RequestId: requestId.current, Kind: 'delete' });
      else { setError(safeError(cause)); requestId.current = undefined; }
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

function InspectPlanDialog({ operationId, applicationUncertain, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { operationId: string; applicationUncertain: boolean }) {
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
  const canApply = Boolean(!applicationUncertain && !loading && !error && details?.operation.CanApply && details.operation.State === 'ready' && details.operation.Source && details.operation.GenerationRevision === details.status.GenerationRevision && details.status.RestoreAvailable);
  async function apply() {
    if (!details || !canApply || !confirmed || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy(true); setError(undefined);
    try {
      const operation = await backupsApi.applyRestore(operationId, { Revision: details.operation.Revision, GenerationRevision: details.status.GenerationRevision }, { signal: controller.signal });
      if (!controller.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (controller.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause)) onUnknown({ RequestId: details.operation.RequestId, Kind: 'restore', ApplyingRevision: details.operation.Revision });
      else { setError(safeError(cause)); setConfirmed(false); }
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

function RollbackDialog({ status, onAccepted, onUnknown, onClose, onNavigationGuardChange }: AdmissionCallbacks & { status: StatusView }) {
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
    try {
      requestId.current ??= newBackupRequestId();
      const operation = await backupsApi.rollback({ RequestId: requestId.current, GenerationRevision: status.GenerationRevision }, { signal: controller.signal });
      if (!controller.signal.aborted) onAccepted(operation);
    } catch (cause) {
      if (controller.signal.aborted || isAbortError(cause)) return;
      if (uncertain(cause) && requestId.current) onUnknown({ RequestId: requestId.current, Kind: 'rollback' });
      else { setError(safeError(cause)); requestId.current = undefined; setConfirmed(false); }
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
  const current = first.Items.find((operation) => operation.RequestId === attempt.RequestId && operation.Kind === attempt.Kind);
  if (current) return current;
  const initial = await backupsApi.getOperations({ StartIndex: 0, Limit: 100 }, { signal });
  const found = initial.Items.find((operation) => operation.RequestId === attempt.RequestId && operation.Kind === attempt.Kind);
  if (found) return found;
  const seen = new Set(initial.Items.map((operation) => operation.Id));
  if (initial.Items.length !== Math.min(initial.Limit, initial.TotalRecordCount)) throw new ApiError('The job list changed during reconciliation.', { code: 'source_changed' });
  for (let start = 100; start < initial.TotalRecordCount; start += 100) {
    const page = await backupsApi.getOperations({ StartIndex: start, Limit: 100 }, { signal });
    const operation = page.Items.find((item) => item.RequestId === attempt.RequestId && item.Kind === attempt.Kind);
    if (operation) return operation;
    if (page.TotalRecordCount !== initial.TotalRecordCount || page.Items.length !== Math.min(page.Limit, initial.TotalRecordCount - start)
      || page.Items.some((item) => seen.has(item.Id))) throw new ApiError('The job list changed during reconciliation.', { code: 'source_changed' });
    page.Items.forEach((item) => seen.add(item.Id));
  }
  return undefined;
}

export function BackupsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [loadError, setLoadError] = useState<unknown>();
  const [actionError, setActionError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [backupStart, setBackupStart] = useState(0);
  const [operationStart, setOperationStart] = useState(0);
  const [dialog, setDialog] = useState<DialogState>();
  const [tracked, setTracked] = useState<OperationView>();
  const [unknownAttempt, setUnknownAttempt] = useState<AdmissionAttempt>();
  const [attemptNotFound, setAttemptNotFound] = useState(false);
  const [notice, setNotice] = useState('');
  const [focusVersion, setFocusVersion] = useState(0);
  const [actionBusy, setActionBusy] = useState('');
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  const action = useRef<AbortController | undefined>(undefined);
  const readController = useRef<AbortController | undefined>(undefined);
  const snapshotVersion = useRef(0);
  const trackedId = tracked?.Id;
  const reload = () => { readController.current?.abort(); snapshotVersion.current += 1; setLoading(true); setRefresh((value) => value + 1); };
  useEffect(() => () => action.current?.abort(), []);
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
          const applyStillUnknown = unknownAttempt.ApplyingRevision !== undefined && admitted?.State === 'ready';
          if (controller.signal.aborted || version !== snapshotVersion.current) return;
          if (admitted && !applyStillUnknown) { selected = admitted; setUnknownAttempt(undefined); setAttemptNotFound(false); setNotice('The server acknowledged the job. Follow its current state below.'); }
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
  }, [backupStart, operationStart, refresh, trackedId, unknownAttempt]);
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
  function accepted(operation: OperationView) {
    snapshotVersion.current += 1;
    setTracked(operation); setDialog(undefined); setUnknownAttempt(undefined); setActionError(undefined);
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
    snapshotVersion.current += 1;
    const kind = unknownAttempt.Kind;
    setUnknownAttempt(undefined); setAttemptNotFound(false);
    setNotice('A new attempt will use a new request ID. The earlier request may still appear in the job list.');
    if (kind === 'create' || kind === 'import') setDialog({ kind });
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
  const unavailable = !snapshot?.status.Available || Boolean(loadError) || Boolean(actionBusy) || Boolean(unknownAttempt) || loading;
  const admissionBusy = unavailable || Boolean(snapshot?.status.Busy);
  const operations = snapshot ? [ ...(tracked && !snapshot.operations.Items.some((operation) => operation.Id === tracked.Id) ? [tracked] : []), ...snapshot.operations.Items.map((operation) => tracked?.Id === operation.Id ? tracked : operation) ] : [];
  const recovering = operations.some((operation) => (operation.Kind === 'restore' || operation.Kind === 'rollback') && (operation.State === 'applying' || operation.Phase === 'activation' || operation.Phase === 'rollback')) || unknownAttempt?.Kind === 'restore' || unknownAttempt?.Kind === 'rollback';
  const callbacks: AdmissionCallbacks = { onAccepted: accepted, onUnknown: unknown, onClose: () => setDialog(undefined), onNavigationGuardChange };
  return <>
    <PageHeading title="Backups & recovery" description="Keep encrypted server backups and review recovery changes before applying them." action={<Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button variant="outlined" startIcon={<UploadFileRounded />} disabled={admissionBusy} onClick={() => setDialog({ kind: 'import' })}>Import backup</Button><Button variant="contained" startIcon={<AddRounded />} disabled={admissionBusy} onClick={() => setDialog({ kind: 'create' })}>Create backup</Button></Stack>} />
    <Stack spacing={3}>
      {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
      {actionError != null && <ErrorNotice error={actionError} retry={reload} />}
      {notice && <Alert severity="info" role="status" onClose={() => setNotice('')}>{notice}</Alert>}
      {unknownAttempt && <Alert id="backup-admission-notice" tabIndex={-1} severity="warning"><Typography variant="body2">{unknownAttempt.ApplyingRevision === undefined ? 'Admission could not be confirmed.' : 'Activation could not be confirmed. A ready plan alone does not confirm that it was applied.'} Check the jobs before starting another attempt. The request is not automatically submitted again.</Typography><Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere', mt: 1 }}>Attempt ID: <span className="mono">{unknownAttempt.RequestId}</span></Typography>{attemptNotFound && <Typography variant="body2" sx={{ mt: 1 }}>A complete check found no matching job and the server is idle. The earlier request could still appear later. You can explicitly start a new attempt with a new request ID.</Typography>}<Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 1 }}><Button color="inherit" onClick={reload} disabled={loading}>Check jobs again</Button>{attemptNotFound && <Button color="inherit" variant="outlined" onClick={startNewAttempt} disabled={loading || Boolean(loadError)}>Start a new attempt</Button>}</Stack></Alert>}
      {recovering && <Alert severity="warning">Recovery is in progress or awaiting confirmation. If the connection drops, reconnect and sign in with the restored account passwords when asked. Check the completed job after signing in; admission and disconnection do not confirm a successful restore.</Alert>}
      {!snapshot && loading && <Stack role="status" aria-label="Loading backups" spacing={2}><Skeleton variant="rounded" height={140} /><Skeleton variant="rounded" height={250} /></Stack>}
      {snapshot && <>
        <Paper component="section" aria-labelledby="backup-status-heading" variant="outlined" sx={sectionSpacing}>
          <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', gap: 2 }}><Box><Typography id="backup-status-heading" variant="h3" component="h2">Recovery readiness</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.6 }}>Encrypted backups include server data and settings. Keep copies outside this server.</Typography></Box><Button startIcon={<RefreshRounded />} onClick={reload} disabled={loading} sx={{ alignSelf: 'flex-start' }}>Reconnect</Button></Stack>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}><Chip size="small" variant="outlined" color={snapshot.status.Available ? 'success' : 'warning'} label={snapshot.status.Available ? 'Backups available' : 'Backups unavailable'} /><Chip size="small" variant="outlined" color={snapshot.status.RestoreAvailable ? 'success' : 'warning'} label={snapshot.status.RestoreAvailable ? 'Restore configured' : 'Restore unavailable'} />{snapshot.status.Busy && <Chip size="small" color="primary" variant="outlined" label="Recovery job in progress" />}{loadError != null && <Chip size="small" color="warning" label="Last confirmed data" />}</Stack>
          {!snapshot.status.Available && <Alert severity="warning" sx={{ mt: 2 }}>{recoveryMessages[snapshot.status.UnavailableReason] || 'Backup service is unavailable. Ask the server operator to check its configuration.'}</Alert>}
          {!snapshot.status.RestoreAvailable && <Alert severity="info" sx={{ mt: 2 }}>{recoveryMessages[snapshot.status.RestoreUnavailableReason] || 'Restore is currently unavailable. Ask the server operator to check recovery setup.'}</Alert>}
          <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 2, m: 0, mt: 2.5 }}>
            {[
              ['Storage used', `${bytes(snapshot.status.Storage.Bytes)} of ${bytes(snapshot.status.Limits.MaxStoredBytes)}`],
              ['Stored backups', `${snapshot.status.Storage.Objects.toLocaleString()} of ${snapshot.status.Limits.MaxBackups.toLocaleString()}`],
              ['Maximum backup size', bytes(snapshot.status.Limits.MaxBackupBytes)],
            ].map(([label, value]) => <Box key={label}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.3, fontWeight: 650, fontVariantNumeric: 'tabular-nums' }}>{value}</Typography></Box>)}
          </Box>
        </Paper>
        <Paper component="section" aria-labelledby="backups-heading" variant="outlined" aria-busy={loading}>
          <Box sx={sectionSpacing}><Typography id="backups-heading" variant="h3" component="h2">Backups</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.6 }}>Downloads are available when a backup is ready. Planning a restore verifies imported files.</Typography></Box>
          {snapshot.backups.Items.length === 0 ? <Box sx={{ ...sectionSpacing, pt: 0 }}><Typography variant="body2" color="text.secondary">No backups yet. Create an encrypted backup of this server or import a Goby backup file.</Typography></Box> : <Box component="ul" aria-label="Backups" sx={{ m: 0, p: 0, listStyle: 'none' }}>
            {snapshot.backups.Items.map((backup) => <Box component="li" key={backup.Id} data-testid={`backup-${backup.Id}`} sx={{ ...sectionSpacing, borderTop: 1, borderColor: 'divider' }}>
              <Stack direction={{ xs: 'column', lg: 'row' }} sx={{ justifyContent: 'space-between', gap: 2 }}><Box sx={{ minWidth: 0 }}><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, alignItems: 'center' }}><Typography variant="h4" component="h3">{dateTime(backup.CreatedAt)}</Typography><StateChip state={backup.State} /><Chip variant="outlined" size="small" label={backup.Verified ? 'Verified' : 'Not verified'} /></Stack><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{backup.Kind === 'generated' ? 'Created here' : 'Imported'} · {bytes(backup.SizeBytes)}{backup.Source ? ` · ${backup.Source.ServerName || 'Unnamed server'}` : ''}</Typography><Typography variant="caption" className="mono" color="text.secondary" component="div" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{backup.Id}</Typography></Box>
                <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, alignItems: 'flex-start', flexShrink: 0 }}><Button size="small" startIcon={<DownloadRounded />} disabled={unavailable || backup.State !== 'ready'} onClick={() => void download(backup)}>Download</Button><Button size="small" disabled={admissionBusy || !snapshot.status.RestoreAvailable || backup.State !== 'ready'} onClick={() => setDialog({ kind: 'plan', backup, status: snapshot.status })}>Plan restore</Button><Button size="small" color="error" disabled={admissionBusy || ['writing', 'deleting'].includes(backup.State)} onClick={() => setDialog({ kind: 'delete', backup })}>Delete backup</Button></Stack>
              </Stack><Box sx={{ mt: 1.5 }}><Digest value={backup.SHA256} /></Box>{backup.ErrorCode && <Alert severity={backup.State === 'cancelled' ? 'info' : 'error'} sx={{ mt: 2 }}>{recoveryMessages[backup.ErrorCode] || 'This backup could not be completed. Check its job before trying again.'}</Alert>}
            </Box>)}
          </Box>}
          <TablePagination component="div" count={snapshot.backups.TotalRecordCount} page={Math.floor(backupStart / pageSize)} rowsPerPage={pageSize} rowsPerPageOptions={[pageSize]} onPageChange={(_, page) => setBackupStart(page * pageSize)} labelRowsPerPage="Backups per page" sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 1, sm: 2 } }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } } }} />
        </Paper>
        <Paper component="section" aria-labelledby="backup-jobs-heading" variant="outlined" aria-busy={loading}>
          <Box sx={sectionSpacing}><Stack direction="row" sx={{ gap: 1, justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap' }}><Typography id="backup-jobs-heading" tabIndex={-1} variant="h3" component="h2">Backup jobs</Typography><Typography variant="caption" color="text.secondary" role="status">{polling ? 'Checking active jobs automatically' : 'Showing the last confirmed state'}</Typography></Stack><Typography variant="body2" color="text.secondary" sx={{ mt: 0.6 }}>An accepted request starts a job. Review its state to confirm the outcome.</Typography></Box>
          {operations.length === 0 ? <Box sx={{ ...sectionSpacing, pt: 0 }}><Typography variant="body2" color="text.secondary">No backup or recovery jobs have been recorded.</Typography></Box> : <Box component="ul" aria-label="Backup jobs" sx={{ listStyle: 'none', m: 0, p: 0 }}>
            {operations.map((operation) => <Box component="li" key={operation.Id} data-testid={`operation-${operation.Id}`} sx={{ ...sectionSpacing, borderTop: 1, borderColor: 'divider' }}>
              <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 2, justifyContent: 'space-between' }}><Box sx={{ minWidth: 0 }}><Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', alignItems: 'center' }}><Typography variant="h4" component="h3">{kindLabels[operation.Kind]}</Typography><StateChip state={operation.State} /></Stack><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{phaseLabels[operation.Phase]} · Updated {dateTime(operation.UpdatedAt)}</Typography><Typography variant="caption" component="div" className="mono" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{operation.Id}</Typography></Box><Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5, flexShrink: 0, alignItems: 'flex-start' }}>{operation.Kind === 'restore' && <Button size="small" disabled={Boolean(actionBusy)} onClick={() => setDialog({ kind: 'inspect', operationId: operation.Id })}>Inspect plan</Button>}{operation.CanCancel && <Button size="small" color="error" disabled={Boolean(actionBusy) || Boolean(loadError) || loading} onClick={() => void cancel(operation)}>Cancel job</Button>}</Stack></Stack>
              {operation.State === 'ready' && operation.CanApply && <Alert severity="info" sx={{ mt: 2 }}>The backup was checked and the restore plan is ready. Inspect its source and settings before applying it.</Alert>}
              {operation.ErrorCode && <Alert severity={operation.State === 'cancelled' ? 'info' : 'error'} sx={{ mt: 2 }}>{recoveryMessages[operation.ErrorCode] || 'This job did not complete. Refresh its state and review the current server before trying again.'}</Alert>}
              {operation.State === 'completed' && (operation.Kind === 'restore' || operation.Kind === 'rollback') && <Alert severity="success" sx={{ mt: 2 }}>The server records this recovery job as completed. Reconnect to confirm access and review your server data before resuming normal work.</Alert>}
            </Box>)}
          </Box>}
          <TablePagination component="div" count={snapshot.operations.TotalRecordCount} page={Math.floor(operationStart / pageSize)} rowsPerPage={pageSize} rowsPerPageOptions={[pageSize]} onPageChange={(_, page) => setOperationStart(page * pageSize)} labelRowsPerPage="Jobs per page" sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 1, sm: 2 } }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } } }} />
        </Paper>
        <Paper component="section" aria-labelledby="rollback-heading" variant="outlined" sx={sectionSpacing}>
          <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', gap: 2 }}><Box><Stack direction="row" sx={{ gap: 1, alignItems: 'center' }}><ShieldOutlined color="primary" /><Typography id="rollback-heading" variant="h3" component="h2">Retained rollback copy</Typography></Stack><Typography variant="body2" color="text.secondary" sx={{ mt: 1, maxWidth: 720 }}>{snapshot.status.Rollback.Available ? `A retained copy of ${snapshot.status.Rollback.ServerName || 'this server'}${snapshot.status.Rollback.CreatedAt ? ` from ${dateTime(snapshot.status.Rollback.CreatedAt)}` : ''} is available. Rolling back requires confirmation and signs everyone out.` : recoveryMessages[snapshot.status.Rollback.UnavailableReason] || 'No verified rollback copy is available. Applying a restore preserves the previously active server for recovery.'}</Typography></Box><Button variant="outlined" color="secondary" startIcon={<RestoreRounded />} disabled={admissionBusy || !snapshot.status.Rollback.Available || !snapshot.status.RestoreAvailable} onClick={() => setDialog({ kind: 'rollback', status: snapshot.status })} sx={{ alignSelf: 'flex-start', flexShrink: 0 }}>Roll back</Button></Stack>
        </Paper>
      </>}
    </Stack>
    {(dialog?.kind === 'create' || dialog?.kind === 'import') && snapshot && <BackupAdmissionDialog {...callbacks} kind={dialog.kind} status={snapshot.status} />}
    {dialog?.kind === 'plan' && <RestorePlanDialog {...callbacks} backup={dialog.backup} status={dialog.status} />}
    {dialog?.kind === 'delete' && <DeleteBackupDialog {...callbacks} backup={dialog.backup} />}
    {dialog?.kind === 'inspect' && <InspectPlanDialog {...callbacks} operationId={dialog.operationId} applicationUncertain={unknownAttempt?.ApplyingRevision !== undefined} />}
    {dialog?.kind === 'rollback' && <RollbackDialog {...callbacks} status={dialog.status} />}
  </>;
}
