import type { CSSProperties } from 'react';
import type { PlayerPreferences } from '../types';

const outline = [[-1, -1], [1, -1], [-1, 1], [1, 1], [0, -1], [0, 1], [-1, 0], [1, 0]]
  .map(([x, y]) => `${x * .06}em ${y * .06}em 0 #000`).join(', ');

export function subtitleStyles(preferences: Pick<PlayerPreferences, 'subtitleSize' | 'subtitleStyle'>): CSSProperties {
  return {
    '--subtitle-scale': [0.82, 1, 1.2, 1.42][preferences.subtitleSize] ?? 1,
    '--subtitle-shadow': preferences.subtitleStyle === 'outline' ? outline : preferences.subtitleStyle === 'background' ? 'none' : '0 0 3px rgba(0,0,0,.95), 0 2px 12px rgba(0,0,0,.7)',
    '--subtitle-background': preferences.subtitleStyle === 'background' ? 'rgba(8,8,9,.78)' : 'transparent',
    '--subtitle-padding': preferences.subtitleStyle === 'background' ? '.06em .42em' : '0',
  } as CSSProperties;
}
