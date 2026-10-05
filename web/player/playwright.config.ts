import { defineConfig, devices } from '@playwright/test';
import { resolve } from 'node:path';

const artifacts = resolve(import.meta.dirname, '../../.artifacts/player-acceptance');
const fixturePort = process.env.GOBY_FIXTURE_PORT ?? '4174';
const runSuffix = fixturePort === '4174' ? '' : `-${fixturePort}`;

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.ts',
  outputDir: resolve(artifacts, `test-results${runSuffix}`),
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [['list'], ['html', { outputFolder: resolve(artifacts, `report${runSuffix}`), open: 'never' }], ['json', { outputFile: resolve(artifacts, `results${runSuffix}.json`) }]],
  use: {
    baseURL: process.env.GOBY_PLAYER_TEST_URL ?? `http://127.0.0.1:${fixturePort}`,
    headless: true,
    channel: process.env.GOBY_BROWSER_CHANNEL,
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    reducedMotion: 'reduce',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
  webServer: {
    command: 'node tests/fixture-server.mjs',
    url: `http://127.0.0.1:${fixturePort}/__fixture/health`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
