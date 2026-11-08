import type { Locator, Page } from '@playwright/test';
import type { ServerPolicy, ServerPolicyInput } from '../src/serverPolicyApi';
import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

const policyPath = '/admin/v1/server-policy';
const previewPath = `${policyPath}/preview`;
const restartPath = '/admin/v1/server/restart';
const initialRevision = '9007199254740993';

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

test('restart needs the exact confirmation word and reports acceptance without claiming completion', async ({ page, api }, testInfo) => {
  installPolicy(api);
  await openPolicy(page);
  await page.getByRole('button', { name: 'Restart server', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Restart server?', exact: true });
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
    await page.keyboard.press('Escape');
    await expect(restart).not.toBeVisible();
    expect(changes(api, 'POST', previewPath)).toHaveLength(1);
    expect(changes(api, 'PUT', policyPath)).toEqual([]);
    expect(changes(api, 'POST', restartPath)).toEqual([]);
  });
});
