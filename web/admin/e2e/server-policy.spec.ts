import type { Locator, Page } from '@playwright/test';
import type { ServerPolicy, ServerPolicyInput } from '../src/serverPolicyApi';
import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

const policyPath = '/admin/v1/server-policy';
const previewPath = `${policyPath}/preview`;
const restartPath = '/admin/v1/server/restart';
const playbackPath = '/admin/v1/playback/sessions';
const messagePath = '/admin/v1/playback/message';
const initialRevision = '9007199254740993';
const privatePlayingTitle = 'Maintenance fixture hidden media title';
const reminder = 'The server will restart in 5 minutes. Playback will be briefly interrupted.';

function installPlayback(api: DashboardAPI, playing: number, atCapacity = false) {
  const count = atCapacity ? 256 : playing + 1;
  const items = Array.from({ length: count }, (_, index) => ({
    Id: `client-${index}`, DeviceName: `Hidden device ${index}`, UserName: `Hidden viewer ${index}`,
    ...(index < playing ? { NowPlayingItem: { Id: 'hidden-item', Name: privatePlayingTitle, Type: 'Movie' } } : {}),
  }));
  api.handlers.set(`GET ${playbackPath}`, (route, request) => {
    expect(new URL(request.url()).search).toBe('');
    return json(route, { Items: items, TotalRecordCount: count, ObservedAt: stamp, Limit: 256, AtCapacity: atCapacity });
  });
}

function messageReceipt(dispositions: { status: 'queued' | 'offline' | 'unsupported' | 'unavailable'; deliveryCount: number }[]) {
  return { AcceptedAt: stamp, Results: dispositions.map(({ status, deliveryCount }, index) => ({
    SessionId: `unlisted-receipt-${index}`, Command: 'DisplayMessage', Status: status, DeliveryCount: deliveryCount, ExecutionConfirmed: false,
    AcceptedAt: status === 'queued' ? stamp : '0001-01-01T00:00:00Z',
  })) };
}

async function openMessage(page: Page) {
  await page.getByRole('region', { name: 'Server maintenance', exact: true }).getByRole('button', { name: 'Send message', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Send a message to playing clients', exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}

function fixturePolicy(): ServerPolicy {
  return {
    Revision: initialRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'],
    UICulture: 'zh-CN', UpdatedAt: stamp, Client: { Address: '203.0.113.17', IsLocal: false },
    Restart: { Available: true, InProgress: false },
    Updates: { CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: false, CurrentVersion: '0.1.0-policy-fixture' },
  };
}

function applyPolicy(saved: ServerPolicy, input: ServerPolicyInput): ServerPolicy {
  return {
    ...saved, Revision: (BigInt(input.Revision) + 1n).toString(), EnableRemoteAccess: input.EnableRemoteAccess,
    LocalNetworkSubnets: [...input.LocalNetworkSubnets], UICulture: input.UICulture,
  };
}

function installPolicy(api: DashboardAPI) {
  const state = { policy: fixturePolicy() };
  api.handlers.set(`GET ${policyPath}`, (route) => json(route, state.policy));
  api.handlers.set(`POST ${previewPath}`, (route, request) => {
    const input = request.postDataJSON() as ServerPolicyInput;
    expect(input.Revision).toBe(state.policy.Revision);
    return json(route, { Revision: input.Revision, WouldDisconnect: !input.EnableRemoteAccess, Client: state.policy.Client });
  });
  api.handlers.set(`PUT ${policyPath}`, (route, request) => {
    const input = request.postDataJSON() as ServerPolicyInput & { ConfirmDisconnect: boolean };
    expect(input.Revision).toBe(state.policy.Revision);
    state.policy = applyPolicy(state.policy, input);
    return json(route, { ...state.policy, DisconnectExpected: !input.EnableRemoteAccess });
  });
  api.handlers.set(`POST ${restartPath}`, (route) => json(route, { Accepted: true, Restarting: true }, 202));
  return state;
}

function changes(api: DashboardAPI, method: string, path: string) {
  return api.writes().filter((request) => request.method === method && request.path === path);
}

async function openPolicy(page: Page) {
  await page.goto('/admin/settings/general');
  await expect(page.getByRole('region', { name: 'Remote access and local networks', exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Default interface language', exact: true })).toBeEnabled();
}

async function dialogFitsViewport(dialog: Locator) {
  expect(await dialog.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    return bounds.left >= 0 && bounds.right <= innerWidth + 1 && element.scrollWidth <= element.clientWidth + 1;
  }), 'The confirmation must fit the viewport without horizontal scrolling.').toBe(true);
}

test('default language saves use preview and preserve the independent exact revision and CSRF', async ({ page, api }, testInfo) => {
  const state = installPolicy(api);
  await openPolicy(page);
  await expect(page.getByRole('combobox', { name: 'Default interface language', exact: true })).toHaveValue('zh-CN');
  await page.getByRole('combobox', { name: 'Default interface language', exact: true }).selectOption('en-US');
  await page.getByRole('button', { name: 'Save network policy', exact: true }).click();
  await expect(page.getByText('Network policy and default language saved.', { exact: true })).toBeVisible();

  const expected = { Revision: initialRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'], UICulture: 'en-US' };
  expect(api.writes()).toEqual([
    { method: 'POST', path: previewPath, body: expected, csrf },
    { method: 'PUT', path: policyPath, body: { ...expected, ConfirmDisconnect: false }, csrf },
  ]);
  expect(state.policy.Revision).toBe('9007199254740994');
  expect(changes(api, 'PUT', '/admin/v1/settings')).toEqual([]);
  await expect(page.getByRole('button', { name: 'Save network policy', exact: true })).toBeDisabled();
  await page.getByRole('region', { name: 'Remote access and local networks', exact: true }).scrollIntoViewIfNeeded();
  await noOverflow(page);
  await evidence(page, testInfo, 'server-policy-default-language-saved-en-light');
});

test('remote access changes require an explicit self-disconnection confirmation and a single write', async ({ page, api }, testInfo) => {
  installPolicy(api);
  await openPolicy(page);
  const remote = page.getByRole('switch', { name: 'Allow remote access', exact: true });
  await remote.uncheck();
  await page.getByRole('button', { name: 'Save network policy', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Disconnect this connection?', exact: true });
  await expect(confirmation).toBeVisible();
  await expect(confirmation).toContainText('These changes will block your current connection');
  expect(changes(api, 'POST', previewPath)).toHaveLength(1);
  expect(changes(api, 'PUT', policyPath)).toEqual([]);
  await evidence(page, testInfo, 'server-policy-self-disconnection-confirmation-en-light');
  await confirmation.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  await expect(remote).not.toBeChecked();
  expect(changes(api, 'PUT', policyPath)).toEqual([]);

  await page.getByRole('button', { name: 'Save network policy', exact: true }).click();
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole('button', { name: 'Save and disconnect', exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  await expect(page.getByText('The settings were saved. This connection is no longer allowed. Reconnect from a local network.', { exact: true })).toBeVisible();
  expect(changes(api, 'POST', previewPath)).toHaveLength(2);
  expect(changes(api, 'PUT', policyPath)).toEqual([{
    method: 'PUT', path: policyPath,
    body: { Revision: initialRevision, EnableRemoteAccess: false, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'], UICulture: 'zh-CN', ConfirmDisconnect: true }, csrf,
  }]);
  await expect(remote).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Save network policy', exact: true })).toBeDisabled();
  expect(changes(api, 'PUT', '/admin/v1/settings')).toEqual([]);
});

test('an unconfirmed policy write keeps the draft and blocks another write until saved state is explicitly reloaded', async ({ page, api }, testInfo) => {
  const state = installPolicy(api);
  api.handlers.set(`PUT ${policyPath}`, async (route, request) => {
    state.policy = applyPolicy(state.policy, request.postDataJSON() as ServerPolicyInput);
    await route.abort('connectionreset');
  });
  await openPolicy(page);
  const language = page.getByRole('combobox', { name: 'Default interface language', exact: true });
  const save = page.getByRole('button', { name: 'Save network policy', exact: true });
  await language.selectOption('en-US');
  await save.click();
  const review = page.getByText('The settings changed or the result could not be confirmed. Your draft is kept. Reload saved settings before trying another change.', { exact: true });
  await expect(review).toBeVisible();
  await expect(language).toHaveValue('en-US');
  await expect(language).toBeDisabled();
  await expect(save).toBeDisabled();
  expect(changes(api, 'PUT', policyPath)).toHaveLength(1);
  expect(changes(api, 'POST', previewPath)).toHaveLength(1);
  await evidence(page, testInfo, 'server-policy-unconfirmed-write-keeps-draft-en-light');

  let confirmations = 0;
  page.once('dialog', async (dialog) => {
    expect(dialog.type()).toBe('confirm');
    expect(dialog.message()).toBe('Discard your network policy draft and reload the saved settings?');
    confirmations += 1;
    await dialog.accept();
  });
  await page.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
  await expect(review).not.toBeVisible();
  await expect(language).toBeEnabled();
  await expect(language).toHaveValue('en-US');
  expect(confirmations).toBe(1);
  expect(changes(api, 'PUT', policyPath)).toHaveLength(1);
  await expect(save).toBeDisabled();

  api.handlers.set(`PUT ${policyPath}`, (route, request) => {
    const input = request.postDataJSON() as ServerPolicyInput;
    state.policy = applyPolicy(state.policy, input);
    return json(route, { ...state.policy, DisconnectExpected: false });
  });
  await language.selectOption('zh-CN');
  await save.click();
  await expect(page.getByText('Network policy and default language saved.', { exact: true })).toBeVisible();
  expect(changes(api, 'PUT', policyPath)).toHaveLength(2);
  expect(changes(api, 'PUT', policyPath)[1]).toMatchObject({ body: { Revision: '9007199254740994', UICulture: 'zh-CN', ConfirmDisconnect: false }, csrf });
  expect(changes(api, 'PUT', '/admin/v1/settings')).toEqual([]);
});

test('restart needs the exact confirmation word even when the playback count is unavailable', async ({ page, api }, testInfo) => {
  installPolicy(api);
  api.handlers.set(`GET ${playbackPath}`, (route) => json(route, { Error: { Code: 'remote_command_unavailable', Message: 'Fixture snapshot unavailable.' } }, 503));
  await openPolicy(page);
  await page.getByRole('button', { name: 'Restart server', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Restart server?', exact: true });
  await expect(confirmation.getByText('The number of playing clients could not be checked.')).toBeVisible();
  const restart = confirmation.getByRole('button', { name: 'Request restart', exact: true });
  const phrase = confirmation.getByRole('textbox', { name: 'Type the confirmation word RESTART', exact: true });
  await expect(restart).toBeDisabled();
  await phrase.fill('restart');
  await expect(restart).toBeDisabled();
  expect(changes(api, 'POST', restartPath)).toEqual([]);
  await phrase.fill('RESTART');
  await expect(restart).toBeEnabled();
  await evidence(page, testInfo, 'server-policy-restart-confirmation-en-light');
  await restart.click();
  await expect(confirmation).not.toBeVisible();
  await expect(page.getByText('Restart requested. The connection may close. Reconnect after the server starts.', { exact: true })).toBeVisible();
  expect(changes(api, 'POST', restartPath)).toEqual([{ method: 'POST', path: restartPath, body: { Revision: initialRevision, Confirm: true }, csrf }]);
  await expect(page.getByRole('button', { name: 'Restart server', exact: true })).toBeDisabled();
  await expect(page.getByRole('main')).not.toContainText(/restart completed|server restarted|restart successful/i);
  expect(changes(api, 'PUT', policyPath)).toEqual([]);
  expect(changes(api, 'PUT', '/admin/v1/settings')).toEqual([]);
});

test('maintenance sends a byte-validated message and summarizes client receipts, not the snapshot or queue count', async ({ page, api }) => {
  installPolicy(api);
  installPlayback(api, 4, true);
  api.handlers.set(`POST ${messagePath}`, (route) => json(route, messageReceipt([
    { status: 'queued', deliveryCount: 2 }, { status: 'queued', deliveryCount: 1 }, { status: 'unsupported', deliveryCount: 0 },
    { status: 'offline', deliveryCount: 0 }, { status: 'unavailable', deliveryCount: 0 },
  ]), 202));
  await openPolicy(page);
  const dialog = await openMessage(page);
  await expect(dialog.getByText('Playback snapshot: 4+ clients are playing.')).toBeVisible();
  await expect(dialog).not.toContainText(privatePlayingTitle);
  await expect(dialog).not.toContainText('Hidden viewer');
  await expect(dialog).not.toContainText('Hidden device');
  const header = dialog.getByRole('textbox', { name: 'Title (optional)', exact: true });
  const message = dialog.getByRole('textbox', { name: 'Message', exact: true });
  const duration = dialog.getByRole('combobox', { name: 'Display duration', exact: true });
  const send = dialog.getByRole('button', { name: 'Send message', exact: true });
  await expect(header).toBeFocused();
  await expect(duration).toHaveValue('10000');
  await expect(duration.getByRole('option')).toHaveText(['5 seconds', '10 seconds', '30 seconds', '60 seconds']);
  const maxHeader = '界'.repeat(42) + 'ab';
  const maxText = '界'.repeat(682) + 'ab';
  await header.fill(maxHeader + 'c');
  await message.fill('Ready for maintenance');
  await expect(dialog.getByText('The title must be at most 128 UTF-8 bytes.')).toBeVisible();
  await expect(send).toBeDisabled();
  await header.fill(maxHeader);
  await message.fill(maxText + 'c');
  await expect(dialog.getByText('The message must be at most 2,048 UTF-8 bytes.')).toBeVisible();
  await expect(send).toBeDisabled();
  await message.fill('Line one\nLine two');
  await expect(dialog.getByText('Use valid single-line text without control characters.')).toBeVisible();
  await expect(send).toBeDisabled();
  await dialog.getByRole('button', { name: 'Maintenance begins at 23:00 tonight and is expected to last 10 minutes.' }).click();
  await expect(message).toHaveValue('Maintenance begins at 23:00 tonight and is expected to last 10 minutes.');
  await message.fill(maxText);
  await duration.selectOption('60000');
  await expect(send).toBeEnabled();
  expect(api.writes()).toHaveLength(0);
  await send.click();
  await expect(dialog).not.toBeVisible();
  const maintenance = page.getByRole('region', { name: 'Server maintenance', exact: true });
  await expect(maintenance.getByRole('status')).toContainText('Queued for 2 clients. 1 client did not support or allow messages. 1 client was disconnected. 1 client was unavailable. Queuing does not confirm display on the client.');
  await expect(maintenance.getByRole('button', { name: 'Send message', exact: true })).toBeFocused();
  await expect(maintenance).not.toContainText('unlisted-receipt');
  expect(api.writes()).toEqual([{ method: 'POST', path: messagePath, body: { Text: maxText, Header: maxHeader, TimeoutMs: 60000 }, csrf }]);
});

test('send reminder first switches to a prefilled message and never submits or schedules a restart', async ({ page, api }) => {
  installPolicy(api);
  installPlayback(api, 1);
  api.handlers.set(`POST ${messagePath}`, (route) => json(route, messageReceipt([{ status: 'queued', deliveryCount: 1 }]), 202));
  await openPolicy(page);
  const trigger = page.getByRole('button', { name: 'Restart server', exact: true });
  await trigger.click();
  const confirmRestart = page.getByRole('dialog', { name: 'Restart server?', exact: true });
  await expect(confirmRestart.getByText('1 client is currently playing.')).toBeVisible();
  const phrase = confirmRestart.getByRole('textbox', { name: 'Type the confirmation word RESTART', exact: true });
  await phrase.fill('RESTART');
  await confirmRestart.getByRole('button', { name: 'Send reminder first', exact: true }).click();
  await expect(confirmRestart).not.toBeVisible();
  const dialog = page.getByRole('dialog', { name: 'Send a message to playing clients', exact: true });
  const header = dialog.getByRole('textbox', { name: 'Title (optional)', exact: true });
  await expect(header).toBeFocused();
  await expect(header).toHaveValue('Server maintenance');
  await expect(dialog.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue(reminder);
  await expect(dialog.getByRole('combobox', { name: 'Display duration', exact: true })).toHaveValue('30000');
  await expect(dialog.getByText(/does not schedule a restart/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Send message', exact: true }).click();
  await expect(trigger).toBeFocused();
  expect(api.writes()).toEqual([{ method: 'POST', path: messagePath, body: { Text: reminder, Header: 'Server maintenance', TimeoutMs: 30000 }, csrf }]);
  await trigger.click();
  await expect(phrase).toHaveValue('');
  await expect(confirmRestart.getByRole('button', { name: 'Request restart', exact: true })).toBeDisabled();
  expect(changes(api, 'POST', restartPath)).toHaveLength(0);
});

test('definite rejection preserves the draft without an unknown-outcome gate', async ({ page, api }) => {
  installPolicy(api);
  installPlayback(api, 2);
  api.handlers.set(`POST ${messagePath}`, (route) => json(route, { Error: { Code: 'invalid_remote_command', Message: 'Fixture message rejected.' } }, 400));
  await openPolicy(page);
  const dialog = await openMessage(page);
  const message = dialog.getByRole('textbox', { name: 'Message', exact: true });
  const send = dialog.getByRole('button', { name: 'Send message', exact: true });
  await message.fill('Maintenance starts soon.');
  await send.click();
  await expect(dialog.getByRole('alert')).toContainText('The server rejected the message. Nothing was queued.');
  await expect(dialog.getByText(/message result is unknown/)).toHaveCount(0);
  await expect(message).toHaveValue('Maintenance starts soon.');
  await expect(send).toBeEnabled();
  expect(changes(api, 'POST', messagePath)).toHaveLength(1);
  page.once('dialog', (confirmation) => { expect(confirmation.message()).toBe('Discard this maintenance message draft?'); void confirmation.dismiss(); });
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(message).toHaveValue('Maintenance starts soon.');
  page.once('dialog', (confirmation) => { void confirmation.accept(); });
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).not.toBeVisible();
});

test('an unconfirmed send requires a successful explicit refresh and never automatically replays', async ({ page, api }) => {
  installPolicy(api);
  installPlayback(api, 3);
  api.handlers.set(`POST ${messagePath}`, (route) => json(route, { AcceptedAt: stamp, Results: [
    { SessionId: 'mismatched-receipt', Command: 'DisplayMessage', Status: 'queued', DeliveryCount: 0, ExecutionConfirmed: false, AcceptedAt: stamp },
  ] }, 202));
  await openPolicy(page);
  const dialog = await openMessage(page);
  const message = dialog.getByRole('textbox', { name: 'Message', exact: true });
  const send = dialog.getByRole('button', { name: 'Send message', exact: true });
  const review = dialog.getByRole('button', { name: 'Refresh playback count', exact: true });
  await message.fill('Maintenance starts soon.');
  await send.click();
  await expect(dialog.getByText(/message result is unknown/)).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: 'Message', exact: true })).toBeDisabled();
  await expect(send).toBeDisabled();
  expect(changes(api, 'POST', messagePath)).toHaveLength(1);

  api.handlers.set(`GET ${playbackPath}`, (route) => json(route, { Error: { Code: 'remote_command_unavailable', Message: 'Fixture snapshot unavailable.' } }, 503));
  await review.click();
  await expect(dialog.getByText('The number of playing clients could not be checked.')).toBeVisible();
  await expect(send).toBeDisabled();
  expect(changes(api, 'POST', messagePath)).toHaveLength(1);

  installPlayback(api, 2);
  await review.click();
  await expect(dialog.getByText('Playback snapshot: 2 clients are playing.')).toBeVisible();
  await expect(dialog.getByText(/A refreshed count cannot confirm delivery/)).toBeVisible();
  await expect(message).toHaveValue('Maintenance starts soon.');
  await expect(send).toBeEnabled();
  expect(changes(api, 'POST', messagePath)).toHaveLength(1);
  expect(changes(api, 'POST', restartPath)).toHaveLength(0);
});

test('a pending broadcast protects navigation and a timeout cannot be bypassed by reopening Settings', async ({ page, api }) => {
  installPolicy(api);
  installPlayback(api, 1);
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  api.handlers.set(`POST ${messagePath}`, async (route) => {
    await held;
    await json(route, messageReceipt([{ status: 'queued', deliveryCount: 1 }]), 202).catch(() => undefined);
  });
  try {
    await openPolicy(page);
    const dialog = await openMessage(page);
    await page.clock.install();
    const post = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === messagePath);
    await dialog.getByRole('textbox', { name: 'Message', exact: true }).fill('Maintenance is about to start.');
    await dialog.getByRole('button', { name: 'Send message', exact: true }).click();
    await post;
    await expect(dialog.getByRole('button', { name: 'Sending message…', exact: true })).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeVisible();
    await page.evaluate(() => {
      history.pushState(null, '', '/admin/system/notifications');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    await expect(page).toHaveURL(/\/admin\/settings\/general$/);
    await expect(dialog).toBeVisible();
    await page.clock.fastForward(15_001);
    await expect(dialog.getByText(/message result is unknown/)).toBeVisible();
    expect(changes(api, 'POST', messagePath)).toHaveLength(1);
    page.once('dialog', (confirmation) => { void confirmation.accept(); });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await page.evaluate(() => {
      history.pushState(null, '', '/admin/system/notifications');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    await expect(page.getByRole('heading', { name: 'Notifications', exact: true }).first()).toBeVisible();
    await page.evaluate(() => {
      history.pushState(null, '', '/admin/settings/general');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    const maintenance = page.getByRole('region', { name: 'Server maintenance', exact: true });
    await expect(maintenance.getByRole('button', { name: 'Send message', exact: true })).toBeDisabled();
    await maintenance.getByRole('button', { name: 'Refresh playback count', exact: true }).click();
    await expect(maintenance.getByText('Playback snapshot: 1 client is playing.')).toBeVisible();
    await expect(maintenance.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
    expect(changes(api, 'POST', messagePath)).toHaveLength(1);
    expect(changes(api, 'POST', restartPath)).toHaveLength(0);
  } finally { release(); }
});

test.describe('Chinese light mobile server policy', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'light' } });
  test('network policy and both confirmations fit the phone without horizontal overflow', async ({ page, api }, testInfo) => {
    installPolicy(api);
    await page.goto('/admin/settings/general');
    const network = page.getByRole('region', { name: '远程访问与本地网络', exact: true });
    await expect(network).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await network.scrollIntoViewIfNeeded();
    await noOverflow(page);
    await evidence(page, testInfo, 'server-policy-network-390-zh-light');

    await page.getByRole('switch', { name: '允许远程访问', exact: true }).uncheck();
    await page.getByRole('button', { name: '保存网络策略', exact: true }).click();
    const disconnect = page.getByRole('dialog', { name: '断开当前连接？', exact: true });
    await expect(disconnect).toBeVisible();
    await dialogFitsViewport(disconnect);
    await noOverflow(page);
    await evidence(page, testInfo, 'server-policy-disconnect-390-zh-light');
    await disconnect.getByRole('button', { name: '继续编辑', exact: true }).click();
    const actions = page.getByRole('group', { name: '网络策略操作', exact: true });
    await actions.getByRole('button', { name: '放弃更改', exact: true }).click();
    await expect(page.getByRole('switch', { name: '允许远程访问', exact: true })).toBeChecked();

    await page.getByRole('button', { name: '重启服务器', exact: true }).click();
    const restart = page.getByRole('dialog', { name: '重启服务器？', exact: true });
    await expect(restart).toBeVisible();
    await dialogFitsViewport(restart);
    await noOverflow(page);
    await evidence(page, testInfo, 'server-policy-restart-390-zh-light');
    await restart.getByRole('button', { name: '先发送提醒', exact: true }).click();
    const reminderDialog = page.getByRole('dialog', { name: '向正在播放的客户端发送消息', exact: true });
    await expect(reminderDialog).toBeVisible();
    await expect(reminderDialog.getByRole('textbox', { name: '标题（可选）', exact: true })).toHaveValue('服务器维护');
    await expect(reminderDialog.getByRole('textbox', { name: '消息', exact: true })).toHaveValue('服务器将在 5 分钟后重启，播放会短暂中断。');
    await dialogFitsViewport(reminderDialog);
    await noOverflow(page);
    page.once('dialog', (confirmation) => { void confirmation.accept(); });
    await page.keyboard.press('Escape');
    await expect(reminderDialog).not.toBeVisible();
    expect(changes(api, 'POST', messagePath)).toHaveLength(0);
    expect(changes(api, 'POST', previewPath)).toHaveLength(1);
    expect(changes(api, 'PUT', policyPath)).toEqual([]);
    expect(changes(api, 'POST', restartPath)).toEqual([]);
  });
});
