import { chromium } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const origin = process.env.GOBY_PLAYER_REVIEW_URL ?? 'http://127.0.0.1:4184';
const output = resolve(import.meta.dirname, '../../../.artifacts/player-acceptance/layout-review');
await mkdir(output, { recursive: true });
const browser = await chromium.launch();
const results = [];
for (const [width, height] of [[375, 667], [844, 390], [720, 600], [768, 1024], [1024, 768]]) {
  const context = await browser.newContext({ viewport: { width, height }, locale: 'zh-CN', reducedMotion: 'reduce', hasTouch: width < 720 });
  const page = await context.newPage();
  await page.goto(origin);
  await page.getByLabel('用户名', { exact: true }).fill('reviewer');
  await page.getByLabel('密码', { exact: true }).fill('goby-player-test');
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.locator('.feature-info h1').waitFor();
  for (const route of ['home', 'movies', 'detail/deep', 'settings']) {
    await page.goto(`${origin}/#/${route}`);
    await page.locator(route === 'home' ? '.feature-info' : route === 'movies' ? '.poster-card' : route.startsWith('detail') ? '.detail-actions' : '.settings-profile').first().waitFor();
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(1200);
    const metrics = await page.evaluate(() => ({
      viewport: innerWidth,
      content: document.documentElement.scrollWidth,
      navigation: [...document.querySelectorAll('.nav-tab, .mobile-tools button')].filter(element => getComputedStyle(element).display !== 'none' && element.getBoundingClientRect().height > 0).map(element => {
        const rect = element.getBoundingClientRect();
        return { label: element.textContent || element.getAttribute('aria-label'), left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
      }),
      scrollAreas: [...document.querySelectorAll('.deck-panel[data-active="true"] .stage-scroll')].map(element => ({ height: element.clientHeight, content: element.scrollHeight })),
    }));
    results.push({ width, height, route, ...metrics });
    if (metrics.content > width) throw new Error(`Horizontal overflow at ${width}x${height}: ${route}`);
    await page.screenshot({ path: resolve(output, `${width}x${height}-${route.replace('/', '-')}.png`), fullPage: true });
  }
  await context.close();
}
await writeFile(resolve(output, 'results.json'), JSON.stringify(results, null, 2));
await browser.close();
console.log(`${results.length} responsive page layouts checked without horizontal overflow.`);
