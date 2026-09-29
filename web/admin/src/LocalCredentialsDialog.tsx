import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Skeleton, Stack, Switch, Typography, useMediaQuery } from '@mui/material';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { LocalCredentials, LocalCredentialsInput } from './api';
import { ErrorNotice } from './components';
import { fieldError, PasswordField } from './formFields';
import { colors, theme } from './theme';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

export function LocalCredentialsDialog({ userId, userName, hasPassword, isCurrentUser, onClose, onChanged, onNavigationGuardChange }: {
  userId: string; userName: string; hasPassword: boolean; isCurrentUser: boolean; onClose: () => void; onChanged: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const [saved, setSaved] = useState<LocalCredentials>();
  const [enabled, setEnabled] = useState(false);
  const [password, setPassword] = useState('');
  const [passwordConfirmation, setPasswordConfirmation] = useState('');
  const [pin, setPin] = useState('');
  const [pinConfirmation, setPinConfirmation] = useState('');
  const [clearPassword, setClearPassword] = useState(false);
  const [clearPin, setClearPin] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [revision, setRevision] = useState(0);
  const [confirming, setConfirming] = useState(false);
  const [notice, setNotice] = useState('');
  const [submitted, setSubmitted] = useState(false);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const passwordBytes = new TextEncoder().encode(password).length;
  const dirty = Boolean(saved && (enabled !== saved.EnableLocalPassword || password || passwordConfirmation || pin || pinConfirmation || clearPassword || clearPin));
  const passwordError = passwordBytes > 72 ? 'Use at most 72 UTF-8 bytes.' : fieldError(error, 'LocalPassword');
  const passwordConfirmationError = password !== passwordConfirmation && (passwordConfirmation || submitted) ? 'The passwords do not match.' : undefined;
  const pinError = pin && !/^[0-9]{4}$/.test(pin) ? 'Use exactly 4 ASCII digits.' : fieldError(error, 'ProfilePin');
  const pinConfirmationError = pin !== pinConfirmation && (pinConfirmation || submitted) ? 'The PINs do not match.' : undefined;
  const enabledError = enabled && !(password || (saved?.HasLocalPassword && !clearPassword)) ? 'Set a local password before enabling it.' : fieldError(error, 'EnableLocalPassword');
  const invalid = passwordBytes > 72 || password !== passwordConfirmation || Boolean(pin && (!/^[0-9]{4}$/.test(pin) || !hasPassword)) || pin !== pinConfirmation || Boolean(enabledError);
  const endsSignIns = Boolean(saved && (enabled !== saved.EnableLocalPassword || password || clearPassword));
  const disabled = loading || busy || review;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved local credentials and leave this page?');

  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setSaved(undefined);
    void adminApi.getLocalCredentials(userId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setSaved(result); setEnabled(result.EnableLocalPassword); clearSecrets(); setReview(false); setNotice(''); setSubmitted(false);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [userId, revision]);

  function clearSecrets() {
    setPassword(''); setPasswordConfirmation(''); setPin(''); setPinConfirmation(''); setClearPassword(false); setClearPin(false);
  }
  function changed() { setNotice(''); setError(null); }
  function close() { if (!inFlight.current && (!dirty || window.confirm('Discard unsaved local credentials?'))) onClose(); }
  function reload() {
    if (inFlight.current || (dirty && !window.confirm('Discard your draft and reload saved local credentials?'))) return;
    clearSecrets(); setConfirming(false); setRevision((value) => value + 1);
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setSubmitted(true);
    if (!inFlight.current && saved && dirty && !invalid && !disabled) setConfirming(true);
  }
  async function save() {
    if (inFlight.current || !saved || !dirty || invalid || disabled) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    const input: LocalCredentialsInput = { Revision: saved.Revision, EnableLocalPassword: enabled };
    if (clearPassword || password) input.LocalPassword = clearPassword ? '' : password;
    if (clearPin || pin) input.ProfilePin = clearPin ? '' : pin;
    try {
      const result = await adminApi.updateLocalCredentials(userId, input);
      if (!mounted.current || result.CurrentSessionRevoked) return;
      setSaved(result.Credentials); setEnabled(result.Credentials.EnableLocalPassword); clearSecrets(); setSubmitted(false);
      setNotice(endsSignIns ? 'Local credentials saved. Existing sign-ins for this account have ended.' : 'Profile PIN saved for compatible client profile locks.'); onChanged();
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally {
      inFlight.current = false;
      if (mounted.current) { setBusy(false); setConfirming(false); }
    }
  }

  return <>
    <Dialog open fullWidth maxWidth="sm" fullScreen={fullScreen} onClose={close} aria-labelledby="local-credentials-title" slotProps={{ paper: { sx: { maxWidth: fullScreen ? undefined : 560, ...(fullScreen ? { borderRadius: 0, m: 0 } : {}) } } }}>
      <Box component="form" noValidate onSubmit={submit} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: fullScreen ? '100%' : undefined, overflow: 'hidden' }}>
        <DialogTitle id="local-credentials-heading"><Typography component="span" variant="inherit" id="local-credentials-title">Local credentials</Typography><Typography component="span" variant="body2" color="text.secondary" sx={{ display: 'block', mt: 0.5, overflowWrap: 'anywhere' }}>{userName}</Typography></DialogTitle>
        <DialogContent aria-busy={loading || busy}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {error != null && <ErrorNotice error={error} />}
          {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>The account changed or the save response could not be confirmed. Credentials may already have changed. Reload and review the saved status before saving again.</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {loading && <Skeleton variant="rounded" height={220} aria-label="Loading local credentials" />}
          {saved && <>
            <Alert severity="info">Saved passwords and PINs are never displayed here. Leave a new credential empty to keep it, or choose Clear to remove it.</Alert>
            <Box component="section" aria-label="Local password" sx={{ p: { xs: 2, sm: 2.5 }, borderRadius: '16px', bgcolor: colors.paper }}><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">Local password</Typography><Chip size="small" color={saved.HasLocalPassword ? 'success' : 'default'} label={saved.HasLocalPassword ? 'Password set' : 'Not set'} /></Stack><Stack spacing={2} sx={{ mt: 2 }}>
              <Box sx={{ p: 1.5, borderRadius: '12px', bgcolor: colors.surface }}><FormControlLabel sx={{ m: 0, width: '100%', alignItems: 'center', justifyContent: 'space-between', gap: 1.5, '& .MuiFormControlLabel-label': { minWidth: 0 } }} labelPlacement="start" control={<Switch checked={enabled} disabled={disabled || clearPassword} onChange={(event) => { setEnabled(event.target.checked); changed(); }} slotProps={{ input: { id: 'local-password-enabled', 'aria-labelledby': 'local-password-label', 'aria-describedby': 'local-password-help' } }} />} label={<Box component="span"><Typography component="span" id="local-password-label" variant="body2" sx={{ display: 'block', fontWeight: 600 }}>Enable local password</Typography><Typography component="span" id="local-password-help" variant="body2" color={enabledError ? 'error.main' : 'text.secondary'} sx={{ display: 'block', mt: 0.5 }}>{enabledError ?? 'Allows a separate password on the server\'s trusted local network in supported clients.'}</Typography></Box>} /></Box>
              {clearPassword ? <Alert severity="warning" action={<Button color="inherit" disabled={disabled} onClick={() => { setClearPassword(false); setEnabled(saved.EnableLocalPassword); changed(); }}>Undo</Button>}>The local password will be cleared and local-password sign-in disabled when you save.</Alert> : <>
                <PasswordField fullWidth label="New local password" value={password} autoComplete="new-password" disabled={disabled} onChange={(event) => { setPassword(event.target.value); changed(); }} error={Boolean(passwordError)} helperText={passwordError ?? 'Use 1 to 72 UTF-8 bytes. Leave empty to keep the current password.'} />
                <PasswordField fullWidth label="Confirm local password" value={passwordConfirmation} autoComplete="new-password" disabled={disabled} onChange={(event) => { setPasswordConfirmation(event.target.value); changed(); }} error={Boolean(passwordConfirmationError)} helperText={passwordConfirmationError} />
                <Button variant="outlined" color="error" disabled={disabled || !saved.HasLocalPassword} onClick={() => { setClearPassword(true); setEnabled(false); setPassword(''); setPasswordConfirmation(''); changed(); }} sx={{ alignSelf: 'flex-start' }}>Clear local password</Button>
              </>}
            </Stack></Box>
            <Box component="section" aria-label="Profile PIN" sx={{ p: { xs: 2, sm: 2.5 }, borderRadius: '16px', bgcolor: colors.paper }}><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">Profile PIN</Typography><Chip size="small" color={saved.HasProfilePin ? 'success' : 'default'} label={saved.HasProfilePin ? 'PIN set' : 'Not set'} /></Stack><Stack spacing={2} sx={{ mt: 2 }}>
              <Typography variant="body2" color="text.secondary">Compatible clients use the PIN to lock a profile after sign-in. It cannot sign in to the server.</Typography>
              {!hasPassword && <Alert severity="info">Set a normal account password before setting a profile PIN.</Alert>}
              {clearPin ? <Alert severity="warning" action={<Button color="inherit" disabled={disabled} onClick={() => { setClearPin(false); changed(); }}>Undo</Button>}>The profile PIN will be cleared when you save.</Alert> : <>
                <PasswordField fullWidth label="New profile PIN" value={pin} autoComplete="new-password" disabled={disabled || !hasPassword} onChange={(event) => { setPin(event.target.value); changed(); }} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 4 } }} error={Boolean(pinError)} helperText={pinError ?? 'Use exactly 4 ASCII digits. Leave empty to keep the current PIN.'} />
                <PasswordField fullWidth label="Confirm profile PIN" value={pinConfirmation} autoComplete="new-password" disabled={disabled || !hasPassword} onChange={(event) => { setPinConfirmation(event.target.value); changed(); }} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 4 } }} error={Boolean(pinConfirmationError)} helperText={pinConfirmationError} />
                <Button variant="outlined" color="error" disabled={disabled || !saved.HasProfilePin} onClick={() => { setClearPin(true); setPin(''); setPinConfirmation(''); changed(); }} sx={{ alignSelf: 'flex-start' }}>Clear profile PIN</Button>
              </>}
            </Stack></Box>
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ px: 3, py: 2.5, borderTop: 1, borderColor: 'divider', gap: 1, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto', width: '100%' }}>{review ? 'Reload required before saving' : dirty ? 'Unsaved credential changes' : saved ? 'All changes saved' : ''}</Typography><Button onClick={reload} disabled={busy || loading} sx={{ mr: 'auto' }}>Reload</Button><Button color="secondary" onClick={close} disabled={busy}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !saved || !dirty || invalid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving credentials...' : 'Save local credentials'}</Button></DialogActions>
      </Box>
    </Dialog>
    {confirming && <Dialog open fullWidth maxWidth="xs" onClose={() => { if (!inFlight.current) setConfirming(false); }} aria-labelledby="confirm-local-credentials-title" aria-describedby="confirm-local-credentials-description">
      <DialogTitle id="confirm-local-credentials-title">Save local credentials?</DialogTitle>
      <DialogContent><Stack spacing={2}><Typography id="confirm-local-credentials-description">{endsSignIns ? `Existing sign-ins for ${userName} will end. ${isCurrentUser ? 'You will return to sign in using your normal account password.' : 'The user will need to sign in again.'}` : `Save the profile PIN change for ${userName}? Compatible clients use it to lock the profile after sign-in.`}</Typography>{(clearPassword || clearPin) && <Alert severity="warning">{clearPassword && 'The local password will be removed. '}{clearPin && 'The profile PIN will be removed.'}</Alert>}</Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus color="secondary" disabled={busy} onClick={() => setConfirming(false)}>Keep editing</Button><Button variant="contained" disabled={busy} onClick={() => void save()} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{endsSignIns ? 'Save and end sign-ins' : 'Save profile PIN'}</Button></DialogActions>
    </Dialog>}
  </>;
}
