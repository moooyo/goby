import type { Locator, Page } from '@playwright/test';
import { expect, test } from './dashboard-fixture';
import { destinations, evidence, ready } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const descriptionText = 'Group media directories into a library for your connected clients.';

async function settleFocusLayout(page: Page): Promise<void> {
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
}

async function expectReadableTitle(heading: Locator) {
  await expect(heading).toBeVisible();
  const layout = await heading.evaluate((element) => {
    const dialog = element.closest('dialog');
    const header = element.closest('.goby-dialog-heading');
    const close = dialog?.querySelector('[aria-label="Close dialog"]');
    const form = element.closest('form');
    const legacyAncestor = element.closest('.media-create-library');
    if (!dialog || !header || !close) throw new Error('The create library title must have a heading container, dialog, and close control.');
    const box = (rect: DOMRect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom });
    const range = document.createRange();
    range.selectNodeContents(element);
    return { title: box(element.getBoundingClientRect()), header: box(header.getBoundingClientRect()), dialog: box(dialog.getBoundingClientRect()), close: box(close.getBoundingClientRect()),
      text: [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0).map(box),
      viewport: { width: innerWidth, height: innerHeight }, fontSize: getComputedStyle(element).fontSize,
      legacySelectorMatches: element.matches('.media-create-library .MuiDialogTitle-root'),
      form: form ? { tag: form.tagName.toLowerCase(), className: form.getAttribute('class') ?? '' } : null,
      legacyAncestor: legacyAncestor ? { tag: legacyAncestor.tagName.toLowerCase(), className: legacyAncestor.getAttribute('class') ?? '' } : null };
  });
  expect(layout.text.length, 'The title must contain rendered text.').toBeGreaterThan(0);
  for (const rect of layout.text) {
    for (const bounds of [layout.header, layout.dialog]) {
      expect(rect.left).toBeGreaterThanOrEqual(bounds.left - 1);
      expect(rect.right).toBeLessThanOrEqual(bounds.right + 1);
      expect(rect.top).toBeGreaterThanOrEqual(bounds.top - 1);
      expect(rect.bottom).toBeLessThanOrEqual(bounds.bottom + 1);
    }
    expect(rect.left).toBeGreaterThanOrEqual(layout.title.left - 1);
    expect(rect.right).toBeLessThanOrEqual(layout.title.right + 1);
    expect(rect.right, 'The complete title must not overlap the close control.').toBeLessThanOrEqual(layout.close.left - 8);
    expect(rect.left).toBeGreaterThanOrEqual(0);
    expect(rect.right).toBeLessThanOrEqual(layout.viewport.width);
    expect(rect.top).toBeGreaterThanOrEqual(0);
    expect(rect.bottom).toBeLessThanOrEqual(layout.viewport.height);
  }
  return layout;
}

async function expectReadableOpening(dialog: Locator, name: Locator) {
  await expect(name).toBeFocused();
  const description = dialog.getByText(descriptionText, { exact: true });
  await expect(description).toBeVisible();
  expect(await description.innerText()).toBe(descriptionText);
  const layout = await description.evaluate((element) => {
    const content = element.closest('.goby-dialog-content');
    const dialog = element.closest('dialog');
    const name = dialog?.querySelector('#library-name');
    const label = name?.closest('.goby-field')?.querySelector('label');
    const footer = dialog?.querySelector('.goby-dialog-actions');
    if (!content || !dialog || !name || !label || !footer) throw new Error('The create library opening must include its description, name field, and scroll containers.');
    const box = (rect: DOMRect) => ({ left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom });
    const scrollport = (container: Element) => {
      const bounds = container.getBoundingClientRect();
      const left = bounds.left + container.clientLeft; const top = bounds.top + container.clientTop;
      return { bounds: { left, right: left + container.clientWidth, top, bottom: top + container.clientHeight },
        scrollTop: container.scrollTop, scrollHeight: container.scrollHeight, clientHeight: container.clientHeight };
    };
    const textRects = (node: Element) => {
      const range = document.createRange();
      range.selectNodeContents(node);
      return [...range.getClientRects()].filter((rect) => rect.width > 0 && rect.height > 0).map(box);
    };
    return { description: textRects(element), nameLabel: textRects(label), name: box(name.getBoundingClientRect()), footer: box(footer.getBoundingClientRect()),
      content: scrollport(content), dialog: scrollport(dialog), viewport: { left: 0, right: innerWidth, top: 0, bottom: innerHeight }, activeId: document.activeElement?.id };
  });
  expect(layout.content.scrollTop, 'The opening description must remain at the top of its content scroll container.').toBe(0);
  expect(layout.dialog.scrollTop, 'The title and opening fields must remain at the top of the dialog.').toBe(0);
  for (const [part, rects] of [['description', layout.description], ['name label', layout.nameLabel], ['name field', [layout.name]]] as const) {
    expect(rects.length, `The complete ${part} must be rendered.`).toBeGreaterThan(0);
    for (const rect of rects) {
      for (const bounds of [layout.content.bounds, layout.dialog.bounds, layout.viewport]) {
        expect(rect.left, `The ${part} must fit its actual clipping containers.`).toBeGreaterThanOrEqual(bounds.left - 1);
        expect(rect.right, `The ${part} must fit its actual clipping containers.`).toBeLessThanOrEqual(bounds.right + 1);
        expect(rect.top, `The ${part} must fit its actual clipping containers.`).toBeGreaterThanOrEqual(bounds.top - 1);
        expect(rect.bottom, `The ${part} must fit its actual clipping containers.`).toBeLessThanOrEqual(bounds.bottom + 1);
      }
      expect(rect.bottom, `The ${part} must remain above the dialog footer.`).toBeLessThanOrEqual(layout.footer.top + 1);
    }
  }
  return layout;
}

async function closeDraft(page: Page, accept: boolean): Promise<void> {
  const prompt = page.waitForEvent('dialog');
  const closing = page.keyboard.press('Escape');
  const confirmation = await prompt;
  const type = confirmation.type(); const message = confirmation.message();
  if (accept) await confirmation.accept();
  else await confirmation.dismiss();
  await closing;
  expect(type).toBe('confirm');
  expect(message).toBe('Discard the new library draft?');
}

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`${viewport.width}px create library title and description stay readable through keyboard navigation and the unsaved draft close guard`, async ({ page, api }, testInfo) => {
    await page.setViewportSize(viewport);
    await page.goto('/admin/media/libraries');
    await ready(page, destinations[1]);
    const trigger = page.getByRole('button', { name: 'Create library', exact: true }).first();
    await trigger.click();
    const dialog = page.getByRole('dialog', { name: 'Create library', exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-labelledby', 'create-library-title');
    const heading = dialog.getByRole('heading', { level: 2, name: 'Create library', exact: true });
    await expect(heading).toHaveAttribute('id', 'create-library-title');
    await expect(heading).toHaveText('Create library');
    await page.evaluate(() => document.fonts.ready);
    await settleFocusLayout(page);
    const name = dialog.getByRole('textbox', { name: 'Library name', exact: true });
    const initialTitle = await expectReadableTitle(heading);
    const initialOpening = await expectReadableOpening(dialog, name);
    await testInfo.attach('create-library-initial-layout', { body: JSON.stringify({ title: initialTitle, opening: initialOpening }, null, 2), contentType: 'application/json' });
    await evidence(page, testInfo, `design2-create-library-title-${viewport.width}`);

    await page.keyboard.press('Tab');
    await expect(dialog.getByRole('combobox', { name: 'Content type', exact: true })).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await settleFocusLayout(page);
    await expectReadableOpening(dialog, name);
    await page.keyboard.press('Shift+Tab');
    await expect(dialog.getByRole('button', { name: 'Close dialog', exact: true })).toBeFocused();
    await page.keyboard.press('Tab');
    await settleFocusLayout(page);
    await expectReadableOpening(dialog, name);
    for (let count = 0; count < 18; count += 1) {
      await page.keyboard.press(count < 9 ? 'Tab' : 'Shift+Tab');
      expect(await dialog.evaluate((element) => element.contains(document.activeElement)), 'Keyboard focus must remain inside the open dialog.').toBe(true);
    }
    await settleFocusLayout(page);
    const keyboardTitle = await expectReadableTitle(heading);
    const keyboardOpening = await expectReadableOpening(dialog, name);
    await testInfo.attach('create-library-keyboard-layout', { body: JSON.stringify({ title: keyboardTitle, opening: keyboardOpening }, null, 2), contentType: 'application/json' });
    await evidence(page, testInfo, `design2-create-library-keyboard-${viewport.width}`);

    await name.fill('Unsubmitted library draft');
    await closeDraft(page, false);
    await expect(dialog).toBeVisible();
    await expect(name).toHaveValue('Unsubmitted library draft');
    await closeDraft(page, true);
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();
    expect(api.writes()).toEqual([]);
  });
}
