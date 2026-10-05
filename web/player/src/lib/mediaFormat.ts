import type { MediaSource, MediaStream } from '../types';

const join = (values: Array<string | undefined>) => values.filter(Boolean).join(' · ');
const compact = (value?: string) => (value ?? '').replace(/[\s_.-]/g, '').toLowerCase();

export function videoResolution(stream?: MediaStream): string {
  if (!stream) return '';
  const width = stream.Width ?? 0;
  const height = stream.Height ?? 0;
  if (width >= 7600) return '8K';
  if (width >= 3800) return '4K';
  if (width >= 1900 || height >= 1000) return '1080p';
  if (width >= 1200 || height >= 700) return '720p';
  return height ? `${height}p` : '';
}

export function videoDynamicRange(stream?: MediaStream): string {
  if (!stream) return '';
  const range = stream.VideoRangeType || stream.VideoRange;
  const key = compact(range);
  if ((stream.DvProfile ?? 0) > 0 || key.includes('dovi') || key.includes('dolbyvision')) return 'Dolby Vision';
  if (key === 'hdr10plus' || key === 'hdr10+') return 'HDR10+';
  if (key === 'hdr10') return 'HDR10';
  if (key === 'hlg' || compact(stream.ColorTransfer) === 'aribstdb67') return 'HLG';
  if (key === 'hdr' || compact(stream.ColorTransfer) === 'smpte2084') return 'HDR';
  return range && !['sdr', 'unknown'].includes(key) ? range : '';
}

export function videoCodec(stream?: MediaStream): string {
  const value = stream?.Codec ?? '';
  return ({ h264: 'H.264', avc: 'H.264', avc1: 'H.264', hevc: 'HEVC', h265: 'HEVC', mpeg2video: 'MPEG-2', mpeg4: 'MPEG-4', av1: 'AV1', vp9: 'VP9', vp8: 'VP8' } as Record<string, string>)[value.toLowerCase()] ?? value.toUpperCase();
}

export function videoColorFacts(stream?: MediaStream): string {
  if (!stream) return '';
  const primaries = ({ bt2020: 'BT.2020', bt709: 'BT.709', smpte432: 'Display P3', smpte431: 'DCI-P3' } as Record<string, string>)[stream.ColorPrimaries ?? ''] ?? stream.ColorPrimaries;
  const matrix = ({ bt2020nc: 'BT.2020 NCL', bt2020c: 'BT.2020 CL', bt709: 'BT.709' } as Record<string, string>)[stream.ColorSpace ?? ''] ?? stream.ColorSpace;
  const transfer = ({ smpte2084: 'PQ', 'arib-std-b67': 'HLG', bt709: 'BT.709', 'iec61966-2-1': 'sRGB' } as Record<string, string>)[stream.ColorTransfer ?? ''] ?? stream.ColorTransfer;
  return join([...new Set([primaries || matrix, transfer])]);
}

function codecLevel(stream: MediaStream): string {
  if (!stream.Level || stream.Level < 0) return '';
  const codec = compact(stream.Codec);
  const level = ['hevc', 'h265'].includes(codec) && stream.Level >= 30 ? stream.Level / 30 : ['h264', 'avc', 'avc1'].includes(codec) && stream.Level >= 10 ? stream.Level / 10 : stream.Level;
  return `L${Number(level.toFixed(2))}`;
}

function chromaSampling(pixelFormat?: string): string {
  if (!pixelFormat) return '';
  if (/^(?:yuvj?420|yuva420|nv12|nv21|p010|p012|p016)/i.test(pixelFormat)) return '4:2:0';
  if (/^(?:yuvj?422|yuva422|yuyv422|uyvy422|nv16|p210|p216)/i.test(pixelFormat)) return '4:2:2';
  if (/^(?:yuvj?444|yuva444|gbr|rgb|bgr)/i.test(pixelFormat)) return /^(gbr|rgb|bgr)/i.test(pixelFormat) ? 'RGB' : '4:4:4';
  return pixelFormat;
}

export function videoHUD(stream?: MediaStream): string {
  if (!stream) return '';
  const level = codecLevel(stream);
  const codec = [videoCodec(stream), stream.Profile, level ? `@ ${level}` : ''].filter(Boolean).join(' ');
  const sampling = [stream.BitDepth ? `${stream.BitDepth}-bit` : '', chromaSampling(stream.PixelFormat)].filter(Boolean).join(' ');
  return join([codec, sampling]);
}

export function audioFormat(stream?: MediaStream): string {
  if (!stream) return '';
  if (/\batmos\b|\bjoc\b/i.test(stream.Profile ?? '')) return 'Dolby Atmos';
  const codec = compact(stream.Codec);
  if (codec === 'eac3') return 'Dolby Digital Plus';
  if (codec === 'ac3') return 'Dolby Digital';
  if (codec === 'truehd') return 'Dolby TrueHD';
  if (codec === 'dts' && /dts[- ]hd\s*(?:ma|master audio)|master audio/i.test(stream.Profile ?? '')) return 'DTS-HD Master Audio';
  if (codec === 'aac' && stream.Channels === 2) return 'AAC Stereo';
  return stream.Codec?.toUpperCase() ?? '';
}

function aspectRatio(stream?: MediaStream): string {
  if (!stream?.Width || !stream.Height) return '';
  const ratio = stream.Width / stream.Height;
  if (Math.abs(ratio - 16 / 9) < .02) return '16 : 9';
  if (Math.abs(ratio - 4 / 3) < .02) return '4 : 3';
  return `${ratio.toFixed(2)} : 1`;
}

export function mediaFormatMarks(video?: MediaStream, audio?: MediaStream, source?: MediaSource): Array<{ big: string; small: string }> {
  const resolution = videoResolution(video);
  const range = videoDynamicRange(video);
  const resolutionTitle = ({ '8K': '8K Ultra HD', '4K': '4K Ultra HD', '1080p': 'Full HD 1080p', '720p': 'HD 720p' } as Record<string, string>)[resolution] ?? resolution;
  const dvProfile = video?.DvProfile ? `Profile ${video.DvProfile}${video.DvProfile === 8 && [1, 2, 4].includes(video.DvBlSignalCompatibilityId ?? 0) ? `.${video.DvBlSignalCompatibilityId}` : ''}` : '';
  const channelLayout = ({ mono: '1.0', stereo: '2.0' } as Record<string, string>)[audio?.ChannelLayout ?? ''] || audio?.ChannelLayout || ({ 1: '1.0', 2: '2.0', 6: '5.1', 8: '7.1' } as Record<number, string>)[audio?.Channels ?? 0] || (audio?.Channels ? `${audio.Channels} ch` : '');
  const audioBitrate = audio?.BitRate ? `${(audio.BitRate / 1000).toLocaleString('en-US', { maximumFractionDigits: 0 })} kbps` : '';
  return [
    { big: resolutionTitle || source?.Container?.toUpperCase() || '', small: join([video?.Width && video.Height ? `${video.Width} × ${video.Height}` : '', aspectRatio(video)]) },
    { big: range, small: join([range === 'Dolby Vision' ? dvProfile : '', videoColorFacts(video), video?.BitDepth ? `${video.BitDepth}-bit` : '']) },
    { big: audioFormat(audio), small: join([[audio?.Codec?.toUpperCase(), channelLayout].filter(Boolean).join(' '), audioBitrate, audio?.SampleRate ? `${audio.SampleRate / 1000} kHz` : '']) },
  ].filter(mark => mark.big);
}
