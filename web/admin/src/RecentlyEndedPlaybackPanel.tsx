import { useEffect, useState } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { ActivityResponse } from './api';
import { ErrorNotice } from './components';
import { RefreshRounded } from './icons';
import { useTranslation } from './i18n';
import { Box, Button, Skeleton, Stack, Typography } from './ui';

const requestTimeout = 15000;
const recentLimit = 6;
const timeoutMessage = 'Recently ended playback did not respond within 15 seconds. Refresh to try again.';

export function RecentlyEndedPlaybackPanel() {
  const { t, locale } = useTranslation();
  const [revision, setRevision] = useState(0);
  const [loaded, setLoaded] = useState<{ revision: number; result: ActivityResponse }>();
  const [failure, setFailure] = useState<{ revision: number; cause: unknown }>();
  const data = loaded?.revision === revision ? loaded.result : undefined;
  const failed = failure?.revision === revision;
  const loading = !data && !failed;
  const items = data?.Items.filter((entry) => entry.Action === 'playback.stopped').slice(0, recentLimit) ?? [];

  useEffect(() => {
    let active = true;
    let timedOut = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const controller = new AbortController();
    const timeoutError = new ApiError(timeoutMessage, { code: 'playback_activity_timeout' });
    const deadline = new Promise<never>((_resolve, reject) => {
      timer = setTimeout(() => {
        timedOut = true;
        controller.abort();
        reject(timeoutError);
      }, requestTimeout);
    });
    void Promise.race([
      adminApi.getActivity({ Action: 'playback.stopped', StartIndex: 0, Limit: recentLimit }, { signal: controller.signal }),
      deadline,
    ]).then((result) => {
      if (active && !controller.signal.aborted) setLoaded({ revision, result });
    }).catch((cause: unknown) => {
      if (active && (timedOut || (!controller.signal.aborted && !isAbortError(cause)))) {
        setFailure({ revision, cause: timedOut ? timeoutError : cause });
      }
    }).finally(() => clearTimeout(timer));
    return () => {
      active = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [revision]);

  function refresh() { setRevision((value) => value + 1); }

  return <section className="access-panel" aria-label={t('Recently ended')} aria-busy={loading}>
    <div className="access-panel-heading">
      <h2>{t('Recently ended')}</h2>
      <Button size="small" color="secondary" startIcon={<RefreshRounded fontSize="small" />} aria-label={t('Refresh recently ended playback')} onClick={refresh} disabled={loading}>{t('Refresh')}</Button>
    </div>
    {loading && <Stack role="status" aria-live="polite" aria-label={t('Loading recently ended playback')} spacing={1} sx={{ px: 2, py: 2, borderTop: 1, borderColor: 'divider' }}>
      <Skeleton height={32} /><Skeleton height={32} /><Skeleton height={32} />
      <Typography variant="caption" color="text.secondary">{t('Loading recently ended playback')}</Typography>
    </Stack>}
    {failed && <Box sx={{ p: 2, borderTop: 1, borderColor: 'divider' }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
    {data && items.length === 0 && <Box className="access-empty" role="status" sx={{ borderTop: 1, borderColor: 'divider' }}>
      <h3>{t('No recently ended playback')}</h3>
      <p>{t('No retained playback stop reports are available.')}</p>
    </Box>}
    {data && items.length > 0 && <Box component="ul" aria-label={t('Recently ended playback')} sx={{ listStyle: 'none', m: 0, p: 0 }}>
      {items.map((entry) => {
        const itemName = entry.Resource.Name;
        const userName = entry.Actor.Name;
        return <Box component="li" key={entry.Id} sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: 'divider' }}>
          <Stack direction="row" sx={{ alignItems: 'flex-start', justifyContent: 'space-between', flexWrap: 'wrap', columnGap: 3, rowGap: 1 }}>
            <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              <Typography variant="body2" component="div" sx={{ fontWeight: 600 }}><span translate={itemName?.trim() ? 'no' : undefined}>{itemName?.trim() ? itemName : t('Item unavailable')}</span></Typography>
              <Typography variant="caption" component="div" color="text.secondary" sx={{ mt: 0.35 }}><span translate={userName?.trim() ? 'no' : undefined}>{userName?.trim() ? userName : t('User unavailable')}</span></Typography>
            </Box>
            <Typography variant="caption" color="text.secondary" component="div" sx={{ minWidth: 0, overflowWrap: 'anywhere', fontVariantNumeric: 'tabular-nums' }}>{t('Ended at')} <time dateTime={entry.Date} title={entry.Date}>{new Date(entry.Date).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</time></Typography>
          </Stack>
        </Box>;
      })}
    </Box>}
    <Box sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: 'divider' }}>
      <Typography variant="caption" component="p" color="text.secondary" sx={{ m: 0, lineHeight: 1.65 }}>{t('Recent playback stop reports retained for up to {days} days. These events are not complete playback statistics.', { days: data?.RetentionDays ?? 30 })}</Typography>
      <Typography variant="caption" component="p" color="text.secondary" sx={{ mt: 0.5, mb: 0, lineHeight: 1.65 }}>{t('Names reflect currently available users and media.')} {t('Times use your local time zone.')}</Typography>
    </Box>
  </section>;
}
