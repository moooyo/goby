import { useLayoutEffect, useRef } from 'react';
import type { FormEvent } from 'react';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { RefreshRounded, SubjectRounded } from './icons';
import type { PlaybackMessageCounts, PlaybackMessageInput, PlayingClientCount } from './playbackApi';
import { Alert, Button, CircularProgress, DialogActions, DialogContent, DialogTitle, MenuItem, Stack, TextField } from './ui';

export type PlaybackAudience = { state: 'loading' } | { state: 'unavailable' } | { state: 'ready'; value: PlayingClientCount };
export interface MaintenanceDraft { header: string; text: string; timeoutMs: number }
export const emptyMaintenanceDraft = { header: '', text: '', timeoutMs: 10000 };
export const restartReminder = 'The server will restart in 5 minutes. Playback will be briefly interrupted.';
const messageTemplates = [restartReminder, 'Maintenance begins at 23:00 tonight and is expected to last 10 minutes.'];
const displayTimes = [5000, 10000, 30000, 60000];
const encoder = new TextEncoder();
const invalidText = /[\p{Cc}\uD800-\uDFFF]/u;
const blankText = /^\p{White_Space}*$/u;
const headerLimit = 128;
const textLimit = 2048;

export function PlaybackAudienceStatus({ audience, forRestart = false }: { audience: PlaybackAudience; forRestart?: boolean }) {
  const { t, locale } = useTranslation();
  if (audience.state !== 'ready') return <span role="status">{t(audience.state === 'loading' ? 'Checking how many clients are playing…' : 'The number of playing clients could not be checked.')}</span>;
  const { count, atCapacity } = audience.value;
  const one = count === 1 && !atCapacity;
  return <span role="status">{t(forRestart
    ? one ? '{count} client is currently playing.' : '{count} clients are currently playing.'
    : one ? 'Playback snapshot: {count} client is playing.' : 'Playback snapshot: {count} clients are playing.',
  { count: count.toLocaleString(locale) + (atCapacity ? '+' : '') })}</span>;
}

export function MaintenanceMessageReview({ required, loading, readback, onReview }: { required: boolean; loading: boolean; readback?: PlaybackAudience; onReview: () => void }) {
  return <Alert severity="warning" className="server-maintenance-notice" action={required && <Button size="small" onClick={onReview} disabled={loading} startIcon={<RefreshRounded />}>Refresh playback count</Button>}>
    The message result is unknown. A refreshed count cannot confirm delivery. Sending again could show a duplicate.
    {readback && <div className="server-maintenance-readback"><PlaybackAudienceStatus audience={readback} /></div>}
  </Alert>;
}

export function MaintenanceMessageOutcome({ counts, onDismiss }: { counts: PlaybackMessageCounts; onDismiss: () => void }) {
  const { t, locale } = useTranslation();
  const queued = t(counts.queued === 1 ? 'Queued for {count} client.' : 'Queued for {count} clients.', { count: counts.queued.toLocaleString(locale) });
  const skipped = [
    ['unsupported', '{count} client did not support or allow messages.', '{count} clients did not support or allow messages.'],
    ['offline', '{count} client was disconnected.', '{count} clients were disconnected.'],
    ['unavailable', '{count} client was unavailable.', '{count} clients were unavailable.'],
  ] as const;
  const outcome = skipped.filter(([key]) => counts[key] > 0).map(([key, singular, plural]) => t(counts[key] === 1 ? singular : plural, { count: counts[key].toLocaleString(locale) }));
  return <Alert severity={counts.queued === 0 || outcome.length ? 'warning' : 'success'} className="server-maintenance-notice" onClose={onDismiss}>
    {[queued, ...outcome, t('Queuing does not confirm display on the client.')].join(' ')}
  </Alert>;
}

export function MaintenanceMessageForm({ audience, draft, fromRestart, busy, disabled, error, unknown, reviewRequired, onRefresh, onClose, onDraftChange, onSend }: {
  audience: PlaybackAudience;
  draft: MaintenanceDraft;
  fromRestart: boolean;
  busy: boolean;
  disabled: boolean;
  error: unknown;
  unknown: boolean;
  reviewRequired: boolean;
  onRefresh: () => void;
  onClose: () => void;
  onDraftChange: (patch: Partial<MaintenanceDraft>) => void;
  onSend: (input: PlaybackMessageInput) => void;
}) {
  const { t, locale } = useTranslation();
  const header = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    // The restart reminder swaps content in an already-open native modal.
    if (header.current?.closest('dialog[open]')) header.current.focus({ preventScroll: true });
  }, []);
  const editable = !busy && !disabled && !reviewRequired;
  const headerBytes = encoder.encode(draft.header).length;
  const textBytes = encoder.encode(draft.text).length;
  const headerIssue = invalidText.test(draft.header) ? 'Use valid single-line text without control characters.'
    : headerBytes > headerLimit ? 'The title must be at most 128 UTF-8 bytes.' : undefined;
  const textIssue = invalidText.test(draft.text) ? 'Use valid single-line text without control characters.'
    : textBytes > textLimit ? 'The message must be at most 2,048 UTF-8 bytes.'
      : draft.text && blankText.test(draft.text) ? 'Enter a message, not just spaces.' : undefined;
  const canSend = editable && audience.state === 'ready' && !headerIssue && !textIssue && !blankText.test(draft.text) && displayTimes.includes(draft.timeoutMs);
  function byteCount(used: number, limit: number) {
    return t('{used} / {limit} UTF-8 bytes', { used: used.toLocaleString(locale), limit: limit.toLocaleString(locale) });
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSend) return;
    onSend({ Text: draft.text, ...(draft.header ? { Header: draft.header } : {}), TimeoutMs: draft.timeoutMs });
  }
  return <form className="server-maintenance-form" noValidate onSubmit={submit} aria-busy={busy}>
    <DialogTitle id="server-policy-message-title">Send a message to playing clients</DialogTitle>
    <DialogContent><Stack spacing={1.5}>
      <p className="server-maintenance-copy" id="server-policy-message-description">{t('The server checks again for playing clients when you send. Idle clients do not receive the message, and it is not replayed later.')}</p>
      <div className="server-maintenance-audience">
        <PlaybackAudienceStatus audience={audience} />
        {audience.state === 'unavailable' && !reviewRequired && <Button size="small" onClick={onRefresh} disabled={busy || disabled}>Refresh playback count</Button>}
      </div>
      {fromRestart && <p className="server-maintenance-copy">{t('Sending this reminder does not schedule a restart. Return here to restart when you are ready.')}</p>}
      {unknown && <MaintenanceMessageReview required={reviewRequired} loading={audience.state === 'loading'} onReview={onRefresh} />}
      {error != null && <Alert severity="error">{t('The server rejected the message. Nothing was queued.')} {t(errorText(error))}</Alert>}
      <TextField id="maintenance-message-header" label="Title (optional)" inputRef={header} autoFocus fullWidth autoComplete="off" placeholder="Server maintenance" value={draft.header} disabled={!editable} onChange={(event) => onDraftChange({ header: event.target.value })} error={Boolean(headerIssue)} helperText={headerIssue ?? byteCount(headerBytes, headerLimit)}
        slotProps={{ htmlInput: { maxLength: headerLimit + 1 } }} />
      <TextField id="maintenance-message-text" label="Message" rows={3} multiline required fullWidth value={draft.text} disabled={!editable} onChange={(event) => onDraftChange({ text: event.target.value })} error={Boolean(textIssue)} helperText={textIssue ?? byteCount(textBytes, textLimit)}
        slotProps={{ htmlInput: { 'aria-label': 'Message', maxLength: textLimit + 1 } }} />
      <TextField id="maintenance-message-duration" className="server-maintenance-duration" select label="Display duration" value={draft.timeoutMs} disabled={!editable} onChange={(event) => onDraftChange({ timeoutMs: Number(event.target.value) })}>
        {displayTimes.map((value) => <MenuItem key={value} value={value}>{t('{seconds} seconds', { seconds: value / 1000 })}</MenuItem>)}
      </TextField>
      <div className="server-maintenance-templates" role="group" aria-label={t('Common messages')}>
        <p>{t('Common messages')}</p>
        {messageTemplates.map((template) => <Button key={template} type="button" size="small" variant="outlined" disabled={!editable} onClick={() => onDraftChange({ text: t(template) })}>{t(template)}</Button>)}
      </div>
    </Stack></DialogContent>
    <DialogActions>
      <p className="server-maintenance-copy">{t('The server queues the message for compatible, connected clients. It cannot confirm that every client displays it.')}</p>
      <Button type="button" onClick={onClose} disabled={busy}>Cancel</Button>
      <Button type="submit" variant="contained" disabled={!canSend} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SubjectRounded />}>{busy ? 'Sending message…' : 'Send message'}</Button>
    </DialogActions>
  </form>;
}
