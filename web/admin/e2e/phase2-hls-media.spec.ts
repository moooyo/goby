import { createHash, randomBytes } from 'node:crypto';
import { closeSync, constants, existsSync, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const bundleSHA256 = '04a55387b26d6becff5b87b470b9b19c3fe41d25c0cd3c1e6c3d885a56572fdb';

interface CueFixture {
  StreamIndex: number;
  Language: string;
  CueText: string;
  StartSeconds: number;
  EndSeconds: number;
}

interface Fixture {
  Marker: string;
  RunId: string;
  BaseURL: string;
  Token: string;
  HlsBundlePath: string;
  ArtifactsDir: string;
  ResultPath: string;
  Finite: {
    PlaybackURL: string;
    ObservePath: string;
    StopPath: string;
    Tracks: CueFixture[];
    ProbeSeconds: number;
    TimelineOffsetSeconds: number;
    PixelRGB: number[];
    OffsetTicks: number;
  };
  Dynamic: {
    PlaybackURL: string;
    ObservePath: string;
    StopPath: string;
    DisconnectPath: string;
    ReconnectPath: string;
    SubtitleLanguage: string;
    CuePrefix: string;
    PauseSeconds: number;
    ExpectedExpiredStatus: number;
  };
}

interface Observation {
  ProducerIds: string[];
  ActiveJobs: number;
  Epoch: number;
  MediaSequence: number;
  WindowStartTicks: number;
  WindowEndTicks: number;
}

interface Cue {
  Text: string;
  Start: number;
  End: number;
}

interface CueChange {
  At: number;
  MediaTime: number;
  PresentedTime: number;
  Language: string;
  Mode: string;
  Active: Cue[];
}

interface PresentationFrame {
  FrameNumber: number;
  CallbackAt: number;
  PresentedAt: number;
  MediaTime: number;
  PresentedTime: number;
  ReadyState: number;
  Seeking: boolean;
  Paused: boolean;
}

interface PresentationTransition {
  Kind: 'load' | 'seek' | 'resume' | 'live';
  Generation: number;
  TargetSeconds: number | null;
  FramesBefore: number;
  BrowserFramesBefore: number;
  StartedAt: number;
  WaitMilliseconds: number;
  PlayResolved: boolean;
  Seeked: boolean;
  Completed: boolean;
  Failure: string;
  Frames: PresentationFrame[];
  StableFrames: PresentationFrame[];
}

interface Snapshot {
  MediaTime: number;
  PresentedTime: number;
  Frames: number;
  Paused: boolean;
  Seeking: boolean;
  ReadyState: number;
  Presentation: PresentationTransition | null;
  Width: number;
  Height: number;
  ErrorCode: number;
  SelectedTrack: number;
  Tracks: { Index: number; Language: string; Name: string }[];
  ActiveCues: Cue[];
  CueChanges: CueChange[];
  Pixel: number[];
  LevelUpdates: number;
  Sequence: number;
  Discontinuity: number;
  FragmentStart: number;
  FragmentEnd: number;
  LiveSyncPosition: number | null;
  LoadedFragments: number;
  SubtitleFragments: number;
  FatalErrors: string[];
}

interface Harness {
  load(kind: 'Finite' | 'Dynamic', selection?: number, offsetTicks?: number, at?: number): Promise<void>;
  snapshot(): Snapshot;
  select(language: string): void;
  off(): void;
  seek(seconds: number): Promise<void>;
  pause(): void;
  resume(): Promise<void>;
  live(): Promise<void>;
  observe(kind: 'Finite' | 'Dynamic'): Promise<Observation>;
  control(action: 'DisconnectPath' | 'ReconnectPath'): Promise<void>;
  expired(): Promise<number>;
  stop(kind: 'Finite' | 'Dynamic'): Promise<void>;
}

declare global { interface Window { gobyPhase2: Harness } }

function privateDirectory(directory: string): void {
  const stat = lstatSync(directory);
  if (!path.isAbsolute(directory) || directory !== path.resolve(directory) || realpathSync(directory) !== directory
    || !stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid?.() || (stat.mode & 0o777) !== 0o700) {
    throw new Error('Phase 2 browser artifacts require an owned private directory.');
  }
}

function loadFixture(): { fixture: Fixture; bundle: string } {
  const filename = process.env.GOBY_PHASE2_BROWSER_CONTEXT;
  if (process.platform !== 'linux' || !filename || !path.isAbsolute(filename) || filename !== path.resolve(filename)) {
    throw new Error('Phase 2 browser acceptance requires its isolated Linux fixture.');
  }
  privateDirectory(path.dirname(filename));
  const descriptor = openSync(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: Fixture;
  try {
    const stat = fstatSync(descriptor);
    if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600
      || stat.size < 1 || stat.size > 64 * 1024 || realpathSync(`/proc/self/fd/${descriptor}`) !== filename) {
      throw new Error('The phase 2 fixture must be a bounded private regular file.');
    }
    fixture = JSON.parse(readFileSync(descriptor, 'utf8')) as Fixture;
  } finally { closeSync(descriptor); }
  const base = new URL(fixture.BaseURL);
  if (fixture.Marker !== 'goby-phase2-hls-browser-fixture-v1' || fixture.RunId !== process.env.GOBY_PHASE2_BROWSER_RUN_ID
    || !/^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId)
    || base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || ['5432', '15432', '18096', '18097'].includes(base.port)
    || base.username || base.password || base.pathname !== '/' || base.search || base.hash
    || typeof fixture.Token !== 'string' || fixture.Token.length < 16 || fixture.Token.length > 512
    || fixture.ArtifactsDir !== path.dirname(filename) || path.dirname(fixture.ResultPath) !== fixture.ArtifactsDir
    || !path.isAbsolute(fixture.ResultPath) || fixture.ResultPath !== path.resolve(fixture.ResultPath)
    || path.extname(fixture.ResultPath) !== '.json' || fixture.ResultPath === filename || existsSync(fixture.ResultPath)) {
    throw new Error('The phase 2 fixture does not describe an isolated disposable run.');
  }
  privateDirectory(fixture.ArtifactsDir);
  fixture.BaseURL = base.origin;
  for (const target of [fixture.Finite.PlaybackURL, fixture.Finite.ObservePath, fixture.Finite.StopPath,
    fixture.Dynamic.PlaybackURL, fixture.Dynamic.ObservePath, fixture.Dynamic.StopPath, fixture.Dynamic.DisconnectPath, fixture.Dynamic.ReconnectPath]) {
    const resolved = new URL(target, base);
    if (!target.startsWith('/') || target.startsWith('//') || resolved.origin !== base.origin || resolved.username || resolved.password || resolved.hash) {
      throw new Error('Phase 2 fixture URLs must remain on the owned loopback origin.');
    }
  }
  for (const target of [fixture.Finite.ObservePath, fixture.Dynamic.ObservePath, fixture.Dynamic.DisconnectPath, fixture.Dynamic.ReconnectPath]) {
    if (!new URL(target, base).pathname.startsWith('/__phase2-')) throw new Error('The fixture controls must be private phase 2 harness routes.');
  }
  if (!Array.isArray(fixture.Finite.Tracks) || fixture.Finite.Tracks.length < 2 || fixture.Finite.Tracks.length > 8
    || new Set(fixture.Finite.Tracks.map((track) => track.Language)).size !== fixture.Finite.Tracks.length
    || !fixture.Finite.Tracks.every((track) => Number.isInteger(track.StreamIndex) && track.StreamIndex >= 0
      && /^[a-z]{2,3}$/.test(track.Language) && typeof track.CueText === 'string' && track.CueText.length > 0 && track.CueText.length < 256
      && Number.isFinite(track.StartSeconds) && Number.isFinite(track.EndSeconds) && track.StartSeconds >= 0 && track.EndSeconds > track.StartSeconds)
    || !Number.isFinite(fixture.Finite.ProbeSeconds) || !Number.isFinite(fixture.Finite.TimelineOffsetSeconds)
    || Math.abs(fixture.Finite.TimelineOffsetSeconds) > 10 || !Number.isInteger(fixture.Finite.OffsetTicks)
    || fixture.Finite.OffsetTicks < 2_000_000 || fixture.Finite.OffsetTicks > 10_000_000
    || fixture.Finite.PixelRGB.length !== 3 || !fixture.Finite.PixelRGB.every((value) => Number.isInteger(value) && value >= 0 && value <= 255)
    || !Number.isFinite(fixture.Dynamic.PauseSeconds) || fixture.Dynamic.PauseSeconds < 3 || fixture.Dynamic.PauseSeconds > 8
    || !/^[a-z]{2,3}$/.test(fixture.Dynamic.SubtitleLanguage) || typeof fixture.Dynamic.CuePrefix !== 'string'
    || fixture.Dynamic.CuePrefix.length < 1 || fixture.Dynamic.CuePrefix.length > 128
    || ![404, 410].includes(fixture.Dynamic.ExpectedExpiredStatus)) {
    throw new Error('The phase 2 caption, media clock, or retention facts are invalid.');
  }
  for (const track of fixture.Finite.Tracks) {
    const delay = fixture.Finite.OffsetTicks / 1e7;
    if (fixture.Finite.ProbeSeconds <= track.StartSeconds + delay + .15 || fixture.Finite.ProbeSeconds >= track.EndSeconds - .15) {
      throw new Error('The finite probe must be inside every cue before and after the explicit positive delay.');
    }
  }
  const bundlePath = fixture.HlsBundlePath;
  if (!path.isAbsolute(bundlePath) || bundlePath !== path.resolve(bundlePath) || realpathSync(bundlePath) !== bundlePath) {
    throw new Error('The retained HLS engine path is invalid.');
  }
  const engine = openSync(bundlePath, constants.O_RDONLY | constants.O_NOFOLLOW);
  let bundle: string;
  try {
    const stat = fstatSync(engine);
    if (!stat.isFile() || stat.size < 100_000 || stat.size > 8 * 1024 * 1024) throw new Error('The retained HLS engine is not a bounded regular bundle.');
    bundle = readFileSync(engine, 'utf8');
  } finally { closeSync(engine); }
  if (createHash('sha256').update(bundle).digest('hex') !== bundleSHA256) throw new Error('The HLS engine differs from the reviewed official client bundle.');
  return { fixture, bundle };
}

function writePrivateResult(fixture: Fixture, value: unknown): void {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (encoded.includes(fixture.Token) || encoded.length > 512 * 1024 || existsSync(fixture.ResultPath)) {
    throw new Error('The phase 2 result is not a new bounded credential-free artifact.');
  }
  const temporary = path.join(fixture.ArtifactsDir, `.phase2-${randomBytes(8).toString('hex')}.tmp`);
  const descriptor = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    try { writeFileSync(descriptor, encoded, 'utf8'); fsyncSync(descriptor); }
    finally { closeSync(descriptor); }
    if (existsSync(fixture.ResultPath)) throw new Error('The phase 2 result appeared before publication.');
    renameSync(temporary, fixture.ResultPath);
  } finally { if (existsSync(temporary)) unlinkSync(temporary); }
}

async function installHarness(page: Page, fixture: Fixture, bundle: string): Promise<void> {
  await page.evaluate(() => {
    const host = window as unknown as { define?: (dependencies: string[], factory: (exports: Record<string, unknown>) => void) => void; phase2Hls?: unknown };
    host.define = (dependencies, factory) => {
      if (dependencies.length !== 1 || dependencies[0] !== 'exports') throw new Error('Unexpected HLS bundle dependencies.');
      const exports: Record<string, unknown> = {};
      factory(exports);
      host.phase2Hls = exports.default;
    };
  });
  await page.addScriptTag({ content: bundle });
  await page.evaluate((input) => {
    interface Track { lang?: string; name?: string }
    interface Fragment { sn: number | string; cc: number; start: number; duration: number; url: string }
    interface Details { fragments: Fragment[]; startSN: number }
    interface Instance {
      on(event: string, listener: (event: string, data: { fatal?: boolean; type?: string; details?: Details | string }) => void): void;
      attachMedia(media: HTMLVideoElement): void;
      loadSource(url: string): void;
      startLoad(position?: number): void;
      destroy(): void;
      subtitleTrack: number;
      subtitleTracks: Track[];
      liveSyncPosition: number | null;
    }
    interface Constructor {
      new(options: Record<string, unknown>): Instance;
      version: string;
      isSupported(): boolean;
      Events: Record<string, string>;
    }
    const host = window as unknown as { define?: unknown; phase2Hls: Constructor };
    const Hls = host.phase2Hls;
    delete host.define;
    if (Hls.version !== '1.6.0-beta.2' || !Hls.isSupported()) throw new Error('The reviewed HLS engine is unavailable on this browser.');
    const video = document.querySelector<HTMLVideoElement>('#phase2-media')!;
    const canvas = document.querySelector<HTMLCanvasElement>('#phase2-pixels')!;
    const context = canvas.getContext('2d', { willReadFrequently: true })!;
    video.muted = true;
    let hls: Instance | undefined;
    let frames = 0;
    let presented = -1;
    let browserFrames = 0;
    let presentation: PresentationTransition | null = null;
    let settling = false;
    let generation = 0;
    let updates = 0;
    let loaded = 0;
    let subtitles = 0;
    let details: Details | undefined;
    let expiredURL = '';
    let errors: string[] = [];
    let changes: CueChange[] = [];
    const listeners = new Map<TextTrack, EventListener>();
    const cue = (value: TextTrackCue): Cue => {
      const text = 'text' in value ? String((value as VTTCue).text) : '';
      return { Text: text.slice(0, 512), Start: value.startTime, End: value.endTime };
    };
    const bindTracks = () => {
      for (const track of Array.from(video.textTracks)) {
        if (listeners.has(track)) continue;
        const listener = () => {
          if (changes.length >= 128) changes.shift();
          changes.push({ At: performance.now(), MediaTime: video.currentTime, PresentedTime: presented,
            Language: track.language, Mode: track.mode, Active: Array.from(track.activeCues ?? []).map(cue) });
        };
        listeners.set(track, listener);
        track.addEventListener('cuechange', listener);
      }
    };
    video.textTracks.addEventListener('addtrack', bindTracks);
    const nextFrame: VideoFrameRequestCallback = (_now, metadata) => {
      frames++;
      presented = metadata.mediaTime;
      browserFrames = metadata.presentedFrames;
      video.requestVideoFrameCallback(nextFrame);
    };
    video.requestVideoFrameCallback(nextFrame);
    const api = async (target: string, method = 'GET'): Promise<Response> => {
      const resolved = new URL(target, input.BaseURL);
      if (resolved.origin !== input.BaseURL) throw new Error('Harness API escaped the owned origin.');
      return fetch(resolved.href, { method, headers: { 'X-Emby-Token': input.Token }, cache: 'no-store', signal: AbortSignal.timeout(12_000) });
    };
    const snapshot = (): Snapshot => {
      let pixel: number[] = [];
      if (video.readyState >= 2 && video.videoWidth > 0) {
        context.drawImage(video, 0, 0, 1, 1);
        pixel = Array.from(context.getImageData(0, 0, 1, 1).data).slice(0, 3);
      }
      const fragments = details?.fragments ?? [];
      return { MediaTime: video.currentTime, PresentedTime: presented, Frames: frames, Paused: video.paused,
        Seeking: video.seeking, ReadyState: video.readyState, Presentation: presentation,
        Width: video.videoWidth, Height: video.videoHeight, ErrorCode: video.error?.code ?? 0,
        SelectedTrack: hls?.subtitleTrack ?? -1,
        Tracks: (hls?.subtitleTracks ?? []).map((track, index) => ({ Index: index, Language: track.lang ?? '', Name: track.name ?? '' })),
        ActiveCues: Array.from(video.textTracks).filter((track) => track.mode === 'showing').flatMap((track) => Array.from(track.activeCues ?? []).map(cue)),
        CueChanges: changes.slice(), Pixel: pixel, LevelUpdates: updates, Sequence: details?.startSN ?? -1,
        Discontinuity: fragments.length ? fragments[fragments.length - 1].cc : -1,
        FragmentStart: fragments.length ? fragments[0].start : -1,
        FragmentEnd: fragments.length ? fragments[fragments.length - 1].start + fragments[fragments.length - 1].duration : -1,
        LiveSyncPosition: hls?.liveSyncPosition ?? null, LoadedFragments: loaded, SubtitleFragments: subtitles, FatalErrors: errors.slice() };
    };
    // Assigning currentTime and resolving play() do not establish that the
    // compositor has presented the requested frame. Fence each transition by
    // its own seeked event and three new, advancing frame callbacks. Never use
    // a previous seek's last callback to certify a newly assigned media clock.
    const settlePresentation = (kind: PresentationTransition['Kind'], target: number | null, action: () => Promise<void>): Promise<void> => {
      if (settling) return Promise.reject(new Error('A media presentation transition is already pending.'));
      settling = true;
      const record: PresentationTransition = {
        Kind: kind, Generation: generation, TargetSeconds: target, FramesBefore: frames, BrowserFramesBefore: browserFrames,
        StartedAt: performance.now(), WaitMilliseconds: 0, PlayResolved: false,
        Seeked: kind !== 'seek' && kind !== 'live', Completed: false, Failure: '', Frames: [], StableFrames: [],
      };
      presentation = record;
      return new Promise<void>((resolve, reject) => {
        let finished = false;
        let callback: number | undefined;
        const onSeeked = () => { record.Seeked = true; };
        const onError = () => finish('media-element-error');
        const finish = (failure = '') => {
          if (finished) return;
          finished = true;
          settling = false;
          record.WaitMilliseconds = performance.now() - record.StartedAt;
          record.Completed = failure === '';
          record.Failure = failure;
          clearTimeout(timer);
          if (callback !== undefined) video.cancelVideoFrameCallback(callback);
          video.removeEventListener('seeked', onSeeked);
          video.removeEventListener('error', onError);
          if (failure) reject(new Error(`The actual media presentation did not settle: ${failure}.`));
          else resolve();
        };
        const observe: VideoFrameRequestCallback = (now, metadata) => {
          if (finished) return;
          if (record.Generation !== generation) { finish('source-generation-changed'); return; }
          const frame: PresentationFrame = {
            FrameNumber: metadata.presentedFrames, CallbackAt: now, PresentedAt: metadata.presentationTime,
            MediaTime: video.currentTime, PresentedTime: metadata.mediaTime, ReadyState: video.readyState,
            Seeking: video.seeking, Paused: video.paused,
          };
          if (record.Frames.length >= 16) record.Frames.shift();
          record.Frames.push(frame);
          const previous = record.StableFrames.at(-1);
          const fresh = frame.FrameNumber > record.BrowserFramesBefore && frame.PresentedAt >= record.StartedAt
            && frames > record.FramesBefore && frame.ReadyState >= 2 && !frame.Seeking && !frame.Paused
            && record.PlayResolved && record.Seeked && Math.abs(frame.MediaTime - frame.PresentedTime) < .3;
          const advancing = !previous || frame.FrameNumber > previous.FrameNumber && frame.PresentedTime > previous.PresentedTime
            && frame.MediaTime > previous.MediaTime && frame.CallbackAt > previous.CallbackAt;
          if (!fresh || !advancing) record.StableFrames = [];
          if (fresh && (record.StableFrames.length > 0 || target === null || Math.abs(frame.PresentedTime - target) < .8)) {
            record.StableFrames.push(frame);
          }
          if (record.StableFrames.length === 3) { finish(); return; }
          callback = video.requestVideoFrameCallback(observe);
        };
        const timer = window.setTimeout(() => finish('new-frame-timeout'), 15_000);
        video.addEventListener('seeked', onSeeked);
        video.addEventListener('error', onError);
        Promise.resolve().then(action).then(() => {
          if (finished) return;
          record.PlayResolved = true;
          callback = video.requestVideoFrameCallback(observe);
        }, () => finish('play-request-failed'));
      });
    };
    const seek = async (seconds: number, kind: 'seek' | 'live' = 'seek') => {
      if (!Number.isFinite(seconds) || seconds < 0) throw new Error('Invalid media seek.');
      await settlePresentation(kind, seconds, async () => { video.currentTime = seconds; await video.play(); });
    };
    window.gobyPhase2 = {
      async load(kind, selection = -1, offsetTicks = 0, at = -1) {
        generation++;
        const current = generation;
        hls?.destroy();
        for (const [track, listener] of listeners) track.removeEventListener('cuechange', listener);
        listeners.clear();
        video.pause();
        video.removeAttribute('src');
        video.load();
        frames = 0; presented = -1; browserFrames = 0; presentation = null; updates = 0; loaded = 0; subtitles = 0; details = undefined; errors = []; changes = [];
        if (kind === 'Dynamic') expiredURL = '';
        const target = new URL(input[kind].PlaybackURL, input.BaseURL);
        if (kind === 'Finite') {
          target.searchParams.set('SubtitleStreamIndex', String(selection));
          target.searchParams.set('SubtitleOffsetTicks', String(offsetTicks));
          if (at >= 0) target.searchParams.set('StartTimeTicks', String(Math.round((at - input.Finite.TimelineOffsetSeconds) * 1e7)));
        }
        hls = new Hls({ enableWorker: false, debug: false, renderTextTracksNatively: true,
          maxBufferLength: 12, maxMaxBufferLength: 24, liveSyncDurationCount: 2, liveMaxLatencyDurationCount: 8,
          startPosition: at, xhrSetup(xhr: XMLHttpRequest, raw: string) {
            if (new URL(raw, input.BaseURL).origin !== input.BaseURL) throw new Error('HLS request escaped the owned origin.');
            xhr.setRequestHeader('X-Emby-Token', input.Token);
          } });
        hls.on(Hls.Events.LEVEL_UPDATED, (_event, data) => {
          if (current !== generation || !data.details || typeof data.details === 'string') return;
          details = data.details; updates++;
          if (kind === 'Dynamic' && !expiredURL && details.fragments.length) expiredURL = details.fragments[0].url;
        });
        hls.on(Hls.Events.FRAG_LOADED, () => { if (current === generation) loaded++; });
        hls.on(Hls.Events.SUBTITLE_FRAG_PROCESSED, () => { if (current === generation) subtitles++; });
        hls.on(Hls.Events.SUBTITLE_TRACKS_UPDATED, bindTracks);
        hls.on(Hls.Events.ERROR, (_event, data) => {
          if (current === generation && data.fatal && errors.length < 16) errors.push(`${data.type ?? 'unknown'}:${typeof data.details === 'string' ? data.details : 'unknown'}`);
        });
        hls.attachMedia(video);
        hls.loadSource(target.href);
        await settlePresentation('load', at >= 0 ? at : null, () => video.play());
      },
      snapshot,
      select(language) {
        const index = hls?.subtitleTracks.findIndex((track) => track.lang === language) ?? -1;
        if (!hls || index < 0) throw new Error('The requested language is absent from the real HLS engine.');
        hls.subtitleTrack = index;
      },
      off() { if (!hls) throw new Error('No HLS playback is active.'); hls.subtitleTrack = -1; },
      seek,
      pause() { video.pause(); },
      async resume() { await settlePresentation('resume', null, () => video.play()); },
      async live() {
        const position = hls?.liveSyncPosition;
        if (position == null || !Number.isFinite(position) || position < 0) throw new Error('No actual live synchronization point is available.');
        await seek(position, 'live');
      },
      async observe(kind) {
        const response = await api(input[kind].ObservePath);
        if (!response.ok) throw new Error(`Owned phase 2 observation failed with status ${response.status}.`);
        return response.json() as Promise<Observation>;
      },
      async control(action) {
        const response = await api(input.Dynamic[action], 'POST');
        if (!response.ok) throw new Error(`Owned phase 2 control failed with status ${response.status}.`);
      },
      async expired() {
        if (!expiredURL) throw new Error('No actual dynamic fragment URL was observed before eviction.');
        const response = await api(expiredURL);
        await response.body?.cancel();
        return response.status;
      },
      async stop(kind) {
        video.pause();
        hls?.destroy();
        hls = undefined;
        const response = await api(input[kind].StopPath, 'DELETE');
        if (!response.ok) throw new Error(`Owned phase 2 stop failed with status ${response.status}.`);
      },
    };
  }, fixture);
}

async function snapshot(page: Page): Promise<Snapshot> { return page.evaluate(() => window.gobyPhase2.snapshot()); }

function checkClock(value: Snapshot): void {
  expect(value.ErrorCode, 'the actual media element has no decode error').toBe(0);
  expect(value.Frames, 'the media element presented decoded frames').toBeGreaterThan(2);
  expect(value.Seeking, 'the current seek completed before inspecting playback').toBe(false);
  expect(value.ReadyState, 'the media element has a decoded current frame').toBeGreaterThanOrEqual(2);
  expect(value.Presentation?.Completed, 'the current media transition produced its own settled new frames').toBe(true);
  expect(value.Presentation?.StableFrames.length, 'three fresh advancing callbacks established presentation').toBe(3);
  expect(Math.abs(value.MediaTime - value.PresentedTime), 'presented frames follow the actual media clock').toBeLessThan(.3);
  expect(value.FatalErrors, 'the real HLS engine reported no fatal error').toEqual([]);
}

function checkCue(value: Snapshot, cue: CueFixture, delta: number): void {
  checkClock(value);
  const actual = value.ActiveCues.find((item) => item.Text.includes(cue.CueText));
  expect(Boolean(actual), 'the selected language produced a real active browser cue').toBe(true);
  if (!actual) throw new Error('The expected caption is absent.');
  expect(Math.abs(actual.Start - cue.StartSeconds - delta), 'cue start maps to independently supplied fixture time').toBeLessThan(.1);
  expect(Math.abs(actual.End - cue.EndSeconds - delta), 'cue end maps to independently supplied fixture time').toBeLessThan(.1);
  expect(value.MediaTime >= actual.Start && value.MediaTime < actual.End, 'caption is active on the actual presented interval').toBe(true);
  expect(value.CueChanges.some((event) => event.Language === cue.Language && event.Active.some((item) => item.Text.includes(cue.CueText))), 'a real cuechange event identified the selected language').toBe(true);
}

test('phase2 native HLS browser verifies subtitle views and bounded dynamic playback', async ({ page }) => {
  // Finish the test before the outer 180-second Node deadline so its finally
  // block, screenshot and private failure result can survive a stalled player.
  test.setTimeout(150_000);
  const { fixture, bundle } = loadFixture();
  let pageErrors = 0;
  let externalRequests = 0;
  let complete = false;
  let active: 'Finite' | 'Dynamic' | undefined;
  const finite: Record<string, unknown> = {};
  const dynamic: Record<string, unknown> = {};
  page.on('pageerror', () => { pageErrors++; });
  await page.route('**/*', async (route) => {
    const target = new URL(route.request().url());
    if (target.origin !== fixture.BaseURL) { externalRequests++; await route.abort(); return; }
    if (target.pathname === '/__phase2-browser-harness') {
      await route.fulfill({ contentType: 'text/html', body: '<!doctype html><html lang="en"><meta charset="utf-8"><title>Goby phase 2 HLS acceptance harness</title><h1>Private HLS media acceptance</h1><p>This test harness is not a consumer player or Emby Web.</p><video id="phase2-media" width="640" height="360" playsinline></video><canvas id="phase2-pixels" width="1" height="1" hidden></canvas></html>' });
    } else { await route.continue(); }
  });
  try {
    await page.goto(`${fixture.BaseURL}/__phase2-browser-harness`);
    await installHarness(page, fixture, bundle);
    active = 'Finite';
    const first = fixture.Finite.Tracks[0];
    const second = fixture.Finite.Tracks[1];
    const probe = fixture.Finite.ProbeSeconds + fixture.Finite.TimelineOffsetSeconds;
    await page.evaluate((track) => window.gobyPhase2.load('Finite', track), first.StreamIndex);
    await expect.poll(async () => (await snapshot(page)).Tracks.length, { timeout: 30_000 }).toBe(fixture.Finite.Tracks.length);
    await page.evaluate((language) => window.gobyPhase2.select(language), first.Language);
    await page.evaluate((seconds) => window.gobyPhase2.seek(seconds), probe);
    await expect.poll(async () => (await snapshot(page)).ActiveCues.some((cue) => cue.Text.includes(first.CueText)), { timeout: 20_000 }).toBe(true);
    const firstCue = await snapshot(page);
    checkCue(firstCue, first, fixture.Finite.TimelineOffsetSeconds);
    for (let channel = 0; channel < 3; channel++) expect(Math.abs(firstCue.Pixel[channel] - fixture.Finite.PixelRGB[channel]), 'caption time agrees with the independently known source color').toBeLessThan(25);
    const initial = await page.evaluate(() => window.gobyPhase2.observe('Finite'));
    expect(initial.ProducerIds.length, 'finite subtitle renditions share exactly one actual A/V producer').toBe(1);
    finite.First = firstCue;
    finite.Initial = initial;
    await page.evaluate((language) => window.gobyPhase2.select(language), second.Language);
    await page.evaluate((seconds) => window.gobyPhase2.seek(seconds), probe);
    await expect.poll(async () => (await snapshot(page)).ActiveCues.some((cue) => cue.Text.includes(second.CueText)), { timeout: 20_000 }).toBe(true);
    const secondCue = await snapshot(page);
    checkCue(secondCue, second, fixture.Finite.TimelineOffsetSeconds);
    expect(secondCue.ActiveCues.some((cue) => cue.Text.includes(first.CueText)), 'switching removes the previously selected language').toBe(false);
    finite.Switched = secondCue;
    await page.evaluate(() => window.gobyPhase2.off());
    await expect.poll(async () => (await snapshot(page)).ActiveCues.length).toBe(0);
    finite.Off = await snapshot(page);
    await page.evaluate(({ selection, offset, at }) => window.gobyPhase2.load('Finite', selection, offset, at),
      { selection: second.StreamIndex, offset: fixture.Finite.OffsetTicks, at: probe });
    await expect.poll(async () => (await snapshot(page)).Tracks.length, { timeout: 30_000 }).toBe(fixture.Finite.Tracks.length);
    await page.evaluate((language) => window.gobyPhase2.select(language), second.Language);
    await page.evaluate((seconds) => window.gobyPhase2.seek(seconds), probe);
    await expect.poll(async () => (await snapshot(page)).ActiveCues.some((cue) => cue.Text.includes(second.CueText)), { timeout: 20_000 }).toBe(true);
    const offsetCue = await snapshot(page);
    checkCue(offsetCue, second, fixture.Finite.TimelineOffsetSeconds + fixture.Finite.OffsetTicks / 1e7);
    const finalFinite = await page.evaluate(() => window.gobyPhase2.observe('Finite'));
    expect(finalFinite.ProducerIds, 'switch, off and caption delay retain the actual original A/V producer').toEqual(initial.ProducerIds);
    finite.Offset = offsetCue;
    finite.Final = finalFinite;
    await page.evaluate(() => window.gobyPhase2.stop('Finite'));
    active = undefined;

    active = 'Dynamic';
    await page.evaluate(() => window.gobyPhase2.load('Dynamic'));
    await expect.poll(async () => (await snapshot(page)).Tracks.some((track) => track.Language === fixture.Dynamic.SubtitleLanguage), { timeout: 30_000 }).toBe(true);
    await page.evaluate((language) => window.gobyPhase2.select(language), fixture.Dynamic.SubtitleLanguage);
    await expect.poll(async () => (await snapshot(page)).ActiveCues.some((cue) => cue.Text.startsWith(fixture.Dynamic.CuePrefix)), { timeout: 30_000 }).toBe(true);
    await expect.poll(async () => (await snapshot(page)).Frames, { timeout: 30_000 }).toBeGreaterThan(5);
    await expect.poll(async () => (await snapshot(page)).FragmentEnd - (await snapshot(page)).FragmentStart, { timeout: 30_000 }).toBeGreaterThan(4);
    const beforePause = await snapshot(page);
    checkClock(beforePause);
    expect(beforePause.CueChanges.some((event) => event.Language === fixture.Dynamic.SubtitleLanguage
      && event.Active.some((cue) => cue.Text.startsWith(fixture.Dynamic.CuePrefix) && event.MediaTime >= cue.Start - .15 && event.MediaTime <= cue.End + .15)),
    'dynamic subtitles produce actual cuechange events on the current media interval').toBe(true);
    const observedBefore = await page.evaluate(() => window.gobyPhase2.observe('Dynamic'));
    dynamic.BeforePause = beforePause;
    dynamic.Initial = observedBefore;
    await page.evaluate(() => window.gobyPhase2.pause());
    const paused = await snapshot(page);
    await expect.poll(async () => (await page.evaluate(() => window.gobyPhase2.observe('Dynamic'))).WindowEndTicks,
      { timeout: Math.ceil(fixture.Dynamic.PauseSeconds * 1000) + 15_000, intervals: [250, 500, 1000] }).toBeGreaterThan(observedBefore.WindowEndTicks + fixture.Dynamic.PauseSeconds * 1e7);
    const receivedWhilePaused = await snapshot(page);
    expect(receivedWhilePaused.Paused).toBe(true);
    expect(Math.abs(receivedWhilePaused.MediaTime - paused.MediaTime), 'pause holds the actual media position while the server continues ingesting').toBeLessThan(.15);
    expect(receivedWhilePaused.LevelUpdates, 'the HLS engine continues receiving the rolling manifest while paused').toBeGreaterThan(paused.LevelUpdates);
    dynamic.Paused = receivedWhilePaused;
    const sameProducer = await page.evaluate(() => window.gobyPhase2.observe('Dynamic'));
    expect(sameProducer.ProducerIds, 'pause does not restart dynamic ingestion').toEqual(observedBefore.ProducerIds);
    await page.evaluate(() => window.gobyPhase2.resume());
    await expect.poll(async () => (await snapshot(page)).MediaTime, { timeout: 15_000 }).toBeGreaterThan(paused.MediaTime + .3);
    const resumed = await snapshot(page);
    checkClock(resumed);
    dynamic.Resumed = resumed;
    // Resume can legitimately consume bytes that the client buffered before
    // the server evicted them. Establish a fresh retained-window origin before
    // testing a new server-window replay, without extending retention.
    await page.evaluate(() => window.gobyPhase2.live());
    await expect.poll(async () => {
      const value = await snapshot(page);
      return Math.abs(value.MediaTime - (value.LiveSyncPosition ?? -100)) < 1.5
        && value.MediaTime > value.FragmentStart + 1.25 && value.MediaTime < value.FragmentEnd;
    }, { timeout: 15_000 }).toBe(true);
    const replayOrigin = await snapshot(page);
    checkClock(replayOrigin);
    dynamic.ReplayOrigin = replayOrigin;
    expect((await page.evaluate(() => window.gobyPhase2.observe('Dynamic'))).ProducerIds,
      'establishing a current retained replay origin keeps the original producer').toEqual(observedBefore.ProducerIds);
    const back = replayOrigin.MediaTime - 1;
    expect(back, 'the one-second rewind begins inside the current retained window').toBeGreaterThan(replayOrigin.FragmentStart + .25);
    expect(replayOrigin.MediaTime - back, 'replay actually moves backward within the current retained window').toBeGreaterThan(.5);
    expect(back, 'replay stays before the actual retained live edge').toBeLessThan(replayOrigin.FragmentEnd);
    await page.evaluate((seconds) => window.gobyPhase2.seek(seconds), back);
    await expect.poll(async () => Math.abs((await snapshot(page)).MediaTime - back), { timeout: 15_000 }).toBeLessThan(.8);
    dynamic.Replay = await snapshot(page);
    checkClock(dynamic.Replay as Snapshot);
    await page.evaluate(() => window.gobyPhase2.live());
    await expect.poll(async () => { const value = await snapshot(page); return Math.abs(value.MediaTime - (value.LiveSyncPosition ?? -100)); }, { timeout: 15_000 }).toBeLessThan(1.5);
    dynamic.Live = await snapshot(page);
    checkClock(dynamic.Live as Snapshot);
    expect((await page.evaluate(() => window.gobyPhase2.observe('Dynamic'))).ProducerIds, 'replay and live-edge return share the same producer').toEqual(observedBefore.ProducerIds);
    const beforeDisconnect = await snapshot(page);
    await page.evaluate(() => window.gobyPhase2.control('DisconnectPath'));
    await page.evaluate(() => window.gobyPhase2.control('ReconnectPath'));
    await expect.poll(async () => (await page.evaluate(() => window.gobyPhase2.observe('Dynamic'))).Epoch, { timeout: 30_000 }).toBeGreaterThan(observedBefore.Epoch);
    await expect.poll(async () => (await snapshot(page)).Discontinuity, { timeout: 30_000 }).toBeGreaterThan(beforeDisconnect.Discontinuity);
    await page.evaluate(() => window.gobyPhase2.live());
    await expect.poll(async () => (await snapshot(page)).Frames, { timeout: 15_000 }).toBeGreaterThan(beforeDisconnect.Frames + 3);
    await expect.poll(async () => (await snapshot(page)).CueChanges.some((event) => event.At > (beforeDisconnect.CueChanges.at(-1)?.At ?? 0)
      && event.Language === fixture.Dynamic.SubtitleLanguage && event.Active.some((cue) => cue.Text.startsWith(fixture.Dynamic.CuePrefix)
        && event.MediaTime >= cue.Start - .15 && event.MediaTime <= cue.End + .15)), { timeout: 20_000 }).toBe(true);
    const reconnected = await snapshot(page);
    checkClock(reconnected);
    dynamic.Reconnected = reconnected;
    await expect.poll(() => page.evaluate(() => window.gobyPhase2.expired()), { timeout: 40_000, intervals: [500, 1000] }).toBe(fixture.Dynamic.ExpectedExpiredStatus);
    dynamic.ExpiredStatus = fixture.Dynamic.ExpectedExpiredStatus;
    dynamic.Final = await page.evaluate(() => window.gobyPhase2.observe('Dynamic'));
    await page.evaluate(() => window.gobyPhase2.stop('Dynamic'));
    active = undefined;
    expect(pageErrors).toBe(0);
    expect(externalRequests).toBe(0);
    complete = true;
  } finally {
    let cleanupFailed = false;
    let lastSnapshot: Snapshot | null = null;
    try { lastSnapshot = await snapshot(page); } catch { /* Preserve earlier evidence when the page has already closed. */ }
    if (active) {
      try { await page.evaluate((kind) => window.gobyPhase2.stop(kind), active); }
      catch { cleanupFailed = true; }
    }
    writePrivateResult(fixture, { Marker: 'goby-phase2-hls-browser-result-v1', RunId: fixture.RunId, Complete: complete && !cleanupFailed,
      PlayerKind: 'native-html-video-with-retained-official-client-hls-engine', OriginalEmbyWebExercised: false,
      HlsVersion: '1.6.0-beta.2', HlsBundleSHA256: bundleSHA256, PageErrors: pageErrors, ExternalRequests: externalRequests,
      CleanupFailed: cleanupFailed, LastSnapshot: lastSnapshot, Finite: finite, Dynamic: dynamic });
  }
});
