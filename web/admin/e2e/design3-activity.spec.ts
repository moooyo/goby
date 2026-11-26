import type { Page } from '@playwright/test';
import type { ActivityEntry } from '../src/api';
import type { DashboardAPI } from './dashboard-fixture';
import { administrator, expect, json, test } from './dashboard-fixture';
import { noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', preferences: { language: 'en', theme: 'light' } });

const path = '/admin/v1/activity';
const privateTitle = 'Private playback title';
const privateItemId = 'private-playback-item';
const privateFingerprints = ['a'.repeat(64), 'b'.repeat(64)];
const now = new Date('2026-11-25T18:30:00Z');

function entry(overrides: Partial<ActivityEntry> = {}): ActivityEntry {
  return {
    Id: '50', Date: now.toISOString(), Action: 'settings.updated', Severity: 'Info', Source: 'native',
    Actor: { Kind: 'user', Id: administrator.Id, Name: 'Current administrator' }, Resource: { Kind: 'settings', Id: 'runtime' },
    Revision: '2', Count: '1', State: null, ChangedFields: ['ServerName'], Name: 'Server settings updated', Overview: 'Supported server settings were updated.', ...overrides,
  };
}

function playback(id: string, action: 'playback.started' | 'playback.stopped'): ActivityEntry {
  return entry({
    Id: id, Date: now.toISOString(), Action: action, Source: 'emby', Revision: null, Count: '0', ChangedFields: [],
    Actor: { Kind: 'user', Id: 'example-user', Name: 'Current viewer' }, Resource: { Kind: 'item', Id: privateItemId, Name: privateTitle },
    ObservationFingerprint: privateFingerprints[action === 'playback.started' ? 0 : 1], Name: `Playback report · ${privateTitle}`, Overview: `Playing ${privateTitle}.`,
  });
}

function installActivity(api: DashboardAPI, items: ActivityEntry[]) {
  const queries: URLSearchParams[] = [];
  api.handlers.set(`GET ${path}`, (route, request) => {
    const query = new URL(request.url()).searchParams;
    queries.push(query);
    const limit = Number(query.get('Limit') ?? '50'), start = Number(query.get('StartIndex') ?? '0');
    const filtered = items.filter((item) => (!query.has('Severity') || item.Severity === query.get('Severity'))
      && (!query.has('Action') || item.Action === query.get('Action'))
      && (!query.has('MinDate') || Date.parse(item.Date) >= Date.parse(query.get('MinDate')!)));
    return json(route, { Items: filtered.slice(start, start + limit), TotalRecordCount: filtered.length, StartIndex: start, Limit: limit, RetentionDays: 45 });
  });
  return queries;
}

async function openActivity(page: Page) {
  await page.goto('/admin/system/observability');
  await expect(page.getByRole('heading', { name: 'Activity & logs' })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Activity', exact: true })).toHaveAttribute('aria-selected', 'true');
  return page.getByRole('tabpanel', { name: 'Activity', exact: true });
}

test('severity totals, wire action and local date filters combine; search stays on the loaded page', async ({ page, api }) => {
  await page.clock.setFixedTime(now);
  const items = Array.from({ length: 51 }, (_, index) => entry({ Id: String(index + 1), Name: index === 50 ? 'Second-page marker' : 'Server settings updated' }));
  items.push(entry({ Id: '90', Severity: 'Warn', Action: 'task.finished', Source: 'system', State: 'interrupted', Name: 'Task interrupted' }));
  items.push(entry({ Id: '91', Severity: 'Error', Action: 'scan.finished', Source: 'system', State: 'failed', Name: 'Library scan failed' }));
  items.push(entry({ Id: '92', Severity: 'Error', Action: 'scan.finished', Source: 'system', State: 'failed', Date: '2026-11-16T18:30:00Z', Name: 'Older library scan failed' }));
  items.push(entry({ Id: '93', Severity: 'Debug', Name: 'Debug diagnostic' }));
  const queries = installActivity(api, items);
  const panel = await openActivity(page);
  const table = panel.getByRole('table', { name: 'Activity records' });
  const severity = panel.getByRole('group', { name: 'Severity' });
  const action = panel.getByRole('combobox', { name: 'Action' });
  const period = panel.getByRole('combobox', { name: 'Time range' });
  const search = panel.getByRole('searchbox', { name: 'Find on this page' });
  await expect(table.getByRole('columnheader')).toHaveText(['Time', 'Severity', 'Source', 'Message', 'Actor']);
  // The totals count all retained server results, including Debug under All.
  for (const [label, count] of [['All', 55], ['Error', 2], ['Warning', 1], ['Info', 51]] as const) {
    await expect(severity.getByRole('button', { name: label, exact: true })).toHaveAccessibleDescription(`Activity count: ${count}`);
  }
  expect(queries.filter((query) => query.get('Limit') === '1').map((query) => query.get('Severity')).sort()).toEqual([null, 'Error', 'Info', 'Warn'].sort());
  expect(await action.getByRole('option').evaluateAll((options) => options.map((option) => (option as HTMLOptionElement).value))).toEqual([
    '', 'settings.updated', 'user.updated', 'user.created', 'scan.finished', 'task.finished', 'session.login', 'playback.started',
  ]);
  const reads = queries.length;
  await search.fill('Second-page marker');
  await expect(panel.getByText('No matching activity on this page.')).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Export current page' })).toBeDisabled();
  expect(queries).toHaveLength(reads);
  await panel.getByRole('button', { name: 'Go to next page' }).click();
  await expect(table.getByText('Second-page marker', { exact: true })).toBeVisible();
  await search.clear();
  await action.selectOption('scan.finished');
  const minDate = await page.evaluate(() => { const date = new Date(); date.setHours(0, 0, 0, 0); return date.toISOString(); });
  await period.selectOption('today');
  await severity.getByRole('button', { name: 'Error', exact: true }).click();
  await expect(table.getByRole('row')).toHaveCount(2);
  await expect(table.getByRole('row').filter({ hasText: 'Library scan failed' }).getByRole('cell').nth(2)).toHaveText('System');
  await expect(severity.getByRole('button', { name: 'All', exact: true })).toHaveAccessibleDescription('Activity count: 1');
  expect(queries.some((query) => query.get('Limit') === '50' && query.get('StartIndex') === '0'
    && query.get('Severity') === 'Error' && query.get('Action') === 'scan.finished' && query.get('MinDate') === minDate)).toBe(true);
  await period.selectOption('week');
  await expect.poll(() => queries.some((query) => query.get('Limit') === '50' && query.get('MinDate') === '2026-11-18T18:30:00.000Z')).toBe(true);
  await expect(panel.getByText(/Activity is retained for 45 days and includes sign-ins and playback records/)).toBeVisible();
  await expect(panel.getByRole('link', { name: 'Users', exact: true })).toHaveAttribute('href', '/admin/access/users');
  expect(api.writes()).toHaveLength(0);
});

test('playback start and stop mask the title and item identity in the table, details, clipboard source and page export', async ({ page, api }) => {
  const signIn = entry({ Id: '3', Action: 'session.login', Name: 'Session issued', Resource: { Kind: 'session', Id: 'fixture-session' } });
  installActivity(api, [playback('1', 'playback.started'), playback('2', 'playback.stopped'), signIn]);
  const panel = await openActivity(page);
  const table = panel.getByRole('table', { name: 'Activity records' });
  const start = table.getByRole('button', { name: 'Activity details: Playback started' });
  await expect(start).toBeVisible();
  await expect(table.getByRole('button', { name: 'Activity details: Playback stopped' })).toBeVisible();
  await expect(table.getByRole('button', { name: 'Activity details: Session issued' })).toBeVisible();
  await expect(table.getByRole('cell', { name: 'User', exact: true })).toHaveCount(3);
  await expect(table).not.toContainText(privateTitle);
  // Observe the exact text that the copy control submits, without clipboard permissions.
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async (value: string) => { document.documentElement.dataset.copiedActivity = value; },
    } });
  });
  await start.click();
  const detail = page.getByRole('dialog', { name: 'Activity details' });
  await expect(detail.getByLabel('Activity JSON')).toContainText('playback.started');
  await expect(detail).not.toContainText(privateTitle);
  await expect(detail).not.toContainText(privateItemId);
  await detail.getByRole('button', { name: 'Copy JSON' }).click();
  await expect(detail.getByRole('button', { name: 'Copied' })).toBeVisible();
  const copied = (await page.locator('html').getAttribute('data-copied-activity'))!;
  expect(JSON.parse(copied)).toMatchObject({ Action: 'playback.started', Resource: { Kind: 'item' } });
  for (const marker of [privateTitle, privateItemId, ...privateFingerprints]) expect(copied).not.toContain(marker);
  await page.keyboard.press('Escape');
  await expect(start).toBeFocused();

  const search = panel.getByRole('searchbox', { name: 'Find on this page' });
  await search.fill(privateTitle);
  await expect(panel.getByText('No matching activity on this page.')).toBeVisible();
  await search.clear();
  await expect(start).toBeVisible();
  const exported = page.waitForEvent('download');
  await panel.getByRole('button', { name: 'Export current page' }).click();
  const download = await exported;
  try {
    expect(download.suggestedFilename()).toBe('goby-activity-page.jsonl');
    const stream = await download.createReadStream();
    const parts: Buffer[] = [];
    for await (const chunk of stream) parts.push(Buffer.from(chunk));
    const output = Buffer.concat(parts).toString('utf8');
    for (const marker of [privateTitle, privateItemId, ...privateFingerprints]) expect(output).not.toContain(marker);
    const rows = output.split('\n').map((line) => JSON.parse(line));
    expect(rows.map((row) => row.Action)).toEqual(['playback.started', 'playback.stopped', 'session.login']);
    expect(rows[2].Resource).toEqual({ Kind: 'session', Id: 'fixture-session' });
  } finally { await download.delete(); }
  expect(api.writes()).toHaveLength(0);
});

test.describe('Chinese Activity on a narrow screen', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('shows translated controls and keeps all five columns in a scrollable table', async ({ page, api }) => {
    installActivity(api, [playback('1', 'playback.started'), entry()]);
    await page.goto('/admin/system/observability');
    await expect(page.getByRole('combobox', { name: '时间范围' })).toBeVisible();
    await expect(page.getByRole('searchbox', { name: '在当前页中查找' })).toBeVisible();
    const table = page.getByRole('table', { name: '活动记录' });
    await expect(table.getByRole('columnheader')).toHaveText(['时间', '级别', '来源', '消息', '操作者']);
    await expect(table.getByRole('button', { name: '活动详情：开始播放' })).toBeVisible();
    await noOverflow(page);
  });
});
