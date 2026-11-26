import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Radio, RadioGroup, Skeleton, Stack, Switch, TextField, Typography, Tabs, Tab } from './ui';
import { DeleteOutlineRounded, LockResetRounded, RefreshRounded, SaveOutlined } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { DeleteUserResponse, Library, ManagedUser, UpdateUserInput, User, UserMutationResponse } from './api';
import { ErrorNotice } from './components';
import { UserPolicyFields, UserPolicyLimits } from './UserPolicyFields';
import { FeatureAccessFields } from './FeatureAccessFields';
import { LocalCredentialsDialog } from './LocalCredentialsDialog';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import { configuredPlaybackPermission, draftFromUserPolicy, parseUserPolicyDraft } from './userPolicy';
import type { UserPolicyDraft } from './userPolicy';
import { fieldError, PasswordField } from './formFields';
import { useTranslation } from './i18n';
import { LoginTimestamp, UserCreatedDate } from './UserActivitySummary';
import type { LoginDate } from './UserActivitySummary';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { boundedUserRequest, isUnknownUserWrite, isUserRevisionConflict } from './UserBatchDialog';
import { UserLoginSessions } from './UserLoginSessions';
import './access-pages.css';
import './user-activity.css';

type ManagedUserDraft = Omit<UpdateUserInput, 'Policy'> & { Policy: UserPolicyDraft };
type Mutation = { operation: 'settings' | 'password' | 'delete'; error: unknown };
type Confirmation = 'role' | 'disable' | 'password';
const tabs = [['permissions', 'Permissions'], ['profile', 'Profile'], ['devices', 'Login devices'], ['password', 'Password']] as const;
type UserTab = typeof tabs[number][0];

export const maxUserPasswordBytes = 72;
export const passwordByteLength = (value: string) => new TextEncoder().encode(value).length;
export const userInitial = (name: string) => (Array.from(name.trim())[0] ?? '?').toUpperCase();

const playbackSettings = [
  { key: 'EnableMediaPlayback', label: 'Media playback', description: 'Play media must also be checked in Feature access.' },
  { key: 'EnablePlaybackRemuxing', label: 'Remuxing' },
  { key: 'EnableAudioPlaybackTranscoding', label: 'Audio transcoding' },
  { key: 'EnableVideoPlaybackTranscoding', label: 'Video transcoding' },
  { key: 'EnableContentDownloading', label: 'Download media', description: 'Download original media must also be checked in Feature access.' },
] as const;

function inputFor(user: ManagedUser): ManagedUserDraft {
  return { Revision: user.Revision, Name: user.Name, IsAdministrator: user.IsAdministrator, IsDisabled: user.IsDisabled, Policy: draftFromUserPolicy(user.Policy) };
}

function draftKey(input: ManagedUserDraft): string {
  const policy = parseUserPolicyDraft(input.Policy).policy ?? input.Policy;
  return JSON.stringify({ ...input, Policy: { ...policy, EnabledFolders: [...policy.EnabledFolders].sort() } });
}

export function RoleChoices({ administrator, disabled, memberLocked = false, administratorLocked = false, onChange }: { administrator: boolean; disabled: boolean; memberLocked?: boolean; administratorLocked?: boolean; onChange: (value: boolean) => void }) {
  return <div>
    <Typography component="h3" className="users-section-title">Role</Typography>
    <RadioGroup className="users-role-choices" value={administrator ? 'administrator' : 'member'} aria-label="Role" onChange={(_event, value: string) => onChange(value === 'administrator')}>
      <FormControlLabel className="users-role-choice" control={<Radio value="member" disabled={disabled || memberLocked} slotProps={{ input: { 'aria-label': 'Member', 'aria-describedby': 'user-role-member-description' } }} />} label={<><strong>Member</strong><small id="user-role-member-description">Use media services with the configured permissions.</small></>} />
      <FormControlLabel className="users-role-choice" control={<Radio value="administrator" disabled={disabled || administratorLocked} slotProps={{ input: { 'aria-label': 'Administrator', 'aria-describedby': 'user-role-administrator-description' } }} />} label={<><strong>Administrator</strong><small id="user-role-administrator-description">Manage the server and all users, and access every library.</small></>} />
    </RadioGroup>
  </div>;
}

export function UnsavedChangesDialog({ reload, onKeep, onDiscard }: { reload: boolean; onKeep: () => void; onDiscard: () => void }) {
  return <Dialog open onClose={onKeep} maxWidth="xs" aria-label="Discard unsaved changes?" aria-describedby="discard-user-description">
    <DialogTitle id="discard-user-title">Discard unsaved changes?</DialogTitle>
    <DialogContent><Typography id="discard-user-description" color="text.secondary">{reload ? 'Reloading replaces your draft with the latest saved user. Your unsaved changes will be lost.' : 'Your changes have not been saved. Keep editing to finish them, or discard this draft.'}</Typography></DialogContent>
    <DialogActions><Button onClick={onKeep} autoFocus color="secondary">Keep editing</Button><Button onClick={onDiscard} color="error" variant="contained">{reload ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions>
  </Dialog>;
}

function MutationNotice({ error, password = false, deleting = false, onReload }: { error: unknown; password?: boolean; deleting?: boolean; onReload: () => void }) {
  if (error == null) return null;
  if (deleting) {
    const message = error instanceof ApiError && error.code === 'last_administrator'
      ? 'Keep at least one enabled administrator. Give another active user administrator access before deleting this account.'
      : isUserRevisionConflict(error)
        ? 'This user changed after you opened it. Reload the latest user and review the account before deciding whether to delete it.'
        : error instanceof ApiError && error.status === 404
          ? 'This user could not be found. Reload the latest user to check whether the account still exists.'
          : isUnknownUserWrite(error)
            ? 'The deletion response could not be confirmed. The account may already have been deleted. Reload the latest user before deciding what to do next.'
            : undefined;
    return <Stack spacing={1}>
      {message ? <Alert severity={error instanceof ApiError && error.code === 'last_administrator' ? 'error' : 'warning'}>
        {message}<UserMutationRequestId error={error} />
      </Alert> : <ErrorNotice error={error} />}
      <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={onReload} sx={{ alignSelf: 'flex-start' }}>Reload latest user</Button>
    </Stack>;
  }
  if (isUserRevisionConflict(error) || isUnknownUserWrite(error)) {
    return <Alert severity="warning">
      <Typography variant="body2">{isUserRevisionConflict(error)
        ? 'This user changed after you opened it. Your draft is still here. Reload the latest user and review it before saving again.'
        : password
          ? 'The response could not be confirmed. The password may already have changed and existing sign-ins may have ended. Reload this user before deciding whether to reset the password again.'
          : 'The response could not be confirmed. Your changes may already have been saved. Reload this user and check the saved details before trying again.'}</Typography>
      <UserMutationRequestId error={error} />
      <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={onReload} sx={{ mt: 1, ml: -1 }}>Reload latest user</Button>
    </Alert>;
  }
  if (error instanceof ApiError && error.code === 'last_administrator') return <Alert severity="error">Keep at least one enabled administrator. Give another active user administrator access before disabling this account or removing its administrator access.</Alert>;
  return <ErrorNotice error={error} />;
}

function UserMutationRequestId({ error }: { error: unknown }) {
  return error instanceof ApiError && error.requestId ? <Typography variant="caption" component="div" sx={{ mt: 0.5 }}>Request ID: <span className="mono" translate="no">{error.requestId}</span></Typography> : null;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const { t } = useTranslation();
  return <section aria-label={t(title)} className="users-sheet-section"><h3 className="users-section-title">{t(title)}</h3>{children}</section>;
}

function PermissionSwitch({ id, label, description, translateLabel = true, checked, disabled, error, onChange }: { id: string; label: string; description?: string; translateLabel?: boolean; checked: boolean; disabled: boolean; error?: string; onChange: (checked: boolean) => void }) {
  const { t } = useTranslation();
  return <FormControlLabel className="users-setting-row" labelPlacement="start" control={<Switch checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} slotProps={{ input: { 'aria-labelledby': `${id}-label`, 'aria-describedby': error || description ? `${id}-help` : undefined } }} />} label={<>
    <span id={`${id}-label`} translate={translateLabel ? undefined : 'no'}>{label}</span>
    {(error || description) && <small id={`${id}-help`} className={error ? 'users-help users-field-error' : 'users-help'}>{error ?? t(description ?? '')}</small>}
  </>} />;
}

function DeleteUserDialog({ user, isCurrentUser, onClose, onDeleted, onReload, onReviewRequired, onBusyChange }: { user: ManagedUser; isCurrentUser: boolean; onClose: () => void; onDeleted: (result: DeleteUserResponse) => void; onReload: () => void; onReviewRequired: (error: unknown) => void; onBusyChange: (busy: boolean) => void }) {
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const attempted = useRef(false);
  const cancel = useRef<HTMLButtonElement>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => { cancel.current?.focus(); }, []);

  function close() {
    if (!inFlight.current) onClose();
  }

  async function remove() {
    if (inFlight.current || attempted.current) return;
    attempted.current = true;
    inFlight.current = true;
    setBusy(true);
    onBusyChange(true);
    try {
      onDeleted(await boundedUserRequest((signal) => adminApi.deleteUser(user.Id, { Revision: user.Revision }, { signal })));
    } catch (cause) {
      setError(cause);
      onReviewRequired(cause);
    } finally {
      inFlight.current = false;
      setBusy(false);
      onBusyChange(false);
    }
  }

  return <Dialog open data-layout="modal" onClose={close} maxWidth="xs" aria-label="Delete user" aria-describedby="delete-user-description">
    <DialogTitle id="delete-user-title">Delete user</DialogTitle>
    <DialogContent aria-busy={busy}>
      <Stack spacing={2}>
        <Typography variant="body2">Delete <strong translate="no">{user.Name}</strong>?</Typography>
        <Typography id="delete-user-description" variant="body2" color="text.secondary">This permanently deletes the account, ends its sign-ins, and removes its personal playback history. Media files are kept.</Typography>
        {isCurrentUser && <Alert severity="warning">You are deleting your own account. You will be signed out as soon as it is deleted.</Alert>}
        <MutationNotice error={error} deleting onReload={onReload} />
      </Stack>
    </DialogContent>
    <DialogActions>
      <Button ref={cancel} onClick={close} disabled={busy} color="secondary">Cancel</Button>
      <Button variant="contained" color="error" onClick={() => void remove()} disabled={busy || error != null} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <DeleteOutlineRounded />}>{busy ? 'Deleting user...' : 'Delete user'}</Button>
    </DialogActions>
  </Dialog>;
}

export function ManagedUserDialog({ userId, initialName, currentUserId, enabledAdministratorCount, loginDate, loginLoading, loginObservedAt, onClose, onObserved, onLoginObserved, onUpdated, onRemoved, onNavigationGuardChange }: {
  userId: string; initialName: string; currentUserId: string; enabledAdministratorCount?: number;
  loginDate: LoginDate; loginLoading: boolean; loginObservedAt?: number;
  onClose: () => void; onObserved: (user: User) => void; onLoginObserved: (userId: string, createdAt: string) => void; onUpdated: (user: ManagedUser, message: string) => void;
  onRemoved: (userId: string, message: string, severity: 'success' | 'info') => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<UserTab>('permissions');
  const [user, setUser] = useState<ManagedUser>();
  const [draft, setDraft] = useState<ManagedUserDraft>();
  const [libraries, setLibraries] = useState<Library[]>();
  const [loading, setLoading] = useState(true);
  const [librariesLoading, setLibrariesLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [librariesError, setLibrariesError] = useState<unknown>(null);
  const [mutation, setMutation] = useState<Mutation>();
  const [reloadRevision, setReloadRevision] = useState(0);
  const [libraryRevision, setLibraryRevision] = useState(0);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [confirming, setConfirming] = useState<Confirmation>();
  const [deletingUser, setDeletingUser] = useState(false);
  const [editingCredentials, setEditingCredentials] = useState(false);
  const [editingAvatar, setEditingAvatar] = useState(false);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [loginBusy, setLoginBusy] = useState(false);
  const [password, setPassword] = useState('');
  const [passwordConfirmation, setPasswordConfirmation] = useState('');
  const [pendingAction, setPendingAction] = useState<'close' | 'reload'>();
  const [notice, setNotice] = useState('');
  const parsedPolicy = draft ? parseUserPolicyDraft(draft.Policy) : undefined;
  const error = mutation?.operation === 'settings' ? mutation.error : undefined;
  const folderError = parsedPolicy?.errors['Policy.EnabledFolders'] ?? fieldError(error, 'Policy.EnabledFolders');
  const dirty = Boolean(user && draft && draftKey(draft) !== draftKey(inputFor(user)));
  const passwordDirty = Boolean(password || passwordConfirmation);
  const blocked = mutation?.operation === 'delete' || isUserRevisionConflict(mutation?.error) || isUnknownUserWrite(mutation?.error);
  const working = busy || deleteBusy || loginBusy;
  const disabled = working || loading || loadError != null;
  const lastAdministrator = Boolean(user?.IsAdministrator && !user.IsDisabled && enabledAdministratorCount === 1);
  const promotionLocked = Boolean(user && !user.IsAdministrator && !user.HasPassword);
  const accountChangeLocked = Boolean(user && draft && ((lastAdministrator && (!draft.IsAdministrator || draft.IsDisabled)) || (promotionLocked && draft.IsAdministrator)));
  const roleChanged = user?.IsAdministrator !== draft?.IsAdministrator;
  const disabling = Boolean(user && draft && !user.IsDisabled && draft.IsDisabled);
  const isSelf = user?.Id === currentUserId;
  const saveDisabled = disabled || !draft?.Name.trim() || !parsedPolicy?.policy || !dirty || passwordDirty || blocked || accountChangeLocked;
  const passwordTooLong = passwordByteLength(password) > maxUserPasswordBytes;
  const passwordMismatch = Boolean(passwordConfirmation && password !== passwordConfirmation);
  const passwordDisabled = disabled || dirty || blocked;
  const cannotResetPassword = passwordDisabled || !password || password !== passwordConfirmation || passwordTooLong;
  const passwordFieldError = fieldError(mutation?.operation === 'password' ? mutation.error : undefined, 'Password');
  useUserDraftNavigation(dirty || passwordDirty, working, onNavigationGuardChange);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError(null);
    void adminApi.getUser(userId, { signal: controller.signal }).then(({ User: result }) => {
      if (controller.signal.aborted) return;
      if (result.Id !== userId) throw new ApiError('The selected user could not be verified.', { code: 'invalid_response' });
      setUser(result);
      setDraft(inputFor(result));
      setMutation(undefined);
      setNotice('');
      onObserved(result);
    }).catch((cause) => {
      if (controller.signal.aborted || isAbortError(cause)) return;
      if (cause instanceof ApiError && cause.status === 404) onRemoved(userId, 'This user is no longer available.', 'info');
      else setLoadError(cause);
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [userId, reloadRevision, onObserved, onRemoved]);

  useEffect(() => {
    const controller = new AbortController();
    setLibrariesLoading(true);
    setLibrariesError(null);
    void adminApi.getLibraries({ signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) setLibraries(result.Items);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setLibrariesError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLibrariesLoading(false); });
    return () => controller.abort();
  }, [libraryRevision]);

  function discard() {
    if (user) setDraft(inputFor(user));
    setPassword('');
    setPasswordConfirmation('');
  }

  function reload() {
    discard();
    setLoading(true);
    setPendingAction(undefined);
    setDeletingUser(false);
    setConfirming(undefined);
    setReloadRevision((value) => value + 1);
    setLibraryRevision((value) => value + 1);
  }

  function requestAction(action: 'close' | 'reload') {
    if (inFlight.current || working || editingAvatar || editingCredentials) return;
    if (dirty || passwordDirty) setPendingAction(action);
    else if (action === 'reload') reload();
    else onClose();
  }

  function change<K extends keyof ManagedUserDraft>(key: K, value: ManagedUserDraft[K]) {
    setDraft((current) => current && ({ ...current, [key]: value }));
    setNotice('');
  }

  function changePolicy<K extends keyof UserPolicyDraft>(key: K, value: UserPolicyDraft[K]) {
    setDraft((current) => current && ({ ...current, Policy: { ...current.Policy, [key]: value } }));
    setNotice('');
  }

  function changeLibrary(id: string, checked: boolean) {
    setDraft((current) => {
      if (!current || current.IsAdministrator) return current;
      const folders = current.Policy.EnableAllFolders ? [...new Set([...(libraries ?? []).map((library) => library.Id), ...current.Policy.EnabledFolders])] : current.Policy.EnabledFolders;
      return { ...current, Policy: { ...current.Policy, EnableAllFolders: false, EnabledFolders: checked ? [...folders, id] : folders.filter((value) => value !== id) } };
    });
    setNotice('');
  }

  function saved(result: UserMutationResponse, resettingPassword: boolean) {
    if (result.CurrentSessionRevoked) return;
    if (result.User.Id !== userId) throw new ApiError('The selected user could not be verified.', { code: 'invalid_response' });
    setUser(result.User);
    setDraft(inputFor(result.User));
    setPassword('');
    setPasswordConfirmation('');
    setMutation(undefined);
    const message = t(resettingPassword ? 'Password reset for {name}. Existing sign-ins have ended.' : 'Saved settings for {name}.', { name: result.User.Name });
    setNotice(message);
    onUpdated(result.User, message);
  }

  async function write(resettingPassword: boolean) {
    if (inFlight.current || !user || !draft || (resettingPassword ? cannotResetPassword : saveDisabled)) return;
    inFlight.current = true;
    setBusy(true);
    setMutation(undefined);
    setNotice('');
    try {
      const result = await boundedUserRequest((signal) => resettingPassword
        ? adminApi.resetUserPassword(userId, { Revision: user.Revision, Password: password }, { signal })
        : adminApi.updateUser(userId, { ...draft, Name: draft.Name.trim(), Policy: parsedPolicy!.policy! }, { signal }));
      saved(result, resettingPassword);
    } catch (cause) {
      if (!isAbortError(cause)) setMutation({ operation: resettingPassword ? 'password' : 'settings', error: cause });
    } finally {
      inFlight.current = false;
      setConfirming(undefined);
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || saveDisabled || confirming || deletingUser || editingAvatar || editingCredentials) return;
    if (roleChanged || disabling) setConfirming(roleChanged ? 'role' : 'disable');
    else void write(false);
  }

  useEffect(() => {
    function saveShortcut(event: KeyboardEvent) {
      if ((!event.ctrlKey && !event.metaKey) || event.key.toLowerCase() !== 's') return;
      event.preventDefault();
      const form = document.querySelector<HTMLFormElement>('#managed-user-form');
      const topDialog = Array.from(document.querySelectorAll('dialog:modal')).at(-1);
      if (form && topDialog?.contains(form)) form.requestSubmit();
    }
    document.addEventListener('keydown', saveShortcut);
    return () => document.removeEventListener('keydown', saveShortcut);
  }, []);

  function selectTab(value: UserTab, focus = false) {
    if (working) return;
    setActiveTab(value);
    if (focus) document.getElementById(`user-tab-${value}`)?.focus();
  }

  const unavailableFolderIds = libraries && draft ? draft.Policy.EnabledFolders.filter((id) => !libraries.some((library) => library.Id === id)) : [];
  const playbackPermission = draft ? configuredPlaybackPermission(draft.Policy) : undefined;
  const roleHelp = promotionLocked ? t('This account has no password. Set one in Password before making this account an administrator.')
    : lastAdministrator ? t('This is the only enabled administrator and cannot be changed to a member.')
      : roleChanged && draft && user ? t(draft.IsAdministrator
        ? draft.IsDisabled ? 'Once the account is enabled, {name} can sign in to the dashboard and manage the server, all users, and all settings.' : 'After saving, {name} can sign in to the dashboard and manage the server, all users, and all settings.'
        : draft.IsDisabled ? 'After saving, {name} will have member permissions when the account is enabled again.' : 'After saving, {name} will lose administrator logins. Any existing client logins stay active with member permissions.', { name: user.Name })
        : undefined;
  const accountName = user?.Name ?? initialName;
  const formDisabled = disabled || passwordDirty;
  const confirmationName = { name: user?.Name ?? initialName };
  const confirmTitle = confirming === 'password' ? t('Reset password?') : confirming === 'disable' ? t('Disable {name}?', confirmationName)
    : t(draft?.IsAdministrator ? 'Make {name} an administrator?' : 'Change {name} to a member?', confirmationName);
  const confirmAction = confirming === 'password' ? t('Reset password') : confirming === 'disable' ? t('Disable account')
    : t(draft?.IsAdministrator ? 'Make administrator' : isSelf && !disabling ? 'Change to member and sign out' : 'Change to member');

  return <>
    <Dialog open maxWidth="sm" onClose={() => requestAction('close')} aria-label={t('Manage user {name}', { name: accountName })} className="access-user-sheet users-sheet">
      <Box component="form" id="managed-user-form" noValidate onSubmit={submit} aria-busy={working || loading} className="access-sheet-form">
        <DialogTitle id="manage-user-title">
          <span className={`access-avatar${(draft?.IsAdministrator ?? user?.IsAdministrator) ? ' access-avatar-admin' : ''}`} aria-hidden="true">{userInitial(accountName)}</span>
          <span className="users-sheet-title-copy"><span id="manage-user-eyebrow" className="access-sheet-eyebrow">{t('Manage user')}</span><strong id="manage-user-name" className="access-sheet-user-title" translate="no">{accountName}</strong></span>
        </DialogTitle>
        <Tabs value={activeTab} variant="scrollable" className="access-sheet-tabs users-sheet-tabs" aria-label="User settings" onChange={(_event, value: UserTab) => selectTab(value)}>
          {tabs.map(([value, label]) => <Tab key={value} value={value} label={label} disabled={working} id={`user-tab-${value}`} aria-controls={`user-panel-${value}`} />)}
        </Tabs>
        <DialogContent className="access-sheet-content users-sheet-content">
          <Stack spacing={2}>
            {loadError != null && <ErrorNotice error={loadError} retry={() => requestAction('reload')} />}
            {!draft && loading && <Stack role="status" aria-label="Loading user details" spacing={2}><Skeleton height={70} /><Skeleton height={90} /><Skeleton height={150} /></Stack>}
            {!draft && !loading && loadError != null && <Typography variant="body2" color="text.secondary">Reload this user to view and edit account details.</Typography>}
            {draft && user && <>
              <MutationNotice error={mutation?.error} password={mutation?.operation === 'password'} deleting={mutation?.operation === 'delete'} onReload={() => requestAction('reload')} />
              {notice && <Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert>}
              <div role="tabpanel" id="user-panel-permissions" aria-labelledby="user-tab-permissions" hidden={activeTab !== 'permissions'} className="users-permissions">
                <div>
                  <RoleChoices administrator={draft.IsAdministrator} disabled={formDisabled} memberLocked={lastAdministrator} administratorLocked={promotionLocked} onChange={(value) => change('IsAdministrator', value)} />
                  {roleHelp && <p className={roleChanged ? 'users-help users-warning' : 'users-help'}>{roleHelp}</p>}
                  {fieldError(error, 'IsAdministrator') && <p className="users-help users-field-error">{fieldError(error, 'IsAdministrator')}</p>}
                </div>
                <Section title="Library access">
                  {draft.IsAdministrator && <p className="users-help">Administrators can access every library. The settings below are kept and apply if the account becomes a member.</p>}
                  {librariesError != null && <ErrorNotice error={librariesError} retry={() => setLibraryRevision((value) => value + 1)} />}
                  <div className="users-setting-list">
                    <PermissionSwitch id="managed-user-all-libraries" label="All libraries" description="Include every current library and any libraries added later." checked={draft.Policy.EnableAllFolders} disabled={formDisabled || draft.IsAdministrator} onChange={(value) => changePolicy('EnableAllFolders', value)} error={fieldError(error, 'Policy.EnableAllFolders')} />
                    {librariesLoading && !libraries && <Stack className="users-list-notice" role="status" aria-label="Loading available libraries" spacing={1}><Skeleton height={32} /><Skeleton height={32} /></Stack>}
                    {libraries?.map((library, index) => <PermissionSwitch key={library.Id} id={`managed-user-library-${index}`} label={library.Name} translateLabel={false} checked={draft.Policy.EnableAllFolders || draft.Policy.EnabledFolders.includes(library.Id)} disabled={formDisabled || draft.IsAdministrator || librariesLoading || librariesError != null} onChange={(checked) => changeLibrary(library.Id, checked)} />)}
                  </div>
                  {libraries?.length === 0 && <p className="users-help">{t('No libraries are available.')}</p>}
                  {unavailableFolderIds.length > 0 && <Alert severity="warning" action={<Button size="small" color="inherit" disabled={formDisabled || librariesLoading || librariesError != null} onClick={() => changePolicy('EnabledFolders', draft.Policy.EnabledFolders.filter((id) => !unavailableFolderIds.includes(id)))}>Remove unavailable selections</Button>}>Some saved library selections are no longer available. Remove them to save changes.</Alert>}
                  {folderError && <p className="users-help users-field-error">{t(folderError)}</p>}
                </Section>
                <Section title="Playback and downloads">
                  <div className="users-setting-list">{playbackSettings.map((setting) => <PermissionSwitch key={setting.key} id={`managed-user-${setting.key}`} label={setting.label} description={'description' in setting ? setting.description : undefined} checked={draft.Policy[setting.key]} disabled={formDisabled} onChange={(value) => changePolicy(setting.key, value)} error={fieldError(error, `Policy.${setting.key}`)} />)}</div>
                  {playbackPermission && <div className="users-help"><span>{t(playbackPermission.allowed ? 'Configured playback is allowed.' : 'Configured playback is blocked.')}</span>{playbackPermission.blockers.map((reason) => <span key={reason}> {t(reason)}</span>)}</div>}
                </Section>
                <Section title="Limits">
                  <UserPolicyLimits policy={draft.Policy} disabled={formDisabled} error={error} errors={parsedPolicy?.errors ?? {}} onChange={changePolicy} />
                </Section>
                <details className="access-advanced-permissions"><summary>{t('Advanced permissions and parental controls')}</summary><Stack spacing={2} sx={{ pt: 2 }}><FeatureAccessFields restricted={draft.Policy.RestrictedFeatures} disabled={formDisabled} error={parsedPolicy?.errors['Policy.RestrictedFeatures'] ?? fieldError(error, 'Policy.RestrictedFeatures')} onChange={(value) => changePolicy('RestrictedFeatures', value)} /><UserPolicyFields policy={draft.Policy} disabled={formDisabled} error={error} errors={parsedPolicy?.errors ?? {}} onChange={changePolicy} /></Stack></details>
              </div>
              <div role="tabpanel" id="user-panel-profile" aria-labelledby="user-tab-profile" hidden={activeTab !== 'profile'} className="access-sheet-profile">
                <TextField id="managed-user-name" name="Name" required fullWidth label="Name" value={draft.Name} onChange={(event) => change('Name', event.target.value)} disabled={formDisabled} autoComplete="off" error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }} />
                <dl className="access-sheet-kv">
                  <div><dt>{t('Role')}</dt><dd>{t(user.IsAdministrator ? 'Administrator' : 'Member')} <Button size="small" onClick={() => selectTab('permissions', true)} disabled={working}>Change role</Button></dd></div>
                  <div><dt>{t('Status')}</dt><dd><span className="users-account-state" data-active={!user.IsDisabled}>{t(user.IsDisabled ? 'Disabled' : 'Active')}</span></dd></div>
                  <div><dt>{t('Last sign-in')}</dt><dd><LoginTimestamp value={loginDate} loading={loginLoading} observedAt={loginObservedAt} /></dd></div>
                  <div><dt>{t('Created')}</dt><dd><UserCreatedDate value={user.CreatedAt} /></dd></div>
                </dl>
                <div className="users-setting-list">
                  <PermissionSwitch id="managed-user-disabled" label="Disable account" description="Disabling ends existing sign-ins and prevents new ones. Re-enabling requires the user to sign in again." checked={draft.IsDisabled} disabled={formDisabled || lastAdministrator} onChange={(value) => change('IsDisabled', value)} error={fieldError(error, 'IsDisabled')} />
                </div>
                {lastAdministrator && <p className="users-help">{t('The last enabled administrator cannot be disabled.')}</p>}
                {disabling && <Alert severity="warning">{isSelf ? 'Saving will end your current sign-in and disable this account.' : 'Saving will end existing sign-ins for this account and prevent it from signing in.'}</Alert>}
                <Button variant="outlined" disabled={formDisabled || dirty || blocked} onClick={() => setEditingAvatar(true)} sx={{ justifySelf: 'start' }}>Manage avatar</Button>
                {dirty && <p className="users-help">{t('Save or discard account changes before editing the avatar.')}</p>}
              </div>
              <div role="tabpanel" id="user-panel-devices" aria-labelledby="user-tab-devices" hidden={activeTab !== 'devices'}>
                {activeTab === 'devices' && <>
                  {(dirty || passwordDirty || blocked) && <p className="users-help">{t(blocked ? 'Reload the latest user before ending logins.' : 'Save or discard your changes before ending logins.')}</p>}
                  <UserLoginSessions key={`${user.Id}:${user.Revision}`} user={user} loginDate={loginDate} observedAt={loginObservedAt} actionsDisabled={disabled || dirty || passwordDirty || blocked} onBusyChange={setLoginBusy} onLoginObserved={onLoginObserved} />
                </>}
              </div>
              <div role="tabpanel" id="user-panel-password" aria-labelledby="user-tab-password" hidden={activeTab !== 'password'}>
                <Stack spacing={1.5} onKeyDown={(event) => {
                  if (event.key === 'Enter' && event.target instanceof HTMLInputElement) { event.preventDefault(); if (!cannotResetPassword) setConfirming('password'); }
                }}>
                  <PasswordField id="reset-user-password" fullWidth label="New password" value={password} disabled={passwordDisabled} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" error={passwordTooLong || Boolean(passwordFieldError)} helperText={passwordTooLong ? 'Use at most 72 UTF-8 bytes for the password.' : passwordFieldError ?? 'Use a nonempty password of at most 72 UTF-8 bytes. Some characters use more than one byte.'} />
                  <PasswordField id="reset-user-confirm-password" fullWidth label="Confirm new password" value={passwordConfirmation} disabled={passwordDisabled} onChange={(event) => setPasswordConfirmation(event.target.value)} autoComplete="new-password" error={passwordMismatch} helperText={passwordMismatch ? 'The passwords do not match.' : 'Resetting the password ends all logins for this account.'} />
                  <Button variant="outlined" onClick={() => setConfirming('password')} disabled={cannotResetPassword} startIcon={<LockResetRounded />} sx={{ alignSelf: 'flex-start' }}>Reset password</Button>
                  <div className="users-password-secondary">
                    <Button variant="outlined" onClick={() => setEditingCredentials(true)} disabled={passwordDisabled || passwordDirty}>Manage local credentials</Button>
                    <p className="users-help">{t('Manage local-network sign-in and the separate client profile lock.')}</p>
                  </div>
                  <div className="users-password-secondary">
                    <Button variant="outlined" color="error" onClick={() => setDeletingUser(true)} disabled={passwordDisabled || passwordDirty || lastAdministrator} startIcon={<DeleteOutlineRounded />}>Delete user</Button>
                    <p className="users-help">{t(lastAdministrator ? 'Keep at least one enabled administrator.' : 'Permanently remove this account. Media files are kept.')}</p>
                  </div>
                  {dirty && <p className="users-help">{t('Save or discard your changes before changing credentials or deleting this user.')}</p>}
                </Stack>
              </div>
            </>}
          </Stack>
        </DialogContent>
        <DialogActions className="users-sheet-footer">
          <Typography variant="caption">{loading ? 'Loading user...' : blocked && !dirty && !passwordDirty ? 'Reload required before saving' : dirty || passwordDirty ? 'Unsaved changes' : user ? 'All changes saved' : ''}</Typography>
          {(dirty || passwordDirty) && <Button onClick={discard} disabled={disabled} color="secondary">Discard</Button>}
          <Button onClick={() => requestAction('close')} disabled={working} color="secondary">Close</Button>
          <Button type="submit" variant="contained" disabled={saveDisabled} startIcon={busy && confirming !== 'password' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy && confirming !== 'password' ? 'Saving changes...' : 'Save changes'}</Button>
        </DialogActions>
      </Box>
    </Dialog>
    {confirming && user && draft && <Dialog open role="alertdialog" maxWidth="xs" onClose={() => { if (!inFlight.current) setConfirming(undefined); }} aria-label={confirmTitle} aria-describedby="confirm-user-description">
      <DialogTitle id="confirm-user-title">{confirmTitle}</DialogTitle>
      <DialogContent>
        <Stack spacing={1.5}>
          <Typography id="confirm-user-description" variant="body2" color="text.secondary">{confirming === 'password' ? t('Resetting the password ends all logins for this account. The new password will be needed at the next sign-in.')
            : confirming === 'disable' ? t('Disabling ends all logins for this account and prevents new ones. Its permissions and playback history are kept.')
              : t(draft.IsAdministrator ? 'An administrator can sign in to the dashboard, manage the server, all users, and all settings, and access every library.' : draft.IsDisabled ? 'The account will have member permissions when enabled again.' : 'Administrator logins will end immediately. Client logins remain active and use the account’s member permissions.')}</Typography>
          {confirming === 'role' && draft.IsDisabled && <Alert severity="warning">{disabling ? 'Disabling also ends all client logins and prevents new logins.' : 'This account remains disabled and cannot sign in until re-enabled.'}</Alert>}
          {isSelf && <Alert severity="warning">{confirming === 'password' ? 'You will return to sign in and must use the new password.' : 'You are changing your own account. Saving signs you out immediately. Another administrator will need to manage the server.'}</Alert>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button autoFocus onClick={() => setConfirming(undefined)} color="secondary" disabled={busy}>Cancel</Button>
        <Button variant="contained" color={isSelf || disabling ? 'error' : 'primary'} onClick={() => void write(confirming === 'password')} disabled={busy || (confirming === 'password' ? cannotResetPassword : saveDisabled)}>{busy && confirming === 'password' ? 'Resetting password...' : confirmAction}</Button>
      </DialogActions>
    </Dialog>}
    {pendingAction && <UnsavedChangesDialog reload={pendingAction === 'reload'} onKeep={() => setPendingAction(undefined)} onDiscard={() => { if (pendingAction === 'reload') reload(); else onClose(); }} />}
    {deletingUser && user && <DeleteUserDialog user={user} isCurrentUser={isSelf} onClose={() => setDeletingUser(false)} onReload={() => requestAction('reload')} onDeleted={(result) => { if (!result.CurrentSessionRevoked) onRemoved(user.Id, t('User {name} deleted.', { name: user.Name }), 'success'); }} onReviewRequired={(cause) => { setMutation({ operation: 'delete', error: cause }); setNotice(''); }} onBusyChange={setDeleteBusy} />}
    {editingCredentials && user && <LocalCredentialsDialog userId={user.Id} userName={user.Name} hasPassword={user.HasPassword} isCurrentUser={isSelf} onClose={() => { setEditingCredentials(false); reload(); }} onChanged={() => setReloadRevision((value) => value + 1)} onNavigationGuardChange={onNavigationGuardChange} />}
    {editingAvatar && user && <ArtworkManagerDialog target={{ kind: 'users', id: user.Id, name: user.Name }} onClose={() => setEditingAvatar(false)} onNavigationGuardChange={onNavigationGuardChange} />}
  </>;
}
