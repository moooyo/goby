import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { adminApi, ApiError, clearSession, isAbortError } from './api';
import type { SessionResponse } from './api';
import { useAdminPreferences, useTranslation } from './i18n';
import { setupNeedsReview, setupRequest } from './setupRequest';
import './auth.css';
import './setup-wizard.css';

type AuthPageProps = {
  mode: 'setup' | 'login';
  initialName?: string;
  notice?: string;
  onSession: (session: SessionResponse) => void;
  onSetupSession: (session: SessionResponse) => void;
  onSetupInterrupted: (notice: string) => void;
  onReload: () => void;
};

function fieldError(error: unknown, name: string): string | undefined {
  if (!(error instanceof ApiError) || !error.fields) return undefined;
  const fields = error.fields as Record<string, unknown>;
  const value = fields[name] ?? fields[name.toLowerCase()];
  if (typeof value === 'string') return value;
  if (Array.isArray(value)) return value.filter((entry) => typeof entry === 'string').join(' ');
  return undefined;
}

function AuthGlyph({ name }: { name: 'sun' | 'moon' | 'eye' | 'eye-off' | 'alert' }) {
  return <svg aria-hidden="true" viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
    {name === 'sun' && <><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42" /></>}
    {name === 'moon' && <path d="M20.8 13A9 9 0 0 1 11 3.2 9 9 0 1 0 20.8 13Z" />}
    {(name === 'eye' || name === 'eye-off') && <><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" /><circle cx="12" cy="12" r="3" />{name === 'eye-off' && <path d="m3 3 18 18" />}</>}
    {name === 'alert' && <><path d="M10.3 4.2 2.6 17.5a2 2 0 0 0 1.7 3h15.4a2 2 0 0 0 1.7-3L13.7 4.2a2 2 0 0 0-3.4 0Z" /><path d="M12 9v4m0 4h.01" /></>}
  </svg>;
}

export function AuthFrame({ children }: { children: ReactNode }) {
  const { language, theme, setLanguage, setTheme } = useAdminPreferences();
  const { t } = useTranslation();
  const zh = language === 'zh';
  const themeLabel = theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme';
  return <main className="auth-page">
    <div className="auth-brand">
      <svg aria-hidden="true" viewBox="0 0 44 44" width="28" height="28"><rect width="44" height="44" rx="13" fill="var(--acc)" /><path d="M29.5 15.7a10 10 0 1 0 1.1 11.1V21H22v3h5.4v1.8a6.5 6.5 0 1 1-.4-8Z" fill="var(--on-acc)" /></svg>
      <strong>Goby</strong><span>{t('Server administration')}</span>
    </div>
    <div className="auth-preferences" aria-label={t('Interface preferences')}>
      <button type="button" className="auth-theme-button" aria-label={t(themeLabel)} title={t(themeLabel)} onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}><AuthGlyph name={theme === 'dark' ? 'sun' : 'moon'} /></button>
      <span className="auth-language-control" role="group" aria-label={t('Language')}>
        <button type="button" lang="zh-CN" aria-pressed={zh} onClick={() => setLanguage('zh')}>中</button>
        <button type="button" lang="en" aria-pressed={!zh} onClick={() => setLanguage('en')}>EN</button>
      </span>
    </div>
    {children}
  </main>;
}

export function SetupProgress({ step }: { step: number }) {
  const { t } = useTranslation();
  return <ol className="setup-progress" aria-label={t('First-run setup progress')}>{['Administrator', 'Media library', 'Remote access', 'Ready'].map((label, index) => <li key={label} aria-current={index === step ? 'step' : undefined} data-complete={index < step || undefined}><span aria-hidden="true">{index < step ? <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="1.7"><path d="m3 8 3 3 7-7" /></svg> : index + 1}</span><strong>{t(label)}</strong></li>)}</ol>;
}

export function AuthPage({ mode, initialName = '', notice, onSession, onSetupSession, onSetupInterrupted, onReload }: AuthPageProps) {
  const setup = mode === 'setup';
  const { language } = useAdminPreferences();
  const { t } = useTranslation();
  const zh = language === 'zh';
  const copy = zh ? {
    title: '登录 Goby', setupTitle: '设置你的服务器', server: '服务器管理',
    username: '用户名', administrator: '管理员名称', password: '密码', token: '设置令牌',
    tokenHint: '输入服务器部署时提供的一次性设置令牌。', passwordHint: '为管理员设置一个独立的安全密码。',
    setupDescription: '创建第一个管理员账户，随后添加媒体库并设置远程访问。',
    signIn: '登录', create: '创建管理员', signingIn: '正在登录…', creating: '正在创建管理员…',
    adminOnly: '仅管理员可登录', setupFoot: '首次设置', check: '已完成设置？检查服务器',
    showPassword: '显示密码', hidePassword: '隐藏密码', light: '切换到浅色主题', dark: '切换到深色主题',
    failure: '请求未能完成，请重试。', requestId: '请求 ID',
  } : {
    title: 'Sign in to Goby', setupTitle: 'Set up your server', server: 'Server administration',
    username: 'Username', administrator: 'Administrator username', password: 'Password', token: 'Setup token',
    tokenHint: 'Enter the one-time token from your server deployment.', passwordHint: 'Use a unique password for this administrator account.',
    setupDescription: 'Create the first administrator, then add a library and choose remote access.',
    signIn: 'Sign in', create: 'Create administrator', signingIn: 'Signing in…', creating: 'Creating administrator…',
    adminOnly: 'Administrator accounts only', setupFoot: 'First-run setup', check: 'Already set up? Check server',
    showPassword: 'Show password', hidePassword: 'Hide password', light: 'Switch to light theme', dark: 'Switch to dark theme',
    failure: 'The request could not be completed. Please try again.', requestId: 'Request ID',
  };
  const [name, setName] = useState(initialName);
  const [password, setPassword] = useState('');
  const [setupToken, setSetupToken] = useState('');
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [bootstrapUnknown, setBootstrapUnknown] = useState(false);
  const [setupInitialized, setSetupInitialized] = useState<boolean>();
  const inFlight = useRef(false);
  const lifetime = useRef(new AbortController());
  const errorSummary = useRef<HTMLDivElement>(null);
  const tokenError = fieldError(error, 'SetupToken');
  const nameError = fieldError(error, 'Name');
  const passwordError = fieldError(error, 'Password');

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, []);

  function clearCredentials() { setSetupToken(''); setPassword(''); setVisible(false); }

  async function checkSetup() {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setError(null);
    try {
      const value = await setupRequest((signal) => adminApi.getBootstrap({ signal }), lifetime.current.signal);
      if (!value || typeof value.Initialized !== 'boolean') throw new ApiError('The setup response is incomplete. Read the saved information before continuing.', { code: 'invalid_response' });
      if (!lifetime.current.signal.aborted) setSetupInitialized(value.Initialized);
    } catch (cause) { if (!isAbortError(cause) && !lifetime.current.signal.aborted) setError(cause); }
    finally { inFlight.current = false; if (!lifetime.current.signal.aborted) setBusy(false); }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || bootstrapUnknown) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      if (setup) {
        const credentials = { Name: name.trim(), Password: password };
        const result = await setupRequest((signal) => adminApi.bootstrap({ SetupToken: setupToken, ...credentials }, { signal }), lifetime.current.signal);
        if (!result?.User || typeof result.User.Id !== 'string' || !result.User.Id || typeof result.User.Name !== 'string') throw new ApiError('The server returned an invalid response. Please try again.', { code: 'invalid_response' });
        const login = setupRequest((signal) => adminApi.login(credentials, { signal }), lifetime.current.signal);
        credentials.Password = '';
        clearCredentials();
        try {
          const session = await login;
          if (session.User.Id !== result.User.Id || !session.User.IsAdministrator || session.User.IsDisabled) throw new ApiError('Your session changed. Please try the action again.', { code: 'session_changed' });
          if (!lifetime.current.signal.aborted) onSetupSession(session);
        } catch (cause) {
          if (lifetime.current.signal.aborted) return;
          if (setupNeedsReview(cause)) {
            try {
              const session = await setupRequest((signal) => adminApi.getSession({ signal }), lifetime.current.signal);
              if (!lifetime.current.signal.aborted && session.User.Id === result.User.Id && session.User.IsAdministrator && !session.User.IsDisabled) { onSetupSession(session); return; }
            } catch { /* An explicit sign-in can recover the initialized server without repeating bootstrap. */ }
          }
          if (!lifetime.current.signal.aborted) {
            clearSession();
            onSetupInterrupted('Your administrator account was created. Sign in to manage your libraries and finish configuration.');
          }
        }
      } else {
        const result = await adminApi.login({ Name: name.trim(), Password: password });
        setPassword('');
        setVisible(false);
        onSession(result);
      }
    } catch (cause) {
      if (!isAbortError(cause) && !lifetime.current.signal.aborted) {
        if (setup && cause instanceof ApiError && cause.status === 409) {
          clearCredentials();
          onSetupInterrupted('This server has already been initialized. Sign in with an administrator account.');
          return;
        }
        if (setup && setupNeedsReview(cause)) { clearCredentials(); setBootstrapUnknown(true); }
        setError(cause);
        requestAnimationFrame(() => errorSummary.current?.focus());
      }
    } finally {
      inFlight.current = false;
      if (!lifetime.current.signal.aborted) setBusy(false);
    }
  }

  return <AuthFrame>
    <section className={`auth-card${setup ? ' auth-card-setup' : ''}`} aria-labelledby="auth-title">
      {setup && <SetupProgress step={0} />}
      {setup && <div className="auth-eyebrow">{copy.setupFoot}</div>}
      <h1 id="auth-title">{setup ? copy.setupTitle : copy.title}</h1>
      <p className="auth-subtitle">{setup ? copy.setupDescription : copy.server}</p>
      <form className="auth-form" onSubmit={submit} aria-busy={busy}>
        {notice && <div className="auth-notice" role="status">{t(notice)}</div>}
        {error != null && <div className="auth-error-summary" role="alert" tabIndex={-1} ref={errorSummary}>
          <AuthGlyph name="alert" />
          <div>
            <p>{t(error instanceof Error ? error.message : copy.failure)}</p>
            {error instanceof ApiError && error.requestId && <p className="auth-request-id">{copy.requestId}: {error.requestId}</p>}
          </div>
        </div>}
        {bootstrapUnknown && <div className="auth-notice" role="status"><div><p>{t('Administrator creation could not be confirmed. Check the server status or sign in with the account you entered. Do not submit setup again.')}</p>{setupInitialized !== undefined && <p>{t(setupInitialized ? 'The server reports that initial setup is complete. Sign in to continue.' : 'The server still reports initial setup. Wait and check again before refreshing this page.')}</p>}<button type="button" className="setup-inline-action" disabled={busy} onClick={() => onSetupInterrupted('Administrator creation could not be confirmed. Sign in with the account you entered, then check your libraries.')}>{t('Sign in to continue')}</button></div></div>}
        {setup && <div className="auth-field">
          <label htmlFor="setup-token">{copy.token}</label>
          <input id="setup-token" name="SetupToken" type="password" required autoFocus autoComplete="off" value={setupToken} onChange={(event) => setSetupToken(event.target.value)} disabled={busy || bootstrapUnknown} aria-invalid={Boolean(tokenError)} aria-describedby="setup-token-hint" />
          <span id="setup-token-hint" className={tokenError ? 'auth-field-error' : 'auth-field-hint'}>{t(tokenError ?? copy.tokenHint)}</span>
        </div>}
        <div className="auth-field">
          <label htmlFor="account-name">{setup ? copy.administrator : copy.username}</label>
          <input id="account-name" name="Name" type="text" required autoFocus={!setup} autoComplete="username" autoCapitalize="none" spellCheck={false} value={name} onChange={(event) => setName(event.target.value)} disabled={busy || bootstrapUnknown} aria-invalid={Boolean(nameError)} aria-describedby={nameError ? 'account-name-error' : undefined} />
          {nameError && <span id="account-name-error" className="auth-field-error">{t(nameError)}</span>}
        </div>
        <div className="auth-field">
          <label htmlFor="account-password">{copy.password}</label>
          <div className="auth-password-control">
            <input id="account-password" name="Password" type={visible ? 'text' : 'password'} required autoComplete={setup ? 'new-password' : 'current-password'} value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy || bootstrapUnknown} aria-invalid={Boolean(passwordError)} aria-describedby={passwordError || setup ? 'account-password-hint' : undefined} />
            <button type="button" className="auth-password-toggle" aria-label={visible ? copy.hidePassword : copy.showPassword} aria-pressed={visible} disabled={busy || bootstrapUnknown} onClick={() => setVisible((value) => !value)}><AuthGlyph name={visible ? 'eye-off' : 'eye'} /></button>
          </div>
          {(passwordError || setup) && <span id="account-password-hint" className={passwordError ? 'auth-field-error' : 'auth-field-hint'}>{t(passwordError ?? copy.passwordHint)}</span>}
        </div>
        <button type="submit" className="auth-submit" disabled={busy || bootstrapUnknown || !name.trim() || !password || (setup && !setupToken)}>
          {busy && <span className="auth-spinner" aria-hidden="true" />}
          {busy ? setup ? copy.creating : copy.signingIn : setup ? copy.create : copy.signIn}
        </button>
      </form>
      <div className="auth-card-footer">
        <span>{copy.adminOnly}</span>
        {setup && <button type="button" disabled={busy} onClick={bootstrapUnknown ? () => void checkSetup() : onReload}>{copy.check}</button>}
        {!setup && <span className="auth-version">Goby</span>}
      </div>
    </section>
  </AuthFrame>;
}
