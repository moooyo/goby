import { expect, test } from '@playwright/test';
import { availablePlayers, effectivePlayer, externalPlayerUrl, type Platform } from '../src/lib/players';

const source = 'https://media.example:8443/emby/Videos/item%23part/stream?Static=true&MediaSourceId=source%26part&api_key=alpha%2523beta%26tail&title=\u4e2d\u6587%20%26%20cut';
const canonicalSource = 'https://media.example:8443/emby/Videos/item%23part/stream?Static=true&MediaSourceId=source%26part&api_key=alpha%2523beta%26tail&title=%E4%B8%AD%E6%96%87%20%26%20cut';

function launch(player: string, platform: Platform, mediaUrl = source): string {
  return externalPlayerUrl(player, mediaUrl, 'Display title |filePath=untrusted', platform);
}

// Android Intent.parseUri separates its metadata at the final fragment marker.
function readAndroidIntent(value: string) {
  const serialized = new URL(value).href;
  const marker = serialized.lastIndexOf('#Intent;');
  expect(marker).toBeGreaterThan(0);
  const fields = serialized.slice(marker + '#Intent;'.length).split(';');
  expect(fields.pop()).toBe('end');
  const options = Object.fromEntries(fields.map(field => {
    const separator = field.indexOf('=');
    return [field.slice(0, separator), field.slice(separator + 1)];
  }));
  expect(serialized.startsWith('intent://')).toBe(true);
  return {
    mediaUrl: `${options.scheme}://${serialized.slice('intent://'.length, marker)}`,
    options,
  };
}

test('retains every handoff player only on its supported platforms', () => {
  const expected: Record<Platform, string[]> = {
    win: ['goby', 'potplayer', 'vlc', 'mpv', 'dandanplay'],
    mac: ['goby', 'vlc', 'iina', 'infuse', 'stellar', 'mpv', 'dandanplay'],
    linux: ['goby', 'vlc', 'mpv'],
    ios: ['goby', 'vlc', 'nplayer', 'infuse', 'stellar'],
    android: ['goby', 'vlc', 'nplayer', 'mx', 'dandanplay'],
  };
  for (const platform of Object.keys(expected) as Platform[]) {
    expect(availablePlayers(platform).map(player => player.id), platform).toEqual(expected[platform]);
  }
});

test('player preferences fall back by platform while retaining valid manual choices', () => {
  expect(effectivePlayer('potplayer', 'mpv', 'win').id).toBe('potplayer');
  expect(effectivePlayer('goby', 'mpv', 'win').id).toBe('goby');
  expect(effectivePlayer('iina', 'mpv', 'win').id).toBe('mpv');
  expect(effectivePlayer('potplayer', 'iina', 'mac').id).toBe('iina');
  expect(effectivePlayer('iina', 'potplayer', 'android').id).toBe('goby');
  expect(effectivePlayer('unknown', 'infuse', 'ios').id).toBe('infuse');
  expect(effectivePlayer(undefined, 'vlc', 'linux').id).toBe('vlc');
  expect(effectivePlayer('unknown', 'removed-player', 'win').id).toBe('goby');
  expect(effectivePlayer(undefined, undefined, 'ios').id).toBe('goby');

  const manual = effectivePlayer('nplayer', 'vlc', 'android');
  expect(manual.id).toBe('nplayer');
  expect(manual.launchSupported).toBe(false);
  const desktopVlc = effectivePlayer('vlc', 'iina', 'mac');
  expect(desktopVlc.id).toBe('vlc');
  expect(desktopVlc.launchSupported).toBe(false);
});

test('PotPlayer preserves the complete HTTP URL after browser serialization', () => {
  const value = launch('potplayer', 'win');
  const serialized = new URL(value).href;
  expect(serialized.startsWith('potplayer:')).toBe(true);
  expect(serialized.startsWith('potplayer://')).toBe(false);
  expect(serialized.slice('potplayer:'.length)).toBe(canonicalSource);
  expect(launch('potplayer', 'win', source.replace('https:', 'http:')).slice('potplayer:'.length))
    .toBe(canonicalSource.replace('https:', 'http:'));
});

test('Windows and Linux mpv decode the original signed media URL exactly once', () => {
  for (const platform of ['win', 'linux'] as const) {
    const serialized = new URL(launch('mpv', platform)).href;
    expect(serialized.startsWith('mpv://')).toBe(true);
    expect(decodeURIComponent(serialized.slice('mpv://'.length))).toBe(canonicalSource);
  }
});

test('macOS mpv preserves escapes through the application and protocol decoders', () => {
  const serialized = new URL(launch('mpv', 'mac')).href;
  expect(serialized.startsWith('mpv://')).toBe(true);
  const applicationInput = decodeURIComponent(serialized);
  const mediaUrl = decodeURIComponent(applicationInput.slice('mpv://'.length));
  expect(mediaUrl).toBe(canonicalSource);
  expect(new URL(mediaUrl).searchParams.get('api_key')).toBe('alpha%23beta&tail');
});

test('Dandanplay decodes only its media payload and does not infer filename commands', () => {
  const serialized = new URL(launch('dandanplay', 'win')).href;
  expect(serialized.startsWith('ddplay:')).toBe(true);
  expect(decodeURIComponent(serialized.slice('ddplay:'.length))).toBe(canonicalSource);
  expect(serialized).not.toContain('filePath');
});

test('query-based player handlers receive one complete media URL parameter', () => {
  for (const [player, platform, protocol] of [
    ['iina', 'mac', 'iina:'],
    ['infuse', 'ios', 'infuse:'],
    ['infuse', 'mac', 'infuse:'],
    ['vlc', 'ios', 'vlc-x-callback:'],
  ] as const) {
    const parsed = new URL(launch(player, platform));
    expect(parsed.protocol, `${player}/${platform}`).toBe(protocol);
    expect([...parsed.searchParams], `${player}/${platform}`).toEqual([['url', canonicalSource]]);
  }
});

test('Android intents preserve the media URL and select only the requested package', () => {
  for (const [player, packageName] of [
    ['vlc', 'org.videolan.vlc'],
    ['mx', 'com.mxtech.videoplayer.ad'],
    ['dandanplay', 'com.xyoye.dandanplay'],
  ] as const) {
    for (const scheme of ['https', 'http']) {
      const parsed = readAndroidIntent(launch(player, 'android', source.replace('https:', `${scheme}:`)));
      expect(parsed.mediaUrl, `${player}/${scheme}`).toBe(canonicalSource.replace('https:', `${scheme}:`));
      expect(parsed.options).toEqual({ scheme, package: packageName, type: 'video/*' });
    }
  }
});

test('Android intent metadata cannot be supplied by a media query or fragment', () => {
  const mediaUrl = `${source}&extra=%23Intent%3Bpackage%3Dquery.example%3Bend#Intent;package=fragment.example;end`;
  const parsed = readAndroidIntent(launch('dandanplay', 'android', mediaUrl));
  expect(parsed.options).toEqual({ scheme: 'https', package: 'com.xyoye.dandanplay', type: 'video/*' });
  expect(parsed.mediaUrl).toBe(`${canonicalSource}&extra=%23Intent%3Bpackage%3Dquery.example%3Bend#Intent;package=fragment.example;end`);
});

test('client combinations without a verified handler keep the manual link path', () => {
  for (const [player, platform] of [
    ['vlc', 'win'], ['vlc', 'mac'], ['vlc', 'linux'],
    ['nplayer', 'ios'], ['nplayer', 'android'],
    ['stellar', 'ios'], ['stellar', 'mac'], ['dandanplay', 'mac'],
  ] as const) {
    expect(availablePlayers(platform).find(option => option.id === player)?.launchSupported).toBe(false);
    expect(launch(player, platform)).toBe('');
  }
});

test('wrong platforms, unknown players, and invalid transports cannot create launch links', () => {
  for (const [player, platform] of [
    ['potplayer', 'mac'], ['iina', 'win'], ['infuse', 'android'],
    ['mx', 'ios'], ['mpv', 'android'], ['dandanplay', 'ios'],
    ['unknown', 'win'], ['goby', 'win'],
  ] as const) expect(launch(player, platform)).toBe('');

  for (const mediaUrl of [
    '', '/relative/stream', 'not a URL', 'https://',
    'javascript:alert(1)', 'file:///private/video.mkv',
    'data:video/mp4;base64,AAAA', 'ftp://media.example/video.mkv',
  ]) {
    expect(() => launch('potplayer', 'win', mediaUrl)).not.toThrow();
    expect(launch('potplayer', 'win', mediaUrl), mediaUrl).toBe('');
    expect(launch('dandanplay', 'android', mediaUrl), mediaUrl).toBe('');
  }
});
