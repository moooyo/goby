import type { AttentionCheck } from './dashboardAttention';
import { useTranslation } from './i18n';
import { InfoOutlined, WarningAmberRounded } from './icons';

export function AttentionChecks({ checks }: { checks: readonly AttentionCheck[] }) {
  const { t } = useTranslation();
  return checks.map((check) => <div key={check.id} className="attention-check-status" data-check={check.id} data-state={check.state} data-stale={check.stale} role="status">
    {check.state === 'loading' ? <InfoOutlined /> : <WarningAmberRounded />}
    <div><strong>{check.title}</strong>{check.detail && <small>{check.detail}</small>}{check.stale && <small>{t('Stale')}</small>}</div>
  </div>);
}
