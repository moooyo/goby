import { randomBytes } from 'node:crypto';
import { closeSync, constants, existsSync, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

interface Fixture {
  Marker: string;
  RunId: string;
  BaseURL: string;
  Token: string;
  UserId: string;
  SessionId: string;
  DeviceId: string;
  ItemId: string;
  SourceId: string;
  DurationTicks: number;
  ArtifactsDir: string;
  ResultPath: string;
}

interface Plan {
  StartTicks: number;
  CopyTimestamps: boolean;
  VideoCodec: string;
  AudioCodec: string;
  VideoCopyCodec: string;
  HasCopySeekProof: boolean;
  ProducerStates: string[];
}

interface Observation {
  State: string;
  PositionTicks: number;
  UserPositionTicks: number;
  Sessions: number;
  ActiveJobs: number;
  StreamSlots: number;
  Plans: Plan[];
}

interface ClockFrame {
  FrameNumber: number;
  AtMilliseconds: number;
  FrameTime: number;
  MediaTime: number;
  ReadyState: number;
  Paused: boolean;
  Seeking: boolean;
}

interface StartupEvidence {
  Frames: ClockFrame[];
  Events: { Name: string; AtMilliseconds: number; MediaTime: number; PresentedFrameTime: number }[];
  StableFrames: ClockFrame[];
  ClockReady: boolean;
  WaitMilliseconds: number;
  Report: { MediaTime: number; PresentedFrameTime: number; SourceTime: number; DisplayedTime: number; PositionTicks: number } | null;
  Observation: Observation | null;
}

interface Sample {
  PlayId: string;
  RequestedTicks: number;
  AlignedTicks: number;
  CopyTimestamps: boolean;
  VideoCodec: string;
  OffsetSeconds: number;
  FirstFrameTime: number;
  FirstClockTime: number;
  MediaTime: number;
  SourceTime: number;
  DisplayedTime: number;
  ReportedTicks: number;
  FrameCount: number;
  PresentedFrameTime: number;
  Width: number;
  Height: number;
  Paused: boolean;
  ReadyState: number;
  ErrorCode: number;
  Pixel: number[];
  Startup: StartupEvidence;
}

interface CaseResult {
  Name: string;
  PlayId: string;
  RequestedTicks: number;
  AlignedTicks: number;
  CopyTimestamps: boolean;
  VideoCodec: string;
  FirstFrameTime: number;
  FirstClockTime: number;
  StoppedTicks: number;
  Startup: StartupEvidence;
  Checks: Record<string, boolean>;
  Samples: Sample[];
  Observations: Observation[];
}

interface Harness {
  load(seconds: number, codec: 'copy' | 'av1'): Promise<void>;
  snapshot(): Sample;
  observe(playId?: string): Promise<Observation>;
  retirement(playId: string): Observation;
  progress(): Promise<number>;
  pending(): Promise<void>;
  shutdown(): Promise<void>;
}

declare global { interface Window { gobyAMDMedia: Harness } }

function privateDirectory(directory: string): void {
  const stat = lstatSync(directory);
  if (!path.isAbsolute(directory) || directory !== path.resolve(directory) || realpathSync(directory) !== directory
    || !stat.isDirectory() || stat.isSymbolicLink() || stat.uid !== process.getuid?.() || (stat.mode & 0o777) !== 0o700) {
    throw new Error('Native media browser artifacts require an owned private directory.');
  }
}

function loadFixture(): Fixture {
  const filename = process.env.GOBY_AMD_MEDIA_BROWSER_CONTEXT;
  if (process.platform !== 'linux' || !filename || !path.isAbsolute(filename) || filename !== path.resolve(filename)) {
    throw new Error('Native media browser acceptance requires its isolated Linux fixture.');
  }
  privateDirectory(path.dirname(filename));
  const descriptor = openSync(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  let fixture: Fixture;
  try {
    const stat = fstatSync(descriptor);
    if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600
      || stat.size < 1 || stat.size > 32 * 1024 || realpathSync(`/proc/self/fd/${descriptor}`) !== filename) {
      throw new Error('The native media fixture must remain a bounded private regular file.');
    }
    try { fixture = JSON.parse(readFileSync(descriptor, 'utf8')) as Fixture; }
    catch { throw new Error('The native media fixture is not valid JSON.'); }
  } finally { closeSync(descriptor); }
  let origin: URL;
  try { origin = new URL(fixture.BaseURL); } catch { throw new Error('The native media origin is invalid.'); }
  if (fixture.Marker !== 'goby-amd-native-browser-fixture-v1'
    || !/^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/.test(fixture.RunId) || fixture.RunId !== process.env.GOBY_AMD_MEDIA_BROWSER_RUN_ID
    || origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || !origin.port
    || ['5432', '15432', '18096', '18097'].includes(origin.port)
    || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash
    || ![fixture.Token, fixture.SessionId, fixture.DeviceId, fixture.SourceId].every((value) => typeof value === 'string' && value.length > 0 && value.length <= 512)
    || !/^[0-9a-f]{32}$/.test(fixture.UserId) || !/^[0-9a-f]{32}$/.test(fixture.ItemId)
    || fixture.DurationTicks < 120 * 1e7 || fixture.DurationTicks > 140 * 1e7
    || typeof fixture.ResultPath !== 'string' || !path.isAbsolute(fixture.ResultPath) || fixture.ResultPath !== path.resolve(fixture.ResultPath)
    || path.dirname(fixture.ResultPath) !== fixture.ArtifactsDir || path.extname(fixture.ResultPath) !== '.json'
    || fixture.ResultPath === filename || fixture.ArtifactsDir !== path.dirname(filename)) {
    throw new Error('The native media fixture does not describe an owned disposable run.');
  }
  privateDirectory(fixture.ArtifactsDir);
  if (existsSync(fixture.ResultPath)) throw new Error('The native media result already exists.');
  fixture.BaseURL = origin.origin;
  return fixture;
}

function writePrivateJSON(filename: string, value: unknown, secret: string): void {
  const encoded = `${JSON.stringify(value, null, 2)}\n`;
  if (encoded.includes(secret)) throw new Error('Authentication tokens cannot be written to browser results.');
  if (existsSync(filename)) throw new Error('The browser result already exists.');
  const temporary = path.join(path.dirname(filename), `.${path.basename(filename)}.${randomBytes(8).toString('hex')}.tmp`);
  const descriptor = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    try {
      const stat = fstatSync(descriptor);
      if (!stat.isFile() || stat.uid !== process.getuid?.() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600) throw new Error('Browser results must remain private.');
      writeFileSync(descriptor, encoded, 'utf8');
      fsyncSync(descriptor);
    } finally { closeSync(descriptor); }
    if (existsSync(filename)) throw new Error('The result appeared before publication.');
    renameSync(temporary, filename);
  } finally { if (existsSync(temporary)) unlinkSync(temporary); }
}

async function installHarness(page: Page, fixture: Fixture): Promise<void> {
  await page.evaluate((input) => {
    const video = document.querySelector<HTMLVideoElement>('#media')!;
    const display = document.querySelector<HTMLOutputElement>('#position')!;
    const canvas = document.querySelector<HTMLCanvasElement>('#pixels')!;
    const pixels = canvas.getContext('2d', { willReadFrequently: true })!;
    video.muted = true;
    video.preload = 'auto';
    let active: { play: string; requested: number; aligned: number; copy: boolean; codec: string; offset: number } | undefined;
    let lastReport = 0;
    let frames = 0;
    let firstFrame = -1;
    let firstClock = -1;
    let presented = -1;
    let generation = 0;
    let retired = false;
    let work: Promise<void> = Promise.resolve();
    let failed = false;
    let clockReady = false;
    let playResolved = false;
    let playingObserved = false;
    let clockEpoch = performance.now();
    let startup: StartupEvidence = { Frames: [], Events: [], StableFrames: [], ClockReady: false, WaitMilliseconds: -1, Report: null, Observation: null };
    const retiredStates: Record<string, Observation> = {};
    const api = async (pathname: string, method = 'GET', body?: unknown) => {
      const response = await fetch(pathname, { method, headers: { 'X-Emby-Token': input.Token, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, body: body === undefined ? undefined : JSON.stringify(body) });
      if (!response.ok) throw new Error(`Owned media API failed with status ${response.status}.`);
      return response.status === 204 ? undefined : response.json();
    };
    const sourceTime = () => video.currentTime + (active?.offset ?? 0);
    const render = () => { display.value = sourceTime().toFixed(3); };
    video.addEventListener('timeupdate', render);
    for (const name of ['loadstart', 'loadedmetadata', 'loadeddata', 'canplay', 'play', 'playing', 'timeupdate', 'seeking', 'seeked', 'waiting', 'error']) {
      video.addEventListener(name, () => {
        if (!active || retired) return;
        if (name === 'playing') playingObserved = true;
        if (startup.Events.length < 32) startup.Events.push({ Name: name, AtMilliseconds: performance.now() - clockEpoch, MediaTime: video.currentTime, PresentedFrameTime: presented });
      });
    }
    const report = async (event: 'Started' | 'Progress' | 'Stopped') => {
      if (!active) throw new Error('A media report requires an active owned play.');
      if (!clockReady) throw new Error('Playback reports require a settled real media clock.');
      // Read the clock once. The UI, mapped source time, and report must be
      // derived from this exact observation, never a requested/aligned value.
      const current = video.currentTime;
      if (!Number.isFinite(current) || Math.abs(current - presented) > .15) throw new Error('The real media clock diverged from its presented frame before reporting.');
      const mapped = current + active.offset;
      const reportTicks = Math.round(mapped * 1e7);
      lastReport = reportTicks;
      display.value = mapped.toFixed(3);
      if (event === 'Started') startup.Report = { MediaTime: current, PresentedFrameTime: presented, SourceTime: mapped, DisplayedTime: Number(display.value), PositionTicks: reportTicks };
      await api(`/emby/Sessions/Playing${event === 'Started' ? '' : `/${event}`}`, 'POST', {
        PlaySessionId: active.play, ItemId: input.ItemId, MediaSourceId: input.SourceId, SessionId: input.SessionId,
        PositionTicks: reportTicks, IsPaused: video.paused, CanSeek: true, PlayMethod: active.codec === 'copy' ? 'DirectStream' : 'Transcode', EventName: 'TimeUpdate',
      });
      return reportTicks;
    };
    const observe = async (playId?: string): Promise<Observation> => {
      if (!active) throw new Error('An observation requires an owned play.');
      return api(`/__amd-media-observe?PlaySessionId=${encodeURIComponent(playId ?? active.play)}`) as Promise<Observation>;
    };
    const stop = async () => {
      if (!active || retired) return;
      video.pause();
      if (clockReady) await report('Stopped');
      else {
        // Failed startup must still retire its owned preparation without
        // writing the uninitialized media-element position into user state.
        await api('/emby/Sessions/Playing/Stopped', 'POST', { PlaySessionId: active.play, ItemId: input.ItemId, MediaSourceId: input.SourceId });
      }
      await api(`/emby/Videos/ActiveEncodings?DeviceId=${encodeURIComponent(input.DeviceId)}&PlaySessionId=${encodeURIComponent(active.play)}`, 'DELETE');
      // Preserve the old source's actual resume row before the next source's
      // Started report legitimately changes that same per-item user row.
      retiredStates[active.play] = await observe();
      retired = true;
      generation++;
      video.removeAttribute('src');
      video.load();
    };
    const load = async (seconds: number, codec: 'copy' | 'av1') => {
      if (active) await stop();
      active = undefined;
      const profile = { Type: 'Video', Container: 'mp4', Protocol: 'http', Context: 'Streaming', VideoCodec: codec === 'copy' ? 'h264' : 'av1', AudioCodec: 'aac', ...(codec === 'av1' ? { MaxWidth: 96, MaxHeight: 54 } : {}) };
      const prepared = await api(`/emby/Items/${input.ItemId}/PlaybackInfo`, 'POST', {
        EnableDirectPlay: false, EnableDirectStream: false, EnableTranscoding: true, StartTimeTicks: Math.round(seconds * 1e7),
        ...(codec === 'av1' ? { AllowVideoStreamCopy: false, AllowAudioStreamCopy: false } : {}),
        DeviceProfile: { TranscodingProfiles: [profile] },
      }) as { PlaySessionId: string; ErrorCode?: string; MediaSources: { Id: string; TranscodingUrl: string; TranscodingSubProtocol: string }[] };
      if (prepared.ErrorCode || !prepared.PlaySessionId?.startsWith('play_') || prepared.MediaSources.length !== 1
        || prepared.MediaSources[0].Id !== input.SourceId || prepared.MediaSources[0].TranscodingSubProtocol !== 'http') {
        throw new Error('The real media negotiation did not return one owned progressive source.');
      }
      const url = new URL(prepared.MediaSources[0].TranscodingUrl, location.origin);
      if (url.origin !== location.origin || url.pathname !== `/emby/Videos/${input.ItemId}/stream.mp4`
        || url.searchParams.get('api_key') !== input.Token || url.searchParams.get('PlaySessionId') !== prepared.PlaySessionId) {
        throw new Error('The negotiated media URL escaped its owned source or play.');
      }
      const copy = url.searchParams.get('CopyTimestamps') === 'true';
      // This is the documented native harness contract. It mirrors the inspected
      // client offset rule but does not execute or impersonate Emby Web modules.
      active = { play: prepared.PlaySessionId, requested: Math.round(seconds * 1e7), aligned: Number(url.searchParams.get('StartTimeTicks')), copy,
        codec: url.searchParams.get('VideoCodec') ?? '', offset: copy ? 0 : seconds };
      retired = false;
      firstFrame = -1;
      firstClock = -1;
      frames = 0;
      presented = -1;
      lastReport = 0;
      clockReady = false;
      playResolved = false;
      playingObserved = false;
      clockEpoch = performance.now();
      startup = { Frames: [], Events: [], StableFrames: [], ClockReady: false, WaitMilliseconds: -1, Report: null, Observation: null };
      const currentGeneration = ++generation;
      let resolveStableClock: () => void = () => undefined;
      let rejectStableClock: (reason: Error) => void = () => undefined;
      const stableClock = new Promise<void>((resolve, reject) => { resolveStableClock = resolve; rejectStableClock = reject; });
      let consecutive: ClockFrame[] = [];
      const frame = (_now: number, metadata: VideoFrameCallbackMetadata) => {
        if (currentGeneration !== generation) return;
        frames++;
        presented = metadata.mediaTime;
        const current = video.currentTime;
        if (firstFrame < 0) { firstFrame = metadata.mediaTime; firstClock = current; }
        const observed: ClockFrame = { FrameNumber: frames, AtMilliseconds: performance.now() - clockEpoch,
          FrameTime: metadata.mediaTime, MediaTime: current, ReadyState: video.readyState, Paused: video.paused, Seeking: video.seeking };
        if (!clockReady) {
          if (startup.Frames.length < 32) startup.Frames.push(observed);
          const previous = consecutive.at(-1);
          const synchronized = playResolved && playingObserved && !video.paused && !video.seeking && !video.error
            && video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && Number.isFinite(current) && current >= 0 && Number.isFinite(metadata.mediaTime) && metadata.mediaTime >= 0
            && Math.abs(current - metadata.mediaTime) <= .1;
          if (!synchronized) consecutive = [];
          else {
            if (previous && (metadata.mediaTime <= previous.FrameTime || current <= previous.MediaTime)) consecutive = [];
            consecutive.push(observed);
            if (consecutive.length === 3) {
              startup.StableFrames = consecutive.slice();
              startup.ClockReady = true;
              clockReady = true;
              resolveStableClock();
            }
          }
        }
        video.requestVideoFrameCallback(frame);
      };
      video.requestVideoFrameCallback(frame);
      video.src = url.href;
      await video.play();
      playResolved = true;
      const waitingAt = performance.now();
      const clockDeadline = window.setTimeout(() => rejectStableClock(new Error('The browser media clock did not settle within three seconds of playback.')), 3000);
      try { await stableClock; }
      finally { window.clearTimeout(clockDeadline); startup.WaitMilliseconds = performance.now() - waitingAt; }
      await report('Started');
      // Observe Started before any Progress can overwrite an incorrect value.
      startup.Observation = await observe();
    };
    const snapshot = (): Sample => {
      if (!active) throw new Error('A media snapshot requires an owned play.');
      let pixel: number[] = [];
      if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
        pixels.drawImage(video, 0, 0, 1, 1);
        pixel = Array.from(pixels.getImageData(0, 0, 1, 1).data).slice(0, 3);
      }
      return { PlayId: active.play, RequestedTicks: active.requested, AlignedTicks: active.aligned, CopyTimestamps: active.copy, VideoCodec: active.codec,
        OffsetSeconds: active.offset, FirstFrameTime: firstFrame, FirstClockTime: firstClock, MediaTime: video.currentTime, SourceTime: sourceTime(),
        DisplayedTime: Number(display.value), ReportedTicks: lastReport, FrameCount: frames, PresentedFrameTime: presented,
        Width: video.videoWidth, Height: video.videoHeight, Paused: video.paused, ReadyState: video.readyState, ErrorCode: video.error?.code ?? 0, Pixel: pixel, Startup: startup };
    };
    const action = (id: string, fn: () => Promise<unknown>) => document.querySelector(`#${id}`)!.addEventListener('click', () => {
      work = fn().then(() => undefined, () => { failed = true; });
    });
    action('pause', async () => { video.pause(); await report('Progress'); });
    action('resume', async () => { await video.play(); await report('Progress'); });
    action('change', async () => { await load(Number(document.querySelector<HTMLInputElement>('#seek')!.value), 'copy'); });
    action('stop', stop);
    window.gobyAMDMedia = { load, snapshot, observe, retirement: (id) => {
      if (!retiredStates[id]) throw new Error('No actual stop observation exists for that owned play.');
      return retiredStates[id];
    }, progress: () => report('Progress'),
      pending: async () => { await work; if (failed) throw new Error('A native player action failed.'); },
      shutdown: async () => { if (video.currentSrc) await stop(); },
    };
  }, { Token: fixture.Token, ItemId: fixture.ItemId, SourceId: fixture.SourceId, SessionId: fixture.SessionId, DeviceId: fixture.DeviceId });
}

async function action(page: Page, id: string): Promise<void> {
  await page.locator(`#${id}`).click();
  await page.evaluate(() => window.gobyAMDMedia.pending());
}

async function completeRetirement(page: Page, result: CaseResult, globallyIdle: boolean): Promise<void> {
  const stopped = await page.evaluate((id) => window.gobyAMDMedia.retirement(id), result.PlayId);
  result.Observations.push(stopped);
  await expect.poll(async () => {
    const state = await page.evaluate((id) => window.gobyAMDMedia.observe(id), result.PlayId);
    return state.Sessions === 0 && state.ActiveJobs === 0 && (!globallyIdle || state.StreamSlots === 0);
  }, { timeout: 15_000, intervals: [50, 100, 250] }).toBe(true);
  expect(stopped.State).toBe('Stopped');
  expect(stopped.PositionTicks).toBe(result.StoppedTicks);
  expect(stopped.UserPositionTicks).toBe(result.StoppedTicks);
  result.Checks.StopRetired = true;
  result.Checks.ResumePositionPersisted = true;
}

async function runCase(page: Page, name: string, requested: number, aligned: number, encoded = false, stopAtEnd = true): Promise<CaseResult> {
  await expect.poll(async () => (await page.evaluate(() => window.gobyAMDMedia.snapshot())).FrameCount, { timeout: 30_000, intervals: [25, 50, 100] }).toBeGreaterThanOrEqual(3);
  await action(page, 'pause');
  const sample = await page.evaluate(() => window.gobyAMDMedia.snapshot());
  const result: CaseResult = { Name: name, PlayId: sample.PlayId, RequestedTicks: sample.RequestedTicks, AlignedTicks: sample.AlignedTicks,
    CopyTimestamps: sample.CopyTimestamps, VideoCodec: sample.VideoCodec, FirstFrameTime: sample.FirstFrameTime, FirstClockTime: sample.FirstClockTime,
    StoppedTicks: 0, Startup: sample.Startup, Checks: {}, Samples: [sample], Observations: [] };
  expect(sample.ErrorCode).toBe(0);
  expect(sample.ReadyState).toBeGreaterThanOrEqual(2);
  expect(sample.Width).toBe(encoded ? 96 : 160);
  expect(sample.Height).toBe(encoded ? 54 : 90);
  expect(sample.FrameCount).toBeGreaterThanOrEqual(3);
  result.Checks.DecodedFrames = true;
  expect(sample.RequestedTicks).toBe(Math.round(requested * 1e7));
  expect(sample.AlignedTicks).toBe(Math.round(aligned * 1e7));
  expect(sample.CopyTimestamps).toBe(!encoded);
  expect(sample.VideoCodec).toBe(encoded ? 'av1' : 'copy');
  expect(sample.OffsetSeconds).toBe(encoded ? requested : 0);
  const first = encoded ? 0 : aligned;
  expect(Math.abs(sample.FirstFrameTime - first), 'The first actual presented frame must retain the agreed media origin.').toBeLessThanOrEqual(.1);
  // Preserve the raw first callback, including a transient uninitialized clock.
  // Acceptance instead requires observed clock convergence before Started.
  expect(sample.Startup.Frames[0].FrameTime).toBe(sample.FirstFrameTime);
  expect(sample.Startup.Frames[0].MediaTime).toBe(sample.FirstClockTime);
  expect(sample.Startup.ClockReady).toBe(true);
  expect(sample.Startup.WaitMilliseconds).toBeGreaterThanOrEqual(0);
  expect(sample.Startup.WaitMilliseconds).toBeLessThanOrEqual(3000);
  expect(sample.Startup.StableFrames.length).toBe(3);
  sample.Startup.StableFrames.forEach((frame, index, sequence) => {
    expect(frame.ReadyState).toBeGreaterThanOrEqual(2);
    expect(frame.Paused).toBe(false);
    expect(frame.Seeking).toBe(false);
    expect(Math.abs(frame.MediaTime - frame.FrameTime)).toBeLessThanOrEqual(.1);
    if (index > 0) {
      expect(frame.FrameNumber).toBe(sequence[index - 1].FrameNumber + 1);
      expect(frame.FrameTime).toBeGreaterThan(sequence[index - 1].FrameTime);
      expect(frame.MediaTime).toBeGreaterThan(sequence[index - 1].MediaTime);
    }
  });
  result.Checks.StartupClockSettled = true;
  const started = sample.Startup.Report!;
  expect(started).not.toBeNull();
  expect(started.MediaTime).toBeGreaterThanOrEqual(sample.Startup.StableFrames[2].MediaTime);
  expect(started.MediaTime).toBeGreaterThanOrEqual(sample.FirstFrameTime - .1);
  expect(Math.abs(started.MediaTime - started.PresentedFrameTime)).toBeLessThanOrEqual(.15);
  expect(Math.abs(started.SourceTime - started.MediaTime - sample.OffsetSeconds)).toBeLessThan(1e-6);
  expect(started.PositionTicks).toBe(Math.round(started.SourceTime * 1e7));
  expect(Math.abs(started.DisplayedTime - started.SourceTime)).toBeLessThan(.003);
  const startedState = sample.Startup.Observation!;
  expect(startedState).not.toBeNull();
  expect(startedState.State).toBe('Playing');
  expect(startedState.PositionTicks).toBe(started.PositionTicks);
  expect(startedState.UserPositionTicks).toBe(started.PositionTicks);
  result.Observations.push(startedState);
  result.Checks.StartedPositionPersisted = true;
  expect(Math.abs(sample.MediaTime - sample.PresentedFrameTime)).toBeLessThan(.15);
  expect(Math.abs(sample.SourceTime - sample.MediaTime - sample.OffsetSeconds)).toBeLessThan(1e-6);
  result.Checks.ClockMapping = true;
  const expectedColor = sample.SourceTime < 3 ? [255, 0, 0] : sample.SourceTime < 6 ? [0, 128, 0] : sample.SourceTime < 9 ? [0, 0, 255] : [255, 255, 0];
  expect(sample.Pixel.length).toBe(3);
  sample.Pixel.forEach((value, channel) => expect(Math.abs(value - expectedColor[channel])).toBeLessThanOrEqual(24));
  result.Checks.SourceColor = true;
  expect(Math.abs(sample.DisplayedTime - sample.SourceTime)).toBeLessThan(.003);
  await expect(page.locator('#position')).toHaveText(sample.SourceTime.toFixed(3));
  result.Checks.DisplayedPosition = true;
  expect(Math.abs(sample.ReportedTicks / 1e7 - sample.SourceTime)).toBeLessThan(.003);
  const paused = await page.evaluate(() => window.gobyAMDMedia.observe());
  result.Observations.push(paused);
  expect(paused.State).toBe('Paused');
  expect(paused.PositionTicks).toBe(sample.ReportedTicks);
  expect(paused.UserPositionTicks).toBe(sample.ReportedTicks);
  expect(paused.Plans.length).toBe(1);
  expect(paused.Plans[0].StartTicks).toBe(sample.AlignedTicks);
  expect(paused.Plans[0].CopyTimestamps).toBe(!encoded);
  expect(paused.Plans[0].VideoCodec).toBe(encoded ? 'av1' : 'copy');
  if (!encoded) {
    expect(paused.Plans[0].VideoCopyCodec).toBe('h264');
    expect(paused.Plans[0].HasCopySeekProof).toBe(true);
  }
  expect(paused.Plans[0].ProducerStates.length).toBe(1);
  expect(['running', 'completed']).toContain(paused.Plans[0].ProducerStates[0]);
  result.Checks.PersistedProgress = true;
  // This wait observes a deliberately paused media element; it is not a
  // substitute for waiting on media readiness or successful playback.
  await page.waitForTimeout(250);
  const still = await page.evaluate(() => window.gobyAMDMedia.snapshot());
  expect(still.Paused).toBe(true);
  expect(Math.abs(still.MediaTime - sample.MediaTime)).toBeLessThan(.015);
  result.Samples.push(still);
  result.Checks.PauseStable = true;
  await action(page, 'resume');
  await expect.poll(async () => (await page.evaluate(() => window.gobyAMDMedia.snapshot())).MediaTime, { timeout: 10_000, intervals: [25, 50] }).toBeGreaterThan(sample.MediaTime + .3);
  await action(page, 'pause');
  const resumed = await page.evaluate(() => window.gobyAMDMedia.snapshot());
  expect(resumed.FrameCount).toBeGreaterThan(still.FrameCount);
  result.Samples.push(resumed);
  result.Checks.ResumeAdvanced = true;
  result.StoppedTicks = resumed.ReportedTicks;
  if (stopAtEnd) {
    await action(page, 'stop');
    await completeRetirement(page, result, true);
  }
  return result;
}

test.use({ screenshot: 'off', trace: 'off', video: 'off' });

test('native browser plays Goby global copy timestamps and AV1 output', async ({ page, browser }) => {
  test.setTimeout(240_000);
  const fixture = loadFixture();
  const result = { Marker: 'goby-amd-native-browser-result-v1', RunId: fixture.RunId, HarnessComplete: false,
    PlayerKind: 'native-html-media-element-harness', OriginalEmbyWeb: false, AMDHardwareExercised: false,
    BrowserVersion: browser.version(), PageErrors: 0, ExternalRequests: 0,
    Capabilities: {} as Record<string, unknown>, Cases: [] as CaseResult[],
    AV1: { Status: 'not-attempted', Case: undefined as CaseResult | undefined },
    HEVC: { Status: 'capability-observed-only', BrowserPlaybackVerified: false },
    Failure: '', LastPlayerSnapshot: null as Sample | null,
  };
  page.on('pageerror', () => { result.PageErrors++; });
  await page.context().route('**/*', async (route) => {
    let allowed = false;
    try { allowed = new URL(route.request().url()).origin === fixture.BaseURL; } catch { /* Unknown origins remain blocked. */ }
    if (allowed) await route.continue();
    else { result.ExternalRequests++; await route.abort('blockedbyclient'); }
  });
  try {
    await page.goto(`${fixture.BaseURL}/__amd-media-player`);
    result.Capabilities = await page.evaluate(async () => {
      const element = document.createElement('video');
      const types = { H264: 'video/mp4; codecs="avc1.64000a, mp4a.40.2"', AV1: 'video/mp4; codecs="av01.0.00M.08, mp4a.40.2"', HEVC: 'video/mp4; codecs="hvc1.1.6.L30.B0, mp4a.40.2"' };
      const facts: Record<string, unknown> = { UserAgent: navigator.userAgent, RequestVideoFrameCallback: typeof element.requestVideoFrameCallback === 'function' };
      for (const [codec, mime] of Object.entries(types)) {
        let decoding: MediaCapabilitiesDecodingInfo | undefined;
        try {
          decoding = await navigator.mediaCapabilities?.decodingInfo({ type: 'file', video: { contentType: mime.replace(', mp4a.40.2', ''), width: 160, height: 90, bitrate: 300_000, framerate: 24 }, audio: { contentType: 'audio/mp4; codecs="mp4a.40.2"', channels: '1', bitrate: 96_000, samplerate: 48_000 } });
        } catch { /* A missing capability query is diagnostic, not playback evidence. */ }
        facts[codec] = { MIME: mime, CanPlayType: element.canPlayType(mime), DecodingInfo: decoding ? { supported: decoding.supported, smooth: decoding.smooth, powerEfficient: decoding.powerEfficient } : null };
      }
      return facts;
    });
    expect(result.Capabilities.RequestVideoFrameCallback).toBe(true);
    expect((result.Capabilities.H264 as { CanPlayType: string }).CanPlayType, 'The browser environment must actually admit the required H.264/AAC MIME.').not.toBe('');
    await installHarness(page, fixture);
    await page.evaluate(() => window.gobyAMDMedia.load(6.37, 'copy'));
    result.Cases.push(await runCase(page, 'initial-noninteger-copy', 6.37, 6, false, false));
    await page.locator('#seek').fill('3.41');
    await action(page, 'change');
    await completeRetirement(page, result.Cases[0], false);
    result.Cases.push(await runCase(page, 'backward-noninteger-copy', 3.41, 3, false, false));
    await page.locator('#seek').fill('7.25');
    await action(page, 'change');
    await completeRetirement(page, result.Cases[1], false);
    result.Cases.push(await runCase(page, 'forward-noninteger-copy', 7.25, 6));
    if ((result.Capabilities.AV1 as { CanPlayType: string }).CanPlayType === '') {
      result.AV1.Status = 'platform-unsupported';
    } else {
      result.AV1.Status = 'attempted';
      await page.evaluate(() => window.gobyAMDMedia.load(4.25, 'av1'));
      result.AV1.Case = await runCase(page, 'av1-progressive-encoded', 4.25, 4.25, true);
      result.AV1.Status = 'played';
    }
    expect(result.PageErrors).toBe(0);
    expect(result.ExternalRequests).toBe(0);
    result.HarnessComplete = true;
  } catch (error) {
    result.Failure = error instanceof Error ? error.name : 'UnknownError';
    throw error;
  } finally {
    try { result.LastPlayerSnapshot = await page.evaluate(() => window.gobyAMDMedia?.snapshot() ?? null); }
    catch { /* Preserve the already recorded failure if media setup never completed. */ }
    try { await page.evaluate(async () => { if (window.gobyAMDMedia) await window.gobyAMDMedia.shutdown(); }); }
    catch { result.HarnessComplete = false; }
    writePrivateJSON(fixture.ResultPath, result, fixture.Token);
  }
});
