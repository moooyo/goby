import type { MediaInfoResponse, MediaStreamInfo, MediaTechnicalSummary } from '../src/mediaSummaryApi';

export const videoStream: MediaStreamInfo = {
  Index: 0, Type: 'video', Codec: 'hevc', Language: '', Title: 'Original HDR picture', Width: 3840, Height: 2160,
  Channels: null, SampleRate: null, Bitrate: '47000000', Profile: 'Main 10', Level: 153, BitDepth: 10,
  PixelFormat: 'yuv420p10le', FrameRate: '24000/1001', ChannelLayout: '', VideoRange: 'HDR10',
  ColorSpace: 'bt2020nc', ColorTransfer: 'smpte2084', ColorPrimaries: 'bt2020', IsDefault: true, IsForced: false,
  IsExternal: false, IsAttachedPicture: false, IsHearingImpaired: false,
};
export const audioStream: MediaStreamInfo = {
  ...videoStream, Index: 1, Type: 'audio', Codec: 'eac3', Language: 'en', Title: 'Original surround mix',
  Width: null, Height: null, Channels: 6, SampleRate: 48000, Bitrate: '640000', Profile: '', Level: null,
  BitDepth: null, PixelFormat: '', FrameRate: '', ChannelLayout: '5.1', VideoRange: '', ColorSpace: '', ColorTransfer: '', ColorPrimaries: '',
};
export const subtitleStream: MediaStreamInfo = {
  ...audioStream, Index: 12, Type: 'subtitle', Codec: 'webvtt', Language: 'zh', Title: 'Translated subtitles',
  Channels: null, SampleRate: null, Bitrate: null, ChannelLayout: '', IsDefault: false, IsForced: true,
  IsExternal: true, IsHearingImpaired: true,
};
export const mediaSummaryFixture: MediaTechnicalSummary = {
  State: 'available', DurationTicks: '72000000000', Size: '42949672960', Bitrate: '47721858', Container: 'matroska',
  Video: videoStream, Audio: audioStream, Images: { Count: 3, Types: ['Backdrop', 'Primary'] },
  Analysis: { Intro: null, Credits: 'qualified', PreviewCount: 12, BackgroundPreview: 'ready', AudioWaveform: null, SubtitleTimeline: 'failed' },
};
export const mediaInfoFixture: MediaInfoResponse = {
  Id: 'film-one', LibraryId: 'b'.repeat(32), Summary: mediaSummaryFixture, ProbeVersion: 8, UpdatedAt: '2026-11-04T08:00:00Z',
  Streams: [videoStream, audioStream, subtitleStream],
  Chapters: [{ Index: 0, Name: 'Opening sequence', StartTicks: '0', EndTicks: '1200000000' },
    { Index: 1, Name: 'A new beginning', StartTicks: '1200000000', EndTicks: '72000000000' }],
  StreamCount: 3, ChapterCount: 2, StreamsTruncated: false, ChaptersTruncated: false,
};

