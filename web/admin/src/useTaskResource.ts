import { useCallback, useEffect, useRef, useState } from 'react';
import { isAbortError } from './api';

interface TaskResourceOptions<T> {
  key: string;
  load: (signal: AbortSignal) => Promise<T>;
  poll: (result: T) => boolean;
  enabled?: boolean;
}

export function useTaskResource<T>({ key, load, poll, enabled = true }: TaskResourceOptions<T>) {
  const [snapshot, setSnapshot] = useState<{ key: string; data?: T; error?: unknown; loading: boolean }>({ key, loading: true });
  const [revision, setRevision] = useState(0);
  const [paused, setPaused] = useState(document.hidden);
  const stop = useRef<(() => void) | undefined>(undefined);
  const reload = useCallback(() => { stop.current?.(); setRevision((value) => value + 1); }, []);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    let failed = false;
    let shouldPoll = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let controller: AbortController | undefined;
    function clearTimer() { if (timer !== undefined) clearTimeout(timer); timer = undefined; }
    function halt() { clearTimer(); controller?.abort(); controller = undefined; }
    async function refresh() {
      if (!active || failed || controller || document.hidden) return;
      clearTimer();
      const request = new AbortController();
      controller = request;
      setSnapshot((current) => ({ key, data: current.key === key ? current.data : undefined, loading: true }));
      try {
        const result = await load(request.signal);
        if (!active || request.signal.aborted) return;
        shouldPoll = poll(result);
        setSnapshot({ key, data: result, loading: false });
      } catch (error) {
        if (active && !request.signal.aborted && !isAbortError(error)) {
          failed = true;
          setSnapshot({ key, error, loading: false });
        }
      } finally {
        if (controller === request) controller = undefined;
        if (active && !request.signal.aborted && !failed && shouldPoll && !document.hidden) timer = setTimeout(() => void refresh(), 5000);
      }
    }
    function visibilityChanged() {
      setPaused(document.hidden);
      if (document.hidden) halt();
      else if (!failed) void refresh();
    }
    stop.current = halt;
    setPaused(document.hidden);
    setSnapshot((current) => ({ key, data: current.key === key ? current.data : undefined, loading: !document.hidden }));
    document.addEventListener('visibilitychange', visibilityChanged);
    void refresh();
    return () => { active = false; halt(); if (stop.current === halt) stop.current = undefined; document.removeEventListener('visibilitychange', visibilityChanged); };
  }, [key, load, poll, revision, enabled]);

  return {
    data: snapshot.key === key ? snapshot.data : undefined,
    error: snapshot.key === key ? snapshot.error : undefined,
    loading: enabled && (snapshot.key === key ? snapshot.loading : true),
    paused,
    reload,
  };
}
