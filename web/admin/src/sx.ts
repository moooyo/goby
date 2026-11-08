import { useSyncExternalStore } from 'react';
import type { CSSProperties } from 'react';

export const breakpoints = { xs: 0, sm: 640, md: 1000, lg: 1240, xl: 1536 } as const;
export type Breakpoint = keyof typeof breakpoints;
export type Responsive<T> = T | Partial<Record<Breakpoint, T>> | readonly (T | null)[];

const palette: Record<string, string> = {
  'primary.main': 'var(--acc)',
  'primary.light': 'var(--acc-soft)',
  'primary.dark': 'var(--acc-ink)',
  'primary.contrastText': 'var(--on-acc)',
  'secondary.main': 'var(--ink2)',
  'secondary.light': 'var(--s3)',
  'secondary.dark': 'var(--ink)',
  'secondary.contrastText': 'var(--bg)',
  'background.default': 'var(--bg)',
  'background.paper': 'var(--s1)',
  'text.primary': 'var(--ink)',
  'text.secondary': 'var(--ink2)',
  'text.disabled': 'var(--ink3)',
  'action.active': 'var(--ink2)',
  'action.hover': 'var(--s3)',
  'action.selected': 'var(--acc-soft)',
  'action.disabled': 'var(--ink3)',
  'action.disabledBackground': 'var(--s3)',
  'success.main': 'var(--ok)',
  'success.light': 'color-mix(in srgb, var(--ok) 13%, transparent)',
  'success.dark': 'var(--ok)',
  'success.contrastText': 'var(--bg)',
  'warning.main': 'var(--warn)',
  'warning.light': 'color-mix(in srgb, var(--warn) 13%, transparent)',
  'warning.dark': 'var(--warn)',
  'warning.contrastText': 'var(--bg)',
  'error.main': 'var(--err)',
  'error.light': 'color-mix(in srgb, var(--err) 13%, transparent)',
  'error.dark': 'var(--err)',
  'error.contrastText': 'var(--bg)',
  'info.main': 'var(--acc-ink)',
  'info.light': 'var(--acc-soft)',
  'info.dark': 'var(--acc-ink)',
  'info.contrastText': 'var(--on-acc)',
  divider: 'var(--line)',
  'common.white': '#ffffff',
  'common.black': '#000000',
  'grey.50': 'var(--s1)',
  'grey.100': 'var(--s2)',
  'grey.200': 'var(--s3)',
  'grey.300': 'var(--line2)',
  'grey.400': 'var(--ink3)',
  'grey.500': 'var(--ink3)',
  'grey.600': 'var(--ink2)',
  'grey.700': 'var(--ink2)',
  'grey.800': 'var(--ink)',
  'grey.900': 'var(--ink)',
};

const spacingKeys: Record<string, readonly string[]> = {
  m: ['margin'], mt: ['marginTop'], mr: ['marginRight'], mb: ['marginBottom'], ml: ['marginLeft'],
  mx: ['marginLeft', 'marginRight'], my: ['marginTop', 'marginBottom'],
  marginX: ['marginLeft', 'marginRight'], marginY: ['marginTop', 'marginBottom'],
  p: ['padding'], pt: ['paddingTop'], pr: ['paddingRight'], pb: ['paddingBottom'], pl: ['paddingLeft'],
  px: ['paddingLeft', 'paddingRight'], py: ['paddingTop', 'paddingBottom'],
  paddingX: ['paddingLeft', 'paddingRight'], paddingY: ['paddingTop', 'paddingBottom'],
  margin: ['margin'], marginTop: ['marginTop'], marginRight: ['marginRight'], marginBottom: ['marginBottom'], marginLeft: ['marginLeft'],
  marginInline: ['marginInline'], marginInlineStart: ['marginInlineStart'], marginInlineEnd: ['marginInlineEnd'],
  marginBlock: ['marginBlock'], marginBlockStart: ['marginBlockStart'], marginBlockEnd: ['marginBlockEnd'],
  padding: ['padding'], paddingTop: ['paddingTop'], paddingRight: ['paddingRight'], paddingBottom: ['paddingBottom'], paddingLeft: ['paddingLeft'],
  paddingInline: ['paddingInline'], paddingInlineStart: ['paddingInlineStart'], paddingInlineEnd: ['paddingInlineEnd'],
  paddingBlock: ['paddingBlock'], paddingBlockStart: ['paddingBlockStart'], paddingBlockEnd: ['paddingBlockEnd'],
  gap: ['gap'], rowGap: ['rowGap'], columnGap: ['columnGap'],
};
const sizeKeys = new Set(['width', 'height', 'minWidth', 'maxWidth', 'minHeight', 'maxHeight']);
const colorKeys = new Set(['color', 'background', 'backgroundColor', 'borderColor', 'borderTopColor', 'borderRightColor', 'borderBottomColor', 'borderLeftColor', 'outlineColor', 'fill', 'stroke', 'textDecorationColor', 'caretColor', 'accentColor']);
const borderKeys = new Set(['border', 'borderTop', 'borderRight', 'borderBottom', 'borderLeft', 'borderInline', 'borderInlineStart', 'borderInlineEnd', 'borderBlock', 'borderBlockStart', 'borderBlockEnd', 'outline']);
const shadows = ['none', '0 1px 3px rgb(0 0 0 / .12)', '0 4px 12px rgb(0 0 0 / .16)', '0 12px 32px rgb(0 0 0 / .2)'];
const typeStyles: Record<string, CSSProperties> = {
  h1: { fontFamily: 'var(--serif)', fontSize: 28, lineHeight: 1.4, fontWeight: 900 },
  h2: { fontFamily: 'var(--serif)', fontSize: 24, lineHeight: 1.4, fontWeight: 900 },
  h3: { fontSize: 16, lineHeight: 1.5, fontWeight: 600 },
  h4: { fontSize: 15, lineHeight: 1.5, fontWeight: 600 },
  h5: { fontSize: 14, lineHeight: 1.5, fontWeight: 600 },
  h6: { fontSize: 14, lineHeight: 1.5, fontWeight: 600 },
  body1: { fontSize: 13.5, lineHeight: 1.5 },
  body2: { fontSize: 13, lineHeight: 1.5 },
  caption: { fontSize: 11.5, lineHeight: 1.5 },
  overline: { fontSize: 11, lineHeight: 1.5, letterSpacing: '.12em', fontWeight: 600 },
  button: { fontSize: 13, lineHeight: 1.5, fontWeight: 600 },
};

/** Resolve a palette path while leaving ordinary CSS colors intact. */
export function themeColor(value: unknown): string | undefined {
  return typeof value === 'string' ? palette[value] ?? value : undefined;
}

/** Numeric spacing uses the existing eight-pixel rhythm; CSS strings stay exact. */
export function spacing(value: unknown): string | number | undefined {
  if (typeof value === 'number') return value * 8;
  return typeof value === 'string' ? value : undefined;
}

export const sxTheme = {
  spacing: (...values: (string | number)[]) => values.map((value) => typeof value === 'number' ? `${value * 8}px` : value).join(' '),
  shape: { borderRadius: 4 },
  palette: {
    primary: { main: palette['primary.main'], light: palette['primary.light'], dark: palette['primary.dark'], contrastText: palette['primary.contrastText'] },
    secondary: { main: palette['secondary.main'], light: palette['secondary.light'], dark: palette['secondary.dark'] },
    text: { primary: palette['text.primary'], secondary: palette['text.secondary'], disabled: palette['text.disabled'] },
    background: { default: palette['background.default'], paper: palette['background.paper'] },
    success: { main: palette['success.main'], light: palette['success.light'], dark: palette['success.dark'] },
    warning: { main: palette['warning.main'], light: palette['warning.light'], dark: palette['warning.dark'] },
    error: { main: palette['error.main'], light: palette['error.light'], dark: palette['error.dark'] },
    info: { main: palette['info.main'], light: palette['info.light'], dark: palette['info.dark'] },
    divider: palette.divider,
  },
  breakpoints: {
    values: breakpoints,
    up: (key: Breakpoint | number) => `@media (min-width:${typeof key === 'number' ? key : breakpoints[key]}px)`,
    down: (key: Breakpoint | number) => `@media (max-width:${(typeof key === 'number' ? key : breakpoints[key]) - .05}px)`,
  },
  typography: typeStyles,
} as const;

export type SxTheme = typeof sxTheme;
export type Sx = Record<string, unknown> | readonly Sx[] | ((theme: SxTheme) => Sx) | boolean | null | undefined;

function viewportWidth(): number {
  return typeof window === 'undefined' ? 1440 : window.innerWidth;
}

const widthListeners = new Set<() => void>();
let resizeListening = false;
function onResize(): void { widthListeners.forEach((listener) => listener()); }
function subscribeWidth(listener: () => void): () => void {
  widthListeners.add(listener);
  if (!resizeListening && typeof window !== 'undefined') {
    window.addEventListener('resize', onResize, { passive: true });
    resizeListening = true;
  }
  return () => {
    widthListeners.delete(listener);
    if (resizeListening && widthListeners.size === 0 && typeof window !== 'undefined') {
      window.removeEventListener('resize', onResize);
      resizeListening = false;
    }
  };
}

export function useViewportWidth(): number {
  return useSyncExternalStore(subscribeWidth, viewportWidth, () => 1440);
}

/** Responsive declarations only inherit from breakpoints that are present. */
export function resolveResponsive<T = unknown>(value: Responsive<T>, width = viewportWidth()): T | undefined {
  if (Array.isArray(value)) {
    let resolved: T | undefined;
    (Object.keys(breakpoints) as Breakpoint[]).forEach((key, index) => {
      if (width >= breakpoints[key] && value[index] !== null && value[index] !== undefined) resolved = value[index] as T;
    });
    return resolved;
  }
  if (value && typeof value === 'object') {
    const responsive = value as Record<string, T>;
    if (!Object.keys(responsive).some((key) => key in breakpoints)) return undefined;
    let resolved: T | undefined;
    (Object.keys(breakpoints) as Breakpoint[]).forEach((key) => {
      if (width >= breakpoints[key] && responsive[key] !== null && responsive[key] !== undefined) resolved = responsive[key];
    });
    return resolved;
  }
  return value as T | undefined;
}

/**
 * Convert the flat layout declarations retained by business views into styles.
 * Interaction selectors and media rules belong to the static component CSS.
 */
export function sxStyle(sx: unknown, width = viewportWidth()): CSSProperties {
  if (!sx || typeof sx === 'boolean') return {};
  if (typeof sx === 'function') return sxStyle(sx(sxTheme), width);
  if (Array.isArray(sx)) return Object.assign({}, ...sx.map((entry) => sxStyle(entry, width))) as CSSProperties;
  if (typeof sx !== 'object') return {};
  const result: Record<string, string | number | undefined> = {};
  for (const [sourceKey, raw] of Object.entries(sx)) {
    if (sourceKey === '&' || sourceKey === '&&') {
      Object.assign(result, sxStyle(raw, width));
      continue;
    }
    if (raw === null || raw === undefined || sourceKey.startsWith('&') || sourceKey.startsWith('@') || sourceKey.includes(' ') || sourceKey.includes(':')) continue;
    const value = resolveResponsive<unknown>(raw, width);
    if (value === undefined || value === null || typeof value === 'object' || typeof value === 'boolean') continue;
    const key = sourceKey === 'bgcolor' ? 'backgroundColor' : sourceKey;
    if (key === 'typography') {
      Object.assign(result, typeStyles[String(value)] ?? {});
      continue;
    }
    const spacingTargets = spacingKeys[key];
    if (spacingTargets) {
      const converted = spacing(value);
      for (const target of spacingTargets) result[target] = converted;
      continue;
    }
    if (colorKeys.has(key)) {
      result[key] = themeColor(value);
      continue;
    }
    if (borderKeys.has(key) && typeof value === 'number') {
      result[key] = value === 0 ? 0 : `${value}px solid var(--line)`;
      continue;
    }
    if (sizeKeys.has(key) && typeof value === 'number' && value > 0 && value <= 1) {
      result[key] = `${value * 100}%`;
      continue;
    }
    if (key === 'borderRadius' && typeof value === 'number') {
      result[key] = value * 4;
      continue;
    }
    if (key === 'boxShadow' && typeof value === 'number') {
      result[key] = shadows[Math.min(Math.max(0, Math.floor(value)), shadows.length - 1)];
      continue;
    }
    if (key === 'fontWeight' && typeof value === 'string') {
      result[key] = ({ light: 300, regular: 400, medium: 500, semibold: 600, bold: 700 } as Record<string, number>)[value] ?? value;
      continue;
    }
    if (key === 'zIndex' && typeof value === 'string') {
      result[key] = ({ appBar: 100, drawer: 110, modal: 1300, snackbar: 1400, tooltip: 1500 } as Record<string, number>)[value] ?? value;
      continue;
    }
    if (typeof value === 'string' || typeof value === 'number') result[key] = typeof value === 'string' ? value.replace(/\s*!important\s*$/i, '') : value;
  }
  return result as CSSProperties;
}

export function useSx(sx: unknown): CSSProperties {
  return sxStyle(sx, useViewportWidth());
}
