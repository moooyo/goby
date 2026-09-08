import { useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Paper, Stack, TextField, Typography } from '@mui/material';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import ShieldOutlined from '@mui/icons-material/ShieldOutlined';
import { adminApi, isAbortError } from './api';
import type { SessionResponse } from './api';
import { Brand, ErrorNotice, fieldError, PasswordField } from './components';
import { colors } from './theme';

type AuthPageProps = {
  mode: 'setup' | 'login';
  initialName?: string;
  notice?: string;
  onSession: (session: SessionResponse) => void;
  onSetup: (name: string) => void;
  onReload: () => void;
};

export function AuthPage({ mode, initialName = '', notice, onSession, onSetup, onReload }: AuthPageProps) {
  const setup = mode === 'setup';
  const [name, setName] = useState(initialName);
  const [password, setPassword] = useState('');
  const [setupToken, setSetupToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      if (setup) {
        const result = await adminApi.bootstrap({ SetupToken: setupToken, Name: name.trim(), Password: password });
        setSetupToken('');
        setPassword('');
        onSetup(result.User.Name);
      } else {
        const result = await adminApi.login({ Name: name.trim(), Password: password });
        setPassword('');
        onSession(result);
      }
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Box sx={{ minHeight: '100dvh', display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'minmax(350px, 42%) 1fr' } }}>
      <Stack sx={{ backgroundColor: colors.deep, color: 'white', p: { xs: 3, sm: 5, lg: 7 }, justifyContent: 'space-between', minHeight: { md: '100dvh' } }}>
        <Brand light />
        <Box sx={{ maxWidth: 420, my: { xs: 4, md: 10 } }}>
          <Chip icon={<ShieldOutlined />} label="Administrator workspace" sx={{ mb: 3, color: '#C9E5E9', border: '1px solid #FFFFFF28', bgcolor: 'transparent', '& .MuiChip-icon': { color: '#94C9CE' } }} />
          <Typography component="p" variant="h1" sx={{ fontSize: { xs: '2rem', lg: '3.05rem' }, maxWidth: 360 }}>Make room for your media.</Typography>
          <Typography sx={{ color: '#B8D2D9', maxWidth: 340, mt: 2.5 }}>Manage the people, libraries, and settings behind your Goby server.</Typography>
        </Box>
        <Stack direction="row" sx={{ alignItems: 'center', gap: 1, display: { xs: 'none', md: 'flex' }, color: '#B8D2D9' }}>
          <Box sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: '#72C5C2' }} />
          <Typography variant="body2">Your server. Your collection.</Typography>
        </Stack>
      </Stack>
      <Stack component="main" sx={{ justifyContent: 'center', alignItems: 'center', p: { xs: 3, sm: 5 }, py: { xs: 5, md: 8 } }}>
        <Paper component="section" sx={{ width: '100%', maxWidth: 420, p: 0, bgcolor: 'transparent' }}>
          <Typography variant="overline" color="primary">{setup ? 'First-run setup' : 'Welcome back'}</Typography>
          <Typography variant="h2" component="h1" sx={{ mt: 1 }}>{setup ? 'Set up your server' : 'Sign in to Goby'}</Typography>
          <Typography color="text.secondary" sx={{ mt: 1, mb: 3.5 }}>{setup ? 'Create the administrator account that will manage this server.' : 'Use your administrator account to continue.'}</Typography>
          <Stack component="form" onSubmit={submit} spacing={2.5} aria-busy={busy}>
            {notice && <Alert severity="info">{notice}</Alert>}
            {error != null && <ErrorNotice error={error} />}
            {setup && (
              <TextField
                id="setup-token"
                name="SetupToken"
                label="Setup token"
                type="password"
                required
                fullWidth
                autoFocus
                autoComplete="off"
                value={setupToken}
                onChange={(event) => setSetupToken(event.target.value)}
                disabled={busy}
                error={Boolean(fieldError(error, 'SetupToken'))}
                helperText={fieldError(error, 'SetupToken') ?? 'Enter the one-time token from your server deployment.'}
              />
            )}
            <TextField
              id="account-name"
              name="Name"
              label={setup ? 'Administrator username' : 'Username'}
              required
              fullWidth
              autoFocus={!setup}
              autoComplete="username"
              value={name}
              onChange={(event) => setName(event.target.value)}
              disabled={busy}
              error={Boolean(fieldError(error, 'Name'))}
              helperText={fieldError(error, 'Name')}
              slotProps={{ htmlInput: { autoCapitalize: 'none', spellCheck: false } }}
            />
            <PasswordField
              id="account-password"
              name="Password"
              label="Password"
              required
              fullWidth
              autoComplete={setup ? 'new-password' : 'current-password'}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={busy}
              error={Boolean(fieldError(error, 'Password'))}
              helperText={fieldError(error, 'Password') ?? (setup ? 'Use a unique password for this administrator account.' : undefined)}
            />
            <Button type="submit" variant="contained" size="large" disabled={busy || !name.trim() || !password || (setup && !setupToken)} endIcon={busy ? <CircularProgress size={18} color="inherit" /> : <ArrowForwardRounded />} sx={{ minHeight: 48 }}>
              {busy ? (setup ? 'Creating administrator...' : 'Signing in...') : (setup ? 'Create administrator' : 'Sign in')}
            </Button>
          </Stack>
          {setup && <Button onClick={onReload} disabled={busy} size="small" sx={{ mt: 2, px: 0 }}>Already set up? Check server</Button>}
          <Typography variant="body2" color="text.secondary" sx={{ mt: 4, borderTop: 1, borderColor: 'divider', pt: 2.5 }}>To browse and play your media, connect a compatible client to this server.</Typography>
        </Paper>
      </Stack>
    </Box>
  );
}
