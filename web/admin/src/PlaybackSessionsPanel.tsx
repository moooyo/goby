import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, MetadataItemSummary, MetadataItemsResponse } from './api';
import { errorText } from './components';
import { confirmAction, useTranslation } from './i18n';
import { Dialog, DialogActions, DialogContent, DialogTitle } from './ui';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import { playbackApi, playbackArtworkURL, playbackTicks } from './playbackApi';
import type { PlaybackCommand, PlaybackCommandInput, PlaybackCommandReceipt, PlaybackSession, PlaybackSessionsResponse } from './playbackApi';
import './playback.css';

const noGuard: UserNavigationGuardChange = () => undefined;
const pollInterval = 5000;
const requestTimeout = 15000;
const ticksPerSecond = 10000000n;

// A page can host both playback forms and existing task or sign-in dialogs.
// Each owner keeps its guard until its own operation or draft has settled.
export function usePlaybackPageGuards(onChange: UserNavigationGuardChange) {
  const guards = useRef(new Map<string, UserNavigationGuard>());
  const set = useCallback((key: string, guard: UserNavigationGuard | undefined) => {
    if (guard) guards.current.set(key, guard); else guards.current.delete(key);
    onChange(guards.current.size ? () => Array.from(guards.current.values()).every((check) => check()) : undefined);
  }, [onChange]);
  const handlers = useMemo(() => ({
    page: (guard: UserNavigationGuard | undefined) => set('page', guard),
    playback: (guard: UserNavigationGuard | undefined) => set('playback', guard),
  }), [set]);
  useEffect(() => () => { guards.current.clear(); onChange(undefined); }, [onChange]);
  return handlers;
}

function usePlaybackSessions() {
  const [snapshot, setSnapshot] = useState<{ data?: PlaybackSessionsResponse; error?: unknown; loading: boolean }>({ loading: true });
  const [revision, setRevision] = useState(0);
  const [paused, setPaused] = useState(document.hidden);
  const [review, setReview] = useState(0);
  const reviewNext = useRef(false);
  const reload = useCallback((reviewResult = false) => {
    reviewNext.current = reviewResult;
    setRevision((value) => value + 1);
  }, []);
  useEffect(() => {
    let active = true;
    let failed = false;
    let controller: AbortController | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let reviewing = reviewNext.current;
    reviewNext.current = false;
    function halt() { clearTimeout(timer); controller?.abort(); controller = undefined; }
    async function refresh() {
      if (!active || failed || controller || document.hidden) return;
      const request = new AbortController(); controller = request;
      let timeout: ReturnType<typeof setTimeout> | undefined;
      let timedOut = false;
      setSnapshot((current) => ({ ...current, loading: true }));
      try {
        const deadline = new Promise<never>((_resolve, reject) => {
          timeout = setTimeout(() => { timedOut = true; request.abort(); reject(new ApiError('Playback did not respond within 15 seconds. Refresh to try again.', { code: 'playback_timeout' })); }, requestTimeout);
        });
        const result = await Promise.race([playbackApi.sessions({ signal: request.signal }), deadline]);
        if (!active || request.signal.aborted) return;
        setSnapshot({ data: result, loading: false });
        if (reviewing) { reviewing = false; setReview((value) => value + 1); }
      } catch (error) {
        if (active && (timedOut || (!request.signal.aborted && !isAbortError(error)))) {
          failed = true;
          setSnapshot((current) => ({ ...current, error: timedOut ? new ApiError('Playback did not respond within 15 seconds. Refresh to try again.', { code: 'playback_timeout' }) : error, loading: false }));
        }
      } finally {
        clearTimeout(timeout);
        if (controller === request) controller = undefined;
        if (active && !failed && !document.hidden && !request.signal.aborted) timer = setTimeout(() => void refresh(), pollInterval);
      }
    }
    function visibilityChanged() {
      setPaused(document.hidden);
      if (document.hidden) { halt(); setSnapshot((current) => ({ ...current, loading: false })); }
      else if (!failed) void refresh();
    }
    setPaused(document.hidden);
    document.addEventListener('visibilitychange', visibilityChanged);
    void refresh();
    return () => { active = false; halt(); document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [revision]);
  return { ...snapshot, paused, reload, review };
}

function Icon({ name, size = 15 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    refresh: <><path d="M20 7v5h-5M4 17v-5h5" /><path d="M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3" /></>,
    play: <path d="m8 4 13 8-13 8Z" />,
    pause: <><path d="M8 5v14M16 5v14" /></>,
    stop: <rect x="6" y="6" width="12" height="12" rx="2" />,
    message: <><path d="M20 11a8 8 0 0 1-8 8H5l-4 3 2-7a8 8 0 1 1 17-4Z" /><path d="M7 9h8M7 13h5" /></>,
    device: <><rect x="2" y="3" width="20" height="14" rx="2" /><path d="M8 21h8M12 17v4" /></>,
    next: <path d="m9 5 7 7-7 7" />,
    close: <path d="m6 6 12 12M6 18 18 6" />,
    info: <><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7v.1" /></>,
    seek: <><path d="M7 4v16M11 7l7 5-7 5Z" /></>,
  };
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name] ?? paths.info}</svg>;
}

export function playbackTime(value: unknown): string {
  const ticks = playbackTicks(value);
  if (ticks === undefined) return '—';
  const seconds = ticks / ticksPerSecond, hours = seconds / 3600n, minutes = seconds / 60n % 60n;
  return hours > 0n ? `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds % 60n).padStart(2, '0')}` : `${seconds / 60n}:${String(seconds % 60n).padStart(2, '0')}`;
}

export function parsePlaybackTime(value: string): string | undefined {
  const parts = value.trim().split(':');
  if (parts.length < 2 || parts.length > 3 || !parts.every((part) => /^\d{1,8}$/.test(part))
    || parts.slice(1).some((part) => Number(part) > 59)) return undefined;
  const seconds = parts.reduce((total, part) => total * 60n + BigInt(part), 0n);
  return seconds * ticksPerSecond <= 9223372036854775807n ? (seconds * ticksPerSecond).toString() : undefined;
}

function mediaSpecification(session: PlaybackSession): string {
  const item = session.NowPlayingItem;
  const streams = item?.MediaStreams ?? [];
  const video = streams.find((stream) => stream.Type === 'Video');
  const audio = streams.find((stream) => stream.Type === 'Audio');
  return [video?.Codec?.toUpperCase(), video?.Width && video.Height ? `${video.Width} × ${video.Height}` : '', audio?.Codec?.toUpperCase(), item?.Container?.toUpperCase()].filter(Boolean).join(' · ');
}

function sessionTitle(session: PlaybackSession) { return session.DeviceName || session.Client || session.DeviceId; }
function commandAllowed(session: PlaybackSession | undefined, command: PlaybackCommand) {
  if (!session?.TransportConnected) return false;
  if (command === 'DisplayMessage') return session.Controls.Message;
  if (command === 'PlayNext' || command === 'PlayLast') return session.Controls.Play && session.SupportedCommands.includes(command);
  if (command.startsWith('Play')) return session.Controls.Play;
  return session.Controls[command as 'Stop' | 'Pause' | 'Unpause' | 'Seek'];
}

function PlaybackArtwork({ session }: { session: PlaybackSession }) {
  const url = playbackArtworkURL(session.NowPlayingItem?.ArtworkUrl);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [url]);
  const current = playbackTicks(session.PlayState.PositionTicks), duration = playbackTicks(session.NowPlayingItem?.RunTimeTicks);
  const progress = current !== undefined && duration !== undefined && duration > 0n ? Number((current > duration ? duration : current) * 10000n / duration) / 100 : undefined;
  return <div className="playback-artwork">
    {url && !failed ? <img src={url} alt="" loading="lazy" onError={() => setFailed(true)} /> : <Icon name="play" size={24} />}
    {progress !== undefined && <div className="playback-artwork-progress" aria-hidden="true"><i style={{ width: `${progress}%` }} /></div>}
  </div>;
}

type PlaybackAction = { kind: 'stop' | 'seek' | 'play'; session: PlaybackSession } | { kind: 'message'; session?: PlaybackSession };
interface PendingCommand { receipt: PlaybackCommandReceipt; previousReport?: string }
interface Notice { key: string; values?: Record<string, string | number>; warning?: boolean }
interface Props { compact?: boolean; onAllSessions?: () => void; onNavigationGuardChange?: UserNavigationGuardChange }

export function PlaybackSessionsPanel({ compact = false, onAllSessions, onNavigationGuardChange = noGuard }: Props) {
  const { t, locale, formatCount } = useTranslation();
  const resource = usePlaybackSessions();
  const guards = usePlaybackPageGuards(onNavigationGuardChange);
  const [action, setAction] = useState<PlaybackAction>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [uncertain, setUncertain] = useState(false);
  const [notice, setNotice] = useState<Notice>();
  const [pending, setPending] = useState<PendingCommand>();
  const inFlight = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(true);
  const review = useRef(resource.review);
  const playing = resource.data?.Items.filter((session) => session.NowPlayingItem) ?? [];
  const idle = resource.data?.Items.filter((session) => !session.NowPlayingItem) ?? [];
  const disabled = busy || uncertain || resource.error != null || !resource.data || resource.paused;
  useUserDraftNavigation(false, busy, guards.page);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; inFlight.current?.abort(); }; }, []);
  useEffect(() => {
    if (resource.error || !resource.data || review.current === resource.review) return;
    review.current = resource.review; setUncertain(false); setError(undefined);
  }, [resource.data, resource.error, resource.review]);
  useEffect(() => {
    if (!pending || !resource.data) return;
    const current = resource.data.Items.find((session) => session.Id === pending.receipt.SessionId);
    if ((pending.receipt.Command === 'Stop' && !current?.NowPlayingItem)
      || (current?.PlaybackUpdatedAt && current.PlaybackUpdatedAt !== pending.previousReport
        && Date.parse(current.PlaybackUpdatedAt) >= Date.parse(pending.receipt.AcceptedAt))) {
      setPending(undefined);
      setNotice({ key: 'Playback status refreshed. Review the latest player report.' });
    }
  }, [resource.data, pending]);

  async function send(session: PlaybackSession | undefined, input: PlaybackCommandInput) {
    if (inFlight.current || disabled) return false;
    const latest = session ? resource.data?.Items.find((entry) => entry.Id === session.Id) : undefined;
    if (session && !commandAllowed(latest, input.Command)) { setError(new ApiError('This player is no longer available for that command. Refresh playback.', { code: 'playback_command_unsupported' })); return false; }
    const controller = new AbortController(); inFlight.current = controller; setBusy(true); setError(undefined); setNotice(undefined);
    let timeout: ReturnType<typeof setTimeout> | undefined;
    let timedOut = false;
    function bounded<T>(request: Promise<T>): Promise<T> {
      const deadline = new Promise<never>((_resolve, reject) => {
        timeout = setTimeout(() => { timedOut = true; controller.abort(); reject(new ApiError('The command response timed out. Its result is unknown.', { code: 'playback_command_timeout' })); }, requestTimeout);
      });
      return Promise.race([request, deadline]);
    }
    try {
      if (session) {
        const receipt = await bounded(playbackApi.command(session.Id, { ...input,
          ...(['Stop', 'Pause', 'Unpause', 'Seek'].includes(input.Command) && session.PlaySessionId ? { PlaySessionId: session.PlaySessionId } : {}),
        }, { signal: controller.signal }));
        if (!mounted.current || controller.signal.aborted) return false;
        if (receipt.Command !== 'DisplayMessage') setPending({ receipt, previousReport: latest?.PlaybackUpdatedAt });
        setNotice({ key: receipt.DeliveryCount > 0 ? 'Command sent. The player has not confirmed execution.' : 'Command accepted, but no connected player received it.', warning: receipt.DeliveryCount === 0 });
      } else {
        const receipt = await bounded(playbackApi.message({ Text: input.Text!, Header: input.Header, TimeoutMs: input.TimeoutMs }, { signal: controller.signal }));
        if (!mounted.current || controller.signal.aborted) return false;
        const queued = receipt.Results.filter((entry) => entry.Status === 'queued' && entry.DeliveryCount > 0).length;
        setNotice({ key: 'Messages queued: {queued}. Players skipped: {skipped}. Sending does not confirm display.', values: { queued, skipped: receipt.Results.length - queued }, warning: queued === 0 });
      }
      setAction(undefined); resource.reload(); return true;
    } catch (cause) {
      if (mounted.current && (timedOut || !controller.signal.aborted)) {
        setError(timedOut ? new ApiError('The command response timed out. Its result is unknown.', { code: 'playback_command_timeout' }) : cause);
        const unknown = timedOut || !(cause instanceof ApiError) || ['network_error', 'invalid_response', 'session_changed'].includes(cause.code) || cause.status >= 500;
        setUncertain(unknown);
        if (unknown) setAction(undefined);
        else resource.reload();
      }
      return false;
    } finally {
      clearTimeout(timeout);
      if (inFlight.current === controller) inFlight.current = undefined;
      if (mounted.current) setBusy(false);
    }
  }

  function controls(session: PlaybackSession) {
    const pauseCommand = session.PlayState.IsPaused ? 'Unpause' : 'Pause';
    const queued = pending?.receipt.SessionId === session.Id ? pending.receipt.Command : undefined;
    const unavailable = !session.TransportConnected ? 'The player is disconnected. Reconnect it to use playback controls.' : 'This player does not support this command.';
    return <div className="playback-row-actions">
      {!compact && <button type="button" className="playback-button playback-icon-button" aria-label={t(session.PlayState.IsPaused ? 'Resume playback' : 'Pause playback')} title={t(commandAllowed(session, pauseCommand) ? session.PlayState.IsPaused ? 'Resume playback' : 'Pause playback' : unavailable)} disabled={disabled || queued === pauseCommand || !commandAllowed(session, pauseCommand)} onClick={() => void send(session, { Command: pauseCommand })}><Icon name={session.PlayState.IsPaused ? 'play' : 'pause'} /></button>}
      <button type="button" className="playback-button" title={!commandAllowed(session, 'DisplayMessage') ? t(unavailable) : undefined} disabled={disabled || !commandAllowed(session, 'DisplayMessage')} onClick={() => { setError(undefined); setAction({ kind: 'message', session }); }}><Icon name="message" />{t('Message')}</button>
      <button type="button" className="playback-button" title={!commandAllowed(session, 'Stop') ? t(unavailable) : undefined} disabled={disabled || queued === 'Stop' || !commandAllowed(session, 'Stop')} onClick={() => { setError(undefined); setAction({ kind: 'stop', session }); }}><Icon name="stop" />{t('Stop')}</button>
      {!compact && <details className="playback-more"><summary>{t('More playback controls')}</summary><div>
        <button type="button" className="playback-button" disabled={disabled || !session.Controls.Seek || !session.PlayState.CanSeek || !session.NowPlayingItem?.RunTimeTicks || !session.TransportConnected} onClick={() => { setError(undefined); setAction({ kind: 'seek', session }); }}><Icon name="seek" />{t('Seek to time')}</button>
        <button type="button" className="playback-button" disabled={disabled || !commandAllowed(session, 'PlayNow')} onClick={() => { setError(undefined); setAction({ kind: 'play', session }); }}><Icon name="play" />{t('Play media on this device')}</button>
      </div></details>}
    </div>;
  }

  function row(session: PlaybackSession) {
    const item = session.NowPlayingItem!;
    const position = playbackTicks(session.PlayState.PositionTicks), duration = playbackTicks(item.RunTimeTicks);
    const percentage = position !== undefined && duration !== undefined && duration > 0n ? Number((position > duration ? duration : position) * 10000n / duration) / 100 : undefined;
    const method = ({ DirectPlay: 'Direct play', DirectStream: 'Direct stream', Transcode: 'Transcoding' } as Record<string, string>)[session.PlayState.PlayMethod ?? ''] ?? 'Playback method not reported';
    const specification = mediaSpecification(session);
    const episode = item.Type === 'Episode' ? [item.ParentIndexNumber == null ? '' : `S${String(item.ParentIndexNumber).padStart(2, '0')}`, item.IndexNumber == null ? '' : `E${String(item.IndexNumber).padStart(2, '0')}`].join('') : '';
    return <article className="playback-row" key={session.Id} aria-label={item.Name} data-playback-session={session.Id}>
      <PlaybackArtwork session={session} />
      <div className="playback-identity"><div className="playback-title"><strong translate="no">{item.Name || t('Untitled item')}</strong>{episode && <span className="playback-mono" translate="no">{episode}</span>}{session.PlayState.IsPaused && <span className="playback-paused">{t('Paused')}</span>}</div>
        {item.SeriesName && <span className="playback-series" translate="no">{item.SeriesName}</span>}
        <div className="playback-client"><span className="playback-avatar" aria-hidden="true" translate="no">{Array.from(session.UserName || session.Client || '?').slice(0, 1).join('').toUpperCase()}</span><strong translate="no">{session.UserName || t('Application client')}</strong><span translate="no">{sessionTitle(session)}{session.Client && session.Client !== sessionTitle(session) ? ` · ${session.Client}` : ''}</span></div>
        <div className="playback-connection"><i className={session.TransportConnected ? 'is-connected' : ''} /><span>{t(session.TransportConnected ? 'Connected' : 'Disconnected')}</span>{session.RemoteEndPoint && <span className="playback-mono" translate="no">{session.RemoteEndPoint}</span>}</div>
      </div>
      <div className="playback-specification"><span className={`playback-method${session.PlayState.PlayMethod === 'Transcode' ? ' is-transcoding' : ''}`}>{t(method)}</span>{specification && <span className="playback-mono" translate="no">{specification}</span>}{session.PlaybackUpdatedAt && <span className="playback-last-report">{t('Last report')} <time dateTime={session.PlaybackUpdatedAt}>{new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(session.PlaybackUpdatedAt))}</time></span>}</div>
      <div className="playback-control-column"><div className="playback-time playback-mono" role={percentage === undefined ? undefined : 'progressbar'} aria-label={t('Playback progress')} aria-valuenow={percentage} aria-valuemin={percentage === undefined ? undefined : 0} aria-valuemax={percentage === undefined ? undefined : 100} aria-valuetext={`${playbackTime(session.PlayState.PositionTicks)} / ${playbackTime(item.RunTimeTicks)}`}><span>{playbackTime(session.PlayState.PositionTicks)}</span><span>/ {playbackTime(item.RunTimeTicks)}</span></div>{controls(session)}{pending?.receipt.SessionId === session.Id && <small className="playback-waiting">{t('Waiting for player report')}</small>}</div>
    </article>;
  }

  return <section className={`playback-panel${compact ? ' playback-panel-compact' : ''}`} aria-label={t('Now playing')} aria-busy={!resource.data && resource.loading}>
    <div className="playback-panel-heading"><h2>{t('Now playing')}</h2>{resource.data && <span className="playback-mono">{formatCount(playing.length, 'playing session', 'playing sessions')}</span>}<div className="playback-heading-actions">
      {!compact && <button type="button" className="playback-button" disabled={disabled || !playing.some((session) => commandAllowed(session, 'DisplayMessage'))} onClick={() => { setError(undefined); setAction({ kind: 'message' }); }}><Icon name="message" />{t('Message all players')}</button>}
      {compact && onAllSessions && <button type="button" className="playback-text-button" onClick={onAllSessions}>{t('All sessions')}<Icon name="next" size={13} /></button>}
      <button type="button" className="playback-icon-button playback-text-button" aria-label={t('Refresh playback')} title={t('Refresh playback')} disabled={busy || resource.loading} onClick={() => resource.reload(uncertain)}><Icon name="refresh" size={16} /></button>
    </div></div>
    {resource.error != null && <div className="playback-notice is-error" role="alert"><Icon name="info" /><div>{t(errorText(resource.error))}{resource.data && <small>{t('Showing the last successful update. Playback controls are paused.')}</small>}</div><button type="button" className="playback-text-button" disabled={busy || resource.loading} onClick={() => resource.reload(uncertain)}>{t('Retry')}</button></div>}
    {error != null && !action && <div className="playback-notice is-error" role="alert"><Icon name="info" /><div>{t(errorText(error))}</div></div>}
    {uncertain && <div className="playback-notice is-warning" role="status"><Icon name="info" /><div>{t('The command result is unknown. Review current playback before sending another command.')}</div><button type="button" className="playback-text-button" disabled={busy || resource.loading} onClick={() => resource.reload(true)}>{t('Review playback state')}</button></div>}
    {notice && <div className={`playback-notice${notice.warning ? ' is-warning' : ''}`} role="status"><Icon name="info" /><div>{t(notice.key, notice.values)}</div><button type="button" className="playback-icon-button playback-text-button" aria-label={t('Dismiss playback notice')} onClick={() => setNotice(undefined)}><Icon name="close" /></button></div>}
    {!resource.data && resource.loading && <div className="playback-loading" role="status" aria-label={t('Loading playback sessions')}>{[0, 1].map((index) => <span key={index} />)}</div>}
    {resource.data && playing.length === 0 && <div className="playback-empty"><span className="playback-empty-icon"><Icon name="play" size={24} /></span><div><strong>{t('Nobody is playing right now')}</strong><p>{t('Playing titles and progress appear when a player reports playback.')}</p></div></div>}
    {(compact ? playing.slice(0, 3) : playing).map(row)}
    {compact && playing.length > 3 && onAllSessions && <div className="playback-panel-footer"><button type="button" className="playback-text-button" onClick={onAllSessions}>{t('View all playing sessions')}<Icon name="next" size={13} /></button></div>}
    {!compact && resource.data && idle.length > 0 && <details className="playback-idle"><summary>{t('Available players')}<span className="playback-mono">{formatCount(idle.length, 'player', 'players')}</span></summary><ul>{idle.map((session) => <li key={session.Id}><Icon name="device" size={19} /><div><strong translate="no">{sessionTitle(session)}</strong><small translate="no">{[session.UserName, session.Client].filter(Boolean).join(' · ')}</small></div><span className={`playback-device-status${session.TransportConnected ? ' is-connected' : ''}`}>{t(session.TransportConnected ? 'Connected' : 'Disconnected')}</span><button type="button" className="playback-button" disabled={disabled || !commandAllowed(session, 'PlayNow')} onClick={() => { setError(undefined); setAction({ kind: 'play', session }); }}><Icon name="play" />{t('Play media')}</button></li>)}</ul></details>}
    {resource.data && <div className="playback-panel-footer"><span>{t(resource.paused ? 'Updates paused while this tab is hidden.' : resource.error ? 'Updates paused after a request error.' : 'Updates every 5 seconds')}</span>{resource.data.AtCapacity && <span className="playback-capacity">{t('The session list reached its limit. More clients may be active.')}</span>}</div>}
    {action && <PlaybackCommandDialog action={action} sessions={resource.data?.Items ?? []} busy={busy} disabled={disabled} error={error} onNavigationGuardChange={guards.playback} onClose={() => { if (!inFlight.current) { setAction(undefined); setError(undefined); } }} onSubmit={send} />}
  </section>;
}

function PlaybackCommandDialog({ action, sessions, busy, disabled, error, onNavigationGuardChange, onClose, onSubmit }: {
  action: PlaybackAction; sessions: PlaybackSession[]; busy: boolean; disabled: boolean; error: unknown;
  onNavigationGuardChange: UserNavigationGuardChange; onClose: () => void;
  onSubmit: (session: PlaybackSession | undefined, input: PlaybackCommandInput) => Promise<boolean>;
}) {
  const { t, formatCount } = useTranslation();
  const [message, setMessage] = useState('');
  const [time, setTime] = useState(action.session ? playbackTime(action.session.PlayState.PositionTicks) : '0:00');
  const [selected, setSelected] = useState<MetadataItemSummary[]>([]);
  const [playMode, setPlayMode] = useState<'PlayNow' | 'PlayNext' | 'PlayLast'>('PlayNow');
  const [selectionReduced, setSelectionReduced] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const initialFocus = useRef<HTMLInputElement>(null);
  const live = action.session ? sessions.find((session) => session.Id === action.session?.Id) : undefined;
  const title = ({ stop: 'Stop this playback?', message: action.session ? 'Message this player' : 'Message all players', seek: 'Seek to time', play: 'Play media on this device' })[action.kind];
  const command: PlaybackCommand = action.kind === 'stop' ? 'Stop' : action.kind === 'message' ? 'DisplayMessage' : action.kind === 'seek' ? 'Seek' : playMode;
  const playbackChanged = (action.kind === 'stop' || action.kind === 'seek') && action.session.PlaySessionId !== live?.PlaySessionId;
  const targetAvailable = action.session ? !playbackChanged && commandAllowed(live, command) && (action.kind !== 'seek' || !!live?.PlayState.CanSeek) : sessions.some((session) => session.NowPlayingItem && commandAllowed(session, 'DisplayMessage'));
  const target = action.session ? sessionTitle(action.session) : undefined;
  const parsedTime = parsePlaybackTime(time);
  const duration = playbackTicks(live?.NowPlayingItem?.RunTimeTicks);
  const invalidTime = action.kind === 'seek' && (parsedTime === undefined || duration === undefined || BigInt(parsedTime) > duration);
  const invalidMessage = action.kind === 'message' && (!message.trim() || message.length > 120 || /[\p{Cc}\uD800-\uDFFF]/u.test(message));
  const unsupportedSelection = action.kind === 'play' && selected.some((item) => !live?.PlayableMediaTypes.includes(item.Type === 'Audio' ? 'Audio' : 'Video'));
  const dirty = action.kind === 'message' ? message !== '' : action.kind === 'play' ? selected.length > 0 : action.kind === 'seek' ? time !== playbackTime(action.session?.PlayState.PositionTicks) : false;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard this playback command and leave this page?');
  useLayoutEffect(() => {
    // React autoFocus runs before the native dialog opens. Focus after the
    // dialog's child layout effect calls showModal so the first edit is ready.
    const input = initialFocus.current;
    const dialog = input?.closest('dialog');
    if (input && dialog?.open && dialog.matches(':modal')) input.focus({ preventScroll: true });
  }, [action.kind]);
  function close() { if (!busy && (!dirty || confirmAction('Discard this playback command?'))) onClose(); }
  function changePlayMode(mode: typeof playMode) {
    setPlayMode(mode);
    if (mode === 'PlayNow' && selected.length > 1) {
      setSelected((current) => current.slice(0, 1));
      setSelectionReduced(true);
    } else setSelectionReduced(false);
  }
  function chooseMedia(items: MetadataItemSummary[]) {
    setSelected(playMode === 'PlayNow' ? items.slice(0, 1) : items);
    setSelectionReduced(false);
  }
  function submit(event: FormEvent) {
    event.preventDefault(); setSubmitted(true);
    if (disabled || busy || !targetAvailable || invalidMessage || invalidTime || unsupportedSelection
      || action.kind === 'play' && (selected.length === 0 || selected.length > (playMode === 'PlayNow' ? 1 : 128))) return;
    void onSubmit(action.session, { Command: command,
      ...(action.kind === 'message' ? { Text: message.trim(), Header: 'Goby', TimeoutMs: 10000 } : {}),
      ...(action.kind === 'seek' ? { SeekPositionTicks: parsedTime } : {}),
      ...(action.kind === 'play' ? { ItemIds: selected.map((item) => item.Id) } : {}),
    });
  }
  return <Dialog open data-layout={action.kind === 'play' ? undefined : 'modal'} maxWidth={action.kind === 'play' ? 'sm' : 'xs'} onClose={close} aria-labelledby="playback-command-title" aria-describedby="playback-command-description" className="playback-command-dialog">
    <form className="playback-command-form" onSubmit={submit} aria-busy={busy}>
      <DialogTitle id="playback-command-title">{t(title)}</DialogTitle>
      <DialogContent><div className="playback-dialog-content"><p id="playback-command-description">{target ? <><span translate="no">{action.session?.UserName ? `${action.session.UserName} · ` : ''}{target}</span></> : t('Your message will be sent to supported, connected players. Other sessions are listed as skipped.')}</p>
        {action.kind === 'stop' && <p>{t('The player will receive a stop request. Playback stays visible until it reports that it has stopped.')}</p>}
        {action.kind === 'message' && <><label className="playback-field" htmlFor="playback-message"><span>{t('Message')}</span><input id="playback-message" ref={initialFocus} autoComplete="off" value={message} maxLength={120} disabled={busy} onChange={(event) => setMessage(event.target.value)} aria-invalid={submitted && invalidMessage} aria-describedby="playback-message-help" /></label><small id="playback-message-help" className={submitted && invalidMessage ? 'playback-field-error' : ''}>{submitted && invalidMessage ? t('Enter a single-line message of up to 120 characters.') : t('Shown on the player for 10 seconds, if the client supports messages.')}</small><div className="playback-message-templates" aria-label={t('Message templates')}>{['The server will restart in 5 minutes.', 'Maintenance starts at 23:00 and lasts about 10 minutes.'].map((template) => <button key={template} type="button" className="playback-text-button" disabled={busy} onClick={() => setMessage(t(template))}>{t(template)}</button>)}</div><span className="playback-mono playback-message-count">{message.length} / 120</span></>}
        {action.kind === 'seek' && <><label className="playback-field" htmlFor="playback-seek"><span>{t('Playback time')}</span><input id="playback-seek" ref={initialFocus} autoComplete="off" inputMode="numeric" value={time} disabled={busy} onChange={(event) => setTime(event.target.value)} aria-invalid={submitted && invalidTime} aria-describedby="playback-seek-help" /></label><small id="playback-seek-help" className={submitted && invalidTime ? 'playback-field-error' : ''}>{t('Enter minutes:seconds or hours:minutes:seconds, up to {duration}.', { duration: playbackTime(live?.NowPlayingItem?.RunTimeTicks) })}</small></>}
        {action.kind === 'play' && <><label className="playback-field" htmlFor="playback-mode"><span>{t('Queue action')}</span><select id="playback-mode" value={playMode} disabled={busy} onChange={(event) => changePlayMode(event.target.value as typeof playMode)}><option value="PlayNow">{t('Play now')}</option><option value="PlayNext" disabled={!commandAllowed(live, 'PlayNext')}>{t('Play next')}</option><option value="PlayLast" disabled={!commandAllowed(live, 'PlayLast')}>{t('Add to queue')}</option></select></label>{playMode === 'PlayNow' && live?.NowPlayingItem && <p className="playback-inline-warning">{t('Playing now replaces the current playback on this device.')}</p>}{selectionReduced && <p className="playback-inline-warning" role="status">{t('Play now supports one item. The first selected item has been kept.')}</p>}<PlaybackMediaPicker mediaTypes={live?.PlayableMediaTypes ?? []} multiple={playMode !== 'PlayNow'} disabled={busy} selected={selected} onSelectedChange={chooseMedia} /><div className="playback-picker-selection"><span>{formatCount(selected.length, 'selected item', 'selected items')}</span><small>{t(playMode === 'PlayNow' ? 'Choose one media item to play now.' : 'Select up to 128 media items.')}</small>{selected.length > 0 && <button type="button" className="playback-text-button" disabled={busy} onClick={() => chooseMedia([])}>{t('Clear selection')}</button>}</div>{selected.length > 0 && <ul className="playback-selected-items" aria-label={t('Selected media')}>{selected.map((item) => <li key={item.Id}><span translate="no">{item.Name}</span><button type="button" className="playback-text-button playback-icon-button" disabled={busy} aria-label={t('Remove {name} from selection', { name: item.Name })} onClick={() => chooseMedia(selected.filter((entry) => entry.Id !== item.Id))}><Icon name="close" size={13} /></button></li>)}</ul>}<small>{t('Only media this user can access can be sent to the player.')}</small>{submitted && selected.length === 0 && <p className="playback-field-error">{t('Select media to play.')}</p>}{unsupportedSelection && <p className="playback-field-error">{t('Some selected media is no longer supported by this player. Remove it before sending.')}</p>}</>}
        {!targetAvailable && <p className="playback-inline-warning" role="status">{t(playbackChanged ? 'Playback changed while these controls were open. Close this dialog and review the current title.' : 'This player is no longer available for that command. Refresh playback.')}</p>}
        {error != null && <div className="playback-notice is-error" role="alert"><Icon name="info" /><div>{t(errorText(error))}</div></div>}
      </div></DialogContent>
      <DialogActions><button type="button" className="playback-button" disabled={busy} onClick={close}>{t('Cancel')}</button><button type="submit" className={`playback-button${action.kind === 'stop' ? ' playback-button-danger' : ' playback-button-primary'}`} disabled={disabled || !targetAvailable || busy}>{busy ? t('Sending command…') : t(action.kind === 'stop' ? 'Request stop' : action.kind === 'message' ? 'Send message' : action.kind === 'seek' ? 'Request seek' : 'Send to player')}</button></DialogActions>
    </form>
  </Dialog>;
}

function PlaybackMediaPicker({ mediaTypes, multiple, disabled, selected, onSelectedChange }: { mediaTypes: string[]; multiple: boolean; disabled: boolean; selected: MetadataItemSummary[]; onSelectedChange: (items: MetadataItemSummary[]) => void }) {
  const { t } = useTranslation();
  const [libraries, setLibraries] = useState<Library[]>();
  const [libraryId, setLibraryId] = useState('');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState({ term: '', page: 0 });
  const [data, setData] = useState<MetadataItemsResponse>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const types = useMemo(() => [...(mediaTypes.includes('Video') ? ['Movie', 'Episode', 'Video'] : []), ...(mediaTypes.includes('Audio') ? ['Audio'] : [])], [mediaTypes.join(',')]);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined);
    void playbackPickerRead((signal) => adminApi.getLibraries({ signal }), controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      setLibraries(result.Items); setLibraryId((current) => result.Items.some((library) => library.Id === current) ? current : result.Items[0]?.Id ?? '');
      if (!result.Items.length) setLoading(false);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); setLoading(false); } });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    if (!libraryId || !types.length) return;
    const controller = new AbortController(); setLoading(true); setData(undefined); setError(undefined);
    void playbackPickerRead((signal) => adminApi.getLibraryItems(libraryId, { SearchTerm: query.term || undefined, Types: types, StartIndex: query.page * 25, Limit: 25 }, { signal }), controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      if (query.page > 0 && query.page * 25 >= result.TotalRecordCount) setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1) }));
      else setData(result);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [libraryId, query, revision, types]);
  const playable = data?.Items.filter((item) => !item.IsFolder && types.includes(item.Type)) ?? [];
  return <div className="playback-media-picker">
    <div className="playback-picker-filters"><label className="playback-field" htmlFor="playback-library"><span>{t('Source library')}</span><select id="playback-library" value={libraryId} disabled={disabled || !libraries?.length} onChange={(event) => { setLibraryId(event.target.value); setSearch(''); setQuery({ term: '', page: 0 }); }}>{libraries?.map((library) => <option key={library.Id} value={library.Id} translate="no">{library.Name}</option>)}</select></label><label className="playback-field" htmlFor="playback-search"><span>{t('Find media')}</span><input id="playback-search" value={search} disabled={disabled || !libraries?.length} maxLength={256} onChange={(event) => setSearch(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.nativeEvent.isComposing && event.keyCode !== 229) { event.preventDefault(); setQuery({ term: search.trim(), page: 0 }); } }} /></label><button type="button" className="playback-button" disabled={disabled || loading || !libraryId} onClick={() => setQuery({ term: search.trim(), page: 0 })}>{t('Search')}</button></div>
    {error != null && <div className="playback-notice is-error" role="alert"><div>{t(errorText(error))}</div><button type="button" className="playback-text-button" disabled={disabled || loading} onClick={() => setRevision((value) => value + 1)}>{t('Retry')}</button></div>}
    {!types.length && <p className="playback-inline-warning">{t('This player has not declared a supported media type.')}</p>}
    {loading && types.length > 0 && <div className="playback-loading" role="status" aria-label={t('Loading playable media')}><span /></div>}
    {libraries?.length === 0 && <p>{t('Create and scan a library before choosing media.')}</p>}
    {data && <><ul className="playback-picker-items" aria-label={t('Playable media')}>{playable.map((item) => <li key={item.Id}><label><input type={multiple ? 'checkbox' : 'radio'} name={multiple ? undefined : 'playback-media-selection'} checked={selected.some((entry) => entry.Id === item.Id)} disabled={disabled || multiple && selected.length >= 128 && !selected.some((entry) => entry.Id === item.Id)} onChange={(event) => onSelectedChange(multiple ? event.target.checked ? [...selected, item] : selected.filter((entry) => entry.Id !== item.Id) : [item])} /><span><strong translate="no">{item.Name}</strong><small><span>{t(({ Movie: 'Movie', Episode: 'Episode', Video: 'Video', Audio: 'Audio' } as Record<string, string>)[item.Type] ?? item.Type)}</span>{item.ParentName && <span translate="no"> · {item.ParentName}</span>}{item.ProductionYear ? ` · ${item.ProductionYear}` : ''}</small></span></label></li>)}{playable.length === 0 && <li className="playback-picker-empty">{t('No matching media.')}</li>}</ul><div className="playback-picker-pagination"><button type="button" className="playback-button" disabled={disabled || loading || query.page === 0} onClick={() => setQuery((current) => ({ ...current, page: current.page - 1 }))}>{t('Previous page')}</button><span>{t('Page {page}', { page: query.page + 1 })}</span><button type="button" className="playback-button" disabled={disabled || loading || (query.page + 1) * 25 >= data.TotalRecordCount} onClick={() => setQuery((current) => ({ ...current, page: current.page + 1 }))}>{t('Next page')}</button></div></>}
  </div>;
}

async function playbackPickerRead<T>(load: (signal: AbortSignal) => Promise<T>, signal: AbortSignal): Promise<T> {
  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal.addEventListener('abort', abort, { once: true });
  if (signal.aborted) abort();
  let timeout: ReturnType<typeof setTimeout> | undefined;
  let timedOut = false;
  try {
    const deadline = new Promise<never>((_resolve, reject) => {
      timeout = setTimeout(() => { timedOut = true; controller.abort(); reject(new ApiError('Media selection did not respond within 15 seconds. Retry to load it again.', { code: 'playback_picker_timeout' })); }, requestTimeout);
    });
    return await Promise.race([load(controller.signal), deadline]);
  } catch (error) {
    if (timedOut) throw new ApiError('Media selection did not respond within 15 seconds. Retry to load it again.', { code: 'playback_picker_timeout' });
    throw error;
  } finally { clearTimeout(timeout); signal.removeEventListener('abort', abort); }
}
