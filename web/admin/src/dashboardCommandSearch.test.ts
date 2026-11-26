import assert from 'node:assert/strict';
import test from 'node:test';
import { searchDashboardCommands } from './dashboardCommandSearch.ts';
import type { Page } from './dashboardNavigation';

const items: { page: Page; label: string; category: string }[] = [
  { page: 'overview', label: 'Sign out', category: 'Commands' },
  { page: 'overview', label: 'Overview', category: 'Pages' },
  { page: 'users', label: 'Users', category: 'Pages' },
  { page: 'tasks', label: 'Tasks', category: 'Pages' },
  { page: 'settings-transcode', label: 'Transcoding', category: 'Pages' },
  { page: 'settings-metadata', label: 'Metadata & subtitles', category: 'Pages' },
];
const translations = new Map([['Sign out', '退出登录'], ['Users', '用户']]);
const zh = (text: string) => translations.get(text) ?? text;

test('dashboard aliases rank their destinations before partial command matches', () => {
  for (const [query, label] of [['  GaiLan ', 'Overview'], ['账户', 'Users'], ['计划', 'Tasks'], ['硬件加速', 'Transcoding'], ['在线提供方', 'Metadata & subtitles'], ['登录', 'Users']]) {
    assert.equal(searchDashboardCommands(items, query, 'zh-CN', zh)[0]?.label, label, query);
  }
  assert.deepEqual(searchDashboardCommands(items, 'gailan', 'zh-CN', zh).map(({ label }) => label), ['Overview']);
  assert.deepEqual(searchDashboardCommands(items, ' ', 'zh-CN', zh), items);
});
