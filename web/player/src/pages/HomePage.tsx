import { useEffect, useState, type CSSProperties } from 'react';
import type { MediaItem } from '../types';
import { api } from '../lib/api';
import { savedPage, savePage } from '../lib/navigation';
import { formatDuration, mediaBadges, progressPercent } from '../lib/format';
import { useApp } from '../context';
import { Icon, MediaImage, PageState, StageDeck, StageRail } from '../components';
import './home.css';

const labels = ['精选', '继续观看', '最新电影', '最新剧集'];

function videoBadges(item: MediaItem) {
  return mediaBadges(item).filter(badge => /^(?:\d+p|[48]K|HDR|HLG|Dolby Vision)/.test(badge));
}

function HomeFeaturedMeta({ item }: { item: MediaItem }) {
  const source = item.MediaSources?.[0];
  const streams = source?.MediaStreams ?? item.MediaStreams ?? [];
  const audio = streams.find(stream => stream.Type === 'Audio' && stream.Index === source?.DefaultAudioStreamIndex)
    ?? streams.find(stream => stream.Type === 'Audio' && stream.IsDefault)
    ?? streams.find(stream => stream.Type === 'Audio');
  const atmos = audio && /atmos|全景声/i.test([audio.Profile, audio.DisplayTitle, audio.Title].filter(Boolean).join(' '));
  const audioBadge = !audio ? '' : atmos ? '杜比全景声' : audio.Channels === 2 ? '立体声' : audio.Channels === 1 ? '单声道'
    : ['ac3', 'eac3'].includes(audio.Codec?.toLowerCase() ?? '') && audio.Channels === 6 ? '杜比 5.1' : '';
  const bits = [item.ProductionYear, item.Genres?.join(' / '), item.Type === 'Series' ? item.ChildCount ? `${item.ChildCount} 季` : '' : formatDuration(item.RunTimeTicks || source?.RunTimeTicks)].filter(Boolean);
  const badges = [...videoBadges(item), audioBadge].filter(Boolean);
  return <div className="media-meta home-featured-meta">
    {bits.map((bit, index) => <span className="home-meta-bit" key={`${index}-${bit}`}>{index > 0 && <span className="home-meta-separator" aria-hidden="true">·</span>}<span>{bit}</span></span>)}
    {item.CommunityRating != null && <span className="rating"><Icon name="star" size={17} fill="currentColor"/>{item.CommunityRating.toFixed(1)}</span>}
    {badges.length > 0 && <span className="home-featured-badges">{badges.map(badge => <span key={badge} className="spec-badge">{badge}</span>)}</span>}
  </div>;
}

function HomeStripInfo({ item, label, resume }: { item: MediaItem; label: string; resume: boolean }) {
  const [displayedItem, setDisplayedItem] = useState(item);
  const exiting = displayedItem.Id !== item.Id;
  const displayed = exiting ? displayedItem : item;

  useEffect(() => {
    if (displayedItem.Id === item.Id) {
      if (displayedItem !== item) setDisplayedItem(item);
      return;
    }
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setDisplayedItem(item);
      return;
    }
    // Only presentation is delayed; playback and the rail keep the current item.
    const timer = window.setTimeout(() => setDisplayedItem(item), 150);
    return () => window.clearTimeout(timer);
  }, [item, displayedItem.Id]);

  const episode = displayed.Type === 'Episode';
  const episodeLabel = [displayed.ParentIndexNumber != null ? `第 ${displayed.ParentIndexNumber} 季` : '', displayed.IndexNumber != null ? `第 ${displayed.IndexNumber} 集` : ''].filter(Boolean).join(' ');
  const progress = progressPercent(displayed);
  const duration = displayed.RunTimeTicks || displayed.MediaSources?.[0]?.RunTimeTicks;
  const remaining = duration ? `剩余 ${progress >= 100 ? '0分钟' : formatDuration(duration * (1 - progress / 100))}` : '';
  const status = progress > 0 ? [`已看 ${Math.round(progress)}%`, remaining].filter(Boolean).join(' · ') : episode ? '下一集' : formatDuration(duration);
  const metadata = resume
    ? [episode ? displayed.SeriesName ? displayed.Name : '' : [displayed.ProductionYear, displayed.Genres?.join(' / ')].filter(Boolean).join(' · '), status].filter(Boolean).join(' · ')
    : [displayed.CommunityRating != null ? `★ ${displayed.CommunityRating.toFixed(1)}` : '', displayed.Type === 'Series' ? displayed.ChildCount ? `${displayed.ChildCount} 季` : '' : formatDuration(duration), videoBadges(displayed).join(' ')].filter(Boolean).join(' · ');
  const title = episode ? displayed.SeriesName || displayed.Name : displayed.Name;
  const eyebrow = [label, resume ? episode ? episodeLabel || '剧集' : '电影' : [displayed.ProductionYear, displayed.Genres?.join(' / ')].filter(Boolean).join(' · ')].filter(Boolean).join(' · ');

  return <div className="strip-info home-strip-info" data-stage-info>
    <div key={displayed.Id} className={`home-strip-info-content${exiting ? ' home-strip-info-out' : ''}`}>
      <span className="strip-eyebrow">{eyebrow}</span>
      <h1>{title}</h1>
      <div className="media-meta">{metadata}</div>
      {displayed.Overview && <p>{displayed.Overview}</p>}
    </div>
  </div>;
}

export function HomePage() {
  const { prefs, navigate, play, notify, revision, changed, setBackdrop } = useApp();
  const [page, setPage] = useState(() => savedPage('home', 4));
  const [hero, setHero] = useState(0);
  const [focus, setFocus] = useState([0, 0, 0, 0]);
  const [catalog, setCatalog] = useState<{ movies: MediaItem[]; series: MediaItem[]; resume: MediaItem[] } | null>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const [heroPaused, setHeroPaused] = useState(false);
  const [heroCycle, setHeroCycle] = useState(0);
  useEffect(() => savePage('home', page), [page]);
  useEffect(() => {
    const controller = new AbortController();
    setError('');
    Promise.all([api.latest({ IncludeItemTypes: 'Movie', Limit: 12 }), api.latest({ IncludeItemTypes: 'Series', Limit: 18 }), api.resume(12)])
      .then(([movies, series, resume]) => { if (!controller.signal.aborted) setCatalog({ movies, series, resume: resume.Items }); })
      .catch(() => { if (!controller.signal.aborted) setError('无法加载片库，请检查服务器连接后重试。'); });
    return () => controller.abort();
  }, [revision, retry]);
  const featured = catalog ? [...catalog.series, ...catalog.movies].sort((a, b) => (b.DateCreated ?? '').localeCompare(a.DateCreated ?? '')).slice(0, 6) : [];
  const groups = [featured, catalog?.resume ?? [], catalog?.movies ?? [], catalog?.series ?? []];
  const current = groups[page][page === 0 ? hero % Math.max(featured.length, 1) : Math.min(focus[page], groups[page].length - 1)];
  const featuredAccent = featured[hero % Math.max(featured.length, 1)] ?? null;
  useEffect(() => { setBackdrop(current ?? featured[0] ?? null, 'stage', featuredAccent); }, [current, featured[0], featuredAccent, setBackdrop]);
  useEffect(() => {
    if (!prefs.heroRotate || page !== 0 || featured.length < 2 || heroPaused || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    setHeroCycle(cycle => cycle + 1);
    const interval = setInterval(() => { if (!document.hidden) setHero(index => (index + 1) % featured.length); }, 8000);
    return () => clearInterval(interval);
  }, [prefs.heroRotate, page, featured.length, hero, heroPaused]);
  useEffect(() => {
    for (const item of groups.map((group, index) => group[focus[index] ?? 0]).filter(Boolean)) { const src = api.imageUrl(item, 'Backdrop', 1920); if (src) { const img = new Image(); img.src = src; } }
  }, [catalog]);
  if (!catalog) return <PageState loading={!error} error={error} onRetry={() => setRetry(n => n + 1)}/>;
  if (!featured.length) return <PageState><Icon name="film" size={42}/><h1>片库还空着</h1><p>媒体入库后，电影与剧集会出现在这里。</p><button className="glass-button" onClick={() => setRetry(n => n + 1)}>刷新片库</button></PageState>;
  const featuredItem = featured[hero % featured.length];
  const favorite = async () => { try { await api.setFavorite(featuredItem.Id, !featuredItem.UserData?.IsFavorite); changed(); notify(featuredItem.UserData?.IsFavorite ? '已取消收藏' : '已加入收藏'); } catch { notify('收藏更新失败，请重试。'); } };
  return <StageDeck labels={labels} page={page} onPage={setPage}>{[
    <div className="stage-page home-featured" data-stage="true" key="featured" style={{ '--feature-count': featured.length } as CSSProperties}>
      <div className="feature-info stage-scroll" data-stage-info onFocusCapture={() => setHeroPaused(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) setHeroPaused(false); }}>
        <div className="feature-eyebrow"><span className="accent-pill">最新入库</span><span>{featuredItem.Type === 'Series' ? '剧集' : '电影'}</span></div>
        <h1>{featuredItem.Name}</h1><HomeFeaturedMeta item={featuredItem}/>
        {featuredItem.Overview && <p className="feature-overview">{featuredItem.Overview}</p>}
        <div className="feature-actions"><button className="glass-button" onClick={() => navigate({ page: 'detail', id: featuredItem.Id })}><Icon name="info"/>详情</button><button className={`glass-button circle ${featuredItem.UserData?.IsFavorite ? 'is-favorite' : ''}`} aria-label={featuredItem.UserData?.IsFavorite ? '取消收藏' : '收藏'} aria-pressed={!!featuredItem.UserData?.IsFavorite} onClick={favorite}><Icon name="heart" fill={featuredItem.UserData?.IsFavorite ? 'currentColor' : 'none'}/></button></div>
        <div className="hero-dots" aria-label="精选影片">{featured.map((item, index) => <button key={item.Id} aria-label={`精选：${item.Name}`} aria-pressed={index === hero % featured.length} className={index === hero % featured.length ? 'active' : ''} onClick={() => setHero(index)}/>)}</div>
      </div>
      <div className="hero-thumbnails" aria-label="精选影片" onFocusCapture={() => setHeroPaused(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) setHeroPaused(false); }}>{featured.map((item, index) => <button key={item.Id} title={item.Name} aria-label={`精选：${item.Name}`} aria-pressed={index === hero % featured.length} className={index === hero % featured.length ? 'active' : ''} onClick={() => setHero(index)}><MediaImage item={item} kind="Backdrop"/>{index === hero % featured.length && <span key={`${item.Id}-${hero}-${heroCycle}`} className={`hero-progress${prefs.heroRotate ? '' : ' hero-progress-static'}`} style={{ animationPlayState: heroPaused ? 'paused' : undefined }}/>}</button>)}</div>
    </div>,
    ...groups.slice(1).map((items, groupIndex) => {
      const index = groupIndex + 1;
      const selectedIndex = Math.min(focus[index], items.length - 1);
      const selected = items[selectedIndex];
      const activate = (item: MediaItem) => index === 1 ? play(item) : navigate({ page: 'detail', id: item.SeriesId || item.Id });
      return <div className="stage-page home-strip-page" data-stage="true" key={labels[index]}>
        {selected ? <><div className="stage-click-target" onClick={() => activate(selected)} aria-hidden="true"/><HomeStripInfo item={selected} label={labels[index]} resume={index === 1}/><StageRail title={labels[index]} items={items} active={selectedIndex} resume={index === 1} onFocus={itemIndex => setFocus(values => values.map((value, i) => i === index ? itemIndex : value))} onActivate={activate} action={index > 1 && <button className="text-button" onClick={() => navigate({ page: index === 2 ? 'movies' : 'series' })}>查看全部<Icon name="chevronRight" size={17}/></button>}/></> : index === 1 ? <div className="strip-info home-strip-info home-resume-empty" data-stage-info><span className="strip-eyebrow">继续观看</span><h1>看到一半的影片会出现在这里</h1></div> : <div className="stage-empty"><Icon name="film" size={36}/><h1>片库还空着</h1><p>媒体入库后会自动显示。</p><button className="glass-button" onClick={() => navigate({ page: 'movies' })}>去逛逛电影</button></div>}
      </div>;
    }),
  ]}</StageDeck>;
}
