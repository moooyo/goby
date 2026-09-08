import type { ReactNode } from 'react';
import { Alert, Box, Button, CircularProgress, Stack, Typography } from '@mui/material';
import WavesRounded from '@mui/icons-material/WavesRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { ApiError } from './api';
import { colors } from './theme';

export function Brand({ light = false, compact = false }: { light?: boolean; compact?: boolean }) {
  return (
    <Stack direction="row" spacing={1.2} sx={{ alignItems: 'center', color: light ? 'white' : colors.deep }}>
      <Box aria-hidden="true" sx={{ display: 'grid', placeItems: 'center', width: 39, height: 39, borderRadius: '12px', backgroundColor: light ? '#FFFFFF16' : '#007E8710' }}>
        <WavesRounded sx={{ fontSize: 27 }} />
      </Box>
      <Box>
        <Typography component="span" sx={{ fontFamily: '"Manrope Variable", sans-serif', fontSize: compact ? 25 : 28, lineHeight: 1, fontWeight: 780, letterSpacing: '-0.055em' }}>goby</Typography>
        {!compact && <Typography sx={{ mt: 0.4, fontSize: 10, letterSpacing: '0.13em', textTransform: 'uppercase', opacity: 0.67 }}>Server administration</Typography>}
      </Box>
    </Stack>
  );
}

export function errorText(error: unknown) {
  return error instanceof Error ? error.message : 'The request could not be completed. Please try again.';
}

export function ErrorNotice({ error, retry }: { error: unknown; retry?: () => void }) {
  return (
    <Alert
      severity="error"
      sx={{ alignItems: 'flex-start', '& .MuiAlert-message': { minWidth: 0, overflowWrap: 'anywhere' } }}
      action={retry ? <Button color="inherit" size="small" onClick={retry} startIcon={<RefreshRounded fontSize="small" />}>Retry</Button> : undefined}
    >
      {errorText(error)}
      {error instanceof ApiError && error.requestId && (
        <Typography component="div" variant="caption" sx={{ mt: 0.5 }}>Request ID: <span className="mono">{error.requestId}</span></Typography>
      )}
    </Alert>
  );
}

export function LoadingView({ label = 'Connecting to your server' }: { label?: string }) {
  return (
    <Stack role="status" aria-live="polite" spacing={3} sx={{ alignItems: 'center', justifyContent: 'center', minHeight: '100dvh', p: 3 }}>
      <Brand />
      <CircularProgress size={25} thickness={3} aria-label={label} />
      <Typography variant="body2" color="text.secondary">{label}</Typography>
    </Stack>
  );
}

export function PageHeading({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 2, mb: 3.5 }}>
      <Box>
        <Typography variant="h2" component="h1">{title}</Typography>
        <Typography color="text.secondary" sx={{ mt: 0.8 }}>{description}</Typography>
      </Box>
      {action}
    </Stack>
  );
}
