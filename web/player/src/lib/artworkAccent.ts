import { useEffect, useState, useSyncExternalStore, type CSSProperties } from 'react';
import type { MediaItem } from '../types';
import { api } from './api';

export type AccentStyle = CSSProperties & Record<'--accent' | '--accent-soft' | '--accent-glow' | '--accent-line', string>;

export function deriveArtworkAccent(image: HTMLImageElement): AccentStyle | undefined {
  try {
    const canvas = document.createElement('canvas'); canvas.width = 32; canvas.height = 32;
    const context = canvas.getContext('2d', { willReadFrequently: true }); if (!context) return;
    context.drawImage(image, 0, 0, 32, 32);
    const values = context.getImageData(0, 0, 32, 32).data;
    const bins = Array.from({ length: 24 }, () => ({ weight: 0, r: 0, g: 0, b: 0 }));
    let totalWeight = 0;
    for (let i = 0; i < values.length; i += 4) {
      const r = values[i] / 255, g = values[i + 1] / 255, b = values[i + 2] / 255;
      const max = Math.max(r, g, b), min = Math.min(r, g, b), chroma = max - min, lightness = (max + min) / 2;
      if (chroma < .1 || lightness < .1 || lightness > .92) continue;
      let hue = max === r ? (g - b) / chroma % 6 : max === g ? (b - r) / chroma + 2 : (r - g) / chroma + 4;
      hue = (hue * 60 + 360) % 360;
      const weight = chroma * chroma * (1 - Math.abs(lightness - .5) * 1.2);
      const bin = bins[Math.floor(hue / 15) % 24];
      bin.weight += weight; bin.r += r * weight; bin.g += g * weight; bin.b += b * weight;
      totalWeight += weight;
    }
    if (totalWeight < 6) return;
    let dominant = 0, dominantWeight = -1;
    for (let i = 0; i < bins.length; i++) {
      const weight = bins[i].weight + .6 * (bins[(i + 1) % 24].weight + bins[(i + 23) % 24].weight);
      if (weight > dominantWeight) { dominantWeight = weight; dominant = i; }
    }
    const bin = bins[dominant]; if (!bin.weight) return;
    const r = bin.r / bin.weight, g = bin.g / bin.weight, b = bin.b / bin.weight;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), delta = max - min;
    const l = (max + min) / 2;
    let hue = delta === 0 ? 0 : max === r ? (g - b) / delta % 6 : max === g ? (b - r) / delta + 2 : (r - g) / delta + 4;
    hue = Math.round((hue * 60 + 360) % 360);
    const rawSaturation = delta ? Number((delta / (1 - Math.abs(2 * l - 1))).toFixed(3)) : 0;
    const saturation = Math.round(Math.max(.36, Math.min(.72, rawSaturation)) * 100);
    const lightness = Math.round(Math.max(.6, Math.min(.7, Number(l.toFixed(3)))) * 100);
    return {
      '--accent': `hsl(${hue} ${saturation}% ${lightness}%)`,
      '--accent-soft': `hsl(${hue} ${saturation}% ${lightness}% / .16)`,
      '--accent-glow': `hsl(${hue} ${saturation}% ${lightness}% / .42)`,
      '--accent-line': `hsl(${hue} ${saturation}% ${lightness}% / .55)`,
    };
  } catch { /* Cross-origin artwork keeps the neutral accent. */ }
}

type ArtworkScope = { owner: string | null };
type ArtworkResult = { scope: ArtworkScope; url: string; style: AccentStyle | undefined };

const cacheLimit = 64;
const cache = new Map<string, AccentStyle | undefined>();
const sessionListeners = new Set<() => void>();
let artworkScope: ArtworkScope = { owner: null };

function currentScope(): ArtworkScope {
  const session = api.getSession();
  const owner = session ? JSON.stringify([session.serverUrl, session.ServerId, session.User.Id, session.AccessToken]) : null;
  if (owner !== artworkScope.owner) {
    // A fresh scope also rejects a pending result after an A-to-B-to-A switch.
    artworkScope = { owner };
    cache.clear();
  }
  return artworkScope;
}

function sessionChanged(): void {
  currentScope();
  sessionListeners.forEach(listener => listener());
}

function subscribeToSession(listener: () => void): () => void {
  if (!sessionListeners.size) window.addEventListener('goby:session', sessionChanged);
  sessionListeners.add(listener);
  return () => {
    sessionListeners.delete(listener);
    if (!sessionListeners.size) window.removeEventListener('goby:session', sessionChanged);
  };
}

function remember(url: string, style: AccentStyle | undefined): void {
  cache.delete(url);
  cache.set(url, style);
  if (cache.size > cacheLimit) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
}

export function useArtworkAccent(item: MediaItem | undefined | null): CSSProperties | undefined {
  const scope = useSyncExternalStore(subscribeToSession, currentScope, currentScope);
  const url = scope.owner && item ? api.imageUrl(item, 'Primary', 320) : '';
  const [result, setResult] = useState<ArtworkResult>();

  useEffect(() => {
    if (!url || !scope.owner) { setResult(undefined); return; }
    if (currentScope() !== scope) return;
    if (cache.has(url)) {
      const style = cache.get(url);
      remember(url, style);
      setResult({ scope, url, style });
      return;
    }

    setResult(undefined);
    let alive = true;
    let image: HTMLImageElement | undefined = new Image();
    const release = () => {
      if (!image) return;
      image.onload = null;
      image.onerror = null;
      image.removeAttribute('src');
      image = undefined;
    };
    const finish = (style: AccentStyle | undefined, cacheable = true) => {
      release();
      if (!alive || currentScope() !== scope) return;
      if (cacheable) remember(url, style);
      setResult({ scope, url, style });
    };
    image.crossOrigin = 'anonymous';
    image.onload = () => finish(image ? deriveArtworkAccent(image) : undefined);
    image.onerror = () => finish(undefined, false);
    image.src = url;
    return () => { alive = false; release(); };
  }, [scope, url]);

  return result?.scope === scope && result.url === url ? result.style : undefined;
}
