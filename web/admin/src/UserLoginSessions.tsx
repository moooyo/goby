import { useEffect, useMemo, useState } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { LoginSession, User } from './api';
import { AccessBadge, AccessIconTile } from './accessVisuals';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import { ArrowBackRounded, ArrowForwardRounded, DevicesOutlined, LogoutRounded, RefreshRounded } from './icons';
import { Button, IconButton, Skeleton, Stack } from './ui';
import { daysSinceLogin, loginExpiryText, loginLifetimes } from './UserActivitySummary';
import type { LoginDate } from './UserActivitySummary';
import { boundedUserRequest, UserBatchDialog } from './UserBatchDialog';
import type { BatchStep } from './UserBatchDialog';

const sessionPageSize = 200;

async function getActiveLogins(userId: string, signal: AbortSignal): Promise<LoginSession[]> {
  const items: LoginSession[] = [];
  const seen = new Set<string>();
  let total: number | undefined;
  do {
    const result = await boundedUserRequest((requestSignal) => adminApi.getSessions({ UserId: userId, Status: 'active', StartIndex: items.length, Limit: sessionPageSize }, { signal: requestSignal }), signal);
    const expected = Math.min(sessionPageSize, Math.max(0, result.TotalRecordCount - items.length));
    if ((total !== undefined && result.TotalRecordCount !== total) || result.Items.length !== expected || result.Items.some((item) => item.UserId !== userId || item.Status !== 'active' || seen.has(item.Id))) {
      throw new ApiError('The active login list is incomplete or changed while loading. Refresh logins to try again.', { code: 'invalid_response' });
    }
    total = result.TotalRecordCount;
    for (const item of result.Items) { seen.add(item.Id); items.push(item); }
  } while (items.length < total);
  return items;
}

async function revokeLogin(session: LoginSession, _shouldStop: () => boolean, lifetime: AbortSignal): Promise<BatchStep> {
  const saved = await boundedUserRequest((signal) => adminApi.revokeSession(session.Id, { signal }), lifetime);
  if (saved.CurrentSessionRevoked) return 'signedOut';
  if (saved.UserId !== session.UserId || saved.Kind !== session.Kind) throw new ApiError('The sign-out response did not match the selected login.', { code: 'invalid_response' });
  return 'changed';
}

export function UserLoginSessions({ user, loginDate, observedAt, actionsDisabled, onBusyChange, onLoginObserved }: {
  user: User;
  loginDate: LoginDate;
  observedAt?: number;
  actionsDisabled: boolean;
  onBusyChange: (busy: boolean) => void;
  onLoginObserved: (userId: string, createdAt: string) => void;
}) {
  const { t, locale, formatCount } = useTranslation();
  const [sessions, setSessions] = useState<LoginSession[]>();
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [page, setPage] = useState(0);
  const [confirm, setConfirm] = useState<{ selected: LoginSession[]; all: boolean }>();
  const dateFormatter = useMemo(() => new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }), [locale]);
  const accountId = user.Id;
  const loading = sessions === undefined && error == null;

  function refresh() {
    setSessions(undefined);
    setError(null);
    setRevision((value) => value + 1);
  }

  useEffect(() => {
    const controller = new AbortController();
    void getActiveLogins(accountId, controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      setSessions(result);
      // A scoped active session is also a persisted login observation. Keep its
      // creation time when the directory activity snapshot predates that login.
      if (result[0]) onLoginObserved(accountId, result[0].CreatedAt);
    }).catch((cause) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    });
    return () => controller.abort();
  }, [accountId, onLoginObserved, revision]);

  const total = sessions?.length ?? 0;
  const activePage = Math.min(page, Math.max(0, Math.ceil(total / sessionPageSize) - 1));
  const from = activePage * sessionPageSize;
  const visible = sessions?.slice(from, from + sessionPageSize) ?? [];
  const days = observedAt !== undefined ? daysSinceLogin(loginDate, observedAt) : undefined;
  const empty = user.IsDisabled ? t('This account is disabled. Disabling it ended all its logins.')
    : loginDate === null ? t('This account has no recorded login.')
      : days !== undefined && days >= loginLifetimes.days ? t('The last recorded login was {elapsed} days ago. Logins expire automatically after at most {days} days.', { elapsed: days.toLocaleString(locale), days: loginLifetimes.days })
        : t('This account has no active logins.');
  const loginCount = formatCount(total, 'active login', 'active logins');
  const deviceName = (session: LoginSession) => session.DeviceName || t('Unnamed device');

  return <Stack spacing={1.5}>
    <div className="users-logins-toolbar">
      <span className="users-help" role="status">{loading ? t('Loading active logins') : error != null ? t('Active logins unavailable') : total === 0 ? t('No active logins') : loginCount}</span>
      <IconButton size="small" aria-label="Refresh logins" title="Refresh logins" disabled={loading || confirm !== undefined} onClick={refresh}><RefreshRounded fontSize="small" /></IconButton>
      <Button variant="outlined" color="error" size="small" startIcon={<LogoutRounded />} disabled={actionsDisabled || total === 0 || confirm !== undefined} onClick={() => {
        if (sessions?.length) setConfirm({ selected: [...sessions.filter((session) => !session.IsCurrent), ...sessions.filter((session) => session.IsCurrent)], all: true });
      }}>Sign out all logins</Button>
    </div>
    {error != null && <ErrorNotice error={error} retry={refresh} />}
    {loading && <Skeleton height={120} />}
    {sessions?.length === 0 && <div className="users-logins-empty"><DevicesOutlined /><p>{empty}</p></div>}
    {visible.length > 0 && <ul className="access-device-list users-logins-list" aria-label={t('Active logins')}>{visible.map((session) => <li key={session.Id}>
      <AccessIconTile><DevicesOutlined fontSize="small" /></AccessIconTile>
      <div className="users-login-identity">
        <div className="users-login-name"><strong translate={session.DeviceName ? 'no' : undefined}>{deviceName(session)}</strong>
          {session.Kind === 'admin' && <AccessBadge label="Administrator login" />}{session.IsCurrent && <AccessBadge label="Current login" tone="info" />}</div>
        <small>{(session.Client || session.ApplicationVersion) && <><span translate="no">{[session.Client, session.ApplicationVersion].filter(Boolean).join(' ')}</span> · </>}
          {t('Last activity {time}', { time: dateFormatter.format(new Date(session.LastSeenAt)) })} · {t('Expires {time}', { time: dateFormatter.format(new Date(session.ExpiresAt)) })}</small>
      </div>
      <Button size="small" color="secondary" disabled={actionsDisabled || confirm !== undefined} onClick={() => setConfirm({ selected: [session], all: false })} title={t('Sign out {device}', { device: deviceName(session) })}>Sign out</Button>
    </li>)}</ul>}
    {total > sessionPageSize && <div className="users-pagination users-login-pagination">
      <span className="access-mono">{(from + 1).toLocaleString(locale)}–{(from + visible.length).toLocaleString(locale)} / {total.toLocaleString(locale)}</span>
      <IconButton size="small" aria-label="Previous logins page" disabled={activePage === 0} onClick={() => setPage(activePage - 1)}><ArrowBackRounded fontSize="small" /></IconButton>
      <IconButton size="small" aria-label="Next logins page" disabled={from + visible.length >= total} onClick={() => setPage(activePage + 1)}><ArrowForwardRounded fontSize="small" /></IconButton>
    </div>}
    <p className="users-help">{t(loginExpiryText, loginLifetimes)} {t('After sign-out, the user must sign in again on that device. Playback using that login can be interrupted.')}</p>
    {confirm && <UserBatchDialog kind="signOut" single={!confirm.all} targets={confirm.selected} submit={revokeLogin} onBusyChange={onBusyChange} onFinished={refresh} onClose={() => setConfirm(undefined)} confirmation={{
      title: confirm.all ? t('Sign out all listed logins?') : t('Sign out the login on {device}?', { device: deviceName(confirm.selected[0]) }),
      description: t(confirm.all ? 'This ends every active login listed for this account, one at a time. Any new logins created since the list was read are not included.' : 'This ends only the selected login. The user can sign in again on the same device.'),
      note: t('Playback using a login can be interrupted when that login is signed out.'),
      selfWarning: confirm.selected.some((session) => session.IsCurrent) ? t('This includes your current administrator login. You will be signed out of the dashboard.') : undefined,
      action: t(confirm.all ? 'Sign out all logins' : 'Sign out'),
    }} />}
  </Stack>;
}
