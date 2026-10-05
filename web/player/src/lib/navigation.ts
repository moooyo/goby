import { api } from './api';
import type { Route } from '../context';

function scope() {
  const session = api.getSession();
  return `${session?.ServerId ?? 'local'}.${session?.User.Id ?? 'anonymous'}`;
}

export function savedPage(key: string, count: number): number {
  try {
    const value = Number(sessionStorage.getItem(`goby.player.page.${scope()}.${key}`) ?? 0);
    return Number.isSafeInteger(value) ? Math.min(count - 1, Math.max(0, value)) : 0;
  } catch { return 0; }
}

export function savePage(key: string, page: number): void {
  try { sessionStorage.setItem(`goby.player.page.${scope()}.${key}`, String(page)); } catch { /* Navigation remains usable without browser storage. */ }
}

export function savedRoute(): Route {
  try {
    const value = JSON.parse(localStorage.getItem(`goby.player.route.${scope()}`) ?? 'null') as Route | null;
    if (value && ['home', 'movies', 'series', 'favorites', 'search', 'settings', 'detail'].includes(value.page)
      && (value.page !== 'detail' || typeof value.id === 'string')) return value;
  } catch { /* Start at home if saved navigation is unavailable. */ }
  return { page: 'home' };
}

export function saveRoute(route: Route): void {
  if (route.page === 'player') return;
  try { localStorage.setItem(`goby.player.route.${scope()}`, JSON.stringify(route)); } catch { /* Keep the current in-memory route. */ }
}
