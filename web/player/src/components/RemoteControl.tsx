import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useApp } from '../context';
import { api } from '../lib/api';
import { connectRemoteControl, type RemoteCommand, type RemoteConnectionState, type RemotePlaybackCommand } from '../lib/remoteControl';
import './remote-control.css';

type PlaybackReceiver = { requestId?: number; run: (command: RemotePlaybackCommand) => Promise<void> };
interface RemoteControlState {
  status: RemoteConnectionState;
  register: (receiver: PlaybackReceiver) => () => void;
}
const RemoteContext = createContext<RemoteControlState>({ status: 'unavailable', register: () => () => undefined });
type DisplayMessage = Extract<RemoteCommand, { kind: 'message' }> & { serial: number };

export function useRemoteControl() { return useContext(RemoteContext); }

export function useRemotePlayback(receiver: (command: RemotePlaybackCommand) => Promise<void>, enabled: boolean, requestId?: number) {
  const { register } = useRemoteControl();
  const current = useRef(receiver);
  current.current = receiver;
  useEffect(() => {
    if (!enabled) return;
    return register({ requestId, run: command => current.current(command) });
  }, [register, enabled, requestId]);
}

function ServerMessage({ message, dismiss }: { message: DisplayMessage; dismiss: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const panel = useRef<HTMLElement>(null);
  const [container, setContainer] = useState<Element>(() => document.fullscreenElement ?? document.body);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const remaining = useRef(message.timeoutMs ?? 0);
  const started = useRef(0);
  const hovered = useRef(false);
  const dismissRef = useRef(dismiss);
  dismissRef.current = dismiss;
  const pause = useCallback(() => {
    if (!timer.current) return;
    clearTimeout(timer.current); timer.current = undefined;
    remaining.current = Math.max(0, remaining.current - (performance.now() - started.current));
  }, []);
  const resume = useCallback(() => {
    if (!message.timeoutMs || timer.current || hovered.current || panel.current?.contains(document.activeElement)) return;
    started.current = performance.now();
    timer.current = setTimeout(() => { timer.current = undefined; dismissRef.current(); }, remaining.current);
  }, [message.timeoutMs]);
  useEffect(() => {
    const changed = () => setContainer(document.fullscreenElement ?? document.body);
    document.addEventListener('fullscreenchange', changed);
    return () => document.removeEventListener('fullscreenchange', changed);
  }, []);
  useEffect(() => {
    if (message.timeoutMs) { resume(); return pause; }
    const element = dialog.current;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    element?.showModal();
    return () => {
      const restore = element?.contains(document.activeElement) || document.activeElement === document.body;
      element?.close();
      if (restore && previous?.isConnected) previous.focus({ preventScroll: true });
    };
  }, [message.serial, message.timeoutMs, container, resume, pause]);
  if (message.timeoutMs) return createPortal(<aside ref={panel} className="remote-message remote-message-timed" role="status" aria-live="polite"
    onPointerEnter={() => { hovered.current = true; pause(); }} onPointerLeave={() => { hovered.current = false; resume(); }}
    onFocusCapture={pause} onBlurCapture={() => { queueMicrotask(resume); }}>
    <div><h2>{message.header}</h2><p>{message.text}</p></div><button type="button" aria-label="关闭消息" onClick={dismiss}>×</button>
  </aside>, container);
  return createPortal(<dialog ref={dialog} className="remote-message remote-message-modal" aria-labelledby={`remote-message-heading-${message.serial}`}
    aria-describedby={`remote-message-content-${message.serial}`} onCancel={event => { event.preventDefault(); dismiss(); }} onKeyDown={event => event.stopPropagation()}>
    <div><span className="remote-message-eyebrow">服务器消息</span><h2 id={`remote-message-heading-${message.serial}`}>{message.header}</h2><p id={`remote-message-content-${message.serial}`}>{message.text}</p></div>
    <div className="remote-message-actions"><button type="button" onClick={dismiss} autoFocus>知道了</button></div>
  </dialog>, container);
}

export function RemoteControlProvider({ children }: { children: ReactNode }) {
  const app = useApp();
  const appRef = useRef(app); appRef.current = app;
  const receiver = useRef<PlaybackReceiver | null>(null);
  const waiters = useRef(new Set<() => void>());
  const requestId = useRef(0);
  const messageId = useRef(0);
  const [message, setMessage] = useState<DisplayMessage | null>(null);
  const [status, setStatus] = useState<RemoteConnectionState>('connecting');
  const register = useCallback((value: PlaybackReceiver) => {
    receiver.current = value;
    for (const ready of waiters.current) ready();
    return () => { if (receiver.current === value) receiver.current = null; };
  }, []);
  const dismiss = useCallback(() => setMessage(null), []);

  useEffect(() => {
    const owner = app.session;
    let active = true;
    const owned = () => active && api.matchesSession(owner);
    setMessage(null);
    const waitForReceiver = (id: number, signal: AbortSignal) => new Promise<void>((resolve, reject) => {
      let timeout: ReturnType<typeof setTimeout> | undefined;
      const finish = (error?: Error) => {
        clearTimeout(timeout); waiters.current.delete(ready); signal.removeEventListener('abort', aborted);
        if (error) reject(error); else resolve();
      };
      const ready = () => { if (receiver.current?.requestId === id) finish(); };
      const aborted = () => finish(new Error('The remote playback request was cancelled.'));
      if (signal.aborted || !owned()) { aborted(); return; }
      timeout = setTimeout(() => finish(new Error('The remote playback request did not become ready.')), 15_000);
      signal.addEventListener('abort', aborted, { once: true });
      waiters.current.add(ready); ready();
    });
    const disconnected = connectRemoteControl({
      sessionId: owner.SessionInfo?.Id,
      current: owned,
      register: signal => api.registerRemoteCapabilities(signal),
      socketUrl: () => api.remoteSocketUrl(owner),
      status: value => { if (owned()) setStatus(value); },
      command: async (command, signal) => {
        if (!owned() || signal.aborted) return;
        if (command.kind === 'message') { setMessage({ ...command, serial: ++messageId.current }); return; }
        if (command.kind === 'playstate') {
          const target = receiver.current;
          if (!target) { appRef.current.notify('当前没有正在播放的影片。'); return; }
          await target.run(command);
          return;
        }
        try {
          const item = await api.item(command.itemId, signal);
          if (!owned() || signal.aborted) return;
          if (!['Movie', 'Episode', 'Video', 'MusicVideo', 'Trailer'].includes(item.Type)) {
            appRef.current.notify('远程播放仅支持单个视频，请选择影片或剧集单集。');
            return;
          }
          const id = ++requestId.current;
          // A remote request targets this browser, regardless of an individual's
          // external-player preference. It never launches a custom URL scheme.
          appRef.current.navigate({ page: 'player', id: item.Id, playback: command.options, playbackRequestId: id });
          await waitForReceiver(id, signal);
        } catch {
          if (owned() && !signal.aborted) appRef.current.notify('远程播放未能开始，请检查影片和服务器连接。');
        }
      },
    });
    return () => { active = false; disconnected(); receiver.current = null; };
  }, [app.session.AccessToken, app.session.User.Id, app.session.serverUrl, app.session.SessionInfo?.Id]);

  return <RemoteContext.Provider value={{ status, register }}>{children}{message && <ServerMessage key={message.serial} message={message} dismiss={dismiss}/>}</RemoteContext.Provider>;
}

export function RemoteControlStatus() {
  const { status } = useRemoteControl();
  const labels: Record<RemoteConnectionState, string> = {
    connecting: '正在连接', connected: '已连接', reconnecting: '正在重新连接', offline: '网络已断开', unavailable: '当前浏览器不支持',
  };
  return <span className="remote-control-status" data-state={status} role="status"><i aria-hidden="true"/>{labels[status]}</span>;
}
