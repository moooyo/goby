import { useEffect, useState } from 'react';
import { Skeleton, TablePagination } from './ui';
import { ImageOutlined } from './icons';
import { PersonOutlineRounded } from './icons';
import { CategoryOutlined } from './icons';
import { BusinessOutlined } from './icons';
import { LocalOfferOutlined } from './icons';
import { MusicNoteOutlined } from './icons';
import { RefreshRounded } from './icons';
import { SearchRounded } from './icons';
import { NavigateNextRounded } from './icons';
import { isAbortError } from './api';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import { catalogEntitiesApi } from './catalogEntitiesApi';
import type { CatalogEntity, CatalogEntityPage, EntityView } from './catalogEntitiesApi';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import './artwork-collections.css';

const views: { value: EntityView; label: string }[] = [
  { value: 'Person', label: 'People' }, { value: 'Genre', label: 'Genres' },
  { value: 'Studio', label: 'Studios' }, { value: 'Tag', label: 'Tags' },
  { value: 'MusicArtist', label: 'Music artists' }, { value: 'AlbumArtist', label: 'Album artists' },
  { value: 'MusicGenre', label: 'Music genres' },
];

function entityTypeName(type: string) {
  return ({ Person: 'Person', Genre: 'Genre', Studio: 'Studio', Tag: 'Tag', MusicArtist: 'Music artist', AlbumArtist: 'Album artist', MusicGenre: 'Music genre' } as Record<string, string>)[type] ?? type;
}

function EntityIcon({ item }: { item: CatalogEntity }) {
  if (item.Type === 'Person') return <PersonOutlineRounded />;
  if (item.Type === 'Studio') return <BusinessOutlined />;
  if (item.Type === 'Tag') return <LocalOfferOutlined />;
  if (['MusicArtist', 'AlbumArtist'].includes(item.Type)) return <MusicNoteOutlined />;
  return <CategoryOutlined />;
}

export function CatalogArtworkPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const { t, formatCount } = useTranslation();
  const [view, setView] = useState<EntityView>('Person');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [data, setData] = useState<CatalogEntityPage>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<CatalogEntity>();
  const refresh = () => setRevision((value) => value + 1);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setData(undefined); setError(null);
    void catalogEntitiesApi.list(view, query, page * 25, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= result.TotalRecordCount) setPage(Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1));
      else setData(result);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [view, query, page, revision]);

  return <div className="ac-page catalog-artwork-page" aria-busy={loading}>
    <div className="ac-page-heading">
      <div><h1>{t('Catalog artwork')}</h1><p>{t('People, artists, genres, studios, and tags')}</p></div>
      <button className="ac-button" type="button" disabled={loading} onClick={refresh}><RefreshRounded aria-hidden="true" />{t('Refresh')}</button>
    </div>
    <form className="ac-toolbar" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }}>
      <label className="ac-filter"><span>{t('Catalog type')}</span>
        <select value={view} onChange={(event) => { setView(event.target.value as EntityView); setPage(0); }}>
          {views.map((item) => <option key={item.value} value={item.value}>{t(item.label)}</option>)}
        </select>
      </label>
      <div className="ac-search"><SearchRounded aria-hidden="true" />
        <input aria-label={t('Search names')} placeholder={t('Search names')} value={search} onChange={(event) => setSearch(event.target.value)} autoComplete="off" />
        <button type="submit" aria-label={t('Search names')}><NavigateNextRounded aria-hidden="true" /></button>
      </div>
      <span className="ac-toolbar-count" aria-live="polite">{data ? formatCount(data.TotalRecordCount, 'entry', 'entries') : loading ? t('Loading…') : '—'}</span>
    </form>
    {error != null && <ErrorNotice error={error} retry={refresh} />}
    <section className="ac-panel" aria-label={t('Catalog entries')}>
      {loading && <div className="ac-loading" role="status" aria-label={t('Loading catalog entries')}>{[0, 1, 2, 3].map((row) => <Skeleton key={row} height={48} />)}</div>}
      {data && data.Items.length === 0 && <div className="ac-empty"><ImageOutlined aria-hidden="true" /><h2>{t('No matching entries')}</h2><p>{t('Scan a library to populate these entries, or try another search.')}</p></div>}
      {data && data.Items.length > 0 && <table className="ac-table ac-entity-table" aria-label={t('Catalog entries')}>
        <thead><tr><th>{t('Name')}</th><th>{t('Type')}</th><th className="ac-align-end">{t('Images')}</th></tr></thead>
        <tbody>{data.Items.map((item) => <tr key={item.Id}>
          <th scope="row"><button type="button" className="ac-identity" onClick={() => setSelected(item)} aria-label={t('Manage artwork for') + ' ' + item.Name}>
            <span className="ac-entity-mark" aria-hidden="true"><EntityIcon item={item} /></span>
            <span className="ac-name"><span translate="no">{item.Name}</span><span className="ac-mobile-detail">{t(entityTypeName(item.Type))}</span></span>
          </button></th>
          <td className="ac-entity-kind">{t(entityTypeName(item.Type))}</td>
          <td className="ac-align-end"><button type="button" className="ac-button ac-button-small" aria-label={t('Manage artwork for') + ' ' + item.Name} onClick={() => setSelected(item)}><ImageOutlined aria-hidden="true" /><span>{t('Manage artwork')}</span><NavigateNextRounded aria-hidden="true" /></button></td>
        </tr>)}</tbody>
      </table>}
      {data && <div className="ac-pagination"><TablePagination component="div" count={data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} onPageChange={(_, value) => setPage(value)} disabled={loading} /></div>}
    </section>
    <p className="ac-page-note">{t('Edit track and album credits in Libraries.')}</p>
    {selected && <ArtworkManagerDialog target={{ kind: 'entities', id: selected.Id, name: selected.Name }} onClose={() => setSelected(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
  </div>;
}
