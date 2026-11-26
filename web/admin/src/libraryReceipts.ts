import type { LibraryOptions } from './libraryOptions';
import { supportsVideoAnalysis } from './mediaAnalysis.ts';

const videoProcessing = [
  ['EnableCreditsDetection', 'Automatic credits detection is enabled.'],
  ['EnablePreviewGeneration', 'Automatic seek previews are enabled.'],
  ['EnableBackgroundPreviewGeneration', 'Automatic background clips are enabled.'],
  ['EnableAudioWaveformGeneration', 'Automatic audio waveforms are enabled.'],
  ['EnableSubtitleTimelineGeneration', 'Automatic subtitle timelines are enabled.'],
] as const;

export function libraryProcessingReceipt(type: string, options?: LibraryOptions, scanFailed = false) {
  const messages: string[] = [];
  if (!supportsVideoAnalysis(type) || !options) return messages;
  if (!scanFailed) {
    if (type === 'tvshows' && options.EnableIntroDetection) messages.push('Automatic intro detection is enabled.');
    for (const [option, message] of videoProcessing) if (options[option]) messages.push(message);
    if (messages.length) messages.push('View background processing progress in Tasks.');
  }
  if (options.EnableOnlineImages) messages.push('Automatic image downloads are enabled.', 'View image download progress in Tasks.');
  return messages;
}
