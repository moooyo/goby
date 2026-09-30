import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, InputAdornment, Paper, Skeleton, Snackbar, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import LockOutlined from '@mui/icons-material/LockOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';
import CloudDoneRounded from '@mui/icons-material/CloudDoneRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import EditNoteRounded from '@mui/icons-material/EditNoteRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { ServerSettings, SettingsResetField } from './api';
import { ErrorNotice } from './components';
import { SettingsGroup } from './SettingsLayout';
import { MediaDiagnosticsPanel } from './MediaDiagnosticsPanel';
import { ManagementSettingsFields } from './ManagementSettingsFields';
import { RuntimeSettingsFields } from './RuntimeSettingsFields';
import { SortingSettingsFields } from './SortingSettingsFields';
import { fieldError } from './formFields';
import { draftFromSettings, formatMbps, formatSettingValue, outputSettingsFields, parseSettingsDraft, settingLabels, settingsDraftKey, settingsResetFields } from './settingsDraft';
import type { OutputSettingField, ServerNameDraft, SettingDraft, SettingsDraft } from './settingsDraft';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const fieldHelp: Record<OutputSettingField, string> = {
  MaxBitrate: '0.000001–1000 Mbps. Up to six decimal places are saved exactly as whole bits per second.',
  MaxWidth: '1–8192 pixels. This is the maximum output width.',
  MaxHeight: '1–8192 pixels. This is the maximum output height.',
  MaxAudioChannels: '1–8 channels. This is the maximum output channel count.',
};


function hasSavedOverride(settings: ServerSettings, field: SettingsResetField): boolean {
  if (field === 'ServerName') return settings.ServerNameMode !== 'deployment';
  if (field === 'TranscodingMaxWidth') return settings.Encoding.TranscodingMaxWidth !== 0;
  return settings.Overrides[field] !== null;
}

function resetCurrentValue(settings: ServerSettings, field: SettingsResetField): string {
  if (field === 'TranscodingMaxWidth') return settings.Encoding.TranscodingMaxWidth === 0 ? 'No additional limit' : `${settings.Encoding.TranscodingMaxWidth} px`;
  return formatSettingValue(field, settings.Effective[field]);
}

function resetDefaultValue(settings: ServerSettings, field: SettingsResetField): string {
  return field === 'TranscodingMaxWidth'
    ? 'Reset value: 0 (no additional limit). The server width limit still applies.'
    : `Deployment default: ${formatSettingValue(field, settings.Defaults[field])}`;
}

function requiresReload(error: unknown): boolean {
  if (error == null) return false;
  return !(error instanceof ApiError) || error.status === 409 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
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

function SettingField({ field, settings, draft, disabled, error, onChange }: { field: OutputSettingField; settings: ServerSettings; draft: SettingDraft; disabled: boolean; error?: string; onChange: (value: SettingDraft) => void }) {
  const label = settingLabels[field];
  const defaultInput = field === 'MaxBitrate' ? formatMbps(settings.Defaults.MaxBitrate) : String(settings.Defaults[field]);
  const unit = field === 'MaxBitrate' ? 'Mbps' : field === 'MaxAudioChannels' ? 'channels' : 'px';
  return <Box>
    <TextField id={'settings-' + field + '-input'} name={field} fullWidth label={label}
      value={draft.override ? draft.value : defaultInput} disabled={disabled || !draft.override}
      onChange={(event) => onChange({ ...draft, value: event.target.value })}
      error={draft.override && Boolean(error)} helperText={draft.override && error ? error : fieldHelp[field]}
      slotProps={{ inputLabel: { shrink: true }, input: { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> }, htmlInput: { autoComplete: 'off', spellCheck: false, inputMode: field === 'MaxBitrate' ? 'decimal' : 'numeric', maxLength: 64 } }}
      sx={{ opacity: draft.override ? 1 : 0.45 }} />
    <FormControlLabel sx={{ m: 0, mt: 0.5, alignItems: 'flex-start', '& .MuiCheckbox-root': { p: 0.5, mr: 0.5 }, '& .MuiFormControlLabel-label': { fontSize: 12, pt: 0.5 } }}
      control={<Checkbox checked={!draft.override} disabled={disabled} onChange={(event) => onChange({ ...draft, override: !event.target.checked })} slotProps={{ input: { 'aria-label': 'Use deployment default for ' + label.toLowerCase() } }} />}
      label={'Use deployment default (' + formatSettingValue(field, settings.Defaults[field]) + ')'} />
  </Box>;
}

function ServerNameField({ settings, draft, disabled, stale, error, onChange }: { settings: ServerSettings; draft: ServerNameDraft; disabled: boolean; stale: boolean; error?: string; onChange: (value: ServerNameDraft) => void }) {
  const hostName = draft.mode === 'empty' || draft.mode === 'unset';
  const shownName = draft.mode === 'custom' ? draft.value : hostName ? settings.Deployment.HostName : settings.Defaults.ServerName;
  return <SettingsGroup title="Server identity" description="The saved name applies to new requests and appears in compatible clients. Empty and unset modes use the server host name." source={settings.Sources.ServerName} currentValue={settings.Effective.ServerName} defaultValue={settings.Defaults.ServerName} stale={stale}>
    <ToggleButtonGroup exclusive size="small" value={draft.mode} disabled={disabled} aria-label="Name source"
      onChange={(_, mode: ServerNameDraft['mode'] | null) => { if (mode) onChange({ ...draft, mode }); }}
      sx={{ alignSelf: 'flex-start', maxWidth: '100%', overflowX: 'auto', '& .MuiToggleButton-root': { px: { xs: 1.1, sm: 2 }, whiteSpace: 'nowrap', fontSize: 12 } }}>
      {([['deployment', 'Deployment default'], ['custom', 'Custom'], ['empty', 'Empty'], ['unset', 'Unset']] as const).map(([mode, label]) => <ToggleButton key={mode} value={mode}>{draft.mode === mode && <CheckRounded sx={{ fontSize: 16, mr: 0.5 }} />}{label}</ToggleButton>)}
    </ToggleButtonGroup>
    <Typography variant="caption" color="text.secondary">Empty and unset both use the host name. Compatible clients receive an empty name in empty mode; the name field is omitted in unset mode.</Typography>
    <TextField id="settings-ServerName-input" name="ServerName" fullWidth label="Server name" value={shownName}
      disabled={disabled || draft.mode !== 'custom'} onChange={(event) => onChange({ ...draft, value: event.target.value })}
      error={Boolean(error)} helperText={error || (draft.mode === 'custom' ? 'At most 128 UTF-8 bytes; no null characters. Leading and trailing spaces are preserved.' : hostName ? 'Host name captured when this server starts: ' + settings.Deployment.HostName : 'Uses the deployment name. This default can change when the server is redeployed.')}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: { autoComplete: 'off', spellCheck: false, maxLength: 128 } }} sx={{ opacity: draft.mode === 'custom' ? 1 : 0.45 }} />
  </SettingsGroup>;
}

function AdditionalWidthField({ settings, draft, disabled, stale, error, plannedWidth, plannedServerWidth, onChange }: { settings: ServerSettings; draft: string; disabled: boolean; stale: boolean; error?: string; plannedWidth?: number; plannedServerWidth?: number; onChange: (value: string) => void }) {
  const additional = settings.Encoding.TranscodingMaxWidth;
  const combined = additional > 0 ? Math.min(settings.Effective.MaxWidth, additional) : settings.Effective.MaxWidth;
  const preview = !stale && plannedWidth !== undefined;
  const shownAdditional = preview ? Number(draft) : additional;
  const shownServerWidth = preview ? plannedServerWidth : settings.Effective.MaxWidth;
  return <SettingsGroup title="Additional width limit" description="A shared extra ceiling for compatible clients. A positive limit combines with the server width limit; 0 removes only this additional ceiling." currentValue={additional === 0 ? 'No additional limit' : additional + ' px'} stale={stale}>
    <TextField id="settings-TranscodingMaxWidth-input" name="TranscodingMaxWidth" label="Additional video width limit" value={draft} disabled={disabled}
      onChange={(event) => onChange(event.target.value)} error={Boolean(error)} helperText={error || '0–8192 pixels. Use 0 for no additional limit.'}
      slotProps={{ input: { endAdornment: <InputAdornment position="end">px</InputAdornment> }, htmlInput: { autoComplete: 'off', inputMode: 'numeric', maxLength: 64 } }} sx={{ maxWidth: 360 }} />
    <Box sx={{ bgcolor: '#F3F6FB', borderRadius: '8px', p: 1.5, display: 'flex', gap: 1.5, alignItems: 'baseline' }}>
      <Typography aria-hidden="true" sx={{ color: 'primary.main', fontWeight: 600 }}>Σ</Typography>
      <Typography variant="caption" sx={{ fontVariantNumeric: 'tabular-nums' }}>{preview ? 'Width after save' : 'Last confirmed width'} = {shownAdditional > 0 ? 'min(server ' + shownServerWidth + ' px, additional ' + shownAdditional + ' px)' : 'server limit (no additional ceiling)'} = <strong>{preview ? plannedWidth : combined} px</strong></Typography>
    </Box>
  </SettingsGroup>;
}

function DeploymentSettings({ settings }: { settings: ServerSettings }) {
  const deployment = settings.Deployment;
  const entries = [
    ['Host name', deployment.HostName],
    ['Transcoding', deployment.TranscodingEnabled ? 'Enabled' : 'Disabled'],
    ['Hardware decoder', deployment.HardwareDecoder || 'Not configured'],
    ['Hardware encoder', deployment.HardwareEncoder || 'Not configured'],
    ['Threads per job', deployment.Threads.toLocaleString()],
    ['Maximum concurrent jobs', deployment.MaxJobs.toLocaleString()],
    ['Maximum jobs per user', deployment.MaxUserJobs.toLocaleString()],
    ['Maximum jobs per session', deployment.MaxSessionJobs.toLocaleString()],
  ];
  return <SettingsGroup title="Deployment configuration" description="Startup values managed by the server deployment. Runtime overrides apply to new jobs; identity, availability, and concurrency limits stay deployment settings.">
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><LockOutlined sx={{ fontSize: 18, color: 'text.secondary' }} /><Typography variant="caption" color="text.secondary">Read only</Typography></Stack>
    <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(3, minmax(0, 1fr))' }, gap: 2, m: 0, bgcolor: '#F3F6FB', p: 2.5, borderRadius: '16px' }}>
      {entries.map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.4, fontWeight: 600, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>{value}</Typography></Box>)}
    </Box>
  </SettingsGroup>;
}

export type SettingsSection = 'general' | 'transcode' | 'hardware' | 'metadata';

const sectionDescriptions: Record<SettingsSection, string> = {
  general: 'Manage server identity, HTTP listening, and media sorting. Database overrides take precedence over deployment defaults.',
  transcode: 'These limits apply to new output negotiations and conversion plans. Existing plans keep their current limits.',
  hardware: 'Hardware and CPU options apply to new playback plans and jobs. Existing jobs keep their admitted settings.',
  metadata: 'Configure online lookups, automatic subtitles, and task resources. Changes apply to the next work item or admission.',
};

function changedGroups(settings: ServerSettings, draft: SettingsDraft): number {
  const saved = draftFromSettings(settings);
  const savedKey = settingsDraftKey(saved);
  const candidates: SettingsDraft[] = [
    { ...saved, ServerName: draft.ServerName },
    { ...saved, ...Object.fromEntries(outputSettingsFields.map((field) => [field, draft[field]])) },
    { ...saved, TranscodingMaxWidth: draft.TranscodingMaxWidth },
    { ...saved, Sorting: draft.Sorting },
  ];
  if (saved.Runtime && draft.Runtime) {
    for (const field of ['Network', 'Hardware', 'Threads', 'H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping'] as const) {
      candidates.push({ ...saved, Runtime: { ...saved.Runtime, [field]: draft.Runtime[field] } });
    }
  }
  if (saved.Management && draft.Management) {
    candidates.push({ ...saved, Management: { ...saved.Management, Metadata: { ...saved.Management.Metadata, EnableInternetProviders: draft.Management.Metadata.EnableInternetProviders } } });
    candidates.push({ ...saved, Management: { ...saved.Management, Metadata: { ...draft.Management.Metadata, EnableInternetProviders: saved.Management.Metadata.EnableInternetProviders } } });
    for (const field of ['Subtitles', 'Tasks'] as const) {
      candidates.push({ ...saved, Management: { ...saved.Management, [field]: draft.Management[field] } });
    }
  }
  return candidates.filter((candidate) => settingsDraftKey(candidate) !== savedKey).length;
}

export function SettingsPage({ currentUserId, onNavigationGuardChange, section = 'general', onProviders, onBusyChange }: { currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange; section?: SettingsSection; onProviders?: () => void; onBusyChange?: (busy: boolean) => void }) {
  const [settings, setSettings] = useState<ServerSettings>();
  const [draft, setDraft] = useState<SettingsDraft>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [mutationError, setMutationError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState<'save' | 'reset'>();
  const [diagnosticBusy, setDiagnosticBusy] = useState(false);
  const [resetFields, setResetFields] = useState<SettingsResetField[]>();
  const [pendingAction, setPendingAction] = useState<'reload' | 'discard'>();
  const [notice, setNotice] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  const parsed = draft ? parseSettingsDraft(draft) : undefined;
  const dirty = Boolean(settings && draft && settingsDraftKey(draft) !== settingsDraftKey(draftFromSettings(settings)));
  const dirtyCount = settings && draft ? changedGroups(settings, draft) : 0;
  const blocked = requiresReload(mutationError);
  const disabled = loading || Boolean(busy) || blocked || diagnosticBusy;
  const savedOverrides = settings ? settingsResetFields.filter((field) => hasSavedOverride(settings, field)) : [];
  const plannedServerWidth = settings && parsed?.input ? parsed.input.Overrides.MaxWidth ?? settings.Defaults.MaxWidth : undefined;
  const plannedAdditionalWidth = parsed?.input?.Encoding.TranscodingMaxWidth;
  const plannedWidth = plannedServerWidth === undefined || plannedAdditionalWidth === undefined ? undefined
    : plannedAdditionalWidth > 0 ? Math.min(plannedServerWidth, plannedAdditionalWidth) : plannedServerWidth;
  useUserDraftNavigation(dirty, Boolean(busy) || diagnosticBusy, onNavigationGuardChange, 'Discard unsaved settings changes and leave this page?');
  useEffect(() => {
    onBusyChange?.(Boolean(busy) || diagnosticBusy);
    return () => onBusyChange?.(false);
  }, [busy, diagnosticBusy, onBusyChange]);

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
    if (mutation.current || diagnosticBusy) return;
    if (dirty) setPendingAction('reload');
    else reload();
  }

  function discardDraft() {
    if (!settings || mutation.current || diagnosticBusy) return;
    setDraft(draftFromSettings(settings));
    setPendingAction(undefined);
    setNotice('Unsaved changes discarded.');
  }

  function change<Field extends keyof SettingsDraft>(field: Field, value: SettingsDraft[Field]) {
    if (disabled) return;
    setDraft((current) => current ? { ...current, [field]: value } : current);
    setMutationError(undefined);
    setNotice('');
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings || !draft || !parsed?.input || !dirty || mutation.current || disabled) return;
    const controller = new AbortController();
    mutation.current = controller;
    setBusy('save');
    setMutationError(undefined);
    setNotice('');
    try {
      const value = await adminApi.updateSettings({ Revision: settings.Revision, ...parsed.input }, { signal: controller.signal });
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
    const fields = settingsResetFields.filter((field) => resetFields.includes(field));
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
          for (const field of fields) {
            if (field === 'ServerName') next.ServerName = fresh.ServerName;
            else if (field === 'TranscodingMaxWidth') next.TranscodingMaxWidth = fresh.TranscodingMaxWidth;
            else next[field] = fresh[field];
          }
          return next;
        });
        setResetFields(undefined);
        setNotice('Selected settings reset. Other unsaved edits remain in the form.');
      }
    } catch (error) {
      if (!controller.signal.aborted && !isAbortError(error)) setMutationError(error);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  function renderField(field: OutputSettingField) {
    if (!settings || !draft) return null;
    return <SettingField key={field} field={field} settings={settings} draft={draft[field]} disabled={disabled}
      error={parsed?.errors[field] ?? fieldError(mutationError, field) ?? fieldError(mutationError, `Overrides.${field}`)} onChange={(value) => change(field, value)} />;
  }

  return <Box aria-busy={loading || Boolean(busy) || diagnosticBusy}>
    <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 2, mb: 2 }}>
      <Typography variant="body1" color="text.secondary">{sectionDescriptions[section]}</Typography>
      <Button startIcon={<RefreshRounded />} onClick={requestReload} disabled={loading || Boolean(busy) || diagnosticBusy}>Reload settings</Button>
    </Stack>
    {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
    {loading && <Stack spacing={2.5} role="status" aria-label="Loading settings"><Skeleton variant="rounded" height={250} /><Skeleton variant="rounded" height={420} /></Stack>}
    {settings && draft && <Box component="form" onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)} noValidate>
      {section === 'general' && <>
        <ServerNameField settings={settings} draft={draft.ServerName} disabled={disabled} stale={blocked}
          error={parsed?.errors.ServerName ?? fieldError(mutationError, 'ServerName') ?? fieldError(mutationError, 'Overrides.ServerName') ?? fieldError(mutationError, 'ServerNameMode')}
          onChange={(value) => change('ServerName', value)} />
        {settings.Runtime && draft.Runtime && <RuntimeSettingsFields section="network" settings={settings.Runtime} draft={draft.Runtime} disabled={disabled} stale={blocked} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Runtime', value)} />}
        <SortingSettingsFields settings={settings.Sorting} draft={draft.Sorting} disabled={disabled} stale={blocked}
          error={parsed?.errors['Sorting.SortRemoveWords'] ?? fieldError(mutationError, 'Sorting.SortRemoveWords') ?? fieldError(mutationError, 'Sorting')}
          onChange={(value) => change('Sorting', value)} />
        <DeploymentSettings settings={settings} />
      </>}
      {section === 'transcode' && <>
        <SettingsGroup title="Output planning limits" description="Set an override for each output ceiling, or use its deployment default. Saved limits still apply when hardware falls back to CPU encoding."
          source={outputSettingsFields.some((field) => settings.Sources[field] === 'database') ? 'database' : 'deployment'}
          currentValue={formatMbps(settings.Effective.MaxBitrate) + ' Mbps · ' + settings.Effective.MaxWidth + ' × ' + settings.Effective.MaxHeight + ' · ' + settings.Effective.MaxAudioChannels + ' channels'}
          defaultValue={formatMbps(settings.Defaults.MaxBitrate) + ' Mbps · ' + settings.Defaults.MaxWidth + ' × ' + settings.Defaults.MaxHeight + ' · ' + settings.Defaults.MaxAudioChannels + ' channels'} stale={blocked}>
          {!settings.Deployment.TranscodingEnabled && <Alert severity="info">Transcoding is disabled in deployment configuration. Saved limits will apply when transcoding is enabled after a service restart.</Alert>}
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2.5 }}>{(['MaxBitrate', 'MaxAudioChannels', 'MaxWidth', 'MaxHeight'] as const).map(renderField)}</Box>
        </SettingsGroup>
        <AdditionalWidthField settings={settings} draft={draft.TranscodingMaxWidth} disabled={disabled} stale={blocked} plannedWidth={plannedWidth} plannedServerWidth={plannedServerWidth}
          error={parsed?.errors.TranscodingMaxWidth ?? fieldError(mutationError, 'TranscodingMaxWidth') ?? fieldError(mutationError, 'Encoding.TranscodingMaxWidth')}
          onChange={(value) => change('TranscodingMaxWidth', value)} />
      </>}
      {section === 'hardware' && <>
        {settings.Runtime && draft.Runtime
          ? <RuntimeSettingsFields settings={settings.Runtime} draft={draft.Runtime} disabled={disabled} stale={blocked} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Runtime', value)} />
          : <Alert severity="info">Runtime hardware settings are not available on this server.</Alert>}
        <Box sx={{ mt: 2, mb: 3 }}><MediaDiagnosticsPanel key={currentUserId} currentUserId={currentUserId} startBlocked={dirty || loading || Boolean(busy) || blocked || loadError != null} onBusyChange={setDiagnosticBusy} /></Box>
      </>}
      {section === 'metadata' && (draft.Management
        ? <ManagementSettingsFields draft={draft.Management} defaults={settings.ManagementDefaults} disabled={disabled} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Management', value)} onProviders={onProviders} />
        : <Alert severity="info">Metadata management settings are not available on this server.</Alert>)}
      {resetFields === undefined && mutationError != null && <Box sx={{ my: 2 }}><MutationNotice error={mutationError} reload={requestReload} /></Box>}
      {(section === 'general' || section === 'transcode') && <Box sx={{ pt: 1, pb: 2 }}><Button startIcon={<RestartAltRounded />} onClick={openReset} disabled={disabled || savedOverrides.length === 0}>Reset saved overrides</Button></Box>}
      {/* Offset the scroll container padding to keep the bar 16px from the panel edge and cover the gap with its mask. */}
      <Paper role="status" aria-live="polite" sx={{ position: 'sticky', bottom: { xs: -8, md: -16 }, zIndex: 5, mt: 2, mb: { xs: -1, md: -2 }, p: '10px 16px 10px 20px', borderRadius: '16px', outline: '16px solid #FFFFFF', bgcolor: dirty ? '#2B2F37' : '#EEF2F9', color: dirty ? '#EEF1F8' : 'text.secondary', boxShadow: dirty ? '0 4px 12px rgba(0,0,0,.22)' : 'none' }}>
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'stretch', sm: 'center' }, gap: 1.5 }}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1.2, minWidth: 0 }}>
            {dirty ? <EditNoteRounded sx={{ fontSize: 20, color: '#AFC6FF' }} /> : <CloudDoneRounded sx={{ fontSize: 20, color: blocked ? 'warning.main' : 'success.main' }} />}
            <Box>
              <Typography variant="body2">{dirty ? dirtyCount + (dirtyCount === 1 ? ' unsaved change' : ' unsaved changes') : blocked ? 'Settings need confirmation' : 'All settings saved'}{!dirty && <> · Revision {settings.Revision} · Updated <time dateTime={settings.UpdatedAt}>{new Date(settings.UpdatedAt).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></>}</Typography>
              {blocked && <Typography variant="caption">Reload the latest settings to continue.</Typography>}
              {dirty && !parsed?.input && <Typography variant="caption">Fix invalid fields before saving. Other settings tabs may contain errors.</Typography>}
            </Box>
          </Stack>
          <Stack direction="row" sx={{ flexWrap: 'wrap', justifyContent: 'flex-end', gap: 1 }}>
            {dirty && <Button onClick={() => setPendingAction('discard')} disabled={loading || Boolean(busy) || diagnosticBusy} sx={{ color: '#AFC6FF' }}>Discard changes</Button>}
            <Button type="submit" variant="contained" startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : undefined} disabled={disabled || !dirty || !parsed?.input} sx={dirty ? { bgcolor: '#AFC6FF', color: '#0A2A63', '&:hover': { bgcolor: '#C5D5FF' }, '&.Mui-disabled': { bgcolor: '#AFC6FF44', color: '#EEF1F888' } } : undefined}>{busy === 'save' ? 'Saving changes...' : 'Save changes'}</Button>
          </Stack>
        </Stack>
      </Paper>
    </Box>}
    <Snackbar open={Boolean(notice)} autoHideDuration={6000} onClose={() => setNotice('')} message={notice} />
    {settings && resetFields !== undefined && <Dialog open onClose={busy ? undefined : () => setResetFields(undefined)} fullWidth maxWidth="sm" aria-labelledby="reset-settings-title" aria-describedby="reset-settings-description">
      <DialogTitle id="reset-settings-title" sx={{ pt: 3 }}>Reset saved overrides</DialogTitle>
      <DialogContent aria-busy={busy === 'reset'}><Stack spacing={2}>
        <Typography id="reset-settings-description" variant="body2" color="text.secondary">Selected name and server limits return to deployment defaults. Resetting the additional video width limit sets it to 0; the server width limit still applies.</Typography>
        {dirty && <Alert severity="warning">Unsaved edits to selected settings will be discarded. Other draft changes stay in the form.</Alert>}
        <MutationNotice error={mutationError} reload={requestReload} />
        <FormControlLabel control={<Checkbox checked={savedOverrides.length > 0 && resetFields.length === savedOverrides.length} indeterminate={resetFields.length > 0 && resetFields.length < savedOverrides.length} disabled={Boolean(busy) || blocked} onChange={(event) => setResetFields(event.target.checked ? [...savedOverrides] : [])} />} label="Select all saved overrides" />
        <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 2, overflow: 'hidden' }}>{settingsResetFields.map((field, index) => <Box key={field} sx={{ p: 2, borderTop: index > 0 ? 1 : 0, borderColor: 'divider' }}>
          <FormControlLabel sx={{ m: 0, gap: 1, alignItems: 'flex-start', width: '100%', minWidth: 0, '& .MuiFormControlLabel-label': { minWidth: 0, flex: 1 } }} control={<Checkbox checked={resetFields.includes(field)} disabled={Boolean(busy) || blocked || !hasSavedOverride(settings, field)} onChange={(event) => setResetFields((current) => settingsResetFields.filter((candidate) => candidate === field ? event.target.checked : current?.includes(candidate)))} />} label={<Box sx={{ pt: 0.75 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{settingLabels[field]}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{hasSavedOverride(settings, field) ? `Current: ${resetCurrentValue(settings, field)}` : field === 'TranscodingMaxWidth' ? 'Already has no additional limit' : 'Already using deployment default'}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{resetDefaultValue(settings, field)}</Typography></Box>} />
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
