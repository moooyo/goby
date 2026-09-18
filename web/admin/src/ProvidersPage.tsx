import { useEffect, useState } from 'react';
import { Alert, Box, Button, Chip, Link, Paper, Skeleton, Stack, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { ErrorNotice, PageHeading } from './components';
import { providersApi, providerWebsite } from './providersApi';
import type { OnlineProviders } from './providersApi';
import tmdbLogo from './assets/tmdb.svg';

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
  return <Box aria-busy={loading}>
    <PageHeading title="Online providers" description="Review metadata, image, and subtitle sources configured for this server." action={<Button startIcon={<RefreshRounded />} variant="outlined" disabled={loading} onClick={reload}>Refresh providers</Button>} />
    <Stack spacing={2.5}>
      {error != null && <ErrorNotice error={error} retry={reload} />}
      {loading && !data && <Skeleton variant="rounded" height={240} />}
      {data && <>
        {!data.Enabled && <Alert severity="info">Internet providers are disabled. Enable internet providers in Settings before requesting online metadata, images, or subtitles.</Alert>}
        <Typography variant="body2" color="text.secondary">Provider credentials are managed in the server deployment and loaded at startup. Restart the server after changing credentials. Open a library item and choose Online sources to search, review, and apply metadata, images, or subtitles.</Typography>
        {data.Items.map((provider) => <Paper key={provider.Id} component="section" aria-label={provider.Name} variant="outlined" sx={{ p: 3 }}>
          <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 1 }}><Typography variant="h3" component="h2">{provider.Name}</Typography><Chip size="small" variant="outlined" color={provider.Configured ? 'success' : 'default'} label={!data.Enabled ? 'Disabled' : provider.Configured ? 'Configured' : 'Configuration required'} /></Stack>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, mt: 2 }}>{provider.Capabilities.map((capability) => <Chip key={capability} size="small" label={capability} />)}</Stack>
          <Box component="section" aria-label={`${provider.Name} credits`} sx={{ mt: 2 }}>
            <Typography variant="overline" color="text.secondary">Sources and credits</Typography>
            {provider.Id === 'tmdb' && <Box component="img" src={tmdbLogo} alt="TMDB" sx={{ display: 'block', width: 96, height: 'auto', mt: 0.5, mb: 1.5 }} />}
            {provider.Attribution && <Typography variant="body2" color="text.secondary">{provider.Attribution}</Typography>}
          </Box>
          {providerWebsite(provider.Website) && <Link href={providerWebsite(provider.Website)} target="_blank" rel="noopener noreferrer" sx={{ display: 'inline-block', mt: 1 }}>Provider website</Link>}
        </Paper>)}
        {data.Items.length === 0 && <Typography color="text.secondary">No online providers are available.</Typography>}
      </>}
    </Stack>
  </Box>;
}
