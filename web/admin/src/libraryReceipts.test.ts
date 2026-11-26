import assert from 'node:assert/strict';
import test from 'node:test';
import { libraryProcessingReceipt } from './libraryReceipts.ts';

const options = { EnableLocalMetadata: true, EnableLocalImages: true, EnableOnlineImages: true, EnableIntroDetection: true, EnablePreviewGeneration: true };

test('library receipts list enabled features that apply to the saved catalog', () => {
  const video = ['Automatic seek previews are enabled.', 'View background processing progress in Tasks.', 'Automatic image downloads are enabled.', 'View image download progress in Tasks.'];
  assert.deepEqual(libraryProcessingReceipt('movies', options), video);
  assert.deepEqual(libraryProcessingReceipt('tvshows', options), ['Automatic intro detection is enabled.', ...video]);
  assert.deepEqual(libraryProcessingReceipt('music', options), []);
});

test('a rejected scan does not promise processing results or suppress the separate online-image notice', () => {
  assert.deepEqual(libraryProcessingReceipt('tvshows', options, true), ['Automatic image downloads are enabled.', 'View image download progress in Tasks.']);
});
