import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, MenuItem, Paper, Skeleton, Stack, Switch, TextField, Tooltip, Typography } from './ui';
import { AddRounded, DeleteOutlineRounded, RefreshRounded } from './icons';
import { SaveOutlined } from './icons';
import { CheckCircleRounded } from './icons';
import { CloudDoneRounded } from './icons';
import { EditOutlined } from './icons';
import { ApiError, isAbortError } from './api';
import { ErrorNotice, PageHeading } from './components';
import { PasswordField } from './formFields';
import { notificationDraft, notificationsApi, parseNotificationDraft } from './notificationsApi';
import type { NotificationDraft, NotificationSettings } from './notificationsApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import { confirmAction, useTranslation } from './i18n';
import { destinationDraft, destinationSecretTargetChanged, notificationDestinationsApi, parseDestinationDraft } from './notificationDestinationsApi';
import type { NotificationDestination, NotificationDestinationDraft, NotificationDestinations } from './notificationDestinationsApi';
import './backup-notifications.css';
import './notification-destinations.css';

const notificationEventLabels: Record<string, string> = { CatalogInvalidated: 'Catalog changes', UserDataInvalidated: 'User data changes' };

function ClientNotificationReceiver({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const { locale } = useTranslation();
  const [saved, setSaved] = useState<NotificationSettings>(); const [draft, setDraft] = useState<NotificationDraft>();
  const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false); const [revision, setRevision] = useState(0); const [notice, setNotice] = useState('');
  const [editing, setEditing] = useState(false);
  const mounted = useRef(true); const inFlight = useRef(false);
  const writeRequest = useRef<AbortController | undefined>(undefined);
  const dirty = Boolean(saved && draft && JSON.stringify(draft) !== JSON.stringify(notificationDraft(saved)));
  const changedCount = saved && draft ? [
    draft.enabled !== saved.Enabled,
    draft.endpoint !== saved.Endpoint,
    draft.networks !== saved.AllowedNetworks.join('\n'),
    draft.credentialMode !== 'keep' || draft.credential !== '',
  ].filter(Boolean).length : 0;
  const parsed = useMemo(() => saved && draft ? parseNotificationDraft(draft, saved) : undefined, [saved, draft]);
  const disabled = loading || busy || review;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved notification settings and leave this page?');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; writeRequest.current?.abort(); }; }, []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(null);
    void notificationsApi.get({ signal: controller.signal }).then((value) => { if (!controller.signal.aborted) { setSaved(value); setDraft(notificationDraft(value)); setReview(false); setNotice(''); } })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  function change(patch: Partial<NotificationDraft>) { if (!disabled) { setDraft((value) => value ? { ...value, ...patch } : value); setError(null); setNotice(''); } }
  function discard() { if (!disabled && saved) { setDraft(notificationDraft(saved)); setError(null); setNotice(''); } }
  function reload() { if (!inFlight.current && (!dirty || confirmAction('Discard your draft and reload saved notification settings?'))) setRevision((value) => value + 1); }
  function closeEditor() {
    if (inFlight.current || busy || (dirty && !confirmAction('Discard unsaved notification settings?'))) return;
    if (saved) setDraft(notificationDraft(saved));
    setEditing(false);
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (inFlight.current || disabled || !dirty || !parsed?.input) return;
    const controller = new AbortController(); writeRequest.current = controller;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      const value = await notificationsApi.update(parsed.input, { signal: controller.signal });
      if (mounted.current) { setSaved(value); setDraft(notificationDraft(value)); setNotice('Notification settings saved. Receiver credentials are never returned.'); }
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally { if (writeRequest.current === controller) writeRequest.current = undefined; inFlight.current = false; if (mounted.current) setBusy(false); }
  }
  return <Box className="notifications-page" aria-busy={loading || busy}>
    <div className="notification-receiver-heading"><div><Typography component="h2">Player event receiver</Typography><Typography variant="body2" color="text.secondary">Deliver library and user data changes to registered clients.</Typography></div><Stack direction="row" sx={{ gap: 1 }}><Button onClick={reload} disabled={loading || busy} startIcon={<RefreshRounded />}>Reload notifications</Button><Button variant="outlined" onClick={() => setEditing(true)} disabled={!draft || loading || busy} startIcon={<EditOutlined />}>Configure receiver</Button></Stack></div>
    {error != null && !editing && <ErrorNotice error={error} retry={!saved ? reload : undefined} />}
    {review && !editing && <Alert severity="warning">Notification settings changed or the result could not be confirmed. Your draft is kept. Reload the saved configuration before trying again.</Alert>}
    {notice && !editing && <Alert severity="success">{notice}</Alert>}
    {loading && <Skeleton variant="rounded" height={230} aria-label="Loading notification settings" />}
    {saved && <>
      <Paper component="section" className="notification-targets" aria-labelledby="notification-targets-title" variant="outlined">
        <Stack className="delivery-card-heading" direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography component="h2" id="notification-targets-title">Notification targets</Typography><Typography variant="caption" className="mono" color="text.secondary">{saved.Endpoint ? 1 : 0}</Typography></Stack>
        <Box className="notification-target-row">
          <Box className="notification-target-main"><Box className="delivery-icon-tile"><CloudDoneRounded /></Box><Box sx={{ minWidth: 0 }}><Typography sx={{ fontWeight: 600 }}>HTTPS receiver</Typography><Typography className="mono" variant="caption" component="div" color="text.secondary" sx={{ overflowWrap: 'anywhere', mt: 0.25 }}>{saved.Endpoint || 'No receiver configured'}</Typography></Box></Box>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><Box className={`delivery-status-dot ${saved.Enabled ? 'is-enabled' : ''}`} aria-hidden="true" /><Typography variant="body2" color="text.secondary">{saved.Enabled ? 'Enabled' : 'Disabled'}</Typography></Stack>
          <Typography variant="caption" color="text.secondary">{saved.HasReceiverCredential ? 'Credential configured' : 'Credential required'}</Typography>
          <Button size="small" variant="outlined" onClick={() => setEditing(true)} disabled={loading || busy}>Edit target</Button>
        </Box>
        <Typography className="delivery-card-note" variant="body2" color="text.secondary">The server delivers supported events to one HTTPS receiver.</Typography>
      </Paper>
      <Box className="notification-details-grid">
        <Paper component="section" className="notification-events" variant="outlined" aria-labelledby="notification-events-title">
          <Stack className="delivery-card-heading" direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography component="h2" id="notification-events-title">Supported events</Typography><Typography variant="caption" className="mono" color="text.secondary">{saved.SupportedEvents.length}</Typography></Stack>
          <Box className="notification-event-header"><Typography variant="caption">Event</Typography><Typography variant="caption">HTTPS receiver</Typography></Box>
          {saved.SupportedEvents.length ? saved.SupportedEvents.map((event) => <Box className="notification-event-row" key={event}><Box sx={{ minWidth: 0 }}><Typography sx={{ fontWeight: 500 }}>{notificationEventLabels[event] ?? event}</Typography><Typography variant="caption" className="mono" color="text.secondary">{event}</Typography></Box><Tooltip title={saved.Enabled ? 'Enabled for the configured receiver' : 'Enable the receiver to send these events'}><span className={saved.Enabled ? 'delivery-event-enabled' : 'delivery-event-disabled'}><CheckCircleRounded sx={{ fontSize: 17 }} /><Typography component="span" variant="caption">{saved.Enabled ? 'Enabled' : 'Disabled'}</Typography></span></Tooltip></Box>) : <Typography className="delivery-empty" color="text.secondary">No supported events were reported by this server.</Typography>}
          <Typography className="delivery-card-note" variant="body2" color="text.secondary">Events are reported by the server. Individual event subscriptions are not configurable.</Typography>
        </Paper>
        <Stack spacing={2}>
          <Paper component="section" className="notification-queue" variant="outlined" aria-labelledby="notification-queue-title"><Stack className="delivery-card-heading" direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><Typography component="h2" id="notification-queue-title">Delivery queue</Typography><Typography className="mono" variant="caption" color="text.secondary">Revision {saved.Revision}</Typography></Stack><Box sx={{ px: 2, pb: 2 }}><Typography className="delivery-queue-count">{saved.PendingCount.toLocaleString(locale)}</Typography><Typography variant="body2" color="text.secondary">Pending notifications</Typography><Typography variant="caption" component="p" color="text.secondary" sx={{ mt: 1, mb: 0 }}>Reload to refresh the delivery queue count.</Typography></Box></Paper>
          <Paper component="section" variant="outlined" aria-labelledby="notification-history-title"><Stack className="delivery-card-heading" direction="row"><Typography component="h2" id="notification-history-title">Recent deliveries</Typography></Stack><Typography className="delivery-empty" variant="body2" color="text.secondary">Delivery history is not available from this server.</Typography></Paper>
        </Stack>
      </Box>
    </>}
    <Dialog open={editing} onClose={closeEditor} fullWidth maxWidth="sm" aria-labelledby="notification-editor-title">
      <Box component="form" noValidate onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)} className="notification-editor-form" aria-busy={busy || loading}>
        <DialogTitle id="notification-editor-title">HTTPS receiver</DialogTitle>
        <DialogContent><Stack spacing={2} sx={{ pt: 1 }}>
          <Typography variant="body2" color="text.secondary">Use a receiver that implements the Goby notification contract. Saved credentials are never returned or filled back into this form.</Typography>
          {error != null && <ErrorNotice error={error} retry={!saved ? reload : undefined} />}
          {review && <Alert severity="warning">Notification settings changed or the result could not be confirmed. Your draft is kept. Reload the saved configuration before trying again.</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {saved && draft && <>
            <FormControlLabel sx={{ m: 0, gap: 1 }} control={<Switch checked={draft.enabled} disabled={disabled} onChange={(event) => change({ enabled: event.target.checked })} />} label="Enable notifications" />
            <TextField fullWidth label="Receiver endpoint" value={draft.endpoint} disabled={disabled} onChange={(event) => change({ endpoint: event.target.value })} error={Boolean(parsed?.errors.Endpoint)} helperText={parsed?.errors.Endpoint ?? 'An HTTPS URL without credentials, query parameters, or a fragment. Required when enabled.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
            <TextField fullWidth multiline minRows={2} maxRows={8} label="Allowed private networks" value={draft.networks} disabled={disabled} onChange={(event) => change({ networks: event.target.value })} error={Boolean(parsed?.errors.AllowedNetworks)} helperText={parsed?.errors.AllowedNetworks ?? 'Optional canonical CIDRs for a self-hosted receiver, one per line (maximum 32). Leave empty to allow only public receiver addresses.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
            <TextField fullWidth select label="Receiver credential action" value={draft.credentialMode} disabled={disabled} onChange={(event) => change({ credentialMode: event.target.value as NotificationDraft['credentialMode'], credential: '' })} helperText="Keep the saved credential, replace it, or clear it after disabling notifications."><MenuItem value="keep">Keep current credential</MenuItem><MenuItem value="replace">Replace credential</MenuItem><MenuItem value="clear">Clear credential</MenuItem></TextField>
            {draft.credentialMode === 'replace' && <PasswordField fullWidth label="New receiver credential" value={draft.credential} disabled={disabled} onChange={(event) => change({ credential: event.target.value })} error={Boolean(parsed?.errors.ReceiverCredential)} helperText={parsed?.errors.ReceiverCredential ?? '16–2,048 visible ASCII characters without spaces. The saved value is never filled back into this form.'} autoComplete="new-password" />}
            {draft.credentialMode !== 'replace' && parsed?.errors.ReceiverCredential && <Alert severity="error">{parsed.errors.ReceiverCredential}</Alert>}
            {draft.credentialMode === 'clear' && !draft.enabled && <Typography variant="body2" color="text.secondary">Saving will remove the stored receiver credential.</Typography>}
            <Typography variant="caption" color="text.secondary">Configure trusted TLS certificates in the server deployment.</Typography>
          </>}
        </Stack></DialogContent>
        <DialogActions><Typography role="status" aria-live="polite" variant="caption" color="text.secondary" sx={{ flex: 1 }}>{dirty ? `${changedCount} unsaved ${changedCount === 1 ? 'change' : 'changes'}` : 'All settings saved'}</Typography><Button type="button" onClick={closeEditor} disabled={busy || loading}>Close</Button>{review ? <Button type="button" onClick={reload} disabled={busy || loading}>Reload notifications</Button> : <>{dirty && <Button type="button" onClick={discard} disabled={disabled}>Discard changes</Button>}<Button type="submit" variant="contained" disabled={disabled || !dirty || !parsed?.input} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving notifications...' : 'Save notifications'}</Button></>}</DialogActions>
      </Box>
    </Dialog>
  </Box>;
}

const destinationOutcomeLabels: Record<string, string> = {
  delivered: 'Delivered', pending: 'Queued', sending: 'Sending', failed: 'Delivery failed',
  delivery_unconfirmed: 'Delivery could not be confirmed', interrupted: 'Delivery interrupted',
  configuration_changed: 'Configuration changed', queue_capacity: 'Delivery queue is full',
  secret_unavailable: 'Saved credential is unavailable', tls_failed: 'TLS connection failed',
  authentication_failed: 'Authentication failed', recipient_rejected: 'Recipient rejected', authority_revoked: 'Owner access revoked',
  endpoint_invalid: 'Target address is invalid', target_invalid: 'Target rejected delivery', request_rejected: 'Target rejected delivery',
  network_unavailable: 'Target could not be reached', cancelled: 'Cancelled', test_queued: 'Test queued',
};
const destinationEventLabels: Record<string, string> = { UserChanges: 'User changes', SignIn: 'Sign-ins', LibraryChanges: 'Library changes', ScanFinished: 'Scans finished', TaskFinished: 'Tasks finished', BackupFinished: 'Backups finished', MetadataChanges: 'Metadata changes' };
function uncertainDestinationMutation(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500 || ['invalid_response', 'session_changed'].includes(error.code);
}
function DestinationGlyph({ mail = false }: { mail?: boolean }) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{mail ? <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="m3 7 9 6 9-6" /></> : <><path d="m10 13 4-4M8 16l-2 2a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m4 0 2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 0)" /></>}</svg>;
}
function NotificationDestinationsPanel({ onNavigationGuardChange, reloadToken }: { onNavigationGuardChange: UserNavigationGuardChange; reloadToken: number }) {
  const { t, locale, formatCount } = useTranslation();
  const [data, setData] = useState<NotificationDestinations>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [requestRevision, setRequestRevision] = useState(0);
  const [editor, setEditor] = useState<{ saved?: NotificationDestination; draft: NotificationDestinationDraft }>();
  const [mutationError, setMutationError] = useState<unknown>(null);
  const [busy, setBusy] = useState<'save' | 'delete' | 'test' | 'readback' | ''>('');
  const [review, setReview] = useState(false);
  const [notice, setNotice] = useState('');
  const [testTarget, setTestTarget] = useState<string>();
  const [deleteTarget, setDeleteTarget] = useState<NotificationDestination>();
  const inFlight = useRef<AbortController | undefined>(undefined);
  const backgroundRead = useRef<AbortController | undefined>(undefined);
  const readbackActive = useRef(false);
  const readGeneration = useRef(0);
  const mounted = useRef(true);
  const dirty = Boolean(editor && JSON.stringify(editor.draft) !== JSON.stringify(destinationDraft(editor.saved)));
  const parsed = useMemo(() => editor ? parseDestinationDraft(editor.draft, editor.saved) : undefined, [editor]);
  useUserDraftNavigation(dirty || review, Boolean(busy), onNavigationGuardChange, 'Discard unsaved notification target changes and leave this page?');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; inFlight.current?.abort(); }; }, []);
  useEffect(() => {
    let disposed = false;
    let pending: AbortController | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      if (disposed || document.visibilityState === 'hidden' || pending) return;
      if (readbackActive.current) { timer = setTimeout(() => { void poll(); }, 5000); return; }
      const generation = readGeneration.current;
      const controller = new AbortController(); pending = controller;
      backgroundRead.current = controller;
      setLoading(true);
      try {
        const value = await notificationDestinationsApi.list({ signal: controller.signal });
        if (!disposed && generation === readGeneration.current && !readbackActive.current) { setData(value); setLoadError(null); }
      } catch (error) {
        if (!disposed && generation === readGeneration.current && !readbackActive.current && !isAbortError(error)) setLoadError(error);
      } finally {
        if (pending === controller) pending = undefined;
        if (backgroundRead.current === controller) backgroundRead.current = undefined;
        if (!disposed) { setLoading(false); timer = setTimeout(() => { void poll(); }, 5000); }
      }
    }
    const visibilityChanged = () => {
      clearTimeout(timer);
      if (document.visibilityState === 'hidden') pending?.abort();
      else void poll();
    };
    document.addEventListener('visibilitychange', visibilityChanged);
    void poll();
    return () => { disposed = true; clearTimeout(timer); pending?.abort(); document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [requestRevision, reloadToken]);
  function refresh() { setRequestRevision((value) => value + 1); }
  function openEditor(value?: NotificationDestination) {
    if (busy || review) return;
    setEditor({ saved: value, draft: destinationDraft(value) }); setMutationError(null); setNotice('');
  }
  function closeEditor() {
    if (busy || review || dirty && !confirmAction('Discard unsaved notification target changes?')) return;
    setEditor(undefined); setMutationError(null);
  }
  function change(patch: Partial<NotificationDestinationDraft>) {
    if (busy || review) return;
    setEditor((current) => current ? { ...current, draft: { ...current.draft, ...patch } } : current);
    setMutationError(null); setNotice('');
  }
  async function readBack() {
    if (busy || inFlight.current || dirty && !confirmAction('Discard your draft and reload saved notification targets?')) return;
    const controller = new AbortController(); inFlight.current = controller;
    readbackActive.current = true; readGeneration.current++;
    backgroundRead.current?.abort();
    setBusy('readback'); setMutationError(null); setNotice('');
    try {
      const saved = await notificationDestinationsApi.list({ signal: controller.signal });
      if (mounted.current && inFlight.current === controller && !controller.signal.aborted) {
        setData(saved); setLoadError(null); setEditor(undefined); setDeleteTarget(undefined); setReview(false);
      }
    } catch (error) {
      if (mounted.current && !isAbortError(error)) { setMutationError(error); setReview(true); }
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined;
      readbackActive.current = false;
      if (mounted.current) { setBusy(''); refresh(); }
    }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editor || !parsed?.input || !dirty || busy || review || inFlight.current) return;
    const controller = new AbortController(); inFlight.current = controller; setBusy('save'); setMutationError(null); setNotice('');
    try {
      const value = await notificationDestinationsApi.save(editor.saved?.Id, parsed.input, { signal: controller.signal });
      if (mounted.current) {
        setEditor({ saved: value, draft: destinationDraft(value) });
        setData((current) => current ? { ...current, Items: current.Items.some((item) => item.Id === value.Id) ? current.Items.map((item) => item.Id === value.Id ? value : item) : [...current.Items, value] } : current);
        setNotice('Notification target saved.'); refresh();
      }
    } catch (error) {
      if (mounted.current && !isAbortError(error)) { setMutationError(error); if (uncertainDestinationMutation(error)) setReview(true); }
    } finally { if (inFlight.current === controller) inFlight.current = undefined; if (mounted.current) setBusy(''); }
  }
  async function remove() {
    if (!deleteTarget || busy || review || inFlight.current) return;
    const controller = new AbortController(); inFlight.current = controller; setBusy('delete'); setMutationError(null); setNotice('');
    try {
      await notificationDestinationsApi.remove(deleteTarget.Id, deleteTarget.Revision, { signal: controller.signal });
      if (mounted.current) { setData((current) => current ? { ...current, Items: current.Items.filter((item) => item.Id !== deleteTarget.Id) } : current); setDeleteTarget(undefined); setNotice('Notification target deleted.'); refresh(); }
    } catch (error) {
      if (mounted.current && !isAbortError(error)) { setMutationError(error); if (uncertainDestinationMutation(error)) setReview(true); }
    } finally { if (inFlight.current === controller) inFlight.current = undefined; if (mounted.current) setBusy(''); }
  }
  async function sendTest(target: NotificationDestination) {
    if (busy || review || inFlight.current) return;
    const controller = new AbortController(); inFlight.current = controller; setBusy('test'); setTestTarget(target.Id); setMutationError(null); setNotice('');
    try {
      await notificationDestinationsApi.test(target.Id, target.Revision, { signal: controller.signal });
      if (mounted.current) { setNotice('Test notification queued. Delivery status updates after the receiver responds.'); refresh(); }
    } catch (error) {
      if (mounted.current && !isAbortError(error)) { setMutationError(error); if (uncertainDestinationMutation(error)) setReview(true); }
    } finally { if (inFlight.current === controller) inFlight.current = undefined; if (mounted.current) { setBusy(''); setTestTarget(undefined); } }
  }
  const blocked = Boolean(busy) || review;
  const events = data?.SupportedEvents ?? [];
  const pendingCount = data?.Items.reduce((sum, item) => sum + item.PendingCount, 0) ?? 0;
  return <section className="nd-workspace" aria-label={t('Administrator notification targets')} aria-busy={Boolean(busy) || loading && !data}>
    {loadError != null && <><ErrorNotice error={loadError} retry={refresh} />{data && <Alert severity="warning">Showing the last successful notification target update.</Alert>}</>}
    {mutationError != null && !editor && !deleteTarget && <ErrorNotice error={mutationError} />}
    {review && !editor && !deleteTarget && <Alert severity="warning" action={<Button onClick={() => void readBack()} disabled={Boolean(busy)}>{busy === 'readback' ? 'Reloading saved targets...' : 'Reload saved targets'}</Button>}>The result could not be confirmed. Reload targets before trying again.</Alert>}
    {notice && !editor && <Alert severity="info">{notice}</Alert>}
    <div className="nd-panel">
      <div className="nd-panel-heading"><div><h2>{t('Notification targets')}</h2><span className="nd-count">{data ? formatCount(data.Items.length, 'notification target', 'notification targets') : '—'}</span></div><Button variant="contained" size="small" startIcon={<AddRounded />} disabled={!data || blocked || data.Items.length >= data.Limit} onClick={() => openEditor()}>Add notification target</Button></div>
      {loading && !data && <div className="nd-loading" role="status" aria-label={t('Loading notification targets')}>{[0, 1, 2].map((row) => <Skeleton key={row} height={72} />)}</div>}
      {data && data.Items.length === 0 && <div className="nd-empty"><DestinationGlyph mail /><h3>{t('No notification targets yet')}</h3><p>{t('Add an email address or HTTPS webhook to receive server events.')}</p><Button variant="outlined" onClick={() => openEditor()} disabled={blocked}>Add notification target</Button></div>}
      {data && data.Items.length > 0 && <div className="nd-target-list">{data.Items.map((target) => {
        const outcome = destinationOutcomeLabels[target.LastOutcome] ?? (target.LastOutcome ? 'Delivery status unknown' : 'No delivery recorded');
        const warning = target.DroppedCount > 0 || Boolean(target.LastOutcome && !['delivered', 'pending', 'sending', 'test_queued', 'configuration_changed', 'cancelled'].includes(target.LastOutcome));
        return <article className="nd-target" key={target.Id}>
          <div className="nd-target-identity"><span className="nd-target-icon"><DestinationGlyph mail={target.Transport === 'smtp'} /></span><div><h3 translate="no">{target.Name}</h3><div className="nd-target-address" translate="no">{target.Transport === 'smtp' ? target.SMTP.To.join(', ') : target.Endpoint}</div><span className="nd-target-channel">{t(target.Transport === 'smtp' ? 'Email (SMTP)' : 'HTTPS webhook')}</span></div></div>
          <div className="nd-target-state"><span className={`nd-badge${target.Enabled ? ' nd-badge-enabled' : ''}`}>{t(target.Enabled ? 'Enabled' : 'Disabled')}</span><span className={`nd-outcome${warning ? ' nd-outcome-warning' : ''}`}>{t(outcome)}</span>{target.LastDeliveredAt && <time dateTime={target.LastDeliveredAt}>{new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(target.LastDeliveredAt))}</time>}{target.PendingCount > 0 && <span>{t(target.PendingCount === 1 ? '{count} notification queued' : '{count} notifications queued', { count: target.PendingCount.toLocaleString(locale) })}</span>}{target.DroppedCount > 0 && <strong className="nd-outcome-warning">{t(target.DroppedCount === 1 ? '{count} event was not queued because the delivery queue was full.' : '{count} events were not queued because the delivery queue was full.', { count: target.DroppedCount.toLocaleString(locale) })}</strong>}</div>
          <div className="nd-target-actions"><Button size="small" variant="outlined" disabled={blocked || !target.Enabled} onClick={() => void sendTest(target)}>{busy === 'test' && testTarget === target.Id ? 'Queueing test...' : 'Send test notification'}</Button><Button size="small" onClick={() => openEditor(target)} disabled={blocked}>Edit target</Button><Button size="small" color="error" startIcon={<DeleteOutlineRounded />} aria-label={t('Delete notification target') + ' ' + target.Name} disabled={blocked} onClick={() => { setDeleteTarget(target); setMutationError(null); setNotice(''); }}>Delete</Button></div>
        </article>;
      })}</div>}
      {data && <p className="nd-panel-note">{t('Up to {count} targets. Changes apply to future events; earlier events are not replayed.', { count: data.Limit })}</p>}
    </div>
    {data && <div className="nd-panel"><div className="nd-panel-heading"><div><h2>{t('Event subscriptions')}</h2><span className="nd-count">{formatCount(pendingCount, 'notification queued', 'notifications queued')}</span></div><span className="nd-muted">{t('Edit a target to change its subscriptions.')}</span></div>{data.Items.length ? <div className="nd-matrix-scroll" tabIndex={0} role="region" aria-label={t('Notification subscription matrix')}><table className="nd-matrix"><thead><tr><th scope="col">{t('Event')}</th>{data.Items.map((target) => <th scope="col" key={target.Id}><span translate="no">{target.Name}</span><small>{t(target.Transport === 'smtp' ? 'Email (SMTP)' : 'HTTPS webhook')}</small></th>)}</tr></thead><tbody>{events.map((event) => <tr key={event.Id}><th scope="row">{t(destinationEventLabels[event.Id] ?? event.Name)}</th>{data.Items.map((target) => <td key={target.Id}><span className={target.EventIds.includes(event.Id) && target.Enabled ? 'nd-subscribed' : 'nd-not-subscribed'}>{t(target.EventIds.includes(event.Id) ? target.Enabled ? 'Subscribed' : 'Target disabled' : 'Not subscribed')}</span></td>)}</tr>)}</tbody></table></div> : <p className="nd-panel-note">{t('Subscriptions appear after you add a notification target.')}</p>}</div>}
    <Dialog open={Boolean(editor)} onClose={closeEditor} fullWidth maxWidth="sm" aria-labelledby="notification-destination-title">
      <Box component="form" className="nd-editor-form" onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)} noValidate aria-busy={Boolean(busy)}>
        <DialogTitle id="notification-destination-heading" aria-labelledby="notification-destination-name">
          <span id="notification-destination-title" className="nd-editor-eyebrow">{editor?.saved ? 'Edit notification target' : 'Add notification target'}</span>
          <strong id="notification-destination-name" className="nd-editor-target-name" translate="no">{editor?.draft.name.trim() || t(editor?.saved ? 'Untitled notification target' : 'New notification target')}</strong>
        </DialogTitle>
        <DialogContent>{editor && <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">Choose where server events are delivered. Saved passwords and signing secrets are never returned.</Typography>
          {mutationError != null && <ErrorNotice error={mutationError} />}
          {review && <Alert severity="warning">The result could not be confirmed. Reload targets before trying again.</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          <TextField autoFocus fullWidth label="Target name" value={editor.draft.name} disabled={blocked} onChange={(event) => change({ name: event.target.value })} error={Boolean(parsed?.errors.Name)} helperText={parsed?.errors.Name} slotProps={{ htmlInput: { autoComplete: 'off', maxLength: 120 } }} />
          <TextField fullWidth select label="Delivery channel" value={editor.draft.transport} disabled={blocked} onChange={(event) => change({ transport: event.target.value as NotificationDestinationDraft['transport'], secretAction: 'clear', secret: '' })}><MenuItem value="webhook">HTTPS webhook</MenuItem><MenuItem value="smtp">Email (SMTP)</MenuItem></TextField>
          <FormControlLabel control={<Switch checked={editor.draft.enabled} onChange={(event) => change({ enabled: event.target.checked })} disabled={blocked} />} label="Enable this target" />
          {editor.draft.transport === 'webhook' ? <TextField fullWidth label="Webhook endpoint" value={editor.draft.endpoint} disabled={blocked} onChange={(event) => change({ endpoint: event.target.value })} error={Boolean(parsed?.errors.Endpoint)} helperText={parsed?.errors.Endpoint ?? 'Use an HTTPS receiver URL without credentials, a query, or a fragment.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false, inputMode: 'url' } }} /> : <>
            <div className="nd-mail-server"><TextField fullWidth label="Mail server" value={editor.draft.smtpHost} disabled={blocked} onChange={(event) => change({ smtpHost: event.target.value })} error={Boolean(parsed?.errors.SMTPHost)} helperText={parsed?.errors.SMTPHost} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} /><TextField fullWidth label="Mail server port" value={editor.draft.smtpPort} disabled={blocked} onChange={(event) => change({ smtpPort: event.target.value })} error={Boolean(parsed?.errors.SMTPPort)} helperText={parsed?.errors.SMTPPort} slotProps={{ htmlInput: { inputMode: 'numeric' } }} /></div>
            <TextField fullWidth select label="Connection security" value={editor.draft.smtpSecurity} disabled={blocked} onChange={(event) => change({ smtpSecurity: event.target.value as 'starttls' | 'tls' })} helperText="The server verifies the mail server certificate. Unencrypted mail connections are not supported."><MenuItem value="starttls">STARTTLS</MenuItem><MenuItem value="tls">TLS</MenuItem></TextField>
            <TextField fullWidth label="Sender email address" value={editor.draft.smtpFrom} disabled={blocked} onChange={(event) => change({ smtpFrom: event.target.value })} error={Boolean(parsed?.errors.SMTPFrom)} helperText={parsed?.errors.SMTPFrom} slotProps={{ htmlInput: { autoComplete: 'off', inputMode: 'email' } }} />
            <TextField fullWidth multiline minRows={2} maxRows={5} label="Recipient email addresses" value={editor.draft.smtpTo} disabled={blocked} onChange={(event) => change({ smtpTo: event.target.value })} error={Boolean(parsed?.errors.SMTPTo)} helperText={parsed?.errors.SMTPTo ?? 'Use 1 to 16 distinct ASCII email addresses, one per line. International domains can use Punycode.'} slotProps={{ htmlInput: { autoComplete: 'off', inputMode: 'email' } }} />
            <TextField fullWidth label="Mail server username" value={editor.draft.smtpUsername} disabled={blocked} onChange={(event) => change({ smtpUsername: event.target.value })} error={Boolean(parsed?.errors.SMTPUsername)} helperText={parsed?.errors.SMTPUsername ?? 'Leave empty when the mail server does not require authentication.'} slotProps={{ htmlInput: { autoComplete: 'off' } }} />
          </>}
          <TextField fullWidth select label="Saved secret action" value={editor.draft.secretAction} disabled={blocked} onChange={(event) => change({ secretAction: event.target.value as NotificationDestinationDraft['secretAction'], secret: '' })} helperText={editor.saved?.HasSecret ? destinationSecretTargetChanged(editor.draft, editor.saved) ? 'Changing the target address or account requires replacing or clearing the saved secret.' : 'A secret is saved. Its value is never shown.' : 'No secret is saved.'}><MenuItem value="keep" disabled={destinationSecretTargetChanged(editor.draft, editor.saved)}>Keep saved secret</MenuItem><MenuItem value="replace">Replace secret</MenuItem><MenuItem value="clear">Clear secret</MenuItem></TextField>
          {editor.draft.secretAction === 'replace' && <PasswordField fullWidth label={editor.draft.transport === 'smtp' ? 'Mail server password' : 'Webhook signing secret'} value={editor.draft.secret} disabled={blocked} onChange={(event) => change({ secret: event.target.value })} error={Boolean(parsed?.errors.Secret)} helperText={parsed?.errors.Secret ?? (editor.draft.transport === 'webhook' ? 'Use 16 to 1,024 visible ASCII characters without spaces.' : 'Use a password of at most 1,024 UTF-8 bytes without control characters.')} autoComplete="new-password" />}
          {editor.draft.secretAction !== 'replace' && parsed?.errors.Secret && <Alert severity="error">{parsed.errors.Secret}</Alert>}
          <fieldset className="nd-subscriptions" disabled={blocked}><legend>{t('Event subscriptions')}</legend><p>{t('Select the server events this target receives.')}</p>{events.map((event) => <label key={event.Id}><input type="checkbox" checked={editor.draft.eventIds.includes(event.Id)} onChange={(changeEvent) => change({ eventIds: changeEvent.target.checked ? [...editor.draft.eventIds, event.Id] : editor.draft.eventIds.filter((value) => value !== event.Id) })} /><span>{t(destinationEventLabels[event.Id] ?? event.Name)}</span></label>)}{parsed?.errors.EventIds && <p className="nd-field-error" role="alert">{t(parsed.errors.EventIds)}</p>}</fieldset>
          <TextField fullWidth multiline minRows={2} maxRows={5} label="Allowed private networks" value={editor.draft.networks} disabled={blocked} onChange={(event) => change({ networks: event.target.value })} error={Boolean(parsed?.errors.AllowedNetworks)} helperText={parsed?.errors.AllowedNetworks ?? 'Optional canonical CIDRs for a self-hosted target, one per line. Leave empty to allow only public addresses.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
        </Stack>}</DialogContent>
        <DialogActions><Typography variant="caption" role="status" aria-live="polite">{dirty ? 'Unsaved target changes' : 'All settings saved'}</Typography>{review ? <Button onClick={() => void readBack()} disabled={Boolean(busy)}>{busy === 'readback' ? 'Reloading saved targets...' : 'Reload saved targets'}</Button> : <><Button onClick={closeEditor} disabled={Boolean(busy)}>Close</Button><Button type="submit" variant="contained" disabled={blocked || !dirty || !parsed?.input} startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy === 'save' ? 'Saving target...' : 'Save target'}</Button></>}</DialogActions>
      </Box>
    </Dialog>
    <Dialog open={Boolean(deleteTarget)} onClose={() => { if (!busy && !review) { setDeleteTarget(undefined); setMutationError(null); } }} data-layout="modal" fullWidth maxWidth="xs" aria-labelledby="notification-destination-delete-title">
      <DialogTitle id="notification-destination-delete-title">Delete notification target?</DialogTitle><DialogContent><Stack spacing={2}><Typography variant="body2">Future notifications will no longer be sent to this target. Queued notifications are cancelled.</Typography><Typography sx={{ fontWeight: 700 }} translate="no">{deleteTarget?.Name}</Typography>{mutationError != null && <ErrorNotice error={mutationError} />}{review && <Alert severity="warning">The result could not be confirmed. Reload targets before trying again.</Alert>}</Stack></DialogContent><DialogActions>{review ? <Button onClick={() => void readBack()} disabled={Boolean(busy)}>{busy === 'readback' ? 'Reloading saved targets...' : 'Reload saved targets'}</Button> : <><Button onClick={() => setDeleteTarget(undefined)} disabled={Boolean(busy)}>Cancel</Button><Button color="error" variant="contained" disabled={Boolean(busy)} onClick={() => void remove()}>{busy === 'delete' ? 'Deleting target...' : 'Delete target'}</Button></>}</DialogActions>
    </Dialog>
  </section>;
}

export function NotificationsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t } = useTranslation();
  const [reloadToken, setReloadToken] = useState(0);
  const [receiverOpen, setReceiverOpen] = useState(false);
  const guards = useRef<{ targets?: UserNavigationGuard; receiver?: UserNavigationGuard }>({});
  const publishGuard = useCallback((kind: 'targets' | 'receiver', guard: UserNavigationGuard | undefined) => {
    guards.current[kind] = guard;
    onNavigationGuardChange(guards.current.targets || guards.current.receiver ? () => {
      if (guards.current.targets && !guards.current.targets()) return false;
      return !guards.current.receiver || guards.current.receiver();
    } : undefined);
  }, [onNavigationGuardChange]);
  const targetGuardChange = useCallback((guard: UserNavigationGuard | undefined) => publishGuard('targets', guard), [publishGuard]);
  const receiverGuardChange = useCallback((guard: UserNavigationGuard | undefined) => publishGuard('receiver', guard), [publishGuard]);
  return <div className="notifications-page">
    <PageHeading title="Notifications" description="Choose where server events are delivered and manage event subscriptions." action={<Button startIcon={<RefreshRounded />} onClick={() => setReloadToken((value) => value + 1)}>Refresh targets</Button>} />
    <NotificationDestinationsPanel onNavigationGuardChange={targetGuardChange} reloadToken={reloadToken} />
    <details className="nd-receiver-details" open={receiverOpen} onToggle={(event) => {
      const next = event.currentTarget.open;
      if (!next && receiverOpen && guards.current.receiver && !guards.current.receiver()) { event.currentTarget.open = true; return; }
      setReceiverOpen(next);
    }}><summary>{t('Client delivery receiver')}<span>{t('Library and user data updates for registered players')}</span></summary>{receiverOpen && <ClientNotificationReceiver onNavigationGuardChange={receiverGuardChange} />}</details>
  </div>;
}
