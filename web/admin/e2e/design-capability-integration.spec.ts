import { expect, test as base } from '@playwright/test';
import type { Page } from '@playwright/test';
import type { ProviderConfigurationInput } from '../src/providersApi';
import type { ServerPolicy, ServerPolicyInput } from '../src/serverPolicyApi';
import { csrf, DashboardAPI, json, stamp } from './dashboard-fixture';
import { evidence, navigate, noOverflow } from './dashboard-review';
import { installProviderConfigurationFixture } from './provider-configuration-fixture';

type LanguageSeed = 'none' | 'zh' | 'en';
interface IntegrationFixtures {
  api: DashboardAPI;
  languageSeed: LanguageSeed;
  bootstrapCulture: 'zh-CN' | 'en-US';
}

// Use the base context rather than the dashboard preference fixture: seeding a
// language on every navigation would conceal a server-default regression.
const test = base.extend<IntegrationFixtures>({
  languageSeed: ['en', { option: true }],
  bootstrapCulture: ['zh-CN', { option: true }],
  api: async ({ context, page, languageSeed, bootstrapCulture }, use) => {
    await context.addInitScript((seed) => {
      if (sessionStorage.getItem('goby.capability.integration.preferences')) return;
      localStorage.removeItem('goby.admin.language');
      if (seed !== 'none') localStorage.setItem('goby.admin.language', seed);
      localStorage.setItem('goby.admin.theme', 'light');
      sessionStorage.setItem('goby.capability.integration.preferences', '1');
    }, languageSeed);
    const api = new DashboardAPI();
    api.handlers.set('GET /admin/v1/bootstrap', (route) => json(route, { Initialized: true, UICulture: bootstrapCulture }));
    installProviderConfigurationFixture(api);
    installPolicy(api);
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every integration request must have an explicit synthetic response.').toEqual([]);
    expect(errors, 'Integration flows must not throw browser errors.').toEqual([]);
    expect(api.writes().every((request) => request.csrf === csrf), 'Authenticated writes must preserve CSRF protection.').toBe(true);
  },
});

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

const policyPath = '/admin/v1/server-policy';
const previewPath = policyPath + '/preview';
const providerPath = '/admin/v1/providers/tmdb/configuration';
const exactRevision = '9007199254740993';
const syntheticSecret = 'synthetic-integration-provider-token';

function policyFixture(): ServerPolicy {
  return {
    Revision: exactRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8', '192.168.0.0/16'],
    UICulture: 'zh-CN', UpdatedAt: stamp, Client: { Address: '127.0.0.1', IsLocal: true },
    Restart: { Available: true, InProgress: false },
    Updates: { CanSelfUpdate: false, Delivery: 'docker', ConfiguredSource: false, CurrentVersion: '0.1.0-integration-fixture' },
  };
}

function installPolicy(api: DashboardAPI) {
  const state = {
    saved: policyFixture(),
    apply(input: ServerPolicyInput): ServerPolicy {
      expect(input.Revision).toBe(this.saved.Revision);
      this.saved = {
        ...this.saved, Revision: (BigInt(input.Revision) + 1n).toString(),
        EnableRemoteAccess: input.EnableRemoteAccess, LocalNetworkSubnets: [...input.LocalNetworkSubnets], UICulture: input.UICulture,
      };
      return this.saved;
    },
  };
  api.handlers.set('GET ' + policyPath, (route) => json(route, state.saved));
  api.handlers.set('POST ' + previewPath, (route, request) => {
    const input = request.postDataJSON() as ServerPolicyInput;
    expect(input.Revision).toBe(state.saved.Revision);
    return json(route, { Revision: input.Revision, WouldDisconnect: false, Client: state.saved.Client });
  });
  api.handlers.set('PUT ' + policyPath, (route, request) => json(route, { ...state.apply(request.postDataJSON() as ServerPolicyInput), DisconnectExpected: false }));
  return state;
}

function gate() {
  let release!: () => void;
  const ready = new Promise<void>((resolve) => { release = resolve; });
  return { ready, release };
}

async function general(page: Page) {
  await page.goto('/admin/settings/general');
  await expect(page.getByRole('combobox', { name: 'Default interface language', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled();
}

test.describe('server language defaults and personal choices', () => {
  test.use({ languageSeed: 'none', bootstrapCulture: 'en-US' });

  test('a fresh browser uses the server language without turning it into a personal preference', async ({ page, api }, testInfo) => {
    await general(page);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.getByRole('heading', { level: 1, name: 'General', exact: true })).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('goby.admin.language'))).toBeNull();
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: 'General', exact: true })).toBeVisible();
    expect(await page.evaluate(() => localStorage.getItem('goby.admin.language'))).toBeNull();
    expect(api.reads('/admin/v1/bootstrap')).toHaveLength(2);
    expect(api.writes()).toEqual([]);
    await noOverflow(page);
    await evidence(page, testInfo, 'capability-integration-server-language-default-1440-en-light');
  });
});

test.describe('existing personal language', () => {
  test.use({ languageSeed: 'zh', bootstrapCulture: 'en-US' });

  test('an existing choice wins and a user-selected language survives a different server default', async ({ page, api }, testInfo) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/admin/settings/general');
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.locator('.server-policy')).toBeVisible();
    await expect(page.locator('.server-policy')).not.toHaveAttribute('aria-busy', 'true');
    expect(await page.evaluate(() => localStorage.getItem('goby.admin.language'))).toBe('zh');
    await page.locator('.account-avatar').click();
    await expect(page.getByRole('menuitemradio', { name: '中文', exact: true })).toHaveAttribute('aria-checked', 'true');
    await page.getByRole('menuitemradio', { name: 'English', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    expect(await page.evaluate(() => localStorage.getItem('goby.admin.language'))).toBe('en');
    api.handlers.set('GET /admin/v1/bootstrap', (route) => json(route, { Initialized: true, UICulture: 'zh-CN' }));
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: 'General', exact: true })).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    expect(await page.evaluate(() => localStorage.getItem('goby.admin.language'))).toBe('en');
    expect(api.writes()).toEqual([]);
    await noOverflow(page);
    await evidence(page, testInfo, 'capability-integration-personal-language-390-en-light');
  });
});

test('network drafts survive a parent-form reload and internal settings navigation', async ({ page, api }, testInfo) => {
  const reload = gate();
  await general(page);
  const language = page.getByRole('combobox', { name: 'Default interface language', exact: true });
  const networks = page.getByRole('textbox', { name: 'Local network subnets', exact: true });
  await language.selectOption('en-US');
  await networks.fill('10.0.0.0/8');
  const policyReads = api.reads(policyPath).length;
  let reloadReads = 0;
  // The shell also reads settings during startup. Delay only requests issued
  // after the initial page is ready, rather than assuming a global read order.
  api.handlers.set('GET /admin/v1/settings', async (route) => {
    reloadReads++;
    await reload.ready;
    return json(route, api.settings);
  });
  try {
    await page.getByRole('button', { name: 'Reload settings', exact: true }).click();
    await expect.poll(() => reloadReads).toBeGreaterThanOrEqual(1);
    await expect(networks).toHaveValue('10.0.0.0/8');
    await expect(language).toHaveValue('en-US');
    await expect(networks).toBeDisabled();
  } finally { reload.release(); }
  await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeEnabled();
  await expect(networks).toHaveValue('10.0.0.0/8');
  await navigate(page, '/admin/settings/metadata');
  await expect(page.getByRole('heading', { level: 1, name: 'Metadata & subtitles', exact: true })).toBeVisible();
  await navigate(page, '/admin/settings/general');
  await expect(networks).toHaveValue('10.0.0.0/8');
  await expect(language).toHaveValue('en-US');
  expect(api.reads(policyPath)).toHaveLength(policyReads);
  await page.getByRole('button', { name: 'Save network policy', exact: true }).click();
  await expect(page.getByText('Network policy and default language saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([
    { method: 'POST', path: previewPath, body: { Revision: exactRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8'], UICulture: 'en-US' }, csrf },
    { method: 'PUT', path: policyPath, body: { Revision: exactRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8'], UICulture: 'en-US', ConfirmDisconnect: false }, csrf },
  ]);
  await page.getByRole('region', { name: 'Remote access and local networks', exact: true }).scrollIntoViewIfNeeded();
  await noOverflow(page);
  await evidence(page, testInfo, 'capability-integration-policy-draft-after-parent-reload');
});

test('an in-flight policy write blocks parent reload and never submits the parent form on Enter', async ({ page, api }, testInfo) => {
  const saved = installPolicy(api);
  const write = gate();
  api.handlers.set('PUT ' + policyPath, async (route, request) => {
    await write.ready;
    return json(route, { ...saved.apply(request.postDataJSON() as ServerPolicyInput), DisconnectExpected: false });
  });
  await general(page);
  await page.getByRole('group', { name: 'Name source', exact: true }).getByRole('button', { name: 'Custom', exact: true }).click();
  const name = page.getByRole('textbox', { name: 'Server name', exact: true });
  await name.fill('Unsubmitted parent-form draft');
  const networks = page.getByRole('textbox', { name: 'Local network subnets', exact: true });
  expect(await networks.evaluate((input) => input.closest('form') === null)).toBe(true);
  await networks.fill('10.0.0.0/8');
  await networks.press('End');
  await networks.press('Enter');
  expect(api.writes()).toEqual([]);
  await page.getByRole('combobox', { name: 'Default interface language', exact: true }).selectOption('en-US');
  try {
    await page.getByRole('button', { name: 'Save network policy', exact: true }).click();
    await expect.poll(() => api.writes().filter((request) => request.method === 'PUT' && request.path === policyPath).length).toBe(1);
    await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeDisabled();
    await expect(name).toHaveValue('Unsubmitted parent-form draft');
    expect(api.writes().filter((request) => request.path === '/admin/v1/settings')).toEqual([]);
  } finally { write.release(); }
  await expect(page.getByText('Network policy and default language saved.', { exact: true })).toBeVisible();
  await expect(name).toHaveValue('Unsubmitted parent-form draft');
  const updates = api.writes().filter((request) => request.method === 'PUT' && request.path === policyPath);
  expect(updates).toHaveLength(1);
  expect(updates[0].csrf).toBe(csrf);
  expect(updates[0].body).toEqual({ Revision: exactRevision, EnableRemoteAccess: true, LocalNetworkSubnets: ['10.0.0.0/8'], UICulture: 'en-US', ConfirmDisconnect: false });
  expect(api.writes().filter((request) => request.path === '/admin/v1/settings')).toEqual([]);
  await page.getByRole('region', { name: 'Remote access and local networks', exact: true }).scrollIntoViewIfNeeded();
  await noOverflow(page);
  await evidence(page, testInfo, 'capability-integration-policy-write-isolated-from-parent');
});

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
  test('provider unknown results preserve the parent draft and combined navigation guards at ' + viewport.width + 'px', async ({ page, api }, testInfo) => {
    const provider = installProviderConfigurationFixture(api);
    api.handlers.set('PUT ' + providerPath, async (route, request) => {
      provider.apply('tmdb', request.postDataJSON() as ProviderConfigurationInput);
      await route.abort('failed');
    });
    await page.setViewportSize(viewport);
    await general(page);
    await navigate(page, '/admin/settings/metadata');
    const parentLanguage = page.getByRole('textbox', { name: 'Preferred metadata language', exact: true });
    await expect(parentLanguage).toBeEnabled();
    await parentLanguage.fill('fr');
    await page.getByRole('button', { name: 'Configure The Movie Database', exact: true }).click();
    const sheet = page.getByRole('dialog');
    await expect(sheet).toBeVisible();
    await sheet.getByRole('combobox', { name: 'Access token action', exact: true }).selectOption('replace');
    const secret = sheet.getByLabel('New Access token', { exact: true });
    await secret.fill(syntheticSecret);
    expect(await secret.evaluate((input) => input.closest('form')?.parentElement?.closest('form') === null)).toBe(true);
    await expect(page.locator('form form')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Reload settings', exact: true })).toBeDisabled();
    await secret.press('Enter');
    await expect(sheet.getByRole('alert').filter({ hasText: 'No request will be replayed automatically.' })).toBeVisible();
    expect(api.writes()).toEqual([
      { method: 'PUT', path: providerPath, body: { Revision: exactRevision, Fields: { Token: { Mode: 'replace', Value: syntheticSecret } } }, csrf },
    ]);
    await page.goBack();
    await expect(page).toHaveURL(/\/admin\/settings\/metadata$/);
    await expect(secret).toHaveValue(syntheticSecret);
    await expect(secret).toBeDisabled();
    page.once('dialog', (prompt) => { void prompt.dismiss(); });
    await sheet.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(sheet).toBeVisible();
    await expect(secret).toHaveValue(syntheticSecret);
    await expect(parentLanguage).toHaveValue('fr');
    expect(await sheet.evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      return bounds.left >= 0 && bounds.right <= innerWidth + 1 && element.scrollWidth <= element.clientWidth + 1;
    })).toBe(true);
    await evidence(page, testInfo, 'capability-integration-provider-unknown-guard-' + viewport.width);
    page.once('dialog', (prompt) => { void prompt.accept(); });
    await sheet.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(sheet).toBeHidden();
    await expect(page.getByRole('button', { name: 'Configure The Movie Database', exact: true })).toBeDisabled();
    const prompts: string[] = [];
    page.once('dialog', (prompt) => { prompts.push(prompt.message()); void prompt.dismiss(); });
    await navigate(page, '/admin/media/libraries');
    await expect(page).toHaveURL(/\/admin\/settings\/metadata$/);
    expect(prompts).toHaveLength(1);
    expect(prompts[0]).toContain('settings');
    await expect(parentLanguage).toHaveValue('fr');
    await page.getByRole('button', { name: 'Refresh providers', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Configure The Movie Database', exact: true })).toBeEnabled();
    await expect(parentLanguage).toHaveValue('fr');
    expect(api.writes()).toHaveLength(1);
    await noOverflow(page);
    await evidence(page, testInfo, 'capability-integration-parent-draft-retained-' + viewport.width);
  });
}
