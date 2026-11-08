import type { Page } from '@playwright/test';
import type { MetadataItemSummary } from '../src/api';
import type { PlaybackCommandInput, PlaybackSession } from '../src/playbackApi';
import type { DashboardAPI } from './dashboard-fixture';
import { administrator, csrf, expect, json, library, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

// These explicit fixtures establish browser behavior and request safety. They
// do not claim that a compatible player received or executed a remote command.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const sessionId = 'a4'.repeat(16);
const playingId = `play_${'41'.repeat(16)}`;
const commandPath = `/admin/v1/playback/sessions/${sessionId}/command`;
const acceptedAt = new Date(Date.parse(stamp) + 1000).toISOString();
const laterReport = new Date(Date.parse(stamp) + 2000).toISOString();

function player(overrides: Partial<PlaybackSession> = {}): PlaybackSession {
  return {
    Id: sessionId, UserId: administrator.Id, UserName: 'Family viewer', Client: 'Goby player',
    DeviceId: 'living-room-player', DeviceName: 'Living room display', ApplicationVersion: '0.1.0',
    LastActivityDate: stamp, RemoteEndPoint: '192.0.2.41', TransportConnected: true, SupportsRemoteControl: true,
    SupportedCommands: ['DisplayMessage', 'PlayNext', 'PlayLast'], PlayableMediaTypes: ['Video', 'Audio'],
    Controls: { Stop: true, Pause: true, Unpause: true, Seek: true, Message: true, Play: true },
    PlaySessionId: playingId, PlaybackUpdatedAt: stamp,
    NowPlayingItem: {
      Id: 'film-one', Name: 'A feature in progress', Type: 'Movie', RunTimeTicks: '72000000000', Container: 'mkv',
      MediaStreams: [{ Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 }, { Type: 'Audio', Codec: 'aac' }],
    },
    PlayState: { IsPaused: false, IsMuted: false, CanSeek: true, PositionTicks: '1200000000', PlayMethod: 'DirectPlay', MediaSourceId: 'source-one' },
    ...overrides,
  };
}

function installPlayback(api: DashboardAPI, sessions: () => PlaybackSession[]) {
  api.handlers.set('GET /admin/v1/playback/sessions', (route) => json(route, { Items: sessions(), ObservedAt: stamp, Limit: 256, AtCapacity: false }));
}

function queueCommands(api: DashboardAPI) {
  api.handlers.set(`POST ${commandPath}`, (route, request) => {
    const body = request.postDataJSON() as PlaybackCommandInput;
    return json(route, { SessionId: sessionId, Command: body.Command, Status: 'queued', DeliveryCount: 1, ExecutionConfirmed: false, AcceptedAt: acceptedAt }, 202);
  });
}

function installPlayablePair(api: DashboardAPI) {
  const items: MetadataItemSummary[] = [
    { Id: 'first-film', LibraryId: library.Id, ParentId: '', ParentName: '', Name: 'The first feature', Type: 'Movie', Path: '/media/movies/First.mkv', IsFolder: false, ProductionYear: 2024, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 },
    { Id: 'second-film', LibraryId: library.Id, ParentId: '', ParentName: '', Name: 'The second feature', Type: 'Movie', Path: '/media/movies/Second.mkv', IsFolder: false, ProductionYear: 2025, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 },
  ];
  api.handlers.set(`GET /admin/v1/libraries/${library.Id}/items`, (route, request) => {
    const url = new URL(request.url());
    const startIndex = Number(url.searchParams.get('StartIndex') ?? 0), limit = Number(url.searchParams.get('Limit') ?? 25);
    return json(route, { Library: library, Items: items.slice(startIndex, startIndex + limit), TotalRecordCount: items.length, StartIndex: startIndex, Limit: limit });
  });
}

const playbackRow = (page: Page, id = sessionId) => page.locator(`[data-playback-session="${id}"]`);

async function openSessions(page: Page) {
  await page.goto('/admin/access/sessions');
  await expect(page.getByRole('heading', { name: 'Sessions', exact: true })).toBeVisible();
  await expect(playbackRow(page)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Refresh playback', exact: true })).toBeEnabled();
}

async function refreshPlayback(page: Page) {
  const response = page.waitForResponse((result) => result.request().method() === 'GET' && new URL(result.url()).pathname === '/admin/v1/playback/sessions');
  await page.getByRole('button', { name: 'Refresh playback', exact: true }).click();
  expect((await response).ok()).toBe(true);
}

test('playback cards show reported state and command acceptance does not pretend the player paused', async ({ page, api }, testInfo) => {
  let current = player();
  installPlayback(api, () => [current]); queueCommands(api);
  await openSessions(page);
  const row = playbackRow(page);
  await expect(row).toContainText('A feature in progress');
  await expect(row).toContainText('Direct play');
  await expect(row).toContainText('H264 · 1920 × 1080 · AAC · MKV');
  await expect(row.getByRole('progressbar', { name: 'Playback progress', exact: true })).toHaveAttribute('aria-valuetext', '2:00 / 2:00:00');
  await row.getByRole('button', { name: 'Pause playback', exact: true }).click();
  await expect(page.getByText('Command sent. The player has not confirmed execution.', { exact: true })).toBeVisible();
  await expect(row.getByRole('button', { name: 'Pause playback', exact: true })).toBeDisabled();
  await expect(row.getByRole('button', { name: 'Resume playback', exact: true })).toHaveCount(0);
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'Pause', PlaySessionId: playingId }, csrf }]);

  current = { ...current, PlaybackUpdatedAt: laterReport, PlayState: { ...current.PlayState, IsPaused: true } };
  await refreshPlayback(page);
  await expect(row.getByRole('button', { name: 'Resume playback', exact: true })).toBeEnabled();
  await expect(row).toContainText('Paused');
  expect(api.writes()).toHaveLength(1);
  await noOverflow(page);
  await evidence(page, testInfo, 'playback-reported-pause-after-queued-command');
});

test('stopping playback requires confirmation and binds the request to the selected play session', async ({ page, api }, testInfo) => {
  let current = player();
  installPlayback(api, () => [current]); queueCommands(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByRole('button', { name: 'Stop', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Stop this playback?', exact: true });
  await expect(confirmation).toContainText('Playback stays visible until it reports that it has stopped.');
  expect(api.writes()).toHaveLength(0);
  await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  await row.getByRole('button', { name: 'Stop', exact: true }).click();
  await confirmation.getByRole('button', { name: 'Request stop', exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  await expect(row).toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'Stop', PlaySessionId: playingId }, csrf }]);

  current = { ...current, NowPlayingItem: undefined, PlaySessionId: undefined, PlaybackUpdatedAt: laterReport, Controls: { ...current.Controls, Stop: false, Pause: false, Unpause: false, Seek: false } };
  await refreshPlayback(page);
  await expect(row).toHaveCount(0);
  await expect(page.getByText('Nobody is playing right now', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  await evidence(page, testInfo, 'playback-stop-confirmed-by-client-report');
});

test('a different reported play session invalidates an already open stop confirmation', async ({ page, api }, testInfo) => {
  let current = player();
  installPlayback(api, () => [current]); queueCommands(api);
  await openSessions(page);
  await playbackRow(page).getByRole('button', { name: 'Stop', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Stop this playback?', exact: true });
  await expect(confirmation.getByRole('button', { name: 'Request stop', exact: true })).toBeEnabled();

  // Keep the dialog open while the ordinary polling path reports a new title.
  const nextReport = page.waitForResponse((response) => response.request().method() === 'GET'
    && new URL(response.url()).pathname === '/admin/v1/playback/sessions');
  current = { ...current, PlaySessionId: `play_${'42'.repeat(16)}`, PlaybackUpdatedAt: laterReport,
    NowPlayingItem: { ...current.NowPlayingItem!, Id: 'film-two', Name: 'Another feature in progress' } };
  expect((await nextReport).ok()).toBe(true);
  await expect(confirmation.getByText('Playback changed while these controls were open. Close this dialog and review the current title.', { exact: true })).toBeVisible();
  await expect(confirmation.getByRole('button', { name: 'Request stop', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(0);
  await evidence(page, testInfo, 'playback-stop-confirmation-invalidated-by-new-title');
  await confirmation.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(playbackRow(page)).toContainText('Another feature in progress');
  expect(api.writes()).toHaveLength(0);
});

test('disconnected players retain their last report and expose no usable remote controls', async ({ page, api }, testInfo) => {
  installPlayback(api, () => [player({ TransportConnected: false, SupportsRemoteControl: false, Controls: { Stop: false, Pause: false, Unpause: false, Seek: false, Message: false, Play: false } })]);
  await openSessions(page);
  const row = playbackRow(page);
  await expect(row).toContainText('Disconnected');
  await expect(row.getByRole('button', { name: 'Pause playback', exact: true })).toBeDisabled();
  await expect(row.getByRole('button', { name: 'Message', exact: true })).toBeDisabled();
  await expect(row.getByRole('button', { name: 'Stop', exact: true })).toBeDisabled();
  await row.getByText('More playback controls', { exact: true }).click();
  await expect(row.getByRole('button', { name: 'Seek to time', exact: true })).toBeDisabled();
  await expect(row.getByRole('button', { name: 'Play media on this device', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Message all players', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(0);
  await noOverflow(page);
  await evidence(page, testInfo, 'playback-disconnected-controls-disabled');
});

test('message broadcasting reports queued and skipped sessions without claiming display confirmation', async ({ page, api }, testInfo) => {
  const offlineId = 'b5'.repeat(16), unsupportedId = 'c6'.repeat(16);
  installPlayback(api, () => [
    player(),
    player({ Id: offlineId, DeviceName: 'Disconnected television', TransportConnected: false, Controls: { Stop: false, Pause: false, Unpause: false, Seek: false, Message: false, Play: false } }),
    player({ Id: unsupportedId, DeviceName: 'Player without messages', Controls: { Stop: true, Pause: true, Unpause: true, Seek: true, Message: false, Play: true } }),
  ]);
  api.handlers.set('POST /admin/v1/playback/message', (route) => json(route, {
    AcceptedAt: acceptedAt,
    Results: [
      { SessionId: sessionId, Status: 'queued', DeliveryCount: 1, ExecutionConfirmed: false },
      { SessionId: offlineId, Status: 'offline', DeliveryCount: 0, ExecutionConfirmed: false },
      { SessionId: unsupportedId, Status: 'unsupported', DeliveryCount: 0, ExecutionConfirmed: false },
    ],
  }, 202));
  await openSessions(page);
  await page.getByRole('button', { name: 'Message all players', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Message all players', exact: true });
  await dialog.getByRole('textbox', { name: 'Message', exact: true }).fill('Maintenance begins in five minutes.');
  await dialog.getByRole('button', { name: 'Send message', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText('Messages queued: 1. Players skipped: 2. Sending does not confirm display.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: '/admin/v1/playback/message', body: { Text: 'Maintenance begins in five minutes.', Header: 'Goby', TimeoutMs: 10000 }, csrf }]);
  await evidence(page, testInfo, 'playback-message-broadcast-partial-delivery');
});

test('an unknown command result blocks repeat commands until an explicit review finishes', async ({ page, api }, testInfo) => {
  let holdReview = false;
  let releaseReview = () => {};
  const reviewResult = new Promise<void>((resolve) => { releaseReview = resolve; });
  api.handlers.set('GET /admin/v1/playback/sessions', async (route) => {
    if (holdReview) await reviewResult;
    await json(route, { Items: [player()], ObservedAt: stamp, Limit: 256, AtCapacity: false });
  });
  api.handlers.set(`POST ${commandPath}`, (route) => json(route, { Error: { Code: 'command_unconfirmed', Message: 'The command response could not be confirmed.' } }, 503));
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByRole('button', { name: 'Stop', exact: true }).click();
  await page.getByRole('dialog', { name: 'Stop this playback?', exact: true }).getByRole('button', { name: 'Request stop', exact: true }).click();
  await expect(page.getByText('The command result is unknown. Review current playback before sending another command.', { exact: true })).toBeVisible();
  await expect(row.getByRole('button', { name: 'Stop', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  holdReview = true;
  try {
    const request = page.waitForRequest((value) => value.method() === 'GET' && new URL(value.url()).pathname === '/admin/v1/playback/sessions');
    await page.getByRole('button', { name: 'Review playback state', exact: true }).click();
    await request;
    await expect(row.getByRole('button', { name: 'Stop', exact: true })).toBeDisabled();
    expect(api.writes()).toHaveLength(1);
  } finally { releaseReview(); }
  await expect(row.getByRole('button', { name: 'Stop', exact: true })).toBeEnabled();
  await expect(page.getByText('The command result is unknown. Review current playback before sending another command.', { exact: true })).not.toBeVisible();
  await refreshPlayback(page);
  expect(api.writes()).toHaveLength(1);
  await evidence(page, testInfo, 'playback-unknown-command-reviewed-without-replay');
});

test('seeking validates the duration and serializes tick values without numeric precision loss', async ({ page, api }, testInfo) => {
  const current = player({ NowPlayingItem: { ...player().NowPlayingItem!, RunTimeTicks: '9223372036854775807' } });
  installPlayback(api, () => [current]); queueCommands(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByText('More playback controls', { exact: true }).click();
  await row.getByRole('button', { name: 'Seek to time', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Seek to time', exact: true });
  const time = dialog.getByRole('textbox', { name: 'Playback time', exact: true });
  await time.fill('1:90:00');
  await dialog.getByRole('button', { name: 'Request seek', exact: true }).click();
  await expect(time).toHaveAttribute('aria-invalid', 'true');
  expect(api.writes()).toHaveLength(0);
  await time.fill('250200:00:01');
  await dialog.getByRole('button', { name: 'Request seek', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'Seek', SeekPositionTicks: '9007200010000000', PlaySessionId: playingId }, csrf }]);
  await evidence(page, testInfo, 'playback-seek-preserves-canonical-ticks');
});

test('seeking beyond the reported duration never sends a command', async ({ page, api }) => {
  installPlayback(api, () => [player()]); queueCommands(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByText('More playback controls', { exact: true }).click();
  await row.getByRole('button', { name: 'Seek to time', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Seek to time', exact: true });
  await dialog.getByRole('textbox', { name: 'Playback time', exact: true }).fill('2:00:01');
  await dialog.getByRole('button', { name: 'Request seek', exact: true }).click();
  await expect(dialog.getByRole('textbox', { name: 'Playback time', exact: true })).toHaveAttribute('aria-invalid', 'true');
  expect(api.writes()).toHaveLength(0);
});

for (const mode of ['PlayNow', 'PlayNext', 'PlayLast'] as const) {
  test(`choosing media sends ${mode} to the selected player`, async ({ page, api }, testInfo) => {
    installPlayback(api, () => [player()]); queueCommands(api);
    await openSessions(page);
    const row = playbackRow(page);
    await row.getByText('More playback controls', { exact: true }).click();
    await row.getByRole('button', { name: 'Play media on this device', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Play media on this device', exact: true });
    await dialog.getByRole('combobox', { name: 'Queue action', exact: true }).selectOption(mode);
    await dialog.getByRole(mode === 'PlayNow' ? 'radio' : 'checkbox', { name: /The feature film/ }).check();
    await dialog.getByRole('button', { name: 'Send to player', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: mode, ItemIds: ['film-one'] }, csrf }]);
    await evidence(page, testInfo, `playback-media-${mode.toLowerCase()}`);
  });
}

test('a player without queue declarations offers play now while disabling queue insertion', async ({ page, api }, testInfo) => {
  installPlayback(api, () => [player({ SupportedCommands: ['DisplayMessage'] })]); queueCommands(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByText('More playback controls', { exact: true }).click();
  await row.getByRole('button', { name: 'Play media on this device', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Play media on this device', exact: true });
  const mode = dialog.getByRole('combobox', { name: 'Queue action', exact: true });
  await expect(mode).toHaveValue('PlayNow');
  await expect(mode.getByRole('option', { name: 'Play now', exact: true })).toHaveJSProperty('disabled', false);
  await expect(mode.getByRole('option', { name: 'Play next', exact: true })).toHaveJSProperty('disabled', true);
  await expect(mode.getByRole('option', { name: 'Add to queue', exact: true })).toHaveJSProperty('disabled', true);
  await mode.focus();
  await page.keyboard.press('ArrowDown');
  await expect(mode).toHaveValue('PlayNow');
  await page.keyboard.press('End');
  await expect(mode).toHaveValue('PlayNow');
  expect(api.writes()).toHaveLength(0);
  await dialog.getByRole('radio', { name: /The feature film/ }).check();
  await evidence(page, testInfo, 'playback-client-without-queue-capabilities');
  await dialog.getByRole('button', { name: 'Send to player', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'PlayNow', ItemIds: ['film-one'] }, csrf }]);
});

test('play now replaces the previous selection and sends exactly one media item', async ({ page, api }, testInfo) => {
  installPlayback(api, () => [player()]); queueCommands(api); installPlayablePair(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByText('More playback controls', { exact: true }).click();
  await row.getByRole('button', { name: 'Play media on this device', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Play media on this device', exact: true });
  const first = dialog.getByRole('radio', { name: /The first feature/ });
  const second = dialog.getByRole('radio', { name: /The second feature/ });
  await first.check();
  await expect(first).toBeChecked();
  await second.check();
  await expect(second).toBeChecked();
  await expect(first).not.toBeChecked();
  const selected = dialog.getByRole('list', { name: 'Selected media', exact: true });
  await expect(selected).toContainText('The second feature');
  await expect(selected).not.toContainText('The first feature');
  await evidence(page, testInfo, 'playback-play-now-single-selection');
  await dialog.getByRole('button', { name: 'Send to player', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'PlayNow', ItemIds: ['second-film'] }, csrf }]);
});

test('switching a multiple-item queue draft to play now keeps the first item with an explicit notice', async ({ page, api }, testInfo) => {
  installPlayback(api, () => [player()]); queueCommands(api); installPlayablePair(api);
  await openSessions(page);
  const row = playbackRow(page);
  await row.getByText('More playback controls', { exact: true }).click();
  await row.getByRole('button', { name: 'Play media on this device', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Play media on this device', exact: true });
  const mode = dialog.getByRole('combobox', { name: 'Queue action', exact: true });
  await mode.selectOption('PlayNext');
  await dialog.getByRole('checkbox', { name: /The first feature/ }).check();
  await dialog.getByRole('checkbox', { name: /The second feature/ }).check();
  await expect(dialog.getByRole('list', { name: 'Selected media', exact: true }).getByRole('listitem')).toHaveCount(2);
  await mode.selectOption('PlayNow');
  await expect(dialog.getByText('Play now supports one item. The first selected item has been kept.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('radio', { name: /The first feature/ })).toBeChecked();
  await expect(dialog.getByRole('radio', { name: /The second feature/ })).not.toBeChecked();
  const selected = dialog.getByRole('list', { name: 'Selected media', exact: true });
  await expect(selected.getByRole('listitem')).toHaveCount(1);
  await expect(selected).toContainText('The first feature');
  await evidence(page, testInfo, 'playback-queue-draft-reduced-to-play-now');
  await dialog.getByRole('button', { name: 'Send to player', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.writes()).toEqual([{ method: 'POST', path: commandPath, body: { Command: 'PlayNow', ItemIds: ['first-film'] }, csrf }]);
});

test('a failed refresh preserves the last report and pauses controls until a new snapshot succeeds', async ({ page, api }, testInfo) => {
  let failed = false;
  api.handlers.set('GET /admin/v1/playback/sessions', (route) => failed
    ? json(route, { Error: { Code: 'playback_unavailable', Message: 'Playback is temporarily unavailable.' } }, 503)
    : json(route, { Items: [player()], ObservedAt: stamp, Limit: 256, AtCapacity: false }));
  await openSessions(page);
  failed = true;
  await page.getByRole('button', { name: 'Refresh playback', exact: true }).click();
  await expect(page.getByText('Showing the last successful update. Playback controls are paused.', { exact: true })).toBeVisible();
  await expect(playbackRow(page)).toContainText('A feature in progress');
  await expect(playbackRow(page).getByRole('button', { name: 'Stop', exact: true })).toBeDisabled();
  failed = false;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(playbackRow(page).getByRole('button', { name: 'Stop', exact: true })).toBeEnabled();
  expect(api.writes()).toHaveLength(0);
  await evidence(page, testInfo, 'playback-refresh-recovers-without-losing-last-report');
});

test.describe('Chinese playback on a narrow screen', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' }, viewport: { width: 390, height: 844 } });

  test('keyboard message editing preserves raw names, unsaved text, and focus', async ({ page, api }, testInfo) => {
    const current = player({ UserName: 'Message', DeviceName: 'Connected', NowPlayingItem: { ...player().NowPlayingItem!, Name: 'Pause playback' } });
    installPlayback(api, () => [current]);
    await page.goto('/admin/access/sessions');
    const row = playbackRow(page);
    await expect(row).toBeVisible();
    await expect(row.locator('.playback-title strong')).toHaveText('Pause playback');
    await expect(row.locator('.playback-client > strong')).toHaveText('Message');
    await expect(row.locator('.playback-client > span:last-child')).toContainText('Connected');
    const trigger = row.getByRole('button', { name: '消息', exact: true });
    await trigger.focus();
    await page.keyboard.press('Enter');
    const dialog = page.getByRole('dialog', { name: '向此播放器发送消息', exact: true });
    const message = dialog.getByRole('textbox', { name: '消息', exact: true });
    await expect(message).toBeFocused();
    await expect(dialog.getByRole('button', { name: '发送消息', exact: true })).toBeVisible();
    await message.fill('A message still being edited.');
    page.once('dialog', (confirmation) => confirmation.dismiss());
    await page.keyboard.press('Escape');
    await expect(dialog).toBeVisible();
    await expect(message).toHaveValue('A message still being edited.');
    await noOverflow(page);
    await evidence(page, testInfo, 'playback-mobile-zh-dark-message-keyboard');
    page.once('dialog', (confirmation) => confirmation.accept());
    await dialog.getByRole('button', { name: '取消', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();
    expect(api.writes()).toHaveLength(0);
    await noOverflow(page);
    await evidence(page, testInfo, 'playback-mobile-zh-dark-reported-sessions');
  });
});
