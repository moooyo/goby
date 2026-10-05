import { readFileSync } from 'node:fs';

const { raw, pools } = JSON.parse(readFileSync(new URL('./data/catalogue.json', import.meta.url), 'utf8'));
const minute = 600_000_000;
const now = Date.parse('2026-10-04T00:00:00.000Z');
export const USER_ID = 'fixture-viewer';
export const TOKEN = 'isolated-player-acceptance-token';
export const LIBRARIES = [
  ['cn', '华语电影', 'movies', 'guitu lantern lighthouse typist fireworks oldtown frost'],
  ['intl', '海外电影', 'movies', 'pendulum pier island mist drift fjord'],
  ['uhd', '4K 原盘', 'movies', 'dunes canyon startrail velocity snowline highway'],
  ['cntv', '国产剧', 'tvshows', 'rain bamboo plateau lasttrain'],
  ['intltv', '海外剧', 'tvshows', 'deep neon aurora'],
].map(([Id, Name, CollectionType, children]) => ({ Id, Name, Type: 'CollectionFolder', IsFolder: true, CollectionType, children: children.split(' ') }));

function sourceFor(item, resolution, hdr, audio) {
  return {
    Id: `${item.Id}-source`, ItemId: item.Id, Name: `${resolution} ${hdr}`.trim(), Container: 'webm',
    RunTimeTicks: item.RunTimeTicks, Bitrate: resolution === '4K' ? 21_000_000 : 9_000_000,
    Size: 3_800_000_000, SupportsDirectPlay: true, SupportsDirectStream: true, SupportsTranscoding: false,
    DefaultAudioStreamIndex: 1, DefaultSubtitleStreamIndex: 3,
    MediaStreams: [
      { Index: 0, Type: 'Video', Codec: 'vp8', Width: resolution === '4K' ? 3840 : 1920, Height: resolution === '4K' ? 2160 : 1080, BitRate: 20_000_000, BitDepth: hdr ? 10 : 8, RealFrameRate: 24, VideoRange: hdr ? 'HDR' : 'SDR', VideoRangeType: hdr === 'Dolby Vision' ? 'DOVI' : hdr || 'SDR' },
      { Index: 1, Type: 'Audio', Codec: 'opus', Language: 'eng', DisplayTitle: `英语 · ${audio}`, Channels: /全景声/.test(audio) ? 8 : /立体声/.test(audio) ? 2 : 6, IsDefault: true, BitRate: 640_000, SampleRate: 48000 },
      { Index: 2, Type: 'Audio', Codec: 'ac3', Language: 'chi', DisplayTitle: '国语 · AC3 5.1', Channels: 6, IsDefault: false, BitRate: 640_000 },
      { Index: 3, Type: 'Subtitle', Codec: 'webvtt', Language: 'chi', DisplayTitle: '简体中文', IsDefault: true, IsExternal: true, IsTextSubtitleStream: true, SupportsExternalStream: true, DeliveryMethod: 'External', DeliveryUrl: `/emby/Videos/${item.Id}/${item.Id}-source/Subtitles/3/Stream.vtt` },
      { Index: 4, Type: 'Subtitle', Codec: 'webvtt', Language: 'eng', DisplayTitle: '英文', IsExternal: true, IsTextSubtitleStream: true, SupportsExternalStream: true, DeliveryMethod: 'External', DeliveryUrl: `/emby/Videos/${item.Id}/${item.Id}-source/Subtitles/4/Stream.vtt` },
    ],
  };
}

export function createCatalogue() {
  const items = [];
  const imageIds = {};
  for (const entry of raw) {
    const [Id, kind, Name, OriginalTitle, ProductionYear, genres, CommunityRating, image, pool, resolution, hdr, audio, director, actors, days, Overview, duration] = entry;
    const library = LIBRARIES.find((entry) => entry.children.includes(Id));
    const People = [{ Id: `person-${director}`, Name: director, Type: 'Director' }, ...actors.split(',').map((actor) => {
      const [Name, Role] = actor.split(':');
      return { Id: `person-${Name}`, Name, Role, Type: 'Actor' };
    })];
    const item = {
      Id, Name, OriginalTitle, ProductionYear, CommunityRating, Overview, People, Genres: genres.split('/'),
      Type: kind === 's' ? 'Series' : 'Movie', IsFolder: kind === 's', CanPlay: true,
      ParentId: library?.Id, DateCreated: new Date(now - days * 86_400_000).toISOString(),
      OfficialRating: /悬疑|动作|犯罪/.test(genres) ? '16+' : '12+',
      RunTimeTicks: (typeof duration === 'number' ? duration : 48) * minute,
      ImageTags: { Primary: `primary-${Id}`, Thumb: `thumb-${Id}` },
      BackdropImageTags: [`backdrop-${Id}`], PrimaryImageAspectRatio: 2 / 3,
      UserData: { ItemId: Id, PlaybackPositionTicks: 0, PlayedPercentage: 0, Played: false, IsFavorite: ['deep', 'lighthouse', 'startrail', 'plateau', 'typist', 'bamboo'].includes(Id), PlayCount: 0 },
    };
    imageIds[Id] = { primary: image, backdrop: image };
    if (kind === 'm') {
      if (Id === 'guitu') Object.assign(item.UserData, { PlaybackPositionTicks: 80_000_000, PlayedPercentage: 38, LastPlayedDate: '2026-10-03T04:00:00Z' });
      if (Id === 'drift') Object.assign(item.UserData, { PlaybackPositionTicks: 40_000_000, PlayedPercentage: 15, LastPlayedDate: '2026-10-01T04:00:00Z' });
      if (['lighthouse', 'typist'].includes(Id)) Object.assign(item.UserData, { Played: true, PlayedPercentage: 100, PlayCount: 1 });
      item.MediaSources = [sourceFor(item, resolution, hdr, audio)];
      item.MediaStreams = item.MediaSources[0].MediaStreams;
      item.Container = 'webm';
    } else {
      item.ChildCount = duration.length;
      item.RecursiveItemCount = duration.reduce((count, entries) => count + entries.split('|').length, 0);
      const stills = pools[pool].filter((value) => value !== image);
      duration.forEach((season, seasonIndex) => {
        const seasonNumber = seasonIndex + 1;
        const SeasonId = `${Id}-season-${seasonNumber}`;
        const episodes = season.split('|');
        items.push({ Id: SeasonId, Name: `第 ${seasonNumber} 季`, Type: 'Season', IsFolder: true, SeriesId: Id, SeriesName: Name, ParentId: Id, IndexNumber: seasonNumber, ChildCount: episodes.length, ImageTags: item.ImageTags, UserData: { Played: false } });
        imageIds[SeasonId] = imageIds[Id];
        episodes.forEach((entry, episodeIndex) => {
          const [episodeTitle, episodeOverview] = entry.split('@');
          const episodeNumber = episodeIndex + 1;
          const episodeId = `${Id}-${seasonNumber}-${episodeNumber}`;
          const episode = {
            ...item, Id: episodeId, Name: episodeTitle, Overview: episodeOverview, Type: 'Episode', IsFolder: false,
            SeriesId: Id, SeriesName: Name, SeasonId, SeasonName: `第 ${seasonNumber} 季`, ParentId: SeasonId,
            IndexNumber: episodeNumber, ParentIndexNumber: seasonNumber,
            RunTimeTicks: (42 + ((episodeIndex * 11 + seasonIndex * 7) % 17)) * minute,
            ParentBackdropItemId: Id, ParentBackdropImageTags: item.BackdropImageTags,
            SeriesPrimaryImageTag: `primary-${Id}`, ImageTags: { Primary: `primary-${episodeId}` }, BackdropImageTags: [`backdrop-${episodeId}`],
            UserData: { ItemId: episodeId, PlaybackPositionTicks: 0, PlayedPercentage: 0, Played: Id === 'deep' && (seasonNumber === 1 || seasonNumber === 2 && episodeNumber < 5), IsFavorite: false, PlayCount: 0 },
          };
          if (Id === 'deep' && seasonNumber === 2 && episodeNumber === 5) Object.assign(episode.UserData, { PlaybackPositionTicks: 120_000_000, PlayedPercentage: 52, LastPlayedDate: '2026-10-03T23:00:00Z' });
          if (Id === 'rain' && episodeNumber === 3) Object.assign(episode.UserData, { PlaybackPositionTicks: 60_000_000, PlayedPercentage: 30, LastPlayedDate: '2026-10-02T19:00:00Z' });
          episode.MediaSources = [sourceFor(episode, resolution, hdr, audio)];
          episode.MediaStreams = episode.MediaSources[0].MediaStreams;
          episode.Container = 'webm';
          items.push(episode);
          imageIds[episodeId] = { primary: stills[(seasonNumber * 5 + episodeNumber) % stills.length], backdrop: stills[(seasonNumber * 5 + episodeNumber) % stills.length] };
        });
      });
    }
    items.push(item);
  }
  return { items, imageIds };
}

export const INITIAL_CONFIGURATION = { AudioLanguagePreference: 'eng', SubtitleLanguagePreference: 'chi', SubtitleMode: 'Default', EnableNextEpisodeAutoPlay: true, IntroSkipMode: 'ShowButton', RememberAudioSelections: true, RememberSubtitleSelections: true };
export function createUser() {
  return { Id: USER_ID, Name: 'Moooyo', ServerId: 'fixture-server', HasPassword: true, HasConfiguredPassword: true, Configuration: { ...INITIAL_CONFIGURATION }, Policy: { IsAdministrator: true, IsDisabled: false, EnableMediaPlayback: true, EnableVideoPlaybackTranscoding: false, EnableAudioPlaybackTranscoding: false, EnablePlaybackRemuxing: true, EnableUserPreferenceAccess: true } };
}
