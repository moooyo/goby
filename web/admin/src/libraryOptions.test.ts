import assert from 'node:assert/strict';
import test from 'node:test';
import { completeLibraryOptions, validLibraryOptions } from './libraryOptions.ts';

test('older library responses keep automatic processing off without mutating the response', () => {
  const previous = { EnableLocalMetadata: false, EnableLocalImages: true };
  assert.equal(validLibraryOptions(previous), true);
  assert.deepEqual(completeLibraryOptions(previous), { ...previous, EnableEmbeddedArtwork: true, EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  assert.equal('EnableEmbeddedArtwork' in previous, false);
  assert.equal('EnableIntroDetection' in previous, false);
  assert.equal('EnableCreditsDetection' in previous, false);
  assert.equal('EnablePreviewGeneration' in previous, false);
  assert.equal('EnableBackgroundPreviewGeneration' in previous, false);
  assert.equal('EnableAudioWaveformGeneration' in previous, false);
  assert.equal('EnableSubtitleTimelineGeneration' in previous, false);
});

test('stored extraction choice survives a disabled image import master switch', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: true, EnableLocalImages: false, EnableEmbeddedArtwork: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), { ...options, EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  }
});

test('an explicit intro choice survives unrelated import choices', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: enabled, EnablePreviewGeneration: false };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), { ...options, EnableCreditsDetection: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  }
  for (const value of [null, 'true', 1]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableIntroDetection: value }), false);
  }
});

test('seek previews are an independent explicit library choice', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: true, EnablePreviewGeneration: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), { ...options, EnableCreditsDetection: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
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

test('background previews are independent from seek previews and remain opt in', () => {
  for (const enabled of [true, false]) {
    const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: enabled };
    assert.equal(validLibraryOptions(options), true);
    assert.deepEqual(completeLibraryOptions(options), { ...options, EnableCreditsDetection: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  }
  for (const value of [null, 'true', 1, undefined]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableBackgroundPreviewGeneration: value }), false);
  }
});

test('audio waveform generation preserves explicit choices independently of background previews', () => {
  for (const enabled of [true, false]) {
    for (const backgroundEnabled of [true, false]) {
      const options = { EnableLocalMetadata: false, EnableLocalImages: false, EnableEmbeddedArtwork: false, EnableIntroDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: backgroundEnabled, EnableAudioWaveformGeneration: enabled };
      assert.equal(validLibraryOptions(options), true);
      assert.deepEqual(completeLibraryOptions(options), { ...options, EnableCreditsDetection: false, EnableSubtitleTimelineGeneration: false });
    }
  }
  for (const value of [null, 'true', 1, undefined]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableAudioWaveformGeneration: value }), false);
  }
});

test('credits detection is opt in and preserves every existing processing choice', () => {
  for (const enabled of [true, false]) {
    for (const existingEnabled of [true, false]) {
      const options = { EnableLocalMetadata: existingEnabled, EnableLocalImages: existingEnabled, EnableEmbeddedArtwork: existingEnabled, EnableIntroDetection: existingEnabled, EnableCreditsDetection: enabled, EnablePreviewGeneration: existingEnabled, EnableBackgroundPreviewGeneration: existingEnabled, EnableAudioWaveformGeneration: existingEnabled };
      assert.equal(validLibraryOptions(options), true);
      assert.deepEqual(completeLibraryOptions(options), { ...options, EnableSubtitleTimelineGeneration: false });
    }
  }
  for (const value of [null, 'true', 1, undefined]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableCreditsDetection: value }), false);
  }
});

test('subtitle timeline generation is opt in and preserves independent processing choices', () => {
  for (const enabled of [true, false]) {
    for (const existingEnabled of [true, false]) {
      const options = { EnableLocalMetadata: existingEnabled, EnableLocalImages: existingEnabled, EnableEmbeddedArtwork: existingEnabled,
        EnableIntroDetection: existingEnabled, EnableCreditsDetection: existingEnabled, EnablePreviewGeneration: existingEnabled,
        EnableBackgroundPreviewGeneration: existingEnabled, EnableAudioWaveformGeneration: existingEnabled, EnableSubtitleTimelineGeneration: enabled };
      assert.equal(validLibraryOptions(options), true);
      assert.deepEqual(completeLibraryOptions(options), options);
    }
  }
  for (const value of [null, 'true', 1, undefined]) {
    assert.equal(validLibraryOptions({ EnableLocalMetadata: true, EnableLocalImages: true, EnableSubtitleTimelineGeneration: value }), false);
  }
});
