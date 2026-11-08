import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { confirmAction, useTranslation } from './i18n';
import { RefreshRounded, RestartAltRounded, SaveOutlined } from './icons';
import { SettingsGroup } from './SettingsLayout';
import { ServerUpdatePanel } from './ServerUpdatePanel';
import { parseServerPolicyDraft, serverPolicyApi, serverPolicyDraft } from './serverPolicyApi';
import type { ServerPolicy, ServerPolicyDraft, ServerPolicyInput, ServerUICulture } from './serverPolicyApi';
import { Alert, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Skeleton, TextField } from './ui';
import './server-policy.css';

export interface ServerPolicyState { dirty: boolean; busy: boolean }
interface Props {
  disabled?: boolean;
  onStateChange?: (state: ServerPolicyState) => void;
}
type Pending = 'preview' | 'save' | 'restart' | null;
type Confirmation = { kind: 'disconnect'; input: ServerPolicyInput } | { kind: 'restart' } | null;
const requestTimeout = 15000;

function needsReview(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
}

export function ServerPolicyFields({ disabled = false, onStateChange }: Props) {
  const { t, locale } = useTranslation();
  const [saved, setSaved] = useState<ServerPolicy>();
  const [draft, setDraft] = useState<ServerPolicyDraft>();
  const [loadVersion, setLoadVersion] = useState(0);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<Pending>(null);
  const [confirmation, setConfirmation] = useState<Confirmation>(null);
  const [restartPhrase, setRestartPhrase] = useState('');
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [stale, setStale] = useState(false);
  const [notice, setNotice] = useState('');
  const [restartRequested, setRestartRequested] = useState(false);
  const [disconnectExpected, setDisconnectExpected] = useState(false);
  const mounted = useRef(true);
  const inFlight = useRef(false);
  const mutation = useRef<AbortController | undefined>(undefined);
  const stateChange = useRef(onStateChange);
  stateChange.current = onStateChange;
  const dirty = Boolean(saved && draft && JSON.stringify(serverPolicyDraft(saved)) !== JSON.stringify(draft));
  const busy = pending !== null || confirmation !== null;
  const parsed = useMemo(() => saved && draft ? parseServerPolicyDraft(draft, saved) : undefined, [saved, draft]);
  const blocked = disabled || loading || busy || review || stale || restartRequested || disconnectExpected || Boolean(saved?.Restart.InProgress);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; mutation.current?.abort(); stateChange.current?.({ dirty: false, busy: false }); };
  }, []);
  useEffect(() => { stateChange.current?.({ dirty, busy: loading || busy }); }, [dirty, busy, loading]);
  useEffect(() => {
    const controller = new AbortController();
    let disposed = false;
    let timedOut = false;
    const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, requestTimeout);
    setLoading(true);
    setError(null);
    setNotice('');
    void serverPolicyApi.get({ signal: controller.signal }).then((value) => {
      if (disposed) return;
      setSaved(value);
      setDraft(serverPolicyDraft(value));
      setReview(false);
      setStale(false);
      setNotice('');
      setRestartRequested(false);
      setDisconnectExpected(false);
    }).catch((cause: unknown) => {
      if (!disposed && (timedOut || !isAbortError(cause))) {
        setStale(true);
        setError(timedOut ? new ApiError('Server settings did not respond within 15 seconds. Reload to try again.', { code: 'network_error' }) : cause);
      }
    }).finally(() => { clearTimeout(deadline); if (!disposed) setLoading(false); });
    return () => { disposed = true; clearTimeout(deadline); controller.abort(); };
  }, [loadVersion]);

  const request = useCallback(async <T,>(action: (signal: AbortSignal) => Promise<T>): Promise<T> => {
    const controller = new AbortController();
    mutation.current = controller;
    let timedOut = false;
    const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, requestTimeout);
    try { return await action(controller.signal); }
    catch (cause) {
      if (timedOut) throw new ApiError('The request timed out. Its result could not be confirmed.', { code: 'network_error' });
      throw cause;
    } finally { clearTimeout(deadline); if (mutation.current === controller) mutation.current = undefined; }
  }, []);

  function change(patch: Partial<ServerPolicyDraft>) {
    if (blocked) return;
    setDraft((value) => value ? { ...value, ...patch } : value);
    setError(null);
    setNotice('');
  }
  function reload() {
    if (inFlight.current || busy || disabled) return;
    if (dirty && !confirmAction('Discard your network policy draft and reload the saved settings?')) return;
    setConfirmation(null);
    setLoadVersion((value) => value + 1);
  }
  function discard() {
    if (blocked || !saved) return;
    setDraft(serverPolicyDraft(saved));
    setError(null);
    setNotice('');
  }
  function closeConfirmation() {
    if (inFlight.current) return;
    setConfirmation(null);
    setRestartPhrase('');
  }
  async function commit(input: ServerPolicyInput, confirmDisconnect: boolean) {
    setPending('save');
    try {
      const value = await request((signal) => serverPolicyApi.update(input, confirmDisconnect, { signal }));
      if (!mounted.current) return;
      setSaved(value);
      setDraft(serverPolicyDraft(value));
      setConfirmation(null);
      setDisconnectExpected(Boolean(value.DisconnectExpected));
      setNotice(value.DisconnectExpected ? 'The settings were saved. This connection is no longer allowed. Reconnect from a local network.' : 'Network policy and default language saved.');
    } catch (cause) {
      if (!mounted.current) return;
      setConfirmation(null);
      setError(cause);
      if (needsReview(cause)) setReview(true);
    }
  }
  async function save() {
    if (inFlight.current || blocked || !dirty || !parsed?.input) return;
    inFlight.current = true;
    setPending('preview');
    setError(null);
    setNotice('');
    const input = parsed.input;
    try {
      const preview = await request((signal) => serverPolicyApi.preview(input, { signal }));
      if (!mounted.current) return;
      if (preview.WouldDisconnect) setConfirmation({ kind: 'disconnect', input });
      else await commit(input, false);
    } catch (cause) {
      if (!mounted.current) return;
      setError(cause);
      // Preview has no side effects; conflicts still require a fresh saved revision.
      if (cause instanceof ApiError && (cause.status === 409 || cause.code === 'session_changed')) setReview(true);
    } finally { inFlight.current = false; if (mounted.current) setPending(null); }
  }
  async function confirmDisconnect() {
    if (inFlight.current || disabled || confirmation?.kind !== 'disconnect') return;
    inFlight.current = true;
    setError(null);
    try { await commit(confirmation.input, true); }
    finally { inFlight.current = false; if (mounted.current) setPending(null); }
  }
  async function restart() {
    if (inFlight.current || disabled || confirmation?.kind !== 'restart' || restartPhrase !== 'RESTART' || !saved?.Restart.Available) return;
    inFlight.current = true;
    setPending('restart');
    setError(null);
    setNotice('');
    try {
      await request((signal) => serverPolicyApi.restart(saved.Revision, { signal }));
      if (mounted.current) { setConfirmation(null); setRestartPhrase(''); setRestartRequested(true); }
    } catch (cause) {
      if (mounted.current) { setConfirmation(null); setError(cause); if (needsReview(cause)) setReview(true); }
    } finally { inFlight.current = false; if (mounted.current) setPending(null); }
  }

  const networkError = parsed?.errors.LocalNetworkSubnets ?? fieldError(error, 'LocalNetworkSubnets');
  const languageError = parsed?.errors.UICulture ?? fieldError(error, 'UICulture');
  return <div className="server-policy" aria-busy={loading || pending !== null}>
    {loading && !saved && <Skeleton variant="rounded" height={230} aria-label="Loading network policy" />}
    {error != null && <ErrorNotice error={error} retry={!saved && !loading ? reload : undefined} />}
    {stale && saved && <Alert severity="warning">Saved settings could not be refreshed. The last confirmed values and your draft are shown. Reload before editing.</Alert>}
    {review && <Alert severity="warning">The settings changed or the result could not be confirmed. Your draft is kept. Reload saved settings before trying another change.</Alert>}
    {notice && <Alert severity={disconnectExpected ? 'warning' : 'success'}>{notice}</Alert>}
    {restartRequested && <Alert severity="info">Restart requested. The connection may close. Reconnect after the server starts.</Alert>}
    {saved && draft && <>
      <SettingsGroup title="Remote access and local networks" description="This policy applies to the whole server, including active connections. Only the server's trusted proxy configuration is used to identify a client.">
        <div className="server-policy-row">
          <label htmlFor="server-policy-remote"><strong id="server-policy-remote-label">{t('Allow remote access')}</strong><span id="server-policy-remote-help">{t('When disabled, only local networks can connect.')}</span></label>
          <span className="server-policy-switch"><input id="server-policy-remote" type="checkbox" role="switch" aria-labelledby="server-policy-remote-label" aria-describedby="server-policy-remote-help" checked={draft.enableRemoteAccess} disabled={blocked} onChange={(event) => change({ enableRemoteAccess: event.target.checked })} /><span aria-hidden="true" /></span>
        </div>
        <TextField id="server-policy-lan" label="Local network subnets" fullWidth multiline minRows={3} maxRows={8} value={draft.localNetworkSubnets} disabled={blocked}
          onChange={(event) => change({ localNetworkSubnets: event.target.value })} error={Boolean(networkError)}
          helperText={networkError ?? 'One network CIDR per line, up to 32. Loopback is always local. The server verifies canonical network addresses.'}
          slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false, maxLength: 2200 } }} />
        <dl className="server-policy-client">
          <div><dt>{t('This connection')}</dt><dd><code translate="no">{saved.Client.Address ?? '—'}</code><span className={'server-policy-status' + (saved.Client.IsLocal ? ' is-local' : '')}>{t(saved.Client.IsLocal ? 'Local network' : 'Remote or unknown network')}</span></dd></div>
          <div><dt>{t('Last saved')}</dt><dd><time dateTime={saved.UpdatedAt}>{new Date(saved.UpdatedAt).toLocaleString(locale)}</time></dd></div>
        </dl>
      </SettingsGroup>
      <SettingsGroup title="Default interface language" description="New browsers use this default. A person's saved language preference takes priority and is not changed.">
        <label className="settings-native-field" htmlFor="server-policy-language"><span>{t('Default interface language')}</span><select id="server-policy-language" value={draft.uiCulture} disabled={blocked} aria-invalid={Boolean(languageError)} aria-describedby={languageError ? 'server-policy-language-error' : undefined} onChange={(event) => change({ uiCulture: event.target.value as ServerUICulture })}><option value="zh-CN">{t('Simplified Chinese')}</option><option value="en-US">English</option></select></label>
        {languageError && <p id="server-policy-language-error" className="server-policy-field-error" role="alert">{t(languageError)}</p>}
      </SettingsGroup>
      <div className="server-policy-actions" role="group" aria-label={t('Network policy actions')}>
        <span role="status" aria-live="polite">{t(loading ? 'Reloading saved network policy…' : dirty ? 'Unsaved network policy changes' : 'Network policy is saved')}</span>
        <Button type="button" size="small" onClick={reload} disabled={disabled || loading || busy} startIcon={<RefreshRounded />}>Reload saved settings</Button>
        {dirty && <Button type="button" size="small" onClick={discard} disabled={blocked}>Discard changes</Button>}
        <Button type="button" variant="contained" size="small" onClick={() => void save()} disabled={blocked || !dirty || !parsed?.input} startIcon={pending === 'save' || pending === 'preview' ? <CircularProgress size={15} /> : <SaveOutlined />}>{t(pending === 'preview' ? 'Checking access…' : pending === 'save' ? 'Saving network policy…' : 'Save network policy')}</Button>
      </div>
      <SettingsGroup title="Server maintenance" description="Restart is available only when this deployment supplies a safe restart operation. Updates are managed through the Docker deployment.">
        <div className="server-policy-row server-policy-maintenance-row"><div><strong>{t('Restart server')}</strong><span>{t(saved.Restart.InProgress ? 'A restart is already in progress.' : saved.Restart.Available ? 'Active playback and connections will be interrupted.' : 'This deployment has not enabled a safe restart operation.')}</span>{dirty && <span>{t('Save or discard network changes before restarting.')}</span>}</div><Button type="button" variant="outlined" color="error" startIcon={<RestartAltRounded />} disabled={blocked || dirty || !saved.Restart.Available} onClick={() => { setRestartPhrase(''); setConfirmation({ kind: 'restart' }); }}>Restart server</Button></div>
      </SettingsGroup>
      <ServerUpdatePanel disabled={blocked} />
    </>}
    <Dialog open={confirmation?.kind === 'disconnect'} data-layout="modal" maxWidth="xs" onClose={closeConfirmation} aria-labelledby="server-policy-disconnect-title" aria-describedby="server-policy-disconnect-description">
      <DialogTitle id="server-policy-disconnect-title">Disconnect this connection?</DialogTitle>
      <DialogContent><p id="server-policy-disconnect-description" className="server-policy-dialog-copy">{t('These changes will block your current connection and may prevent you from signing in again from this network. Make sure you can reconnect from an allowed local network.')}</p><Alert severity="warning">The new policy also applies to active remote playback and connections.</Alert></DialogContent>
      <DialogActions><Button type="button" onClick={closeConfirmation} disabled={pending !== null} autoFocus>Keep editing</Button><Button type="button" color="error" variant="contained" onClick={() => void confirmDisconnect()} disabled={disabled || pending !== null}>{t(pending === 'save' ? 'Saving network policy…' : 'Save and disconnect')}</Button></DialogActions>
    </Dialog>
    <Dialog open={confirmation?.kind === 'restart'} data-layout="modal" maxWidth="xs" onClose={closeConfirmation} aria-labelledby="server-policy-restart-title" aria-describedby="server-policy-restart-description">
      <DialogTitle id="server-policy-restart-title">Restart server?</DialogTitle>
      <DialogContent><p id="server-policy-restart-description" className="server-policy-dialog-copy">{t('Active playback and connections will be interrupted. The server must finish shutting down before it can start again.')}</p><label className="server-policy-confirm-phrase" htmlFor="server-policy-restart-confirm"><span>{t('Type the confirmation word')} <code translate="no">RESTART</code></span><input id="server-policy-restart-confirm" value={restartPhrase} onChange={(event) => setRestartPhrase(event.target.value)} disabled={pending !== null} autoComplete="off" spellCheck={false} maxLength={7} autoFocus /></label></DialogContent>
      <DialogActions><Button type="button" onClick={closeConfirmation} disabled={pending !== null}>Cancel</Button><Button type="button" color="error" variant="contained" onClick={() => void restart()} disabled={disabled || pending !== null || restartPhrase !== 'RESTART'}>{t(pending === 'restart' ? 'Requesting restart…' : 'Request restart')}</Button></DialogActions>
    </Dialog>
  </div>;
}
