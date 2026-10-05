import { createContext, useContext } from 'react';
import type { AuthSession, MediaItem, PlayerPreferences } from './types';

export interface Route {
  page: 'home' | 'movies' | 'series' | 'favorites' | 'search' | 'settings' | 'detail' | 'player';
  id?: string;
  restart?: boolean;
}

export interface AppState {
  session: AuthSession;
  prefs: PlayerPreferences;
  setPrefs: (patch: Partial<PlayerPreferences>) => void;
  navigate: (route: Route) => void;
  play: (item: MediaItem, restart?: boolean) => void;
  notify: (message: string) => void;
  revision: number;
  changed: () => void;
  setBackdrop: (item: MediaItem | null, mode?: 'stage' | 'blur' | 'none', accentItem?: MediaItem | null) => void;
  logout: () => Promise<void>;
}

export const AppContext = createContext<AppState | null>(null);
export function useApp() {
  const value = useContext(AppContext);
  if (!value) throw new Error('AppContext is not available');
  return value;
}
