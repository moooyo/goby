import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, LinearProgress, Skeleton, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography } from './ui';
import { ArrowBackRounded, ArrowForwardRounded, BlockOutlined, NavigateNextRounded, PeopleOutlineRounded, PersonAddAltRounded, RefreshRounded, ScheduleOutlined, SearchRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { User } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError, PasswordField } from './formFields';
import { ManagedUserDialog, RoleChoices, UnsavedChangesDialog, maxUserPasswordBytes, passwordByteLength, userInitial } from './ManagedUserDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { useTranslation } from './i18n';
import { hasNoRecentLogin, idleLoginDays, LoginTimestamp, loginExpiryText, loginLifetimes, UserCreatedDate, useUserActivity } from './UserActivitySummary';
import { boundedUserRequest, isUnknownUserWrite, UserBatchDialog } from './UserBatchDialog';
import type { BatchStep } from './UserBatchDialog';
import './user-activity.css';

const pageSize = 50;
const noIds: string[] = [];
const noUsers: User[] = [];
const groups = ['all', 'idle', 'disabled', 'administrators'] as const;
type UserGroup = typeof groups[number];

function CreateUserDialog({ onClose, onCreated, onNavigationGuardChange }: { onClose: (checkUsers?: boolean) => void; onCreated: (user: User) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [administrator, setAdministrator] = useState(false);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState<unknown>(null);
  const [discarding, setDiscarding] = useState(false);
  const uncertain = isUnknownUserWrite(error);
  const passwordTooLong = passwordByteLength(password) > maxUserPasswordBytes;
  const passwordError = fieldError(error, 'Password');
  const nameError = fieldError(error, 'Name');
  const dirty = Boolean(name || password || administrator);
  const invalid = !name.trim() || !password || passwordTooLong;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange);

  function requestClose() {
    if (inFlight.current) return;
    if (dirty) setDiscarding(true);
    else onClose(uncertain);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || uncertain || invalid) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      const result = await boundedUserRequest((signal) => adminApi.createUser({ Name: name.trim(), Password: password, IsAdministrator: administrator }, { signal }));
      setPassword('');
      onCreated(result.User);
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return <>
    <Dialog open maxWidth="xs" className="users-create-dialog" onClose={requestClose} aria-label="Add user">
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-user-title">Add user</DialogTitle>
        <DialogContent>
          <Stack spacing={1.75}>
            <Typography color="text.secondary" variant="body2">New accounts can access all libraries. After creating an account, you can change its permissions and role in the account details.</Typography>
            {error != null && !uncertain && <ErrorNotice error={error} />}
            {uncertain && <Alert severity="warning">The response could not be confirmed. This user may already have been created. Check the user list before trying again.</Alert>}
            <TextField id="new-user-name" name="Name" autoFocus fullWidth required label="Name" value={name} onChange={(event) => setName(event.target.value)} disabled={busy || uncertain} autoComplete="off" error={Boolean(nameError)} helperText={nameError} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
            <PasswordField id="new-user-password" name="Password" fullWidth required label="Password" value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy || uncertain} autoComplete="new-password" error={passwordTooLong || Boolean(passwordError)} helperText={passwordTooLong ? 'Use at most 72 UTF-8 bytes for the password.' : passwordError ?? 'Choose a unique password of at most 72 UTF-8 bytes.'} />
            <RoleChoices administrator={administrator} disabled={busy || uncertain} onChange={setAdministrator} />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={requestClose} disabled={busy} color="secondary">Cancel</Button>
          {uncertain ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={() => onClose(true)}>Check users</Button> : <Button type="submit" variant="contained" disabled={busy || invalid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <PersonAddAltRounded />}>{busy ? 'Adding user...' : 'Add user'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
    {discarding && <UnsavedChangesDialog reload={false} onKeep={() => setDiscarding(false)} onDiscard={() => onClose(uncertain)} />}
  </>;
}

interface Directory { users: User[]; ids: string[] }
interface DisableSelection { source: 'idle' | 'selected'; targets: User[] }

export function UsersPage({ currentUser, onCurrentUserUpdated, onNavigationGuardChange }: { currentUser: User; onCurrentUserUpdated: (user: User) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, locale } = useTranslation();
  const [directory, setDirectory] = useState<Directory>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [managing, setManaging] = useState<User>();
  const [notice, setNotice] = useState<{ message: string; severity: 'success' | 'info' }>();
  const [group, setGroup] = useState<UserGroup>('all');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const [selection, setSelection] = useState<ReadonlySet<string>>(new Set());
  const [batch, setBatch] = useState<DisableSelection>();
  const [batchBusy, setBatchBusy] = useState(false);
  const [observedLogins, setObservedLogins] = useState<ReadonlyMap<string, string>>(new Map());
  useUserDraftNavigation(false, batchBusy, onNavigationGuardChange);

  const users = directory?.users ?? noUsers;
  const ids = directory?.ids ?? noIds;
  const activity = useUserActivity(ids);
  const { records, observedAt, prioritize } = activity;
  const loginFor = useCallback((id: string) => {
    const recorded = records.get(id)?.date;
    const seen = observedLogins.get(id);
    return seen && (!recorded || Date.parse(seen) > Date.parse(recorded)) ? seen : recorded;
  }, [records, observedLogins]);
  const idle = useMemo(() => observedAt === undefined ? [] : users.filter(({ Id }) => observedLogins.has(Id) ? hasNoRecentLogin(loginFor(Id), observedAt) : records.get(Id)?.idle), [users, records, loginFor, observedLogins, observedAt]);

  const rememberLogin = useCallback((userId: string, createdAt: string) => {
    setObservedLogins((current) => {
      const previous = current.get(userId);
      if (previous && Date.parse(previous) >= Date.parse(createdAt)) return current;
      return new Map(current).set(userId, createdAt);
    });
  }, []);
  const { administrators, disabledUsers, enabledAdministrators } = useMemo(() => {
    const administrators: User[] = [];
    const disabledUsers: User[] = [];
    let enabledAdministrators = 0;
    for (const user of users) {
      if (user.IsDisabled) disabledUsers.push(user);
      if (user.IsAdministrator) {
        administrators.push(user);
        if (!user.IsDisabled) enabledAdministrators += 1;
      }
    }
    return { administrators, disabledUsers, enabledAdministrators };
  }, [users]);
  const groupUsers = { all: users, idle, disabled: disabledUsers, administrators };
  const members = groupUsers[group];
  const query = search.trim().normalize('NFKC').toLowerCase();
  const filtered = useMemo(() => query ? members.filter((user) => user.Name.normalize('NFKC').toLowerCase().includes(query)) : members, [members, query]);
  const lastPage = Math.max(0, Math.ceil(filtered.length / pageSize) - 1);
  const activePage = Math.min(page, lastPage);
  const from = activePage * pageSize;
  const visible = useMemo(() => filtered.slice(from, from + pageSize), [filtered, from]);
  useLayoutEffect(() => { prioritize(visible.map((user) => user.Id)); }, [visible, prioritize]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    void adminApi.getUsers({ signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) setDirectory({ users: result.Items, ids: result.Items.map((user) => user.Id) });
    }).catch((cause) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [reloadRevision]);

  const refresh = useCallback(() => {
    setLoading(true);
    setSelection(new Set());
    setReloadRevision((value) => value + 1);
  }, []);
  const removeUser = useCallback((userId: string, message: string, severity: 'success' | 'info') => {
    setDirectory((current) => current && ({ ...current, users: current.users.filter((user) => user.Id !== userId) }));
    setManaging(undefined);
    setNotice({ message, severity });
    refresh();
  }, [refresh]);
  const observeUser = useCallback((user: User) => {
    setDirectory((current) => {
      const older = current?.users.find((item) => item.Id === user.Id);
      if (!current || !older || (older.Name === user.Name && older.IsAdministrator === user.IsAdministrator && older.IsDisabled === user.IsDisabled && older.HasPassword === user.HasPassword)) return current;
      return { ...current, users: current.users.map((item) => item.Id === user.Id ? user : item) };
    });
  }, []);

  const count = (value: number) => value.toLocaleString(locale);
  const idleComplete = Boolean(directory && !activity.loading && activity.missing === 0);
  const candidates = useMemo(() => !loading && error == null && (group !== 'idle' || idleComplete) ? filtered.filter((user) => !user.IsDisabled && selection.has(user.Id)) : [], [error, filtered, group, idleComplete, loading, selection]);
  const idleTargets = group === 'idle' && idleComplete && !loading && error == null ? filtered.filter((user) => !user.IsAdministrator && !user.IsDisabled) : [];
  const pageSelection = visible.filter((user) => selection.has(user.Id)).length;
  const searchLabel = t('Find accounts');
  const groupLabels = { all: t('All'), idle: t('No login in {days} days', { days: idleLoginDays }), disabled: t('Disabled'), administrators: t('Administrators') };
  const groupCount = (value: UserGroup) => value === 'idle' && !idleComplete ? t(activity.loading ? 'Calculating' : 'Unavailable') : count(groupUsers[value].length);
  const recordFor = (id: string) => !records.has(id) && activity.loading;

  function select(ids: string[], checked: boolean) {
    setSelection((current) => {
      const next = new Set(current);
      for (const id of ids) {
        if (checked) next.add(id);
        else next.delete(id);
      }
      return next;
    });
  }

  function prepareDisable(source: DisableSelection['source'], targets: User[]) {
    // Revoking this browser's authentication must be the last operation.
    const others = targets.filter((user) => user.Id !== currentUser.Id);
    const self = targets.find((user) => user.Id === currentUser.Id);
    setBatch({ source, targets: self ? [...others, self] : others });
  }

  async function disableAccount(target: User, shouldStop: () => boolean, signal: AbortSignal): Promise<BatchStep> {
    let result;
    try {
      result = await boundedUserRequest((requestSignal) => adminApi.getUser(target.Id, { signal: requestSignal }), signal);
    } catch (cause) {
      if (cause instanceof ApiError && (cause.status === 401 || cause.status === 403 || cause.code === 'session_changed')) throw cause;
      return cause instanceof ApiError && cause.status === 404 ? 'ineligible' : 'failed';
    }
    if (shouldStop()) return 'cancelled';
    const user = result.User;
    if (user.Id !== target.Id) return 'failed';
    if (user.IsDisabled || (user.IsAdministrator && (batch?.source === 'idle' || !target.IsAdministrator))) return 'ineligible';
    const saved = await boundedUserRequest((requestSignal) => adminApi.updateUser(user.Id, { Revision: user.Revision, Name: user.Name, IsAdministrator: user.IsAdministrator, IsDisabled: true, Policy: user.Policy }, { signal: requestSignal }), signal);
    if (saved.CurrentSessionRevoked) return 'signedOut';
    if (saved.User.Id !== user.Id || !saved.User.IsDisabled) return 'unconfirmed';
    return 'changed';
  }

  return <Box className="access-page users-page">
    <PageHeading title="Users" description="Manage accounts, permissions, and logins." action={<Button variant="contained" startIcon={<PersonAddAltRounded />} onClick={() => setCreating(true)}>Add user</Button>} />
    {notice && !managing && !creating && <Alert role={notice.severity === 'success' ? 'status' : 'alert'} severity={notice.severity} className="users-page-notice" onClose={() => setNotice(undefined)}>{notice.message}</Alert>}
    {error != null && <div className="users-page-notice"><ErrorNotice error={error} retry={refresh} /></div>}
    <section className="access-panel users-accounts" aria-label={t('Accounts')}>
      <header className="users-panel-header">
        <h2>{t('Accounts')}</h2>
        {directory && <span className="access-mono">{groupLabels[group]} · {groupCount(group)}</span>}
        <div className="users-panel-actions">
          {idleTargets.length > 0 && <Button variant="outlined" color="error" size="small" startIcon={<BlockOutlined />} onClick={() => prepareDisable('idle', idleTargets)}>{t('Disable these accounts ({count})', { count: count(idleTargets.length) })}</Button>}
          <IconButton size="small" onClick={refresh} disabled={loading} aria-label="Refresh users" title="Refresh users"><RefreshRounded fontSize="small" /></IconButton>
        </div>
      </header>
      {!directory && loading && <Stack spacing={1} className="users-list-notice" role="status" aria-label="Loading users"><Skeleton height={52} /><Skeleton height={52} /><Skeleton height={52} /></Stack>}
      {directory && <>
        <div className="users-toolbar">
          <ToggleButtonGroup exclusive value={group} className="users-groups" aria-label="Account groups" onChange={(_event, next: UserGroup | null) => { if (next) { setGroup(next); setPage(0); setSelection(new Set()); } }}>
            {groups.map((value) => <ToggleButton key={value} value={value}>{groupLabels[value]} <span className="users-group-count">{groupCount(value)}</span></ToggleButton>)}
          </ToggleButtonGroup>
          <label className="users-search"><SearchRounded fontSize="small" /><input type="search" aria-label={searchLabel} placeholder={t('Find accounts by name')} value={search} onChange={(event) => { setSearch(event.target.value); setPage(0); setSelection(new Set()); }} /></label>
          {activity.loading && <div className="users-activity-progress" role="status"><span>{t('Reading login records')}</span><span className="access-mono">{count(activity.done)} / {count(ids.length)}</span><LinearProgress variant="determinate" value={activity.done * 100 / ids.length} aria-label="Login records loaded" /></div>}
        </div>
        {group === 'idle' && activity.loading && <Alert className="users-inline-alert" severity="info">{t('Still reading login records ({loaded} / {total}). The list may be incomplete. Bulk disabling is available only after all records have loaded.', { loaded: count(activity.done), total: count(ids.length) })}</Alert>}
        {selection.size > 0 && <div className="users-toolbar users-selection">
          <strong>{t('Accounts selected: {count}', { count: count(selection.size) })}</strong>
          {candidates.length > 0 && <Button size="small" variant="outlined" color="error" startIcon={<BlockOutlined />} onClick={() => prepareDisable('selected', candidates)}>Disable</Button>}
          <Button size="small" color="secondary" onClick={() => setSelection(new Set())}>Clear selection</Button>
        </div>}
        {visible.length > 0 ? <div className="access-table-scroll users-table-scroll" role="region" aria-label={t('Accounts table')} tabIndex={0}>
          <table className="access-data-table users-table" aria-label={t('Server users')}>
            <colgroup><col className="users-select-column" /><col /><col className="users-role-column" /><col className="users-login-column" /><col className="users-created-column" /><col className="users-status-column" /><col className="users-action-column" /></colgroup>
            <thead><tr>
              <th scope="col"><Checkbox checked={pageSelection === visible.length} indeterminate={pageSelection > 0 && pageSelection < visible.length} onChange={(event) => select(visible.map((user) => user.Id), event.target.checked)} slotProps={{ input: { 'aria-label': 'Select all accounts on this page' } }} /></th>
              <th scope="col">{t('User')}</th><th scope="col">{t('Role')}</th><th scope="col">{t('Last sign-in')}</th><th scope="col">{t('Created')}</th><th scope="col">{t('Status')}</th><th scope="col"><span className="visually-hidden">{t('Actions')}</span></th>
            </tr></thead>
            <tbody>{visible.map((user) => <tr key={user.Id} className="access-clickable-row" data-selected={selection.has(user.Id)} onClick={() => setManaging(user)}>
              <td onClick={(event) => event.stopPropagation()}><Checkbox checked={selection.has(user.Id)} onChange={(event) => select([user.Id], event.target.checked)} slotProps={{ input: { 'aria-label': t('Select {name}', { name: user.Name }) } }} /></td>
              <th scope="row"><button type="button" className="access-user-open" onClick={(event) => { event.stopPropagation(); setManaging(user); }} aria-label={t('Manage {name}', { name: user.Name })}><span className={`access-avatar${user.IsAdministrator ? ' access-avatar-admin' : ''}`} aria-hidden="true">{userInitial(user.Name)}</span><span className="access-user-name"><strong translate="no" title={user.Name}>{user.Name}</strong>{user.Id === currentUser.Id && <small>{t('You')}</small>}</span></button></th>
              <td className="access-muted">{t(user.IsAdministrator ? 'Administrator' : 'Member')}</td>
              <td className="users-login-cell"><LoginTimestamp value={loginFor(user.Id)} loading={recordFor(user.Id)} observedAt={observedAt} /></td>
              <td><UserCreatedDate value={user.CreatedAt} /></td>
              <td><span className="users-account-state" data-active={!user.IsDisabled}>{t(user.IsDisabled ? 'Disabled' : 'Active')}</span></td>
              <td><NavigateNextRounded fontSize="small" aria-hidden="true" /></td>
            </tr>)}</tbody>
          </table>
        </div> : <div className="access-empty"><PeopleOutlineRounded /><h3>{t(group === 'idle' && !idleComplete ? 'No confirmed matching accounts yet' : 'No matching accounts')}</h3></div>}
        <div className="users-pagination">
          <span>{t('Accounts per page: {count}', { count: count(pageSize) })}</span>
          <span className="access-mono">{filtered.length ? `${count(from + 1)}–${count(from + visible.length)}` : '0'} / {group === 'idle' && !idleComplete ? '…' : count(filtered.length)}</span>
          <div className="users-pagination-nav"><IconButton size="small" aria-label="Go to previous page" disabled={activePage === 0} onClick={() => setPage(activePage - 1)}><ArrowBackRounded fontSize="small" /></IconButton><IconButton size="small" aria-label="Go to next page" disabled={activePage === lastPage} onClick={() => setPage(activePage + 1)}><ArrowForwardRounded fontSize="small" /></IconButton></div>
        </div>
        {activity.missing > 0 && <div className="users-list-notice">
          {activity.error != null && <ErrorNotice error={activity.error} retry={refresh} />}
          <p className="users-help">{t('Some login records are unavailable. The inactivity group is incomplete and cannot be used to disable accounts.')}</p>
        </div>}
      </>}
      {!directory && !loading && error != null && <p className="access-empty-message">{t('The user list could not be loaded. Retry the request to see your users.')}</p>}
    </section>
    <div className="users-footnotes">
      <p className="access-footnote"><ScheduleOutlined fontSize="small" /><span>{t('Last sign-in shows recorded logins, with the current page loaded first. The {days}-day group includes accounts with no login on record and is counted after all records load.', { days: idleLoginDays })}</span></p>
      <p className="access-footnote">{t(loginExpiryText, loginLifetimes)} {t('No manual cleanup is needed.')}</p>
    </div>
    {creating && <CreateUserDialog onClose={(checkUsers) => { setCreating(false); if (checkUsers) refresh(); }} onCreated={(user) => {
      setCreating(false);
      setGroup('all');
      setSearch('');
      setPage(0);
      setNotice({ message: t(user.IsAdministrator ? 'Added administrator {name}.' : 'Added user {name}.', { name: user.Name }), severity: 'success' });
      refresh();
    }} onNavigationGuardChange={onNavigationGuardChange} />}
    {managing && <ManagedUserDialog key={managing.Id} userId={managing.Id} initialName={managing.Name} currentUserId={currentUser.Id}
      enabledAdministratorCount={!loading && error == null ? enabledAdministrators : undefined}
      loginDate={loginFor(managing.Id)} loginLoading={recordFor(managing.Id)} loginObservedAt={observedAt}
      onClose={() => setManaging(undefined)} onObserved={observeUser} onLoginObserved={rememberLogin}
      onUpdated={(user, message) => { observeUser(user); if (user.Id === currentUser.Id) onCurrentUserUpdated(user); setNotice({ message, severity: 'success' }); }}
      onRemoved={removeUser} onNavigationGuardChange={onNavigationGuardChange} />}
    {batch && <UserBatchDialog kind="disable" targets={batch.targets} submit={disableAccount} onClose={() => setBatch(undefined)} onBusyChange={setBatchBusy} onFinished={refresh} confirmation={{
      title: t('Disable accounts ({count})?', { count: count(batch.targets.length) }),
      action: t('Disable accounts ({count})', { count: count(batch.targets.length) }),
      description: t('Disabling ends all logins for these accounts and prevents new ones. Their accounts, permissions, and playback history are kept. You can enable the accounts again later.'),
      note: t(batch.source === 'idle' ? 'These accounts are chosen from the loaded login records and the current search. Administrator accounts are excluded.' : 'The last enabled administrator cannot be disabled.'),
      selfWarning: batch.targets.some((user) => user.Id === currentUser.Id) ? t('This selection includes your own account. If it is disabled, you will be signed out of the dashboard.') : undefined,
    }} />}
  </Box>;
}
