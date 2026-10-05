export type Platform = 'win' | 'mac' | 'linux' | 'ios' | 'android';
export interface ExternalPlayer {
  id: string;
  name: string;
  description: string;
  color: string;
  platforms: Platform[];
  launchSupported: boolean;
}

interface PlayerDefinition extends Omit<ExternalPlayer, 'launchSupported'> {
  launchPlatforms: Platform[];
}

export function getPlatform(): Platform {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent;
  if (/iPhone|iPad|iPod/.test(ua) || /Macintosh/.test(ua) && navigator.maxTouchPoints > 1) return 'ios';
  if (/Android/.test(ua)) return 'android';
  if (/Windows/.test(ua)) return 'win';
  if (/Macintosh|Mac OS X/.test(ua)) return 'mac';
  return 'linux';
}
export const platformName = (): string => ({ win: 'Windows', mac: 'macOS', linux: 'Linux', ios: 'iOS', android: 'Android' })[getPlatform()];

const DIRECT_DESCRIPTION = '\u5916\u90e8\u5e94\u7528 \u00b7 \u76f4\u8fde\u539f\u59cb\u6587\u4ef6';
const MANUAL_DESCRIPTION = '\u590d\u5236\u4e32\u6d41\u94fe\u63a5\u5230\u64ad\u653e\u5668';

const PLAYERS: PlayerDefinition[] = [
  { id: 'goby', name: '\u5185\u7f6e\u64ad\u653e\u5668', description: '\u7f51\u9875\u5185\u64ad\u653e', color: 'var(--accent)', platforms: ['win', 'mac', 'linux', 'ios', 'android'], launchPlatforms: ['win', 'mac', 'linux', 'ios', 'android'] },
  { id: 'potplayer', name: 'PotPlayer', description: DIRECT_DESCRIPTION, color: '#d9a400', platforms: ['win'], launchPlatforms: ['win'] },
  { id: 'vlc', name: 'VLC', description: DIRECT_DESCRIPTION, color: '#e8730c', platforms: ['win', 'mac', 'linux', 'ios', 'android'], launchPlatforms: ['ios', 'android'] },
  { id: 'iina', name: 'IINA', description: DIRECT_DESCRIPTION, color: '#5552d6', platforms: ['mac'], launchPlatforms: ['mac'] },
  { id: 'nplayer', name: 'nPlayer', description: DIRECT_DESCRIPTION, color: '#1f9d5c', platforms: ['ios', 'android'], launchPlatforms: [] },
  { id: 'mx', name: 'MX Player', description: DIRECT_DESCRIPTION, color: '#2f6fe4', platforms: ['android'], launchPlatforms: ['android'] },
  { id: 'infuse', name: 'Infuse', description: DIRECT_DESCRIPTION, color: '#e0560f', platforms: ['ios', 'mac'], launchPlatforms: ['ios', 'mac'] },
  { id: 'stellar', name: '\u6052\u661f\u64ad\u653e\u5668', description: DIRECT_DESCRIPTION, color: '#4338ca', platforms: ['ios', 'mac'], launchPlatforms: [] },
  { id: 'mpv', name: 'mpv', description: '\u9700 mpv 0.41+ \u53ca\u7cfb\u7edf\u534f\u8bae\u5173\u8054', color: '#6b3fa0', platforms: ['win', 'mac', 'linux'], launchPlatforms: ['win', 'mac', 'linux'] },
  { id: 'dandanplay', name: '\u5f39\u5f39play', description: DIRECT_DESCRIPTION, color: '#0a8aa8', platforms: ['win', 'mac', 'android'], launchPlatforms: ['win', 'android'] },
];

export function availablePlayers(platform = getPlatform()): ExternalPlayer[] {
  return PLAYERS.filter(player => player.platforms.includes(platform)).map(({ launchPlatforms, ...player }) => ({
    ...player,
    launchSupported: launchPlatforms.includes(platform),
    description: launchPlatforms.includes(platform) ? player.description : MANUAL_DESCRIPTION,
  }));
}

export function effectivePlayer(remembered: string | undefined, defaultPlayer: string | undefined, platform = getPlatform()): ExternalPlayer {
  const players = availablePlayers(platform);
  return players.find(player => player.id === remembered)
    ?? players.find(player => player.id === defaultPlayer)
    ?? players.find(player => player.id === 'goby')!;
}

function androidIntent(url: URL, packageName: string): string {
  return `intent://${url.href.replace(/^https?:\/\//, '')}#Intent;scheme=${url.protocol.slice(0, -1)};package=${packageName};type=video/*;end`;
}

export function externalPlayerUrl(playerId: string, streamUrl: string, _title = '', platform = getPlatform()): string {
  if (!availablePlayers(platform).some(player => player.id === playerId && player.launchSupported)) return '';
  let url: URL;
  try { url = new URL(streamUrl); } catch { return ''; }
  if (!['http:', 'https:'].includes(url.protocol)) return '';
  const encoded = encodeURIComponent(url.href);
  switch (playerId) {
    // The opaque form preserves the nested HTTP scheme during browser parsing.
    case 'potplayer': return `potplayer:${url.href}`;
    case 'iina': return `iina://weblink?url=${encoded}`;
    case 'infuse': return `infuse://x-callback-url/play?url=${encoded}`;
    case 'vlc': return platform === 'android' ? androidIntent(url, 'org.videolan.vlc') : `vlc-x-callback://x-callback-url/stream?url=${encoded}`;
    case 'mx': return androidIntent(url, 'com.mxtech.videoplayer.ad');
    // The macOS application unescapes once before the mpv protocol demuxer does.
    case 'mpv': return `mpv://${platform === 'mac' ? encodeURIComponent(encoded) : encoded}`;
    case 'dandanplay': return platform === 'android' ? androidIntent(url, 'com.xyoye.dandanplay') : `ddplay:${encoded}`;
    default: return '';
  }
}
