import assert from 'node:assert/strict';
import test from 'node:test';
import { parseSortingDraft, sortingDraft, sortingDraftKey, validSortingSettings } from './sortingSettings.ts';
import { sortingWordKey } from './sortingSettingsFold.ts';

test('sorting drafts preserve token spelling and order and clear to an empty array', () => {
  assert.equal(sortingDraft(), '');
  assert.equal(sortingDraft({ SortRemoveWords: ['The', 'AN', 'das'] }), 'The\nAN\ndas');
  assert.deepEqual(parseSortingDraft('The\r\nAN\n\ndas\n'), { value: { SortRemoveWords: ['The', 'AN', 'das'] } });
  assert.deepEqual(parseSortingDraft(''), { value: { SortRemoveWords: [] } });
  assert.equal(sortingDraftKey('The\nAN'), sortingDraftKey('The\r\nAN\n'));
  assert.notEqual(sortingDraftKey('The\nAN'), sortingDraftKey('AN\nThe'));
  assert.notEqual(sortingDraftKey('The'), sortingDraftKey('the'));
  assert.notEqual(sortingDraftKey('The\nthe'), sortingDraftKey('The\nTHE'));
});

test('sorting input rejects whitespace without trimming or coercing tokens', () => {
  for (const draft of [' The', 'The ', 'the title', 'The\rAN', '\t', '\u0085', '\u00a0', '\u1680', '\u2007', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', 'The\n ']) {
    const parsed = parseSortingDraft(draft);
    assert.equal(parsed.value, undefined, JSON.stringify(draft));
    assert.equal(typeof parsed.error, 'string');
  }
  assert.deepEqual(parseSortingDraft('a.*\n$the\nthe-word'), { value: { SortRemoveWords: ['a.*', '$the', 'the-word'] } });
});

test('sorting response validation enforces count, UTF-8 bytes, controls, and valid Unicode', () => {
  assert.equal(validSortingSettings({ SortRemoveWords: ['x'.repeat(128), '😀'.repeat(32)] }), true);
  assert.equal(validSortingSettings({ SortRemoveWords: Array.from({ length: 32 }, (_, index) => `word${index}`) }), true);
  for (const words of [['x'.repeat(129)], ['😀'.repeat(33)], [''], ['a\0b'], ['a\u001fb'], ['a\u007fb'], ['a\u009fb'], ['\ud800'], ['\udfff'], Array.from({ length: 33 }, (_, index) => `word${index}`)]) {
    assert.equal(validSortingSettings({ SortRemoveWords: words }), false, JSON.stringify(words));
  }
  for (const value of [null, [], {}, { SortRemoveWords: null }, { SortRemoveWords: 'The' }, { SortRemoveWords: [1] }, { SortRemoveWords: [], Extra: true }]) {
    assert.equal(validSortingSettings(value), false, JSON.stringify(value));
  }
});

test('duplicate matching uses the pinned Go Unicode simple lowercase rules', () => {
  assert.equal(sortingWordKey('ÄΣΣİ'), 'äσσi');
  assert.equal(sortingWordKey('\u{10400}\u{10d50}\u{16ea0}'), '\u{10428}\u{10d70}\u{16ebb}');
  for (const words of [['The', 'the'], ['Ä', 'ä'], ['ΣΣ', 'σσ'], ['İ', 'i'], ['\u{16ea0}', '\u{16ebb}']]) {
    assert.equal(validSortingSettings({ SortRemoveWords: words }), false, JSON.stringify(words));
  }
  // Simple lowercase preserves final sigma and does not expand sharp s or normalize accents.
  assert.equal(validSortingSettings({ SortRemoveWords: ['Σ', 'ς', 'ß', 'ss', 'é', 'e\u0301'] }), true);
});
