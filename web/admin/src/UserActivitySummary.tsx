import { useCallback, useEffect, useState } from 'react';
import { userActivityApi } from './userActivityApi';
import type { UserActivity } from './userActivityApi';
import { useTranslation } from './i18n';
import { Button, Skeleton } from './ui';
import { ErrorNotice } from './components';
import './user-activity.css';

interface ActivitySnapshot {
  key: string;
  items?: Record<string, UserActivity>;
  loading: boolean;
  error?: unknown;
}

export function useUserActivity(ids: readonly string[], externalRevision = 0) {
  const key = JSON.stringify([...new Set(ids)].sort());
  const enabled = ids.length > 0;
  const [revision, setRevision] = useState(0);
  const [snapshot, setSnapshot] = useState<ActivitySnapshot>({ key, loading: enabled });
  const refresh = useCallback(() => setRevision((value) => value + 1), []);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(new Error('User activity did not respond within 15 seconds. Refresh to try again.')), 15000);
    setSnapshot((current) => ({ key, items: current.key === key ? current.items : undefined, loading: true }));
    let onAbort: (() => void) | undefined;
    const cancelled = new Promise<never>((_resolve, reject) => {
      onAbort = () => reject(controller.signal.reason);
      controller.signal.addEventListener('abort', onAbort, { once: true });
    });
    void Promise.race([userActivityApi.getMany(JSON.parse(key) as string[], { signal: controller.signal }), cancelled])
      .then((items) => {
        if (active) setSnapshot({ key, items: Object.fromEntries(items.map((item) => [item.Id, item])), loading: false });
      })
      .catch((error: unknown) => {
        if (active) setSnapshot((current) => ({ key, items: current.key === key ? current.items : undefined, loading: false, error }));
      })
      .finally(() => {
        clearTimeout(timeout);
        if (onAbort) controller.signal.removeEventListener('abort', onAbort);
      });
    return () => { active = false; clearTimeout(timeout); controller.abort(); };
  }, [key, enabled, revision, externalRevision]);

  return {
    items: snapshot.key === key ? snapshot.items : undefined,
    error: snapshot.key === key ? snapshot.error : undefined,
    loading: enabled && (snapshot.key !== key || snapshot.loading),
    refresh,
  };
}

export function ActivityTimestamp({ value, unavailable = false, loading = false }: { value?: string | null; unavailable?: boolean; loading?: boolean }) {
  const { t, locale } = useTranslation();
  if (value === undefined && loading) return <Skeleton width={92} height={18} aria-label={t('Loading user activity')} />;
  if (value === undefined && unavailable) return <span className="access-muted">{t('Unavailable')}</span>;
  if (!value) return <span className="access-muted">{t('Not recorded')}</span>;
  return <time dateTime={value} title={new Date(value).toLocaleString(locale)}>{new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))}</time>;
}

export function UserActivityDetails({ userId }: { userId: string }) {
  const { t, locale } = useTranslation();
  const activity = useUserActivity([userId]);
  const item = activity.items?.[userId];
  return <section className="user-activity-details" aria-label={t('Recorded user activity')} aria-busy={activity.loading}>
    <div className="user-activity-heading"><h3>{t('Activity')}</h3><Button size="small" color="secondary" disabled={activity.loading} onClick={activity.refresh}>{t('Refresh activity')}</Button></div>
    {activity.error != null && <div className="user-activity-error"><ErrorNotice error={activity.error} retry={activity.refresh} />{item && <p>{t('Showing the last successful activity update.')}</p>}</div>}
    <dl className="access-sheet-kv">
      <div><dt>{t('Last sign-in')}</dt><dd><ActivityTimestamp value={item?.LastLoginDate} unavailable={activity.error != null} loading={activity.loading} /></dd></div>
      <div><dt>{t('Last activity')}</dt><dd><ActivityTimestamp value={item?.LastActivityDate} unavailable={activity.error != null} loading={activity.loading} /></dd></div>
      <div><dt>{t('Last played')}</dt><dd><ActivityTimestamp value={item?.LastPlayedDate} unavailable={activity.error != null} loading={activity.loading} /></dd></div>
      <div><dt>{t('Watched items')}</dt><dd>{item ? new Intl.NumberFormat(locale).format(item.PlayedItemCount) : activity.loading ? <Skeleton width={48} height={18} /> : t(activity.error ? 'Unavailable' : 'Not recorded')}</dd></div>
    </dl>
    <p className="access-footnote">{t('Activity is sampled from authenticated requests. It does not mean the user is online. Watched items count completed movies and episodes once, not individual plays or viewing hours.')}</p>
  </section>;
}
