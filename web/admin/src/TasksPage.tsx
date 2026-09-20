import { useCallback, useRef, useState } from 'react';
import { Box, Button, Tab, Tabs } from '@mui/material';
import LibraryBooksOutlined from '@mui/icons-material/LibraryBooksOutlined';
import { PageHeading } from './components';
import { MediaOperationsPanel } from './MediaOperationsPanel';
import { ScanHistoryPanel } from './ScanHistoryPanel';
import { ScheduledTasksPanel } from './ScheduledTasksPanel';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';

type TasksTab = 'available' | 'history' | 'media-processing';

export function TasksPage({ onLibraries, currentUserId, onNavigationGuardChange }: { onLibraries: () => void; currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [tab, setTab] = useState<TasksTab>(() => {
    const initialTab = window.history.state?.tasksTab;
    return initialTab === 'history' || initialTab === 'media-processing' ? initialTab : 'available';
  });
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => {
    navigationGuard.current = guard;
    onNavigationGuardChange(guard);
  }, [onNavigationGuardChange]);

  function changeTab(next: TasksTab) {
    if (next === tab || (navigationGuard.current && !navigationGuard.current())) return;
    setTab(next);
    window.history.replaceState({ ...window.history.state, tasksTab: next }, '');
  }

  return <Box>
    <PageHeading title="Tasks" description="Run library scans, process media, refresh metadata, download subtitles, maintain caches, and manage task schedules." action={<Button variant="outlined" startIcon={<LibraryBooksOutlined />} onClick={onLibraries}>Manage libraries</Button>} />
    <Tabs value={tab} onChange={(_event, next: TasksTab) => changeTab(next)} aria-label="Task views" variant="scrollable" allowScrollButtonsMobile sx={{ borderBottom: 1, borderColor: 'divider', mb: 3 }}>
      <Tab value="available" label="Available tasks" id="available-tasks-tab" aria-controls="available-tasks-panel" />
      <Tab value="history" label="Scan history" id="scan-history-tab" aria-controls="scan-history-panel" />
      <Tab value="media-processing" label="Media processing" id="media-processing-tab" aria-controls="media-processing-panel" />
    </Tabs>
    {tab === 'available' && <Box role="tabpanel" id="available-tasks-panel" aria-labelledby="available-tasks-tab"><ScheduledTasksPanel currentUserId={currentUserId} onNavigationGuardChange={setNavigationGuard} /></Box>}
    {tab === 'history' && <Box role="tabpanel" id="scan-history-panel" aria-labelledby="scan-history-tab"><ScanHistoryPanel onLibraries={onLibraries} /></Box>}
    {tab === 'media-processing' && <Box role="tabpanel" id="media-processing-panel" aria-labelledby="media-processing-tab"><MediaOperationsPanel onNavigationGuardChange={setNavigationGuard} /></Box>}
  </Box>;
}
