import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, FormControlLabel, MenuItem, Paper, Skeleton, Stack, Switch, TextField, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { ApiError, isAbortError } from './api';
import { ErrorNotice, PageHeading } from './components';
import { PasswordField } from './formFields';
import { notificationDraft, notificationsApi, parseNotificationDraft } from './notificationsApi';
import type { NotificationDraft, NotificationSettings } from './notificationsApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

export function NotificationsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [saved, setSaved] = useState<NotificationSettings>(); const [draft, setDraft] = useState<NotificationDraft>();
  const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false); const [revision, setRevision] = useState(0); const [notice, setNotice] = useState('');
  const mounted = useRef(true); const inFlight = useRef(false);
  const dirty = Boolean(saved && draft && JSON.stringify(draft) !== JSON.stringify(notificationDraft(saved)));
  const parsed = useMemo(() => saved && draft ? parseNotificationDraft(draft, saved) : undefined, [saved, draft]);
  const disabled = loading || busy || review;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved notification settings and leave this page?');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(null); setSaved(undefined); setDraft(undefined);
    void notificationsApi.get({ signal: controller.signal }).then((value) => { if (!controller.signal.aborted) { setSaved(value); setDraft(notificationDraft(value)); setReview(false); setNotice(''); } })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  function change(patch: Partial<NotificationDraft>) { if (!disabled) { setDraft((value) => value ? { ...value, ...patch } : value); setError(null); setNotice(''); } }
  function reload() { if (!inFlight.current && (!dirty || window.confirm('Discard your draft and reload saved notification settings?'))) setRevision((value) => value + 1); }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (inFlight.current || disabled || !dirty || !parsed?.input) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      const value = await notificationsApi.update(parsed.input);
      if (mounted.current) { setSaved(value); setDraft(notificationDraft(value)); setNotice('Notification settings saved. Receiver credentials are never returned.'); }
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  }
  return <Box aria-busy={loading || busy}>
    <PageHeading title="Notifications" description="Configure the HTTPS receiver used for supported Goby notification events." action={<Button variant="outlined" onClick={reload} disabled={loading || busy} startIcon={<RefreshRounded />}>Reload notifications</Button>} />
    {error != null && <Box sx={{ mb: 2 }}><ErrorNotice error={error} retry={!saved ? reload : undefined} /></Box>}
    {review && <Alert severity="warning" sx={{ mb: 2 }}>Notification settings changed or the result could not be confirmed. Your draft is kept. Reload the saved configuration before trying again.</Alert>}
    {loading && <Skeleton variant="rounded" height={300} aria-label="Loading notification settings" />}
    {saved && draft && <Stack component="form" spacing={3} noValidate onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)}>
      <Paper component="section" aria-label="Notification receiver status" variant="outlined" sx={{ p: 3 }}><Stack spacing={1.5}>
        <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h2" variant="h3">Receiver status</Typography><Chip size="small" variant="outlined" label={saved.Enabled ? 'Enabled' : 'Disabled'} /></Stack>
        <Typography variant="body2">{saved.PendingCount.toLocaleString()} pending notifications · {saved.HasReceiverCredential ? 'Receiver credential configured' : 'No receiver credential configured'}</Typography>
        <Typography variant="caption" color="text.secondary">Saved revision {saved.Revision}. Reload to refresh the delivery queue count.</Typography>
        <Typography variant="body2">Supported events: {saved.SupportedEvents.join(', ') || 'None reported'}</Typography>
      </Stack></Paper>
      <Paper component="section" aria-label="Notification receiver settings" variant="outlined" sx={{ p: 3 }}><Stack spacing={2.5}>
        <Typography component="h2" variant="h3">HTTPS receiver</Typography>
        <Typography variant="body2" color="text.secondary">Use a receiver that implements the Goby notification contract. Configure trusted TLS certificates in the server deployment.</Typography>
        <FormControlLabel control={<Switch checked={draft.enabled} disabled={disabled} onChange={(event) => change({ enabled: event.target.checked })} />} label="Enable notifications" />
        <TextField fullWidth label="Receiver endpoint" value={draft.endpoint} disabled={disabled} onChange={(event) => change({ endpoint: event.target.value })} error={Boolean(parsed?.errors.Endpoint)} helperText={parsed?.errors.Endpoint ?? 'An HTTPS URL without credentials, query parameters, or a fragment. Required when enabled.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
        <TextField fullWidth multiline minRows={3} maxRows={8} label="Allowed private networks" value={draft.networks} disabled={disabled} onChange={(event) => change({ networks: event.target.value })} error={Boolean(parsed?.errors.AllowedNetworks)} helperText={parsed?.errors.AllowedNetworks ?? 'Optional canonical CIDRs for a self-hosted receiver, one per line (maximum 32). Leave empty to allow only public receiver addresses.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
        <TextField fullWidth select label="Receiver credential action" value={draft.credentialMode} disabled={disabled} onChange={(event) => change({ credentialMode: event.target.value as NotificationDraft['credentialMode'], credential: '' })}><MenuItem value="keep">Keep current credential</MenuItem><MenuItem value="replace">Replace credential</MenuItem><MenuItem value="clear">Clear credential</MenuItem></TextField>
        {draft.credentialMode === 'replace' && <PasswordField fullWidth label="New receiver credential" value={draft.credential} disabled={disabled} onChange={(event) => change({ credential: event.target.value })} error={Boolean(parsed?.errors.ReceiverCredential)} helperText={parsed?.errors.ReceiverCredential ?? '16–2,048 visible ASCII characters without spaces. The saved value is never filled back into this form.'} autoComplete="new-password" />}
        {draft.credentialMode !== 'replace' && parsed?.errors.ReceiverCredential && <Alert severity="error">{parsed.errors.ReceiverCredential}</Alert>}
        {draft.credentialMode === 'clear' && !draft.enabled && <Typography variant="body2" color="text.secondary">Saving will remove the stored receiver credential.</Typography>}
      </Stack></Paper>
      {notice && <Alert severity="success">{notice}</Alert>}
      <Paper variant="outlined" sx={{ p: 2.5 }}><Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 2 }}><Typography variant="body2">{dirty ? 'Unsaved notification settings' : 'All notification settings saved'}</Typography><Button type="submit" variant="contained" disabled={disabled || !dirty || !parsed?.input} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving notifications...' : 'Save notifications'}</Button></Stack></Paper>
    </Stack>}
  </Box>;
}
