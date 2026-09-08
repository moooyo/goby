import { useEffect, useState } from 'react';
import { Box, Button, Chip, Divider, Paper, Skeleton, Stack, Typography } from '@mui/material';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import Inventory2Outlined from '@mui/icons-material/Inventory2Outlined';
import SensorsRounded from '@mui/icons-material/SensorsRounded';
import DnsOutlined from '@mui/icons-material/DnsOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded';
import RadioButtonUncheckedRounded from '@mui/icons-material/RadioButtonUncheckedRounded';
import { adminApi, isAbortError } from './api';
import type { OverviewResponse, User } from './api';
import { ErrorNotice, PageHeading } from './components';
import { colors } from './theme';

export function OverviewPage({ user, onUsers }: { user: User; onUsers: () => void }) {
  const [data, setData] = useState<OverviewResponse>();
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    adminApi.getOverview({ signal: controller.signal })
      .then(setData)
      .catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  const refresh = () => setRevision((value) => value + 1);
  const databaseReady = data && ['ok', 'ready', 'connected', 'healthy'].includes(data.Database.Status.toLowerCase());
  const stats = data ? [
    { label: 'Users', value: data.Counts.Users, icon: PeopleOutlineRounded },
    { label: 'Libraries', value: data.Counts.Libraries, icon: VideoLibraryOutlined },
    { label: 'Media items', value: data.Counts.Items, icon: Inventory2Outlined },
    { label: 'Active sessions', value: data.Counts.ActiveSessions, icon: SensorsRounded },
  ] : [];

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Overview" description="A clear view of your server and its activity." action={<Button variant="outlined" startIcon={<RefreshRounded />} onClick={refresh} disabled={loading}>Refresh</Button>} />
      {error != null && <Box sx={{ mb: 3 }}><ErrorNotice error={error} retry={refresh} /></Box>}
      {!data && loading && (
        <Stack spacing={3} role="status" aria-label="Loading server overview">
          <Skeleton variant="rounded" height={132} />
          <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 2 }}><Skeleton variant="rounded" height={120} /><Skeleton variant="rounded" height={120} /></Box>
          <Skeleton variant="rounded" height={260} />
        </Stack>
      )}
      {data && (
        <Stack spacing={3}>
          <Paper variant="outlined" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr auto' }, overflow: 'hidden' }}>
            <Stack direction="row" spacing={2} sx={{ alignItems: 'center', p: { xs: 2.5, md: 3 }, minWidth: 0 }}>
              <Box sx={{ display: 'grid', placeItems: 'center', width: 52, height: 52, flexShrink: 0, borderRadius: 2, bgcolor: '#007E870D', color: 'primary.main' }}><DnsOutlined sx={{ fontSize: 28 }} /></Box>
              <Box sx={{ minWidth: 0 }}>
                <Typography variant="overline" color="text.secondary">Your server</Typography>
                <Typography variant="h3" component="h2" sx={{ overflowWrap: 'anywhere', mt: 0.15 }}>{data.Server.Name}</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>Goby {data.Server.Version}</Typography>
              </Box>
            </Stack>
            <Stack spacing={0.7} sx={{ justifyContent: 'center', p: { xs: 2.5, md: 3 }, bgcolor: '#EEF6F6', borderLeft: { sm: 1 }, borderTop: { xs: 1, sm: 0 }, borderColor: 'divider', minWidth: 200 }}>
              <Typography variant="body2" color="text.secondary">Database connection</Typography>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 0.8 }}>
                <Box aria-hidden="true" sx={{ width: 7, height: 7, borderRadius: '50%', bgcolor: databaseReady ? 'success.main' : 'warning.main' }} />
                <Typography sx={{ fontWeight: 650, textTransform: 'capitalize' }}>{data.Database.Status}</Typography>
              </Stack>
            </Stack>
          </Paper>

          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'repeat(2, minmax(0, 1fr))', lg: 'repeat(4, minmax(0, 1fr))' }, gap: { xs: 1.5, md: 2 } }}>
            {stats.map(({ label, value, icon: Icon }) => (
              <Paper key={label} variant="outlined" sx={{ p: { xs: 2, md: 2.5 } }}>
                <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', mb: 2.5 }}>
                  <Typography variant="body2" color="text.secondary">{label}</Typography>
                  <Icon aria-hidden="true" sx={{ color: '#7F9BA5', fontSize: 20 }} />
                </Stack>
                <Typography sx={{ fontFamily: '"Manrope Variable", sans-serif', fontWeight: 680, fontSize: '2rem', lineHeight: 1, letterSpacing: '-0.04em', fontVariantNumeric: 'tabular-nums' }}>{value.toLocaleString()}</Typography>
              </Paper>
            ))}
          </Box>

          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', lg: '1.2fr 1fr' }, gap: 3 }}>
            <Paper variant="outlined">
              <Box sx={{ px: 3, pt: 3, pb: 2 }}><Typography variant="h3" component="h2">Server details</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>Identity and software running this server.</Typography></Box>
              <Box component="dl" sx={{ m: 0, px: 3, pb: 1 }}>
                {[
                  ['Server ID', data.Server.Id],
                  ['Goby version', data.Server.Version],
                  ['Database', 'PostgreSQL'],
                  ['Go runtime', data.Runtime.GoVersion],
                ].map(([label, value]) => (
                  <Box key={label} sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '125px minmax(0, 1fr)' }, gap: { xs: 0.5, sm: 2 }, borderTop: 1, borderColor: 'divider', py: 1.9 }}>
                    <Typography component="dt" variant="body2" color="text.secondary">{label}</Typography>
                    <Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere', fontFamily: label === 'Server ID' ? 'ui-monospace, Consolas, monospace' : 'inherit' }}>{value}</Typography>
                  </Box>
                ))}
              </Box>
            </Paper>

            <Paper variant="outlined" sx={{ p: 3 }}>
              <Typography variant="h3" component="h2">Server capabilities</Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2 }}>Availability reported by this server version.</Typography>
              {[
                { label: 'Library management', available: data.Features.LibraryManagement },
                { label: 'Media playback', available: data.Features.Playback },
                { label: 'Transcoding', available: data.Features.Transcoding },
              ].map(({ label, available }) => (
                <Stack key={label} direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, py: 1.3 }}>
                  <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}>
                    {available ? <CheckCircleOutlineRounded sx={{ fontSize: 18, color: 'success.main' }} /> : <RadioButtonUncheckedRounded sx={{ fontSize: 18, color: '#97ADB5' }} />}
                    <Typography variant="body2">{label}</Typography>
                  </Stack>
                  <Chip size="small" label={available ? 'Available' : 'Not available'} color={available ? 'success' : 'default'} variant="outlined" />
                </Stack>
              ))}
              <Divider sx={{ mt: 1.5, mb: 2 }} />
              <Typography variant="body2" color="text.secondary">Playback takes place in a compatible client. This dashboard manages the server.</Typography>
            </Paper>
          </Box>

          <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between', gap: 2, borderTop: 1, borderColor: 'divider', pt: 2.5, px: 0.5 }}>
            <Typography variant="body2" color="text.secondary">Signed in as <Box component="span" sx={{ color: colors.ink, fontWeight: 600, overflowWrap: 'anywhere' }}>{user.Name}</Box>. Manage who can access your server.</Typography>
            <Button onClick={onUsers} endIcon={<ArrowForwardRounded />} sx={{ flexShrink: 0 }}>Manage users</Button>
          </Stack>
        </Stack>
      )}
    </Box>
  );
}
