import { administrator, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import { installUserDesignFixture, viewerId } from './user-design-fixture';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });

const lastLogin = '2026-10-12T10:45:00Z';
const lastActivity = '2026-10-13T11:30:00Z';
const lastPlayed = '2026-10-11T09:15:00Z';
const known = { Id: viewerId, LastLoginDate: lastLogin, LastActivityDate: lastActivity, LastPlayedDate: lastPlayed, PlayedItemCount: 17 };
const never = { Id: administrator.Id, LastLoginDate: null, LastActivityDate: null, LastPlayedDate: null, PlayedItemCount: 0 };

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test.describe(`${viewport.width}px recorded user activity`, () => {
    test.use({ viewport });
    test('the list and profile use recorded dates and completed-item counts', async ({ page, api }, testInfo) => {
      installUserDesignFixture(api);
      api.handlers.set('GET /admin/v1/users/activity', (route, request) => {
        const ids = new URL(request.url()).searchParams.get('UserIds')!.split(',');
        return json(route, { Items: [known, never].filter((item) => ids.includes(item.Id)), ObservedAt: stamp });
      });
      await page.goto('/admin/access/users');
      const row = page.getByRole('table', { name: 'Server users', exact: true }).getByRole('row').filter({ hasText: 'Family viewer' });
      if (viewport.width > 999) {
        await expect(row.getByRole('cell', { name: '17', exact: true })).toBeVisible();
        await expect(row.locator(`time[datetime="${lastActivity}"]`)).toBeVisible();
      }
      await page.getByRole('button', { name: 'Manage Family viewer', exact: true }).click();
      const sheet = page.getByRole('dialog');
      await sheet.getByRole('tab', { name: 'Profile', exact: true }).click();
      const activity = sheet.getByRole('region', { name: 'Recorded user activity', exact: true });
      for (const date of [lastLogin, lastActivity, lastPlayed]) await expect(activity.locator(`time[datetime="${date}"]`)).toBeVisible();
      await expect(activity).toContainText('17');
      await expect(activity).toContainText('not individual plays or viewing hours');
      await expect(activity.locator(`time[datetime="${stamp}"]`)).toHaveCount(0);
      expect(api.writes()).toHaveLength(0);
      await noOverflow(page);
      await evidence(page, testInfo, `user-activity-profile-${viewport.width}`);
    });
  });
}

test('unknown activity remains unknown and a failed refresh retains its last successful data', async ({ page, api }) => {
  installUserDesignFixture(api);
  let unavailable = false;
  api.handlers.set('GET /admin/v1/users/activity', (route, request) => {
    if (unavailable) return json(route, { Error: { Code: 'service_unavailable', Message: 'Synthetic activity service unavailable.' } }, 503);
    const ids = new URL(request.url()).searchParams.get('UserIds')!.split(',');
    return json(route, { Items: [known, never].filter((item) => ids.includes(item.Id)), ObservedAt: stamp });
  });
  await page.goto('/admin/access/users');
  const table = page.getByRole('table', { name: 'Server users', exact: true });
  const unknown = table.getByRole('row').filter({ hasText: administrator.Name });
  await expect(unknown.getByRole('cell', { name: 'Not recorded', exact: true })).toBeVisible();
  await expect(unknown.getByRole('cell', { name: '0', exact: true })).toBeVisible();
  unavailable = true;
  await page.getByRole('button', { name: 'Refresh users', exact: true }).click();
  await expect(page.getByText('Showing the last successful activity update.', { exact: true })).toBeVisible();
  const recorded = table.getByRole('row').filter({ hasText: 'Family viewer' });
  await expect(recorded.getByRole('cell', { name: '17', exact: true })).toBeVisible();
  await expect(recorded.locator(`time[datetime="${lastActivity}"]`)).toBeVisible();
  unavailable = false;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByText('Showing the last successful activity update.', { exact: true })).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});

test('a malformed activity count is rejected instead of displayed as zero', async ({ page, api }) => {
  installUserDesignFixture(api);
  api.handlers.set('GET /admin/v1/users/activity', (route) => json(route, { Items: [{ ...known, PlayedItemCount: -1 }], ObservedAt: stamp }));
  await page.goto('/admin/access/users');
  await expect(page.getByText('The user activity response is incomplete. Refresh to try again.', { exact: true })).toBeVisible();
  const row = page.getByRole('table', { name: 'Server users', exact: true }).getByRole('row').filter({ hasText: 'Family viewer' });
  await expect(row.getByRole('cell', { name: 'Unavailable', exact: true })).toHaveCount(2);
  await expect(row.getByRole('cell', { name: '0', exact: true })).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});
