import type { Page } from '@playwright/test';
import { expect, json, test } from './dashboard-fixture';
import type { DashboardAPI } from './dashboard-fixture';
import type { TaskDefinition, TaskTrigger } from '../src/api';
import type { BackupPolicy, BackupPolicyInput } from '../src/backupPolicyApi';

const policyPath = '/admin/v1/backups/policy';
const backupsPath = '/admin/system/backups';
const policyRevision = '9007199254740993';
const taskId = '9'.repeat(32);
const nextRun = '2026-11-07T19:00:00Z';
const reviewMessage = 'The settings changed or the save result could not be confirmed. Reload the saved settings before making another change. This request is not submitted again automatically.';

function initialPolicy(overrides: Partial<BackupPolicy> = {}): BackupPolicy {
  return {
    Revision: policyRevision, Enabled: false,
    Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' },
    KeepCount: 7, IncludeImages: true, HasPassphrase: true, NextRunAt: null, Available: true, UnavailableReason: '',
    TaskId: taskId, LastRunAt: null, LastRunState: '', LastBackupId: '', ...overrides,
  };
}

function initialTrigger(overrides: Partial<TaskTrigger> = {}): TaskTrigger {
  return {
    Id: 'backup-daily-rule', Kind: 'daily', IntervalTicks: null, TimeOfDayTicks: '108000000000', DayOfWeek: null,
    MaxRuntimeTicks: null, NextFireAt: nextRun, CalculationError: '', ...overrides,
  };
}

function initialTask(overrides: Partial<TaskDefinition> = {}): TaskDefinition {
  return {
    Id: taskId, Key: 'backup.create', Name: 'Create automatic backup',
    Description: 'Create an encrypted backup using the saved backup settings.', Category: 'System',
    IsHidden: false, Enabled: true, Revision: policyRevision, ScheduleTimezone: 'UTC',
    Triggers: [initialTrigger()], CurrentRun: null, LastRun: null, NextRunAt: nextRun, ...overrides,
  };
}

function installPolicy(api: DashboardAPI, initial = initialPolicy(), linkedTask = initialTask({ Enabled: initial.Enabled, NextRunAt: initial.NextRunAt })) {
  let saved = structuredClone(initial);
  let savedTask = structuredClone(linkedTask);
  const apply = (input: BackupPolicyInput) => {
    saved = {
      ...saved, KeepCount: input.KeepCount, IncludeImages: input.IncludeImages,
      Revision: (BigInt(saved.Revision) + 1n).toString(),
      HasPassphrase: input.Passphrase === undefined ? saved.HasPassphrase : input.Passphrase !== '',
    };
    return saved;
  };
  api.handlers.set('GET ' + policyPath, (route) => json(route, saved));
  api.handlers.set('PATCH ' + policyPath, (route, request) => {
    const input = request.postDataJSON() as BackupPolicyInput;
    if (input.Revision !== saved.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Settings changed.' } }, 409);
    return json(route, apply(input));
  });
  api.handlers.set('GET /admin/v1/tasks', (route) => json(route, { Items: [savedTask], TotalRecordCount: 1 }));
  api.handlers.set('GET /admin/v1/tasks/' + taskId, (route) => json(route, { Task: savedTask }));
  return {
    get: () => saved, set: (value: BackupPolicy) => { saved = structuredClone(value); }, apply,
    getTask: () => savedTask, setTask: (value: TaskDefinition) => { savedTask = structuredClone(value); },
  };
}

async function openSettings(page: Page) {
  await page.goto(backupsPath);
  await page.getByRole('button', { name: 'Backup settings', exact: true }).click();
  const drawer = page.getByRole('dialog', { name: 'Backup settings', exact: true });
  await expect(drawer.getByLabel('Automatic backups to keep', { exact: true })).toBeEnabled();
  return drawer;
}

test('backup policy loads with the page and saves only contents, retention, encryption, and the exact revision', async ({ page, api }) => {
  const state = installPolicy(api);
  const originalSchedule = structuredClone(state.get().Schedule);
  await page.goto(backupsPath);
  await expect(page.getByRole('button', { name: 'Create backup', exact: true })).toBeEnabled();
  await expect(page.getByText('Task disabled', { exact: true })).toBeVisible();
  expect(api.reads(policyPath).length).toBeGreaterThan(0);
  expect(api.reads('/admin/v1/tasks/' + taskId).length).toBeGreaterThan(0);
  const readsBeforeOpen = api.reads(policyPath).length;
  await page.getByRole('button', { name: 'Backup settings', exact: true }).click();
  const drawer = page.getByRole('dialog', { name: 'Backup settings', exact: true });
  await expect(drawer.getByLabel('Automatic backups to keep', { exact: true })).toHaveValue('7');
  expect(api.reads(policyPath).length).toBeGreaterThan(readsBeforeOpen);
  await expect(drawer.getByLabel('Enable automatic backups', { exact: true })).toHaveCount(0);
  await expect(drawer.getByLabel('Frequency', { exact: true })).toHaveCount(0);
  await expect(drawer.getByLabel('Day of the week', { exact: true })).toHaveCount(0);
  await expect(drawer.getByLabel('Backup time', { exact: true })).toHaveCount(0);
  await expect(drawer.getByLabel('Schedule time zone', { exact: true })).toHaveCount(0);
  await expect(drawer.getByText('An encryption passphrase is saved. Its contents are never returned.', { exact: true })).toBeVisible();
  await expect(drawer.locator('input[type="password"]')).toHaveCount(0);
  await drawer.getByLabel('Automatic backups to keep', { exact: true }).fill('5');
  await drawer.getByLabel('Include library images and avatars', { exact: true }).uncheck();
  await expect(drawer.getByText('Library images, cached covers, user avatars, and manual image overrides will be excluded. Restore will use source-folder images when available.', { exact: true })).toBeVisible();
  await drawer.getByLabel('Saved passphrase action', { exact: true }).selectOption('replace');
  const exactPassphrase = '  Fixture-Only Backup Passphrase  ';
  await drawer.getByLabel('New backup passphrase', { exact: true }).fill(exactPassphrase);
  await drawer.getByLabel('Confirm new backup passphrase', { exact: true }).fill(exactPassphrase);
  await drawer.getByRole('button', { name: 'Save backup settings', exact: true }).click();
  await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'PATCH', path: policyPath });
  expect(api.writes()[0].body).toEqual({ Revision: policyRevision, KeepCount: 5, IncludeImages: false, Passphrase: exactPassphrase });
  expect(state.get().Schedule).toEqual(originalSchedule);
  expect(state.get().Enabled).toBe(false);
  expect(state.get()).not.toHaveProperty('Passphrase');
  await expect(drawer.locator('input[type="password"]')).toHaveCount(0);
  await expect(drawer.getByLabel('Saved passphrase action', { exact: true })).toHaveValue('keep');
});

test('custom backup triggers and their time zone survive policy-only settings changes', async ({ page, api }) => {
  const customTask = initialTask({ ScheduleTimezone: 'Asia/Shanghai', Triggers: [
    initialTrigger({ Id: 'hourly-backup', Kind: 'interval', IntervalTicks: '36000000000', TimeOfDayTicks: null }),
    initialTrigger({ Id: 'startup-backup', Kind: 'startup', TimeOfDayTicks: null, NextFireAt: null }),
  ] });
  const state = installPolicy(api, initialPolicy({ Schedule: { Kind: 'custom', DayOfWeek: null, TimeOfDay: '', Timezone: 'Asia/Shanghai' } }), customTask);
  const before = structuredClone(state.getTask());
  const originalSchedule = structuredClone(state.get().Schedule);
  const drawer = await openSettings(page);
  await expect(drawer.getByText('Every 1 hour', { exact: true })).toBeVisible();
  await expect(drawer.getByText('At server startup', { exact: true })).toBeVisible();
  const taskLink = drawer.getByRole('link', { name: 'Trigger rules', exact: true });
  await expect(taskLink).toHaveAttribute('href', '/admin/system/tasks?task=' + taskId);
  await drawer.getByLabel('Automatic backups to keep', { exact: true }).fill('8');
  await expect(taskLink).toHaveAttribute('aria-disabled', 'true');
  await expect(drawer.getByText('Save or reload this settings draft before editing trigger rules.', { exact: true })).toBeVisible();
  await drawer.getByRole('button', { name: 'Save backup settings', exact: true }).click();
  await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0]).toMatchObject({ method: 'PATCH', path: policyPath });
  expect(api.writes()[0].body).toEqual({ Revision: policyRevision, KeepCount: 8, IncludeImages: true });
  expect(state.getTask()).toEqual(before);
  expect(state.get().Schedule).toEqual(originalSchedule);
  await expect(taskLink).not.toHaveAttribute('aria-disabled', 'true');
  await expect(drawer.getByText('Trigger rules and their time zone are edited in Tasks. Saving these settings preserves every trigger rule.', { exact: true })).toBeVisible();
});

test('backup policy enforces both UTF-8 limits and typed passphrase confirmation before sending a request', async ({ page, api }) => {
  installPolicy(api);
  const drawer = await openSettings(page);
  const mode = drawer.getByLabel('Saved passphrase action', { exact: true });
  const passphrase = drawer.getByLabel('New backup passphrase', { exact: true });
  const confirmation = drawer.getByLabel('Confirm new backup passphrase', { exact: true });
  const save = drawer.getByRole('button', { name: 'Save backup settings', exact: true });
  await mode.selectOption('replace');
  await passphrase.fill('\u{1f642}\u{1f642}');
  await confirmation.fill('\u{1f642}\u{1f642}');
  await save.click();
  await expect(passphrase).toBeFocused();
  expect(api.writes()).toEqual([]);
  const minimum = '\u5bc6'.repeat(4);
  await passphrase.fill(minimum);
  await confirmation.fill(minimum + ' ');
  await save.click();
  await expect(confirmation).toBeFocused();
  await expect(drawer.getByText('The passphrases do not match.', { exact: true })).toBeVisible();
  expect(api.writes()).toEqual([]);
  await confirmation.fill(minimum);
  await save.click();
  await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toEqual({ Revision: policyRevision, KeepCount: 7, IncludeImages: true, Passphrase: minimum });
  await mode.selectOption('replace');
  const oversized = '\u5bc6'.repeat(342);
  await passphrase.fill(oversized);
  await confirmation.fill(oversized);
  await save.click();
  await expect(passphrase).toBeFocused();
  expect(api.writes()).toHaveLength(1);
  const maximum = '\u5bc6'.repeat(341) + 'a';
  await passphrase.fill(maximum);
  await confirmation.fill(maximum);
  await save.click();
  await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(2);
  expect(api.writes()[1].body).toEqual({ Revision: (BigInt(policyRevision) + 1n).toString(), KeepCount: 7, IncludeImages: true, Passphrase: maximum });
  await expect(drawer.locator('input[type="password"]')).toHaveCount(0);
});

test('clearing a saved passphrase relies on the server field error until all automatic triggers are removed', async ({ page, api }) => {
  const state = installPolicy(api, initialPolicy({ Enabled: true, NextRunAt: nextRun }));
  const fieldMessage = 'Remove all automatic backup triggers on Tasks before clearing the saved passphrase.';
  api.handlers.set('PATCH ' + policyPath, (route, request) => {
    const input = request.postDataJSON() as BackupPolicyInput;
    expect(input.Revision).toBe(state.get().Revision);
    if (state.getTask().Triggers.length) return json(route, {
      Error: { Code: 'invalid_backup_policy', Message: 'Check the backup settings.', Fields: { Passphrase: fieldMessage } },
    }, 400);
    return json(route, state.apply(input));
  });
  const drawer = await openSettings(page);
  const mode = drawer.getByLabel('Saved passphrase action', { exact: true });
  const save = drawer.getByRole('button', { name: 'Save backup settings', exact: true });
  await mode.selectOption('clear');
  await expect(drawer.getByText('Remove all automatic backup trigger rules in Tasks before clearing the saved passphrase.', { exact: true })).toBeVisible();
  await save.click();
  await expect(drawer.getByText(fieldMessage, { exact: true })).toBeVisible();
  await expect(mode).toBeFocused();
  await expect(save).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
  expect(api.writes()[0].body).toEqual({ Revision: policyRevision, KeepCount: 7, IncludeImages: true, Passphrase: '' });
  expect(state.get().HasPassphrase).toBe(true);
  state.setTask({ ...state.getTask(), Triggers: [], NextRunAt: null, Revision: (BigInt(policyRevision) + 1n).toString() });
  state.set({ ...state.get(), Enabled: false, NextRunAt: null, Revision: (BigInt(policyRevision) + 1n).toString() });
  page.once('dialog', (prompt) => void prompt.accept());
  await drawer.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
  await expect(mode).toHaveValue('keep');
  await expect(drawer.getByText('No triggers · manual only', { exact: true })).toBeVisible();
  await mode.selectOption('clear');
  await expect(drawer.getByText('Saving will remove the stored automatic backup passphrase.', { exact: true })).toBeVisible();
  await save.click();
  await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(2);
  expect(api.writes()[1].body).toEqual({ Revision: (BigInt(policyRevision) + 1n).toString(), KeepCount: 7, IncludeImages: true, Passphrase: '' });
  expect(state.get().HasPassphrase).toBe(false);
});

for (const outcome of ['unconfirmed', 'conflict'] as const) {
  test('a ' + (outcome === 'unconfirmed' ? '503 policy save' : '409 policy conflict') + ' clears secrets and requires explicit reload before a new revision can be submitted', async ({ page, api }) => {
    const state = installPolicy(api);
    let firstWrite = true;
    api.handlers.set('PATCH ' + policyPath, (route, request) => {
      const input = request.postDataJSON() as BackupPolicyInput;
      if (firstWrite) {
        firstWrite = false;
        if (outcome === 'unconfirmed') state.apply(input);
        else state.set({ ...state.get(), KeepCount: 11, IncludeImages: false, Revision: (BigInt(policyRevision) + 2n).toString() });
        return json(route, { Error: {
          Code: outcome === 'unconfirmed' ? 'database_unavailable' : 'revision_conflict',
          Message: outcome === 'unconfirmed' ? 'The result could not be confirmed.' : 'The settings changed.',
        } }, outcome === 'unconfirmed' ? 503 : 409);
      }
      expect(input.Revision).toBe(state.get().Revision);
      return json(route, state.apply(input));
    });
    const drawer = await openSettings(page);
    const retention = drawer.getByLabel('Automatic backups to keep', { exact: true });
    const mode = drawer.getByLabel('Saved passphrase action', { exact: true });
    const passphrase = drawer.getByLabel('New backup passphrase', { exact: true });
    const confirmation = drawer.getByLabel('Confirm new backup passphrase', { exact: true });
    const save = drawer.getByRole('button', { name: 'Save backup settings', exact: true });
    await retention.fill('9');
    await mode.selectOption('replace');
    const firstSecret = '  Never Replay This Policy Secret  ';
    await passphrase.fill(firstSecret);
    await confirmation.fill(firstSecret);
    await save.click();
    await expect(drawer.getByText(reviewMessage, { exact: true })).toBeVisible();
    await expect(save).toBeDisabled();
    await expect(retention).toBeDisabled();
    await expect(mode).toBeDisabled();
    await expect(passphrase).toBeDisabled();
    await expect(confirmation).toBeDisabled();
    await expect(drawer.getByLabel('Include library images and avatars', { exact: true })).toBeDisabled();
    await expect(passphrase).toHaveValue('');
    await expect(confirmation).toHaveValue('');
    expect(api.writes()).toHaveLength(1);
    expect(api.writes()[0].body).toEqual({ Revision: policyRevision, KeepCount: 9, IncludeImages: true, Passphrase: firstSecret });
    const readsBeforeReload = api.reads(policyPath).length;
    page.once('dialog', (prompt) => void prompt.dismiss());
    await drawer.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
    await expect(retention).toHaveValue('9');
    await expect(retention).toBeDisabled();
    expect(api.reads(policyPath)).toHaveLength(readsBeforeReload);
    expect(api.writes()).toHaveLength(1);
    const confirmedRevision = state.get().Revision;
    page.once('dialog', (prompt) => void prompt.accept());
    await drawer.getByRole('button', { name: 'Reload saved settings', exact: true }).click();
    await expect(retention).toHaveValue(outcome === 'unconfirmed' ? '9' : '11');
    await expect(retention).toBeEnabled();
    await expect(mode).toHaveValue('keep');
    await expect(save).toBeDisabled();
    await expect(drawer.locator('input[type="password"]')).toHaveCount(0);
    expect(api.writes()).toHaveLength(1);
    await retention.fill('10');
    await mode.selectOption('replace');
    await expect(passphrase).toHaveValue('');
    await expect(confirmation).toHaveValue('');
    const nextSecret = 'Fresh policy secret after confirmed reload';
    await passphrase.fill(nextSecret);
    await confirmation.fill(nextSecret);
    await save.click();
    await expect(drawer.getByText('Backup settings saved.', { exact: true })).toBeVisible();
    expect(api.writes()).toHaveLength(2);
    expect(api.writes()[1].body).toEqual({ Revision: confirmedRevision, KeepCount: 10, IncludeImages: outcome === 'unconfirmed', Passphrase: nextSecret });
  });
}

test('a rejected browser navigation preserves the settings draft and closing restores keyboard focus', async ({ page, api }) => {
  installPolicy(api);
  await page.goto('/admin/');
  const navigation = page.getByRole('navigation', { name: 'Administration', exact: true });
  await navigation.getByRole('link', { name: 'Backups & recovery', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(backupsPath + '$'));
  const trigger = page.getByRole('button', { name: 'Backup settings', exact: true });
  await trigger.click();
  const drawer = page.getByRole('dialog', { name: 'Backup settings', exact: true });
  const retention = drawer.getByLabel('Automatic backups to keep', { exact: true });
  await expect(retention).toBeEnabled();
  await retention.fill('8');
  await retention.focus();
  const confirmation = page.waitForEvent('dialog').then(async (prompt) => {
    const details = { type: prompt.type(), message: prompt.message() };
    await prompt.dismiss();
    return details;
  });
  const [, dismissed] = await Promise.all([page.goBack({ waitUntil: 'commit' }), confirmation]);
  expect(dismissed).toEqual({ type: 'confirm', message: 'Discard unsaved backup settings and leave this page?' });
  await expect(page).toHaveURL(new RegExp(backupsPath + '$'));
  await expect(drawer).toBeVisible();
  await expect(retention).toHaveValue('8');
  await expect(retention).toBeFocused();
  page.once('dialog', (prompt) => void prompt.dismiss());
  await page.keyboard.press('Escape');
  await expect(drawer).toBeVisible();
  await expect(retention).toHaveValue('8');
  page.once('dialog', (prompt) => void prompt.accept());
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await expect(trigger).toBeFocused();
  const overview = navigation.getByRole('link', { name: 'Overview', exact: true });
  await overview.focus();
  await overview.press('Enter');
  await expect(page).toHaveURL(/\/admin\/$/);
  await expect(page.getByRole('main')).toBeFocused();
  expect(api.writes()).toEqual([]);
});

test.describe('mobile Chinese backup policy', () => {
  test.use({ viewport: { width: 390, height: 844 }, preferences: { language: 'zh', theme: 'dark' } });
  test('preserves drafts on dismissed close and returns focus without overflowing the viewport', async ({ page, api }, testInfo) => {
    installPolicy(api);
    await page.goto(backupsPath);
    const trigger = page.getByRole('button', { name: '\u5907\u4efd\u8bbe\u7f6e', exact: true });
    await trigger.click();
    const drawer = page.getByRole('dialog', { name: '\u5907\u4efd\u8bbe\u7f6e', exact: true });
    const retention = drawer.getByLabel('\u81ea\u52a8\u5907\u4efd\u4fdd\u7559\u4efd\u6570', { exact: true });
    await expect(retention).toHaveValue('7');
    await retention.fill('8');
    page.once('dialog', (prompt) => void prompt.dismiss());
    await page.keyboard.press('Escape');
    await expect(drawer).toBeVisible();
    await expect(retention).toHaveValue('8');
    expect(await drawer.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('backup-policy-mobile-zh-dark.png') });
    page.once('dialog', (prompt) => void prompt.accept());
    await page.keyboard.press('Escape');
    await expect(drawer).toBeHidden();
    await expect(trigger).toBeFocused();
    expect(api.writes()).toEqual([]);
  });
});
