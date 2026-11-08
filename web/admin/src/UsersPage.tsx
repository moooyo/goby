import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, Skeleton, Snackbar, Stack, Switch, TextField, Tooltip, Typography } from './ui';
import { PersonAddAltRounded } from './icons';
import { PeopleOutlineRounded } from './icons';
import { AdminPanelSettingsRounded } from './icons';
import { RefreshRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { User, UsersResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError, PasswordField } from './formFields';
import { ManagedUserDialog, UnsavedChangesDialog } from './ManagedUserDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { AccessBadge } from './accessVisuals';
import { useTranslation } from './i18n';
import { ActivityTimestamp, useUserActivity } from './UserActivitySummary';
import './access-pages.css';

function initials(name: string): string {
  return name.trim().slice(0, 2).toUpperCase();
}

function CreateUserDialog({ open, onClose, onCreated, onNavigationGuardChange }: { open: boolean; onClose: () => void; onCreated: (user: User) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [administrator, setAdministrator] = useState(false);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState<unknown>(null);
  const [discarding, setDiscarding] = useState(false);
  const [outcomeUnknown, setOutcomeUnknown] = useState(false);
  const passwordTooLong = new TextEncoder().encode(password).length > 72;
  useUserDraftNavigation(Boolean(name || password || administrator), busy, onNavigationGuardChange);

  function requestClose() {
    if (inFlight.current) return;
    if (name || password || administrator) setDiscarding(true);
    else onClose();
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || outcomeUnknown || passwordTooLong) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.createUser({ Name: name.trim(), Password: password, IsAdministrator: administrator });
      setPassword('');
      onCreated(result.User);
    } catch (cause) {
      if (cause instanceof ApiError && ['network_error', 'invalid_response'].includes(cause.code)) setOutcomeUnknown(true);
      if (!isAbortError(cause)) setError(cause);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return (
    <>
    <Dialog open={open} onClose={requestClose} fullWidth maxWidth="xs" aria-labelledby="create-user-title" slotProps={{ paper: { sx: { maxWidth: 480, borderRadius: 18 } } }}>
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-user-title" sx={{ px: 3, pt: 3, pb: 0.5 }}>Create user</DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography color="text.secondary" variant="body2" sx={{ mb: 3 }}>Give someone an account on your media server.</Typography>
          <Stack spacing={2.5}>
            {error != null && !outcomeUnknown && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The response could not be confirmed. This user may already have been created. Check the user list before trying again.</Alert>}
            <TextField id="new-user-name" name="Name" autoFocus fullWidth required label="Username" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} autoComplete="off" error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name') ? <span translate="no">{fieldError(error, 'Name')}</span> : undefined} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
            <PasswordField id="new-user-password" name="Password" fullWidth required label="Password" value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy} autoComplete="new-password" error={passwordTooLong || Boolean(fieldError(error, 'Password'))} helperText={fieldError(error, 'Password') ? <span translate="no">{fieldError(error, 'Password')}</span> : (passwordTooLong ? 'Use at most 72 UTF-8 bytes for the password.' : 'Choose a unique password of at most 72 UTF-8 bytes.')} />
            <Box sx={{ p: 2, borderRadius: '12px', bgcolor: 'var(--s2)', border: '1px solid var(--line)' }}>
              <FormControlLabel sx={{ m: 0, width: '100%', gap: 2, justifyContent: 'space-between' }} labelPlacement="start" control={<Switch checked={administrator} onChange={(event) => setAdministrator(event.target.checked)} disabled={busy} slotProps={{ input: { 'aria-describedby': 'administrator-help' } }} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>Administrator access</Typography><Typography id="administrator-help" color="text.secondary" variant="caption" component="div" sx={{ mt: 0.5 }}>Administrators can manage all server settings and users, and sign in to this dashboard.</Typography></Box>} />
            </Box>
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button onClick={requestClose} disabled={busy} color="secondary">Cancel</Button>
          {outcomeUnknown ? <Button variant="contained" startIcon={<RefreshRounded />} onClick={onClose}>Check users</Button> : <Button type="submit" variant="contained" disabled={busy || !name.trim() || !password || passwordTooLong} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <PersonAddAltRounded />}>{busy ? 'Creating user...' : 'Create user'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
    {discarding && <UnsavedChangesDialog reload={false} onKeep={() => setDiscarding(false)} onDiscard={onClose} />}
    </>
  );
}

export function UsersPage({ currentUser, onCurrentUserUpdated, onNavigationGuardChange }: { currentUser: User; onCurrentUserUpdated: (user: User) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, locale, formatCount } = useTranslation();
  const [data, setData] = useState<UsersResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [managing, setManaging] = useState<User>();
  const [notice, setNotice] = useState('');
  const [noticeSeverity, setNoticeSeverity] = useState<'success' | 'info'>('success');
  const listRequest = useRef(0);
  const activity = useUserActivity(data?.Items.map((user) => user.Id) ?? [], revision);

  useEffect(() => {
    const controller = new AbortController();
    const requestId = ++listRequest.current;
    setLoading(true);
    setError(null);
    adminApi.getUsers({ signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted && requestId === listRequest.current) setData(result); })
      .catch((cause: unknown) => { if (!isAbortError(cause) && requestId === listRequest.current) setError(cause); })
      .finally(() => { if (!controller.signal.aborted && requestId === listRequest.current) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  const refresh = () => setRevision((value) => value + 1);
  const removeUser = useCallback((userId: string, message: string, severity: 'success' | 'info') => {
    listRequest.current += 1;
    setData((current) => current ? {
      ...current,
      Items: current.Items.filter((user) => user.Id !== userId),
      TotalRecordCount: Math.max(0, current.TotalRecordCount - (current.Items.some((user) => user.Id === userId) ? 1 : 0)),
    } : current);
    setManaging(undefined);
    setNotice(message);
    setNoticeSeverity(severity);
    setRevision((value) => value + 1);
  }, []);

  const administratorCount = data?.Items.filter((user) => user.IsAdministrator).length;

  return (
    <Box aria-busy={loading} className="access-page">
      <PageHeading title="Users" description="Manage the people who can access your server." action={<Button variant="contained" startIcon={<PersonAddAltRounded />} onClick={() => setCreating(true)}>Create user</Button>} />
      {data && <div className="access-page-summary"><span>{formatCount(data.TotalRecordCount, 'User', 'Users')}</span><span>{administratorCount === undefined ? '—' : formatCount(administratorCount, 'Administrator', 'Administrators')}</span></div>}
      {error != null && <Box sx={{ mb: 2 }}><ErrorNotice error={error} retry={refresh} /></Box>}
      <section className="access-panel" aria-label={t('All users')}>
        <div className="access-panel-heading"><h2>{t('All users')}</h2><Tooltip title="Refresh users"><span><IconButton onClick={refresh} disabled={loading} aria-label="Refresh users" size="small"><RefreshRounded sx={{ fontSize: 17 }} /></IconButton></span></Tooltip></div>
        {!data && loading && <Stack spacing={1} sx={{ p: 2 }} role="status" aria-label="Loading users"><Skeleton height={52} /><Skeleton height={52} /><Skeleton height={52} /></Stack>}
        {data && data.Items.length === 0 && <div className="access-empty"><PeopleOutlineRounded sx={{ fontSize: 32, color: 'var(--ink3)' }} /><h3>{t('No users to display')}</h3><p>{t('Create a user to give someone access to this server.')}</p><Button onClick={() => setCreating(true)} startIcon={<PersonAddAltRounded />}>Create user</Button></div>}
        {data && data.Items.length > 0 && <div className="access-table-scroll"><table className="access-data-table access-users-table" aria-label={t('Server users')}>
          <thead><tr><th>{t('User')}</th><th>{t('Role')}</th><th>{t('Library access')}</th><th className="user-activity-column">{t('Watched items')}</th><th className="user-activity-column">{t('Last activity')}</th><th>{t('Status')}</th></tr></thead>
          <tbody>{data.Items.map((user) => <tr key={user.Id} onClick={() => setManaging(user)} className="access-clickable-row">
            <th scope="row"><button type="button" className="access-user-open" onClick={(event) => { event.stopPropagation(); setManaging(user); }} aria-label={`${t('Manage')} ${user.Name}`}><span className={`access-avatar${user.IsAdministrator ? ' access-avatar-admin' : ''}`} translate="no">{initials(user.Name)}</span><span className="access-user-name"><strong translate="no">{user.Name}</strong>{user.Id === currentUser.Id && <small>{t('You')}</small>}</span></button></th>
            <td><span className="access-role">{user.IsAdministrator && <AdminPanelSettingsRounded sx={{ fontSize: 15 }} />}{t(user.IsAdministrator ? 'Administrator' : 'Member')}</span></td>
            <td className="access-secondary-column"><span className="access-muted">{t(user.IsAdministrator ? 'All libraries' : 'Configured permissions')}</span></td>
            <td className="access-secondary-column access-mono">{activity.items?.[user.Id] ? new Intl.NumberFormat(locale).format(activity.items[user.Id].PlayedItemCount) : activity.loading ? <Skeleton width={36} height={18} aria-label={t('Loading user activity')} /> : <span className="access-muted">{t(activity.error ? 'Unavailable' : 'Not recorded')}</span>}</td>
            <td className="access-secondary-column access-mono user-activity-cell"><ActivityTimestamp value={activity.items?.[user.Id]?.LastActivityDate} unavailable={activity.error != null} loading={activity.loading} /></td>
            <td><AccessBadge label={user.IsDisabled ? 'Disabled' : 'Active'} tone={user.IsDisabled ? 'neutral' : 'success'} /></td>
          </tr>)}</tbody>
        </table></div>}
        {activity.error != null && <div className="user-activity-list-notice"><ErrorNotice error={activity.error} retry={activity.refresh} />{activity.items && <p>{t('Showing the last successful activity update.')}</p>}</div>}
        {!data && !loading && error != null && <p className="access-empty-message">{t('The user list could not be loaded. Retry the request to see your users.')}</p>}
      </section>
      <p className="access-footnote">{t('Members use compatible media clients. Administrator accounts can also sign in to this dashboard.')}</p>
      {creating && <CreateUserDialog open onClose={() => { setCreating(false); refresh(); }} onCreated={(user) => { setCreating(false); setNotice(`User ${user.Name} created.`); setNoticeSeverity('success'); refresh(); }} onNavigationGuardChange={onNavigationGuardChange} />}
      {managing && <ManagedUserDialog key={managing.Id} userId={managing.Id} currentUserId={currentUser.Id} onClose={() => { setManaging(undefined); refresh(); }} onUpdated={(user, message) => { setData((current) => current ? { ...current, Items: current.Items.map((item) => item.Id === user.Id ? user : item) } : current); if (user.Id === currentUser.Id) onCurrentUserUpdated(user); setNotice(message); setNoticeSeverity('success'); }} onRemoved={removeUser} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice) && !managing && !creating} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity={noticeSeverity} variant="filled" onClose={() => setNotice('')}>{notice}</Alert></Snackbar>
    </Box>
  );
}
