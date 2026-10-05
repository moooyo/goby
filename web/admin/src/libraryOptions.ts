export interface LibraryOptions {
  EnableLocalMetadata: boolean;
  EnableLocalImages: boolean;
  EnableEmbeddedArtwork?: boolean;
  EnableIntroDetection?: boolean;
  EnableCreditsDetection?: boolean;
  EnablePreviewGeneration?: boolean;
  EnableBackgroundPreviewGeneration?: boolean;
  EnableAudioWaveformGeneration?: boolean;
  EnableSubtitleTimelineGeneration?: boolean;
}

export function validLibraryOptions(value: unknown): value is LibraryOptions {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  return 'EnableLocalMetadata' in value && typeof value.EnableLocalMetadata === 'boolean'
    && 'EnableLocalImages' in value && typeof value.EnableLocalImages === 'boolean'
    && (!('EnableEmbeddedArtwork' in value) || typeof value.EnableEmbeddedArtwork === 'boolean')
    && (!('EnableIntroDetection' in value) || typeof value.EnableIntroDetection === 'boolean')
    && (!('EnableCreditsDetection' in value) || typeof value.EnableCreditsDetection === 'boolean')
    && (!('EnablePreviewGeneration' in value) || typeof value.EnablePreviewGeneration === 'boolean')
    && (!('EnableBackgroundPreviewGeneration' in value) || typeof value.EnableBackgroundPreviewGeneration === 'boolean')
    && (!('EnableAudioWaveformGeneration' in value) || typeof value.EnableAudioWaveformGeneration === 'boolean')
    && (!('EnableSubtitleTimelineGeneration' in value) || typeof value.EnableSubtitleTimelineGeneration === 'boolean');
}

export function completeLibraryOptions(value: LibraryOptions): Required<LibraryOptions> {
  return {
    EnableLocalMetadata: value.EnableLocalMetadata,
    EnableLocalImages: value.EnableLocalImages,
    EnableEmbeddedArtwork: value.EnableEmbeddedArtwork ?? true,
    EnableIntroDetection: value.EnableIntroDetection ?? false,
    EnableCreditsDetection: value.EnableCreditsDetection ?? false,
    EnablePreviewGeneration: value.EnablePreviewGeneration ?? false,
    EnableBackgroundPreviewGeneration: value.EnableBackgroundPreviewGeneration ?? false,
    EnableAudioWaveformGeneration: value.EnableAudioWaveformGeneration ?? false,
    EnableSubtitleTimelineGeneration: value.EnableSubtitleTimelineGeneration ?? false,
  };
}
