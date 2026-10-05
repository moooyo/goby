import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createUser, TOKEN } from '../tests/fixture-data.mjs';

assert.equal(process.platform, 'linux');
const root = await realpath('/opt/goby-test/player-live-20261004-165c');
assert.equal((await readFile(resolve(root, '.owner'), 'utf8')).trim(), 'goby-player-live-20261004-165c');
const output = resolve(root, 'artifacts/other-pages-review-20261005/detail/touchscreen');
await mkdir(output, { recursive: true });
const origin = 'http://127.0.0.1:4223';
const server = spawn(process.execPath, [resolve(import.meta.dirname, '../tests/fixture-server.mjs')], { env: { ...process.env, GOBY_FIXTURE_PORT: '4223', GOBY_HANDOFF_DIR: resolve(root, 'handoff-v3') }, stdio: 'ignore' });
const browser = await chromium.launch({ headless: true });
const records = [];
const failures = [];
try {
  for (let attempt = 0; attempt < 50; attempt++) {
    try { if ((await fetch(`${origin}/__fixture/health`)).ok) break; } catch { /* Wait for the owned fixture server. */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  for (const height of [667, 520, 480]) {
    const context = await browser.newContext({ viewport: { width: 375, height }, hasTouch: true, locale: 'zh-CN', reducedMotion: 'reduce' });
    await context.addInitScript(({ origin, user, token }) => localStorage.setItem('goby.player.session.v1', JSON.stringify({ AccessToken: token, User: user, ServerId: 'fixture-server', serverUrl: origin })), { origin, user: createUser(), token: TOKEN });
    const page = await context.newPage();
    page.on('pageerror', error => failures.push(error.message));
    await page.goto(`${origin}/#/detail/deep`);
    await page.locator('.detail-overview-info h1').waitFor();
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(1100);
    const scroll = page.locator('.detail-overview-scroll');
    const overflow = await scroll.evaluate(element => element.scrollHeight - element.clientHeight);
    let beforeGesture = 0;
    let afterGesture = 0;
    if (overflow > 0) {
      await scroll.evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      beforeGesture = await scroll.evaluate(element => element.scrollTop);
      const cdp = await context.newCDPSession(page);
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 100, y: 300 }] });
      for (const y of [280, 255, 230, 200, 170]) {
        await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 100, y }] });
        await page.waitForTimeout(20);
      }
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
      await cdp.detach();
      await page.waitForTimeout(400);
      afterGesture = await scroll.evaluate(element => element.scrollTop);
      assert(afterGesture > beforeGesture, 'An upward touch gesture must consume the remaining content scroll.');
      assert.equal(await page.locator('.deck-panel[data-active="true"]').getAttribute('aria-label'), '概览');
    }
    await scroll.evaluate(element => { element.scrollTop = element.scrollHeight; });
    const lastControl = await page.locator('.deck-panel[data-active="true"]').getByRole('button', { name: '看过', exact: true }).boundingBox();
    const navigation = await page.getByRole('navigation', { name: '主导航' }).boundingBox();
    assert(lastControl && navigation);
    assert(lastControl.y + lastControl.height < navigation.y, 'The last detail control must clear fixed bottom navigation.');
    await page.screenshot({ path: resolve(output, `375x${height}-bottom.png`) });
    records.push({ height, overflow, beforeGesture, afterGesture, lastControl, navigation, clearance: navigation.y - lastControl.y - lastControl.height });
    await context.close();
  }
} catch (error) { failures.push(error.stack); process.exitCode = 1; }
finally {
  await browser.close(); server.kill('SIGTERM');
  await writeFile(resolve(output, 'results.json'), JSON.stringify({ records, failures }, null, 2));
  console.log(JSON.stringify({ output, records, failures }));
}
