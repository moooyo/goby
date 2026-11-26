import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { maxUserActivityBatch, userActivityApi } from './userActivityApi';
import { useTranslation } from './i18n';
import { Skeleton } from './ui';

// Keep the lifetime copy in step with internal/identity/store.go.
const clientLoginDays = 30;
const administratorLoginHours = 24;
const millisecondsPerDay = 86_400_000;

export const idleLoginDays = 90;
export const loginLifetimes = { days: clientLoginDays, hours: administratorLoginHours };
export const loginExpiryText = 'Client logins expire after {days} days and administrator logins after {hours} hours.';
export type LoginDate = string | null | undefined;

export function daysSinceLogin(value: LoginDate, observedAt: number): number | undefined {
  return value == null ? undefined : Math.floor((observedAt - Date.parse(value)) / millisecondsPerDay);
}

export function hasNoRecentLogin(value: LoginDate, observedAt: number): boolean {
  return value === null || (daysSinceLogin(value, observedAt) ?? 0) >= idleLoginDays;
}

interface LoginRecord { date: LoginDate; idle: boolean }
interface LoginSnapshot {
  ids: readonly string[];
  records: ReadonlyMap<string, LoginRecord>;
  observedAt?: number;
  error?: unknown;
  missing: number;
}

export function useUserActivity(ids: readonly string[]) {
  const priority = useRef<readonly string[]>([]);
  const empty = useMemo<LoginSnapshot>(() => ({ ids, records: new Map(), missing: 0 }), [ids]);
  const [snapshot, setSnapshot] = useState(empty);
  const prioritize = useCallback((visibleIds: readonly string[]) => { priority.current = visibleIds; }, []);

  useEffect(() => {
    const pending = new Set(ids);
    const controller = new AbortController();
    let observedAt: number | undefined;
    let error: unknown;
    setSnapshot(empty);

    async function readBatches() {
      while (pending.size > 0 && !controller.signal.aborted) {
        // Request just the visible, unread accounts first, rather than making
        // the first paint wait for a full background batch to be processed.
        const batch = priority.current.filter((id) => pending.has(id)).slice(0, maxUserActivityBatch);
        if (batch.length === 0) {
          let remaining = maxUserActivityBatch;
          for (const id of pending) {
            batch.push(id);
            if (--remaining === 0) break;
          }
        }
        for (const id of batch) pending.delete(id);
        const request = new AbortController();
        const abort = () => request.abort();
        const timeout = setTimeout(abort, 15_000);
        controller.signal.addEventListener('abort', abort, { once: true });
        const records = new Map<string, LoginRecord>(batch.map((id) => [id, { date: undefined, idle: false }]));
        try {
          const page = await userActivityApi.getBatch(batch, { signal: request.signal });
          observedAt ??= Date.parse(page.ObservedAt);
          for (const item of page.Items) records.set(item.Id, { date: item.LastLoginDate, idle: hasNoRecentLogin(item.LastLoginDate, observedAt) });
        } catch (cause) {
          error ??= request.signal.aborted
            ? new Error('Login records did not respond within 15 seconds. Refresh to try again.')
            : cause;
        } finally {
          clearTimeout(timeout);
          controller.signal.removeEventListener('abort', abort);
        }
        if (controller.signal.aborted) return;
        // A missing user (e.g. deleted since the list loaded) is different from
        // a returned null login date. Neither it nor a failed batch implies inactivity.
        const missing = batch.reduce((count, id) => count + Number(records.get(id)?.date === undefined), 0);
        const clock = observedAt;
        const batchError = error;
        setSnapshot((previous) => {
          const current = previous.ids === ids ? previous : empty;
          const merged = new Map(current.records);
          for (const [id, record] of records) merged.set(id, record);
          return { ids, records: merged, missing: current.missing + missing, observedAt: clock, error: batchError };
        });
      }
    }

    void readBatches();
    return () => controller.abort();
  }, [empty, ids]);

  const current = snapshot.ids === ids ? snapshot : empty;
  const done = current.records.size;
  return { ...current, done, loading: done < ids.length, prioritize };
}

export function LoginTimestamp({ value, loading = false, observedAt }: { value?: LoginDate; loading?: boolean; observedAt?: number }) {
  const { t, locale } = useTranslation();
  if (value === undefined && loading) return <span aria-label={t('Loading login record')}><Skeleton width={84} height={16} /><span className="visually-hidden">{t('Loading login record')}</span></span>;
  if (value === undefined) return <span className="access-muted">{t('Unavailable')}</span>;
  const idle = observedAt !== undefined && hasNoRecentLogin(value, observedAt);
  const warning = idle ? t('No login in {days} days', { days: idleLoginDays }) : undefined;
  if (value === null) return <span className={idle ? 'users-idle-date' : 'access-muted'}>{t('No login recorded')}</span>;
  const date = new Date(value);
  const full = date.toLocaleString(locale);
  return <time dateTime={value} className={idle ? 'users-idle-date' : undefined} title={warning ? `${full} · ${warning}` : full}>{date.toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric' })}{warning && <span className="visually-hidden"> · {warning}</span>}</time>;
}

export function UserCreatedDate({ value }: { value: string }) {
  const { locale } = useTranslation();
  return <time dateTime={value} className="access-mono">{new Date(value).toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric' })}</time>;
}
