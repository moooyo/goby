import type { Page } from './dashboardNavigation';

const pageAliases: Partial<Record<Page, readonly string[]>> = {
  overview: ['gailan'],
  users: ['account', 'accounts', 'devices', 'sessions', 'logins', 'sign-ins', 'login', '账户', '设备', '会话', '登录'],
  tasks: ['计划'],
  'settings-transcode': ['硬件加速'],
  'settings-metadata': ['在线提供方'],
};

export function searchDashboardCommands<T extends { page: Page; label: string; category: string }>(items: readonly T[], query: string, locale: string, translate: (text: string) => string): T[] {
  const normalize = (text: string) => text.toLocaleLowerCase(locale);
  const term = normalize(query.trim());
  if (!term) return [...items];
  const exact: T[] = [];
  const partial: T[] = [];
  for (const item of items) {
    const aliases = (item.category === 'Pages' ? pageAliases[item.page] ?? [] : []).map(normalize);
    if (aliases.includes(term)) exact.push(item);
    else if (normalize([item.label, translate(item.label), translate(item.category), ...aliases].join(' ')).includes(term)) partial.push(item);
  }
  return exact.concat(partial);
}
