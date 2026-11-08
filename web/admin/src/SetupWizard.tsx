import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { adminApi, ApiError, clearSession, isAbortError } from './api';
import type { LibrariesResponse, Library, LibraryInput, LibraryResponse, StorageRootsResponse, User } from './api';
import { AuthFrame, SetupProgress } from './AuthPage';
import { ErrorNotice } from './components';
import { CreateLibraryDialog } from './LibrariesPage';
import { useTranslation } from './i18n';
import { parseServerPolicyDraft, serverPolicyApi, serverPolicyDraft } from './serverPolicyApi';
import type { ServerPolicy, ServerPolicyDraft, ServerUICulture } from './serverPolicyApi';
import { setupNeedsReview, setupRequest } from './setupRequest';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import './setup-wizard.css';

type Step = 'library' | 'network' | 'complete';
interface Props { user: User; onComplete: () => void; onInterrupted: (notice: string) => void }
interface LibrarySnapshot { roots?: StorageRootsResponse; libraries?: LibrariesResponse; rootsError?: unknown; librariesError?: unknown; loading: boolean }

function invalidSetupResponse(): never { throw new ApiError('The setup response is incomplete. Read the saved information before continuing.', { code: 'invalid_response' }); }
function validLibrary(value: Library | undefined): value is Library {
  return Boolean(value && typeof value.Id === 'string' && value.Id && typeof value.Name === 'string' && typeof value.CollectionType === 'string'
    && Array.isArray(value.Paths) && value.Paths.every((path) => typeof path === 'string'));
}
function collectionLabel(type: string): string { return ({ movies: 'Movies', tvshows: 'TV shows', music: 'Music', mixed: 'Mixed media' } as Record<string, string>)[type] ?? type; }

export function SetupWizard({ user, onComplete, onInterrupted }: Props) {
  const { t } = useTranslation();
  const [step, setStep] = useState<Step>('library');
  const [libraryVersion, setLibraryVersion] = useState(0);
  const [librarySnapshot, setLibrarySnapshot] = useState<LibrarySnapshot>({ loading: true });
  const [creating, setCreating] = useState(false);
  const [creationUnknown, setCreationUnknown] = useState(false);
  const [selectedLibrary, setSelectedLibrary] = useState<Library>();
  const [creation, setCreation] = useState<LibraryResponse>();
  const [policyVersion, setPolicyVersion] = useState(0);
  const [savedPolicy, setSavedPolicy] = useState<ServerPolicy>();
  const [draft, setDraft] = useState<ServerPolicyDraft>();
  const [policyLoading, setPolicyLoading] = useState(false);
  const [policyReview, setPolicyReview] = useState(false);
  const [policyKept, setPolicyKept] = useState(false);
  const [pending, setPending] = useState<'preview' | 'save' | 'logout' | 'session' | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [logoutReview, setLogoutReview] = useState(false);
  const loadedPolicyVersion = useRef(-1);
  const lifetime = useRef(new AbortController());
  const inFlight = useRef(false);
  const heading = useRef<HTMLHeadingElement>(null);
  const errorSummary = useRef<HTMLDivElement>(null);
  const dialogGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const registerDialogGuard: UserNavigationGuardChange = useCallback((guard) => { dialogGuard.current = guard; }, []);
  const request = useCallback(<T,>(action: (signal: AbortSignal) => Promise<T>) => setupRequest(action, lifetime.current.signal), []);
  const createRequest = useCallback((input: LibraryInput) => request((signal) => adminApi.createLibrary(input, { signal })), [request]);
  const dirty = Boolean(savedPolicy && draft && JSON.stringify(serverPolicyDraft(savedPolicy)) !== JSON.stringify(draft));
  const parsed = useMemo(() => savedPolicy && draft ? parseServerPolicyDraft(draft, savedPolicy) : undefined, [savedPolicy, draft]);
  const busy = pending !== null;
  const blocked = busy || policyLoading || policyReview || Boolean(savedPolicy?.Restart.InProgress);
  useUserDraftNavigation(step === 'network' && dirty, busy, registerDialogGuard, 'Leave setup with unsaved network settings?');

  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    return () => controller.abort();
  }, []);
  useEffect(() => { heading.current?.focus(); }, [step]);
  useEffect(() => { if (error != null) errorSummary.current?.focus(); }, [error]);
  useEffect(() => {
    let disposed = false;
    setLibrarySnapshot({ loading: true });
    void Promise.allSettled([
      request((signal) => adminApi.getStorageRoots({ signal })),
      request((signal) => adminApi.getLibraries({ signal })),
    ]).then(([rootsResult, librariesResult]) => {
      if (disposed || lifetime.current.signal.aborted) return;
      const snapshot: LibrarySnapshot = { loading: false };
      if (rootsResult.status === 'fulfilled') {
        const roots = rootsResult.value;
        if (roots && typeof roots.Configured === 'boolean' && Array.isArray(roots.Items) && roots.Items.every((root) => root && typeof root.Path === 'string' && typeof root.Available === 'boolean')) snapshot.roots = roots;
        else snapshot.rootsError = new ApiError('The setup response is incomplete. Read the saved information before continuing.', { code: 'invalid_response' });
      } else snapshot.rootsError = rootsResult.reason;
      if (librariesResult.status === 'fulfilled') {
        const libraries = librariesResult.value;
        if (libraries && Array.isArray(libraries.Items) && libraries.Items.every(validLibrary) && typeof libraries.TotalRecordCount === 'number' && libraries.TotalRecordCount === libraries.Items.length) snapshot.libraries = libraries;
        else snapshot.librariesError = new ApiError('The setup response is incomplete. Read the saved information before continuing.', { code: 'invalid_response' });
      } else snapshot.librariesError = librariesResult.reason;
      setLibrarySnapshot(snapshot);
    });
    return () => { disposed = true; };
  }, [libraryVersion, request]);
  useEffect(() => {
    if (step !== 'network' || loadedPolicyVersion.current === policyVersion) return;
    let disposed = false;
    setPolicyLoading(true); setError(null);
    void request((signal) => serverPolicyApi.get({ signal })).then((policy) => {
      if (disposed || lifetime.current.signal.aborted) return;
      loadedPolicyVersion.current = policyVersion; setSavedPolicy(policy); setDraft(serverPolicyDraft(policy)); setPolicyReview(false);
    }).catch((cause: unknown) => { if (!disposed && !isAbortError(cause)) { setError(cause); setPolicyReview(true); } })
      .finally(() => { if (!disposed && !lifetime.current.signal.aborted) setPolicyLoading(false); });
    return () => { disposed = true; };
  }, [step, policyVersion, request]);

  function chooseLibrary(library: Library) { setSelectedLibrary(library); setError(null); }
  function created(result: LibraryResponse) {
    if (!result || !validLibrary(result.Library)
      || result.Job !== undefined && (!result.Job || typeof result.Job.Id !== 'string' || !result.Job.Id || result.Job.LibraryId !== result.Library.Id || typeof result.Job.Status !== 'string')
      || result.ScanError !== undefined && (!result.ScanError || typeof result.ScanError.Code !== 'string' || typeof result.ScanError.Message !== 'string')) invalidSetupResponse();
    setCreation(result); setSelectedLibrary(result.Library); setCreating(false);
    setLibrarySnapshot((snapshot) => ({ ...snapshot, libraries: { Items: [...(snapshot.libraries?.Items ?? []).filter((library) => library.Id !== result.Library.Id), result.Library], TotalRecordCount: (snapshot.libraries?.Items.filter((library) => library.Id !== result.Library.Id).length ?? 0) + 1 } }));
  }
  function closeCreate(needsRefresh?: boolean) {
    setCreating(false);
    if (needsRefresh) { setCreationUnknown(true); setLibraryVersion((value) => value + 1); }
  }
  function nextNetwork(skip: boolean) { if (skip) setSelectedLibrary(undefined); setError(null); setStep('network'); }
  function keepPolicy() { if (busy) return; if (savedPolicy) setDraft(serverPolicyDraft(savedPolicy)); setPolicyKept(true); setError(null); setStep('complete'); }
  async function savePolicy(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || blocked || !savedPolicy || !draft) return;
    if (!dirty) { keepPolicy(); return; }
    if (!parsed?.input) return;
    inFlight.current = true; setPending('preview'); setError(null);
    const input = parsed.input;
    let saving = false;
    try {
      const preview = await request((signal) => serverPolicyApi.preview(input, { signal }));
      if (lifetime.current.signal.aborted) return;
      if (preview.WouldDisconnect) { setError(new Error('These settings would disconnect this setup session. Keep the current settings and configure access after signing in from an allowed local network.')); return; }
      saving = true; setPending('save');
      const policy = await request((signal) => serverPolicyApi.update(input, false, { signal }));
      if (lifetime.current.signal.aborted) return;
      setSavedPolicy(policy); setDraft(serverPolicyDraft(policy)); setPolicyKept(false); setStep('complete');
    } catch (cause) {
      if (!isAbortError(cause) && !lifetime.current.signal.aborted) {
        setError(cause);
        if (saving && setupNeedsReview(cause) || cause instanceof ApiError && (cause.status === 409 || cause.code === 'session_changed')) setPolicyReview(true);
      }
    } finally { inFlight.current = false; if (!lifetime.current.signal.aborted) setPending(null); }
  }
  async function finish() {
    if (inFlight.current || logoutReview) return;
    inFlight.current = true; setPending('logout'); setError(null);
    try { await request((signal) => adminApi.logout({ signal })); if (!lifetime.current.signal.aborted) onComplete(); }
    catch (cause) {
      if (!lifetime.current.signal.aborted) {
        if (cause instanceof ApiError && cause.status === 401) { clearSession(); onComplete(); }
        else if (!isAbortError(cause)) { setError(cause); setLogoutReview(true); }
      }
    } finally { inFlight.current = false; if (!lifetime.current.signal.aborted) setPending(null); }
  }
  async function checkSession() {
    if (inFlight.current) return;
    inFlight.current = true; setPending('session'); setError(null);
    try {
      const session = await request((signal) => adminApi.getSession({ signal }));
      if (lifetime.current.signal.aborted) return;
      if (session.User.Id !== user.Id || !session.User.IsAdministrator || session.User.IsDisabled) { clearSession(); onInterrupted('Your session changed. Sign in again to manage the initialized server.'); return; }
      setLogoutReview(false);
    } catch (cause) {
      if (!lifetime.current.signal.aborted) {
        if (cause instanceof ApiError && cause.status === 401) { clearSession(); onComplete(); }
        else if (!isAbortError(cause)) setError(cause);
      }
    } finally { inFlight.current = false; if (!lifetime.current.signal.aborted) setPending(null); }
  }

  const readableRoots = librarySnapshot.roots?.Items.some((root) => root.Available) ?? false;
  const hasLibraryError = librarySnapshot.rootsError != null || librarySnapshot.librariesError != null;
  const libraryOptions = librarySnapshot.libraries?.Items ?? [];
  const title = step === 'library' ? 'Add your first library' : step === 'network' ? 'Remote access' : 'Your server is ready';
  return <AuthFrame><section className="auth-card auth-card-setup setup-card" aria-labelledby="setup-title" aria-busy={busy}>
    <SetupProgress step={step === 'library' ? 1 : step === 'network' ? 2 : 3} />
    <div className="auth-eyebrow">{t('First-run setup')}</div><h1 id="setup-title" ref={heading} tabIndex={-1}>{t(title)}</h1>
    <p className="auth-subtitle">{t(step === 'library' ? 'Choose media directories for your first library. You can add more libraries after signing in.' : step === 'network' ? 'Choose who can connect to your server and the default language for new browsers.' : 'Your administrator account is ready. Sign out of setup, then sign in to open administration.')}</p>
    <div className="setup-body">
      {error != null && <div ref={errorSummary} tabIndex={-1}><ErrorNotice error={error} /></div>}
      {step === 'library' && <>
        {librarySnapshot.loading && <p className="setup-status" role="status"><span className="auth-spinner" aria-hidden="true" />{t('Reading libraries and allowed directories…')}</p>}
        {hasLibraryError && <div className="setup-notice" role="alert"><p>{t('Saved libraries or allowed directories could not be read. Reload them before adding a library.')}</p><button className="setup-inline-action" type="button" disabled={librarySnapshot.loading} onClick={() => setLibraryVersion((value) => value + 1)}>{t('Read saved libraries')}</button></div>}
        {creationUnknown && <div className="setup-notice setup-warning" role="status"><p>{t('Library creation could not be confirmed. Review the saved libraries below. Setup will not send another creation request.')}</p><button className="setup-inline-action" type="button" disabled={librarySnapshot.loading} onClick={() => setLibraryVersion((value) => value + 1)}>{t('Read saved libraries')}</button></div>}
        {!librarySnapshot.loading && !hasLibraryError && !readableRoots && <div className="setup-notice" role="status"><p>{t('No readable media directories are available. Check the media mounts in your deployment, or add a library later.')}</p></div>}
        {libraryOptions.length > 0 && <ul className="setup-library-list" aria-label={t('Saved libraries')}>{libraryOptions.map((library) => <li key={library.Id} data-selected={selectedLibrary?.Id === library.Id || undefined}><div><strong translate="no">{library.Name}</strong><span>{t(collectionLabel(library.CollectionType))}</span>{library.Paths.map((path) => <code key={path} translate="no">{path}</code>)}</div><button type="button" aria-pressed={selectedLibrary?.Id === library.Id} aria-label={t('Use {name}', { name: library.Name })} disabled={librarySnapshot.loading} onClick={() => chooseLibrary(library)}>{t(selectedLibrary?.Id === library.Id ? 'Selected' : 'Use library')}</button></li>)}</ul>}
        {creation && selectedLibrary?.Id === creation.Library.Id && <p className="setup-notice" role="status">{t(creation.ScanError ? 'The library was created, but its initial scan was not started. Start a scan from Libraries after signing in.' : creation.Job ? 'The library was created and its initial scan was requested. Check Tasks after signing in for scan progress.' : 'The library was created. You can start its first scan from Libraries after signing in.')}</p>}
        <button type="button" className="setup-add-library" disabled={librarySnapshot.loading || hasLibraryError || !readableRoots || creationUnknown} onClick={() => setCreating(true)}><svg aria-hidden="true" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7"><path d="M12 5v14M5 12h14" /></svg>{t('Add library')}</button>
        <p className="setup-help">{t('Directories remain limited to the media roots approved by your deployment.')}</p>
        <div className="setup-actions">{selectedLibrary ? <button className="auth-submit" type="button" onClick={() => nextNetwork(false)}>{t('Continue')}</button> : <button className="setup-secondary" type="button" onClick={() => nextNetwork(true)}>{t('Skip for now')}</button>}</div>
      </>}
      {step === 'network' && <form className="setup-network-form" onSubmit={(event) => void savePolicy(event)} aria-busy={busy || policyLoading}>
        {policyLoading && <p className="setup-status" role="status"><span className="auth-spinner" aria-hidden="true" />{t('Loading network policy')}</p>}
        {savedPolicy?.Restart.InProgress && <p className="setup-notice" role="status">{t('A server restart is in progress. Keep the current settings or reload them after reconnecting.')}</p>}
        {policyReview && <div className="setup-notice setup-warning" role="status"><p>{t('The saved policy must be read again before another change. Your draft has not been resubmitted.')}</p><button type="button" className="setup-inline-action" disabled={busy || policyLoading} onClick={() => setPolicyVersion((value) => value + 1)}>{t('Reload saved settings')}</button></div>}
        {draft && savedPolicy && <>
          <label className="setup-switch-row" htmlFor="setup-remote"><span><strong id="setup-remote-label">{t('Allow remote access')}</strong><small id="setup-remote-help">{t('When disabled, only local networks can connect.')}</small></span><input id="setup-remote" type="checkbox" role="switch" aria-labelledby="setup-remote-label" aria-describedby="setup-remote-help" checked={draft.enableRemoteAccess} disabled={blocked} onChange={(event) => { setError(null); setDraft({ ...draft, enableRemoteAccess: event.target.checked }); }} /></label>
          <label className="setup-field" htmlFor="setup-lan"><span id="setup-lan-label">{t('Local network subnets')}</span><textarea id="setup-lan" rows={3} value={draft.localNetworkSubnets} disabled={blocked} autoComplete="off" spellCheck={false} maxLength={2200} aria-labelledby="setup-lan-label" aria-invalid={Boolean(parsed?.errors.LocalNetworkSubnets)} aria-describedby="setup-lan-help" onChange={(event) => { setError(null); setDraft({ ...draft, localNetworkSubnets: event.target.value }); }} /><small id="setup-lan-help" className={parsed?.errors.LocalNetworkSubnets ? 'setup-field-error' : ''}>{t(parsed?.errors.LocalNetworkSubnets ?? 'One network CIDR per line, up to 32. Loopback is always local. The server verifies canonical network addresses.')}</small></label>
          <label className="setup-field" htmlFor="setup-culture"><span id="setup-culture-label">{t('Default interface language')}</span><select id="setup-culture" value={draft.uiCulture} disabled={blocked} aria-labelledby="setup-culture-label" aria-describedby="setup-culture-help" onChange={(event) => { setError(null); setDraft({ ...draft, uiCulture: event.target.value as ServerUICulture }); }}><option value="zh-CN">{t('Simplified Chinese')}</option><option value="en-US">English</option></select><small id="setup-culture-help">{t("New browsers use this default. A person's saved language preference takes priority and is not changed.")}</small></label>
          <p className="setup-connection">{t('This connection')}: <code translate="no">{savedPolicy.Client.Address ?? '—'}</code><span>{t(savedPolicy.Client.IsLocal ? 'Local network' : 'Remote or unknown network')}</span></p>
          <p className="setup-help">{t('Remote access also requires your deployment and network to allow connections.')}</p>
        </>}
        <div className="setup-actions"><button className="setup-secondary" type="button" disabled={busy} onClick={() => { setError(null); setStep('library'); }}>{t('Back')}</button>{savedPolicy && draft && !policyReview && <button className="auth-submit" type="submit" disabled={blocked || Boolean(parsed && !parsed.input)}>{busy && <span className="auth-spinner" aria-hidden="true" />}{t(pending === 'preview' ? 'Checking access…' : pending === 'save' ? 'Saving network policy…' : dirty ? 'Save and continue' : 'Continue')}</button>}</div>
        {(dirty || policyReview || savedPolicy?.Restart.InProgress || !savedPolicy && !policyLoading) && <button className="setup-inline-action setup-keep-policy" type="button" disabled={busy || policyLoading} onClick={keepPolicy}>{t('Keep current settings')}</button>}
      </form>}
      {step === 'complete' && <>
        <dl className="setup-summary"><div><dt>{t('Administrator')}</dt><dd translate="no">{user.Name}</dd></div><div><dt>{t('Media library')}</dt><dd>{selectedLibrary ? <span translate="no">{selectedLibrary.Name}</span> : t('Add a library after signing in')}</dd></div><div><dt>{t('Remote access')}</dt><dd>{t(policyKept ? 'Current settings kept' : savedPolicy?.EnableRemoteAccess ? 'Allowed' : 'Local networks only')}</dd></div></dl>
        <p className="setup-help">{t('You can manage libraries, scanning, users, and network settings from administration.')}</p>
        {logoutReview && <div className="setup-notice setup-warning" role="status"><p>{t('Finishing setup could not be confirmed. Check your session before trying again.')}</p><button className="setup-inline-action" type="button" disabled={busy} onClick={() => void checkSession()}>{t(pending === 'session' ? 'Checking session…' : 'Check session')}</button></div>}
        <div className="setup-actions"><button className="setup-secondary" type="button" disabled={busy || logoutReview} onClick={() => { setError(null); setStep('network'); }}>{t('Back')}</button><button className="auth-submit" type="button" disabled={busy || logoutReview} onClick={() => void finish()}>{busy && <span className="auth-spinner" aria-hidden="true" />}{t(pending === 'logout' ? 'Finishing setup…' : 'Finish setup')}</button></div>
      </>}
    </div>
    <p className="setup-footer">{t('Setup credentials are used only for this session. You can resume management by signing in if setup is interrupted.')}</p>
  </section>{creating && librarySnapshot.roots && <CreateLibraryDialog roots={librarySnapshot.roots} onClose={closeCreate} onCreated={created} onNavigationGuardChange={registerDialogGuard} createRequest={createRequest} />}</AuthFrame>;
}
