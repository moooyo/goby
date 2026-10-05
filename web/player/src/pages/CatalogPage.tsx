import { useEffect, useRef, useState, type RefObject } from 'react';
import { Icon, MediaImage, PosterGrid } from '../components';
import { useApp } from '../context';
import { api, ApiError } from '../lib/api';
import type { ItemQuery, ItemsResponse, MediaItem, SearchHint } from '../types';
import './catalog.css';

type CatalogKind = 'movies' | 'series' | 'favorites' | 'search';
type SortKey = 'DateCreated' | 'CommunityRating' | 'ProductionYear' | 'SortName';
type FavoriteKind = 'all' | 'Movie' | 'Series';
type VideoFilter = 'all' | '4k' | 'hdr' | 'hlg' | 'dolby-vision';
type CatalogResponse = ItemsResponse & { hints?: SearchHint[]; received: number };
type GenreCard = { image?: MediaItem; count: number };
const PAGE_SIZE = 48;
const VIDEO_FILTERS: Array<{ value: VideoFilter; label: string; range?: string }> = [
  { value: 'all', label: '全部' },
  { value: '4k', label: '4K' },
  { value: 'hdr', label: 'HDR', range: 'Hdr10,Hdr10Plus' },
  { value: 'hlg', label: 'HLG', range: 'HyperLogGamma' },
  { value: 'dolby-vision', label: 'Dolby Vision', range: 'DolbyVision' },
];
const SORTS: Array<{ value: SortKey; label: string }> = [
  { value: 'DateCreated', label: '最近添加' },
  { value: 'CommunityRating', label: '评分' },
  { value: 'ProductionYear', label: '年份' },
  { value: 'SortName', label: '名称' },
];

function readStored<T,>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) as T : fallback;
  } catch {
    return fallback;
  }
}

function saveStored(key: string, value: unknown) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* Preferences remain usable without browser storage. */ }
}

function loadingError(reason: unknown) {
  if (reason instanceof ApiError && reason.status === 403) return '没有访问这些影片的权限。';
  if (reason instanceof ApiError && reason.status === 401) return '登录已过期，请重新登录。';
  return '暂时无法加载影片，请检查服务器连接后重试。';
}

function hintKey(hint: SearchHint): string { return `${hint.GobyReference.Kind}:${hint.GobyReference.Id}`; }

function hintItem(hint: SearchHint): MediaItem {
  return { Id: hint.GobyReference.Id, Name: hint.Name, Type: hint.Type, ProductionYear: hint.ProductionYear,
    ImageTags: { ...(hint.PrimaryImageTag ? { Primary: hint.PrimaryImageTag } : {}), ...(hint.ThumbImageTag ? { Thumb: hint.ThumbImageTag } : {}) },
    BackdropImageTags: hint.BackdropImageTag ? [hint.BackdropImageTag] : undefined, PrimaryImageAspectRatio: hint.PrimaryImageAspectRatio };
}

function SearchEntityImage({ hint }: { hint: SearchHint }) {
  const [failed, setFailed] = useState(false);
  const path = hint.PrimaryImageUrl || hint.ThumbImageUrl;
  useEffect(() => setFailed(false), [path]);
  return <span className="catalog-entity-image">{path && !failed ? <img src={api.mediaUrl(path)} alt="" loading="lazy" onError={() => setFailed(true)} /> : hint.Type === 'Person' ? Array.from(hint.Name)[0] : <Icon name="film" size={18} />}</span>;
}

function VideoFilterOptions({ value, onChange }: { value: VideoFilter; onChange: (value: VideoFilter) => void }) {
  return <div className="catalog-resolution" aria-label="画质筛选">{VIDEO_FILTERS.map(option => <button key={option.value} className={value === option.value ? 'is-active' : ''} aria-pressed={value === option.value} onClick={() => onChange(option.value)}>{option.label}</button>)}</div>;
}

function useRailControls(ref: RefObject<HTMLDivElement | null>, contentKey: string) {
  const [edges, setEdges] = useState({ left: false, right: false });
  useEffect(() => {
    const rail = ref.current;
    if (!rail) return;
    const update = () => {
      const left = rail.scrollLeft > 1;
      const right = rail.scrollWidth - rail.clientWidth - rail.scrollLeft > 2;
      setEdges(previous => previous.left === left && previous.right === right ? previous : { left, right });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(rail);
    Array.from(rail.children).forEach(child => observer.observe(child));
    rail.addEventListener('scroll', update, { passive: true });
    return () => { observer.disconnect(); rail.removeEventListener('scroll', update); };
  }, [ref, contentKey]);
  const move = (direction: number) => {
    const rail = ref.current;
    rail?.scrollBy({ left: direction * rail.clientWidth * .7, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  };
  return { ...edges, move };
}

function RailArrows({ label, controls }: { label: string; controls: ReturnType<typeof useRailControls> }) {
  return <>{([-1, 1] as const).map(direction => {
    const available = direction < 0 ? controls.left : controls.right;
    return <button key={direction} className={`catalog-rail-arrow ${direction < 0 ? 'is-left' : 'is-right'}`} hidden={!available} aria-label={`向${direction < 0 ? '左' : '右'}浏览${label}`} onClick={() => controls.move(direction)}><Icon name={direction < 0 ? 'chevronLeft' : 'chevronRight'} size={16}/></button>;
  })}</>;
}

function CatalogView({ kind }: { kind: CatalogKind }) {
  const { session, navigate, revision, setBackdrop } = useApp();
  const storagePrefix = `goby.catalog.${session?.ServerId ?? ''}.${session?.User.Id ?? ''}`;
  const [library, setLibrary] = useState(() => readStored(`${storagePrefix}.${kind}.library`, ''));
  const [libraries, setLibraries] = useState<MediaItem[]>([]);
  const [genres, setGenres] = useState<MediaItem[]>([]);
  const [genreFilter, setGenreFilter] = useState({ name: '', id: '' });
  const genre = genreFilter.name;
  const setGenre = (name: string, id = '') => setGenreFilter({ name, id });
  const [sort, setSort] = useState<SortKey>('DateCreated');
  const [unwatched, setUnwatched] = useState(false);
  const [videoFilter, setVideoFilter] = useState<VideoFilter>('all');
  const [favoriteKind, setFavoriteKind] = useState<FavoriteKind>('all');
  const [favoriteCounts, setFavoriteCounts] = useState<{ all?: number; Movie?: number; Series?: number }>({});
  const [term, setTerm] = useState('');
  const [query, setQuery] = useState('');
  const [hints, setHints] = useState<SearchHint[]>([]);
  const [person, setPerson] = useState<MediaItem | null>(null);
  const [year, setYear] = useState('');
  const [yearInput, setYearInput] = useState('');
  const [recentSearches, setRecentSearches] = useState<string[]>(() => readStored(`${storagePrefix}.searches`, []));
  const [genreCards, setGenreCards] = useState<Record<string, GenreCard>>({});
  const [items, setItems] = useState<MediaItem[]>([]);
  const [total, setTotal] = useState(0);
  const [libraryTotal, setLibraryTotal] = useState<number>();
  const [browseBackdrop, setBrowseBackdrop] = useState<MediaItem | null>(null);
  const [received, setReceived] = useState(0);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');
  const [menu, setMenu] = useState<'sort' | 'filter' | 'year' | null>(null);
  const [reload, setReload] = useState(0);
  const requestSequence = useRef(0);
  const genreScope = useRef('');
  const libraryTabsRef = useRef<HTMLDivElement>(null);
  const genreTabsRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const wall = kind === 'movies' || kind === 'series';
  const isSearch = kind === 'search';
  const isFavorites = kind === 'favorites';
  const hasSearchFilters = Boolean(genre || person || year || videoFilter !== 'all');
  const hasSearch = Boolean(query || hasSearchFilters);
  const showingHints = isSearch && Boolean(query) && !hasSearchFilters;
  const entities = showingHints ? hints.filter(entry => entry.GobyReference.Kind === 'Entity') : [];
  const activeFilterCount = Number(unwatched) + Number(videoFilter !== 'all');
  const orderedGenres = [...genres].sort((first, second) => (genreCards[second.Id || second.Name]?.count ?? -1) - (genreCards[first.Id || first.Name]?.count ?? -1));
  const genreOrderKey = orderedGenres.map(item => item.Id || item.Name).join('\0');
  const libraryControls = useRailControls(libraryTabsRef, libraries.map(item => `${item.Id}:${item.Name}`).join('\0'));
  const genreControls = useRailControls(genreTabsRef, genreOrderKey);
  const videoFilterLabel = VIDEO_FILTERS.find(option => option.value === videoFilter)?.label ?? '全部';
  const searchContext = [person ? `${person.Name}的作品` : '', genre, year ? `${year} 年` : '', videoFilter !== 'all' ? videoFilterLabel : ''].filter(Boolean).join(' · ');

  useEffect(() => {
    setBackdrop(null, 'blur');
    return () => { requestSequence.current += 1; };
  }, [setBackdrop]);

  useEffect(() => {
    if (!wall) return;
    let active = true;
    api.views().then((response) => {
      if (!active) return;
      const relevant = response.Items.filter((item) => !item.CollectionType || item.CollectionType === 'mixed' || item.CollectionType === (kind === 'movies' ? 'movies' : 'tvshows'));
      setLibraries(relevant);
      if (library && !relevant.some((item) => item.Id === library)) setLibrary('');
    }).catch(() => { if (active) setLibraries([]); });
    return () => { active = false; };
  }, [kind, wall]);

  useEffect(() => {
    if (!wall && !isSearch) return;
    let active = true;
    const scope = `${kind}:${library}`;
    genreScope.current = '';
    setGenres([]);
    setGenreCards({});
    api.genres(library || undefined, kind === 'movies' ? 'Movie' : kind === 'series' ? 'Series' : 'Movie,Series').then((response) => {
      if (active) { genreScope.current = scope; setGenres(response.Items); }
    }).catch(() => { if (active) setGenres([]); });
    return () => { active = false; };
  }, [library, wall, isSearch, kind]);

  useEffect(() => {
    if (!wall) return;
    let active = true;
    setLibraryTotal(undefined);
    api.items({ ParentId: library || undefined, IncludeItemTypes: kind === 'movies' ? 'Movie' : 'Series', Recursive: true, Limit: 0 }).then(response => {
      if (active) setLibraryTotal(response.TotalRecordCount);
    }).catch(() => { if (active) setLibraryTotal(undefined); });
    return () => { active = false; };
  }, [kind, library, wall, revision]);

  useEffect(() => {
    if (!isSearch && !isFavorites) return;
    let active = true;
    api.items({ IncludeItemTypes: 'Movie,Series', Recursive: true, Limit: 1, SortBy: 'DateCreated', SortOrder: 'Descending' }).then(response => {
      if (active) setBrowseBackdrop(response.Items[0] ?? null);
    }).catch(() => {});
    return () => { active = false; };
  }, [isSearch, isFavorites, revision]);

  useEffect(() => {
    if ((isSearch || isFavorites) && !loading) setBackdrop(items[0] ?? browseBackdrop, 'blur');
  }, [isSearch, isFavorites, loading, items, browseBackdrop, setBackdrop]);

  useEffect(() => {
    if (!isFavorites) return;
    let active = true;
    Promise.all([
      api.favorites({ IncludeItemTypes: 'Movie,Series', Limit: 0 }),
      api.favorites({ IncludeItemTypes: 'Movie', Limit: 0 }),
      api.favorites({ IncludeItemTypes: 'Series', Limit: 0 }),
    ]).then(([all, movies, series]) => {
      if (active) setFavoriteCounts({ all: all.TotalRecordCount, Movie: movies.TotalRecordCount, Series: series.TotalRecordCount });
    }).catch(() => { if (active) setFavoriteCounts({}); });
    return () => { active = false; };
  }, [isFavorites, revision]);

  useEffect(() => {
    if (isSearch) searchRef.current?.focus();
  }, [isSearch]);

  useEffect(() => {
    if (!isSearch) return;
    const timer = window.setTimeout(() => setQuery(term.trim()), 350);
    return () => window.clearTimeout(timer);
  }, [term, isSearch]);

  useEffect(() => {
    if ((!isSearch && !wall) || !genres.length || genreScope.current !== `${kind}:${library}`) return;
    let active = true;
    let next = 0;
    const cards: Record<string, GenreCard> = {};
    const collect = async () => {
      while (active && next < genres.length) {
        const item = genres[next++];
        const supplied = item as MediaItem & { ItemCount?: number };
        const count = supplied.ItemCount ?? item.ChildCount ?? item.RecursiveItemCount;
        if (!isSearch && count !== undefined) {
          cards[item.Id || item.Name] = { count };
          continue;
        }
        try {
          const response = await api.items({
            ParentId: wall ? library || undefined : undefined,
            GenreIds: item.Id || undefined, Genres: item.Id ? undefined : item.Name,
            IncludeItemTypes: kind === 'movies' ? 'Movie' : kind === 'series' ? 'Series' : 'Movie,Series',
            Recursive: true, Limit: isSearch ? 1 : 0, SortBy: 'DateCreated', SortOrder: 'Descending',
          });
          if (active) cards[item.Id || item.Name] = { image: response.Items[0], count: count ?? response.TotalRecordCount };
        } catch { /* Keep the server order when an individual genre count is unavailable. */ }
      }
    };
    void Promise.all(Array.from({ length: Math.min(4, genres.length) }, collect)).then(() => { if (active) setGenreCards(cards); });
    return () => { active = false; };
  }, [genres, isSearch, wall, library, kind, revision]);

  useEffect(() => {
    const tabs = genreTabsRef.current;
    if (!wall || !tabs) return;
    const revealSelection = () => {
      const selected = tabs.querySelector<HTMLButtonElement>('button[aria-pressed="true"]');
      if (!selected) return;
      const container = tabs.getBoundingClientRect();
      const button = selected.getBoundingClientRect();
      // Keep the active label before the trailing fade when filtering changes
      // the tool width or asynchronous genre counts reorder the tabs.
      if (button.left < container.left) tabs.scrollLeft += button.left - container.left;
      else if (button.right > container.right - 40) tabs.scrollLeft += button.right - container.right + 40;
    };
    revealSelection();
    const observer = new ResizeObserver(revealSelection);
    observer.observe(tabs);
    return () => observer.disconnect();
  }, [wall, genre, genreOrderKey]);

  useEffect(() => {
    if (!menu) return;
    const close = (event: PointerEvent) => { if (!menuRef.current?.contains(event.target as Node)) setMenu(null); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { menuRef.current?.querySelector<HTMLButtonElement>('button[aria-expanded="true"]')?.focus(); setMenu(null); } };
    document.addEventListener('pointerdown', close);
    document.addEventListener('keydown', escape);
    return () => {
      document.removeEventListener('pointerdown', close);
      document.removeEventListener('keydown', escape);
    };
  }, [menu]);

  const makeQuery = (startIndex = 0): ItemQuery => ({
    IncludeItemTypes: kind === 'movies' ? 'Movie' : kind === 'series' ? 'Series' : isFavorites && favoriteKind !== 'all' ? favoriteKind : 'Movie,Series',
    Recursive: true,
    Limit: PAGE_SIZE,
    StartIndex: startIndex,
    ParentId: wall ? library || undefined : undefined,
    Genres: genreFilter.id ? undefined : genre || undefined,
    GenreIds: genreFilter.id || undefined,
    PersonIds: person?.Id,
    Years: year || undefined,
    IsPlayed: unwatched ? false : undefined,
    Is4K: videoFilter === '4k' ? true : undefined,
    ExtendedVideoTypes: VIDEO_FILTERS.find(option => option.value === videoFilter)?.range,
    GobyAggregateVideoFilters: (kind === 'series' || isSearch) && videoFilter !== 'all' ? true : undefined,
    SortBy: isSearch ? 'SortName' : sort,
    SortOrder: isSearch || sort === 'SortName' ? 'Ascending' : 'Descending',
  });

  async function fetchPage(startIndex: number): Promise<CatalogResponse> {
    if (showingHints) {
      const response = await api.searchHints(query, startIndex, PAGE_SIZE);
      const physical = response.SearchHints.filter(entry => entry.GobyReference.Kind === 'Item');
      // Hydrate only physical IDs; entity IDs can collide with these IDs.
      const details = physical.length ? await api.items({ Ids: physical.map(entry => entry.GobyReference.Id).join(','), Limit: physical.length }).catch(() => undefined) : undefined;
      const indexed = new Map(details?.Items.map(item => [item.Id, item]));
      return { Items: physical.map(entry => indexed.get(entry.GobyReference.Id) ?? hintItem(entry)), hints: response.SearchHints, received: response.SearchHints.length, TotalRecordCount: response.TotalRecordCount };
    }
    const response = isFavorites ? await api.favorites(makeQuery(startIndex)) : await api.items(makeQuery(startIndex));
    return { ...response, received: response.Items.length };
  }

  useEffect(() => {
    const sequence = ++requestSequence.current;
    setLoadingMore(false);
    setError('');
    setHints([]);
    setReceived(0);
    setTotal(0);
    if (isSearch && !hasSearch) {
      setItems([]);
      setTotal(0);
      setLoading(false);
      return;
    }
    setLoading(true);
    setItems([]);
    fetchPage(0).then((response) => {
      if (sequence !== requestSequence.current) return;
      setItems(response.Items);
      setHints(response.hints ?? []);
      setReceived(response.received);
      setTotal(response.TotalRecordCount);
      if (wall && response.Items[0]) setBackdrop(response.Items[0], 'blur');
    }).catch((reason: unknown) => {
      if (sequence === requestSequence.current) setError(loadingError(reason));
    }).finally(() => { if (sequence === requestSequence.current) setLoading(false); });
  }, [kind, library, genre, genreFilter.id, sort, unwatched, videoFilter, favoriteKind, query, person, year, revision, reload]);

  async function loadMore() {
    if (loadingMore || loading) return;
    const sequence = requestSequence.current;
    setLoadingMore(true);
    setError('');
    try {
      const response = await fetchPage(received);
      if (sequence !== requestSequence.current) return;
      setItems((previous) => {
        const existing = new Set(previous.map((item) => item.Id));
        return [...previous, ...response.Items.filter((item) => !existing.has(item.Id))];
      });
      setHints(previous => {
        const existing = new Set(previous.map(hintKey));
        return [...previous, ...(response.hints ?? []).filter(entry => !existing.has(hintKey(entry)))];
      });
      setReceived(value => value + response.received);
      setTotal(response.TotalRecordCount);
    } catch (reason) {
      if (sequence === requestSequence.current) setError(loadingError(reason));
    } finally {
      if (sequence === requestSequence.current) setLoadingMore(false);
    }
  }

  function chooseLibrary(value: string) {
    setLibrary(value);
    setGenre('');
    saveStored(`${storagePrefix}.${kind}.library`, value);
  }

  function clearVideoFilters() {
    setUnwatched(false);
    setVideoFilter('all');
  }

  function resetFilters() {
    setGenre('');
    clearVideoFilters();
    setMenu(null);
  }

  function rememberSearch(value: string) {
    const normalized = value.trim();
    if (!normalized) return;
    const next = [normalized, ...recentSearches.filter((entry) => entry !== normalized)].slice(0, 8);
    setRecentSearches(next);
    saveStored(`${storagePrefix}.searches`, next);
  }

  function chooseSearch(value: string) {
    setGenre('');
    setPerson(null);
    setYear('');
    setYearInput('');
    setVideoFilter('all');
    setTerm(value);
    setQuery(value.trim());
    rememberSearch(value);
  }

  function clearSearchFilters() {
    setGenre('');
    setPerson(null);
    setYear('');
    setYearInput('');
    setVideoFilter('all');
  }

  function chooseSearchYear(value: string) {
    setTerm('');
    setQuery('');
    setYear(value);
    setYearInput(value);
    menuRef.current?.querySelector<HTMLButtonElement>('button[aria-expanded="true"]')?.focus();
    setMenu(null);
  }

  function chooseSearchVideo(value: VideoFilter) {
    setTerm('');
    setQuery('');
    setVideoFilter(value);
  }

  return (
    <main className={`catalog-page catalog-page--${kind}`}>
      {wall && <>
        {libraries.length > 1 ? <div className="catalog-libraries-wrap"><div className="catalog-libraries" aria-label="媒体库" ref={libraryTabsRef}>
          {[{ Id: '', Name: '全部' }, ...libraries].map((item) => <button key={item.Id} className={library === item.Id ? 'is-active' : ''} aria-pressed={library === item.Id} onClick={() => chooseLibrary(item.Id)}>{item.Name}</button>)}
        </div><RailArrows label="媒体库" controls={libraryControls}/></div> : <h1 className="catalog-title">{kind === 'movies' ? '电影' : '剧集'}</h1>}
        <p className="catalog-count" aria-live="polite">{loading ? '正在加载…' : `${total}${libraryTotal !== undefined && total !== libraryTotal ? ` / ${libraryTotal}` : ''} ${kind === 'movies' ? '部影片' : '部剧集'}`}</p>
        <div className="catalog-toolbar">
          <div className={`catalog-genres-wrap${genreControls.left ? ' has-left' : ''}`}><div className="catalog-genres" aria-label="类型筛选" ref={genreTabsRef}>
            {[{ Id: '', Name: '全部' }, ...orderedGenres].map((item) => <button key={item.Id || item.Name} className={genre === (item.Id ? item.Name : '') ? 'is-active' : ''} aria-pressed={genre === (item.Id ? item.Name : '')} onClick={() => setGenre(item.Id ? item.Name : '')}>{item.Name}<span /></button>)}
          </div><RailArrows label="类型" controls={genreControls}/></div>
          <div className="catalog-tools" ref={menuRef}>
            <div className="catalog-menu-anchor">
              <button className={`catalog-tool${menu === 'sort' ? ' is-open' : ''}`} aria-label="排序" aria-expanded={menu === 'sort'} aria-haspopup="menu" onClick={() => setMenu(menu === 'sort' ? null : 'sort')}><Icon name="sort" size={17} />{SORTS.find((option) => option.value === sort)?.label}<Icon name="chevronDown" size={14} /></button>
              {menu === 'sort' && <div className="catalog-menu catalog-menu--sort" role="menu" aria-label="排序方式"><div className="catalog-menu-label">排序方式</div>{SORTS.map((option) => <button key={option.value} role="menuitemradio" aria-checked={sort === option.value} onClick={() => { setSort(option.value); setMenu(null); }}>{option.label}{sort === option.value && <Icon name="check" size={16} />}</button>)}</div>}
            </div>
            <div className="catalog-menu-anchor">
              <button className={`catalog-tool${menu === 'filter' ? ' is-open' : ''}`} aria-label="筛选" aria-expanded={menu === 'filter'} aria-haspopup="dialog" onClick={() => setMenu(menu === 'filter' ? null : 'filter')}><span className="catalog-filter-icon"><Icon name="settings" size={17} />{activeFilterCount > 0 && <i />}</span>筛选{activeFilterCount > 0 ? ` · ${activeFilterCount}` : ''}</button>
              {menu === 'filter' && <div className="catalog-menu catalog-menu--filter" role="dialog" aria-label="筛选">
                <div className="catalog-menu-label">画质</div>
                <VideoFilterOptions value={videoFilter} onChange={setVideoFilter}/>
                <button className="catalog-unwatched" role="switch" aria-checked={unwatched} onClick={() => setUnwatched(!unwatched)}><span>只看未看完</span><span className={`catalog-switch${unwatched ? ' is-on' : ''}`}><i /></span></button>
                {activeFilterCount > 0 && <button className="catalog-clear" onClick={clearVideoFilters}>清除筛选</button>}
              </div>}
            </div>
          </div>
        </div>
      </>}

      {isFavorites && <div className="catalog-favorites-heading">
        <h1 className="catalog-title">收藏<span>{favoriteCounts.all === undefined ? '' : `${favoriteCounts.all} 部`}</span></h1>
        <div className="catalog-segments" aria-label="收藏类型">{([{ value: 'all', label: '全部' }, { value: 'Movie', label: '电影' }, { value: 'Series', label: '剧集' }] as const).map((option) => <button key={option.value} className={favoriteKind === option.value ? 'is-active' : ''} aria-pressed={favoriteKind === option.value} onClick={() => setFavoriteKind(option.value)}>{option.label} {favoriteCounts[option.value]}</button>)}</div>
      </div>}

      {isSearch && <>
        <form className="catalog-search-box" role="search" onSubmit={(event) => { event.preventDefault(); chooseSearch(term); }}>
          <Icon name="search" size={22} />
          <input ref={searchRef} aria-label="搜索片名、演员、导演、类型或年份" placeholder="搜索片名、演员、导演、类型或年份" value={term} onChange={(event) => { clearSearchFilters(); setTerm(event.target.value); }} onBlur={() => rememberSearch(term)} />
          {term && <button type="button" aria-label="清除搜索" onClick={() => { chooseSearch(''); searchRef.current?.focus(); }}><Icon name="close" size={16} /></button>}
        </form>
        <div className="catalog-search-filters" ref={menuRef}>
          <span className="catalog-search-scope">{showingHints ? '名称搜索' : hasSearchFilters ? '影片筛选' : '按年份或画质浏览'}</span>
          <div className="catalog-menu-anchor">
            <button className={`catalog-tool${menu === 'year' ? ' is-open' : ''}`} aria-label="年份筛选" aria-expanded={menu === 'year'} aria-haspopup="dialog" onClick={() => { setYearInput(year); setMenu(menu === 'year' ? null : 'year'); }}>年份{year ? ` · ${year}` : ''}<Icon name="chevronDown" size={14}/></button>
            {menu === 'year' && <form className="catalog-menu catalog-menu--year" role="dialog" aria-label="年份筛选" onSubmit={event => { event.preventDefault(); if (/^\d{4}$/.test(yearInput) && Number(yearInput) > 0) chooseSearchYear(yearInput); }}>
              <label className="catalog-menu-label" htmlFor="catalog-search-year">上映年份</label>
              <input id="catalog-search-year" inputMode="numeric" pattern="[0-9]{4}" maxLength={4} placeholder="例如 2024" value={yearInput} onChange={event => setYearInput(event.target.value.replace(/\D/g, '').slice(0, 4))} autoFocus required/>
              {showingHints && <p className="catalog-filter-note">应用后将退出名称搜索，按年份和画质浏览影片。</p>}
              <div className="catalog-year-actions"><button type="button" onClick={() => chooseSearchYear('')}>全部年份</button><button type="submit" disabled={!/^\d{4}$/.test(yearInput) || Number(yearInput) === 0}>应用</button></div>
            </form>}
          </div>
          <div className="catalog-menu-anchor">
            <button className={`catalog-tool${menu === 'filter' ? ' is-open' : ''}`} aria-label="画质筛选" aria-expanded={menu === 'filter'} aria-haspopup="dialog" onClick={() => setMenu(menu === 'filter' ? null : 'filter')}>画质{videoFilter !== 'all' ? ` · ${videoFilterLabel}` : ''}<Icon name="chevronDown" size={14}/></button>
            {menu === 'filter' && <div className="catalog-menu catalog-menu--filter" role="dialog" aria-label="画质筛选"><div className="catalog-menu-label">画质</div><VideoFilterOptions value={videoFilter} onChange={chooseSearchVideo}/>{showingHints && <p className="catalog-filter-note">选择后将退出名称搜索，按年份和画质浏览影片。</p>}</div>}
          </div>
        </div>
        {!hasSearch && <>
          {recentSearches.length > 0 && <section className="catalog-recent-searches"><div className="catalog-section-heading"><h2>最近搜索</h2><button onClick={() => { setRecentSearches([]); saveStored(`${storagePrefix}.searches`, []); }}>清除</button></div><div className="catalog-search-chips">{recentSearches.map((value) => <button key={value} onClick={() => chooseSearch(value)}>{value}</button>)}</div></section>}
          {genres.length > 0 && <section className="catalog-genre-browser"><h2>按类型浏览</h2><div className="catalog-genre-tiles">{orderedGenres.slice(0, 8).map((item) => <button key={item.Id || item.Name} className="catalog-genre-tile" onClick={() => { setTerm(''); setQuery(''); setGenre(item.Name); }}>
            {genreCards[item.Id || item.Name]?.image && <MediaImage item={genreCards[item.Id || item.Name].image!} kind="Backdrop" />}
            <span className="catalog-genre-scrim" /><span className="catalog-genre-title">{item.Name}{genreCards[item.Id || item.Name] && <small>{genreCards[item.Id || item.Name].count} 部</small>}</span>
          </button>)}</div></section>}
          {!genres.length && <div className="catalog-search-hint"><Icon name="search" size={38} /><p>输入片名，寻找下一部想看的影片</p></div>}
        </>}
      </>}

      {(!isSearch || hasSearch) && <div className={`catalog-results${isSearch ? ' catalog-results--search' : ''}`}>
        {showingHints && <div className="catalog-people">
          {entities.map(entry => <button key={hintKey(entry)} aria-label={`${entry.Name}，${entry.Type === 'Person' ? '演职员作品' : '类型影片'}`} onClick={() => entry.Type === 'Person' ? setPerson(hintItem(entry)) : setGenre(entry.Name, entry.GobyReference.Id)}><SearchEntityImage hint={entry} /><strong>{entry.Name}</strong><small>{entry.Type === 'Person' ? '演职员' : '类型'}</small></button>)}
          {/^(19|20)\d{2}$/.test(query) && <button onClick={() => chooseSearchYear(query)}><Icon name="film" size={18} />浏览 {query} 年影片</button>}
        </div>}
        {isSearch && !loading && (total > 0 || hasSearchFilters) && <div className="catalog-search-count" aria-live="polite"><span>{searchContext || `“${query}”`} · {total} {showingHints ? '条结果' : '部影片'}</span>{hasSearchFilters && <button onClick={clearSearchFilters}>清除筛选 <Icon name="close" size={14} /></button>}</div>}
        {loading ? <div className="catalog-loading" role="status" aria-label="正在加载影片"><div className="catalog-skeleton-grid">{Array.from({ length: 12 }, (_, index) => <div className="catalog-skeleton" key={index}><div /><span /><span /></div>)}</div></div> : <PosterGrid items={items} variant={isFavorites ? 'favorites' : isSearch ? 'search' : 'catalog'} />}
        {error && <div className="catalog-error" role="alert"><p>{error}</p><button onClick={() => received ? void loadMore() : setReload((value) => value + 1)}>重新加载</button></div>}
        {!loading && !error && !items.length && !entities.length && <div className="catalog-empty">
          {isFavorites && <Icon name="heart" size={44} strokeWidth={1.5} />}
          <h2>{isFavorites ? '这里还空着' : isSearch && query ? `没有找到“${query}”` : '没有符合条件的影片'}</h2>
          <p>{isFavorites ? '在详情页或预览卡上点 ♡，影片就会出现在这里' : isSearch ? '换个关键词，或试试演员名、年份或画质筛选' : '试试放宽类型或画质筛选'}</p>
          {isFavorites ? <button onClick={() => navigate({ page: 'movies' })}>去逛逛电影</button> : wall ? <button onClick={resetFilters}>清除筛选</button> : null}
        </div>}
        {!loading && received > 0 && received < total && <div className="catalog-more"><button disabled={loadingMore} onClick={() => void loadMore()}>{loadingMore ? '正在加载…' : '加载更多'}{!loadingMore && <Icon name="chevronDown" size={16} />}</button><span>已显示 {showingHints ? hints.length : items.length} / {total}</span></div>}
      </div>}
    </main>
  );
}

export function CatalogPage({ kind }: { kind: CatalogKind }) {
  return <CatalogView key={kind} kind={kind} />;
}

