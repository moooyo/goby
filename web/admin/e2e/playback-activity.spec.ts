import type { Page, Request, Route } from '@playwright/test';
import type { ActivityEntry, ActivityResponse } from '../src/api';
import type { DashboardAPI } from './dashboard-fixture';
import { administrator, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, navigate, noOverflow } from './dashboard-review';

// These synthetic responses exercise retained playback reports and browser
// request ownership. They do not represent a complete playback statistics feed.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const activityPath = '/admin/v1/activity';
const stoppedAt = '2026-10-28T07:15:04.123Z';
const newerStop = '2026-11-03T11:24:05Z';
const timeoutMessage = 'Recently ended playback did not respond within 15 seconds. Refresh to try again.';

function playbackEvent(overrides: Partial<ActivityEntry> = {}): ActivityEntry {
  return {
    Id: '41', Date: stoppedAt, Action: 'playback.stopped', Severity: 'Info', Source: 'emby',
    Actor: { Kind: 'user', Id: administrator.Id, Name: 'Current viewer name' },
    Resource: { Kind: 'item', Id: 'film-one', Name: 'Current media title' },
    Revision: null, ObservationFingerprint: 'ab'.repeat(32), Count: '0', State: null, ChangedFields: [],
    Name: 'Playback stopped', Overview: 'The player reported that playback stopped.',
    ...overrides,
  };
}

function activityResponse(items: ActivityEntry[], limit = 6, startIndex = 0): ActivityResponse {
  return { Items: items.slice(startIndex, startIndex + limit), TotalRecordCount: items.length, StartIndex: startIndex, Limit: limit, RetentionDays: 30 };
}

function installActivity(api: DashboardAPI, items: () => ActivityEntry[]) {
  const queries: URLSearchParams[] = [];
  api.handlers.set(`GET ${activityPath}`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    queries.push(query);
    const selected = items().filter((entry) => !query.get('Action') || entry.Action === query.get('Action'));
    return json(route, activityResponse(selected, Number(query.get('Limit') ?? 50), Number(query.get('StartIndex') ?? 0)));
  });
  return queries;
}

const recentPanel = (page: Page) => page.getByRole('region', { name: 'Recently ended', exact: true });
const recentRefresh = (page: Page) => recentPanel(page).getByRole('button', { name: 'Refresh recently ended playback', exact: true });

function activityRead(page: Page, action = 'playback.stopped') {
  return page.waitForResponse((response) => response.request().method() === 'GET'
    && new URL(response.url()).pathname === activityPath
    && new URL(response.url()).searchParams.get('Action') === action);
}

async function openRecent(page: Page) {
  await page.goto('/admin/access/sessions');
  await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
  await expect(recentRefresh(page)).toBeEnabled();
  return recentPanel(page);
}

function holdNextActivity(api: DashboardAPI, lateItems: ActivityEntry[]) {
  let release!: () => void;
  let finish!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const finished = new Promise<void>((resolve) => { finish = resolve; });
  let request: Request | undefined;
  api.handlers.set(`GET ${activityPath}`, async (route: Route, pending: Request) => {
    request = pending;
    try {
      await held;
      // Aborted browser reads may reject fulfillment when their synthetic late
      // response is released. The current page must still keep its newer data.
      await json(route, activityResponse(lateItems)).catch(() => undefined);
    } finally { finish(); }
  });
  return { release, finished, request: () => request };
}

test('recently ended uses retained stop reports, joined names, and the recorded event time', async ({ page, api }, testInfo) => {
  const missing = playbackEvent({ Id: '40', Date: '2026-10-27T18:20:00Z', Actor: { Kind: 'user', Id: 'removed-user', Name: null }, Resource: { Kind: 'item', Id: 'removed-item' } });
  const started = playbackEvent({ Id: '39', Action: 'playback.started', Resource: { Kind: 'item', Id: 'started-item', Name: 'Started playback must stay out of this list' } });
  const queries = installActivity(api, () => [playbackEvent(), missing, started]);
  const panel = await openRecent(page);
  const records = panel.getByRole('list', { name: 'Recently ended playback', exact: true }).getByRole('listitem');
  await expect(records).toHaveCount(2);
  await expect(records.first().getByText('Current media title', { exact: true })).toHaveAttribute('translate', 'no');
  await expect(records.first().getByText('Current viewer name', { exact: true })).toHaveAttribute('translate', 'no');
  await expect(records.first().locator(`time[datetime="${stoppedAt}"]`)).toBeVisible();
  await expect(panel.locator(`time[datetime="${stamp}"]`)).toHaveCount(0);
  await expect(records.nth(1)).toContainText('Item unavailable');
  await expect(records.nth(1)).toContainText('User unavailable');
  await expect(panel).not.toContainText('Started playback must stay out of this list');
  await expect(panel).toContainText('Recent playback stop reports retained for up to 30 days. These events are not complete playback statistics.');
  await expect(panel).toContainText('Names reflect currently available users and media.');
  expect(queries.length).toBeGreaterThan(0);
  for (const query of queries) expect(Object.fromEntries(query)).toEqual({ Action: 'playback.stopped', StartIndex: '0', Limit: '6' });
  expect(api.writes()).toHaveLength(0);
  await noOverflow(page);
  await evidence(page, testInfo, 'playback-recent-retained-stop-reports');
});

test('an empty retained list can be refreshed from the keyboard', async ({ page, api }) => {
  let items: ActivityEntry[] = [];
  const queries = installActivity(api, () => items);
  const panel = await openRecent(page);
  await expect(panel.getByText('No recently ended playback', { exact: true })).toBeVisible();
  await expect(panel).toContainText('No retained playback stop reports are available.');
  await expect(panel.getByRole('listitem')).toHaveCount(0);
  const initialReads = queries.length;
  items = [playbackEvent()];
  const refreshed = activityRead(page);
  await recentRefresh(page).focus();
  await page.keyboard.press('Enter');
  expect((await refreshed).ok()).toBe(true);
  await expect(panel.getByText('Current media title', { exact: true })).toBeVisible();
  await expect(panel.getByText('No recently ended playback', { exact: true })).toHaveCount(0);
  expect(queries).toHaveLength(initialReads + 1);
  expect(api.writes()).toHaveLength(0);
});

test('a failed retained activity read shows an error and recovers through retry', async ({ page, api }) => {
  api.handlers.set(`GET ${activityPath}`, (route) => json(route, { Error: { Code: 'activity_unavailable', Message: 'Synthetic retained playback activity is unavailable.' } }, 503));
  const panel = await openRecent(page);
  await expect(panel.getByRole('alert')).toContainText('Synthetic retained playback activity is unavailable.');
  await expect(panel.getByText('No recently ended playback', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('listitem')).toHaveCount(0);
  installActivity(api, () => [playbackEvent()]);
  const recovered = activityRead(page);
  await panel.getByRole('button', { name: 'Retry', exact: true }).click();
  expect((await recovered).ok()).toBe(true);
  await expect(panel.getByText('Current media title', { exact: true })).toBeVisible();
  await expect(panel.getByRole('alert')).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});

test('a retained activity read times out, aborts, and cannot replace a newer successful result', async ({ page, api }) => {
  installActivity(api, () => [playbackEvent()]);
  const panel = await openRecent(page);
  await expect(panel.getByText('Current media title', { exact: true })).toBeVisible();
  await page.clock.install();
  const late = holdNextActivity(api, [playbackEvent({ Resource: { Kind: 'item', Id: 'late-item', Name: 'Late cancelled title' } })]);
  try {
    await recentRefresh(page).click();
    await expect.poll(() => Boolean(late.request())).toBe(true);
    await expect(recentRefresh(page)).toBeDisabled();
    await page.clock.fastForward(15_001);
    await expect(panel.getByRole('alert')).toContainText(timeoutMessage);
    await expect(recentRefresh(page)).toBeEnabled();
    installActivity(api, () => [playbackEvent({ Date: newerStop, Resource: { Kind: 'item', Id: 'newer-item', Name: 'Newer successful title' } })]);
    await panel.getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(panel.getByText('Newer successful title', { exact: true })).toBeVisible();
    late.release();
    await late.finished;
    await expect.poll(() => late.request()?.failure()?.errorText).toBe('net::ERR_ABORTED');
    await expect(panel.getByText('Newer successful title', { exact: true })).toBeVisible();
    await expect(panel.locator(`time[datetime="${newerStop}"]`)).toBeVisible();
    await expect(panel.getByText('Late cancelled title', { exact: true })).toHaveCount(0);
    await expect(panel.getByRole('alert')).toHaveCount(0);
    expect(api.writes()).toHaveLength(0);
  } finally { late.release(); if (late.request()) await late.finished; }
});

test('leaving sessions aborts its pending read and a late response cannot overwrite a remounted panel', async ({ page, api }) => {
  installActivity(api, () => [playbackEvent()]);
  await openRecent(page);
  const late = holdNextActivity(api, [playbackEvent({ Resource: { Kind: 'item', Id: 'unmounted-item', Name: 'Response from the previous page' } })]);
  try {
    await recentRefresh(page).click();
    await expect.poll(() => Boolean(late.request())).toBe(true);
    await navigate(page, '/admin/access/devices');
    await expect(page.getByRole('heading', { name: 'Devices', exact: true })).toBeVisible();
    await expect(recentPanel(page)).toHaveCount(0);
    installActivity(api, () => [playbackEvent({ Date: newerStop, Resource: { Kind: 'item', Id: 'remounted-item', Name: 'Current page response' } })]);
    await navigate(page, '/admin/access/sessions');
    await expect(recentPanel(page).getByText('Current page response', { exact: true })).toBeVisible();
    late.release();
    await late.finished;
    await expect.poll(() => late.request()?.failure()?.errorText).toBe('net::ERR_ABORTED');
    await expect(recentPanel(page).getByText('Current page response', { exact: true })).toBeVisible();
    await expect(recentPanel(page).getByText('Response from the previous page', { exact: true })).toHaveCount(0);
    await expect(recentPanel(page).locator(`time[datetime="${newerStop}"]`)).toBeVisible();
    expect(api.writes()).toHaveLength(0);
  } finally { late.release(); if (late.request()) await late.finished; }
});

test('activity filters offer both playback actions and send their exact action identifiers', async ({ page, api }) => {
  installActivity(api, () => [playbackEvent({ Id: '51', Action: 'playback.started', Name: 'Playback started' }), playbackEvent({ Id: '52' })]);
  await page.goto('/admin/system/observability');
  await expect(page.getByRole('heading', { name: 'Activity & logs', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Filter activity', exact: true }).click();
  const action = page.getByRole('combobox', { name: 'Action', exact: true });
  await expect(action.getByRole('option', { name: 'Playback started', exact: true })).toHaveAttribute('value', 'playback.started');
  await expect(action.getByRole('option', { name: 'Playback stopped', exact: true })).toHaveAttribute('value', 'playback.stopped');
  const records = page.getByRole('table', { name: 'Activity records', exact: true });
  for (const [value, id, excluded] of [['playback.started', '51', '52'], ['playback.stopped', '52', '51']] as const) {
    await action.selectOption(value);
    const filtered = activityRead(page, value);
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
    expect((await filtered).ok()).toBe(true);
    await expect(records.getByRole('button', { name: `Details for activity ${id}`, exact: true })).toBeVisible();
    await expect(records.getByRole('button', { name: `Details for activity ${excluded}`, exact: true })).toHaveCount(0);
  }
  expect(api.writes()).toHaveLength(0);
});

test.describe('Chinese recently ended playback on a narrow screen', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' }, viewport: { width: 390, height: 844 } });

  test('translated controls preserve raw user and media names that match translation keys', async ({ page, api }, testInfo) => {
    const longTitle = 'LongUnbrokenMediaTitle'.repeat(16);
    const longViewer = 'LongUnbrokenViewerName'.repeat(12);
    const queries = installActivity(api, () => [
      playbackEvent({ Actor: { Kind: 'user', Id: administrator.Id, Name: 'Playback stopped' }, Resource: { Kind: 'item', Id: 'raw-name', Name: 'Recently ended' } }),
      playbackEvent({ Id: '42', Date: newerStop, Actor: { Kind: 'user', Id: 'long-viewer', Name: longViewer }, Resource: { Kind: 'item', Id: 'long-title', Name: longTitle } }),
    ]);
    await page.goto('/admin/access/sessions');
    const panel = page.getByRole('region', { name: '最近结束', exact: true });
    await expect(panel.getByText('Recently ended', { exact: true })).toBeVisible();
    await expect(panel.getByText('Recently ended', { exact: true })).toHaveAttribute('translate', 'no');
    await expect(panel.getByText('Playback stopped', { exact: true })).toBeVisible();
    await expect(panel.getByText('Playback stopped', { exact: true })).toHaveAttribute('translate', 'no');
    await expect(panel.getByText(longTitle, { exact: true })).toHaveAttribute('translate', 'no');
    await expect(panel.getByText(longViewer, { exact: true })).toHaveAttribute('translate', 'no');
    await expect(panel.locator(`time[datetime="${stoppedAt}"]`)).toBeVisible();
    const refresh = panel.getByRole('button', { name: '刷新最近结束的播放', exact: true });
    await expect(refresh).toBeEnabled();
    const initialReads = queries.length;
    await refresh.focus();
    const refreshed = activityRead(page);
    await page.keyboard.press('Enter');
    expect((await refreshed).ok()).toBe(true);
    await expect(refresh).toBeEnabled();
    await expect(panel.getByText('Recently ended', { exact: true })).toBeVisible();
    expect(queries).toHaveLength(initialReads + 1);
    expect(api.writes()).toHaveLength(0);
    await noOverflow(page);
    await evidence(page, testInfo, 'playback-recent-390-zh-dark-raw-names');
  });
});
