import type { Page } from '@playwright/test';
import type { ProviderConfigurationInput } from '../src/providersApi';
import { expect, json, test } from './dashboard-fixture';
import { installProviderConfigurationFixture } from './provider-configuration-fixture';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'dark' } });
const configurationPath = '/admin/v1/providers/tmdb/configuration';
const syntheticValue = 'synthetic-provider-access-token';
async function open(page: Page, provider = 'The Movie Database') {
  await page.goto('/admin/settings/providers');
  const trigger = page.getByRole('button', { name: `Configure ${provider}`, exact: true });
  await expect(trigger).toBeEnabled(); await trigger.click();
  const sheet = page.getByRole('dialog', { name: new RegExp(provider) });
  await expect(sheet).toBeVisible();
  const heading = sheet.getByRole('heading', { level: 2 });
  await expect(heading.getByText(provider, { exact: true })).toHaveAttribute('translate', 'no');
  await expect(heading.locator('small')).toHaveText('Provider configuration');
  await expect(sheet).toHaveAttribute('aria-labelledby', await heading.getAttribute('id') as string);
  return sheet;
}

test('provider replacement preserves exact revision, CSRF, omitted fields, and empty secret readback', async ({ page, api }) => {
  installProviderConfigurationFixture(api);
  const sheet = await open(page);
  const action = sheet.getByRole('combobox', { name: 'Access token action', exact: true });
  await expect(action).toBeFocused(); await action.selectOption('replace');
  const input = sheet.getByLabel('New Access token', { exact: true });
  await expect(input).toHaveAttribute('type', 'password'); await expect(input).toHaveValue('');
  await input.fill(syntheticValue); await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByRole('status')).toContainText('Provider configuration saved.');
  expect(api.writes()[0].body).toEqual({ Revision: '9007199254740993', Fields: { Token: { Mode: 'replace', Value: syntheticValue } } });
  await expect(input).toHaveCount(0); await action.selectOption('replace'); await expect(input).toHaveValue('');
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(syntheticValue);
  expect(api.writes()).toHaveLength(1);
});

test('clear and restore deployment default are distinct operations without returned values', async ({ page, api }) => {
  const fixture = installProviderConfigurationFixture(api); fixture.saved.Items[0].Fields[0].Source = 'override';
  const sheet = await open(page); const action = sheet.getByRole('combobox', { name: 'Access token action', exact: true });
  await action.selectOption('clear'); await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByText('Explicitly cleared', { exact: true })).toBeVisible();
  expect(api.writes()[0].body).toEqual({ Revision: '9007199254740993', Fields: { Token: { Mode: 'clear' } } });
  await action.selectOption('default'); await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByText('Using deployment value', { exact: true })).toBeVisible();
  expect(api.writes()[1].body).toEqual({ Revision: '9007199254740994', Fields: { Token: { Mode: 'default' } } });
  await expect(sheet.locator('input')).toHaveCount(0);
});

test('a configuration conflict preserves and locks the draft until a fresh read, including after closing', async ({ page, api }) => {
  const fixture = installProviderConfigurationFixture(api); const sheet = await open(page);
  await sheet.getByRole('combobox', { name: 'Access token action', exact: true }).selectOption('replace');
  const input = sheet.getByLabel('New Access token', { exact: true }); await input.fill(syntheticValue);
  fixture.saved.Revision = '9007199254740994';
  await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByRole('alert').filter({ hasText: 'No request will be replayed automatically.' })).toBeVisible();
  await expect(input).toHaveValue(syntheticValue); await expect(input).toBeDisabled();
  page.once('dialog', (prompt) => { void prompt.accept(); }); await sheet.getByRole('button', { name: 'Close', exact: true }).click();
  const trigger = page.getByRole('button', { name: 'Configure The Movie Database', exact: true }); await expect(trigger).toBeDisabled();
  await page.getByRole('button', { name: 'Refresh providers', exact: true }).click(); await expect(trigger).toBeEnabled();
  await trigger.click(); await expect(page.getByRole('dialog').getByRole('combobox', { name: 'Access token action', exact: true })).toHaveValue('keep');
  expect(api.writes()).toHaveLength(1);
});

test('an unconfirmed save is read back without replay and subsequent tests use the confirmed revision', async ({ page, api }) => {
  const fixture = installProviderConfigurationFixture(api);
  api.handlers.set(`PUT ${configurationPath}`, async (route, request) => { fixture.apply('tmdb', request.postDataJSON() as ProviderConfigurationInput); await route.abort('failed'); });
  const sheet = await open(page); await sheet.getByRole('combobox', { name: 'Access token action', exact: true }).selectOption('replace');
  await sheet.getByLabel('New Access token', { exact: true }).fill(syntheticValue); await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByRole('alert').filter({ hasText: 'No request will be replayed automatically.' })).toBeVisible();
  page.once('dialog', (prompt) => { void prompt.accept(); }); await sheet.getByRole('button', { name: 'Reload saved configuration', exact: true }).click();
  await expect(sheet.getByRole('button', { name: 'Test saved configuration', exact: true })).toBeEnabled();
  await sheet.getByRole('button', { name: 'Test saved configuration', exact: true }).click();
  await expect(sheet.getByText('Connection test passed', { exact: true })).toBeVisible();
  expect(api.writes().filter((request) => request.method === 'PUT')).toHaveLength(1);
  expect(api.writes().find((request) => request.path.endsWith('/test'))?.body).toEqual({ Revision: '9007199254740994' });
});

test('connection tests use saved values and report rejected credentials without success text', async ({ page, api }) => {
  installProviderConfigurationFixture(api);
  api.handlers.set('POST /admin/v1/providers/tmdb/test', async (route) => json(route, { Provider: 'tmdb', Status: 'authentication_failed', CheckedAt: '2026-11-06T08:00:00Z' }));
  const sheet = await open(page); const testButton = sheet.getByRole('button', { name: 'Test saved configuration', exact: true });
  await sheet.getByRole('combobox', { name: 'Access token action', exact: true }).selectOption('replace');
  await sheet.getByLabel('New Access token', { exact: true }).fill(syntheticValue); await expect(testButton).toBeDisabled();
  page.once('dialog', (prompt) => { void prompt.accept(); }); await sheet.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await expect(testButton).toBeEnabled(); await testButton.click();
  await expect(sheet.getByText('The provider rejected the saved credentials', { exact: true })).toBeVisible();
  await expect(sheet.getByText('Connection test passed', { exact: true })).toHaveCount(0);
  expect(api.writes()).toHaveLength(1); expect(api.writes()[0].body).toEqual({ Revision: '9007199254740993' });
});

test('OpenSubtitles login values must be cleared together while other fields are kept', async ({ page, api }) => {
  installProviderConfigurationFixture(api); const sheet = await open(page, 'OpenSubtitles');
  await sheet.getByRole('combobox', { name: 'Username action', exact: true }).selectOption('clear');
  await expect(sheet.getByRole('button', { name: 'Save provider', exact: true })).toBeDisabled();
  await expect(sheet.getByText('OpenSubtitles username and password must both be configured or cleared.', { exact: true }).first()).toBeVisible();
  await sheet.getByRole('combobox', { name: 'Password action', exact: true }).selectOption('clear');
  await sheet.getByRole('button', { name: 'Save provider', exact: true }).click();
  await expect(sheet.getByText('Explicitly cleared', { exact: true })).toHaveCount(2);
  expect(api.writes()[0].body).toEqual({ Revision: '9007199254740993', Fields: { Username: { Mode: 'clear' }, Password: { Mode: 'clear' } } });
});

test('configuration responses carrying a secret are rejected before editable state is created', async ({ page, api }) => {
  const fixture = installProviderConfigurationFixture(api);
  api.handlers.set('GET /admin/v1/providers/configuration', async (route) => json(route, { ...fixture.saved, Items: fixture.saved.Items.map((provider) => provider.Id === 'tmdb' ? { ...provider, Fields: [{ ...provider.Fields[0], Value: syntheticValue }] } : provider) }));
  await page.goto('/admin/settings/providers'); await expect(page.getByRole('alert')).toContainText('provider configuration response is incomplete');
  await expect(page.getByRole('button', { name: 'Configure The Movie Database', exact: true })).toBeDisabled();
  await expect(page.locator('body')).not.toContainText(syntheticValue); expect(api.writes()).toEqual([]);
});

test.describe('provider names in Chinese', () => {
  test.use({ preferences: { language: 'zh', theme: 'dark' } });
  test('the configuration heading preserves a provider name that matches translated interface copy', async ({ page, api }) => {
    const fixture = installProviderConfigurationFixture(api);
    const providerName = 'Settings';
    fixture.saved.Items[0].Name = providerName;
    await page.goto('/admin/settings/providers');
    await page.getByRole('button', { name: `\u914d\u7f6e ${providerName}`, exact: true }).click();
    const sheet = page.getByRole('dialog', { name: new RegExp(providerName) });
    const heading = sheet.getByRole('heading', { level: 2 });
    await expect(heading.getByText(providerName, { exact: true })).toHaveAttribute('translate', 'no');
    await expect(heading.locator('small')).toHaveText('\u63d0\u4f9b\u65b9\u914d\u7f6e');
    await expect(sheet).toHaveAttribute('aria-labelledby', await heading.getAttribute('id') as string);
    expect(api.writes()).toEqual([]);
  });
});

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`provider configuration drawer preserves keyboard focus and layout at ${viewport.width}px`, async ({ page, api }, testInfo) => {
    installProviderConfigurationFixture(api); await page.setViewportSize(viewport); const sheet = await open(page);
    expect(await sheet.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
    const width = await sheet.evaluate((element) => element.getBoundingClientRect().width); expect(width).toBe(Math.min(540, viewport.width));
    await sheet.getByRole('combobox', { name: 'Access token action', exact: true }).selectOption('replace');
    await sheet.getByLabel('New Access token', { exact: true }).fill(syntheticValue);
    await page.screenshot({ path: testInfo.outputPath(`provider-settings-${viewport.width}.png`), fullPage: true });
    page.once('dialog', (prompt) => { void prompt.dismiss(); }); await page.keyboard.press('Escape'); await expect(sheet).toBeVisible();
    page.once('dialog', (prompt) => { void prompt.accept(); }); await page.keyboard.press('Escape'); await expect(sheet).not.toBeVisible();
    await expect(page.getByRole('button', { name: 'Configure The Movie Database', exact: true })).toBeFocused(); expect(api.writes()).toEqual([]);
  });
}
