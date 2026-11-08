import { useEffect, useRef, useState } from 'react';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import { OpenInNewRounded, RefreshRounded } from './icons';
import { SettingsGroup } from './SettingsLayout';
import { Alert, Button, CircularProgress, Skeleton } from './ui';
import { updatesApi } from './updatesApi';
import type { ServerUpdates, UpdateState } from './updatesApi';
import './server-updates.css';

interface Props { disabled?: boolean }
const requestTimeout = 15000;
const stateCopy: Record<UpdateState, { title: string; description: string }> = {
  unconfigured: { title: 'Release source not configured', description: 'Configure a trusted release source in the deployment to enable update checks. No default source is used.' },
  not_checked: { title: 'Updates have not been checked', description: 'Check the release source to find out whether a newer version is available.' },
  failed: { title: 'Update check failed', description: 'The release source could not be checked. The server update status is unknown.' },
  current: { title: 'No newer release found', description: 'The last successful check found no release newer than the running version.' },
  available: { title: 'Update available', description: 'A newer release is available from the configured source.' },
  version_unknown: { title: 'Running version cannot be compared', description: 'The release source was checked successfully. This build version cannot be compared with the reported release.' },
};

export function ServerUpdatePanel({ disabled = false }: Props) {
  const { t, locale } = useTranslation();
  const [saved, setSaved] = useState<ServerUpdates>();
  const [loadVersion, setLoadVersion] = useState(0);
  const [loading, setLoading] = useState(true);
  const [checking, setChecking] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mounted = useRef(true);
  const loadingRef = useRef(true);
  const inFlight = useRef(false);
  const staleRef = useRef(false);
  const mutation = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; mutation.current?.abort(); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    let disposed = false;
    let timedOut = false;
    loadingRef.current = true;
    setLoading(true);
    setError(null);
    const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, requestTimeout);
    void updatesApi.get({ signal: controller.signal }).then((value) => {
      if (disposed) return;
      staleRef.current = false;
      setSaved(value);
      setStale(false);
    }).catch((cause: unknown) => {
      if (disposed || !timedOut && isAbortError(cause)) return;
      staleRef.current = true;
      setStale(true);
      setError(timedOut ? new ApiError('The cached update result did not respond within 15 seconds. Reload to try again.', { code: 'network_error' }) : cause);
    }).finally(() => {
      clearTimeout(deadline);
      if (!disposed) { loadingRef.current = false; setLoading(false); }
    });
    return () => { disposed = true; clearTimeout(deadline); controller.abort(); };
  }, [loadVersion]);

  function reload() {
    if (disabled || loadingRef.current || inFlight.current) return;
    loadingRef.current = true;
    setLoading(true);
    setLoadVersion((value) => value + 1);
  }

  async function check() {
    if (disabled || loadingRef.current || inFlight.current || staleRef.current || !saved?.ConfiguredSource) return;
    inFlight.current = true;
    const controller = new AbortController();
    mutation.current = controller;
    let timedOut = false;
    setChecking(true);
    setError(null);
    const deadline = setTimeout(() => { timedOut = true; controller.abort(); }, requestTimeout);
    try {
      const value = await updatesApi.check({ signal: controller.signal });
      if (!mounted.current) return;
      staleRef.current = false;
      setSaved(value);
      setStale(false);
    } catch (cause) {
      if (!mounted.current || !timedOut && isAbortError(cause)) return;
      staleRef.current = true;
      setStale(true);
      setError(timedOut ? new ApiError('The update check timed out. Reload the cached result to confirm its outcome.', { code: 'network_error' }) : cause);
    } finally {
      clearTimeout(deadline);
      if (mutation.current === controller) mutation.current = undefined;
      inFlight.current = false;
      if (mounted.current) setChecking(false);
    }
  }

  const busy = loading || checking;
  const copy = saved ? stateCopy[saved.State] : undefined;
  const heading = checking ? 'Checking for updates…' : loading ? 'Loading cached update result…' : stale ? 'Update result needs to be refreshed' : copy?.title;
  return <div className="server-updates" aria-busy={busy}>
    <SettingsGroup title="Server updates" description="Check the release source configured by this deployment. The result is cached until you request another check.">
      <div className="server-updates-summary" role="status" aria-live="polite" aria-atomic="true" data-state={stale ? 'stale' : saved?.State}>
        {heading && <strong>{t(heading)}</strong>}
        {!busy && !stale && copy && <p>{t(copy.description)}</p>}
        {checking && <p>{t('The last confirmed result stays visible while the release source is checked.')}</p>}
      </div>
      {loading && !saved && <Skeleton variant="rounded" height={65} />}
      {error != null && <ErrorNotice error={error} />}
      {stale && <Alert severity="warning" role="note">The latest result could not be confirmed. Any release details shown are from the last confirmed result. Reload the cached result before checking again.</Alert>}
      {saved && <>
        <dl className="server-updates-facts">
          <div><dt>{t('Running version')}</dt><dd><code translate="no">{saved.CurrentVersion || t('Unknown version')}</code></dd></div>
          <div><dt>{t(stale ? 'Last confirmed check attempt' : 'Last check attempt')}</dt><dd>{saved.CheckedAt ? <time dateTime={saved.CheckedAt}>{new Date(saved.CheckedAt).toLocaleString(locale)}</time> : t('Not checked yet')}</dd></div>
        </dl>
        {saved.Release && <div className="server-updates-release">
          <div className="server-updates-release-heading"><h3>{t(stale ? 'Release from last confirmed result' : 'Release reported by source')}</h3><span className="server-updates-classification">{t(saved.Release.Classification === 'Release' ? 'Stable release' : saved.Release.Classification === 'Beta' ? 'Beta release' : 'Development release')}</span></div>
          <dl className="server-updates-facts">
            <div><dt>{t('Release version')}</dt><dd><code translate="no">{saved.Release.Version}</code></dd></div>
            <div><dt>{t('Release published at')}</dt><dd><time dateTime={saved.Release.PublishedAt}>{new Date(saved.Release.PublishedAt).toLocaleString(locale)}</time></dd></div>
          </dl>
          {saved.Release.Description && <div className="server-updates-notes"><h4>{t('Release notes')}</h4><p translate="no">{saved.Release.Description}</p></div>}
          <a className="server-updates-details-link" href={saved.Release.InfoUrl} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">{t('View release details')}<OpenInNewRounded aria-hidden="true" /></a>
          <details className="server-updates-artifact"><summary>{t('Release artifact')}</summary><dl className="server-updates-facts"><div><dt>{t('Target file')}</dt><dd><code translate="no">{saved.Release.TargetFilename}</code></dd></div><div><dt>SHA256</dt><dd><code translate="no">{saved.Release.SHA256}</code></dd></div></dl></details>
        </div>}
      </>}
      <div className="server-updates-actions">
        <Button type="button" size="small" onClick={reload} disabled={disabled || busy} startIcon={<RefreshRounded />}>Reload cached result</Button>
        <Button type="button" size="small" variant="outlined" onClick={() => void check()} disabled={disabled || busy || stale || !saved?.ConfiguredSource} startIcon={checking ? <CircularProgress size={15} /> : <RefreshRounded />}>{t(checking ? 'Checking for updates…' : 'Check for updates')}</Button>
      </div>
      <p className="server-updates-deployment">{t('Deploy the updated Docker image through your deployment tools. Release checks do not change the running image.')}</p>
    </SettingsGroup>
  </div>;
}
