import { Alert, Button } from './ui';
import { RefreshRounded } from './icons';
import { errorText } from './components';
import { useTranslation } from './i18n';
import { taskInitiatorRecordLimit } from './useTaskInitiators';
import type { TaskInitiator, TaskInitiatorIssue } from './useTaskInitiators';

const sourceLabels = new Map([
  ['manual', 'Manual'], ['schedule', 'Scheduled'], ['startup', 'Server startup'], ['system_event', 'System event'],
]);

export function taskRunSourceLabel(source: string, t: (key: string) => string): string {
  const label = sourceLabels.get(source);
  return label === undefined ? source : t(label);
}

export function TaskRunOrigin({ source, actor, loading }: { source: string; actor?: TaskInitiator; loading: boolean }) {
  const { t } = useTranslation();
  const label = actor ? actor.Kind === 'system' ? t('System') : actor.Kind === 'application_key'
    ? actor.Id ? t('API key #{id}', { id: actor.Id }) : t('API key')
    : actor.Name || t('User name unavailable') : t(loading ? 'Loading initiator...' : 'Initiator unknown');
  return <span className="system-task-origin"><span className="system-task-run-source" translate="no">{taskRunSourceLabel(source, t)}</span><span aria-hidden="true"> · </span><span className="system-task-initiator" translate="no" data-actor-state={actor ? 'known' : loading ? 'loading' : 'unknown'} data-actor-kind={actor?.Kind} aria-busy={loading} title={label} aria-description={t('Initiator names reflect current account names.')}>{label}</span></span>;
}

export function TaskInitiatorNotice({ issues, loading, disabled, onRefresh }: { issues: TaskInitiatorIssue[]; loading: boolean; disabled: boolean; onRefresh: () => void }) {
  const { t } = useTranslation();
  const reasons = {
    request: t('Admission activity could not be loaded. Unconfirmed initiators remain unknown.'),
    timeout: t('Initiator lookup reached its time limit. Unconfirmed initiators remain unknown.'),
    limit: t('Only the newest {count} admission activities were checked. Other initiators remain unknown.', { count: taskInitiatorRecordLimit }),
    changed: t('Admission activity changed during loading. Some initiators could not be confirmed.'),
    mismatch: t('Some activity records did not match the admission query and were not associated with a run.'),
    conflict: t('Conflicting admission actors were returned for a run. Its initiator remains unknown.'),
  };
  return <>
    {loading && <p className="system-task-history-loading" role="status" aria-busy="true">{t('Loading run initiators...')}</p>}
    {issues.length > 0 && <Alert severity="warning" className="system-task-history-notice" action={<Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={onRefresh} disabled={loading || disabled}>Reload initiators</Button>}>
      <strong>{t('Some run initiators could not be confirmed.')}</strong><details><summary>{t('Initiator lookup details')}</summary><ul>{issues.map((issue, index) => <li key={issue.reason + ':' + index}><span className="system-task-history-reason">{reasons[issue.reason]}</span>{issue.error != null && <span className="system-task-history-error">{t(errorText(issue.error))}</span>}</li>)}</ul></details>
    </Alert>}
  </>;
}
