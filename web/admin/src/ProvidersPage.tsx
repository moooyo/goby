import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { confirmAction, useTranslation } from './i18n';
import { providersApi, providerWebsite } from './providersApi';
import type { ConfigurableProvider, OnlineProviders, ProviderConfiguration, ProviderConfigurationField, ProviderConfigurationInput, ProviderConnectionTest, ProviderId, ProviderTestStatus } from './providersApi';
import { Dialog, DialogActions, DialogContent, DialogTitle } from './ui';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import tmdbLogo from './assets/tmdb.svg';
import './providers.css';

const capabilityLabels: Record<string, string> = { metadata: 'Metadata', images: 'Images', subtitles: 'Subtitles' };
const noGuard: UserNavigationGuardChange = () => undefined;
const discardMessage = 'Discard unsaved provider changes and leave this page?';
const testLabels: Record<ProviderTestStatus, string> = {
  passed: 'Connection test passed', disabled: 'Online providers are disabled', not_configured: 'Complete the provider configuration before testing',
  authentication_failed: 'The provider rejected the saved credentials', quota: 'The provider rate or account quota was reached', unavailable: 'The provider could not be reached',
};
type FieldAction = 'keep' | 'replace' | 'clear' | 'default';
interface FieldDraft { action: FieldAction; value: string }
type ConfigurationDraft = Record<string, FieldDraft>;
type EditorOperation = 'save' | 'reload' | 'test';

function RefreshIcon() {
  return <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 7v5h-5M4 17v-5h5" /><path d="M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3" /></svg>;
}
function ExternalIcon() {
  return <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 3h7v7m0-7L10 14" /><path d="M10 3H3v18h18v-7" /></svg>;
}
function initialDraft(provider: ConfigurableProvider): ConfigurationDraft {
  return Object.fromEntries(provider.Fields.map((field) => [field.Id, { action: 'keep' as const, value: '' }]));
}
function effectiveChange(field: ProviderConfigurationField, draft: FieldDraft): boolean {
  return draft.action === 'replace' || draft.action === 'clear' && (field.HasValue || field.Source !== 'override')
    || draft.action === 'default' && field.Source !== 'deployment';
}
function fieldLimit(field: ProviderConfigurationField): number { return field.Id === 'UserAgent' ? 512 : 16_384; }
function draftInput(provider: ConfigurableProvider, saved: ProviderConfiguration, draft: ConfigurationDraft): { input: ProviderConfigurationInput; errors: Record<string, string> } {
  const input: ProviderConfigurationInput = { Revision: saved.Revision, Fields: {} };
  const errors: Record<string, string> = {};
  for (const field of provider.Fields) {
    const change = draft[field.Id];
    if (!change || change.action === 'keep' || !effectiveChange(field, change)) continue;
    if (change.action === 'replace') {
      if (!change.value) errors[field.Id] = 'Enter a replacement value, or choose Clear value.';
      else if (new TextEncoder().encode(change.value).length > fieldLimit(field) || /[\p{Cc}\p{Cs}]/u.test(change.value)) errors[field.Id] = 'Use a single-line value within the displayed size limit.';
      input.Fields[field.Id] = { Mode: 'replace', Value: change.value };
    } else input.Fields[field.Id] = { Mode: change.action };
  }
  if (provider.Id === 'opensubtitles') {
    const presence = (id: string): boolean | undefined => {
      const field = provider.Fields.find((value) => value.Id === id)!;
      const value = draft[id];
      if (value.action === 'keep') return field.HasValue;
      if (value.action === 'clear') return false;
      if (value.action === 'replace') return Boolean(value.value);
      return field.Source === 'deployment' ? field.HasValue : undefined;
    };
    const username = presence('Username'), password = presence('Password');
    if (username !== undefined && password !== undefined && username !== password) {
      errors.Username = errors.Password = 'OpenSubtitles username and password must both be configured or cleared.';
    }
  }
  return { input, errors };
}
function requiresReadback(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 409 || error.status === 408 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed', 'invalid_provider_revision'].includes(error.code);
}
function ConfigurationTestResult({ result }: { result: ProviderConnectionTest }) {
  const { t, locale } = useTranslation();
  const time = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(result.CheckedAt));
  return <div className="providers-page__test-result" data-outcome={result.Status} role="status" aria-live="polite">
    <strong>{t(testLabels[result.Status])}</strong>
    <time dateTime={result.CheckedAt}>{t('Checked {time}', { time })}</time>
    <p>{t('This checks the saved credentials and one provider endpoint. It does not reserve download quota or verify every provider feature.')}</p>
  </div>;
}

function ProviderConfigurationEditor({ providerId, initial, onUpdate, onRequireReadback, onClose, onNavigationGuardChange }: {
  providerId: ProviderId; initial: ProviderConfiguration; onUpdate: (configuration: ProviderConfiguration) => void; onRequireReadback: () => void; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t } = useTranslation();
  const formId = useId();
  const [saved, setSaved] = useState(initial);
  const provider = saved.Items.find((value) => value.Id === providerId)!;
  const [draft, setDraft] = useState(() => initialDraft(provider));
  const [operation, setOperation] = useState<EditorOperation>();
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const [review, setReview] = useState(false);
  const [testResult, setTestResult] = useState<ProviderConnectionTest>();
  const pending = useRef<AbortController | undefined>(undefined);
  const form = useRef<HTMLFormElement>(null);
  const mounted = useRef(true);
  const cancelledTest = useRef(false);
  const parsed = useMemo(() => draftInput(provider, saved, draft), [provider, saved, draft]);
  const dirty = Object.keys(parsed.input.Fields).length > 0;
  const disabled = Boolean(operation) || review;
  useUserDraftNavigation(dirty, Boolean(operation), onNavigationGuardChange, discardMessage);
  useEffect(() => {
    mounted.current = true;
    form.current?.querySelector('select')?.focus({ preventScroll: true });
    return () => { mounted.current = false; pending.current?.abort(); };
  }, []);

  function close() {
    if (pending.current) return;
    if (!dirty || confirmAction(discardMessage)) onClose();
  }
  function change(field: string, value: FieldDraft) {
    if (disabled) return;
    setDraft((current) => ({ ...current, [field]: value }));
    setError(undefined); setNotice(''); setTestResult(undefined);
  }
  async function perform(kind: EditorOperation) {
    if (pending.current) return;
    if (kind === 'save' && (!dirty || review || Object.keys(parsed.errors).length)) return;
    if (kind === 'test' && (dirty || review || !saved.Enabled || !provider.Configured)) return;
    if (kind === 'reload' && dirty && !confirmAction('Discard the provider draft and reload the saved configuration?')) return;
    const controller = new AbortController(); pending.current = controller; cancelledTest.current = false;
    setOperation(kind); setError(undefined); setNotice('');
    if (kind !== 'save') setTestResult(undefined);
    let timedOut = false;
    const timer = window.setTimeout(() => { timedOut = true; controller.abort(); }, kind === 'test' ? 30_000 : 20_000);
    try {
      if (kind === 'test') {
        const result = await providersApi.testConnection(providerId, saved.Revision, { signal: controller.signal });
        if (mounted.current && !controller.signal.aborted) setTestResult(result);
      } else {
        const result = kind === 'save'
          ? await providersApi.updateConfiguration(providerId, parsed.input, { signal: controller.signal })
          : await providersApi.getConfiguration({ signal: controller.signal });
        if (mounted.current && !controller.signal.aborted) {
          setSaved(result); setDraft(initialDraft(result.Items.find((value) => value.Id === providerId)!));
          setReview(false); setTestResult(undefined); onUpdate(result);
          setNotice(kind === 'save' ? 'Provider configuration saved. New requests use the saved configuration.' : 'Saved provider configuration loaded. Review it before making another change.');
        }
      }
    } catch (cause) {
      if (!mounted.current) return;
      if (kind === 'test' && cancelledTest.current) { setNotice('Connection test cancelled.'); return; }
      const failure = timedOut ? new ApiError(kind === 'save' ? 'The save timed out. Its result could not be confirmed.' : 'The provider request timed out. Try again after checking the connection.', { code: 'network_error' }) : cause;
      setError(failure);
      if ((kind === 'save' || kind === 'test') && requiresReadback(failure) || kind === 'reload') {
        setReview(true); onRequireReadback();
      }
    } finally {
      window.clearTimeout(timer);
      if (pending.current === controller) pending.current = undefined;
      if (mounted.current) setOperation(undefined);
    }
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); void perform('save'); }
  function discard() { if (!disabled && confirmAction('Discard unsaved provider changes?')) { setDraft(initialDraft(provider)); setError(undefined); setNotice('Provider draft discarded.'); } }

  return <Dialog open className="provider-configuration-sheet" onClose={close} aria-labelledby={`${formId}-title`} aria-describedby={`${formId}-description`}>
    <form ref={form} id={formId} onSubmit={submit} aria-busy={Boolean(operation)}>
      <DialogTitle id={`${formId}-title`}><span className="provider-configuration-sheet__name" translate="no">{provider.Name}</span><small>{t('Provider configuration')}</small></DialogTitle>
      <DialogContent>
        <p id={`${formId}-description`} className="provider-configuration-sheet__intro">{t('Saved values are never displayed. Keep a value, replace it, clear it, or use the deployment default.')}</p>
        {provider.Id === 'opensubtitles' && <p className="provider-configuration-sheet__intro">{t('Username and password must be configured together. To remove the account, clear both values.')}</p>}
        {!saved.DeploymentEnabled && <p className="providers-page__disabled-note" role="status">{t('Online providers are disabled by the deployment. Saving credentials cannot enable network access.')}</p>}
        {saved.DeploymentEnabled && !saved.Enabled && <p className="providers-page__disabled-note" role="status">{t('Enable internet providers in metadata settings before testing a connection.')}</p>}
        {error != null && <ErrorNotice error={error} />}
        {review && <p className="provider-configuration-sheet__review" role="alert">{t('The saved configuration may have changed. Reload it to review the current state before saving or testing again. No request will be replayed automatically.')}</p>}
        {notice && <p className="provider-configuration-sheet__notice" role="status">{t(notice)}</p>}
        <div className="provider-configuration-sheet__fields">
          {provider.Fields.map((field, index) => {
            const value = draft[field.Id]; const id = `${formId}-${field.Id}`;
            const state = field.Source === 'deployment' ? field.HasValue ? 'Using deployment value' : 'No deployment value' : field.HasValue ? 'Saved override' : 'Explicitly cleared';
            return <section key={field.Id} className="provider-configuration-sheet__field" aria-labelledby={`${id}-label`}>
              <div className="provider-configuration-sheet__field-heading"><h3 id={`${id}-label`}>{t(field.Label)}</h3><span>{t(state)}</span></div>
              <label htmlFor={`${id}-action`}>{t('{field} action', { field: t(field.Label) })}</label>
              <select id={`${id}-action`} value={value.action} disabled={disabled} aria-invalid={Boolean(parsed.errors[field.Id])} aria-describedby={parsed.errors[field.Id] && value.action !== 'replace' ? `${id}-error` : undefined} onChange={(event) => change(field.Id, { action: event.target.value as FieldAction, value: '' })} autoFocus={index === 0}>
                <option value="keep">{t('Keep saved value')}</option><option value="replace">{t('Replace value')}</option><option value="clear">{t('Clear value')}</option><option value="default">{t('Use deployment default')}</option>
              </select>
              {value.action === 'replace' && <>
                <label htmlFor={`${id}-value`}>{t('New {field}', { field: t(field.Label) })}</label>
                <input id={`${id}-value`} type={field.Secret ? 'password' : 'text'} autoComplete="off" spellCheck={false} autoCapitalize="none" value={value.value} disabled={disabled}
                  maxLength={fieldLimit(field)} aria-invalid={Boolean(parsed.errors[field.Id])} aria-describedby={`${id}-help`} onChange={(event) => change(field.Id, { action: 'replace', value: event.target.value })} />
                <p id={`${id}-help`} className={parsed.errors[field.Id] ? 'provider-configuration-sheet__field-error' : ''}>{parsed.errors[field.Id] ? t(parsed.errors[field.Id]) : t('Single line; at most {bytes} UTF-8 bytes.', { bytes: fieldLimit(field) })}</p>
              </>}
              {value.action === 'clear' && <p>{t('Clearing this value can make the provider unavailable.')}</p>}
              {value.action === 'default' && <p>{t('The deployment value will be used for new requests.')}</p>}
              {parsed.errors[field.Id] && value.action !== 'replace' && <p id={`${id}-error`} className="provider-configuration-sheet__field-error">{t(parsed.errors[field.Id])}</p>}
            </section>;
          })}
        </div>
        <section className="provider-configuration-sheet__connection" aria-labelledby={`${formId}-connection`}>
          <div className="provider-configuration-sheet__connection-heading"><h3 id={`${formId}-connection`}>{t('Connection test')}</h3>
            {operation === 'test' ? <button type="button" className="providers-page__action" onClick={() => { cancelledTest.current = true; pending.current?.abort(); }}>{t('Cancel test')}</button>
              : <button type="button" className="providers-page__action" disabled={disabled || dirty || !saved.Enabled || !provider.Configured} onClick={() => void perform('test')}>{t('Test saved configuration')}</button>}
          </div>
          {operation === 'test' && <p role="status" aria-live="polite">{t('Testing the saved provider configuration…')}</p>}
          {dirty && <p>{t('Save or discard the draft before testing. Connection tests use the saved configuration.')}</p>}
          {!dirty && !provider.Configured && <p>{t('Complete and save the required provider values before testing.')}</p>}
          {testResult && <ConfigurationTestResult result={testResult} />}
        </section>
      </DialogContent>
      <DialogActions>
        {review ? <button type="button" className="providers-page__action provider-configuration-sheet__reload" disabled={Boolean(operation)} onClick={() => void perform('reload')}><RefreshIcon />{operation === 'reload' ? t('Reloading…') : t('Reload saved configuration')}</button>
          : <button type="button" className="providers-page__action provider-configuration-sheet__discard" disabled={!dirty || Boolean(operation)} onClick={discard}>{t('Discard changes')}</button>}
        <button type="button" className="providers-page__action" disabled={Boolean(operation)} onClick={close}>{t('Close')}</button>
        <button type="submit" className="providers-page__action providers-page__action--primary" disabled={!dirty || disabled || Object.keys(parsed.errors).length > 0}>{operation === 'save' ? t('Saving…') : t('Save provider')}</button>
      </DialogActions>
    </form>
  </Dialog>;
}

export function ProvidersPage({ embedded = false, onNavigationGuardChange = noGuard }: { embedded?: boolean; onNavigationGuardChange?: UserNavigationGuardChange }) {
  const { t } = useTranslation();
  const headingId = useId();
  const [data, setData] = useState<OnlineProviders>();
  const [configuration, setConfiguration] = useState<ProviderConfiguration>();
  const [error, setError] = useState<unknown>();
  const [configurationError, setConfigurationError] = useState<unknown>();
  const [reviewRequired, setReviewRequired] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingConfiguration, setLoadingConfiguration] = useState(true);
  const [revision, setRevision] = useState(0);
  const [editing, setEditing] = useState<ProviderId>();

  useEffect(() => {
    const controller = new AbortController(); let timedOut = false; let disposed = false;
    const deadline = window.setTimeout(() => { timedOut = true; controller.abort(); }, 20_000);
    setLoading(true); setLoadingConfiguration(true); setError(undefined); setConfigurationError(undefined);
    const failure = (cause: unknown) => timedOut ? new ApiError('Provider settings did not respond in time. Refresh to try again.', { code: 'network_error' }) : cause;
    const statusRequest = providersApi.getProviders({ signal: controller.signal })
      .then((value) => { if (!controller.signal.aborted) setData(value); })
      .catch((cause) => { if (!disposed && (!controller.signal.aborted || timedOut)) { if (!isAbortError(cause) || timedOut) setError(failure(cause)); } })
      .finally(() => { if (!disposed && (!controller.signal.aborted || timedOut)) setLoading(false); });
    const configurationRequest = providersApi.getConfiguration({ signal: controller.signal })
      .then((value) => { if (!controller.signal.aborted) { setConfiguration(value); setReviewRequired(false); } })
      .catch((cause) => { if (!disposed && (!controller.signal.aborted || timedOut)) { if (!isAbortError(cause) || timedOut) setConfigurationError(failure(cause)); } })
      .finally(() => { if (!disposed && (!controller.signal.aborted || timedOut)) setLoadingConfiguration(false); });
    void Promise.allSettled([statusRequest, configurationRequest]).finally(() => window.clearTimeout(deadline));
    return () => { disposed = true; window.clearTimeout(deadline); controller.abort(); };
  }, [revision]);

  const reload = () => { if (!editing) setRevision((value) => value + 1); };
  const Heading = embedded ? 'h3' : 'h1';
  const busy = loading || loadingConfiguration;
  function configurationChanged(value: ProviderConfiguration) {
    setConfiguration(value); setConfigurationError(undefined); setReviewRequired(false);
    setData((current) => current && ({ ...current, Enabled: value.Enabled, Items: current.Items.map((provider) => ({ ...provider, Configured: value.Items.find((entry) => entry.Id === provider.Id)?.Configured ?? provider.Configured })) }));
  }

  return <section className={`providers-page${embedded ? ' providers-page--embedded' : ''}`} aria-labelledby={headingId} aria-busy={busy}>
    <div className="providers-page__heading">
      <div><Heading id={headingId}>{t('Online providers')}</Heading>{!embedded && <p>{t('Manage metadata, image, and subtitle sources for this server.')}</p>}</div>
      <button type="button" className="providers-page__refresh" disabled={busy || Boolean(editing)} onClick={reload}><RefreshIcon />{t('Refresh providers')}</button>
    </div>
    {error != null && <ErrorNotice error={error} retry={reload} />}
    {configurationError != null && <ErrorNotice error={configurationError} retry={reload} />}
    {(configurationError != null && configuration || reviewRequired) && <p className="providers-page__disabled-note" role="status">{t('Showing the last loaded provider state. Reload successfully before editing or testing.')}</p>}
    <p className="providers-page__deployment-note"><svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7v.1" /></svg><span>{t('Provider values are encrypted when saved. Changes apply to new requests without restarting the server.')}</span></p>
    {loading && !data && <div className="providers-page__loading" role="status" aria-label={t('Loading providers…')}>{[0, 1, 2].map((index) => <div key={index} className="providers-page__skeleton" aria-hidden="true" />)}</div>}
    {data && <>
      {configuration && !configuration.DeploymentEnabled ? <p className="providers-page__disabled-note" role="status">{t('Online providers are disabled by the deployment. Saving credentials cannot enable network access.')}</p>
        : !data.Enabled && <p className="providers-page__disabled-note" role="status">{t('Enable internet providers in metadata settings to request online metadata, images, or subtitles.')}</p>}
      {data.Items.length > 0 ? <div className="providers-page__list">{data.Items.map((provider) => {
        const website = providerWebsite(provider.Website);
        const editableProvider = configuration?.Items.find((entry) => entry.Id === provider.Id);
        const enabled = configuration?.Enabled ?? data.Enabled;
        const configured = editableProvider?.Configured ?? provider.Configured;
        const state = !enabled ? 'disabled' : configured ? 'configured' : 'required';
        const stateLabel = !enabled ? 'Disabled' : configured ? 'Configured' : 'Configuration required';
        return <article key={provider.Id} className="providers-page__provider" aria-label={provider.Name}>
          <div className="providers-page__row">
            <div className="providers-page__identity"><h4 translate="no">{provider.Name}</h4><p>{provider.Capabilities.map((capability) => t(capabilityLabels[capability] ?? capability)).join(' · ')}</p></div>
            <div className="providers-page__state" data-state={state}><span><i aria-hidden="true" />{t(stateLabel)}</span><small>{t(editableProvider?.Fields.some((field) => field.Source === 'override') ? 'Using saved provider values' : 'Using deployment defaults')}</small></div>
            <div className="providers-page__row-actions">
              <button type="button" className="providers-page__action" disabled={busy || Boolean(configurationError) || reviewRequired || !editableProvider} aria-label={t('Configure {provider}', { provider: provider.Name })} onClick={() => editableProvider && setEditing(editableProvider.Id)}>{t('Configure')}</button>
              {website && <a className="providers-page__website" href={website} target="_blank" rel="noopener noreferrer" aria-label={`${provider.Name} · ${t('Provider website')}`}><span>{t('Provider website')}</span><ExternalIcon /></a>}
            </div>
          </div>
          {(provider.Attribution || provider.Id === 'tmdb') && <details className="providers-page__credits"><summary>{t('Sources and credits')}</summary><div>{provider.Id === 'tmdb' && <img src={tmdbLogo} alt="TMDB" width="96" height="12" />}{provider.Attribution && <p>{provider.Attribution}</p>}</div></details>}
        </article>;
      })}</div> : <p className="providers-page__empty">{t('No online providers are available.')}</p>}
    </>}
    {editing && configuration && <ProviderConfigurationEditor key={editing} providerId={editing} initial={configuration} onUpdate={configurationChanged} onRequireReadback={() => setReviewRequired(true)} onClose={() => setEditing(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
  </section>;
}
