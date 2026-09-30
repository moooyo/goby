import { useCallback, useRef, useState } from 'react';
import { Box, Button, IconButton, Stack, Tab, Tabs, Tooltip, Typography } from '@mui/material';
import LibraryBooksOutlined from '@mui/icons-material/LibraryBooksOutlined';
import CheckRounded from '@mui/icons-material/CheckRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { PageHeading } from './components';
import { MediaOperationsPanel } from './MediaOperationsPanel';
import { ScanHistoryPanel } from './ScanHistoryPanel';
import { ScheduledTasksPanel } from './ScheduledTasksPanel';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';

type TasksTab = 'available' | 'history' | 'media-processing';
export interface TaskPanelStatus { message: string; active: boolean; refresh?: () => void; refreshing?: boolean; refreshLabel?: string }

export function TasksPage({ onLibraries, currentUserId, onNavigationGuardChange }: { onLibraries: () => void; currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [tab, setTab] = useState<TasksTab>(() => {
    const initialTab = window.history.state?.tasksTab;
    return initialTab === 'history' || initialTab === 'media-processing' ? initialTab : 'available';
  });
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const [status, setStatus] = useState<TaskPanelStatus>();
  const onStatusChange = useCallback((value: TaskPanelStatus) => setStatus(value), []);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => {
    navigationGuard.current = guard;
    onNavigationGuardChange(guard);
  }, [onNavigationGuardChange]);

  function changeTab(next: TasksTab) {
    if (next === tab || (navigationGuard.current && !navigationGuard.current())) return;
    setStatus(undefined);
    setTab(next);
    window.history.replaceState({ ...window.history.state, tasksTab: next }, '');
  }

  return <Box>
    <PageHeading title="Tasks" description="Run library scans, process media, refresh metadata, download subtitles, maintain caches, and manage task schedules." action={<Button variant="outlined" startIcon={<LibraryBooksOutlined />} onClick={onLibraries}>Manage libraries</Button>} />
    <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1.5, mb: 2.5 }}>
    <Tabs value={tab} onChange={(_event, next: TasksTab) => changeTab(next)} aria-label="Task views" variant="scrollable" allowScrollButtonsMobile sx={{ width: 'fit-content', maxWidth: '100%', minHeight: 40, border: '1px solid', borderColor: 'text.disabled', borderRadius: '24px', '& .MuiTabs-indicator': { display: 'none' }, '& .MuiTab-root': { minHeight: 38, minWidth: 0, px: { xs: 1, sm: 2.25 }, py: 1, borderRight: '1px solid', borderColor: 'text.disabled', fontSize: { xs: 12, sm: 13 }, fontWeight: 500, '& .MuiTab-icon': { display: { xs: 'none', sm: 'inline-flex' } }, '&:last-of-type': { borderRight: 0 }, '&.Mui-selected': { color: '#0F2A57', bgcolor: '#D8E4FA', fontWeight: 600 } } }}>
      <Tab value="available" icon={tab === 'available' ? <CheckRounded sx={{ fontSize: 17 }} /> : undefined} iconPosition="start" label="Available tasks" id="available-tasks-tab" aria-controls="available-tasks-panel" />
      <Tab value="history" icon={tab === 'history' ? <CheckRounded sx={{ fontSize: 17 }} /> : undefined} iconPosition="start" label="Scan history" id="scan-history-tab" aria-controls="scan-history-panel" />
      <Tab value="media-processing" icon={tab === 'media-processing' ? <CheckRounded sx={{ fontSize: 17 }} /> : undefined} iconPosition="start" label="Media processing" id="media-processing-tab" aria-controls="media-processing-panel" />
    </Tabs>
    {status && <Stack direction="row" sx={{ alignItems: 'center', gap: 1, minWidth: 0 }}>
      <Box aria-hidden="true" sx={{ width: 5, height: 5, flexShrink: 0, borderRadius: '50%', bgcolor: status.active ? 'success.main' : 'text.disabled' }} />
      <Typography variant="caption" color="text.secondary">{status.message}</Typography>
      {status.refresh && <Tooltip title={status.refreshLabel ?? 'Refresh tasks'}><span><IconButton aria-label={status.refreshLabel ?? 'Refresh tasks'} onClick={status.refresh} disabled={status.refreshing} size="small"><RefreshRounded sx={{ fontSize: 18 }} /></IconButton></span></Tooltip>}
    </Stack>}
    </Stack>
    {tab === 'available' && <Box role="tabpanel" id="available-tasks-panel" aria-labelledby="available-tasks-tab"><ScheduledTasksPanel currentUserId={currentUserId} onNavigationGuardChange={setNavigationGuard} onStatusChange={onStatusChange} /></Box>}
    {tab === 'history' && <Box role="tabpanel" id="scan-history-panel" aria-labelledby="scan-history-tab"><ScanHistoryPanel onLibraries={onLibraries} onStatusChange={onStatusChange} /></Box>}
    {tab === 'media-processing' && <Box role="tabpanel" id="media-processing-panel" aria-labelledby="media-processing-tab"><MediaOperationsPanel onNavigationGuardChange={setNavigationGuard} onStatusChange={onStatusChange} /></Box>}
  </Box>;
}
