import { Alert, Button } from './ui';
import { RefreshRounded } from './icons';
import { ApiError } from './api';
import type { TaskDefinition } from './api';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { taskHistoryRunLimit, taskHistoryTaskLimit } from './useTaskHistory';
import type { TaskHistoryIssue } from './useTaskHistory';

export function TaskHistoryNotice({ tasks, issues, loading, hasOlderActiveRuns, disabled, onRefresh }: { tasks: TaskDefinition[]; issues: TaskHistoryIssue[]; loading: boolean; hasOlderActiveRuns: boolean; disabled: boolean; onRefresh: () => void }) {
  const { t } = useTranslation();
  const names = new Map(tasks.map((task) => [task.Id, task.Name]));
  const reasons = {
    request: t('The history request failed. Other loaded runs remain available.'),
    timeout: t('History loading reached its time limit. Reload to try again.'),
    'run-limit': t('Only the newest {count} records per task were checked. More runs may fall within the last 24 hours.', { count: taskHistoryRunLimit }),
    'task-limit': t('History is loaded for at most {count} tasks. Other tasks may have additional runs.', { count: taskHistoryTaskLimit }),
    changed: t('History changed while it was loading. Reload to check for missing runs.'),
    conflict: t('Run identifiers conflict across task responses. Each identifier is shown once; reload to confirm the records.'),
    unconfirmed: t('A recorded active run is no longer confirmed by current task status. Reload to check its final state.'),
  };
  return <>
    {loading && <p className="system-task-history-loading" role="status">{t('Loading recent run history...')}</p>}
    {hasOlderActiveRuns && <p className="system-task-history-loading">{t('Active runs created earlier are also shown.')}</p>}
    {issues.length > 0 && <Alert severity="warning" className="system-task-history-notice" action={<Button color="inherit" size="small" startIcon={<RefreshRounded />} disabled={loading || disabled} onClick={onRefresh}>Reload recent history</Button>}>
      <strong>{t('Recent history is incomplete.')}</strong>
      <details><summary>{t('History loading details')}</summary><ul>{issues.map((issue, index) => <li key={(issue.taskId ?? 'all') + ':' + issue.reason + ':' + index}>
        {issue.taskId && <strong>{t(names.get(issue.taskId) ?? issue.taskId)}: </strong>}<span className="system-task-history-reason">{reasons[issue.reason]}</span>
        {issue.error != null && <span className="system-task-history-error">{t(errorText(issue.error))}{issue.error instanceof ApiError && issue.error.requestId && <> · {t('Request ID')}: {issue.error.requestId}</>}</span>}
      </li>)}</ul></details>
    </Alert>}
  </>;
}
