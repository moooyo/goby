import { useEffect, useState } from 'react';
import type { ElementType } from 'react';
import { Box, Button, ButtonBase, Chip, Divider, IconButton, LinearProgress, Paper, Skeleton, Stack, Tooltip, Typography } from '@mui/material';
import PeopleOutlineRounded from '@mui/icons-material/PeopleOutlineRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import Inventory2Outlined from '@mui/icons-material/Inventory2Outlined';
import SensorsRounded from '@mui/icons-material/SensorsRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import CheckCircleRounded from '@mui/icons-material/CheckCircleRounded';
import RadioButtonUncheckedRounded from '@mui/icons-material/RadioButtonUncheckedRounded';
import MemoryOutlined from '@mui/icons-material/MemoryOutlined';
import StorageOutlined from '@mui/icons-material/StorageOutlined';
import MovieFilterOutlined from '@mui/icons-material/MovieFilterOutlined';
import HistoryRounded from '@mui/icons-material/HistoryRounded';
import LoginRounded from '@mui/icons-material/LoginRounded';
import PersonAddAltRounded from '@mui/icons-material/PersonAddAltRounded';
import SyncRounded from '@mui/icons-material/SyncRounded';
import VpnKeyOutlined from '@mui/icons-material/VpnKeyOutlined';
import SettingsOutlined from '@mui/icons-material/SettingsOutlined';
import BackupOutlined from '@mui/icons-material/BackupOutlined';
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import { adminApi, isAbortError } from './api';
import type { ActivityEntry, User } from './api';
import { ErrorNotice } from './components';
import { systemStatusApi } from './systemStatusApi';
import type { SystemStatus } from './systemStatusApi';
import { colors } from './theme';

interface OverviewPageProps {
  user: User;
  onUsers: () => void;
  onLibraries?: () => void;
  onItems?: () => void;
  onSessions?: () => void;
  onActivity?: () => void;
}
interface Metric { label: string; icon: ElementType; side: string; value: string; unit: string; percent: number | null; note: string }
const metricGridSx = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 200px), 1fr))' };
const panelSx = { borderRadius: '20px', minWidth: 0 };
const unavailable = 'Not available';
const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
const pollInterval = 5000;
const requestTimeout = 15000;
const pageVisible = () => document.visibilityState !== 'hidden';
const loadOverview = (signal: AbortSignal) => adminApi.getOverview({ signal });
const loadSystemStatus = (signal: AbortSignal) => systemStatusApi.getStatus({ signal });
const loadRecentActivity = (signal: AbortSignal) => adminApi.getActivity({ Limit: 6 }, { signal });

function useOverviewResource<T>(load: (signal: AbortSignal) => Promise<T>, label: string, revision: number) {
  const [state, setState] = useState<{ data?: T; error: unknown; loading: boolean }>({ error: null, loading: true });
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let pending: AbortController | undefined;

    function cancelCurrent() {
      clearTimeout(timer);
      const current = pending;
      pending = undefined;
      current?.abort();
    }

    async function poll() {
      if (disposed || !pageVisible() || pending) return;
      const controller = new AbortController();
      pending = controller;
      setState((current) => ({ ...current, loading: true }));
      let deadline: ReturnType<typeof setTimeout> | undefined;
      let abort: (() => void) | undefined;
      try {
        // The cancellation promise also settles a request whose transport has stalled.
        const cancelled = new Promise<never>((_resolve, reject) => {
          abort = () => reject(controller.signal.reason);
          controller.signal.addEventListener('abort', abort, { once: true });
        });
        deadline = setTimeout(() => controller.abort(new Error(`${label} did not respond within 15 seconds. Refresh to try again.`)), requestTimeout);
        const data = await Promise.race([load(controller.signal), cancelled]);
        if (!disposed && pending === controller) setState({ data, error: null, loading: false });
      } catch (error) {
        if (!disposed && pending === controller) {
          setState((current) => ({ ...current, error: isAbortError(error) ? current.error : error, loading: false }));
        }
      } finally {
        clearTimeout(deadline);
        if (abort) controller.signal.removeEventListener('abort', abort);
        const current = pending === controller;
        if (current) pending = undefined;
        if (current && !disposed && pageVisible()) timer = setTimeout(() => { void poll(); }, pollInterval);
      }
    }

    function visibilityChanged() {
      if (document.visibilityState === 'hidden') {
        cancelCurrent();
        setState((current) => ({ ...current, loading: false }));
      } else {
        void poll();
      }
    }
    document.addEventListener('visibilitychange', visibilityChanged);
    void poll();
    return () => {
      disposed = true;
      document.removeEventListener('visibilitychange', visibilityChanged);
      cancelCurrent();
    };
  }, [load, label, revision]);
  return state;
}

function byteUnit(bytes: number): number { return bytes > 0 ? Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), byteUnits.length - 1) : 0; }
function decimal(value: number, precision = 1): string { return value.toLocaleString(undefined, { maximumFractionDigits: precision }); }
function bytes(value: number | null | undefined): string {
  if (value == null) return unavailable;
  const index = byteUnit(value);
  return `${decimal(value / 1024 ** index)} ${byteUnits[index]}`;
}
function capacity(used: number | null | undefined, total: number | null | undefined) {
  if (used == null || total == null) return { value: '—', unit: '', percent: null };
  const index = byteUnit(total);
  return { value: decimal(used / 1024 ** index), unit: `/ ${decimal(total / 1024 ** index)} ${byteUnits[index]}`, percent: total > 0 ? Math.min(100, used / total * 100) : null };
}
function uptime(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor(minutes / 60) % 24;
  if (days > 0) return `Up ${days}d ${hours}h`;
  if (hours > 0) return `Up ${hours}h ${minutes % 60}m`;
  return minutes > 0 ? `Up ${minutes}m` : 'Up less than a minute';
}
function metrics(status: SystemStatus | undefined): Metric[] {
  const cpu = status?.CPU;
  const memory = status?.Memory;
  const storage = status?.Storage;
  const transcoding = status?.Transcoding;
  const load = cpu && [cpu.Load1, cpu.Load5, cpu.Load15].every((value) => value !== null)
    ? `Load ${decimal(cpu.Load1!, 2)} · ${decimal(cpu.Load5!, 2)} · ${decimal(cpu.Load15!, 2)}` : 'Load averages not available';
  const storageNote = !storage ? unavailable : storage.Volumes.length === 0 ? 'No media directories configured'
    : `${storage.Complete ? '' : 'Some directories are unavailable · '}${storage.Volumes.map((volume) => volume.Path).join(' · ')}`;
  const active = transcoding?.Available ? transcoding.Active : null;
  const limit = transcoding?.Available ? transcoding.Limit : null;
  return [
    { label: 'CPU', icon: MemoryOutlined, side: status ? `${status.Host.CPUCount} threads` : '',
      value: cpu?.UsagePercent == null ? '—' : decimal(cpu.UsagePercent, 0), unit: cpu?.UsagePercent == null ? '' : '%', percent: cpu?.UsagePercent ?? null,
      note: cpu?.UsagePercent == null ? `CPU usage not available · ${load}` : load },
    { label: 'Memory', icon: MemoryOutlined, side: memory?.TotalBytes == null ? '' : bytes(memory.TotalBytes), ...capacity(memory?.UsedBytes, memory?.TotalBytes),
      note: memory ? `Cache ${bytes(memory.CachedBytes)} · Swap ${bytes(memory.SwapUsedBytes)}` : unavailable },
    { label: 'Media storage', icon: StorageOutlined, side: storage ? `${storage.Volumes.length} ${storage.Volumes.length === 1 ? 'directory' : 'directories'}` : '',
      ...capacity(storage?.UsedBytes, storage?.TotalBytes), note: storageNote },
    { label: 'Transcoding', icon: MovieFilterOutlined, side: limit == null ? '' : `Limit ${limit}`,
      value: active == null ? '—' : decimal(active, 0), unit: limit == null ? '' : `/ ${limit} jobs`,
      percent: active != null && limit != null && limit > 0 ? Math.min(100, active / limit * 100) : null,
      note: transcoding?.Available ? `${transcoding.HardwareActive ?? '—'} hardware · ${transcoding.SoftwareActive ?? '—'} software`
        : status && !transcoding?.Available && transcoding?.Limit === 0 ? 'Transcoding is disabled' : 'Transcoding metrics not available' },
  ];
}
function MetricCard({ metric, loading }: { metric: Metric; loading: boolean }) {
  const Icon = metric.icon;
  const progressColor = metric.percent != null && metric.percent >= 85 ? 'error' : metric.percent != null && metric.percent >= 75 ? 'warning' : 'primary';
  return <Box component="section" aria-label={`${metric.label} status`} sx={{ p: 2, borderRadius: '16px', bgcolor: colors.surface, minWidth: 0 }}>
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1, color: 'text.secondary' }}><Icon aria-hidden="true" sx={{ fontSize: 18 }} /><Typography sx={{ flex: 1 }}>{metric.label}</Typography><Typography variant="caption" sx={{ color: colors.subtle, textAlign: 'right' }}>{metric.side}</Typography></Stack>
    {loading ? <Skeleton width="70%" height={42} sx={{ mt: 1.25 }} /> : <Stack direction="row" sx={{ alignItems: 'baseline', gap: 0.75, mt: 1.25, flexWrap: 'wrap' }}>
      <Typography component="span" sx={{ fontSize: 26, lineHeight: '32px', fontWeight: 600, letterSpacing: '-.3px', fontVariantNumeric: 'tabular-nums' }} aria-label={metric.value === '—' ? unavailable : undefined}>{metric.value}</Typography>
      <Typography component="span" variant="body2" color="text.secondary" sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>{metric.unit}</Typography>
    </Stack>}
    <LinearProgress variant="determinate" value={metric.percent ?? 0} color={progressColor} aria-label={`${metric.label} utilization`} aria-valuetext={metric.percent == null ? unavailable : `${decimal(metric.percent)}%`} sx={{ mt: 1.25, '& .MuiLinearProgress-bar': { transition: 'none', opacity: metric.percent == null ? 0 : 1 } }} />
    <Typography variant="caption" component="p" title={metric.note} sx={{ color: colors.subtle, mt: 1, mb: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{loading ? 'Loading metrics…' : metric.note}</Typography>
  </Box>;
}
function activityIcon(entry: ActivityEntry): ElementType {
  if (entry.Severity === 'Error' || entry.Severity === 'Fatal') return ErrorOutlineRounded;
  if (entry.Action === 'session.login') return LoginRounded;
  if (entry.Action === 'user.created') return PersonAddAltRounded;
  if (entry.Action.startsWith('scan.') || entry.Action.startsWith('task.')) return SyncRounded;
  if (entry.Action.startsWith('application_key.')) return VpnKeyOutlined;
  if (entry.Action.startsWith('settings.')) return SettingsOutlined;
  if (entry.Action.startsWith('backup.') || entry.Action.startsWith('restore.')) return BackupOutlined;
  return HistoryRounded;
}
function ActivityRow({ entry }: { entry: ActivityEntry }) {
  const Icon = activityIcon(entry);
  const tone = entry.Severity === 'Warn' ? 'warning' : entry.Severity === 'Error' || entry.Severity === 'Fatal' ? 'error' : 'info';
  const actor = entry.Actor.Name || (entry.Actor.Kind === 'system' ? 'System' : entry.Actor.Kind === 'application_key' ? 'API key' : 'User');
  const source = { native: 'Console', emby: 'Emby API', system: 'System' }[entry.Source];
  const date = new Date(entry.Date);
  const today = date.toDateString() === new Date().toDateString();
  const time = today ? date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  return <Box component="li" sx={{ display: 'flex', alignItems: 'center', gap: 1.75, px: 2, py: 1.25, borderRadius: '12px', '&:hover': { bgcolor: colors.surface } }}>
    <Box aria-label={entry.Severity === 'Warn' ? 'Warning' : entry.Severity} sx={{ width: 36, height: 36, flexShrink: 0, borderRadius: '50%', display: 'grid', placeItems: 'center', bgcolor: `${tone}.light`, color: `${tone}.dark` }}><Icon aria-hidden="true" sx={{ fontSize: 18 }} /></Box>
    <Box sx={{ flex: 1, minWidth: 0 }}><Typography title={entry.Name} sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.Name}</Typography><Typography variant="caption" component="div" sx={{ color: colors.subtle, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{actor} · {source}</Typography></Box>
    <Typography component="time" dateTime={entry.Date} title={date.toLocaleString()} variant="caption" sx={{ color: colors.subtle, flexShrink: 0, fontVariantNumeric: 'tabular-nums' }}>{time}</Typography>
  </Box>;
}

export function OverviewPage({ user, onUsers, onLibraries, onItems, onSessions, onActivity }: OverviewPageProps) {
  const [revision, setRevision] = useState(0);
  const { data, error, loading: overviewLoading } = useOverviewResource(loadOverview, 'Server overview', revision);
  const { data: status, error: statusError, loading: statusLoading } = useOverviewResource(loadSystemStatus, 'System metrics', revision);
  const { data: activity, error: activityError, loading: activityLoading } = useOverviewResource(loadRecentActivity, 'Recent activity', revision);
  const refresh = () => setRevision((value) => value + 1);
  const databaseReady = data && ['ok', 'ready', 'connected', 'healthy'].includes(data.Database.Status.toLowerCase());
  const stats = [
    { label: 'Users', value: data?.Counts.Users, icon: PeopleOutlineRounded, action: onUsers },
    { label: 'Libraries', value: data?.Counts.Libraries, icon: VideoLibraryOutlined, action: onLibraries },
    { label: 'Media items', value: data?.Counts.Items, icon: Inventory2Outlined, action: onItems ?? onLibraries },
    { label: 'Active sessions', value: data?.Counts.ActiveSessions, icon: SensorsRounded, action: onSessions },
  ];
  return <Stack spacing={2.5} aria-label="Server overview">
    {error != null && <Stack spacing={1}><ErrorNotice error={error} retry={refresh} />{data && <Typography variant="caption" color="text.secondary">Server details and counts show the last successful update.</Typography>}</Stack>}
    <Paper component="section" variant="outlined" sx={{ ...panelSx, pt: 2, pr: 2, pb: 2.5, pl: { xs: 2, sm: 3 } }}>
      <Stack direction="row" sx={{ alignItems: 'center', gap: '10px 16px', flexWrap: 'wrap' }}>
        {data ? <Typography variant="h3" component="h2" sx={{ overflowWrap: 'anywhere' }}>{data.Server.Name}</Typography> : error != null ? <Typography variant="h3" component="h2">Server unavailable</Typography> : <Skeleton width={120} height={28} />}
        {data && <Typography variant="body2" color="text.secondary">Goby {data.Server.Version}</Typography>}
        {data && <Chip size="small" color={error != null ? 'warning' : databaseReady ? 'success' : 'warning'} label={error != null ? 'Database status outdated' : databaseReady ? 'Database healthy' : `Database ${data.Database.Status}`} icon={<Box component="span" aria-hidden="true" sx={{ width: 6, height: 6, borderRadius: '50%', bgcolor: 'currentColor', ml: '10px !important' }} />} sx={{ borderRadius: '8px', fontSize: 12, fontWeight: 600 }} />}
        {status && <Typography variant="body2" sx={{ color: colors.subtle, whiteSpace: 'nowrap' }}>{uptime(status.UptimeSeconds)}{statusError != null ? ' · Last update' : ''}</Typography>}
        <Box sx={{ flex: 1 }} /><Typography variant="caption" sx={{ color: colors.subtle, whiteSpace: 'nowrap' }}>Updates every 5 seconds</Typography>
        <Tooltip title={overviewLoading || statusLoading || activityLoading ? 'Cancel pending requests and refresh overview' : 'Refresh overview'}><IconButton aria-label="Refresh overview" onClick={refresh}><RefreshRounded sx={{ fontSize: 20 }} /></IconButton></Tooltip>
      </Stack>
      {statusError != null && <Box sx={{ mt: 1.5 }}><ErrorNotice error={statusError} retry={refresh} />{status && <Typography variant="caption" component="p" color="text.secondary" sx={{ mt: 1, mb: 0 }}>System metrics show the last successful update at {new Date(status.Timestamp).toLocaleTimeString()}.</Typography>}</Box>}
      <Box sx={{ ...metricGridSx, gap: 1.5, mt: 1.5 }}>{metrics(status).map((metric) => <MetricCard key={metric.label} metric={metric} loading={!status && statusLoading && statusError == null} />)}</Box>
    </Paper>
    <Box sx={{ ...metricGridSx, gap: 2 }}>
      {stats.map(({ label, value, icon: Icon, action }) => <ButtonBase component="button" type="button" key={label} onClick={action} disabled={!action} aria-label={`View ${label.toLowerCase()}`} sx={{ display: 'flex', flexDirection: 'column', alignItems: 'stretch', gap: '18px', p: '20px 20px 22px', borderRadius: '20px', bgcolor: colors.surface, textAlign: 'left', '&:hover': { bgcolor: colors.surfaceHover }, '&.Mui-focusVisible': { outline: '3px solid', outlineColor: 'primary.main', outlineOffset: 2 } }}>
        <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', gap: 1, color: 'text.secondary' }}><Typography>{label}</Typography><Icon aria-hidden="true" sx={{ color: colors.subtle, fontSize: 20 }} /></Stack>
        {value === undefined && overviewLoading && error == null ? <Skeleton width="50%" height={40} /> : <Typography sx={{ fontSize: 34, lineHeight: '40px', fontWeight: 600, letterSpacing: '-.5px', fontVariantNumeric: 'tabular-nums' }}>{value?.toLocaleString() ?? '—'}</Typography>}
      </ButtonBase>)}
    </Box>
    <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 380px), 1fr))', gap: 2, alignItems: 'start' }}>
      <Paper component="section" variant="outlined" sx={{ ...panelSx, pt: 2.5, px: 1, pb: 1 }}>
        <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5, pl: 2, pr: 1, pb: 1 }}><Box sx={{ flex: 1 }}><Typography variant="h3" component="h2">Recent activity</Typography><Typography variant="body2" color="text.secondary">Administrative changes and server events</Typography></Box>{onActivity && <Button onClick={onActivity} sx={{ px: 1.5 }}>View all</Button>}</Stack>
        {activityError != null && <Stack spacing={1} sx={{ mx: 2, my: 1 }}><ErrorNotice error={activityError} retry={refresh} />{activity && <Typography variant="caption" color="text.secondary">Showing the last successful activity update.</Typography>}</Stack>}
        {!activity && activityLoading && activityError == null && <Stack spacing={1} sx={{ px: 2 }} aria-label="Loading recent activity" role="status">{Array.from({ length: 6 }, (_, index) => <Skeleton key={index} height={48} />)}</Stack>}
        {activity?.Items.length === 0 && <Stack sx={{ textAlign: 'center', alignItems: 'center', px: 3, py: 5, gap: 1 }}><HistoryRounded sx={{ fontSize: 32, color: colors.subtle }} /><Typography>No recorded activity</Typography><Typography variant="body2" color="text.secondary">Administrative changes and completed tasks will appear here.</Typography></Stack>}
        {activity && activity.Items.length > 0 && <Box component="ul" aria-label="Recent activity records" sx={{ m: 0, p: 0, listStyle: 'none' }}>{activity.Items.map((entry) => <ActivityRow key={entry.Id} entry={entry} />)}</Box>}
      </Paper>
      <Stack spacing={2} sx={{ minWidth: 0 }}>
        <Paper component="section" variant="outlined" sx={{ ...panelSx, pt: 2.5, px: 3, pb: 1 }}>
          <Typography variant="h3" component="h2">Server details</Typography><Typography variant="body2" color="text.secondary">Identity and software running this server.</Typography>
          <Box component="dl" sx={{ m: 0, mt: 1.5 }}>{[
            ['Server ID', data?.Server.Id], ['Goby version', data?.Server.Version], ['Database', data ? 'PostgreSQL' : undefined], ['Go runtime', data?.Runtime.GoVersion],
          ].map(([label, value]) => <Box key={label} sx={{ display: 'grid', gridTemplateColumns: { xs: '90px minmax(0, 1fr)', sm: '104px minmax(0, 1fr)' }, gap: 2, py: 1.5, borderTop: 1, borderColor: '#EDF1F7' }}>
            <Typography component="dt" color="text.secondary">{label}</Typography><Typography component="dd" className={label === 'Server ID' ? 'mono' : undefined} sx={{ m: 0, fontSize: label === 'Server ID' ? 13 : 14, overflowWrap: 'anywhere' }}>{value ?? (!data && overviewLoading && error == null ? <Skeleton width="70%" /> : '—')}</Typography>
          </Box>)}</Box>
        </Paper>
        <Paper component="section" variant="outlined" sx={{ ...panelSx, py: 2.5, px: 3 }}>
          <Typography variant="h3" component="h2">Server capabilities</Typography><Typography variant="body2" color="text.secondary">Availability reported by this server version.</Typography>
          <Box sx={{ mt: 1 }}>{[
            { label: 'Library management', available: data?.Features.LibraryManagement }, { label: 'Media playback', available: data?.Features.Playback }, { label: 'Transcoding', available: data?.Features.Transcoding },
          ].map(({ label, available }) => <Stack key={label} direction="row" sx={{ alignItems: 'center', gap: 1.5, py: 1.25 }}>
            {available ? <CheckCircleRounded aria-hidden="true" sx={{ fontSize: 20, color: 'success.main' }} /> : <RadioButtonUncheckedRounded aria-hidden="true" sx={{ fontSize: 20, color: colors.subtle }} />}
            <Typography sx={{ flex: 1 }}>{label}</Typography>{available === undefined && overviewLoading && error == null ? <Skeleton width={70} height={24} /> : <Chip size="small" color={available ? 'success' : 'default'} label={available === undefined ? 'Unknown' : available ? 'Available' : unavailable} sx={{ borderRadius: '8px', fontWeight: 600, fontSize: 12 }} />}
          </Stack>)}</Box>
          <Divider sx={{ mt: 1, mb: 1.75 }} /><Typography variant="body2" color="text.secondary">Playback takes place in a compatible client. This dashboard manages the server.</Typography>
        </Paper>
      </Stack>
    </Box>
    <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.5, borderTop: 1, borderColor: 'divider', pt: 2, px: 0.5 }}>
      <Typography color="text.secondary" sx={{ flex: '1 1 240px' }}>Signed in as <Box component="span" sx={{ color: colors.ink, fontWeight: 600, overflowWrap: 'anywhere' }}>{user.Name}</Box>. Manage who can access your server.</Typography><Button onClick={onUsers} endIcon={<ArrowForwardRounded sx={{ fontSize: 18 }} />} sx={{ px: 1.5 }}>Manage users</Button>
    </Stack>
  </Stack>;
}
