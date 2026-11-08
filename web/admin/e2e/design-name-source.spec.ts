import type { Locator, Page } from '@playwright/test';
import { expect, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

const copy = {
  en: {
    source: 'Name source', choices: ['Deployment default', 'Custom', 'Empty', 'Unset'],
    networks: 'Local network subnets', policyActions: 'Network policy actions',
    policyDirty: 'Unsaved network policy changes', savePolicy: 'Save network policy', save: 'Save changes',
  },
  zh: {
    source: '名称来源', choices: ['部署默认值', '自定义', '留空', '未设置'],
    networks: '本地网络网段', policyActions: '网络策略操作',
    policyDirty: '网络策略有未保存的更改', savePolicy: '保存网络策略', save: '保存更改',
  },
} as const;

async function expectCompleteChoices(page: Page, group: Locator, rows: 1 | 2): Promise<void> {
  await page.evaluate(() => document.fonts.ready);
  await group.scrollIntoViewIfNeeded();
  const choices = group.getByRole('button');
  await expect(choices).toHaveCount(4);
  for (const choice of await choices.all()) await expect(choice).toBeVisible();
  const geometry = await group.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const scroller = element.closest('.dashboard-content')!.getBoundingClientRect();
    return {
      scrollWidth: element.scrollWidth, clientWidth: element.clientWidth, scrollLeft: element.scrollLeft,
      choices: [...element.querySelectorAll('button')].map((button) => {
        const box = button.getBoundingClientRect();
        const walker = document.createTreeWalker(button, NodeFilter.SHOW_TEXT);
        let labelFits = true;
        while (walker.nextNode()) {
          const range = document.createRange();
          range.selectNodeContents(walker.currentNode);
          for (const text of range.getClientRects()) {
            if (text.left < box.left - 1 || text.right > box.right + 1 || text.top < box.top - 1 || text.bottom > box.bottom + 1) labelFits = false;
          }
        }
        const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
        return {
          text: button.textContent, left: box.left, top: box.top, width: box.width, height: box.height,
          inside: box.left >= bounds.left - 1 && box.right <= bounds.right + 1
            && box.top >= Math.max(scroller.top, 0) - 1 && box.bottom <= Math.min(scroller.bottom, innerHeight) + 1,
          labelFits: labelFits && button.scrollWidth <= button.clientWidth + 1,
          reachable: hit !== null && button.contains(hit),
        };
      }),
    };
  });
  expect(geometry.scrollWidth, 'Every name mode must fit without a hidden horizontal scroll area.').toBeLessThanOrEqual(geometry.clientWidth + 1);
  expect(geometry.scrollLeft).toBe(0);
  for (const choice of geometry.choices) {
    expect(choice.inside, choice.text + ' must be completely inside the visible control.').toBe(true);
    expect(choice.labelFits, choice.text + ' must show its complete label.').toBe(true);
    expect(choice.reachable, choice.text + ' must be directly clickable.').toBe(true);
    expect(choice.width).toBeGreaterThanOrEqual(44);
    if (rows === 2) expect(choice.height).toBeGreaterThanOrEqual(44);
  }
  const positions = geometry.choices;
  expect(Math.abs(positions[0].top - positions[1].top)).toBeLessThanOrEqual(1);
  if (rows === 2) {
    expect(positions[2].top).toBeGreaterThan(positions[0].top + positions[0].height - 1);
    expect(Math.abs(positions[2].top - positions[3].top)).toBeLessThanOrEqual(1);
    expect(Math.abs(positions[0].left - positions[2].left)).toBeLessThanOrEqual(1);
    expect(Math.abs(positions[1].left - positions[3].left)).toBeLessThanOrEqual(1);
  } else {
    expect(Math.max(...positions.map((position) => position.top)) - Math.min(...positions.map((position) => position.top))).toBeLessThanOrEqual(1);
  }
  await noOverflow(page);
}

for (const width of [375, 390]) for (const language of ['en', 'zh'] as const) for (const theme of ['light', 'dark'] as const) {
  test.describe(`${width}px ${language} ${theme} name source`, () => {
    test.use({ viewport: { width, height: 844 }, preferences: { language, theme } });
    test('all four modes stay discoverable and unset saves independently from the network policy draft', async ({ page, api }, testInfo) => {
      const text = copy[language];
      const saved = structuredClone(api.settings);
      await page.goto('/admin/settings/general');
      const group = page.getByRole('group', { name: text.source, exact: true });
      const modes = text.choices.map((name) => group.getByRole('button', { name, exact: true }));
      await expect(modes[0]).toBeEnabled();
      await expect(modes[0]).toHaveAttribute('aria-pressed', 'true');
      await expectCompleteChoices(page, group, 2);

      const networks = page.getByRole('textbox', { name: text.networks, exact: true });
      await expect(networks).toBeEnabled();
      await networks.fill('10.0.0.0/8');
      const policyActions = page.getByRole('group', { name: text.policyActions, exact: true });
      await expect(policyActions.getByRole('status')).toHaveText(text.policyDirty);
      const save = page.getByRole('button', { name: text.save, exact: true });
      await expect(save).toBeDisabled();

      await modes[0].focus();
      for (const mode of modes.slice(1)) {
        await page.keyboard.press('Tab');
        await expect(mode).toBeFocused();
      }
      await page.keyboard.press('Space');
      await expect(modes[3]).toHaveAttribute('aria-pressed', 'true');
      await expect(group.locator('button[aria-pressed="true"]')).toHaveCount(1);
      expect(api.writes(), 'Keyboard selection must only change the draft.').toEqual([]);

      await modes[0].click();
      await expect(save).toBeDisabled();
      await modes[3].click();
      await modes[3].click();
      await expect(modes[3]).toHaveAttribute('aria-pressed', 'true');
      await expect(group.locator('button[aria-pressed="true"]')).toHaveCount(1);
      await expect(save).toBeEnabled();
      expect(api.settings.ServerNameMode, 'A name-mode click must not persist the draft.').toBe(saved.ServerNameMode);
      expect(api.writes()).toEqual([]);
      await expect(networks).toHaveValue('10.0.0.0/8');
      await expectCompleteChoices(page, group, 2);
      await evidence(page, testInfo, `settings-name-source-${width}-${language}-${theme}-unset`);

      const response = page.waitForResponse((value) => value.request().method() === 'PUT' && new URL(value.url()).pathname === '/admin/v1/settings');
      await save.click();
      const result = await response;
      expect(result.status()).toBe(200);
      expect(result.request().postDataJSON()).toMatchObject({
        Revision: saved.Revision, ServerNameMode: 'unset', Overrides: saved.Overrides, Encoding: saved.Encoding,
      });
      expect(result.request().postDataJSON()).not.toHaveProperty('LocalNetworkSubnets');
      expect(api.writes()).toHaveLength(1);
      expect(api.writes()[0].path).toBe('/admin/v1/settings');
      await expect(save).toBeDisabled();
      await expect(modes[3]).toHaveAttribute('aria-pressed', 'true');
      expect(api.settings.ServerNameMode).toBe('unset');
      expect(api.settings.Revision).toBe((BigInt(saved.Revision) + 1n).toString());
      await expect(networks).toHaveValue('10.0.0.0/8');
      await expect(policyActions.getByRole('status')).toHaveText(text.policyDirty);
      await expect(policyActions.getByRole('button', { name: text.savePolicy, exact: true })).toBeEnabled();
    });
  });
}

test.describe('desktop name source', () => {
  test.use({ viewport: { width: 1440, height: 900 }, preferences: { language: 'en', theme: 'light' } });
  test('the complete mode selector keeps its single row of pills', async ({ page, api }, testInfo) => {
    await page.goto('/admin/settings/general');
    const group = page.getByRole('group', { name: 'Name source', exact: true });
    await expect(group.getByRole('button', { name: 'Deployment default', exact: true })).toBeEnabled();
    await expectCompleteChoices(page, group, 1);
    await evidence(page, testInfo, 'settings-name-source-1440-en-light');
    expect(api.writes()).toEqual([]);
  });
});
