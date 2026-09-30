export type SettingsSection = 'general' | 'transcode' | 'hardware' | 'metadata';
export type Page = 'overview' | 'users' | 'libraries' | 'tasks' | 'metadata' | 'sessions' | 'devices' | 'api-keys' | 'settings' | 'settings-transcode' | 'settings-hardware' | 'settings-metadata' | 'observability' | 'backups' | 'providers' | 'collections' | 'artwork' | 'notifications' | 'media-analysis';
export type Group = 'overview' | 'media' | 'access' | 'system' | 'settings';
export const groups: { id: Group; title: string; label: string; tabs: { page: Page; label: string }[] }[] = [
  { id: 'overview', title: 'Overview', label: 'Overview', tabs: [] },
  { id: 'media', title: 'Media', label: 'Media', tabs: [{ page: 'libraries', label: 'Libraries' }, { page: 'artwork', label: 'Catalog artwork' }, { page: 'collections', label: 'Playlists & collections' }, { page: 'media-analysis', label: 'Media analysis' }] },
  { id: 'access', title: 'Access control', label: 'Access', tabs: [{ page: 'users', label: 'Users' }, { page: 'devices', label: 'Devices' }, { page: 'sessions', label: 'Sessions' }, { page: 'api-keys', label: 'API keys' }] },
  { id: 'system', title: 'System', label: 'System', tabs: [{ page: 'tasks', label: 'Tasks' }, { page: 'observability', label: 'Activity & logs' }, { page: 'notifications', label: 'Notifications' }, { page: 'backups', label: 'Backups & recovery' }] },
  { id: 'settings', title: 'Settings', label: 'Settings', tabs: [{ page: 'settings', label: 'General' }, { page: 'settings-transcode', label: 'Transcode limits' }, { page: 'settings-hardware', label: 'Hardware acceleration' }, { page: 'settings-metadata', label: 'Metadata & subtitles' }, { page: 'providers', label: 'Online providers' }] },
];
const paths: Record<Exclude<Page, 'metadata'>, string> = {
  overview: '/admin/', libraries: '/admin/media/libraries', artwork: '/admin/media/artwork', collections: '/admin/media/collections', 'media-analysis': '/admin/media/analysis',
  users: '/admin/access/users', devices: '/admin/access/devices', sessions: '/admin/access/sessions', 'api-keys': '/admin/access/api-keys',
  tasks: '/admin/system/tasks', observability: '/admin/system/observability', notifications: '/admin/system/notifications', backups: '/admin/system/backups',
  settings: '/admin/settings/general', 'settings-transcode': '/admin/settings/transcode', 'settings-hardware': '/admin/settings/hardware', 'settings-metadata': '/admin/settings/metadata', providers: '/admin/settings/providers',
};
export function settingsSection(page: Page): SettingsSection | undefined {
  if (page === 'settings') return 'general';
  if (page === 'settings-transcode') return 'transcode';
  if (page === 'settings-hardware') return 'hardware';
  if (page === 'settings-metadata') return 'metadata';
  return undefined;
}
export function groupForPage(page: Page) {
  const selected = page === 'metadata' ? 'libraries' : page;
  return groups.find((group) => group.tabs.some((tab) => tab.page === selected)) ?? groups[0];
}
export function metadataLibraryFromLocation(pathname = window.location.pathname): string | undefined {
  const match = /^\/admin\/(?:media\/)?libraries\/([^/]+)\/items\/?$/.exec(pathname);
  if (!match) return undefined;
  try { return decodeURIComponent(match[1]); } catch { return undefined; }
}
export function pageFromLocation(pathname = window.location.pathname): Page {
  const path = pathname.replace(/\/+$/, '');
  if (metadataLibraryFromLocation(path) !== undefined) return 'metadata';
  const exact = Object.entries(paths).find(([, value]) => value.replace(/\/+$/, '') === path);
  if (exact) return exact[0] as Page;
  const legacy = path.replace(/^\/admin\//, '');
  if (Object.prototype.hasOwnProperty.call(paths, legacy)) return legacy as Exclude<Page, 'metadata'>;
  return 'overview';
}
export function pageURL(page: Page, libraryId?: string): string {
  return page === 'metadata' ? libraryId ? `/admin/media/libraries/${encodeURIComponent(libraryId)}/items` : paths.libraries : paths[page];
}
export type LastTabs = Record<Group, Page>;
export function readLastTabs(): LastTabs {
  const result: LastTabs = { overview: 'overview', media: 'libraries', access: 'users', system: 'tasks', settings: 'settings' };
  try {
    const saved: unknown = JSON.parse(window.sessionStorage.getItem('goby.dashboard.tabs') ?? '{}');
    if (saved && typeof saved === 'object') for (const group of groups) {
      const page = (saved as Record<string, unknown>)[group.id];
      if (group.tabs.some((tab) => tab.page === page)) result[group.id] = page as Page;
    }
  } catch { /* Navigation still works when browser storage is unavailable. */ }
  return result;
}
