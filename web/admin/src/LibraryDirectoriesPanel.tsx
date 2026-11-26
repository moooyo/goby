import { useCallback, useEffect, useId, useMemo, useState } from 'react';
import { adminApi } from './api';
import type { LibraryStorageDirectoriesResponse, LibraryStorageDirectory } from './api';
import { errorText } from './components';
import { directoryCapacity, directoryReadAccess, formatDirectoryBytes, formatDirectoryCapacity, uniqueDirectoryPaths } from './directoryCapacity';
import { RefreshRounded } from './icons';
import { useTranslation } from './i18n';
import { Chip } from './ui';
import './storage-observations.css';

interface DirectoryResource {
  data?: LibraryStorageDirectoriesResponse;
  error?: unknown;
  loading: boolean;
  checkedAt?: Date;
}

export function useLibraryDirectories(revision: number) {
  const [resource, setResource] = useState<DirectoryResource>({ loading: true });
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let disposed = false;
    let deadline: ReturnType<typeof setTimeout> | undefined;
    let cancel: (() => void) | undefined;
    setResource((current) => ({ ...current, loading: true }));
    async function read() {
      try {
        const cancelled = new Promise<never>((_resolve, reject) => {
          cancel = () => reject(controller.signal.reason);
          controller.signal.addEventListener('abort', cancel, { once: true });
        });
        deadline = setTimeout(() => controller.abort(new Error('Directory capacity did not respond within 15 seconds. Refresh to try again.')), 15_000);
        const data = await Promise.race([adminApi.getLibraryStorageDirectories({ signal: controller.signal }), cancelled]);
        if (!disposed) setResource({ data, loading: false, checkedAt: new Date() });
      } catch (error) {
        if (!disposed) setResource((current) => ({ ...current, error, loading: false }));
      } finally {
        clearTimeout(deadline);
        if (cancel) controller.signal.removeEventListener('abort', cancel);
      }
    }
    void read();
    return () => { disposed = true; clearTimeout(deadline); controller.abort(); };
  }, [revision, retry]);
  const directories = useMemo(() => new Map(resource.data?.Items.map((directory) => [directory.Path, directory] as const)), [resource.data]);
  const reload = useCallback(() => setRetry((current) => current + 1), []);
  return { ...resource, directories, reload };
}

type LibraryDirectoriesResource = ReturnType<typeof useLibraryDirectories>;

function accessObservation(directory?: LibraryStorageDirectory): { label: string; detail: string; tone: 'success' | 'warning' | 'unknown' } {
  const readable = directoryReadAccess(directory);
  if (readable === false) return {
    label: directory?.Available ? 'Not readable' : 'Unavailable',
    detail: 'Check the directory mount and service read permissions.', tone: 'warning',
  };
  if (readable == null) return { label: 'Read access unknown', detail: 'Read access has not been established.', tone: 'unknown' };
  if (directory?.Writable == null) return { label: 'Write access unknown', detail: 'The directory is readable. Write access could not be established.', tone: 'unknown' };
  return { label: directory.Writable ? 'Read and write' : 'Read only', detail: 'Effective service permissions are checked without modifying files.', tone: 'success' };
}

function DirectoryRow({ path, directory, loading, stale, libraryNames }: { path: string; directory?: LibraryStorageDirectory; loading: boolean; stale: boolean; libraryNames?: readonly string[] }) {
  const { t, locale } = useTranslation();
  const access = accessObservation(directory);
  const capacity = directoryCapacity(directory);
  const hasBytes = capacity.total != null || capacity.used != null || capacity.available != null;
  const value = formatDirectoryCapacity(capacity, locale);
  const percent = capacity.percent == null ? null : new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(capacity.percent);
  const available = capacity.available == null ? t('Available space unknown') : t('{value} available', { value: formatDirectoryBytes(capacity.available, locale) });
  const exact = [
    capacity.used == null ? t('Used space unknown') : `${t('Used space')}: ${capacity.used.toString()} B`,
    capacity.total == null ? t('Total capacity unknown') : `${t('Total capacity')}: ${capacity.total.toString()} B`,
    capacity.available == null ? t('Available space unknown') : `${t('Available')}: ${capacity.available.toString()} B`,
  ].join(' · ');
  const accessLabel = t(!directory && loading ? 'Checking access…' : access.label);
  return <div className="storage-directory-row" role="listitem" aria-label={path} data-access={access.tone} data-warning={capacity.warning} data-stale={stale && !!directory}>
    <div className="storage-directory-identity">
      <div className="storage-directory-path">
        {!libraryNames && <i className={`storage-access-dot storage-access-${access.tone}`} aria-hidden="true" />}
        <span translate="no" title={path}>{path}</span>
        {libraryNames && <Chip size="small" variant="outlined" color={stale || access.tone === 'unknown' ? 'default' : access.tone} label={accessLabel} title={t(access.detail)} />}
      </div>
      {libraryNames ? <div className="storage-directory-libraries">
        <span translate="no">{libraryNames.join(' · ') || t('Using libraries not reported')}</span>
        {stale && directory && <span className="storage-stale-label"> · {t('Last successful sample')}</span>}
      </div> : <div className="storage-directory-permission" title={t(access.detail)}>{accessLabel}{stale && directory && <span className="storage-stale-label"> · {t('Last successful sample')}</span>}</div>}
    </div>
    <div className="storage-directory-capacity" title={exact}>
      {hasBytes ? <>
        <div className="storage-directory-capacity-value"><span translate="no">{value}</span>{percent != null && <span className={capacity.warning ? 'storage-capacity-warning' : undefined} translate="no">{percent}%</span>}</div>
        {capacity.percent != null && <div className={`storage-capacity-meter${capacity.warning ? ' storage-capacity-meter-warning' : ''}`} role="progressbar" aria-label={t('Directory usage for {path}', { path })} aria-valuenow={capacity.percent} aria-valuemin={0} aria-valuemax={100} aria-valuetext={`${value} · ${available}`}><i style={{ width: `${capacity.percent}%` }} /></div>}
        {(libraryNames || !capacity.complete) && <span className={`storage-capacity-detail${capacity.warning ? ' storage-capacity-warning' : ''}`}>{available}</span>}
      </> : <div className="storage-capacity-empty"><span>{t(!directory && loading ? 'Checking capacity…' : 'Capacity unavailable')}</span><small>{t(directoryReadAccess(directory) === false ? 'The directory cannot be read.' : 'No capacity measurement is available.')}</small></div>}
    </div>
  </div>;
}

interface LibraryDirectoriesPanelProps {
  paths?: readonly string[];
  revision?: number;
}

const noLibraryNames: readonly string[] = [];

export function LibraryDirectoriesPanel({ paths, revision = 0 }: LibraryDirectoriesPanelProps) {
  const resource = useLibraryDirectories(revision);
  return <LibraryDirectoriesPanelContent paths={paths} resource={resource} />;
}

export function LibraryDirectoriesPanelContent({ paths, resource, librariesByPath }: {
  paths?: readonly string[];
  resource: LibraryDirectoriesResource;
  librariesByPath?: ReadonlyMap<string, readonly string[]>;
}) {
  const { t, locale } = useTranslation();
  const titleId = useId();
  const showingStorage = librariesByPath !== undefined;
  const registeredPaths = uniqueDirectoryPaths(paths ?? [...resource.directories.keys()]);
  const stale = resource.error != null && resource.data != null;
  const incomplete = resource.data != null && registeredPaths.some((path) => {
    const directory = resource.directories.get(path);
    return !directory || !directoryCapacity(directory).complete || directoryReadAccess(directory) == null || directory.Writable == null;
  });
  return <section className="storage-directories-panel" aria-labelledby={titleId} aria-busy={resource.loading} data-stale={stale}>
    <div className="storage-directories-heading">
      <h2 id={titleId}>{t(showingStorage ? 'Storage directories' : 'Media directories')}</h2>
      {showingStorage ? <span className="storage-directory-caption">{t('Registered media directories and space on their disks')}</span> : <span className="storage-directory-count">{paths || resource.data ? registeredPaths.length.toLocaleString(locale) : '—'}</span>}
      <button type="button" className="storage-directory-refresh" aria-label={t('Refresh directory capacity')} title={t('Refresh directory capacity')} disabled={resource.loading} onClick={resource.reload}><RefreshRounded aria-hidden="true" sx={{ fontSize: 15 }} /></button>
    </div>
    {resource.error != null && <div className="storage-directory-notice" role="alert"><strong>{t(stale ? 'Directory observations are stale.' : 'Directory observations unavailable.')}</strong><span>{t(errorText(resource.error))}</span></div>}
    {!resource.data && resource.loading && registeredPaths.length === 0 && <div className="storage-directories-loading" role="status">{t('Checking directory capacity…')}</div>}
    {registeredPaths.length > 0 && <div role="list" aria-label={t('Registered media directories')}>{registeredPaths.map((path) => <DirectoryRow key={path} path={path} directory={resource.directories.get(path)} loading={resource.loading} stale={stale} libraryNames={showingStorage ? librariesByPath.get(path) ?? noLibraryNames : undefined} />)}</div>}
    {resource.data && registeredPaths.length === 0 && <p className="storage-directories-empty">{t('No registered media directories')}</p>}
    {incomplete && <p className="storage-directory-partial" role="status">{t('Some directory details are unavailable. Known values are shown.')}</p>}
    <p className="storage-directories-note">
      {t('Capacity is shown for each registered directory. Directories on the same disk share capacity; these values must not be added together.')}
      {showingStorage && <><br />{t('Overview warns when a directory reaches 90% used space.')}{resource.checkedAt && <span className="storage-directory-checked"> · <time dateTime={resource.checkedAt.toISOString()}>{t('Last checked {time}', { time: resource.checkedAt.toLocaleTimeString(locale) })}</time></span>}</>}
    </p>
  </section>;
}
