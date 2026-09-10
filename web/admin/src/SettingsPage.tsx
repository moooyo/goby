import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Paper, Skeleton, Stack, Switch, TextField, Typography } from '@mui/material';
import LockOutlined from '@mui/icons-material/LockOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { ServerSettings, SettingsField } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { draftFromSettings, formatMbps, formatSettingValue, parseSettingsDraft, settingLabels, settingsDraftKey, settingsFields } from './settingsDraft';
import type { SettingDraft, SettingsDraft } from './settingsDraft';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const fieldHelp: Record<SettingsField, string> = {
  ServerName: 'Use a nonempty name of at most 128 UTF-8 bytes.',
  MaxBitrate: '0.000001–1000 Mbps. Up to six decimal places are saved exactly as whole bits per second.',
  MaxWidth: '1–8192 pixels. This is the maximum output width.',
  MaxHeight: '1–8192 pixels. This is the maximum output height.',
  MaxAudioChannels: '1–8 channels. This is the maximum output channel count.',
};

function requiresReload(error: unknown): boolean {
  if (error == null) return false;
  return !(error instanceof ApiError) || error.status === 409 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
}

function sourceName(value: 'deployment' | 'database'): string {
  return value === 'deployment' ? 'Deployment default' : 'Database override';
}

function MutationNotice({ error, reload }: { error: unknown; reload: () => void }) {
  if (error == null) return null;
  return <Stack spacing={1.5}>
    <ErrorNotice error={error} />
    {requiresReload(error) && <Alert severity="warning">
      {error instanceof ApiError && error.status === 409
        ? 'These settings changed after you loaded them. Your draft is still here. Reload the latest settings before making another change.'
        : 'The result could not be confirmed. Your change may already be saved. Shown values are from the last confirmed response. Reload the latest settings before making another change.'}
      <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={reload} sx={{ display: 'flex', mt: 1, ml: -1 }}>Reload latest settings</Button>
    </Alert>}
  </Stack>;
}

function SettingField({ field, settings, draft, disabled, stale, error, onChange }: { field: SettingsField; settings: ServerSettings; draft: SettingDraft; disabled: boolean; stale: boolean; error?: string; onChange: (value: SettingDraft) => void }) {
  const label = settingLabels[field];
  const id = `settings-${field}`;
  const defaultInput = field === 'MaxBitrate' ? formatMbps(settings.Defaults.MaxBitrate) : String(settings.Defaults[field]);
  return <Box component="section" aria-labelledby={`${id}-heading`} sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', md: 'minmax(0, 1fr) minmax(250px, 1fr)' }, gap: { xs: 2, md: 3 }, p: { xs: 2.5, sm: 3 } }}>
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="h4" component="h3" id={`${id}-heading`}>{label}</Typography>
      <Typography variant="caption" color="text.secondary" component="p" sx={{ mt: 1.5, mb: 0.4 }}>{stale ? 'Last confirmed effective value' : 'Current effective value'}</Typography>
      <Typography variant="body1" sx={{ fontWeight: 600, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>{formatSettingValue(field, settings.Effective[field])}</Typography>
      <Chip size="small" variant="outlined" color={settings.Sources[field] === 'database' ? 'primary' : 'default'} label={sourceName(settings.Sources[field])} sx={{ mt: 1 }} />
    </Box>
    <Stack spacing={1.5} sx={{ minWidth: 0 }}>
      <FormControlLabel sx={{ m: 0, alignItems: 'center', gap: 1 }} label={<Typography variant="body2">Use deployment default</Typography>} control={<Switch
        checked={!draft.override}
        disabled={disabled}
        onChange={(event) => onChange({ ...draft, override: !event.target.checked })}
        slotProps={{ input: { 'aria-label': `Use deployment default for ${label.toLowerCase()}`, 'aria-describedby': `${id}-default` } }}
      />} />
      <TextField
        id={`${id}-input`}
        name={field}
        fullWidth
        label={field === 'MaxBitrate' ? `${label} (Mbps)` : label}
        value={draft.override ? draft.value : defaultInput}
        disabled={disabled || !draft.override}
        onChange={(event) => onChange({ ...draft, value: event.target.value })}
        error={draft.override && Boolean(error)}
        helperText={draft.override && error ? error : fieldHelp[field]}
        slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false, inputMode: field === 'ServerName' ? 'text' : field === 'MaxBitrate' ? 'decimal' : 'numeric', maxLength: field === 'ServerName' ? 128 : 64 } }}
      />
      <Typography id={`${id}-default`} variant="caption" color="text.secondary" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>Deployment default: {formatSettingValue(field, settings.Defaults[field])}</Typography>
      <Typography variant="caption" color="text.secondary">{draft.override ? 'Save to use this database override.' : 'Save to use the deployment default. Defaults can change when the server is redeployed.'}</Typography>
    </Stack>
  </Box>;
}

function DeploymentSettings({ settings }: { settings: ServerSettings }) {
  const deployment = settings.Deployment;
  const entries = [
    ['Transcoding', deployment.TranscodingEnabled ? 'Enabled' : 'Disabled'],
    ['Hardware decoder', deployment.HardwareDecoder || 'Not configured'],
    ['Hardware encoder', deployment.HardwareEncoder || 'Not configured'],
    ['Threads per job', deployment.Threads.toLocaleString()],
    ['Maximum concurrent jobs', deployment.MaxJobs.toLocaleString()],
    ['Maximum jobs per user', deployment.MaxUserJobs.toLocaleString()],
    ['Maximum jobs per session', deployment.MaxSessionJobs.toLocaleString()],
  ];
  return <Paper component="section" aria-labelledby="deployment-settings-heading" variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
    <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.2 }}><LockOutlined sx={{ fontSize: 20, color: 'text.secondary' }} /><Typography id="deployment-settings-heading" variant="h3" component="h2">Deployment configuration</Typography><Chip label="Read only" size="small" variant="outlined" /></Stack>
    <Typography variant="body2" color="text.secondary" sx={{ mt: 1, maxWidth: 760 }}>These hardware and resource settings are loaded at server startup. Change the deployment configuration and restart the service to update them.</Typography>
    <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(3, minmax(0, 1fr))' }, gap: 2.5, m: 0, mt: 3 }}>
      {entries.map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.4, fontWeight: 600, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>{value}</Typography></Box>)}
    </Box>
  </Paper>;
}

export function SettingsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [settings, setSettings] = useState<ServerSettings>();
  const [draft, setDraft] = useState<SettingsDraft>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [mutationError, setMutationError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState<'save' | 'reset'>();
  const [resetFields, setResetFields] = useState<SettingsField[]>();
  const [pendingAction, setPendingAction] = useState<'reload' | 'discard'>();
  const [notice, setNotice] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  const parsed = draft ? parseSettingsDraft(draft) : undefined;
  const dirty = Boolean(settings && draft && settingsDraftKey(draft) !== settingsDraftKey(draftFromSettings(settings)));
  const blocked = requiresReload(mutationError);
  const disabled = loading || Boolean(busy) || blocked;
  const savedOverrides = settings ? settingsFields.filter((field) => settings.Overrides[field] !== null) : [];
  useUserDraftNavigation(dirty, Boolean(busy), onNavigationGuardChange, 'Discard unsaved settings changes and leave this page?');

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError(undefined);
    setSettings(undefined);
    setDraft(undefined);
    void adminApi.getSettings({ signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) {
          setSettings(value); setDraft(draftFromSettings(value)); setMutationError(undefined); setNotice('');
        }
      })
      .catch((error: unknown) => { if (!controller.signal.aborted && !isAbortError(error)) setLoadError(error); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => () => mutation.current?.abort(), []);

  function reload() {
    setPendingAction(undefined);
    setResetFields(undefined);
    setMutationError(undefined);
    setNotice('');
    setRevision((value) => value + 1);
  }

  function requestReload() {
    if (mutation.current) return;
    if (dirty) setPendingAction('reload');
    else reload();
  }

  function discardDraft() {
    if (!settings || mutation.current) return;
    setDraft(draftFromSettings(settings));
    setPendingAction(undefined);
    setNotice('Unsaved changes discarded.');
  }

  function change(field: SettingsField, value: SettingDraft) {
    if (disabled) return;
    setDraft((current) => current ? { ...current, [field]: value } : current);
    setMutationError(undefined);
    setNotice('');
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings || !draft || !parsed?.overrides || !dirty || mutation.current || disabled) return;
    const controller = new AbortController();
    mutation.current = controller;
    setBusy('save');
    setMutationError(undefined);
    setNotice('');
    try {
      const value = await adminApi.updateSettings({ Revision: settings.Revision, Overrides: parsed.overrides }, { signal: controller.signal });
      if (!controller.signal.aborted) { setSettings(value); setDraft(draftFromSettings(value)); setNotice('Settings saved.'); }
    } catch (error) {
      if (!controller.signal.aborted && !isAbortError(error)) setMutationError(error);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  function openReset() {
    if (disabled || !settings || savedOverrides.length === 0) return;
    setResetFields([...savedOverrides]);
    setMutationError(undefined);
    setNotice('');
  }

  async function resetSelected() {
    if (!settings || !resetFields?.length || mutation.current || disabled) return;
    const fields = settingsFields.filter((field) => resetFields.includes(field));
    const controller = new AbortController();
    mutation.current = controller;
    setBusy('reset');
    setMutationError(undefined);
    setNotice('');
    try {
      const value = await adminApi.resetSettings({ Revision: settings.Revision, Fields: fields }, { signal: controller.signal });
      if (!controller.signal.aborted) {
        const fresh = draftFromSettings(value);
        setSettings(value);
        setDraft((current) => {
          if (!current) return fresh;
          const next = { ...current };
          for (const field of fields) next[field] = fresh[field];
          return next;
        });
        setResetFields(undefined);
        setNotice('Selected settings now use deployment defaults. Other unsaved edits remain in the form.');
      }
    } catch (error) {
      if (!controller.signal.aborted && !isAbortError(error)) setMutationError(error);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  function renderField(field: SettingsField) {
    if (!settings || !draft) return null;
    return <SettingField key={field} field={field} settings={settings} draft={draft[field]} disabled={disabled} stale={blocked}
      error={parsed?.errors[field] ?? fieldError(mutationError, field) ?? fieldError(mutationError, `Overrides.${field}`)} onChange={(value) => change(field, value)} />;
  }

  return <Box aria-busy={loading || Boolean(busy)}>
    <PageHeading title="Settings" description="Manage server identity and output limits. Database overrides take precedence over deployment defaults." action={<Button variant="outlined" startIcon={<RefreshRounded />} onClick={requestReload} disabled={loading || Boolean(busy)}>Reload settings</Button>} />
    {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
    {loading && <Stack spacing={2.5} role="status" aria-label="Loading settings"><Skeleton variant="rounded" height={250} /><Skeleton variant="rounded" height={420} /></Stack>}
    {settings && draft && <Stack spacing={3}>
      <Typography variant="caption" color="text.secondary">{blocked ? 'Last confirmed save' : 'Last saved'}: <time dateTime={settings.UpdatedAt}>{new Date(settings.UpdatedAt).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</time></Typography>
      <Box component="form" onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)}>
        <Stack spacing={3}>
          <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
            <Box sx={{ p: { xs: 2.5, sm: 3 }, pb: { xs: 0, sm: 0 } }}><Typography variant="h3" component="h2">Server identity</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>A saved server name is used by new requests immediately.</Typography></Box>
            {renderField('ServerName')}
          </Paper>
          <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
            <Box sx={{ p: { xs: 2.5, sm: 3 } }}><Typography variant="h3" component="h2">Output planning limits</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>Changes apply to new output negotiations and conversion plans. Existing plans keep their current limits.</Typography>
              {!settings.Deployment.TranscodingEnabled && <Alert severity="info" sx={{ mt: 2 }}>Transcoding is currently disabled in deployment configuration. You can still save output limits for future use. Enabling transcoding requires a deployment configuration change and service restart.</Alert>}
            </Box>
            <Box sx={{ '& > section': { borderTop: 1, borderColor: 'divider' } }}>{settingsFields.filter((field) => field !== 'ServerName').map(renderField)}</Box>
          </Paper>
          {resetFields === undefined && mutationError != null && <MutationNotice error={mutationError} reload={requestReload} />}
          {notice && <Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert>}
          <Paper variant="outlined" sx={{ p: 2.5 }}>
            <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'stretch', sm: 'center' }, gap: 2 }}>
              <Box><Typography variant="body2" sx={{ fontWeight: 650 }}>{dirty ? 'Unsaved changes' : 'No unsaved changes'}</Typography><Typography variant="caption" color="text.secondary">{blocked ? 'Reload the latest settings to continue.' : 'Save applies the complete set of override choices.'}</Typography></Box>
              <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button type="submit" variant="contained" startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />} disabled={disabled || !dirty || !parsed?.overrides}>{busy === 'save' ? 'Saving settings...' : 'Save settings'}</Button><Button color="secondary" onClick={() => setPendingAction('discard')} disabled={loading || Boolean(busy) || !dirty}>Discard changes</Button></Stack>
            </Stack>
            <Box sx={{ mt: 2, pt: 2, borderTop: 1, borderColor: 'divider' }}><Button color="secondary" startIcon={<RestartAltRounded />} onClick={openReset} disabled={disabled || savedOverrides.length === 0}>Reset saved overrides</Button><Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0, mt: 0.5 }}>Choose which saved settings should use deployment defaults again.</Typography></Box>
          </Paper>
        </Stack>
      </Box>
      <DeploymentSettings settings={settings} />
    </Stack>}
    {settings && resetFields !== undefined && <Dialog open onClose={busy ? undefined : () => setResetFields(undefined)} fullWidth maxWidth="sm" aria-labelledby="reset-settings-title" aria-describedby="reset-settings-description">
      <DialogTitle id="reset-settings-title" sx={{ pt: 3 }}>Reset saved overrides</DialogTitle>
      <DialogContent aria-busy={busy === 'reset'}><Stack spacing={2}>
        <Typography id="reset-settings-description" variant="body2" color="text.secondary">This immediately removes the selected database overrides. Those settings will use deployment defaults.</Typography>
        {dirty && <Alert severity="warning">Unsaved edits to selected settings will be discarded. Other draft changes stay in the form.</Alert>}
        <MutationNotice error={mutationError} reload={requestReload} />
        <FormControlLabel control={<Checkbox checked={savedOverrides.length > 0 && resetFields.length === savedOverrides.length} indeterminate={resetFields.length > 0 && resetFields.length < savedOverrides.length} disabled={Boolean(busy) || blocked} onChange={(event) => setResetFields(event.target.checked ? [...savedOverrides] : [])} />} label="Select all saved overrides" />
        <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 2, overflow: 'hidden' }}>{settingsFields.map((field, index) => <Box key={field} sx={{ p: 2, borderTop: index > 0 ? 1 : 0, borderColor: 'divider' }}>
          <FormControlLabel sx={{ m: 0, gap: 1, alignItems: 'flex-start', width: '100%', minWidth: 0, '& .MuiFormControlLabel-label': { minWidth: 0, flex: 1 } }} control={<Checkbox checked={resetFields.includes(field)} disabled={Boolean(busy) || blocked || settings.Overrides[field] === null} onChange={(event) => setResetFields((current) => settingsFields.filter((candidate) => candidate === field ? event.target.checked : current?.includes(candidate)))} />} label={<Box sx={{ pt: 0.75 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{settingLabels[field]}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{settings.Overrides[field] === null ? 'Already using deployment default' : `Current: ${formatSettingValue(field, settings.Effective[field])}`}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>Deployment default: {formatSettingValue(field, settings.Defaults[field])}</Typography></Box>} />
        </Box>)}</Box>
      </Stack></DialogContent>
      <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" disabled={Boolean(busy)} onClick={() => setResetFields(undefined)}>Cancel</Button><Button color="error" variant="contained" startIcon={busy === 'reset' ? <CircularProgress size={16} color="inherit" /> : <RestartAltRounded />} disabled={Boolean(busy) || blocked || resetFields.length === 0} onClick={() => void resetSelected()}>{busy === 'reset' ? 'Resetting settings...' : 'Reset selected settings'}</Button></DialogActions>
    </Dialog>}
    {pendingAction && <Dialog open onClose={() => setPendingAction(undefined)} fullWidth maxWidth="xs" aria-labelledby="discard-settings-title">
      <DialogTitle id="discard-settings-title">Discard unsaved settings changes?</DialogTitle>
      <DialogContent><Typography variant="body2" color="text.secondary">{pendingAction === 'reload' ? 'Reloading replaces your draft with the latest saved settings. All unsaved edits will be lost.' : 'Your unsaved edits will be replaced with the last loaded settings.'}</Typography></DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" autoFocus onClick={() => setPendingAction(undefined)}>Keep editing</Button><Button color="error" variant="contained" onClick={pendingAction === 'reload' ? reload : discardDraft}>{pendingAction === 'reload' ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions>
    </Dialog>}
  </Box>;
}
