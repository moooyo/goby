import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Avatar, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Paper, Skeleton, Snackbar, Stack, Switch, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Typography } from '@mui/material';
import PersonAddAltRounded from '@mui/icons-material/PersonAddAltRounded';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import ShieldOutlined from '@mui/icons-material/ShieldOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { adminApi, isAbortError } from './api';
import type { User, UsersResponse } from './api';
import { ErrorNotice, fieldError, PageHeading, PasswordField } from './components';

function initials(name: string): string {
  return name.trim().slice(0, 2).toUpperCase();
}

function createdDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

function CreateUserDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (user: User) => void }) {
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [administrator, setAdministrator] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.createUser({ Name: name.trim(), Password: password, IsAdministrator: administrator });
      setPassword('');
      onCreated(result.User);
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onClose={busy ? undefined : onClose} fullWidth maxWidth="sm" aria-labelledby="create-user-title">
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-user-title" sx={{ px: 3, pt: 3, pb: 0.5 }}><Typography component="span" variant="h3">Create user</Typography></DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography color="text.secondary" variant="body2" sx={{ mb: 3 }}>Give someone an account on your media server.</Typography>
          <Stack spacing={2.5}>
            {error != null && <ErrorNotice error={error} />}
            <TextField id="new-user-name" name="Name" autoFocus fullWidth required label="Username" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} autoComplete="off" error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
            <PasswordField id="new-user-password" name="Password" fullWidth required label="Password" value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy} autoComplete="new-password" error={Boolean(fieldError(error, 'Password'))} helperText={fieldError(error, 'Password') ?? 'Choose a unique password for this account.'} />
            <Box sx={{ p: 2, borderRadius: 2, bgcolor: 'background.default', border: 1, borderColor: 'divider' }}>
              <FormControlLabel control={<Switch checked={administrator} onChange={(event) => setAdministrator(event.target.checked)} disabled={busy} slotProps={{ input: { 'aria-describedby': 'administrator-help' } }} />} label={<Typography sx={{ fontWeight: 600 }}>Administrator access</Typography>} />
              <Typography id="administrator-help" color="text.secondary" variant="body2" sx={{ mt: 0.25 }}>Administrators can manage all server settings and users.</Typography>
            </Box>
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button onClick={onClose} disabled={busy} color="secondary">Cancel</Button>
          <Button type="submit" variant="contained" disabled={busy || !name.trim() || !password} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <PersonAddAltRounded />}>{busy ? 'Creating user...' : 'Create user'}</Button>
        </DialogActions>
      </Box>
    </Dialog>
  );
}

export function UsersPage({ currentUser }: { currentUser: User }) {
  const [data, setData] = useState<UsersResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [notice, setNotice] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    adminApi.getUsers({ signal: controller.signal })
      .then(setData)
      .catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  const refresh = () => setRevision((value) => value + 1);

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Users" description="Manage the people who can access your server." action={<Button variant="contained" startIcon={<PersonAddAltRounded />} onClick={() => setCreating(true)}>Create user</Button>} />
      {error != null && <Box sx={{ mb: 3 }}><ErrorNotice error={error} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, px: { xs: 2, sm: 3 }, py: 2.2 }}>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1.2 }}><Typography variant="h4" component="h2">All users</Typography>{data && <Chip label={data.TotalRecordCount.toLocaleString()} size="small" sx={{ bgcolor: 'background.default' }} />}</Stack>
          <Button size="small" onClick={refresh} disabled={loading} startIcon={<RefreshRounded />}>Refresh</Button>
        </Stack>
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
                  <Avatar sx={{ width: 34, height: 34, fontSize: 11, fontWeight: 650, bgcolor: user.IsAdministrator ? '#E0F0F1' : '#EDF1F4', color: user.IsAdministrator ? 'primary.dark' : 'text.secondary' }}>{initials(user.Name)}</Avatar>
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{user.Name}</Typography>
                    <Typography variant="caption" color="text.secondary">{user.IsAdministrator ? 'Administrator' : 'Member'}{user.Id === currentUser.Id ? ' · You' : ''}</Typography>
                  </Box>
                  <Chip size="small" label={user.IsDisabled ? 'Disabled' : 'Active'} variant="outlined" color={user.IsDisabled ? 'default' : 'success'} />
                </Stack>
                <Stack direction="row" sx={{ justifyContent: 'space-between', gap: 1, mt: 2 }}>
                  <Typography variant="caption" color={user.HasPassword ? 'text.secondary' : 'warning.main'}>{user.HasPassword ? 'Password set' : 'Password not set'}</Typography>
                  <Typography variant="caption" color="text.secondary">{createdDate(user.CreatedAt)}</Typography>
                </Stack>
              </Box>
            ))}
          </Box>
        )}
        {data && data.Items.length > 0 && (
          <TableContainer sx={{ display: { xs: 'none', sm: 'block' } }}>
            <Table aria-label="Server users" sx={{ minWidth: 650 }}>
              <TableHead><TableRow><TableCell sx={{ pl: 3 }}>User</TableCell><TableCell>Access</TableCell><TableCell>Status</TableCell><TableCell>Password</TableCell><TableCell sx={{ pr: 3 }}>Created</TableCell></TableRow></TableHead>
              <TableBody>
                {data.Items.map((user) => (
                  <TableRow key={user.Id}>
                    <TableCell component="th" scope="row" sx={{ pl: 3 }}>
                      <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5 }}>
                        <Avatar sx={{ width: 36, height: 36, fontSize: 12, fontWeight: 650, bgcolor: user.IsAdministrator ? '#E0F0F1' : '#EDF1F4', color: user.IsAdministrator ? 'primary.dark' : 'text.secondary' }}>{initials(user.Name)}</Avatar>
                        <Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere', maxWidth: 250 }}>{user.Name}</Typography>{user.Id === currentUser.Id && <Typography variant="caption" color="text.secondary">You</Typography>}</Box>
                      </Stack>
                    </TableCell>
                    <TableCell><Stack direction="row" sx={{ alignItems: 'center', gap: 0.6 }}>{user.IsAdministrator && <ShieldOutlined sx={{ fontSize: 16, color: 'primary.main' }} />}<Typography variant="body2">{user.IsAdministrator ? 'Administrator' : 'Member'}</Typography></Stack></TableCell>
                    <TableCell><Chip size="small" label={user.IsDisabled ? 'Disabled' : 'Active'} variant="outlined" color={user.IsDisabled ? 'default' : 'success'} /></TableCell>
                    <TableCell><Typography variant="body2" color={user.HasPassword ? 'text.secondary' : 'warning.main'}>{user.HasPassword ? 'Set' : 'Not set'}</Typography></TableCell>
                    <TableCell sx={{ pr: 3, whiteSpace: 'nowrap' }}><Typography variant="body2" color="text.secondary">{createdDate(user.CreatedAt)}</Typography></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
        {!data && !loading && error != null && <Typography variant="body2" color="text.secondary" sx={{ px: 3, pb: 3 }}>The user list could not be loaded. Retry the request to see your users.</Typography>}
      </Paper>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Members use compatible media clients. Administrator accounts can also sign in to this dashboard.</Typography>
      {creating && <CreateUserDialog open onClose={() => setCreating(false)} onCreated={(user) => { setCreating(false); setNotice(`User ${user.Name} created.`); refresh(); }} />}
      <Snackbar open={Boolean(notice)} autoHideDuration={6000} onClose={() => setNotice('')} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" onClose={() => setNotice('')}>{notice}</Alert></Snackbar>
    </Box>
  );
}
