import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, InputAdornment, Skeleton, Snackbar, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography } from './ui';
import { LockOutlined } from './icons';
import { RefreshRounded } from './icons';
import { RestartAltRounded } from './icons';
import { CheckRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { ServerSettings, SettingsResetField } from './api';
import { ErrorNotice } from './components';
import { SettingsGroup } from './SettingsLayout';
import { MediaDiagnosticsPanel } from './MediaDiagnosticsPanel';
import { ManagementSettingsFields } from './ManagementSettingsFields';
import { RuntimeSettingsFields } from './RuntimeSettingsFields';
import { runtimeFields } from './runtimeSettings';
import { SortingSettingsFields } from './SortingSettingsFields';
import { ProvidersPage } from './ProvidersPage';
import { ServerPolicyFields } from './ServerPolicyFields';
import type { ServerPolicyState } from './ServerPolicyFields';
import { useAdminPreferences, useTranslation } from './i18n';
import { fieldError } from './formFields';
import { draftFromSettings, formatMbps, formatSettingValue, outputSettingsFields, parseSettingsDraft, settingLabels, settingsDraftKey, settingsResetFields } from './settingsDraft';
import type { OutputSettingField, ServerNameDraft, SettingDraft, SettingsDraft } from './settingsDraft';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';

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

function ResetSettingValue({ settings, field, kind }: { settings: ServerSettings; field: SettingsResetField; kind: 'current' | 'default' }) {
  const { t } = useTranslation();
  let value: string;
  if (kind === 'default') {
    if (field === 'TranscodingMaxWidth') return t('Reset value: 0 (no additional limit). The server width limit still applies.');
    value = formatSettingValue(field, settings.Defaults[field]);
  } else {
    value = resetCurrentValue(settings, field);
  }
  return <>{t(kind === 'current' ? 'Current value' : 'Deployment default')}: {field === 'ServerName' ? <span translate="no">{value}</span> : t(value)}</>;
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
  const { t } = useTranslation();
  const label = settingLabels[field];
  const defaultInput = field === 'MaxBitrate' ? formatMbps(settings.Defaults.MaxBitrate) : String(settings.Defaults[field]);
  const unit = field === 'MaxBitrate' ? 'Mbps' : field === 'MaxAudioChannels' ? 'channels' : 'px';
  return <div className="settings-setting-row">
    <TextField id={'settings-' + field + '-input'} name={field} fullWidth label={label}
      value={draft.override ? draft.value : defaultInput} disabled={disabled || !draft.override}
      onChange={(event) => onChange({ ...draft, value: event.target.value })}
      error={draft.override && Boolean(error)} helperText={draft.override && error ? error : fieldHelp[field]}
      slotProps={{ inputLabel: { shrink: true }, input: { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> }, htmlInput: { autoComplete: 'off', spellCheck: false, inputMode: field === 'MaxBitrate' ? 'decimal' : 'numeric', maxLength: 64 } }} />
    <FormControlLabel className="settings-default-label"
      control={<Checkbox checked={!draft.override} disabled={disabled} onChange={(event) => onChange({ ...draft, override: !event.target.checked })} slotProps={{ input: { 'aria-label': 'Use deployment default for ' + label.toLowerCase() } }} />}
      label={<>{t('Use deployment default')} ({t(formatSettingValue(field, settings.Defaults[field]))})</>} />
  </div>;
}

function ServerNameField({ settings, draft, disabled, stale, error, onChange }: { settings: ServerSettings; draft: ServerNameDraft; disabled: boolean; stale: boolean; error?: string; onChange: (value: ServerNameDraft) => void }) {
  const { t } = useTranslation();
  const hostName = draft.mode === 'empty' || draft.mode === 'unset';
  const shownName = draft.mode === 'custom' ? draft.value : hostName ? settings.Deployment.HostName : settings.Defaults.ServerName;
  return <SettingsGroup title="Server identity" description="The saved name applies to new requests and appears in compatible clients. Empty and unset modes use the server host name." source={settings.Sources.ServerName} currentValue={settings.Effective.ServerName} defaultValue={settings.Defaults.ServerName} stale={stale} preserveValues>
    <TextField id="settings-ServerName-input" name="ServerName" fullWidth label="Server name" value={shownName}
      disabled={disabled || draft.mode !== 'custom'} onChange={(event) => onChange({ ...draft, value: event.target.value })}
      error={Boolean(error)} helperText={error}
      slotProps={{ inputLabel: { shrink: true }, htmlInput: { autoComplete: 'off', spellCheck: false, maxLength: 128 } }} />
    <div className="settings-source-row"><span>{t('Name source')}</span>
    <ToggleButtonGroup className="settings-name-source" exclusive size="small" value={draft.mode} disabled={disabled} aria-label="Name source"
      onChange={(_, mode: ServerNameDraft['mode'] | null) => { if (mode) onChange({ ...draft, mode }); }}>
      {([['deployment', 'Deployment default'], ['custom', 'Custom'], ['empty', 'Empty'], ['unset', 'Unset']] as const).map(([mode, label]) => <ToggleButton key={mode} value={mode}>{draft.mode === mode && <CheckRounded sx={{ fontSize: 16, mr: 0.5 }} />}{label}</ToggleButton>)}
    </ToggleButtonGroup>
    </div>
    {hostName && <Typography variant="caption" color="text.secondary">Empty and unset both use the host name. Compatible clients receive an empty name in empty mode; the name field is omitted in unset mode.</Typography>}
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
      slotProps={{ input: { endAdornment: <InputAdornment position="end">px</InputAdornment> }, htmlInput: { autoComplete: 'off', inputMode: 'numeric', maxLength: 64 } }} />
    <Box sx={{ bgcolor: 'var(--s1)', borderRadius: '10px', p: 1.5, display: 'flex', gap: 1.5, alignItems: 'baseline' }}>
      <Typography aria-hidden="true" sx={{ color: 'primary.main', fontWeight: 600 }}>Σ</Typography>
      <Typography variant="caption" sx={{ fontVariantNumeric: 'tabular-nums' }}>{preview ? 'Width after save' : 'Last confirmed width'} = {shownAdditional > 0 ? 'min(server ' + shownServerWidth + ' px, additional ' + shownAdditional + ' px)' : 'server limit (no additional ceiling)'} = <strong>{preview ? plannedWidth : combined} px</strong></Typography>
    </Box>
  </SettingsGroup>;
}

function DeploymentSettings({ settings }: { settings: ServerSettings }) {
  const { t, locale } = useTranslation();
  const deployment = settings.Deployment;
  const entries = [
    ['Host name', deployment.HostName],
    ['Transcoding', deployment.TranscodingEnabled ? 'Enabled' : 'Disabled'],
    ['Hardware decoder', deployment.HardwareDecoder || 'Not configured'],
    ['Hardware encoder', deployment.HardwareEncoder || 'Not configured'],
    ['Threads per job', deployment.Threads.toLocaleString(locale)],
    ['Maximum concurrent jobs', deployment.MaxJobs.toLocaleString(locale)],
    ['Maximum jobs per user', deployment.MaxUserJobs.toLocaleString(locale)],
    ['Maximum jobs per session', deployment.MaxSessionJobs.toLocaleString(locale)],
  ];
  return <SettingsGroup title="Deployment configuration" description="Startup values managed by the server deployment. Runtime overrides apply to new jobs; identity, availability, and concurrency limits stay deployment settings.">
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><LockOutlined sx={{ fontSize: 18, color: 'text.secondary' }} /><Typography variant="caption" color="text.secondary">Read only</Typography></Stack>
    <dl className="settings-deployment-values">
      {entries.map(([label, value]) => <div key={label}><dt>{t(label)}</dt><dd translate={label === 'Host name' ? 'no' : undefined}>{label === 'Host name' ? value : t(value)}</dd></div>)}
    </dl>
  </SettingsGroup>;
}

export type SettingsSection = 'general' | 'transcode' | 'hardware' | 'metadata';

const sectionDescriptions: Record<SettingsSection, string> = {
  general: 'Server identity, network listening, and library sorting.',
  transcode: 'Hardware, software encoding, and output limits. Changes apply to new playback plans and jobs.',
  hardware: 'Hardware, software encoding, and output limits. Changes apply to new playback plans and jobs.',
  metadata: 'Metadata, subtitle downloads, and online sources. Changes apply to the next task.',
};
const sectionTitles: Record<SettingsSection, string> = { general: 'General', transcode: 'Transcoding', hardware: 'Transcoding', metadata: 'Metadata & subtitles' };

function InterfacePreferences() {
  const { t } = useTranslation();
  const { language, theme, setLanguage, setTheme } = useAdminPreferences();
  return <SettingsGroup title="Interface" description="Appearance and language apply to this browser and are saved immediately.">
    <div className="settings-interface-grid">
      <label className="settings-native-field">{t('Interface language')}
        <select value={language} onChange={(event) => setLanguage(event.target.value === 'en' ? 'en' : 'zh')}>
          <option value="zh">中文（简体）</option><option value="en">English</option>
        </select>
      </label>
      <label className="settings-native-field">{t('Appearance')}
        <select value={theme} onChange={(event) => setTheme(event.target.value === 'light' ? 'light' : 'dark')}>
          <option value="dark">{t('Dark')}</option><option value="light">{t('Light')}</option>
        </select>
      </label>
    </div>
  </SettingsGroup>;
}

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
    for (const field of runtimeFields) {
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
  const { t, locale, countLabel } = useTranslation();
  const form = useRef<HTMLFormElement>(null);
  const [settings, setSettings] = useState<ServerSettings>();
  const [draft, setDraft] = useState<SettingsDraft>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [mutationError, setMutationError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState<'save' | 'reset'>();
  const [diagnosticBusy, setDiagnosticBusy] = useState(false);
  const [serverPolicyState, setServerPolicyState] = useState<ServerPolicyState>({ dirty: false, busy: false });
  const [policyMounted, setPolicyMounted] = useState(section === 'general');
  const [providersMounted, setProvidersMounted] = useState(section === 'metadata');
  const [providerEditing, setProviderEditing] = useState(false);
  const ownGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const providerGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const publishGuard = useCallback(() => {
    onNavigationGuardChange(ownGuard.current || providerGuard.current
      ? () => (!providerGuard.current || providerGuard.current()) && (!ownGuard.current || ownGuard.current())
      : undefined);
  }, [onNavigationGuardChange]);
  const setOwnGuard = useCallback((guard: UserNavigationGuard | undefined) => { ownGuard.current = guard; publishGuard(); }, [publishGuard]);
  const setProviderGuard = useCallback((guard: UserNavigationGuard | undefined) => { providerGuard.current = guard; setProviderEditing(Boolean(guard)); publishGuard(); }, [publishGuard]);
  const [resetFields, setResetFields] = useState<SettingsResetField[]>();
  const [pendingAction, setPendingAction] = useState<'reload' | 'discard'>();
  const [notice, setNotice] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  const parsed = draft ? parseSettingsDraft(draft) : undefined;
  const dirty = Boolean(settings && draft && settingsDraftKey(draft) !== settingsDraftKey(draftFromSettings(settings)));
  const dirtyCount = settings && draft ? changedGroups(settings, draft) : 0;
  const blocked = requiresReload(mutationError);
  const disabled = loading || Boolean(busy) || blocked || diagnosticBusy || serverPolicyState.busy;
  const savedOverrides = settings ? settingsResetFields.filter((field) => hasSavedOverride(settings, field)) : [];
  const plannedServerWidth = settings && parsed?.input ? parsed.input.Overrides.MaxWidth ?? settings.Defaults.MaxWidth : undefined;
  const plannedAdditionalWidth = parsed?.input?.Encoding.TranscodingMaxWidth;
  const plannedWidth = plannedServerWidth === undefined || plannedAdditionalWidth === undefined ? undefined
    : plannedAdditionalWidth > 0 ? Math.min(plannedServerWidth, plannedAdditionalWidth) : plannedServerWidth;
  const transcoding = section === 'transcode' || section === 'hardware';
  useUserDraftNavigation(dirty || serverPolicyState.dirty, Boolean(busy) || diagnosticBusy || serverPolicyState.busy, setOwnGuard, 'Discard unsaved settings changes and leave this page?');
  useEffect(() => {
    if (section === 'general') setPolicyMounted(true);
    if (section === 'metadata') setProvidersMounted(true);
  }, [section]);
  useEffect(() => {
    onBusyChange?.(Boolean(busy) || diagnosticBusy || serverPolicyState.busy || providerEditing);
    return () => onBusyChange?.(false);
  }, [busy, diagnosticBusy, serverPolicyState.busy, providerEditing, onBusyChange]);

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
  useEffect(() => {
    const currentForm = form.current;
    const page = currentForm?.closest<HTMLElement>('.settings-page');
    const scroller = page?.closest<HTMLElement>('.dashboard-content');
    const bar = currentForm?.querySelector<HTMLElement>('.settings-savebar');
    if (!currentForm || !page || !scroller || !bar) return;
    const clearanceProperty = '--settings-focus-clearance';
    const previousClearance = scroller.style.getPropertyValue(clearanceProperty);
    let frame = 0;
    const keepFocusedControlVisible = () => {
      frame = 0;
      let barRect = bar.getBoundingClientRect();
      const scrollerRect = scroller.getBoundingClientRect();
      scroller.style.setProperty(clearanceProperty, Math.ceil(barRect.height + 12) + 'px');
      const focused = document.activeElement;
      if (!(focused instanceof HTMLElement) || !page.contains(focused) || bar.contains(focused)
        || focused.closest('dialog[open]') || !focused.matches('input, select, textarea, button, a[href], summary, [tabindex]:not([tabindex="-1"])')) return;
      let controlRect = focused.getBoundingClientRect();
      if (currentForm.contains(focused) && (controlRect.top < scrollerRect.top
        || controlRect.bottom > Math.min(scrollerRect.bottom, innerHeight))) {
        focused.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
        controlRect = focused.getBoundingClientRect();
        barRect = bar.getBoundingClientRect();
      }
      const barVisible = barRect.bottom > scrollerRect.top && barRect.top < scrollerRect.bottom;
      const overlapsHorizontally = controlRect.right > barRect.left && controlRect.left < barRect.right;
      if (!barVisible || !overlapsHorizontally || controlRect.top >= barRect.bottom
        || controlRect.bottom <= barRect.top - 12) return;
      // Move only a focused control that the action bar actually covers.
      // Manual scrolling and modal focus do not scroll the background form.
      scroller.scrollBy({ top: controlRect.bottom - barRect.top + 12, behavior: 'instant' });
    };
    const scheduleVisibilityCheck = () => {
      if (!frame) frame = requestAnimationFrame(keepFocusedControlVisible);
    };
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(scheduleVisibilityCheck);
    observer?.observe(bar);
    observer?.observe(currentForm);
    page.addEventListener('focusin', scheduleVisibilityCheck);
    window.addEventListener('resize', scheduleVisibilityCheck);
    scheduleVisibilityCheck();
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      page.removeEventListener('focusin', scheduleVisibilityCheck);
      window.removeEventListener('resize', scheduleVisibilityCheck);
      if (previousClearance) scroller.style.setProperty(clearanceProperty, previousClearance);
      else scroller.style.removeProperty(clearanceProperty);
    };
  }, [Boolean(settings && draft), section]);
  useEffect(() => {
    function saveShortcut(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 's') return;
      event.preventDefault();
      if (dirty && !disabled && parsed?.input && resetFields === undefined && !pendingAction) form.current?.requestSubmit();
    }
    document.addEventListener('keydown', saveShortcut);
    return () => document.removeEventListener('keydown', saveShortcut);
  }, [dirty, disabled, parsed?.input, resetFields, pendingAction]);

  function reload() {
    setPendingAction(undefined);
    setResetFields(undefined);
    setMutationError(undefined);
    setNotice('');
    setRevision((value) => value + 1);
  }

  function requestReload() {
    if (mutation.current || diagnosticBusy || serverPolicyState.busy || providerGuard.current) return;
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

  return <div className="settings-page" aria-busy={loading || Boolean(busy) || diagnosticBusy}>
    <header className="settings-page-header">
      <div><h1>{t(sectionTitles[section])}</h1><p className="visually-hidden">{t(sectionDescriptions[section])}</p></div>
      <Button startIcon={<RefreshRounded />} onClick={requestReload} disabled={loading || Boolean(busy) || diagnosticBusy || serverPolicyState.busy || providerEditing}>Reload settings</Button>
    </header>
    {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
    {loading && <Stack spacing={2.5} role="status" aria-label="Loading settings"><Skeleton variant="rounded" height={250} /><Skeleton variant="rounded" height={420} /></Stack>}
    {settings && draft && <form className="settings-form" ref={form} onSubmit={(event: FormEvent<HTMLFormElement>) => void save(event)} noValidate>
      {section === 'general' && <>
        <ServerNameField settings={settings} draft={draft.ServerName} disabled={disabled} stale={blocked}
          error={parsed?.errors.ServerName ?? fieldError(mutationError, 'ServerName') ?? fieldError(mutationError, 'Overrides.ServerName') ?? fieldError(mutationError, 'ServerNameMode')}
          onChange={(value) => change('ServerName', value)} />
        {settings.Runtime && draft.Runtime && <RuntimeSettingsFields section="network" settings={settings.Runtime} draft={draft.Runtime} disabled={disabled} stale={blocked} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Runtime', value)} />}
        <SortingSettingsFields settings={settings.Sorting} draft={draft.Sorting} disabled={disabled} stale={blocked}
          error={parsed?.errors['Sorting.SortRemoveWords'] ?? fieldError(mutationError, 'Sorting.SortRemoveWords') ?? fieldError(mutationError, 'Sorting')}
          onChange={(value) => change('Sorting', value)} />
        <InterfacePreferences />
        <DeploymentSettings settings={settings} />
      </>}
      {transcoding && <>
        {settings.Runtime && draft.Runtime
          ? <RuntimeSettingsFields settings={settings.Runtime} draft={draft.Runtime} disabled={disabled} stale={blocked} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Runtime', value)} />
          : <Alert severity="info">Runtime hardware settings are not available on this server.</Alert>}
        <SettingsGroup title="Output planning limits" description="Set an override for each output ceiling, or use its deployment default. Saved limits still apply when hardware falls back to CPU encoding."
          source={outputSettingsFields.some((field) => settings.Sources[field] === 'database') ? 'database' : 'deployment'}
          currentValue={formatMbps(settings.Effective.MaxBitrate) + ' Mbps · ' + settings.Effective.MaxWidth + ' × ' + settings.Effective.MaxHeight + ' · ' + settings.Effective.MaxAudioChannels + ' ' + countLabel(settings.Effective.MaxAudioChannels, 'channel', 'channels')}
          defaultValue={formatMbps(settings.Defaults.MaxBitrate) + ' Mbps · ' + settings.Defaults.MaxWidth + ' × ' + settings.Defaults.MaxHeight + ' · ' + settings.Defaults.MaxAudioChannels + ' ' + countLabel(settings.Defaults.MaxAudioChannels, 'channel', 'channels')} stale={blocked}>
          {!settings.Deployment.TranscodingEnabled && <Alert severity="info">Transcoding is disabled in deployment configuration. Saved limits will apply when transcoding is enabled after a service restart.</Alert>}
          <div className="settings-form-rows">{(['MaxBitrate', 'MaxAudioChannels', 'MaxWidth', 'MaxHeight'] as const).map(renderField)}</div>
        </SettingsGroup>
        <AdditionalWidthField settings={settings} draft={draft.TranscodingMaxWidth} disabled={disabled} stale={blocked} plannedWidth={plannedWidth} plannedServerWidth={plannedServerWidth}
          error={parsed?.errors.TranscodingMaxWidth ?? fieldError(mutationError, 'TranscodingMaxWidth') ?? fieldError(mutationError, 'Encoding.TranscodingMaxWidth')}
          onChange={(value) => change('TranscodingMaxWidth', value)} />
        <section className="settings-diagnostics" aria-label={t('Playback diagnostics')}><h2>{t('Playback diagnostics')}</h2><MediaDiagnosticsPanel key={currentUserId} currentUserId={currentUserId} startBlocked={dirty || loading || Boolean(busy) || blocked || loadError != null} onBusyChange={setDiagnosticBusy} /></section>
      </>}
      {section === 'metadata' && <>{draft.Management
        ? <ManagementSettingsFields draft={draft.Management} defaults={settings.ManagementDefaults} disabled={disabled} errors={parsed?.errors ?? {}} mutationError={mutationError} onChange={(value) => change('Management', value)} onProviders={onProviders} />
        : <Alert severity="info">Metadata management settings are not available on this server.</Alert>}
      </>}
      {resetFields === undefined && mutationError != null && <Box sx={{ my: 2 }}><MutationNotice error={mutationError} reload={requestReload} /></Box>}
      {(section === 'general' || transcoding) && <div className="settings-reset"><Button startIcon={<RestartAltRounded />} onClick={openReset} disabled={disabled || savedOverrides.length === 0}>Reset saved overrides</Button></div>}
      <div className="settings-savebar" data-dirty={dirty}>
        <div className="settings-savebar-inner">
          <span className="settings-savebar-dot" aria-hidden="true" />
          <div className="settings-savebar-copy" role="status" aria-live="polite">
            <p><strong>{dirty ? t(dirtyCount + (dirtyCount === 1 ? ' unsaved change' : ' unsaved changes')) : t(blocked ? 'Settings need confirmation' : 'All settings saved')}</strong></p>
            {!dirty && <p className="settings-savebar-detail">{t('Revision')} {settings.Revision} · <time dateTime={settings.UpdatedAt}>{new Date(settings.UpdatedAt).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></p>}
            {blocked && <p>{t('Reload the latest settings to continue.')}</p>}
            {dirty && !parsed?.input && <p>{t('Fix invalid fields before saving. Other settings tabs may contain errors.')}</p>}
          </div>
          <div className="settings-savebar-actions">
            {dirty && <><kbd>Ctrl / ⌘ S</kbd><Button variant="outlined" onClick={() => setPendingAction('discard')} disabled={loading || Boolean(busy) || diagnosticBusy}>Discard changes</Button></>}
            <Button type="submit" variant="contained" startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : undefined} disabled={disabled || !dirty || !parsed?.input}>{busy === 'save' ? 'Saving changes...' : 'Save changes'}</Button>
          </div>
        </div>
      </div>
    </form>}
    {policyMounted && <div className="settings-subpage" hidden={section !== 'general'}>
      <ServerPolicyFields disabled={loading || Boolean(busy) || diagnosticBusy || blocked} onStateChange={setServerPolicyState} />
    </div>}
    {providersMounted && <div className="settings-subpage" hidden={section !== 'metadata'}>
      <ProvidersPage embedded onNavigationGuardChange={setProviderGuard} />
    </div>}
    <Snackbar open={Boolean(notice)} autoHideDuration={6000} onClose={() => setNotice('')} message={notice} />
    {settings && resetFields !== undefined && <Dialog open onClose={busy ? undefined : () => setResetFields(undefined)} fullWidth maxWidth="sm" aria-labelledby="reset-settings-title" aria-describedby="reset-settings-description">
      <DialogTitle id="reset-settings-title" sx={{ pt: 3 }}>Reset saved overrides</DialogTitle>
      <DialogContent aria-busy={busy === 'reset'}><Stack spacing={2}>
        <Typography id="reset-settings-description" variant="body2" color="text.secondary">Selected name and server limits return to deployment defaults. Resetting the additional video width limit sets it to 0; the server width limit still applies.</Typography>
        {dirty && <Alert severity="warning">Unsaved edits to selected settings will be discarded. Other draft changes stay in the form.</Alert>}
        <MutationNotice error={mutationError} reload={requestReload} />
        <FormControlLabel control={<Checkbox checked={savedOverrides.length > 0 && resetFields.length === savedOverrides.length} indeterminate={resetFields.length > 0 && resetFields.length < savedOverrides.length} disabled={Boolean(busy) || blocked} onChange={(event) => setResetFields(event.target.checked ? [...savedOverrides] : [])} />} label="Select all saved overrides" />
        <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 2, overflow: 'hidden' }}>{settingsResetFields.map((field, index) => <Box key={field} sx={{ p: 2, borderTop: index > 0 ? 1 : 0, borderColor: 'divider' }}>
          <FormControlLabel sx={{ m: 0, gap: 1, alignItems: 'flex-start', width: '100%', minWidth: 0, '& .MuiFormControlLabel-label': { minWidth: 0, flex: 1 } }} control={<Checkbox checked={resetFields.includes(field)} disabled={Boolean(busy) || blocked || !hasSavedOverride(settings, field)} onChange={(event) => setResetFields((current) => settingsResetFields.filter((candidate) => candidate === field ? event.target.checked : current?.includes(candidate)))} />} label={<Box sx={{ pt: 0.75 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{settingLabels[field]}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{hasSavedOverride(settings, field) ? <ResetSettingValue settings={settings} field={field} kind="current" /> : field === 'TranscodingMaxWidth' ? 'Already has no additional limit' : 'Already using deployment default'}</Typography><Typography variant="caption" color="text.secondary" component="div" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}><ResetSettingValue settings={settings} field={field} kind="default" /></Typography></Box>} />
        </Box>)}</Box>
      </Stack></DialogContent>
      <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" disabled={Boolean(busy)} onClick={() => setResetFields(undefined)}>Cancel</Button><Button color="error" variant="contained" startIcon={busy === 'reset' ? <CircularProgress size={16} color="inherit" /> : <RestartAltRounded />} disabled={Boolean(busy) || blocked || resetFields.length === 0} onClick={() => void resetSelected()}>{busy === 'reset' ? 'Resetting settings...' : 'Reset selected settings'}</Button></DialogActions>
    </Dialog>}
    {pendingAction && <Dialog open onClose={() => setPendingAction(undefined)} fullWidth maxWidth="xs" aria-labelledby="discard-settings-title">
      <DialogTitle id="discard-settings-title">Discard unsaved settings changes?</DialogTitle>
      <DialogContent><Typography variant="body2" color="text.secondary">{pendingAction === 'reload' ? 'Reloading replaces your draft with the latest saved settings. All unsaved edits will be lost.' : 'Your unsaved edits will be replaced with the last loaded settings.'}</Typography></DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" autoFocus onClick={() => setPendingAction(undefined)}>Keep editing</Button><Button color="error" variant="contained" onClick={pendingAction === 'reload' ? reload : discardDraft}>{pendingAction === 'reload' ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions>
    </Dialog>}
  </div>;
}
