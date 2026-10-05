import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { TOKEN } from './fixture-data.mjs';

const reference = JSON.parse(readFileSync(new URL('./data/manual-demo-timeline.json', import.meta.url), 'utf8'));
const catalogue = JSON.parse(readFileSync(new URL('./data/catalogue.json', import.meta.url), 'utf8'));
const featuredId = 'deep-1-1';
const version = 'manual-demo-v1';
const ticksPerSecond = 10_000_000;
const longMedia = process.env.GOBY_DEMO_LONG_MEDIA_FILE;

export function configureManualDemo(state) {
  state.user.Name = '\u9648\u9ed8';
  state.display['goby-player'] = { Id: 'goby-player', Revision: '1', CustomPrefs: { preferences: JSON.stringify({ backgroundMotion: false }) } };
  state.display['goby-player-item-deep'] = { Id: 'goby-player-item-deep', Revision: '1', CustomPrefs: { selection: JSON.stringify({ audioStreamIndex: 1, subtitleStreamIndex: 3, quality: 'original' }) } };
  for (const item of state.items) {
    if (item.SeriesId === 'deep') Object.assign(item.UserData, { Played: false, PlaybackPositionTicks: 0, PlayedPercentage: 0 });
  }
  const item = state.items.find(entry => entry.Id === featuredId);
  item.RunTimeTicks = reference.durationSeconds * ticksPerSecond;
  Object.assign(item.UserData, { PlaybackPositionTicks: reference.resumeSeconds * ticksPerSecond, PlayedPercentage: reference.resumeSeconds / reference.durationSeconds * 100, LastPlayedDate: '2026-10-04T00:00:00Z' });
  const source = item.MediaSources[0];
  Object.assign(source, { Container: 'mkv', RunTimeTicks: item.RunTimeTicks, Bitrate: 21_000_000, DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: 3 });
  source.MediaStreams = [
    { Index: 0, Type: 'Video', Codec: 'hevc', Width: 3840, Height: 1608, BitRate: 21_000_000, BitDepth: 10, RealFrameRate: 23.976, Profile: 'Main 10', Level: 153, PixelFormat: 'yuv420p10le', VideoRange: 'HDR', VideoRangeType: 'HDR10', ColorPrimaries: 'bt2020', ColorTransfer: 'smpte2084' },
    { Index: 1, Type: 'Audio', Codec: 'truehd', Profile: 'Dolby Atmos', Language: 'eng', DisplayTitle: 'English TrueHD Atmos 7.1', Channels: 8, ChannelLayout: '7.1', SampleRate: 48000, BitRate: 4_608_000, IsDefault: true },
    { Index: 2, Type: 'Audio', Codec: 'ac3', Language: 'cmn', DisplayLanguage: '\u56fd\u8bed', DisplayTitle: 'Mandarin AC3 5.1', Channels: 6, ChannelLayout: '5.1', SampleRate: 48000, BitRate: 640_000 },
    { Index: 3, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', Language: 'chi', DisplayTitle: '\u7b80\u4f53\u4e2d\u6587', IsDefault: true, IsExternal: false },
    { Index: 4, Type: 'Subtitle', Codec: 'hdmv_pgs_subtitle', Language: 'chi', DisplayTitle: '\u7e41\u9ad4\u4e2d\u6587', IsExternal: false },
    { Index: 5, Type: 'Subtitle', Codec: 'srt', Language: 'eng', DisplayTitle: '\u82f1\u6587', IsExternal: true, IsTextSubtitleStream: true, DeliveryUrl: `/emby/Videos/${item.Id}/${source.Id}/Subtitles/5/Stream.vtt` },
  ];
  item.MediaStreams = source.MediaStreams;
}

export function filterManualDemoItems(items, searchParams, allItems) {
  const value = name => searchParams.get(name) ?? searchParams.get(name.toLowerCase());
  const is4K = value('Is4K') === 'true';
  const ranges = value('ExtendedVideoTypes')?.toLowerCase().split(',') ?? [];
  if (!is4K && !ranges.length) return items;
  return items.filter(item => {
    const candidates = item.Type === 'Series' ? allItems.filter(child => child.SeriesId === item.Id && child.Type === 'Episode') : [item];
    return candidates.some(candidate => candidate.MediaStreams?.some(stream => {
      if (stream.Type !== 'Video' || is4K && stream.Width < 3800) return false;
      const raw = (stream.VideoRangeType ?? stream.VideoRange ?? '').toLowerCase();
      const range = /dovi|dolby/.test(raw) ? 'dolbyvision' : /hlg|hyperloggamma/.test(raw) ? 'hyperloggamma' : /hdr/.test(raw) ? 'hdr10' : 'sdr';
      return !ranges.length || ranges.includes(range);
    }));
  });
}

function intervalsFor(item, streamIndex) {
  const normalized = reference.subtitles[item.Id === featuredId ? streamIndex - 3 : streamIndex === 3 ? 0 : 2] ?? reference.subtitles[2];
  return normalized.map(([start, end]) => ({ StartTicks: Math.round(start * item.RunTimeTicks), EndTicks: Math.round(end * item.RunTimeTicks) }));
}

function framesFor(item) {
  if (item.Id === featuredId) return reference.frames;
  const raw = catalogue.raw.find(entry => entry[0] === (item.SeriesId ?? item.Id));
  const pool = catalogue.pools[raw?.[8]] ?? reference.frames.map(frame => frame.id);
  return Array.from({ length: 12 }, (_, index) => ({ id: pool[index % pool.length], width: 240, height: 135 }));
}

function waveformBytes(item, streamIndex) {
  const count = 4096;
  const validityBytes = Math.ceil(count / 8);
  const bytes = Buffer.alloc(32 + count * 4 + validityBytes);
  bytes.write('GAWL'); bytes.writeUInt16LE(1, 4); bytes.writeUInt16LE(32, 6);
  bytes.writeUInt32LE(streamIndex, 8); bytes.writeUInt32LE(count, 12);
  bytes.writeBigUInt64LE(BigInt(item.RunTimeTicks), 16); bytes.writeUInt32LE(validityBytes, 24);
  for (let index = 0; index < count; index++) {
    // The featured amplitudes replay the handoff; other tracks are deterministic demo data.
    const amplitude = item.Id === featuredId ? reference.waveforms[streamIndex - 1][Math.min(179, Math.floor(index * 180 / count))]
      : .14 + Math.abs(Math.sin(index / count * 41 + streamIndex) * Math.cos(index / count * 17)) * .8;
    const peak = Math.min(65535, Math.max(0, Math.round(amplitude * 65535)));
    bytes.writeUInt16LE(peak, 32 + index * 4); bytes.writeUInt16LE(Math.round(peak * .65), 34 + index * 4);
  }
  bytes.fill(255, 32 + count * 4);
  return bytes;
}

function stamp(value) {
  const milliseconds = Math.round(value / 10_000);
  return `${String(Math.floor(milliseconds / 3_600_000)).padStart(2, '0')}:${String(Math.floor(milliseconds / 60_000) % 60).padStart(2, '0')}:${String(Math.floor(milliseconds / 1000) % 60).padStart(2, '0')}.${String(milliseconds % 1000).padStart(3, '0')}`;
}

function playbackSource(item) {
  const source = structuredClone(item.MediaSources[0]);
  source.Id = `${source.Id}-demo-playback`;
  Object.assign(source, { Container: 'webm', RunTimeTicks: (item.Id === featuredId && longMedia ? reference.durationSeconds : 30) * ticksPerSecond,
    Bitrate: 220_000, SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: false,
    DirectStreamUrl: `/emby/Videos/${item.Id}/stream.webm?MediaSourceId=${source.Id}&api_key=${TOKEN}` });
  source.MediaStreams = source.MediaStreams.map(stream => {
    if (stream.Type === 'Video') return { Index: stream.Index, Type: 'Video', Codec: 'vp8', Width: 640, Height: 360, BitDepth: 8, RealFrameRate: 10, VideoRange: 'SDR', VideoRangeType: 'SDR' };
    if (stream.Type === 'Audio') return { ...stream, Codec: 'opus', Profile: undefined, Channels: 2, ChannelLayout: 'stereo', BitRate: 32_000, SampleRate: 48000 };
    return { ...stream, Codec: 'webvtt', IsExternal: true, IsTextSubtitleStream: true, SupportsExternalStream: true, DeliveryMethod: 'External', DeliveryUrl: `/emby/Videos/${item.Id}/${source.Id}/Subtitles/${stream.Index}/Stream.vtt?demoPlayback=1` };
  });
  return source;
}

export async function handleManualDemo({ request, response, route, url, state, body, directory, json, sendFile, sendPicsum }) {
  if (['/Sessions/Playing', '/Sessions/Playing/Progress', '/Sessions/Playing/Stopped'].includes(route)) {
    state.events.push({ route, ...body, receivedAt: new Date().toISOString() });
    const playedItem = state.items.find(entry => entry.Id === body.ItemId);
    if (playedItem && Number.isFinite(body.PositionTicks)) {
      const duration = playbackSource(playedItem).RunTimeTicks;
      Object.assign(playedItem.UserData, { PlaybackPositionTicks: body.PositionTicks, LastPlayedDate: new Date().toISOString(), PlayedPercentage: Math.min(100, body.PositionTicks / duration * 100) });
    }
    response.writeHead(204); response.end(); return true;
  }
  const match = route.match(/^\/(?:Items|Videos)\/([^/]+)(?:\/|$)/);
  const item = match && state.items.find(entry => entry.Id === match[1]);
  if (!item) return false;
  const source = item.MediaSources?.[0];
  if (/\/BackgroundPreview$/.test(route)) {
    json(response, { Available: true, StreamUrl: `/emby/Items/${item.Id}/BackgroundPreview/stream.mp4`, RunTimeTicks: 30_000_000, Width: 320, Height: 180 });
    return true;
  }
  if (/\/BackgroundPreview\/stream\.mp4$/.test(route)) {
    sendFile(request, response, resolve(directory, 'assets/background.mp4'), 'video/mp4'); return true;
  }
  if (!source) return false;
  if (/\/AudioWaveforms$/.test(route)) {
    json(response, { Available: true, MediaSourceId: source.Id, SourceVersion: version, DurationTicks: item.RunTimeTicks,
      Streams: source.MediaStreams.filter(stream => stream.Type === 'Audio').map(stream => ({ StreamIndex: stream.Index, Channels: stream.Channels, SampleRate: stream.SampleRate ?? 48000,
        Levels: [{ BucketCount: 4096, Url: `/emby/Items/${item.Id}/AudioWaveforms/${stream.Index}/4096?tag=${version}` }] })) });
    return true;
  }
  const waveform = route.match(/\/AudioWaveforms\/(\d+)\/4096$/);
  if (waveform) {
    const index = Number(waveform[1]);
    if (!source.MediaStreams.some(stream => stream.Index === index && stream.Type === 'Audio')) { json(response, { error: 'Unknown demo track.' }, 404); return true; }
    const bytes = waveformBytes(item, index);
    response.writeHead(200, { 'Content-Type': 'application/octet-stream', 'Content-Length': String(bytes.length), 'Cache-Control': 'no-store' });
    response.end(bytes); return true;
  }
  const bitmapStreams = source.MediaStreams.filter(stream => stream.Type === 'Subtitle' && stream.Codec === 'hdmv_pgs_subtitle');
  if (/\/SubtitleTimelines$/.test(route)) {
    json(response, bitmapStreams.length ? { Available: true, MediaSourceId: source.Id, SourceVersion: version, DurationTicks: item.RunTimeTicks,
      Streams: bitmapStreams.map(stream => ({ StreamIndex: stream.Index, Codec: stream.Codec, IntervalCount: intervalsFor(item, stream.Index).length,
        Url: `/emby/Items/${item.Id}/SubtitleTimelines/${stream.Index}?tag=${version}` })) } : { Available: false });
    return true;
  }
  const timeline = route.match(/\/SubtitleTimelines\/(\d+)$/);
  if (timeline) {
    const index = Number(timeline[1]);
    if (!bitmapStreams.some(stream => stream.Index === index)) { json(response, { error: 'Unknown demo track.' }, 404); return true; }
    json(response, { MediaSourceId: source.Id, SourceVersion: version, StreamIndex: index, DurationTicks: item.RunTimeTicks, Intervals: intervalsFor(item, index) });
    return true;
  }
  if (/\/ThumbnailSet$/.test(route)) {
    const duration = url.searchParams.get('MediaSourceId')?.endsWith('-demo-playback') ? playbackSource(item).RunTimeTicks : item.RunTimeTicks;
    json(response, { AspectRatio: 16 / 9, Thumbnails: framesFor(item).map((_, index) => ({ ImageTag: `demo-frame-${index}`, PositionTicks: Math.round(index * duration / 11) })) });
    return true;
  }
  if (/\/Images\/Thumbnail$/.test(route)) {
    const frameIndex = Number(url.searchParams.get('tag')?.replace('demo-frame-', ''));
    const frame = framesFor(item)[frameIndex];
    if (!frame) { json(response, { error: 'Unknown demo frame.' }, 404); return true; }
    await sendPicsum(request, response, frame.id, frame.width, frame.height); return true;
  }
  if (/\/PlaybackInfo$/.test(route)) {
    if (body.EnableDirectPlay === false && body.EnableDirectStream === false) json(response, { ErrorCode: 'NoCompatibleStream', MediaSources: [] });
    else json(response, { PlaySessionId: `demo-play-${item.Id}-${state.events.length}`, MediaSources: [playbackSource(item)] });
    return true;
  }
  if (item.Id === featuredId && longMedia && /\/stream(?:\.\w+)?$/.test(route)) {
    sendFile(request, response, resolve(longMedia), 'video/webm'); return true;
  }
  const subtitle = route.match(/\/Subtitles\/(\d+)\/Stream\.vtt$/);
  if (subtitle) {
    const index = Number(subtitle[1]);
    const isEnglish = source.MediaStreams.find(stream => stream.Index === index)?.Language === 'eng';
    const cues = isEnglish ? ['Can you hear it?', 'That frequency again.', 'It is answering us.'] : ['\u4f60\u542c\u5230\u4e86\u5417\uff1f', '\u53c8\u662f\u90a3\u4e2a\u9891\u7387\u3002', '\u5b83\u5728\u56de\u5e94\u6211\u4eec\u3002'];
    const intervals = url.searchParams.has('demoPlayback') && !(item.Id === featuredId && longMedia)
      ? [[1, 10], [10, 20], [20, 29]].map(([start, end]) => ({ StartTicks: start * ticksPerSecond, EndTicks: end * ticksPerSecond })) : intervalsFor(item, index);
    response.writeHead(200, { 'Content-Type': 'text/vtt; charset=utf-8', 'Cache-Control': 'no-store' });
    response.end(`WEBVTT\n\n${intervals.map((interval, cue) => `${stamp(interval.StartTicks)} --> ${stamp(interval.EndTicks)}\n${cues[cue % cues.length]}`).join('\n\n')}\n`);
    return true;
  }
  return false;
}
