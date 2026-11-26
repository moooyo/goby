import { administrator, expect, json, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import { installUserDesignFixture, viewerId } from './user-design-fixture';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off' });

const lastLogin = '2026-10-12T10:45:00Z';
const lastActivity = '2026-10-13T11:30:00Z';
const lastPlayed = '2026-10-11T09:15:00Z';
const known = { Id: viewerId, LastLoginDate: lastLogin, LastActivityDate: lastActivity, LastPlayedDate: lastPlayed, PlayedItemCount: 17 };
const never = { Id: administrator.Id, LastLoginDate: null, LastActivityDate: null, LastPlayedDate: null, PlayedItemCount: 0 };

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  const mobile = viewport.width < 600;
  test.describe(`${viewport.width}px recorded user logins`, () => {
    test.use({ viewport, preferences: { language: mobile ? 'zh' : 'en', theme: mobile ? 'dark' : 'light' } });
    test('the Users list and profile expose only the last recorded login', async ({ page, api }, testInfo) => {
      installUserDesignFixture(api);
      api.handlers.set('GET /admin/v1/users/activity', (route, request) => {
        const ids = new URL(request.url()).searchParams.get('UserIds')!.split(',');
        return json(route, { Items: [known, never].filter((item) => ids.includes(item.Id)), ObservedAt: stamp });
      });
      await page.goto('/admin/access/users');
      const table = page.getByRole('table', { name: mobile ? '服务器用户' : 'Server users', exact: true });
      const row = table.getByRole('row').filter({ hasText: 'Family viewer' });
      await expect(row.locator(`time[datetime="${lastLogin}"]`)).toHaveCount(1);
      await expect(row.locator(`time[datetime="${lastActivity}"],time[datetime="${lastPlayed}"]`)).toHaveCount(0);
      if (mobile) {
        const scroller = page.getByRole('region', { name: '账户表格' });
        const dimensions = await scroller.evaluate((element) => ({ total: element.scrollWidth, visible: element.clientWidth }));
        expect(dimensions.total).toBeGreaterThan(dimensions.visible);
      }
      await noOverflow(page);
      await page.getByRole('button', { name: `${mobile ? '管理' : 'Manage'} Family viewer`, exact: true }).click();
      const sheet = page.getByRole('dialog');
      await sheet.getByRole('tab', { name: mobile ? '个人资料' : 'Profile', exact: true }).click();
      const profile = sheet.getByRole('tabpanel');
      await expect(profile.locator(`time[datetime="${lastLogin}"]`)).toBeVisible();
      await expect(profile.locator(`time[datetime="${lastActivity}"],time[datetime="${lastPlayed}"]`)).toHaveCount(0);
      await expect(profile).not.toContainText(/Watched items|Last played|已看条目|最近播放/);
      await expect(profile.getByRole('switch', { name: mobile ? '停用账户' : 'Disable account' })).toBeVisible();
      const bounds = await sheet.boundingBox();
      expect(bounds?.width).toBe(mobile ? viewport.width : 560);
      expect(api.writes()).toHaveLength(0);
      await noOverflow(page);
      await evidence(page, testInfo, `user-logins-profile-${viewport.width}`);
    });
  });
}

test('omitted and failed activity never qualifies as no recorded login', async ({ page, api }) => {
  installUserDesignFixture(api);
  const accounts = [administrator, ...Array.from({ length: 205 }, (_, i) => ({ ...administrator, Id: `login-lookup-${i}`, Name: `Member ${String(i).padStart(3, '0')}`, IsAdministrator: false }))];
  const uncertain = accounts[2];
  api.handlers.set('GET /admin/v1/users', (route) => json(route, { Items: accounts, TotalRecordCount: accounts.length }));
  let recovered = false;
  api.handlers.set('GET /admin/v1/users/activity', (route, request) => {
    const ids = new URL(request.url()).searchParams.get('UserIds')!.split(',');
    if (!recovered && ids.length > 50) return json(route, { Error: { Code: 'service_unavailable', Message: 'Synthetic activity service unavailable.' } }, 503);
    return json(route, { Items: ids.filter((id) => recovered || id !== uncertain.Id).map((id) => id === administrator.Id || id === accounts[1].Id ? { ...never, Id: id } : { ...known, Id: id }), ObservedAt: stamp });
  });
  await page.goto('/admin/access/users');
  const table = page.getByRole('table', { name: 'Server users', exact: true });
  await expect(table.getByRole('row').filter({ hasText: uncertain.Name }).getByRole('cell', { name: 'Unavailable', exact: true })).toBeVisible();
  const idle = page.getByRole('group', { name: 'Account groups' }).getByRole('button', { name: /No login in 90 days/ });
  await expect(idle).toContainText('Unavailable');
  await idle.click();
  await expect(page.getByText('Some login records are unavailable.', { exact: false })).toBeVisible();
  await expect(table.getByRole('row').filter({ hasText: accounts[1].Name })).toContainText('No login recorded');
  await expect(table.getByRole('row').filter({ hasText: uncertain.Name })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Disable these accounts/ })).toHaveCount(0);
  recovered = true;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(idle).toHaveText('No login in 90 days 2');
  await expect(page.getByRole('button', { name: 'Disable these accounts (1)', exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(0);
});

test('a malformed user activity response is not shown as a known login date', async ({ page, api }) => {
  installUserDesignFixture(api);
  api.handlers.set('GET /admin/v1/users/activity', (route) => json(route, { Items: [{ ...known, LastLoginDate: 'invalid' }], ObservedAt: stamp }));
  await page.goto('/admin/access/users');
  await expect(page.getByText('The user activity response is incomplete. Refresh to try again.', { exact: true })).toBeVisible();
  const row = page.getByRole('table', { name: 'Server users', exact: true }).getByRole('row').filter({ hasText: 'Family viewer' });
  await expect(row.getByRole('cell', { name: 'Unavailable', exact: true })).toHaveCount(1);
  await expect(row).not.toContainText('No login recorded');
  expect(api.writes()).toHaveLength(0);
});
