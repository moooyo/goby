import type { Locator, Page } from '@playwright/test';
import type { TaskDefinition } from '../src/api';
import type { EpisodeRoster, EpisodeRosterImport } from '../src/episodeRosterApi';
import { expect, json, seriesLibrary, test } from './dashboard-fixture';
import { destinations, evidence, ready } from './dashboard-review';

// These fixtures exercise existing production routes without a running backend
// or real media files. Keyboard imports prepare drafts and never persist them.
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', preferences: { language: 'en', theme: 'light' } });

test('a keyboard-selected time zone commits the complete suggested IANA name', async ({ page, api }, testInfo) => {
  const task: TaskDefinition = {
    Id: 'scan-library', Key: 'library.scan', Name: 'Scan media libraries',
    Description: 'Discover new files and update library metadata.', Category: 'Library',
    IsHidden: false, Enabled: true, Revision: '9007199254740993', ScheduleTimezone: 'UTC',
    Triggers: [], CurrentRun: null, LastRun: null, NextRunAt: null,
  };
  api.handlers.set('GET /admin/v1/tasks/scan-library', (route) => json(route, { Task: task }));
  await page.goto('/admin/system/tasks');
  await ready(page, destinations[9]);
  const taskRow = page.getByRole('list', { name: 'Available server tasks', exact: true }).getByRole('listitem')
    .filter({ has: page.getByRole('heading', { name: task.Name, exact: true }) });
  await taskRow.getByRole('button', { name: 'Edit schedule', exact: true }).click();
  const schedule = page.getByRole('dialog', { name: /^Trigger rules/ });
  await schedule.locator('.system-schedule-timezone > summary').click();
  const timezone = schedule.getByRole('combobox', { name: 'Schedule time zone', exact: true });
  await expect(timezone).toHaveValue('UTC');
  await timezone.fill('Europe/Lo');
  const london = schedule.getByRole('option', { name: 'Europe/London', exact: true });
  await expect(london).toBeVisible();
  await expect(timezone).not.toHaveAttribute('aria-activedescendant');
  await timezone.press('ArrowDown');
  await expect(london).toHaveAttribute('aria-selected', 'true');
  const optionId = await london.getAttribute('id');
  expect(optionId).toBeTruthy();
  await expect(timezone).toHaveAttribute('aria-activedescendant', optionId!);
  await timezone.press('Enter');
  await expect(timezone).toHaveValue('Europe/London');
  await expect(timezone).toHaveAttribute('aria-expanded', 'false');
  expect(await timezone.evaluate((element) => new Intl.DateTimeFormat('en', { timeZone: (element as HTMLInputElement).value }).resolvedOptions().timeZone)).toBe('Europe/London');
  await expect(schedule.getByRole('button', { name: 'Preview schedule', exact: true })).toBeEnabled();
  await expect(schedule.getByRole('button', { name: 'Save schedule', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
  await evidence(page, testInfo, 'schedule-timezone-keyboard-selection');
  await timezone.fill('');
  const list = schedule.getByRole('listbox');
  expect(await list.getByRole('option').count()).toBeGreaterThan(60);
  const initialPosition = await timezone.evaluate((element) => ({
    top: element.getBoundingClientRect().top,
    dialogScroll: element.closest('dialog')!.scrollTop,
    documentScroll: document.scrollingElement?.scrollTop ?? 0,
    mainScroll: document.getElementById('main-content')!.scrollTop,
  }));
  for (let index = 0; index < 60; index += 1) await timezone.press('ArrowDown');
  const active = list.getByRole('option', { selected: true });
  await expect(timezone).toHaveAttribute('aria-activedescendant', (await active.getAttribute('id'))!);
  await expect.poll(() => active.evaluate((element) => {
    const viewport = element.closest('[role="listbox"]')!.getBoundingClientRect();
    const row = element.getBoundingClientRect();
    return row.top >= viewport.top - 1 && row.bottom <= viewport.bottom + 1;
  }), 'The highlighted time zone must stay inside the candidate list.').toBe(true);
  expect(await list.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  const afterNavigation = await timezone.evaluate((element) => ({
    top: element.getBoundingClientRect().top,
    dialogScroll: element.closest('dialog')!.scrollTop,
    documentScroll: document.scrollingElement?.scrollTop ?? 0,
    mainScroll: document.getElementById('main-content')!.scrollTop,
  }));
  expect(afterNavigation).toEqual(initialPosition);
  const selectedZone = (await active.innerText()).trim();
  const hovered = list.getByRole('option').nth(56);
  await hovered.hover();
  expect(await hovered.evaluate((element) => element.matches(':hover'))).toBe(true);
  await timezone.press('ArrowUp');
  await expect(hovered).toHaveAttribute('aria-selected', 'false');
  await expect(hovered).toHaveCSS('background-color', 'rgb(235, 233, 227)');
  await expect(hovered).toHaveCSS('color', 'rgb(26, 25, 23)');
  await expect(active).toHaveCSS('background-color', 'rgba(214, 157, 72, 0.16)');
  await expect(active).toHaveCSS('color', 'rgb(143, 95, 23)');
  expect(await list.getByRole('option').evaluateAll((options) => options.filter((option) => getComputedStyle(option).backgroundColor === 'rgba(214, 157, 72, 0.16)').length), 'Only the current keyboard option may use the amber selection background.').toBe(1);
  // Hover selects row 56; after ArrowUp, return to the original row 59.
  for (let index = 0; index < 4; index += 1) await timezone.press('ArrowDown');
  await expect(active).toHaveText(selectedZone);
  await evidence(page, testInfo, 'schedule-timezone-long-list-keyboard');
  await timezone.press('Enter');
  await expect(timezone).toHaveValue(selectedZone);
  expect(api.writes()).toEqual([]);
});

test('episode roster imports are reachable by Tab and activate a file chooser with Enter and Space', async ({ page, api }, testInfo) => {
  // The first browser interception subscription is asynchronous. Arm it before
  // navigation while retaining the real keyboard and chooser waits below.
  let chooserEvents = 0;
  page.on('filechooser', () => { chooserEvents += 1; });
  const seriesId = 'series-one';
  const roster: EpisodeRoster = { SeriesId: seriesId, SeriesName: 'Opening series', Revision: '0', State: 'absent', Source: null, Entries: [], RetiredCount: 0 };
  api.handlers.set(`GET /admin/v1/series/${seriesId}/episode-roster`, (route) => json(route, roster));
  await page.goto(`/admin/media/libraries/${seriesLibrary.Id}/items`);
  await expect(page.getByRole('main').getByRole('heading', { level: 1 })).toHaveText('TV series');
  await page.locator('summary[aria-label="More actions: Opening series"]').click();
  await page.getByRole('button', { name: 'Episode roster', exact: true }).click();
  const editor = page.getByRole('dialog', { name: 'Episode roster', exact: true });
  await expect(editor.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('No roster');
  const importer = editor.getByRole('button', { name: 'Import roster JSON', exact: true });
  await expect(importer).toHaveAttribute('tabindex', '0');
  // Cancelling a native picker belongs to its input, not its surrounding dialog.
  await importer.locator('input[type="file"]').dispatchEvent('cancel', { bubbles: true, cancelable: false });
  await expect(editor, 'A child file-input cancellation must keep the roster editor open.').toBeVisible();
  await expect(editor.getByRole('region', { name: 'Saved episode roster', exact: true })).toContainText('No roster');
  for (const [index, key] of (['Enter', 'Space'] as const).entries()) {
    await tabToControl(page, editor.getByRole('button', { name: 'Close dialog', exact: true }), importer);
    const chooser = await test.step(`Open the roster file chooser with ${key}`, async () => {
      const chooserEvent = page.waitForEvent('filechooser', { timeout: 5000 });
      await page.keyboard.press(key);
      return await chooserEvent;
    });
    expect(chooserEvents, 'Each keyboard activation must open exactly one real chooser.').toBe(index + 1);
    expect(chooser.isMultiple()).toBe(false);
    const imported: EpisodeRosterImport = {
      Source: { Key: 'keyboard-guide', Label: 'Keyboard import guide', Revision: `keyboard-${key.toLowerCase()}` },
      Entries: [{ Key: `pilot-${key.toLowerCase()}`, SeasonNumber: 1, EpisodeNumber: 1, Name: `Pilot selected with ${key}` }],
    };
    await chooser.setFiles({ name: `synthetic-roster-${key.toLowerCase()}.json`, mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(imported)) });
    await expect(editor.getByRole('region', { name: 'Episode roster preview', exact: true }).getByRole('table')).toContainText(`Pilot selected with ${key}`);
    await expect(editor.getByRole('button', { name: 'Save roster', exact: true })).toBeEnabled();
    await expect(importer).toHaveAttribute('tabindex', '0');
    expect(api.writes()).toEqual([]);
  }
  await expect(editor).toContainText('Imported roster ready for review.');
  await evidence(page, testInfo, 'episode-roster-keyboard-import-draft');
  const prompt = page.waitForEvent('dialog');
  const close = page.keyboard.press('Escape');
  const confirmation = await prompt;
  expect(confirmation.type()).toBe('confirm');
  expect(confirmation.message()).toBe('Discard the unsaved episode roster?');
  await confirmation.accept();
  await close;
  await expect(editor, 'The dialog itself must still handle Escape after ignoring child cancellation.').not.toBeVisible();
  expect(chooserEvents).toBe(2);
  expect(api.writes()).toEqual([]);
});

async function tabToControl(page: Page, start: Locator, target: Locator): Promise<void> {
  await start.focus();
  for (let count = 0; count < 40; count += 1) {
    await page.keyboard.press('Tab');
    if (await target.evaluate((element) => element === document.activeElement)) break;
  }
  await expect(target, 'The import action must be part of the ordinary Tab sequence.').toBeFocused();
}
