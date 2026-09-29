import assert from 'node:assert/strict';
import test from 'node:test';
import { completeLibraryOptions, validLibraryOptions } from './libraryOptions.ts';

test('older library responses keep automatic processing off without mutating the response', () => {
  const previous = { EnableLocalMetadata: false, EnableLocalImages: true };
  assert.equal(validLibraryOptions(previous), true);
  assert.deepEqual(completeLibraryOptions(previous), { ...previous, EnableEmbeddedArtwork: true, EnableIntroDetection: false, EnablePreviewGeneration: false });
  assert.equal('EnableEmbeddedArtwork' in previous, false);
  assert.equal('EnableIntroDetection' in previous, false);
  assert.equal('EnablePreviewGeneration' in previous, false);
});

test('stored extraction choice survives a disabled image import master switch', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), { ...options, EnableIntroDetection: false, EnablePreviewGeneration: false });
  }
});

test('an explicit intro choice survives unrelated import choices', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: enabled, EnablePreviewGeneration: false };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), options);
  }
  for (const value of [null, 'true', 1]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableIntroDetection: value }), false);
  }
});

test('seek previews are an independent explicit library choice', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: true, EnablePreviewGeneration: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), options);
  }
  for (const value of [null, 'true', 1]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnablePreviewGeneration: value }), false);
  }
});

test('library responses reject malformed artwork choices before editing', () => {
  for (const value of [null, [], {}, { EnableLocalMetadata: true }, { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: null }, { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: 'false' }]) {
    assert.equal(validLibraryOptions(value), false, JSON.stringify(value));
  }
});
