import { useEffect, useRef, useState } from 'react';
import { ApiError, isAbortError } from './api';
import { useTranslation } from './i18n';
import { StopRounded } from './icons';
import { Alert, Button, Dialog, DialogActions, DialogContent, DialogTitle, LinearProgress, Stack, Typography } from './ui';

export function isUnknownUserWrite(error: unknown): boolean {
  return error instanceof ApiError && (error.status >= 500 || ['network_error', 'invalid_response'].includes(error.code));
}

export function isUserRevisionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'revision_conflict';
}

export async function boundedUserRequest<T>(action: (signal: AbortSignal) => Promise<T>, lifetime?: AbortSignal): Promise<T> {
  const deadline = AbortSignal.timeout(15_000);
  try {
    return await action(lifetime ? AbortSignal.any([lifetime, deadline]) : deadline);
  } catch (error) {
    if (isAbortError(error) && deadline.aborted && !lifetime?.aborted) throw new ApiError('The request timed out. Its result could not be confirmed.', { code: 'network_error' });
    throw error;
  }
}

export type BatchOutcome = 'changed' | 'conflict' | 'lastAdministrator' | 'ineligible' | 'failed' | 'denied' | 'unconfirmed';
export type BatchStep = BatchOutcome | 'cancelled' | 'signedOut';

type Totals = Record<BatchOutcome, number>;
const summarize: Record<Exclude<BatchOutcome, 'changed'>, string> = {
  conflict: 'Skipped because another administrator changed the account: {count}. Refresh the list before trying again.',
  lastAdministrator: 'Skipped to keep at least one enabled administrator: {count}.',
  ineligible: 'Skipped because the target no longer qualified or was already changed: {count}.',
  failed: 'Could not be completed: {count}. Review the refreshed list before trying again.',
  denied: 'The request was not authorized. No further changes were submitted.',
  unconfirmed: 'The result of {count} request(s) could not be confirmed. Review the refreshed list before trying again.',
};

interface BatchProgress { totals: Totals; processed: number; finished: boolean; stopped: boolean }

interface Props<T> {
  kind: 'disable' | 'signOut';
  targets: readonly T[];
  confirmation: { title: string; description: string; note?: string; selfWarning?: string; action: string };
  single?: boolean;
  submit: (target: T, shouldStop: () => boolean, signal: AbortSignal) => Promise<BatchStep>;
  onClose: () => void;
  onBusyChange: (busy: boolean) => void;
  onFinished: () => void;
}

export function UserBatchDialog<T>({ kind, targets, confirmation, single = false, submit, onClose, onBusyChange, onFinished }: Props<T>) {
  const { t, locale } = useTranslation();
  const [progress, setProgress] = useState<BatchProgress>();
  const [stopping, setStopping] = useState(false);
  const inFlight = useRef<AbortController | null>(null);
  const stop = useRef(false);
  const progressButton = useRef<HTMLButtonElement>(null);
  const total = targets.length;
  const count = (value: number) => value.toLocaleString(locale);
  const phase = !progress ? 'confirm' : progress.finished ? 'finished' : 'running';
  const running = phase === 'running';
  const disable = kind === 'disable';

  useEffect(() => () => inFlight.current?.abort(), []);
  useEffect(() => { if (phase !== 'confirm') progressButton.current?.focus(); }, [phase]);

  async function start() {
    if (inFlight.current || progress || total === 0) return;
    const controller = new AbortController();
    inFlight.current = controller;
    onBusyChange(true);
    const totals: Totals = { changed: 0, conflict: 0, lastAdministrator: 0, ineligible: 0, failed: 0, denied: 0, unconfirmed: 0 };
    let processed = 0;
    let halted = false;
    const shouldStop = () => stop.current || controller.signal.aborted;
    const update = (finished = false) => setProgress({ totals: { ...totals }, processed, finished, stopped: stop.current && processed < total });
    update();
    try {
      for (const target of targets) {
        if (shouldStop()) break;
        let result: BatchStep;
        try {
          result = await submit(target, shouldStop, controller.signal);
        } catch (error) {
          if (controller.signal.aborted || isAbortError(error)) break;
          if (error instanceof ApiError && (error.status === 401 || error.code === 'session_changed')) result = 'signedOut';
          else if (error instanceof ApiError && error.status === 403) result = 'denied';
          else if (isUserRevisionConflict(error)) result = 'conflict';
          else if (error instanceof ApiError && error.code === 'last_administrator') result = 'lastAdministrator';
          else if (error instanceof ApiError && error.status === 404) result = 'ineligible';
          else if (isUnknownUserWrite(error)) result = 'unconfirmed';
          else result = 'failed';
        }
        if (result === 'signedOut') { halted = true; break; }
        if (controller.signal.aborted || result === 'cancelled') break;
        totals[result] += 1;
        processed += 1;
        update();
        // Do not continue without confirmed authority or after an ambiguous write.
        if (result === 'unconfirmed' || result === 'denied') break;
      }
      if (!halted && !controller.signal.aborted) { update(true); onFinished(); }
    } finally {
      inFlight.current = null;
      if (!controller.signal.aborted && !halted) onBusyChange(false);
    }
  }

  function close() {
    if (!inFlight.current) onClose();
  }

  const title = phase === 'confirm' ? confirmation.title : running
    ? (disable ? t('Disabling accounts') : t(single ? 'Signing out login' : 'Signing out logins'))
    : progress?.totals.unconfirmed || progress?.totals.denied ? t('Review the result') : progress?.stopped ? t('Stopped') : t('Batch complete');
  const completionLabel = disable ? 'Accounts disabled: {count}' : 'Logins signed out: {count}';
  const itemLabel = disable ? 'Submitting account {position}…' : 'Signing out login {position}…';
  const status = progress && (progress.finished ? t(completionLabel, { count: count(progress.totals.changed) }) : stopping
    ? t('Stopping after the current request…') : t(itemLabel, { position: count(Math.min(progress.processed + 1, total)) }));

  return (
    <Dialog open maxWidth="xs" onClose={close} className="users-batch-dialog" data-running={running} aria-label={title} aria-describedby={phase === 'confirm' ? 'user-batch-description' : undefined}>
      <DialogTitle id="user-batch-title">{title}</DialogTitle>
      <DialogContent>
        {!progress ? <Stack spacing={1.5}>
          <Typography id="user-batch-description" variant="body2" color="text.secondary">{confirmation.description}</Typography>
          {confirmation.note && <Typography variant="body2" color="text.secondary">{confirmation.note}</Typography>}
          {confirmation.selfWarning && <Alert severity="warning">{confirmation.selfWarning}</Alert>}
        </Stack> : <Stack spacing={1.5}>
          <div className="users-batch-progress" role="status"><span>{status}</span><span className="access-mono">{count(progress.processed)} / {count(total)}</span></div>
          <LinearProgress variant="determinate" value={progress.processed * 100 / total} aria-label="Batch progress" aria-valuetext={t('{completed} of {total} processed', { completed: count(progress.processed), total: count(total) })} />
          {running && !single && <p className="users-help">{t(disable ? 'Accounts are submitted one at a time. Keep this page open. Stopping does not undo changes already made.' : 'Logins are signed out one at a time. Keep this page open. Stopping does not restore logins already ended.')}</p>}
          {progress.finished && <>
            {progress.stopped && progress.totals.unconfirmed === 0 && <Alert severity="info">{t(disable ? 'Stopped. Confirmed changes stay in place; accounts not submitted were not changed.' : 'Stopped. Confirmed sign-outs stay in effect; the remaining logins were not submitted.')}</Alert>}
            {(Object.keys(summarize) as (keyof typeof summarize)[]).filter((result) => progress.totals[result] > 0).map((result) => <Alert key={result} severity={result === 'failed' ? 'error' : 'warning'}>{t(summarize[result], { count: count(progress.totals[result]) })}</Alert>)}
          </>}
        </Stack>}
      </DialogContent>
      <DialogActions>
        {phase === 'confirm' ? <><Button autoFocus onClick={close} color="secondary">Cancel</Button><Button variant="contained" color="error" onClick={() => void start()}>{confirmation.action}</Button></>
          : running ? (!single && <Button ref={progressButton} variant="outlined" startIcon={<StopRounded />} disabled={stopping} onClick={() => { stop.current = true; setStopping(true); }}>Stop</Button>)
            : <Button ref={progressButton} variant="contained" onClick={close}>Done</Button>}
      </DialogActions>
    </Dialog>
  );
}
