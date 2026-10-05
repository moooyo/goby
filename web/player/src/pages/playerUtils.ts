import type { Chapter, MediaItem, MediaStream } from '../types';

export const TICKS_PER_SECOND = 10_000_000;
export const CREDITS_COUNTDOWN_SECONDS = 10;

export function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, Number.isFinite(value) ? value : min));
}

export function clock(seconds: number) {
  const value = Math.floor(Math.max(0, Number.isFinite(seconds) ? seconds : 0));
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor(value / 60) % 60;
  const rest = String(value % 60).padStart(2, '0');
  return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${rest}` : `${minutes}:${rest}`;
}

export function introInterval(chapters: Chapter[] | undefined, duration: number) {
  const ordered = [...(chapters ?? [])].sort((a, b) => a.StartPositionTicks - b.StartPositionTicks);
  for (let index = 0; index < ordered.length; index += 1) {
    const chapter = ordered[index];
    if (chapter.MarkerType !== 'IntroStart') continue;
    const end = ordered.slice(index + 1).find((entry) => entry.MarkerType === 'IntroEnd');
    const from = chapter.StartPositionTicks / TICKS_PER_SECOND;
    const to = (end?.StartPositionTicks ?? 0) / TICKS_PER_SECOND;
    if (from >= 0 && to > from && (!duration || to <= duration)) return { from, to };
  }
  return null;
}

export interface CreditsInterval {
  from: number;
  to: number;
}

export function creditsIntervals(chapters: Chapter[] | undefined, duration: number): CreditsInterval[] | null {
  if (!Number.isFinite(duration) || duration <= 0) return null;
  const markers = chapters?.filter((chapter) => chapter.MarkerType === 'CreditsStart' || chapter.MarkerType === 'CreditsEnd') ?? [];
  if (!markers.length || markers.some(marker => !Number.isSafeInteger(marker.StartPositionTicks))) return null;
  // A single start remains the legacy manual marker extending to the end of the file.
  if (markers.length === 1 && markers[0].MarkerType === 'CreditsStart') {
    const from = markers[0].StartPositionTicks / TICKS_PER_SECOND;
    return from >= 0 && from < duration ? [{ from, to: duration }] : null;
  }
  if (markers.length % 2 !== 0) return null;
  const intervals: CreditsInterval[] = [];
  for (let index = 0; index < markers.length; index += 2) {
    const start = markers[index];
    const end = markers[index + 1];
    const from = start.StartPositionTicks / TICKS_PER_SECOND;
    const to = end.StartPositionTicks / TICKS_PER_SECOND;
    // Preserve wire order: sorting broken pairs could skip a coda or another scene.
    if (start.MarkerType !== 'CreditsStart' || end.MarkerType !== 'CreditsEnd' || from < 0 || from >= duration
      || to <= from || to > duration || intervals.length > 0 && from < intervals[intervals.length - 1].to) return null;
    intervals.push({ from, to });
  }
  return intervals;
}

export function creditsStart(chapters: Chapter[] | undefined, duration: number) {
  if (!Number.isFinite(duration) || duration <= 0) return null;
  const markers = chapters?.filter((chapter) => chapter.MarkerType === 'CreditsStart') ?? [];
  if (markers.length !== 1) return null;
  if (!Number.isFinite(markers[0].StartPositionTicks)) return null;
  const from = markers[0].StartPositionTicks / TICKS_PER_SECOND;
  return Number.isFinite(from) && from >= 0 && from < duration ? from : null;
}

export function gobyCreditsIntervals(value: unknown, duration: number): CreditsInterval[] | null {
  if (!Number.isFinite(duration) || duration <= 0 || !Array.isArray(value) || !value.length) return null;
  const intervals: CreditsInterval[] = [];
  for (const entry of value) {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry) || !Number.isSafeInteger(entry.StartPositionTicks)
      || !Number.isSafeInteger(entry.EndPositionTicks) || typeof entry.Source !== 'string' || !entry.Source.trim()) return null;
    const from = entry.StartPositionTicks / TICKS_PER_SECOND;
    const to = entry.EndPositionTicks / TICKS_PER_SECOND;
    if (from < 0 || from >= duration || to <= from || to > duration || intervals.length > 0 && from < intervals[intervals.length - 1].to) return null;
    intervals.push({ from, to });
  }
  return intervals;
}

type CreditsOwner = Pick<MediaItem, 'GobyCreditsIntervals' | 'Chapters'>;

export function resolveCreditsIntervals(source: CreditsOwner | null | undefined, item: CreditsOwner | null | undefined, duration: number): CreditsInterval[] | null {
  // A negotiated source is authoritative, including absent or explicitly empty metadata.
  const owner = source ?? item;
  if (!owner) return null;
  if (owner.GobyCreditsIntervals !== undefined) return gobyCreditsIntervals(owner.GobyCreditsIntervals, duration);
  const from = creditsStart(owner.Chapters, duration);
  return from === null ? null : [{ from, to: duration }];
}

export function canSeekTo(ranges: TimeRanges, position: number) {
  for (let index = 0; index < ranges.length; index += 1) {
    if (position >= ranges.start(index) && position <= ranges.end(index)) return true;
  }
  return false;
}

export function episodeLabel(item: MediaItem) {
  const season = item.ParentIndexNumber == null ? '' : `第 ${item.ParentIndexNumber} 季`;
  const episode = item.IndexNumber == null ? '' : `第 ${item.IndexNumber} 集`;
  return `${season}${episode}`;
}

export function streamLabel(stream: MediaStream) {
  const languages: Record<string, string> = {
    chi: '中文', zho: '中文', zh: '中文', 'zh-CN': '简体中文', 'zh-TW': '繁体中文',
    eng: '英语', en: '英语', jpn: '日语', ja: '日语', kor: '韩语', ko: '韩语',
    fra: '法语', fre: '法语', fr: '法语', deu: '德语', ger: '德语', de: '德语',
    spa: '西班牙语', es: '西班牙语', und: '未标注语言',
  };
  const language = languages[stream.Language?.toLowerCase() ?? ''] ?? stream.DisplayLanguage ?? stream.Language ?? '未标注语言';
  const codec = stream.Codec?.toUpperCase();
  const channels = stream.Channels === 6 ? '5.1' : stream.Channels === 8 ? '7.1' : stream.Channels === 2 ? '立体声' : stream.Channels === 1 ? '单声道' : stream.Channels ? `${stream.Channels} 声道` : '';
  return stream.Title || (stream.DisplayTitle && /[\u3400-\u9fff]/.test(stream.DisplayTitle) ? stream.DisplayTitle : '')
    || [language, stream.Type === 'Audio' ? [codec, channels].filter(Boolean).join(' ') : stream.IsForced ? '强制字幕' : null].filter(Boolean).join(' · ');
}
