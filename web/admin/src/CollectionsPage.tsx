import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Skeleton, TablePagination } from './ui';
import { AddRounded } from './icons';
import { CollectionsBookmarkRounded } from './icons';
import { RefreshRounded } from './icons';
import { SearchRounded } from './icons';
import { NavigateNextRounded } from './icons';
import { isAbortError } from './api';
import { collectionsApi } from './collectionsApi';
import type { CollectionPage, ManagedCollection } from './collectionsApi';
import { CollectionDialog } from './CollectionDialog';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import './artwork-collections.css';

export function CollectionsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, locale, countLabel, formatCount } = useTranslation();
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState({ term: '', page: 0, limit: 25 });
  const [data, setData] = useState<CollectionPage<ManagedCollection>>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [editing, setEditing] = useState<{ id?: string }>();
  const number = new Intl.NumberFormat(locale);

  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setData(undefined);
    void collectionsApi.list('collections', query.page * query.limit, query.limit, query.term, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      if (query.page > 0 && query.page * query.limit >= value.TotalRecordCount) setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(value.TotalRecordCount / query.limit) - 1) }));
      else setData(value);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [query, revision]);

  const refresh = () => setRevision((value) => value + 1);
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setQuery((current) => ({ ...current, term: search.trim(), page: 0 })); }

  return <div className="ac-page collection-page" aria-busy={loading}>
    <div className="ac-page-heading">
      <div><h1>{t('Collections')}</h1><p>{data ? <><span className="ac-numeric">{number.format(data.TotalRecordCount)}</span> {countLabel(data.TotalRecordCount, 'collection', 'collections')}{query.term && <> · {t('Search')}: {query.term}</>}</> : t(loading ? 'Loading collections' : 'Collection list unavailable')}</p></div>
      <button className="ac-button ac-button-primary" type="button" onClick={() => setEditing({})}><AddRounded aria-hidden="true" />{t('New collection')}</button>
    </div>
    <form className="ac-toolbar" onSubmit={submit}>
      <div className="ac-search"><SearchRounded aria-hidden="true" />
        <input aria-label={t('Search collections')} placeholder={t('Search collections')} value={search} onChange={(event) => setSearch(event.target.value)} autoComplete="off" />
        <button type="submit" aria-label={t('Search')}><NavigateNextRounded aria-hidden="true" /></button>
      </div>
      <span className="ac-toolbar-count" aria-live="polite">{data ? formatCount(data.TotalRecordCount, 'collection', 'collections') : loading ? t('Loading…') : '—'}</span>
      <button className="ac-button ac-button-small" type="button" aria-label={t('Refresh')} disabled={loading} onClick={refresh}><RefreshRounded aria-hidden="true" />{t('Refresh')}</button>
    </form>
    {error != null && <ErrorNotice error={error} retry={refresh} />}
    <section className="ac-panel" id="collections-list-panel" aria-label={t('Collections')}>
      {loading && <div className="ac-loading" role="status" aria-label={t('Loading collections')}>{[0, 1, 2, 3].map((row) => <Skeleton key={row} height={58} />)}</div>}
      {data?.Items.length === 0 && <div className="ac-empty">
        <CollectionsBookmarkRounded aria-hidden="true" />
        <h2>{t(query.term ? 'No matching entries' : 'No collections yet')}</h2>
        <p>{t(query.term ? 'Try another name.' : 'Create a group to bring media from your libraries together.')}</p>
        {!query.term && <button className="ac-button" type="button" onClick={() => setEditing({})}><AddRounded aria-hidden="true" />{t('New collection')}</button>}
      </div>}
      {data && data.Items.length > 0 && <table className="ac-table ac-collection-table" aria-label={t('Collections')}>
        <thead><tr><th>{t('Name')}</th><th>{t('Items')}</th><th className="ac-optional-column">{t('Media type')}</th><th className="ac-optional-column">{t('Membership')}</th><th><span className="visually-hidden">{t('Actions')}</span></th></tr></thead>
        <tbody>{data.Items.map((entry) => <tr key={entry.Id}>
          <th scope="row"><button type="button" className="ac-identity" aria-label={t('Manage') + ' ' + entry.Name} onClick={() => setEditing({ id: entry.Id })}>
            <span className="ac-collection-mark" aria-hidden="true"><CollectionsBookmarkRounded /></span>
            <span className="ac-name"><span translate="no">{entry.Name}</span><span className="ac-name-detail">{entry.MediaType && t(entry.MediaType)}{entry.IsLocked && <> · {t('Locked')}</>}</span></span>
          </button></th>
          <td className="ac-numeric">{number.format(entry.ChildCount)}</td>
          <td className="ac-optional-column ac-muted">{entry.MediaType ? t(entry.MediaType) : '—'}</td>
          <td className="ac-optional-column ac-muted">{t(entry.IsLocked ? 'Locked' : 'Unlocked')}</td>
          <td className="ac-align-end"><button className="ac-icon-button" type="button" aria-label={t('Manage') + ' ' + entry.Name} title={t('Manage') + ' ' + entry.Name} onClick={() => setEditing({ id: entry.Id })}><NavigateNextRounded aria-hidden="true" /></button></td>
        </tr>)}</tbody>
      </table>}
      {data && <div className="ac-pagination"><TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.limit} rowsPerPageOptions={[25, 50, 100]} disabled={loading} onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => setQuery((current) => ({ ...current, limit: Number(event.target.value), page: 0 }))} /></div>}
    </section>
    {editing && <CollectionDialog key={editing.id ?? 'new'} collectionId={editing.id} onClose={() => { setEditing(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={onNavigationGuardChange} />}
  </div>;
}
