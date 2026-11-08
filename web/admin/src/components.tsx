import type { ReactNode } from 'react';
import { Alert, Button, CircularProgress, Stack, Typography } from './ui';
import { RefreshRounded } from './icons';
import { ApiError } from './api';
import { useTranslation } from './i18n';

export function Brand({ light = false, compact = false }: { light?: boolean; compact?: boolean }) {
  const { t } = useTranslation();
  return <div className="goby-brand" data-light={light || undefined}>
    <svg className="goby-mark" viewBox="0 0 44 44" aria-hidden="true"><rect width="44" height="44" rx="13" fill="var(--acc)"/><path d="M29.5 15.7a10 10 0 1 0 1.1 11.1V21H22v3h5.4v1.8a6.5 6.5 0 1 1-.4-8Z" fill="var(--on-acc)"/></svg>
    <div><strong className="goby-brand-name">Goby</strong>{!compact && <span className="goby-brand-sub">{t('Server admin')}</span>}</div>
  </div>;
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
  const { t } = useTranslation();
  return <div className="page-heading"><div className="page-heading-copy"><h1>{t(title)}</h1><p>{t(description)}</p></div>{action && <div className="page-heading-actions">{action}</div>}</div>;
}
