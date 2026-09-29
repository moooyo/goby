import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, FormControlLabel, MenuItem, Paper, Skeleton, Stack, Switch, TextField, Tooltip, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import CheckCircleRounded from '@mui/icons-material/CheckCircleRounded';
import CloudDoneRounded from '@mui/icons-material/CloudDoneRounded';
import EditOutlined from '@mui/icons-material/EditOutlined';
import KeyRounded from '@mui/icons-material/KeyRounded';
import { ApiError, isAbortError } from './api';
import { ErrorNotice, PageHeading } from './components';
import { PasswordField } from './formFields';
import { notificationDraft, notificationsApi, parseNotificationDraft } from './notificationsApi';
import type { NotificationDraft, NotificationSettings } from './notificationsApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const notificationEventLabels: Record<string, string> = { CatalogInvalidated: 'Catalog changes', UserDataInvalidated: 'User data changes' };

export function NotificationsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [saved, setSaved] = useState<NotificationSettings>(); const [draft, setDraft] = useState<NotificationDraft>();
  const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false); const [revision, setRevision] = useState(0); const [notice, setNotice] = useState('');
  const mounted = useRef(true); const inFlight = useRef(false);
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
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(null); setSaved(undefined); setDraft(undefined);
    void notificationsApi.get({ signal: controller.signal }).then((value) => { if (!controller.signal.aborted) { setSaved(value); setDraft(notificationDraft(value)); setReview(false); setNotice(''); } })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  function change(patch: Partial<NotificationDraft>) { if (!disabled) { setDraft((value) => value ? { ...value, ...patch } : value); setError(null); setNotice(''); } }
  function discard() { if (!disabled && saved) { setDraft(notificationDraft(saved)); setError(null); setNotice(''); } }
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
  return <Box aria-busy={loading || busy} sx={{ minHeight: '100%', display: 'flex', flexDirection: 'column' }}>
    <PageHeading title="Notifications" description="Configure the HTTPS receiver used for supported Goby notification events." action={<Button onClick={reload} disabled={loading || busy} startIcon={<RefreshRounded />}>Reload notifications</Button>} />
    {error != null && <Box sx={{ mb: 2 }}><ErrorNotice error={error} retry={!saved ? reload : undefined} /></Box>}
    {review && <Alert severity="warning" sx={{ mb: 2 }}>Notification settings changed or the result could not be confirmed. Your draft is kept. Reload the saved configuration before trying again.</Alert>}
    {loading && <Skeleton variant="rounded" height={300} aria-label="Loading notification settings" />}
    {saved && draft && <Box component="form" noValidate onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)} sx={{ display: 'flex', flexDirection: 'column', flex: 1, gap: 2.5 }}>
      <Paper component="section" aria-label="Notification receiver status" elevation={0} sx={{ p: { xs: 2.5, md: 3 }, bgcolor: '#F3F6FB', borderRadius: '20px', display: 'flex', flexWrap: 'wrap', gap: '20px 48px' }}>
        <Box sx={{ flex: '1 1 240px', maxWidth: { md: 340 } }}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap', mb: 1 }}>
            <Typography component="h2" sx={{ fontSize: 16, lineHeight: '24px', fontWeight: 600 }}>Receiver status</Typography>
            <Chip size="small" label={saved.Enabled ? 'Enabled' : 'Disabled'} sx={{ bgcolor: saved.Enabled ? '#CDEBD6' : '#E4E8F0', color: saved.Enabled ? '#0D4F2B' : '#434753', borderRadius: '6px', height: 24, fontSize: 12, fontWeight: 600 }} />
          </Stack>
          <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>Saved revision {saved.Revision}. Reload to refresh the delivery queue count.</Typography>
        </Box>
        <Stack spacing={2} sx={{ flex: '999 1 420px', minWidth: 0 }}>
          <Stack direction="row" sx={{ gap: { xs: 3, sm: 5 }, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <Box>
              <Typography sx={{ fontSize: 24, lineHeight: '28px', fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>{saved.PendingCount.toLocaleString()}</Typography>
              <Typography variant="caption" color="text.secondary">Pending notifications</Typography>
            </Box>
            <Box>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minHeight: 28 }}>
                {saved.HasReceiverCredential ? <CheckCircleRounded sx={{ fontSize: 18, color: '#2E7D4F' }} /> : <KeyRounded sx={{ fontSize: 18, color: 'text.secondary' }} />}
                <Typography variant="body2" sx={{ fontWeight: 600 }}>{saved.HasReceiverCredential ? 'Configured' : 'Not configured'}</Typography>
              </Stack>
              <Typography variant="caption" color="text.secondary">Receiver credential</Typography>
            </Box>
          </Stack>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Typography variant="caption" color="text.secondary">Supported events</Typography>
            {saved.SupportedEvents.length ? saved.SupportedEvents.map((event) => <Tooltip key={event} title={event}><Chip size="small" label={notificationEventLabels[event] ?? event} sx={{ bgcolor: '#FFFFFF', color: '#434753', borderRadius: '6px', height: 'auto', minHeight: 24, fontSize: 12, '& .MuiChip-label': { whiteSpace: 'normal', overflowWrap: 'anywhere', py: 0.5 } }} /></Tooltip>) : <Typography variant="caption" color="text.secondary">None reported</Typography>}
          </Stack>
        </Stack>
      </Paper>
      <Box component="section" aria-label="Notification receiver settings" sx={{ borderTop: '1px solid #E3E8F1', py: 3.5, display: 'flex', flexWrap: 'wrap', gap: '20px 48px' }}>
        <Box sx={{ flex: '1 1 240px', maxWidth: { md: 340 } }}>
          <Typography component="h2" sx={{ fontSize: 16, lineHeight: '24px', fontWeight: 600, mb: 1 }}>HTTPS receiver</Typography>
          <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13, lineHeight: '20px' }}>Use a receiver that implements the Goby notification contract. Configure trusted TLS certificates in the server deployment. Saved credentials are never returned or filled back into this form.</Typography>
        </Box>
        <Stack spacing={2.5} sx={{ flex: '999 1 420px', minWidth: 0 }}>
          <FormControlLabel sx={{ m: 0, gap: 1 }} control={<Switch checked={draft.enabled} disabled={disabled} onChange={(event) => change({ enabled: event.target.checked })} />} label="Enable notifications" />
          <TextField fullWidth label="Receiver endpoint" value={draft.endpoint} disabled={disabled} onChange={(event) => change({ endpoint: event.target.value })} error={Boolean(parsed?.errors.Endpoint)} helperText={parsed?.errors.Endpoint ?? 'An HTTPS URL without credentials, query parameters, or a fragment. Required when enabled.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} sx={{ '& .MuiInputBase-input': { fontFamily: '"JetBrains Mono", monospace', fontSize: 13 } }} />
          <TextField fullWidth multiline minRows={2} maxRows={8} label="Allowed private networks" value={draft.networks} disabled={disabled} onChange={(event) => change({ networks: event.target.value })} error={Boolean(parsed?.errors.AllowedNetworks)} helperText={parsed?.errors.AllowedNetworks ?? 'Optional canonical CIDRs for a self-hosted receiver, one per line (maximum 32). Leave empty to allow only public receiver addresses.'} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} sx={{ '& .MuiInputBase-input': { fontFamily: '"JetBrains Mono", monospace', fontSize: 13 } }} />
          <TextField fullWidth select label="Receiver credential action" value={draft.credentialMode} disabled={disabled} onChange={(event) => change({ credentialMode: event.target.value as NotificationDraft['credentialMode'], credential: '' })} helperText="Keep the saved credential, replace it, or clear it after disabling notifications."><MenuItem value="keep">Keep current credential</MenuItem><MenuItem value="replace">Replace credential</MenuItem><MenuItem value="clear">Clear credential</MenuItem></TextField>
          {draft.credentialMode === 'replace' && <PasswordField fullWidth label="New receiver credential" value={draft.credential} disabled={disabled} onChange={(event) => change({ credential: event.target.value })} error={Boolean(parsed?.errors.ReceiverCredential)} helperText={parsed?.errors.ReceiverCredential ?? '16–2,048 visible ASCII characters without spaces. The saved value is never filled back into this form.'} autoComplete="new-password" />}
          {draft.credentialMode !== 'replace' && parsed?.errors.ReceiverCredential && <Alert severity="error">{parsed.errors.ReceiverCredential}</Alert>}
          {draft.credentialMode === 'clear' && !draft.enabled && <Typography variant="body2" color="text.secondary">Saving will remove the stored receiver credential.</Typography>}
        </Stack>
      </Box>
      {notice && <Alert severity="success">{notice}</Alert>}
      <Paper elevation={0} sx={{ position: 'sticky', bottom: 16, zIndex: 2, mt: 'auto', p: { xs: '10px 12px', sm: '12px 16px' }, borderRadius: '16px', bgcolor: dirty ? '#2B2F37' : '#EEF2F9', color: dirty ? '#EEF1F8' : '#434753', outline: '16px solid #FFFFFF', boxShadow: dirty ? '0 4px 12px rgba(0,0,0,.22)' : 'none' }}>
        <Stack direction={{ xs: dirty ? 'column' : 'row', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: dirty ? 'stretch' : 'center', sm: 'center' }, gap: { xs: 1, sm: 2 } }}>
          <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }} role="status" aria-live="polite">
            {dirty ? <EditOutlined sx={{ fontSize: 20, color: '#AFC6FF' }} /> : <CloudDoneRounded sx={{ fontSize: 20, color: '#717785' }} />}
            <Typography variant="body2">{dirty ? `${changedCount} unsaved ${changedCount === 1 ? 'change' : 'changes'}` : <><Box component="span" sx={{ display: { xs: 'inline', sm: 'none' } }}>All settings saved</Box><Box component="span" sx={{ display: { xs: 'none', sm: 'inline' } }}>All notification settings saved · Revision {saved.Revision}</Box></>}</Typography>
          </Stack>
          <Stack direction="row" spacing={1} sx={{ justifyContent: 'flex-end', flexWrap: 'wrap' }}>
            {dirty && <Button type="button" disabled={disabled} onClick={discard} sx={{ color: '#AFC6FF' }}>Discard changes</Button>}
            <Button type="submit" aria-label={busy ? 'Saving notifications...' : 'Save notifications'} variant="contained" disabled={disabled || !dirty || !parsed?.input} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />} sx={{ px: { xs: 1.5, sm: 2.5 }, '& .MuiButton-startIcon': { display: { xs: 'none', sm: 'inherit' } }, ...(dirty ? { bgcolor: '#AFC6FF', color: '#0A2A63', '&:hover': { bgcolor: '#C5D5FF' }, '&.Mui-disabled': { bgcolor: '#555D6D', color: '#C3CAD6' } } : {}) }}><Box component="span" sx={{ display: { xs: 'inline', sm: 'none' } }}>{busy ? 'Saving...' : 'Save'}</Box><Box component="span" sx={{ display: { xs: 'none', sm: 'inline' } }}>{busy ? 'Saving notifications...' : 'Save notifications'}</Box></Button>
          </Stack>
        </Stack>
      </Paper>
    </Box>}
  </Box>;
}
