import { useLayoutEffect, useRef } from 'react';
import { useLibraryInvalidation } from '../components/RemoteControl';
import { useApp } from '../context';
import { api } from './api';

const refreshDelay = 150;
const refreshTimeout = 15_000;

// One read at a time for the mounted view. A change during a read marks it
// dirty, so the superseded response is not applied and a trailing read follows.
export function useLibraryRefresh(enabled: boolean, read: (signal: AbortSignal, current: () => boolean) => Promise<void>) {
  const { session } = useApp();
  const refresh = useRef<(signal: AbortSignal) => void>(() => {});
  useLibraryInvalidation(signal => refresh.current(signal), enabled);

  useLayoutEffect(() => {
    if (!enabled) return;
    const view = new AbortController();
    let pending: AbortSignal | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let running = false;
    const ownsView = () => !view.signal.aborted && api.matchesSession(session);
    function schedule() {
      if (running || timer !== undefined || !pending || pending.aborted || !ownsView()) return;
      timer = setTimeout(() => { timer = undefined; void run(); }, refreshDelay);
    }
    async function run() {
      const connection = pending;
      pending = undefined;
      if (!connection || connection.aborted || !ownsView()) return;
      running = true;
      const signal = AbortSignal.any([view.signal, connection, AbortSignal.timeout(refreshTimeout)]);
      // A timeout may be reported by the reader; an obsolete owner may not
      // publish either data or an error. Readers pass the signal to fetch.
      try {
        await read(signal, () => !connection.aborted && !pending && ownsView());
      } catch {
        // A consumer handles its own errors; do not strand a pending refresh.
      } finally {
        running = false;
        schedule();
      }
    }
    refresh.current = signal => { pending = signal; schedule(); };
    return () => {
      refresh.current = () => {};
      clearTimeout(timer);
      view.abort();
    };
  }, [enabled, read, session]);
}
