import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, Paper, Skeleton, Stack, Switch, TextField, Typography, useMediaQuery } from '@mui/material';
import LockResetRounded from '@mui/icons-material/LockResetRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, ManagedUser, UpdateUserInput, UserPolicy, UserMutationResponse } from './api';
import { ErrorNotice } from './components';
import { fieldError, PasswordField } from './formFields';
import { colors, theme } from './theme';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

function inputFor(user: ManagedUser): UpdateUserInput {
  return {
    Revision: user.Revision,
    Name: user.Name,
    IsAdministrator: user.IsAdministrator,
    IsDisabled: user.IsDisabled,
    Policy: { ...user.Policy, EnabledFolders: [...user.Policy.EnabledFolders] },
  };
}

function draftKey(input: UpdateUserInput): string {
  return JSON.stringify({ ...input, Policy: { ...input.Policy, EnabledFolders: [...input.Policy.EnabledFolders].sort() } });
}

function isUnknownOutcome(error: unknown): boolean {
  return error instanceof ApiError && ['network_error', 'invalid_response'].includes(error.code);
}

function isRevisionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.code === 'revision_conflict';
}

export function UnsavedChangesDialog({ reload, onKeep, onDiscard }: { reload: boolean; onKeep: () => void; onDiscard: () => void }) {
  return (
    <Dialog open onClose={onKeep} fullWidth maxWidth="xs" aria-labelledby="discard-user-title" aria-describedby="discard-user-description">
      <DialogTitle id="discard-user-title">Discard unsaved changes?</DialogTitle>
      <DialogContent><Typography id="discard-user-description" color="text.secondary">{reload ? 'Reloading replaces your draft with the latest saved user. Your unsaved changes will be lost.' : 'Your changes have not been saved. Keep editing to finish them, or discard this draft.'}</Typography></DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}><Button onClick={onKeep} autoFocus color="secondary">Keep editing</Button><Button onClick={onDiscard} color="error" variant="contained">{reload ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions>
    </Dialog>
  );
}

function MutationNotice({ error, password = false, onReload }: { error: unknown; password?: boolean; onReload: () => void }) {
  if (error == null) return null;
  if (isRevisionConflict(error) || isUnknownOutcome(error)) {
    return (
      <Alert severity="warning" sx={{ '& .MuiAlert-message': { minWidth: 0 } }}>
        <Typography variant="body2">{isRevisionConflict(error)
          ? 'This user changed after you opened it. Your draft is still here. Reload the latest user and review it before saving again.'
          : password
            ? 'The response could not be confirmed. The password may already have changed and existing sign-ins may have ended. Reload this user before deciding whether to reset the password again.'
            : 'The response could not be confirmed. Your changes may already have been saved. Reload this user and check the saved details before trying again.'}</Typography>
        {error instanceof ApiError && error.requestId && <Typography variant="caption" component="div" sx={{ mt: 0.5 }}>Request ID: <span className="mono">{error.requestId}</span></Typography>}
        <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={onReload} sx={{ mt: 1, ml: -1 }}>Reload latest user</Button>
      </Alert>
    );
  }
  if (error instanceof ApiError && error.code === 'last_administrator') {
    return <Alert severity="error">Keep at least one enabled administrator. Give another active user administrator access before disabling this account or removing its administrator access.</Alert>;
  }
  return <ErrorNotice error={error} />;
}

function Section({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title}>
      <Typography variant="h4" component="h3">{title}</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2 }}>{description}</Typography>
      {children}
    </Box>
  );
}

function PermissionSwitch({ id, label, description, checked, disabled, error, onChange }: { id: string; label: string; description: string; checked: boolean; disabled: boolean; error?: string; onChange: (checked: boolean) => void }) {
  return (
    <Box>
      <FormControlLabel sx={{ m: 0, width: '100%', justifyContent: 'space-between', alignItems: 'flex-start', gap: 2 }} labelPlacement="start" control={<Switch checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} slotProps={{ input: { 'aria-describedby': `${id}-help` } }} />} label={<Typography variant="body2" sx={{ fontWeight: 650, pt: 0.8 }}>{label}</Typography>} />
      <Typography id={`${id}-help`} variant="body2" color={error ? 'error.main' : 'text.secondary'}>{error ?? description}</Typography>
    </Box>
  );
}

function ResetPasswordDialog({ user, isCurrentUser, onClose, onReset, onReload, onReviewRequired, onDraftStateChange }: { user: ManagedUser; isCurrentUser: boolean; onClose: () => void; onReset: (result: UserMutationResponse) => void; onReload: () => void; onReviewRequired: (error: unknown) => void; onDraftStateChange: (state: { dirty: boolean; busy: boolean }) => void }) {
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState<unknown>(null);
  const [pendingAction, setPendingAction] = useState<'close' | 'reload'>();
  const [submitted, setSubmitted] = useState(false);
  const passwordBytes = new TextEncoder().encode(password).length;
  const passwordError = fieldError(error, 'Password') ?? (passwordBytes > 72 ? 'Use at most 72 UTF-8 bytes for the password.' : submitted && !password ? 'Enter a new password.' : undefined);
  const confirmationError = confirmation && password !== confirmation ? 'The passwords do not match.' : submitted && !confirmation ? 'Confirm the new password.' : undefined;
  const blocked = isRevisionConflict(error) || isUnknownOutcome(error);
  const dirty = Boolean(password || confirmation);
  useEffect(() => {
    onDraftStateChange({ dirty, busy });
  }, [dirty, busy, onDraftStateChange]);

  function requestAction(action: 'close' | 'reload') {
    if (inFlight.current) return;
    if (dirty) setPendingAction(action);
    else if (action === 'reload') onReload();
    else onClose();
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitted(true);
    if (inFlight.current || blocked || !password || passwordBytes > 72 || password !== confirmation) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.resetUserPassword(user.Id, { Revision: user.Revision, Password: password });
      setPassword('');
      setConfirmation('');
      onReset(result);
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
      if (isRevisionConflict(cause) || isUnknownOutcome(cause)) onReviewRequired(cause);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return (
    <>
      <Dialog open onClose={() => requestAction('close')} fullWidth maxWidth="sm" aria-labelledby="reset-user-password-title">
        <Box component="form" onSubmit={submit} aria-busy={busy}>
          <DialogTitle id="reset-user-password-title">Reset password</DialogTitle>
          <DialogContent>
            <Stack spacing={2.5} sx={{ pt: 0.5 }}>
              <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Set a new password for <strong>{user.Name}</strong>.</Typography>
              <Alert severity="warning">Existing sign-ins for this account will end. {isCurrentUser ? 'You will return to sign in and must use the new password.' : 'The user must sign in again with the new password.'}</Alert>
              <MutationNotice error={error} password onReload={() => requestAction('reload')} />
              <PasswordField id="reset-user-password" name="Password" autoFocus fullWidth required label="New password" value={password} disabled={busy} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" error={Boolean(passwordError)} helperText={passwordError ?? 'Use a nonempty password of at most 72 UTF-8 bytes. Some characters use more than one byte.'} />
              <PasswordField id="reset-user-confirm-password" name="ConfirmPassword" fullWidth required label="Confirm new password" value={confirmation} disabled={busy} onChange={(event) => setConfirmation(event.target.value)} autoComplete="new-password" error={Boolean(confirmationError)} helperText={confirmationError} />
            </Stack>
          </DialogContent>
          <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button onClick={() => requestAction('close')} disabled={busy} color="secondary">Cancel</Button><Button type="submit" variant="contained" disabled={busy || blocked || !password || passwordBytes > 72 || password !== confirmation} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <LockResetRounded />}>{busy ? 'Resetting password...' : 'Reset password'}</Button></DialogActions>
        </Box>
      </Dialog>
      {pendingAction && <UnsavedChangesDialog reload={pendingAction === 'reload'} onKeep={() => setPendingAction(undefined)} onDiscard={() => { if (pendingAction === 'reload') onReload(); else onClose(); }} />}
    </>
  );
}

export function ManagedUserDialog({ userId, currentUserId, onClose, onUpdated, onNavigationGuardChange }: { userId: string; currentUserId: string; onClose: () => void; onUpdated: (user: ManagedUser, message: string) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const [user, setUser] = useState<ManagedUser>();
  const [draft, setDraft] = useState<UpdateUserInput>();
  const [libraries, setLibraries] = useState<Library[]>();
  const [loading, setLoading] = useState(true);
  const [librariesLoading, setLibrariesLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [librariesError, setLibrariesError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [passwordReviewRequired, setPasswordReviewRequired] = useState(false);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [libraryRevision, setLibraryRevision] = useState(0);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [resettingPassword, setResettingPassword] = useState(false);
  const [passwordDraftState, setPasswordDraftState] = useState({ dirty: false, busy: false });
  const [pendingAction, setPendingAction] = useState<'close' | 'reload'>();
  const [notice, setNotice] = useState('');
  const dirty = Boolean(user && draft && draftKey(draft) !== draftKey(inputFor(user)));
  const blocked = isRevisionConflict(error) || isUnknownOutcome(error);
  const disabled = busy || loading;
  useUserDraftNavigation(dirty || (resettingPassword && passwordDraftState.dirty), busy || (resettingPassword && passwordDraftState.busy), onNavigationGuardChange);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError(null);
    void adminApi.getUser(userId, { signal: controller.signal })
      .then((result) => { setUser(result.User); setDraft(inputFor(result.User)); setError(null); setPasswordReviewRequired(false); setNotice(''); })
      .catch((cause: unknown) => { if (!isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [userId, reloadRevision]);

  useEffect(() => {
    const controller = new AbortController();
    setLibrariesLoading(true);
    setLibrariesError(null);
    void adminApi.getLibraries({ signal: controller.signal })
      .then((result) => setLibraries(result.Items))
      .catch((cause: unknown) => { if (!isAbortError(cause)) setLibrariesError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLibrariesLoading(false); });
    return () => controller.abort();
  }, [libraryRevision]);

  function reload() {
    setPendingAction(undefined);
    setResettingPassword(false);
    setReloadRevision((value) => value + 1);
    setLibraryRevision((value) => value + 1);
  }

  function requestAction(action: 'close' | 'reload') {
    if (inFlight.current) return;
    if (dirty) setPendingAction(action);
    else if (action === 'reload') reload();
    else onClose();
  }

  function change<K extends keyof UpdateUserInput>(key: K, value: UpdateUserInput[K]) {
    setDraft((current) => current ? { ...current, [key]: value } : current);
    setNotice('');
  }

  function changePolicy<K extends keyof UserPolicy>(key: K, value: UserPolicy[K]) {
    setDraft((current) => current ? { ...current, Policy: { ...current.Policy, [key]: value } } : current);
    setNotice('');
  }

  function saved(result: UserMutationResponse, password: boolean) {
    if (result.CurrentSessionRevoked) return;
    setUser(result.User);
    setDraft(inputFor(result.User));
    setError(null);
    setPasswordReviewRequired(false);
    setResettingPassword(false);
    const message = password ? `Password reset for ${result.User.Name}. Existing sign-ins have ended.` : `User ${result.User.Name} updated.`;
    setNotice(message);
    onUpdated(result.User, message);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || !draft || !dirty || blocked || loading || !draft.Name.trim()) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    setNotice('');
    try {
      const result = await adminApi.updateUser(userId, { ...draft, Name: draft.Name.trim() });
      saved(result, false);
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  const unavailableFolderIds = libraries && draft ? draft.Policy.EnabledFolders.filter((id) => !libraries.some((library) => library.Id === id)) : [];
  const librarySummary = draft?.IsAdministrator ? 'All libraries (administrator)' : draft?.Policy.EnableAllFolders ? 'All current and future libraries' : `${draft?.Policy.EnabledFolders.length ?? 0} selected ${draft?.Policy.EnabledFolders.length === 1 ? 'library' : 'libraries'}`;
  const playbackSettings: { key: 'EnableMediaPlayback' | 'EnablePlaybackRemuxing' | 'EnableAudioPlaybackTranscoding' | 'EnableVideoPlaybackTranscoding'; label: string; description: string }[] = [
    { key: 'EnableMediaPlayback', label: 'Media playback', description: 'Allow this account to play media in compatible clients.' },
    { key: 'EnablePlaybackRemuxing', label: 'Remuxing', description: 'Allow changing the media container without converting the video.' },
    { key: 'EnableAudioPlaybackTranscoding', label: 'Audio transcoding', description: 'Allow converting audio when a client needs another format.' },
    { key: 'EnableVideoPlaybackTranscoding', label: 'Video transcoding', description: 'Allow converting video when a client needs another format or quality.' },
  ];

  return (
    <>
      <Dialog open fullWidth maxWidth="md" fullScreen={fullScreen} onClose={() => requestAction('close')} aria-labelledby="manage-user-title" slotProps={{ paper: { sx: { ...(fullScreen ? { borderRadius: 0 } : {}), maxWidth: fullScreen ? undefined : 820 } } }}>
        <Box component="form" onSubmit={submit} aria-busy={busy || loading} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: fullScreen ? '100%' : undefined, overflow: 'hidden' }}>
          <DialogTitle id="manage-user-title" sx={{ px: { xs: 2.5, sm: 3 }, pt: 3, pb: 1 }}><Typography component="span" variant="h3">Manage user</Typography></DialogTitle>
          <DialogContent sx={{ px: { xs: 2.5, sm: 3 }, pt: '8px !important' }}>
            <Stack spacing={3}>
              {loadError != null && <ErrorNotice error={loadError} retry={() => requestAction('reload')} />}
              {!draft && loading && <Stack role="status" aria-label="Loading user details" spacing={2}><Skeleton variant="rounded" height={116} /><Skeleton height={70} /><Skeleton height={90} /><Skeleton height={150} /></Stack>}
              {!draft && !loading && loadError != null && <Typography variant="body2" color="text.secondary">Reload this user to view and edit account details.</Typography>}
              {draft && user && (
                <>
                  <Paper component="section" aria-label="Permission overview" variant="outlined" sx={{ p: 2.5, bgcolor: colors.canvas, borderLeft: `3px solid ${colors.sea}` }}>
                    <Stack direction="row" sx={{ alignItems: 'flex-start', justifyContent: 'space-between', gap: 1, mb: 2 }}><Box sx={{ minWidth: 0 }}><Typography variant="overline" color="text.secondary">Permission overview</Typography><Typography variant="h3" component="h2" sx={{ overflowWrap: 'anywhere' }}>{draft.Name || 'Unnamed user'}</Typography></Box><Chip size="small" variant="outlined" color={draft.IsDisabled ? 'default' : 'success'} label={draft.IsDisabled ? 'Disabled' : 'Active'} /></Stack>
                    <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1.5fr 1fr' }, gap: { xs: 1.5, sm: 2 } }}>
                      {[['Account', draft.IsAdministrator ? 'Administrator' : 'Member'], ['Library access', librarySummary], ['Playback', draft.Policy.EnableMediaPlayback ? 'Allowed' : 'Blocked']].map(([label, value]) => <Box key={label}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, fontWeight: 650 }}>{value}</Typography></Box>)}
                    </Box>
                    {dirty && <Typography variant="caption" color="primary.dark" sx={{ display: 'block', mt: 2 }}>Preview of your unsaved changes</Typography>}
                  </Paper>
                  <MutationNotice error={error} password={passwordReviewRequired} onReload={() => requestAction('reload')} />
                  {notice && <Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert>}
                  <Section title="Account" description="Choose the account name and who can administer the server.">
                    <Stack spacing={2.5}>
                      <TextField id="managed-user-name" name="Name" autoFocus required fullWidth label="Username" value={draft.Name} onChange={(event) => change('Name', event.target.value)} disabled={disabled} autoComplete="off" error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
                      <PermissionSwitch id="managed-user-administrator" label="Administrator access" description="Administrators can manage this server and every user, and access every library." checked={draft.IsAdministrator} disabled={disabled} onChange={(value) => change('IsAdministrator', value)} error={fieldError(error, 'IsAdministrator')} />
                      {user.Id === currentUserId && user.IsAdministrator && !draft.IsAdministrator && <Alert severity="warning">Removing your administrator access will end your current sign-in. An administrator must restore access before you can use this dashboard again.</Alert>}
                    </Stack>
                  </Section>
                  <Divider />
                  <Section title="Library access" description="Choose the libraries this account can browse and play.">
                    <Stack spacing={2}>
                      {draft.IsAdministrator && <Alert severity="info">Administrators can access every library. The choices below are saved for member access and take effect if administrator access is removed.</Alert>}
                      <PermissionSwitch id="managed-user-all-libraries" label="All libraries" description="Include every current library and any libraries added later." checked={draft.Policy.EnableAllFolders} disabled={disabled} onChange={(value) => changePolicy('EnableAllFolders', value)} error={fieldError(error, 'Policy.EnableAllFolders')} />
                      {(!draft.Policy.EnableAllFolders || unavailableFolderIds.length > 0 || Boolean(fieldError(error, 'Policy.EnabledFolders'))) && (
                        <Box>
                          {draft.Policy.EnableAllFolders && <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>These saved selections apply when All libraries is turned off. Remove any unavailable libraries to keep this configuration valid.</Typography>}
                          {librariesError != null && <ErrorNotice error={librariesError} retry={() => setLibraryRevision((value) => value + 1)} />}
                          {librariesLoading && !libraries && <Stack role="status" aria-label="Loading available libraries"><Skeleton height={40} /><Skeleton height={40} /></Stack>}
                          {libraries && libraries.length === 0 && <Typography variant="body2" color="text.secondary">{draft.Policy.EnableAllFolders ? 'No current libraries are available. Remove unavailable saved selections below.' : 'No libraries are available. This selection gives members no library access until a library is created and selected.'}</Typography>}
                          {libraries && <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, columnGap: 2 }}>{[...libraries.map((library) => ({ id: library.Id, name: library.Name })), ...unavailableFolderIds.map((id) => ({ id, name: `Unavailable library (${id})` }))].map((library) => <FormControlLabel key={library.id} sx={{ m: 0, minWidth: 0, '& .MuiFormControlLabel-label': { overflowWrap: 'anywhere', minWidth: 0 } }} control={<Checkbox checked={draft.Policy.EnabledFolders.includes(library.id)} disabled={disabled || librariesLoading || librariesError != null} onChange={(event) => changePolicy('EnabledFolders', event.target.checked ? [...draft.Policy.EnabledFolders, library.id] : draft.Policy.EnabledFolders.filter((id) => id !== library.id))} />} label={<Typography variant="body2">{library.name}</Typography>} />)}</Box>}
                          {!draft.Policy.EnableAllFolders && libraries && libraries.length > 0 && draft.Policy.EnabledFolders.length === 0 && <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>No libraries selected. Members with this configuration cannot access any library.</Typography>}
                        </Box>
                      )}
                      {fieldError(error, 'Policy.EnabledFolders') && <Typography variant="body2" color="error.main">{fieldError(error, 'Policy.EnabledFolders')}</Typography>}
                    </Stack>
                  </Section>
                  <Divider />
                  <Section title="Playback permissions" description="Control the playback methods available to this account.">
                    <Stack spacing={2}>
                      {playbackSettings.map((setting) => <PermissionSwitch key={setting.key} id={`managed-user-${setting.key}`} label={setting.label} description={setting.description} checked={draft.Policy[setting.key]} disabled={disabled} onChange={(value) => changePolicy(setting.key, value)} error={fieldError(error, `Policy.${setting.key}`)} />)}
                      {!draft.Policy.EnableMediaPlayback && <Typography variant="body2" color="text.secondary">Remuxing and transcoding settings are retained and apply when media playback is enabled.</Typography>}
                    </Stack>
                  </Section>
                  <Divider />
                  <Section title="Account security" description="Manage sign-in access and the account password.">
                    <Stack spacing={2.5}>
                      <PermissionSwitch id="managed-user-disabled" label="Disable account" description="Disabling ends existing sign-ins and prevents new ones. Re-enabling requires the user to sign in again." checked={draft.IsDisabled} disabled={disabled} onChange={(value) => change('IsDisabled', value)} error={fieldError(error, 'IsDisabled')} />
                      {!user.IsDisabled && draft.IsDisabled && <Alert severity="warning">{user.Id === currentUserId ? 'Saving will end your current sign-in and disable this account.' : 'Saving will end existing sign-ins for this account and prevent it from signing in.'}</Alert>}
                      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between', gap: 2, pt: 1 }}><Box><Typography variant="body2" sx={{ fontWeight: 650 }}>Password {user.HasPassword ? 'is set' : 'is not set'}</Typography><Typography variant="body2" color="text.secondary">A reset ends existing sign-ins for this account.</Typography></Box><Button variant="outlined" onClick={() => setResettingPassword(true)} disabled={disabled || dirty || blocked} startIcon={<LockResetRounded />} sx={{ flexShrink: 0 }}>Reset password</Button></Stack>
                      {dirty && <Typography variant="caption" color="text.secondary">Save or discard your account changes before resetting the password.</Typography>}
                    </Stack>
                  </Section>
                </>
              )}
            </Stack>
          </DialogContent>
          <DialogActions sx={{ px: { xs: 2.5, sm: 3 }, py: 2, borderTop: 1, borderColor: 'divider', gap: 1, flexWrap: 'wrap' }}>
            <Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{loading ? 'Loading user...' : dirty ? 'Unsaved changes' : user ? 'All changes saved' : ''}</Typography>
            <Button onClick={() => requestAction('close')} disabled={busy} color="secondary">Close</Button>
            <Button type="submit" variant="contained" disabled={disabled || !draft?.Name.trim() || !dirty || blocked} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving changes...' : 'Save changes'}</Button>
          </DialogActions>
        </Box>
      </Dialog>
      {pendingAction && <UnsavedChangesDialog reload={pendingAction === 'reload'} onKeep={() => setPendingAction(undefined)} onDiscard={() => { if (pendingAction === 'reload') reload(); else onClose(); }} />}
      {resettingPassword && user && <ResetPasswordDialog user={user} isCurrentUser={user.Id === currentUserId} onClose={() => setResettingPassword(false)} onReload={reload} onReset={(result) => saved(result, true)} onReviewRequired={(cause) => { setError(cause); setPasswordReviewRequired(true); }} onDraftStateChange={setPasswordDraftState} />}
    </>
  );
}
