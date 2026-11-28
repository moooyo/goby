import type { FragmentLoaderContext, HlsConfig, Loader, LoaderCallbacks, LoaderConfiguration } from 'hls.js';

// Allow the server's three-minute job budget plus one polling interval.
const preparationBudgetMs = 185_000;

function retryDelay(response: Response): number | undefined {
  if ((response.status !== 429 && response.status !== 503) || response.headers.get('X-Goby-Subtitle-Preparing') !== '1') return;
  const seconds = response.headers.get('Retry-After') ?? '';
  return /^\d+$/.test(seconds) ? Math.min(5_000, Math.max(1_000, Number(seconds) * 1_000)) : 2_000;
}

function waitToRetry(delay: number, signal: AbortSignal): Promise<void> {
  return new Promise(resolve => {
    const finish = () => {
      clearTimeout(timer);
      signal.removeEventListener('abort', finish);
      resolve();
    };
    const timer = window.setTimeout(finish, delay);
    signal.addEventListener('abort', finish, { once: true });
    if (signal.aborted) finish();
  });
}

class SubtitlePreparation {
  private deadline?: number;
  private retryAt = 0;

  constructor(private readonly options: { credentials?: RequestCredentials; requestTimeoutMs?: number; retryPreparing?: boolean } = {}) {}

  async load(url: string, signal: AbortSignal, headers = new Headers()) {
    // A seek can replace one fragment with another without changing the extraction job.
    const deadline = this.deadline ??= performance.now() + preparationBudgetMs;
    const { credentials, requestTimeoutMs, retryPreparing = true } = this.options;
    const timeout = new AbortController();
    const request = AbortSignal.any([signal, timeout.signal]);
    const timer = window.setTimeout(() => timeout.abort(), deadline - performance.now());
    headers.set('Accept', 'text/vtt');
    try {
      while (!request.aborted && performance.now() < deadline) {
        if (this.retryAt >= deadline) return;
        const remaining = this.retryAt - performance.now();
        if (remaining > 0) {
          await waitToRetry(remaining, request);
          continue;
        }
        const readTimeout = requestTimeoutMs === undefined ? undefined : new AbortController();
        const read = readTimeout ? AbortSignal.any([request, readTimeout.signal]) : request;
        const readTimer = readTimeout && window.setTimeout(() => readTimeout.abort(), requestTimeoutMs);
        try {
          const response = await fetch(url, { signal: read, mode: 'same-origin', redirect: 'error', cache: 'no-store', credentials, headers });
          if (read.aborted) return;
          if ((response.status === 200 || response.status === 206) && /^text\/vtt(?:\s*;|$)/i.test(response.headers.get('Content-Type') ?? '')) {
            const data = await response.arrayBuffer();
            if (read.aborted || !/^WEBVTT(?:[\t \r\n]|$)/.test(new TextDecoder().decode(data.slice(0, 10)))) return;
            this.deadline = undefined;
            this.retryAt = 0;
            return { response, data };
          }
          void response.body?.cancel().catch(() => undefined);
          const delay = retryPreparing ? retryDelay(response) : undefined;
          if (delay === undefined) return;
          this.retryAt = performance.now() + delay;
        } finally { if (readTimer !== undefined) clearTimeout(readTimer); }
      }
    } catch { /* A failed subtitle request never interrupts the video. */ }
    finally { clearTimeout(timer); }
  }
}

export async function loadSubtitleText(url: string, signal: AbortSignal, retryPreparing: boolean): Promise<string | undefined> {
  // Detail reads retain their short request timeout; only marked preparation may extend the overall wait.
  const result = await new SubtitlePreparation({ credentials: 'omit', requestTimeoutMs: 15_000, retryPreparing }).load(url, signal);
  return result && new TextDecoder().decode(result.data);
}

export function loadDirectSubtitleTrack(video: HTMLVideoElement, url: string, signal: AbortSignal): void {
  void new SubtitlePreparation().load(url, signal).then(result => {
    if (!result || signal.aborted) return;
    // Native tracks do not expose HTTP status or headers; let the browser parse the successful VTT.
    const address = URL.createObjectURL(new Blob([result.data], { type: 'text/vtt' }));
    const track = document.createElement('track');
    track.kind = 'subtitles';
    track.label = '字幕';
    track.src = address;
    video.append(track);
    track.track.mode = 'hidden';
    signal.addEventListener('abort', () => { track.remove(); URL.revokeObjectURL(address); }, { once: true });
  });
}

export function subtitleCueText(tracks: Iterable<TextTrack>): string[] {
  const values = new Set<string>();
  for (const track of tracks) {
    if (track.mode !== 'hidden') continue;
    for (const cue of Array.from(track.activeCues ?? [])) {
      const text = 'getCueAsHTML' in cue ? (cue as VTTCue).getCueAsHTML().textContent : null;
      if (text) values.add(text);
    }
  }
  return [...values];
}

// Native HLS requests cues even in hidden mode. Only the exact source stream
// named by the Goby rendition may load cues and drive the overlay.
export function bindNativeHLSSubtitles(video: HTMLVideoElement, streamIndex: number, publish: (cues: string[]) => void): () => void {
  const list = video.textTracks;
  const textTracks = () => Array.from(list).filter(track => track.kind === 'subtitles' || track.kind === 'captions');
  const priorSource = new WeakSet(textTracks());
  const suffix = ` [${streamIndex}]`;
  let selected: TextTrack | undefined;
  let disposed = false;
  let reconciling = false;
  const refresh = () => {
    if (!disposed && !reconciling) publish(!video.seeking && selected ? subtitleCueText([selected]) : []);
  };
  const clear = () => { if (!disposed) publish([]); };
  const reconcile = () => {
    if (disposed || reconciling) return;
    reconciling = true;
    try {
      const tracks = textTracks();
      const matches = streamIndex < 0 ? [] : tracks.filter(track => !priorSource.has(track) && track.label.endsWith(suffix));
      const next = matches.length === 1 ? matches[0] : undefined;
      if (selected !== next) {
        selected?.removeEventListener('cuechange', refresh);
        if (selected && selected.mode !== 'disabled') selected.mode = 'disabled';
        selected = next;
        selected?.addEventListener('cuechange', refresh);
      }
      for (const track of tracks) if (track !== selected && track.mode !== 'disabled') track.mode = 'disabled';
      if (selected && selected.mode !== 'hidden') selected.mode = 'hidden';
    } finally { reconciling = false; }
    refresh();
  };
  const listEvents = ['addtrack', 'removetrack', 'change'] as const;
  const readyEvents = ['loadedmetadata', 'loadeddata'] as const;
  for (const event of listEvents) list.addEventListener(event, reconcile);
  for (const event of readyEvents) video.addEventListener(event, reconcile);
  video.addEventListener('seeking', clear);
  video.addEventListener('seeked', refresh);
  reconcile();
  return () => {
    if (disposed) return;
    disposed = true;
    for (const event of listEvents) list.removeEventListener(event, reconcile);
    for (const event of readyEvents) video.removeEventListener(event, reconcile);
    video.removeEventListener('seeking', clear);
    video.removeEventListener('seeked', refresh);
    selected?.removeEventListener('cuechange', refresh);
    if (selected && selected.mode !== 'disabled') selected.mode = 'disabled';
    for (const track of textTracks()) if (track.mode !== 'disabled') track.mode = 'disabled';
    publish([]);
  };
}

export function subtitleFragmentLoader(onUnavailable: () => void): NonNullable<HlsConfig['fLoader']> {
  const preparation = new SubtitlePreparation();
  return class implements Loader<FragmentLoaderContext> {
    readonly delegate: Loader<FragmentLoaderContext>;
    context: FragmentLoaderContext | null = null;
    private request?: AbortController;
    private notifyAbort?: () => void;

    constructor(config: HlsConfig) { this.delegate = new config.loader(config) as Loader<FragmentLoaderContext>; }
    get stats() { return this.delegate.stats; }
    getCacheAge() { return this.delegate.getCacheAge?.() ?? null; }
    getResponseHeader(name: string) { return this.delegate.getResponseHeader?.(name) ?? null; }

    load(context: FragmentLoaderContext, config: LoaderConfiguration, callbacks: LoaderCallbacks<FragmentLoaderContext>) {
      this.context = context;
      if (context.frag.type !== 'subtitle') { this.delegate.load(context, config, callbacks); return; }
      const url = new URL(context.url, location.href);
      if (url.origin !== location.origin || !/\.vtt$/i.test(url.pathname)) { this.delegate.load(context, config, callbacks); return; }
      const request = this.request = new AbortController();
      const { stats } = this;
      stats.loading.start = performance.now();
      this.notifyAbort = () => {
        stats.aborted = true;
        callbacks.onAbort?.(stats, context, null);
      };
      const headers = new Headers(context.headers);
      headers.set('Prefer', 'respond-async');
      if (context.rangeEnd) headers.set('Range', `bytes=${context.rangeStart ?? 0}-${context.rangeEnd - 1}`);
      void preparation.load(url.href, request.signal, headers).then(result => {
        if (request.signal.aborted) return;
        if (!result) {
          // hls.js can escalate a subtitle fragment error to a fatal variant error.
          // Cancel the fragment and disable that rendition instead of retrying at the controller level.
          this.abort();
          onUnavailable();
          return;
        }
        stats.loaded = stats.total = result.data.byteLength;
        stats.loading.first = stats.loading.end = performance.now();
        callbacks.onProgress?.(stats, context, result.data, result.response);
        if (!request.signal.aborted) callbacks.onSuccess({ url: result.response.url, data: result.data, code: result.response.status }, stats, context, result.response);
      });
    }

    abort() {
      if (!this.request) { this.delegate.abort(); return; }
      if (this.request.signal.aborted) return;
      const notify = this.notifyAbort;
      this.notifyAbort = undefined;
      this.request.abort();
      notify?.();
    }

    destroy() {
      this.request?.abort();
      this.notifyAbort = undefined;
      this.context = null;
      this.delegate.destroy();
    }
  };
}
