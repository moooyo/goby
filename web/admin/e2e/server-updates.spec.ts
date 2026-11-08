import type { Locator, Page } from '@playwright/test';
import type { ServerRelease, ServerUpdates, UpdateState } from '../src/updatesApi';
import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

const updatesPath = '/admin/v1/server-updates';
const checkPath = `${updatesPath}/check`;
const staleMessage = 'The latest result could not be confirmed. Any release details shown are from the last confirmed result. Reload the cached result before checking again.';

function releaseFixture(version = '0.2.0'): ServerRelease {
  return {
    Version: version, Classification: 'Release', PublishedAt: '2026-11-05T07:15:00Z',
    Description: 'Playback improvements and library maintenance fixes.\nRelease notes are displayed as plain text: <strong>safe content</strong>.',
    InfoUrl: `https://releases.goby.example/releases/${version}`, SourceUrl: `https://releases.goby.example/files/goby-${version}-linux-amd64.tar.gz`,
    SHA256: 'a'.repeat(64), TargetFilename: `goby-${version}-linux-amd64.tar.gz`,
  };
}

function updatesFixture(state: UpdateState = 'not_checked'): ServerUpdates {
  const release = state === 'available' || state === 'current' || state === 'version_unknown' ? releaseFixture(state === 'current' ? '0.1.0' : '0.2.0') : null;
  return {
    CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: state !== 'unconfigured',
    CurrentVersion: state === 'version_unknown' ? '(devel)' : '0.1.0', State: state,
    CheckedAt: state === 'unconfigured' || state === 'not_checked' ? null : stamp, Release: release,
    ErrorCode: state === 'failed' ? 'update_check_failed' : state === 'version_unknown' ? 'current_version_unknown' : null,
  };
}

function installUpdates(api: DashboardAPI, initial: ServerUpdates = updatesFixture()) {
  const state = { saved: initial };
  api.handlers.set(`GET ${updatesPath}`, (route) => json(route, state.saved));
  api.handlers.set(`POST ${checkPath}`, (route, request) => {
    expect(request.postDataJSON()).toEqual({});
    expect(request.headers()['x-csrf-token']).toBe(csrf);
    state.saved = updatesFixture('available');
    return json(route, state.saved);
  });
  return state;
}

function checks(api: DashboardAPI) {
  return api.writes().filter((request) => request.path === checkPath);
}

async function openUpdates(page: Page, name = 'Server updates'): Promise<Locator> {
  await page.goto('/admin/settings/general');
  const panel = page.getByRole('region', { name, exact: true });
  await expect(panel).toBeVisible();
  await expect(page.locator('.server-updates')).toHaveAttribute('aria-busy', 'false');
  await panel.scrollIntoViewIfNeeded();
  return panel;
}

test('an unconfigured deployment never claims a current release or issues a release check', async ({ page, api }, testInfo) => {
  installUpdates(api, updatesFixture('unconfigured'));
  const panel = await openUpdates(page);
  await expect(panel.getByText('Release source not configured', { exact: true })).toBeVisible();
  await expect(panel.getByText('Configure a trusted release source in the deployment to enable update checks. No default source is used.', { exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
  await expect(panel.getByRole('textbox')).toHaveCount(0);
  await expect(panel.getByRole('combobox')).toHaveCount(0);
  await expect(panel.getByRole('switch')).toHaveCount(0);
  await expect(panel.getByRole('link', { name: 'View release details', exact: true })).toHaveCount(0);
  await expect(panel.getByText('No newer release found', { exact: true })).toHaveCount(0);
  const before = api.reads(updatesPath).length;
  await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
  await expect.poll(() => api.reads(updatesPath).length).toBe(before + 1);
  await expect(page.locator('.server-updates')).toHaveAttribute('aria-busy', 'false');
  expect(checks(api)).toEqual([]);
  await noOverflow(page);
  await evidence(page, testInfo, 'server-updates-unconfigured-en-light');
});

test('an explicit check sends an empty protected request and renders the actual release safely', async ({ page, api }, testInfo) => {
  installUpdates(api);
  const panel = await openUpdates(page);
  await expect(panel.getByText('Updates have not been checked', { exact: true })).toBeVisible();
  expect(checks(api)).toEqual([]);
  await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
  await expect(panel.getByText('Update available', { exact: true })).toBeVisible();
  await expect(panel.getByText('0.2.0', { exact: true })).toBeVisible();
  await expect(panel.getByText('Stable release', { exact: true })).toBeVisible();
  await expect(panel.getByText(releaseFixture().Description, { exact: true })).toBeVisible();
  await expect(panel.locator('.server-updates-notes strong')).toHaveCount(0);
  const link = panel.getByRole('link', { name: 'View release details', exact: true });
  await expect(link).toHaveAttribute('href', releaseFixture().InfoUrl);
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  await expect(panel.locator('time[datetime="2026-11-05T07:15:00Z"]')).toBeVisible();
  await expect(panel.getByText('Deploy the updated Docker image through your deployment tools. Release checks do not change the running image.', { exact: true })).toBeVisible();
  expect(checks(api)).toEqual([{ method: 'POST', path: checkPath, body: {}, csrf }]);
  await noOverflow(page);
  await evidence(page, testInfo, 'server-updates-available-en-light');
});

for (const state of ['current', 'version_unknown'] as const) {
  test(`a confirmed ${state} result distinguishes successful comparison from an unknown build version`, async ({ page, api }, testInfo) => {
    installUpdates(api, updatesFixture(state));
    const panel = await openUpdates(page);
    await expect(panel.getByText(state === 'current' ? 'No newer release found' : 'Running version cannot be compared', { exact: true })).toBeVisible();
    await expect(panel.getByText(state === 'current' ? 'The last successful check found no release newer than the running version.' : 'The release source was checked successfully. This build version cannot be compared with the reported release.', { exact: true })).toBeVisible();
    if (state === 'version_unknown') {
      await expect(panel.getByText('(devel)', { exact: true })).toBeVisible();
      await expect(panel.getByText('No newer release found', { exact: true })).toHaveCount(0);
    }
    await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeEnabled();
    expect(checks(api)).toEqual([]);
    await noOverflow(page);
    await evidence(page, testInfo, `server-updates-${state.replace('_', '-')}-en-light`);
  });
}

test('a confirmed failed check shows its attempt time and can recover through another explicit check', async ({ page, api }, testInfo) => {
  const state = installUpdates(api);
  api.handlers.set(`POST ${checkPath}`, (route) => { state.saved = updatesFixture('failed'); return json(route, state.saved); });
  const panel = await openUpdates(page);
  await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
  await expect(panel.getByText('Update check failed', { exact: true })).toBeVisible();
  await expect(panel.getByText('The release source could not be checked. The server update status is unknown.', { exact: true })).toBeVisible();
  await expect(panel.locator(`time[datetime="${stamp}"]`)).toBeVisible();
  await expect(panel.getByText('No newer release found', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('link')).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeEnabled();
  await evidence(page, testInfo, 'server-updates-confirmed-failure-en-light');
  api.handlers.set(`POST ${checkPath}`, (route) => { state.saved = updatesFixture('available'); return json(route, state.saved); });
  await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
  await expect(panel.getByText('Update available', { exact: true })).toBeVisible();
  await expect(panel.getByText('Update check failed', { exact: true })).toHaveCount(0);
  expect(checks(api)).toHaveLength(2);
});

test('an unconfirmed check keeps the last result visibly stale until the cache is explicitly reloaded', async ({ page, api }, testInfo) => {
  const state = installUpdates(api, updatesFixture('current'));
  api.handlers.set(`POST ${checkPath}`, async (route) => { state.saved = updatesFixture('available'); await route.abort('connectionreset'); });
  const panel = await openUpdates(page);
  await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
  await expect(panel.getByText('Update result needs to be refreshed', { exact: true })).toBeVisible();
  await expect(panel.getByText(staleMessage, { exact: true })).toBeVisible();
  await expect(panel.getByText('Release from last confirmed result', { exact: true })).toBeVisible();
  await expect(panel.getByText('No newer release found', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
  expect(checks(api)).toHaveLength(1);
  await noOverflow(page);
  await evidence(page, testInfo, 'server-updates-unconfirmed-check-en-light');
  await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
  await expect(panel.getByText('Update available', { exact: true })).toBeVisible();
  await expect(panel.getByText(staleMessage, { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeEnabled();
  expect(checks(api)).toHaveLength(1);
});

test('a failed cache refresh cannot leave an older successful status presented as current', async ({ page, api }) => {
  const state = installUpdates(api, updatesFixture('current'));
  const panel = await openUpdates(page);
  api.handlers.set(`GET ${updatesPath}`, (route) => json(route, { Error: { Code: 'synthetic_cache_unavailable', Message: 'Synthetic cache unavailable.' } }, 503));
  await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
  await expect(panel.getByText('Synthetic cache unavailable.', { exact: true })).toBeVisible();
  await expect(panel.getByText(staleMessage, { exact: true })).toBeVisible();
  await expect(panel.getByText('No newer release found', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
  api.handlers.set(`GET ${updatesPath}`, (route) => json(route, state.saved));
  await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
  await expect(panel.getByText('No newer release found', { exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeEnabled();
  expect(checks(api)).toEqual([]);
});

test('an unsafe release link is rejected and a valid cached response can be reloaded', async ({ page, api }) => {
  const invalid = updatesFixture('available');
  invalid.Release!.InfoUrl = 'javascript:alert(1)';
  const state = installUpdates(api, invalid);
  const panel = await openUpdates(page);
  await expect(panel.getByText('The update response is incomplete. Reload the cached result before continuing.', { exact: true })).toBeVisible();
  await expect(panel.getByRole('link')).toHaveCount(0);
  await expect(panel.getByText('Update available', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
  state.saved = updatesFixture('available');
  state.saved.Release!.Classification = 'Beta';
  await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
  await expect(panel.getByText('Update available', { exact: true })).toBeVisible();
  await expect(panel.getByText('Beta release', { exact: true })).toBeVisible();
});

test('rapid repeated clicks start one check and disable both actions while it is pending', async ({ page, api }, testInfo) => {
  const state = installUpdates(api);
  let release: (() => void) | undefined;
  const held = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`POST ${checkPath}`, async (route) => {
    await held;
    state.saved = updatesFixture('available');
    await json(route, state.saved).catch(() => undefined);
  });
  try {
    const panel = await openUpdates(page);
    await panel.getByRole('button', { name: 'Check for updates', exact: true }).evaluate((element) => {
      if (!(element instanceof HTMLButtonElement)) throw new Error('The update action must be a native button.');
      element.click(); element.click();
    });
    await expect(panel.getByRole('button', { name: 'Checking for updates…', exact: true })).toBeDisabled();
    await expect(panel.getByRole('button', { name: 'Reload cached result', exact: true })).toBeDisabled();
    await expect(page.locator('.server-updates')).toHaveAttribute('aria-busy', 'true');
    await expect.poll(() => checks(api).length).toBe(1);
    await evidence(page, testInfo, 'server-updates-pending-single-check-en-light');
    release?.();
    await expect(panel.getByText('Update available', { exact: true })).toBeVisible();
    await expect(page.locator('.server-updates')).toHaveAttribute('aria-busy', 'false');
    expect(checks(api)).toHaveLength(1);
  } finally { release?.(); }
});

test('a timed out check requires cache readback before another request', async ({ page, api }) => {
  const state = installUpdates(api);
  let release: (() => void) | undefined;
  const held = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`POST ${checkPath}`, async (route) => { await held; await route.abort('failed').catch(() => undefined); });
  try {
    const panel = await openUpdates(page);
    await page.clock.install();
    await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
    await expect.poll(() => checks(api).length).toBe(1);
    await page.clock.fastForward(15_001);
    await expect(panel.getByText('The update check timed out. Reload the cached result to confirm its outcome.', { exact: true })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
    state.saved = updatesFixture('failed');
    await panel.getByRole('button', { name: 'Reload cached result', exact: true }).click();
    await expect(panel.getByText('Update check failed', { exact: true })).toBeVisible();
    await expect(panel.getByRole('button', { name: 'Check for updates', exact: true })).toBeEnabled();
    expect(checks(api)).toHaveLength(1);
  } finally { release?.(); }
});

test.describe('Chinese dark update panel', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' } });
  test('release details, long notes, and artifact identifiers fit desktop and phone layouts', async ({ page, api }, testInfo) => {
    const value = updatesFixture('available');
    value.Release!.Classification = 'Dev';
    value.Release!.Description = `Playback improvements.\n${'LongReleaseIdentifier'.repeat(12)}`;
    installUpdates(api, value);
    const panel = await openUpdates(page, '服务器更新');
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(panel.getByText('有可用更新', { exact: true })).toBeVisible();
    await expect(panel.getByText('开发版', { exact: true })).toBeVisible();
    await noOverflow(page);
    await evidence(page, testInfo, 'server-updates-available-zh-dark');
    await page.setViewportSize({ width: 390, height: 844 });
    await panel.getByRole('heading', { name: '服务器更新', exact: true }).scrollIntoViewIfNeeded();
    await expect(panel.getByRole('button', { name: '检查更新', exact: true })).toBeEnabled();
    await noOverflow(page);
    await evidence(page, testInfo, 'server-updates-available-390-zh-dark');
    await panel.getByText('发布文件', { exact: true }).click();
    await expect(panel.getByText(value.Release!.SHA256, { exact: true })).toBeVisible();
    await panel.getByRole('link', { name: '查看发布详情', exact: true }).scrollIntoViewIfNeeded();
    await noOverflow(page);
    await evidence(page, testInfo, 'server-updates-artifact-390-zh-dark');
  });
});
