import { useEffect, useState } from 'react';
import { Alert, Box, Button, Chip, Link, Paper, Skeleton, Stack, Typography } from '@mui/material';
import InfoOutlined from '@mui/icons-material/InfoOutlined';
import OpenInNewRounded from '@mui/icons-material/OpenInNewRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { ErrorNotice } from './components';
import { providersApi, providerWebsite } from './providersApi';
import type { OnlineProviders } from './providersApi';
import { colors } from './theme';
import tmdbLogo from './assets/tmdb.svg';

const providerGrid = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 320px), 1fr))',
  gap: 2,
} as const;

const capabilityLabels: Record<string, string> = {
  metadata: 'Metadata',
  images: 'Images',
  subtitles: 'Subtitles',
};

export function ProvidersPage() {
  const [data, setData] = useState<OnlineProviders>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(undefined);
    void providersApi.getProviders({ signal: controller.signal }).then((value) => { if (!controller.signal.aborted) setData(value); })
      .catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  const reload = () => setRevision((value) => value + 1);
  return <Stack component="section" aria-label="Online providers" aria-busy={loading} spacing={2.5}>
    <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between', gap: 2 }}>
      <Typography color="text.secondary" sx={{ flex: 1 }}>Review metadata, image, and subtitle sources configured for this server.</Typography>
      <Button startIcon={<RefreshRounded />} variant="outlined" disabled={loading} onClick={reload} sx={{ borderColor: colors.outline }}>Refresh providers</Button>
    </Stack>
      {error != null && <ErrorNotice error={error} retry={reload} />}
      {loading && !data && <Box sx={providerGrid}>
        {[0, 1, 2].map((index) => <Skeleton key={index} variant="rounded" height={260} sx={{ borderRadius: '20px' }} />)}
      </Box>}
      {data && <>
        {!data.Enabled && <Alert severity="info">Internet providers are disabled. Enable internet providers in Settings before requesting online metadata, images, or subtitles.</Alert>}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, py: 1.75, px: 2, borderRadius: '16px', bgcolor: colors.surface }}>
          <InfoOutlined aria-hidden="true" sx={{ color: 'primary.main', fontSize: 20, flexShrink: 0 }} />
          <Typography variant="body2" color="text.secondary" sx={{ textWrap: 'pretty' }}>Provider credentials are managed in the server deployment and loaded at startup. Restart the server after changing credentials. Open a library item and choose Online sources to search, review, and apply metadata, images, or subtitles.</Typography>
        </Box>
        {data.Items.length > 0 && <Box sx={providerGrid}>
          {data.Items.map((provider) => {
            const website = providerWebsite(provider.Website);
            return <Paper key={provider.Id} component="section" aria-label={provider.Name} variant="outlined" sx={{ display: 'flex', flexDirection: 'column', gap: 1.75, py: 2.5, px: 3, minWidth: 0 }}>
              <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.25 }}>
                <Typography component="h2" sx={{ flex: 1, fontSize: 18, lineHeight: '24px', fontWeight: 600, overflowWrap: 'anywhere' }}>{provider.Name}</Typography>
                <Chip size="small" color={!data.Enabled ? 'default' : provider.Configured ? 'success' : 'warning'} label={!data.Enabled ? 'Disabled' : provider.Configured ? 'Configured' : 'Configuration required'} sx={{ flexShrink: 0, borderRadius: '8px', fontSize: 12, fontWeight: 600, '& .MuiChip-label': { px: 1.25 } }} />
              </Stack>
              <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.75 }}>
                {provider.Capabilities.map((capability) => <Chip key={capability} label={capabilityLabels[capability] ?? capability} sx={{ bgcolor: colors.surface, color: 'text.primary', '& .MuiChip-label': { px: 1.25 } }} />)}
              </Stack>
              <Box component="section" aria-label={`${provider.Name} credits`} sx={{ pt: 1.5, borderTop: 1, borderColor: 'divider' }}>
                <Typography sx={{ fontSize: 11, lineHeight: '16px', fontWeight: 600, letterSpacing: '0.6px', color: colors.subtle }}>Sources and credits</Typography>
                {provider.Id === 'tmdb' && <Box component="img" src={tmdbLogo} alt="TMDB" sx={{ display: 'block', width: 96, height: 'auto', mt: 1, mb: 0.75 }} />}
                {provider.Attribution && <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{provider.Attribution}</Typography>}
              </Box>
              {website && <Link href={website} target="_blank" rel="noopener noreferrer" underline="none" aria-label={`${provider.Name} website (opens in a new tab)`} sx={{ display: 'inline-flex', alignSelf: 'flex-start', alignItems: 'center', gap: 0.75, minHeight: { xs: 44, sm: 36 }, px: 1.5, ml: -1.5, borderRadius: '18px', fontSize: 14, fontWeight: 600, '&:hover': { bgcolor: 'rgba(31, 95, 191, 0.08)' } }}>
                Provider website <OpenInNewRounded aria-hidden="true" sx={{ fontSize: 16 }} />
              </Link>}
            </Paper>;
          })}
        </Box>}
        {data.Items.length === 0 && <Paper variant="outlined" sx={{ py: 4, px: 3, bgcolor: colors.surface }}><Typography color="text.secondary">No online providers are available.</Typography></Paper>}
      </>}
  </Stack>;
}
