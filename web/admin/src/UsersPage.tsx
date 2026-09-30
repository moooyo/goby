import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Avatar, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, Paper, Skeleton, Snackbar, Stack, Switch, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Tooltip, Typography } from '@mui/material';
import PersonAddAltRounded from '@mui/icons-material/PersonAddAltRounded';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import AdminPanelSettingsRounded from '@mui/icons-material/AdminPanelSettingsRounded';
import WarningAmberRounded from '@mui/icons-material/WarningAmberRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ManageAccountsOutlined from '@mui/icons-material/ManageAccountsOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { User, UsersResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError, PasswordField } from './formFields';
import { ManagedUserDialog, UnsavedChangesDialog } from './ManagedUserDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { AccessBadge, AccessTableHeader, accessTableSx } from './accessVisuals';

function initials(name: string): string {
  return name.trim().slice(0, 2).toUpperCase();
}

function createdDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
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
    <Dialog open={open} onClose={requestClose} fullWidth maxWidth="sm" aria-labelledby="create-user-title" slotProps={{ paper: { sx: { maxWidth: 520 } } }}>
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-user-title" sx={{ px: 3, pt: 3, pb: 0.5 }}>Create user</DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography color="text.secondary" variant="body2" sx={{ mb: 3 }}>Give someone an account on your media server.</Typography>
          <Stack spacing={2.5}>
            {error != null && !outcomeUnknown && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The response could not be confirmed. This user may already have been created. Check the user list before trying again.</Alert>}
            <TextField id="new-user-name" name="Name" autoFocus fullWidth required label="Username" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} autoComplete="off" error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
            <PasswordField id="new-user-password" name="Password" fullWidth required label="Password" value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy} autoComplete="new-password" error={passwordTooLong || Boolean(fieldError(error, 'Password'))} helperText={fieldError(error, 'Password') ?? (passwordTooLong ? 'Use at most 72 UTF-8 bytes for the password.' : 'Choose a unique password of at most 72 UTF-8 bytes.')} />
            <Box sx={{ p: 2, borderRadius: '16px', bgcolor: 'background.paper' }}>
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
  const [data, setData] = useState<UsersResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [managing, setManaging] = useState<User>();
  const [notice, setNotice] = useState('');
  const [noticeSeverity, setNoticeSeverity] = useState<'success' | 'info'>('success');
  const listRequest = useRef(0);

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

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Users" description="Manage the people who can access your server." action={<Button variant="contained" startIcon={<PersonAddAltRounded />} onClick={() => setCreating(true)}>Create user</Button>} />
      {error != null && <Box sx={{ mb: 3 }}><ErrorNotice error={error} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden', borderRadius: '20px' }}>
        <AccessTableHeader title="All users" count={data?.TotalRecordCount}>
          <Tooltip title="Refresh users"><span><IconButton onClick={refresh} disabled={loading} aria-label="Refresh users" size="small"><RefreshRounded sx={{ fontSize: 20 }} /></IconButton></span></Tooltip>
        </AccessTableHeader>
        {!data && loading && <Stack spacing={1} sx={{ p: 3, pt: 0 }} role="status" aria-label="Loading users"><Skeleton height={54} /><Skeleton height={54} /><Skeleton height={54} /></Stack>}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: 5, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <PeopleOutlineRounded sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">No users to display</Typography>
            <Typography color="text.secondary">Create a user to give someone access to this server.</Typography>
            <Button onClick={() => setCreating(true)} startIcon={<PersonAddAltRounded />}>Create user</Button>
          </Stack>
        )}
        {data && data.Items.length > 0 && (
          <Box component="ul" aria-label="Server users" sx={{ display: { xs: 'block', sm: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
            {data.Items.map((user) => (
              <Box component="li" key={user.Id} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}>
                <Stack direction="row" sx={{ gap: 1.2, alignItems: 'flex-start' }}>
                  <Avatar sx={{ width: 36, height: 36, fontSize: 12, fontWeight: 600, bgcolor: user.IsAdministrator ? '#D8E4FA' : '#E4E8F0', color: user.IsAdministrator ? '#0F2A57' : 'text.secondary' }}>{initials(user.Name)}</Avatar>
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{user.Name}</Typography>
                    <Typography variant="caption" color="text.secondary">{user.IsAdministrator ? 'Administrator' : 'Member'}{user.Id === currentUser.Id ? ' · You' : ''}</Typography>
                  </Box>
                  <AccessBadge label={user.IsDisabled ? 'Disabled' : 'Active'} tone={user.IsDisabled ? 'neutral' : 'success'} />
                </Stack>
                <Stack direction="row" sx={{ justifyContent: 'space-between', gap: 1, mt: 2 }}>
                  <Typography variant="caption" color={user.HasPassword ? 'text.secondary' : 'warning.main'}>{user.HasPassword ? 'Password set' : 'Password not set'}</Typography>
                  <Typography variant="caption" color="text.secondary">{createdDate(user.CreatedAt)}</Typography>
                </Stack>
                <Button size="small" variant="outlined" startIcon={<ManageAccountsOutlined />} onClick={() => setManaging(user)} aria-label={`Manage ${user.Name}`} sx={{ mt: 2 }}>Manage</Button>
              </Box>
            ))}
          </Box>
        )}
        {data && data.Items.length > 0 && (
          <TableContainer sx={{ display: { xs: 'none', sm: 'block' } }}>
            <Table aria-label="Server users" sx={{ minWidth: 650, ...accessTableSx }}>
              <TableHead><TableRow><TableCell sx={{ pl: 3 }}>User</TableCell><TableCell>Access</TableCell><TableCell>Status</TableCell><TableCell>Password</TableCell><TableCell>Created</TableCell><TableCell align="right" sx={{ pr: 3 }}>Manage</TableCell></TableRow></TableHead>
              <TableBody>
                {data.Items.map((user) => (
                  <TableRow key={user.Id}>
                    <TableCell component="th" scope="row" sx={{ pl: 3 }}>
                      <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5 }}>
                        <Avatar sx={{ width: 36, height: 36, fontSize: 12, fontWeight: 600, bgcolor: user.IsAdministrator ? '#D8E4FA' : '#E4E8F0', color: user.IsAdministrator ? '#0F2A57' : 'text.secondary' }}>{initials(user.Name)}</Avatar>
                        <Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere', maxWidth: 250 }}>{user.Name}</Typography>{user.Id === currentUser.Id && <Typography variant="caption" color="text.secondary">You</Typography>}</Box>
                      </Stack>
                    </TableCell>
                    <TableCell><Stack direction="row" sx={{ alignItems: 'center', gap: 0.75 }}>{user.IsAdministrator && <AdminPanelSettingsRounded sx={{ fontSize: 17, color: 'primary.main' }} />}<Typography variant="body2">{user.IsAdministrator ? 'Administrator' : 'Member'}</Typography></Stack></TableCell>
                    <TableCell><AccessBadge label={user.IsDisabled ? 'Disabled' : 'Active'} tone={user.IsDisabled ? 'neutral' : 'success'} /></TableCell>
                    <TableCell><Stack direction="row" sx={{ alignItems: 'center', gap: 0.5, color: user.HasPassword ? 'text.secondary' : '#8A5A00' }}>{!user.HasPassword && <WarningAmberRounded sx={{ fontSize: 15 }} />}<Typography variant="body2" sx={{ whiteSpace: 'nowrap' }}>{user.HasPassword ? 'Set' : 'Not set'}</Typography></Stack></TableCell>
                    <TableCell sx={{ whiteSpace: 'nowrap' }}><Typography variant="body2" color="text.secondary">{createdDate(user.CreatedAt)}</Typography></TableCell>
                    <TableCell align="right" sx={{ pr: 3 }}><Button size="small" startIcon={<ManageAccountsOutlined />} onClick={() => setManaging(user)} aria-label={`Manage ${user.Name}`}>Manage</Button></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
        {!data && !loading && error != null && <Typography variant="body2" color="text.secondary" sx={{ px: 3, pb: 3 }}>The user list could not be loaded. Retry the request to see your users.</Typography>}
      </Paper>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Members use compatible media clients. Administrator accounts can also sign in to this dashboard.</Typography>
      {creating && <CreateUserDialog open onClose={() => { setCreating(false); refresh(); }} onCreated={(user) => { setCreating(false); setNotice(`User ${user.Name} created.`); setNoticeSeverity('success'); refresh(); }} onNavigationGuardChange={onNavigationGuardChange} />}
      {managing && <ManagedUserDialog key={managing.Id} userId={managing.Id} currentUserId={currentUser.Id} onClose={() => { setManaging(undefined); refresh(); }} onUpdated={(user, message) => { setData((current) => current ? { ...current, Items: current.Items.map((item) => item.Id === user.Id ? user : item) } : current); if (user.Id === currentUser.Id) onCurrentUserUpdated(user); setNotice(message); setNoticeSeverity('success'); }} onRemoved={removeUser} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice) && !managing && !creating} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity={noticeSeverity} variant="filled" onClose={() => setNotice('')}>{notice}</Alert></Snackbar>
    </Box>
  );
}
