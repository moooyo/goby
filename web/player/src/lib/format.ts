import type { MediaItem, MediaStream } from '../types';

export function ticksToSeconds(ticks: number | undefined): number { return Math.max(0, (ticks || 0) / 10_000_000); }
export function secondsToTicks(seconds: number): number { return Math.max(0, Math.round((Number.isFinite(seconds) ? seconds : 0) * 10_000_000)); }
export function formatTime(seconds: number): string {
  const total = Math.max(0, Math.floor(Number.isFinite(seconds) ? seconds : 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor(total / 60) % 60;
  return `${hours ? `${hours}:` : ''}${hours ? String(minutes).padStart(2, '0') : minutes}:${String(total % 60).padStart(2, '0')}`;
}
export function formatDuration(ticks: number | undefined): string {
  if (!ticks) return '';
  const minutes = Math.max(1, Math.round(ticksToSeconds(ticks) / 60));
  const hours = Math.floor(minutes / 60);
  return hours ? `${hours} \u5c0f\u65f6${minutes % 60 ? ` ${String(minutes % 60).padStart(2, '0')} \u5206` : ''}` : `${minutes} \u5206\u949f`;
}
export function progressPercent(item: MediaItem): number {
  if (item.UserData?.Played) return 100;
  const total = item.RunTimeTicks || item.MediaSources?.[0]?.RunTimeTicks || 0;
  return Math.max(0, Math.min(100, total ? (item.UserData?.PlaybackPositionTicks || 0) / total * 100 : item.UserData?.PlayedPercentage || 0));
}
export function mediaBadges(item: MediaItem): string[] {
  const streams = item.MediaSources?.[0]?.MediaStreams ?? item.MediaStreams ?? [];
  const video = streams.find(stream => stream.Type === 'Video');
  const audio = streams.find(stream => stream.Type === 'Audio' && stream.IsDefault) ?? streams.find(stream => stream.Type === 'Audio');
  const result: string[] = [];
  if (video?.Width && video.Width >= 3800) result.push('4K');
  else if (video?.Height && video.Height >= 1000) result.push('1080p');
  else if (video?.Height && video.Height >= 700) result.push('720p');
  const range = video?.VideoRangeType || video?.VideoRange;
  if (range && range !== 'SDR') result.push(range.replace('DOVI', 'Dolby Vision'));
  if (audio?.Profile?.toLowerCase().includes('atmos')) result.push('Dolby Atmos');
  return result;
}
export function languageLabel(language?: string): string {
  return ({ chi: '\u4e2d\u6587', zho: '\u4e2d\u6587', zh: '\u4e2d\u6587', cmn: '\u56fd\u8bed', 'zh-cn': '\u7b80\u4f53\u4e2d\u6587', 'zh-tw': '\u7e41\u4f53\u4e2d\u6587', eng: '\u82f1\u8bed', en: '\u82f1\u8bed', jpn: '\u65e5\u8bed', ja: '\u65e5\u8bed', kor: '\u97e9\u8bed', ko: '\u97e9\u8bed', fre: '\u6cd5\u8bed', fra: '\u6cd5\u8bed', ger: '\u5fb7\u8bed', deu: '\u5fb7\u8bed', spa: '\u897f\u73ed\u7259\u8bed', rus: '\u4fc4\u8bed', und: '\u672a\u6807\u6ce8\u8bed\u8a00' } as Record<string, string>)[(language || '').toLowerCase()] || language || '\u672a\u6807\u6ce8\u8bed\u8a00';
}
export function audioLabel(stream: MediaStream): string {
  const channels = stream.Channels ? ({ 1: '1.0', 2: '2.0', 6: '5.1', 8: '7.1' } as Record<number, string>)[stream.Channels] || String(stream.Channels) : '';
  return `${languageLabel(stream.Language)}${stream.Codec ? ` \u00b7 ${stream.Codec.toUpperCase()}` : ''}${channels ? ` ${channels}` : ''}`;
}

const languageCode = (value: string) => /^[a-z]{2,3}(?:[-_][a-z0-9]{2,8})*$/i.test(value);
const normalizeLanguage = (value: string) => value.trim().replace(/_/g, '-').toLowerCase();

function subtitleLanguageName(value: string): string | undefined {
  const code = normalizeLanguage(value);
  if (!code || code === 'und') return undefined;
  const known = languageLabel(code);
  if (known !== code) return known;
  if (!languageCode(code)) return undefined;
  try { return new Intl.DisplayNames(['zh-CN'], { type: 'language', fallback: 'none' }).of(code); }
  catch { return undefined; }
}

export function subtitleLabel(stream: MediaStream): string {
  const title = stream.Title?.trim() || '';
  const displayTitle = stream.DisplayTitle?.trim() || '';
  const candidate = title || displayTitle;
  const rawLanguage = stream.Language?.trim() || '';
  const displayLanguage = stream.DisplayLanguage?.trim() || '';
  const matchesLanguage = languageCode(candidate) && [rawLanguage, displayLanguage].some(value => normalizeLanguage(value) === normalizeLanguage(candidate));
  const codecKey = (value: string) => value.replace(/[\s_.\-()[\]]/g, '').toLowerCase();
  const pureCodec = !title && displayTitle && (codecKey(displayTitle) === codecKey(stream.Codec || '') || /^(hdmvpgssubtitle|pgssub|pgs|dvdsubtitle|dvdsub|dvd)$/.test(codecKey(displayTitle)));
  // Only normalize generated metadata; a custom title remains the track's name.
  const label = (matchesLanguage ? subtitleLanguageName(candidate) || candidate : pureCodec ? '' : candidate)
    || subtitleLanguageName(rawLanguage) || subtitleLanguageName(displayLanguage) || displayLanguage
    || (rawLanguage && rawLanguage !== 'und' ? rawLanguage : '未知语言');
  return [label, stream.IsForced ? '强制' : '', stream.IsHearingImpaired ? '听障' : ''].filter(Boolean).join(' · ');
}
