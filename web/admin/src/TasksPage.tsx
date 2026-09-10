import { useState } from 'react';
import { Box, Button, Tab, Tabs } from '@mui/material';
import LibraryBooksOutlined from '@mui/icons-material/LibraryBooksOutlined';
import { PageHeading } from './components';
import { ScanHistoryPanel } from './ScanHistoryPanel';
import { ScheduledTasksPanel } from './ScheduledTasksPanel';
import type { UserNavigationGuardChange } from './userDraftNavigation';

export function TasksPage({ onLibraries, currentUserId, onNavigationGuardChange }: { onLibraries: () => void; currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [tab, setTab] = useState<'available' | 'history'>(() => window.history.state?.tasksTab === 'history' ? 'history' : 'available');
  return <Box>
    <PageHeading title="Tasks" description="Start server tasks, manage their schedules, and review library work." action={<Button variant="outlined" startIcon={<LibraryBooksOutlined />} onClick={onLibraries}>Manage libraries</Button>} />
    <Tabs value={tab} onChange={(_event, next: 'available' | 'history') => { setTab(next); window.history.replaceState({ ...window.history.state, tasksTab: next }, ''); }} aria-label="Task views" variant="scrollable" allowScrollButtonsMobile sx={{ borderBottom: 1, borderColor: 'divider', mb: 3 }}>
      <Tab value="available" label="Available tasks" id="available-tasks-tab" aria-controls="available-tasks-panel" />
      <Tab value="history" label="Scan history" id="scan-history-tab" aria-controls="scan-history-panel" />
    </Tabs>
    {tab === 'available' && <Box role="tabpanel" id="available-tasks-panel" aria-labelledby="available-tasks-tab"><ScheduledTasksPanel currentUserId={currentUserId} onNavigationGuardChange={onNavigationGuardChange} /></Box>}
    {tab === 'history' && <Box role="tabpanel" id="scan-history-panel" aria-labelledby="scan-history-tab"><ScanHistoryPanel onLibraries={onLibraries} /></Box>}
  </Box>;
}
