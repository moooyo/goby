import assert from 'node:assert/strict';
import test from 'node:test';
import { completeLibraryOptions, validLibraryOptions } from './libraryOptions.ts';

test('older library responses retain enabled embedded artwork without mutating the response', () => {
  const previous = { EnableLocalMetadata: false, EnableLocalImages: true };
  assert.equal(validLibraryOptions(previous), true);
  assert.deepEqual(completeLibraryOptions(previous), { ...previous, EnableEmbeddedArtwork: true });
  assert.equal('EnableEmbeddedArtwork' in previous, false);
});

test('stored extraction choice survives a disabled image import master switch', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), options);
  }
});

test('library responses reject malformed artwork choices before editing', () => {
  for (const value of [null, [], {}, { EnableLocalMetadata: true }, { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: null }, { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: 'false' }]) {
    assert.equal(validLibraryOptions(value), false, JSON.stringify(value));
  }
});
