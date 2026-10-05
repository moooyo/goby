import { useEffect, useState, type ReactNode } from 'react';
import { Icon, MediaImage, PlayerLogo } from '../components';
import { useApp } from '../context';
import { api } from '../lib/api';
import { availablePlayers } from '../lib/players';
import { languageLabel } from '../lib/format';
import { viewingStatistics } from '../lib/viewingStatistics';
import { recentPoster, recentViewing } from '../lib/recentViewing';
import { subtitleStyles } from '../lib/subtitleStyles';
import type { BackgroundSource, MediaItem, PlayerPreferences, User } from '../types';
import './settings.css';

const backgroundLabels: Record<BackgroundSource, string> = { theme: '主题视频', generated: '本地生成短片', trailer: '本地预告片' };

function Segments<T extends string | number>({ label, value, options, onChange }: {
  label: string;
  value: T;
  options: Array<{ value: T; label: string }>;
  onChange: (value: T) => void;
}) {
  return <div className="settings-segments" role="group" aria-label={label}>{options.map((option) => <button key={option.value} className={option.value === value ? 'is-active' : ''} aria-pressed={option.value === value} onClick={() => onChange(option.value)}>{option.label}</button>)}</div>;
}

function SettingRow({ label, description, children }: { label: string; description?: string; children: ReactNode }) {
  return <div className="settings-row"><div className="settings-row-label"><div>{label}</div>{description && <p>{description}</p>}</div>{children}</div>;
}

function ToggleRow({ label, description, value, onChange }: { label: string; description: string; value: boolean; onChange: (value: boolean) => void }) {
  return <button className="settings-row settings-toggle-row" role="switch" aria-checked={value} onClick={() => onChange(!value)}><span className="settings-row-label"><span>{label}</span><span className="settings-row-description">{description}</span></span><span className={`settings-switch${value ? ' is-on' : ''}`}><i /></span></button>;
}

function platformName() {
  const agent = navigator.userAgent;
  if (/android/i.test(agent)) return '安卓';
  if (/iPad|iPhone|iPod/.test(agent) || (/Macintosh/.test(agent) && navigator.maxTouchPoints > 1)) return '苹果移动设备';
  if (/Windows/i.test(agent)) return 'Windows';
  if (/Mac/i.test(agent)) return 'macOS';
  if (/Linux/i.test(agent)) return 'Linux';
  return '浏览器';
}

export function SettingsPage() {
  const { session, prefs, setPrefs, navigate, revision, setBackdrop, logout, notify } = useApp();
  const [user, setUser] = useState<User>(session.User);
  const [recent, setRecent] = useState<MediaItem[]>([]);
  const [preview, setPreview] = useState<MediaItem | null>(null);
  const [movieCount, setMovieCount] = useState<number | null>(null);
  const [episodeCount, setEpisodeCount] = useState<number | null>(null);
  const [viewingHours, setViewingHours] = useState<number | null>(null);
  const [connected, setConnected] = useState(false);
  const [network, setNetwork] = useState('');
  const [recentLoading, setRecentLoading] = useState(true);
  const [recentError, setRecentError] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const [priorityAnnouncement, setPriorityAnnouncement] = useState('');
  const players = availablePlayers();
  const selectedPlayer = players.some((player) => player.id === prefs.player) ? prefs.player : 'goby';

  useEffect(() => {
    let active = true;
    const statisticsAbort = new AbortController();
    setBackdrop(null, 'blur');
    setViewingHours(null);
    setRecentLoading(true);
    setRecentError(false);
    void viewingStatistics(session, statisticsAbort.signal).then(statistics => {
      if (active) setViewingHours(statistics.EstimatedContentHours);
    }).catch(() => { /* An unavailable estimate remains visibly unknown. */ });
    Promise.allSettled([
      api.currentUser(),
      api.items({ IncludeItemTypes: 'Movie', IsPlayed: true, Limit: 0 }),
      api.items({ IncludeItemTypes: 'Episode', IsPlayed: true, Limit: 0 }),
      recentViewing(session, statisticsAbort.signal),
      api.endpoint(),
    ]).then(async ([profile, movies, episodes, history, endpoint]) => {
      if (!active) return;
      if (profile.status === 'fulfilled') { setUser(profile.value); setConnected(true); }
      else setConnected(false);
      if (endpoint.status === 'fulfilled' && (endpoint.value.IsInNetwork !== undefined || endpoint.value.IsLocal !== undefined)) setNetwork(endpoint.value.IsInNetwork || endpoint.value.IsLocal ? '本地网络' : '远程连接');
      if (movies.status === 'fulfilled') setMovieCount(movies.value.TotalRecordCount);
      if (episodes.status === 'fulfilled') setEpisodeCount(episodes.value.TotalRecordCount);
      if (history.status === 'fulfilled') {
        const watched = history.value;
        setRecent(watched);
        if (watched[0]) {
          setPreview(watched[0]);
          setBackdrop(watched[0], 'blur');
        } else {
          const latest = await api.latest({ IncludeItemTypes: 'Movie,Series', Limit: 1 }).catch(() => []);
          if (!active) return;
          if (latest[0]) { setPreview(latest[0]); setBackdrop(latest[0], 'blur'); }
        }
      } else setRecentError(true);
      setRecentLoading(false);
    });
    return () => { active = false; statisticsAbort.abort(); };
  }, [revision, setBackdrop, session.AccessToken, session.User.Id, session.serverUrl]);

  let host = window.location.host;
  try { host = new URL(session.serverUrl || window.location.origin).host; } catch { /* The current host remains the connection label. */ }
  const adminUrl = new URL('/admin/', session.serverUrl || window.location.origin).href;
  const normalizedAudio = prefs.audioLanguage.toLowerCase();
  const audioValue = ['chi', 'zho', 'cmn', 'zh', 'zh-cn'].includes(normalizedAudio) ? 'zho' : prefs.audioLanguage;
  const audioOptions = [{ value: '', label: '原声' }, { value: 'zho', label: '国语' }, ...(!['', 'zho'].includes(audioValue) ? [{ value: audioValue, label: languageLabel(audioValue) }] : [])];
  const normalizedSubtitle = prefs.subtitleLanguage.toLowerCase();
  const subtitleValue = prefs.subtitleMode === 'None' ? 'none' : ['zho-tw', 'zh-tw', 'zh-hant'].includes(normalizedSubtitle) ? 'zho-TW' : ['chi', 'zho', 'cmn', 'zh', 'zh-cn', 'zh-hans', ''].includes(normalizedSubtitle) ? 'chi' : normalizedSubtitle === 'en' ? 'eng' : prefs.subtitleLanguage;
  const subtitleOptions = [{ value: 'none', label: '关闭' }, { value: 'chi', label: '简体' }, { value: 'zho-TW', label: '繁体' }, { value: 'eng', label: '英文' }, { value: 'mul', label: '双语' }, ...(!['none', 'chi', 'zho-TW', 'eng', 'mul'].includes(subtitleValue) ? [{ value: subtitleValue, label: languageLabel(subtitleValue) }] : [])];
  const previewStyle = subtitleStyles(prefs);

  function update(patch: Partial<PlayerPreferences>) {
    setPrefs(patch);
  }

  function moveBackgroundSource(index: number, direction: -1 | 1) {
    const nextIndex = index + direction;
    if (nextIndex < 0 || nextIndex >= prefs.backgroundSources.length) return;
    const backgroundSources = [...prefs.backgroundSources];
    const source = backgroundSources[index];
    backgroundSources.splice(index, 1);
    backgroundSources.splice(nextIndex, 0, source);
    update({ backgroundSources });
    setPriorityAnnouncement(`${backgroundLabels[source]}已调整为第 ${nextIndex + 1} 优先`);
  }

  async function signOut() {
    if (signingOut) return;
    setSigningOut(true);
    try { await logout(); }
    catch { notify('退出登录失败，请重试。'); setSigningOut(false); }
  }

  return (
    <main className="settings-page">
      <div className="settings-content">
        <section className="settings-profile">
          <div className="settings-identity">
            <span className="settings-avatar" aria-hidden="true">{Array.from(user.Name || '用')[0]}</span>
            <div className="settings-user-copy">
              <h1>{user.Name}</h1>
              <div className="settings-identity-meta"><span className="settings-role">{user.Policy?.IsAdministrator ? '管理员' : '用户'}</span><span className="settings-server"><i className={connected ? 'is-online' : ''} aria-label={connected ? '已连接' : '连接未确认'} />{[host, network, platformName()].filter(Boolean).join(' · ')}</span></div>
            </div>
          </div>
          <div className="settings-statistics" aria-label="观看统计">
            <div title="已标记看过的电影"><strong>{movieCount ?? '—'}</strong><span>部电影</span></div>
            <div title="已标记看过的剧集单集"><strong>{episodeCount ?? '—'}</strong><span>集剧集</span></div>
            <div title="按已看内容的完整片长及未看完的进度估算；重复观看不重复累计，未知片长不计入" aria-label={viewingHours === null ? '已看内容时长估算暂时不可用' : `已看内容时长估算 ${viewingHours} 小时`}><strong>{viewingHours ?? '—'}</strong><span>小时</span></div>
          </div>
        </section>

        {(recent.length > 0 || recentLoading || recentError) && <section className="settings-section">
          <h2>最近观看</h2>
          {recentLoading ? <div className="settings-recent-grid" aria-label="正在加载最近观看" role="status">{Array.from({ length: 6 }, (_, index) => <div className="settings-recent-placeholder" key={index} />)}</div> : recentError ? <p className="settings-empty">暂时无法加载观看记录</p> : <div className="settings-recent-grid">{recent.map((item) => <button className="settings-recent-card" key={item.Id} onClick={() => navigate({ page: 'detail', id: item.Type === 'Episode' && item.SeriesId ? item.SeriesId : item.Id })}><div className="settings-recent-poster"><MediaImage item={recentPoster(item)} /></div><span>{item.SeriesName || item.Name}</span></button>)}</div>}
        </section>}

        <section className="settings-section">
          <h2>播放</h2>
          <div className="settings-card">
            <SettingRow label="默认画质" description="片源低于所选画质时直接播放原画"><Segments<PlayerPreferences['quality']> label="默认画质" value={prefs.quality} options={[{ value: 'original', label: '原画' }, { value: '1080p', label: '1080p' }, { value: '720p', label: '720p' }, { value: '480p', label: '480p' }]} onChange={(quality) => update({ quality })} /></SettingRow>
            <SettingRow label="默认音轨"><Segments label="默认音轨" value={audioValue} options={audioOptions} onChange={(audioLanguage) => update({ audioLanguage })} /></SettingRow>
            <SettingRow label="默认字幕"><Segments label="默认字幕" value={subtitleValue} options={subtitleOptions} onChange={(value) => update(value === 'none' ? { subtitleMode: 'None' } : { subtitleLanguage: value, subtitleMode: 'Always' })} /></SettingRow>
            <ToggleRow label="自动播放下一集" description="片尾倒计时结束后继续播放" value={prefs.autoNext} onChange={(autoNext) => update({ autoNext })} />
            <ToggleRow label="自动跳过片头" description="影片提供片头标记时直接跳过" value={prefs.autoSkip} onChange={(autoSkip) => update({ autoSkip })} />
            <SettingRow label="默认播放器" description="点击播放时使用；也可以在详情页为单部影片单独指定">
              <div className="settings-players" aria-label="默认播放器">{players.map((player) => <button key={player.id} className={selectedPlayer === player.id ? 'is-active' : ''} aria-pressed={selectedPlayer === player.id} title={player.description} onClick={() => update({ player: player.id })}><span style={{ background: player.color }} className={`settings-player-icon${player.id === 'goby' ? ' is-builtin' : ''}`}><PlayerLogo id={player.id}/></span>{player.name}</button>)}</div>
            </SettingRow>
          </div>
        </section>

        <section className="settings-section">
          <h2>字幕外观</h2>
          <div className="settings-card settings-card--subtitles">
            <div className="settings-subtitle-preview" style={previewStyle}>
              {preview && <MediaImage item={preview} kind="Backdrop" />}
              <div className={`settings-preview-line settings-preview-line--${prefs.subtitleStyle}`}><span>我们得在天亮之前离开这里。</span></div>
            </div>
            <SettingRow label="字号"><Segments<PlayerPreferences['subtitleSize']> label="字幕字号" value={prefs.subtitleSize} options={[{ value: 0, label: '小' }, { value: 1, label: '标准' }, { value: 2, label: '大' }, { value: 3, label: '特大' }]} onChange={(subtitleSize) => update({ subtitleSize })} /></SettingRow>
            <SettingRow label="样式"><Segments<PlayerPreferences['subtitleStyle']> label="字幕样式" value={prefs.subtitleStyle} options={[{ value: 'shadow', label: '阴影' }, { value: 'outline', label: '描边' }, { value: 'background', label: '底框' }]} onChange={(subtitleStyle) => update({ subtitleStyle })} /></SettingRow>
          </div>
        </section>

        <section className="settings-section">
          <h2>界面</h2>
          <div className="settings-card">
            <ToggleRow label="首页精选自动轮播" description="每 8 秒切换一部" value={prefs.heroRotate} onChange={(heroRotate) => update({ heroRotate })} />
            <ToggleRow label="动态背景" description="首页和详情页默认静音播放背景素材；关闭后只显示剧照" value={prefs.backgroundMotion} onChange={(backgroundMotion) => update({ backgroundMotion })} />
            <SettingRow label="背景素材优先级" description="从上到下尝试播放；素材不可用时使用下一来源，最后显示剧照">
              <ol className="settings-background-order" aria-label="背景素材优先级">
                {prefs.backgroundSources.map((source, index) => <li key={source} data-source={source}>
                  <span className="settings-background-position" aria-hidden="true">{index + 1}</span>
                  <span className="settings-background-name">{backgroundLabels[source]}</span>
                  <span className="settings-background-actions">
                    <button aria-label={`提高${backgroundLabels[source]}优先级`} disabled={index === 0} onClick={() => moveBackgroundSource(index, -1)}><Icon name="chevronUp" size={17} /></button>
                    <button aria-label={`降低${backgroundLabels[source]}优先级`} disabled={index === prefs.backgroundSources.length - 1} onClick={() => moveBackgroundSource(index, 1)}><Icon name="chevronDown" size={17} /></button>
                  </span>
                </li>)}
                <li className="settings-background-fallback"><span className="settings-background-position" aria-hidden="true">4</span><span className="settings-background-name">剧照</span><span>始终保留</span></li>
              </ol>
            </SettingRow>
            <span className="settings-priority-status" role="status" aria-live="polite">{priorityAnnouncement}</span>
          </div>
        </section>

        <section className="settings-section">
          <h2>账户</h2>
          <div className="settings-account-actions">
            <button onClick={() => void signOut()} disabled={signingOut}><Icon name="users" size={18} />切换用户</button>
            {user.Policy?.IsAdministrator && <a href={adminUrl} target="_blank" rel="noopener noreferrer"><Icon name="monitor" size={18} />管理后台</a>}
            <button className="settings-logout" onClick={() => void signOut()} disabled={signingOut}><Icon name="logout" size={18} />{signingOut ? '正在退出…' : '退出登录'}</button>
          </div>
        </section>
      </div>
    </main>
  );
}

