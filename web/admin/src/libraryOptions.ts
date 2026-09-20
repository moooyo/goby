export interface LibraryOptions {
  EnableLocalMetadata: boolean;
  EnableLocalImages: boolean;
  EnableEmbeddedArtwork?: boolean;
}

export function validLibraryOptions(value: unknown): value is LibraryOptions {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  return 'EnableLocalMetadata' in value && typeof value.EnableLocalMetadata === 'boolean'
    && 'EnableLocalImages' in value && typeof value.EnableLocalImages === 'boolean'
    && (!('EnableEmbeddedArtwork' in value) || typeof value.EnableEmbeddedArtwork === 'boolean');
}

export function completeLibraryOptions(value: LibraryOptions): Required<LibraryOptions> {
  return {
    EnableLocalMetadata: value.EnableLocalMetadata,
    EnableLocalImages: value.EnableLocalImages,
    EnableEmbeddedArtwork: value.EnableEmbeddedArtwork ?? true,
  };
}
