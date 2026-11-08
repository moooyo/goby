import { useCallback, useRef, useState } from 'react';
import { Box, Button, CircularProgress, IconButton, Stack, Tooltip, Typography } from './ui';
import { LibraryBooksOutlined } from './icons';
import { RefreshRounded } from './icons';
import { MoreVertRounded, SyncRounded } from './icons';
import { PageHeading } from './components';
import { useTranslation } from './i18n';
import { MediaOperationsPanel } from './MediaOperationsPanel';
import { ScanHistoryPanel } from './ScanHistoryPanel';
import { ScheduledTasksPanel } from './ScheduledTasksPanel';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import './system-pages.css';

type TasksTab = 'available' | 'history' | 'media-processing';
export interface TaskPanelStatus {
  message: string;
  active: boolean;
  refresh?: () => void;
  refreshing?: boolean;
  refreshLabel?: string;
  summary?: string;
  primaryAction?: { label: string; onClick: () => void; disabled: boolean; busy: boolean };
}

export function TasksPage({ onLibraries, currentUserId, onNavigationGuardChange, onTaskQueryCleared }: { onLibraries: () => void; currentUserId: string; onNavigationGuardChange: UserNavigationGuardChange; onTaskQueryCleared?: () => void }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<TasksTab>(() => {
    if (new URLSearchParams(window.location.search).get('task')) return 'available';
    const initialTab = window.history.state?.tasksTab;
    return initialTab === 'history' || initialTab === 'media-processing' ? initialTab : 'available';
  });
  const navigationGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const viewMenu = useRef<HTMLDetailsElement>(null);
  const [status, setStatus] = useState<TaskPanelStatus>();
  const onStatusChange = useCallback((value: TaskPanelStatus) => setStatus(value), []);
  const setNavigationGuard = useCallback((guard: UserNavigationGuard | undefined) => {
    navigationGuard.current = guard;
    onNavigationGuardChange(guard);
  }, [onNavigationGuardChange]);

  function closeViewMenu() {
    if (!viewMenu.current) return;
    viewMenu.current.open = false;
    viewMenu.current.querySelector('summary')?.focus();
  }

  function clearTaskQuery(nextTab: TasksTab = tab) {
    const url = new URL(window.location.href);
    url.searchParams.delete('task');
    window.history.replaceState({ ...window.history.state, tasksTab: nextTab }, '', url);
    onTaskQueryCleared?.();
  }

  function changeTab(next: TasksTab) {
    if (next === tab) { closeViewMenu(); return; }
    if (navigationGuard.current && !navigationGuard.current()) return;
    setStatus(undefined);
    setTab(next);
    closeViewMenu();
    clearTaskQuery(next);
  }

  return <Box className="system-page tasks-page">
    <PageHeading title="Tasks" description={status?.summary ?? 'Monitor running work and manage server schedules.'} action={<Stack direction="row" className="system-task-heading-actions">
      {status?.primaryAction ? <Button variant="outlined" startIcon={status.primaryAction.busy ? <CircularProgress size={15} color="inherit" /> : <SyncRounded />} disabled={status.primaryAction.disabled} onClick={status.primaryAction.onClick}>{status.primaryAction.label}</Button> : <Button variant="outlined" startIcon={<LibraryBooksOutlined />} onClick={onLibraries}>Manage libraries</Button>}
      {status?.refresh && <Tooltip title={t(status.message)}><span><IconButton aria-label={status.refreshLabel ?? 'Refresh tasks'} onClick={status.refresh} disabled={status.refreshing} size="small"><RefreshRounded sx={{ fontSize: 18 }} /></IconButton></span></Tooltip>}
      <details className="system-task-view-menu" ref={viewMenu} onKeyDown={(event) => { if (event.key === 'Escape') { event.preventDefault(); closeViewMenu(); } }} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) event.currentTarget.open = false; }}>
        <summary aria-label={t('More task views')} title={t('More task views')}><MoreVertRounded /></summary>
        <div className="system-task-view-options" role="group" aria-label={t('Task views')}>
          <button type="button" aria-pressed={tab === 'available'} id="available-tasks-tab" onClick={() => changeTab('available')}>{t('Available tasks')}</button>
          <button type="button" aria-pressed={tab === 'history'} id="scan-history-tab" onClick={() => changeTab('history')}>{t('Scan history')}</button>
          <button type="button" aria-pressed={tab === 'media-processing'} id="media-processing-tab" onClick={() => changeTab('media-processing')}>{t('Media processing')}</button>
        </div>
      </details>
    </Stack>} />
    {tab !== 'available' && <div className="system-task-secondary-view"><Button size="small" onClick={() => changeTab('available')}>Back to task overview</Button><Typography variant="caption" color="text.secondary">{tab === 'history' ? 'Scan history' : 'Media processing'}</Typography></div>}
    {tab === 'available' && <Box role="region" id="available-tasks-panel" aria-label="Available tasks"><ScheduledTasksPanel currentUserId={currentUserId} onNavigationGuardChange={setNavigationGuard} onStatusChange={onStatusChange} onTaskQueryCleared={onTaskQueryCleared} /></Box>}
    {tab === 'history' && <Box role="region" id="scan-history-panel" aria-label="Scan history"><ScanHistoryPanel onLibraries={onLibraries} onStatusChange={onStatusChange} /></Box>}
    {tab === 'media-processing' && <Box role="region" id="media-processing-panel" aria-label="Media processing"><MediaOperationsPanel onNavigationGuardChange={setNavigationGuard} onStatusChange={onStatusChange} /></Box>}
    {status && <p className="system-task-updates"><span className={`system-status-dot${status.active ? ' is-connected' : ''}`} aria-hidden="true" />{t(status.message)}</p>}
  </Box>;
}
