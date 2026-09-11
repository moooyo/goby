#!/usr/bin/env node
/** Drive the original Emby movie UI through the observer's guarded browser. */

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';

const MOVIE = 'M3e Client Movie';

export async function runMovieWorkflow({ page, report, snapshot, repeatLogin, target }) {
  const result = report.playback = { movie: MOVIE, phase: 'home', steps: [],
    outcome: 'in_progress', method: 'Original client UI actions and read-only HTMLMediaElement observations' };

  async function metrics(label) {
    const videos = await page.evaluate(() => [...document.querySelectorAll('video')].map(video => {
      const quality = typeof video.getVideoPlaybackQuality === 'function' ? video.getVideoPlaybackQuality() : null;
      const rect = video.getBoundingClientRect();
      return { current_time: Number.isFinite(video.currentTime) ? video.currentTime : null,
        duration: Number.isFinite(video.duration) ? video.duration : null,
        paused: video.paused, ended: video.ended, seeking: video.seeking,
        ready_state: video.readyState, network_state: video.networkState,
        video_width: video.videoWidth, video_height: video.videoHeight,
        total_video_frames: quality?.totalVideoFrames ?? null,
        dropped_video_frames: quality?.droppedVideoFrames ?? null,
        visible: rect.width > 0 && rect.height > 0 };
    }));
    const entry = { label, videos, observed_at: new Date().toISOString() };
    result.steps.push(entry);
    return videos.find(video => video.visible) ?? videos[0] ?? null;
  }

  async function inspect(label) {
    await snapshot(label);
    result.steps.push({ label: `${label}-controls`, controls: await page.evaluate(() =>
      [...document.querySelectorAll('button,a,input,[role="button"],[role="slider"]')]
        .filter(element => element.getClientRects().length)
        .slice(0, 180).map(element => ({ tag: element.tagName.toLowerCase(), type: element.getAttribute('type'),
          role: element.getAttribute('role'), label: element.getAttribute('aria-label'),
          title: element.getAttribute('title'), class: typeof element.className === 'string' ? element.className : null,
          text: (element.innerText ?? '').trim().slice(0, 120) }))) });
  }

  async function showControls() {
    const video = page.locator('video:visible').first();
    const box = await video.boundingBox();
    if (!box) throw new Error('The original player has no visible video surface.');
    // The observed video is covered by its player's input surface. Pointer
    // movement must target screen coordinates rather than require the video
    // itself to receive pointer events.
    await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.55, { steps: 4 });
    await page.mouse.move(box.x + box.width * 0.55, box.y + box.height * 0.75, { steps: 4 });
    await page.waitForTimeout(500);
  }

  async function button(name) {
    const locator = page.getByRole('button', { name, exact: typeof name === 'string' }).filter({ visible: true });
    if (await locator.count() !== 1) throw new Error('The visible UI control is missing or ambiguous.');
    await locator.click({ timeout: 8000 });
  }

  async function waitPlaying() {
    await page.waitForFunction(() => [...document.querySelectorAll('video')].some(video =>
      video.getClientRects().length && !video.paused && video.readyState >= 2 && video.videoWidth > 0 && video.currentTime > 1),
      null, { timeout: 30000 });
  }

  async function seek(fraction, label) {
    await showControls();
    const ranges = page.locator('input[type="range"]:visible');
    const candidates = [];
    for (let index = 0; index < await ranges.count(); index += 1) {
      const control = ranges.nth(index);
      const description = await control.evaluate(element =>
        [element.className, element.getAttribute('aria-label'), element.getAttribute('title')].join(' '));
      if (/seek|progress|position|timeline/i.test(description)) candidates.push(control);
    }
    if (candidates.length !== 1) throw new Error('The visible seek slider is missing or ambiguous.');
    const before = await metrics(`${label}-before`);
    const slider = candidates[0];
    const box = await slider.boundingBox();
    if (!box || box.width < 50) throw new Error('The seek slider has no usable UI geometry.');
    await slider.click({ position: { x: box.width * fraction, y: box.height / 2 }, timeout: 8000 });
    await page.waitForTimeout(1000);
    const after = await metrics(`${label}-after`);
    const movedInDirection = before && after && (label.includes('forward')
      ? after.current_time > before.current_time + 10 : after.current_time < before.current_time - 10);
    if (!movedInDirection || !after.duration || Math.abs(after.current_time - after.duration * fraction) > Math.max(5, after.duration * 0.02)) {
      throw new Error('The visible seek action did not move the media timeline.');
    }
    await inspect(label);
    return after;
  }

  async function logout(label) {
    if (await page.locator('video:visible').count()) {
      await showControls();
      await button('Back');
      await page.waitForTimeout(750);
    }
    await inspect(`${label}-before`);
    let signOut = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    if (!await signOut.count()) {
      const settings = page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true });
      if (await settings.count() !== 1) throw new Error('The observed account menu control is unavailable.');
      await settings.click({ timeout: 8000 });
      await page.waitForTimeout(300);
      await inspect(`${label}-menu`);
      signOut = page.getByRole('button', { name: /\bSign Out\b/i }).filter({ visible: true });
    }
    if (await signOut.count() !== 1) throw new Error('The original UI Sign Out control is unavailable.');
    const response = page.waitForResponse(value => value.request().method() === 'POST' &&
      new URL(value.url()).origin === target.origin &&
      /^(?:\/emby)?\/sessions\/logout\/?$/i.test(new URL(value.url()).pathname), { timeout: 10000 }).catch(() => null);
    report.logout.attempted = true;
    await signOut.click({ timeout: 8000 });
    const observed = await response;
    if (!observed || observed.status() < 200 || observed.status() >= 300) throw new Error('No successful original-client logout response was observed.');
    report.logout = { attempted: true, result: 'logout_http_accepted_ui_transition_pending', http_status: observed.status(),
      owned_session: 'possibly_retained_owned_session_ui_logout_transition_unconfirmed' };
    await Promise.any([
      page.getByText('Manual Login', { exact: true }).waitFor({ state: 'visible', timeout: 10000 }),
      page.locator('form:has(input[type="password"]:visible)').waitFor({ state: 'visible', timeout: 10000 }),
    ]);
    report.logout.result = 'ui_logout_http_accepted_and_login_view_visible';
    report.logout.owned_session = 'ui_logout_observed';
    (report.logout_history ??= []).push(report.logout);
    await inspect(`${label}-after`);
  }

  try {
    const movie = page.getByText(MOVIE, { exact: true }).filter({ visible: true });
    await movie.first().waitFor({ state: 'visible', timeout: 20000 });
    await inspect('movie-home');
    result.phase = 'movie_detail';
    const title = movie.first();
    const clickable = title.locator('xpath=ancestor-or-self::*[self::button or self::a][1]');
    if (await clickable.count() === 1) await clickable.click({ timeout: 8000 });
    else {
      const card = title.locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " cardBox ")][1]');
      const imageButton = card.locator('button.cardContent-button');
      if (await imageButton.count() !== 1) throw new Error('The original movie card action is unavailable.');
      await imageButton.click({ timeout: 8000 });
    }
    await page.waitForTimeout(750);
    await inspect('movie-detail');
    result.phase = 'play';
    const fromBeginning = page.getByRole('button', { name: 'From Beginning', exact: true }).filter({ visible: true });
    if (await fromBeginning.count() === 1) await fromBeginning.click({ timeout: 8000 });
    else await button('Play');
    await waitPlaying();
    const started = await metrics('playing-start');
    await page.waitForTimeout(2500);
    const advanced = await metrics('playing-advanced');
    if (!started || !advanced || advanced.current_time <= started.current_time || advanced.total_video_frames <= started.total_video_frames) {
      throw new Error('The original player did not demonstrate advancing decoded frames.');
    }
    await showControls();
    await inspect('movie-playing');
    result.phase = 'pause';
    await button('Pause');
    await page.waitForTimeout(300);
    const paused = await metrics('paused');
    if (!paused?.paused) throw new Error('The original UI did not pause playback.');
    result.phase = 'seek_forward';
    await seek(0.30, 'movie-seek-forward');
    result.phase = 'seek_backward';
    await seek(0.20, 'movie-seek-backward');
    const beforeContinue = await metrics('before-continue');
    if (beforeContinue?.paused) await button('Play');
    await waitPlaying();
    await page.waitForTimeout(1500);
    result.phase = 'stop';
    const stoppingAt = await metrics('before-stop');
    await inspect('movie-before-stop');
    const previousRequests = report.requests.length;
    await button('Back');
    await page.waitForTimeout(1200);
    await inspect('movie-stopped');
    await metrics('after-stop');
    const stopped = report.requests.slice(previousRequests).find(entry => /\/Sessions\/Playing\/Stopped$/i.test(entry.route) && entry.method === 'POST');
    if (!stopped || stopped.status < 200 || stopped.status >= 300) throw new Error('The original client did not complete its Stopped report.');
    result.stop_position_seconds = stoppingAt?.current_time ?? null;
    result.phase = 'relogin';
    await inspect('movie-before-relogin');
    await logout('movie-relogin-sign-out');
    await repeatLogin();
    const resumedMovie = page.getByText(MOVIE, { exact: true }).filter({ visible: true });
    await resumedMovie.first().waitFor({ state: 'visible', timeout: 20000 });
    await inspect('movie-relogin-home');
    const resumedTitle = resumedMovie.first().locator('xpath=ancestor-or-self::*[self::button or self::a][1]');
    if (await resumedTitle.count() !== 1) throw new Error('The original movie card is unavailable after relogin.');
    await resumedTitle.click({ timeout: 8000 });
    await page.waitForTimeout(750);
    result.phase = 'resume';
    await inspect('movie-before-resume');
    await button(/\bResume\b/i);
    await waitPlaying();
    const resumed = await metrics('resumed');
    if (!resumed || !stoppingAt || Math.abs(resumed.current_time - stoppingAt.current_time) > 15) {
      throw new Error('The original resume action did not restore the observed stop position.');
    }
    await page.waitForTimeout(2500);
    const resumedAdvanced = await metrics('resumed-advanced');
    if (!resumedAdvanced || resumedAdvanced.paused || resumedAdvanced.current_time <= resumed.current_time + 1 ||
        resumedAdvanced.total_video_frames <= resumed.total_video_frames) {
      throw new Error('Resumed playback did not demonstrate advancing decoded frames.');
    }
    await inspect('movie-resumed');
    await showControls();
    await button('Back');
    await page.waitForTimeout(750);
    result.phase = 'logout';
    await logout('movie-sign-out');
    result.phase = 'complete';
    result.outcome = 'movie_ui_flow_completed';
  } catch {
    result.outcome = 'blocked_at_observed_ui_step';
    await metrics(`blocked-${result.phase}`).catch(() => {});
    await inspect(`movie-blocked-${result.phase}`).catch(() => {});
    result.cleanup_attempted = true;
    try { await logout('movie-blocked-cleanup'); }
    catch { result.cleanup_result = 'ui_logout_not_completed_private_state_required'; }
    throw new Error('The original movie UI workflow stopped at its recorded phase.');
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.platform !== 'linux') throw new Error('Run movie acceptance only on the authorized Linux test environment.');
  const observer = fileURLToPath(new URL('./client-browser-observe.mjs', import.meta.url));
  const child = spawn(process.execPath, [observer, ...process.argv.slice(2), '--workflow', 'movie'], { stdio: 'inherit' });
  child.on('error', () => { process.stderr.write('The guarded observer could not start.\n'); process.exitCode = 1; });
  child.on('exit', (code, signal) => { process.exitCode = signal ? 1 : code ?? 1; });
}
