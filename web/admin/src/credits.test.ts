import assert from 'node:assert/strict';
import test from 'node:test';
import { creditsDetectionLabel, decodeItemCredits } from './credits.ts';

const base = { ItemId: 'movie', MediaSourceId: 'source', SourceRevision: 'revision', Revision: '1', DurationTicks: 1_000,
  Automatic: null, Effective: null, Override: null, OverrideStale: false, LastEditedBy: '', LastEditedAt: null };
const detection = { Detected: [{ StartTicks: 800, EndTicks: 850, Source: 'BlackFrame' }, { StartTicks: 900, EndTicks: 1_000, Source: 'Combined' }],
  DetectedStale: false, DetectedRevision: '2', DetectedStatus: 'qualified', DetectedReason: '', DetectedUpdatedAt: '2026-10-04T00:00:00Z' };

test('legacy manual credits remain editable without inventing automatic detection', () => {
  const input = { ...base, Automatic: { StartTicks: 920, Provenance: 'Chapter' }, Effective: { StartTicks: 810, Provenance: 'Manual' }, Override: { StartTicks: 810, Provenance: 'Manual' } };
  const value = decodeItemCredits(input, 'movie');
  assert.equal(value?.Effective?.Provenance, 'Manual');
  assert.deepEqual(value?.Detected, []);
  assert.equal(value?.DetectedStatus, 'not_analyzed');
  assert.equal('Detected' in input, false);
});

test('detected intervals preserve intervening content and do not replace server-selected manual or chapter precedence', () => {
  for (const provenance of ['Manual', 'Import', 'Chapter']) {
    const value = decodeItemCredits({ ...base, ...detection, Automatic: provenance === 'Chapter' ? { StartTicks: 930, Provenance: 'Chapter' } : { StartTicks: 800, Provenance: 'Detected' }, Effective: { StartTicks: 930, Provenance: provenance },
      Override: provenance === 'Chapter' ? null : { StartTicks: 930, Provenance: provenance } }, 'movie');
    assert.equal(value?.Effective?.StartTicks, 930);
    assert.deepEqual(value?.Detected.map((segment) => [segment.StartTicks, segment.EndTicks]), [[800, 850], [900, 1_000]]);
  }
});

test('stale detection remains inspectable after a shorter replacement but can never become effective', () => {
  const value = decodeItemCredits({ ...base, ...detection, DurationTicks: 600, DetectedStale: true, DetectedReason: 'source_changed' }, 'movie');
  assert.equal(value?.Detected[1].EndTicks, 1_000);
  assert.equal(value?.Effective, null);
  assert.equal(value && creditsDetectionLabel(value), '检测结果已过期');
  assert.equal(decodeItemCredits({ ...base, ...detection, DetectedStale: true, Effective: { StartTicks: 800, Provenance: 'Detected' } }, 'movie'), undefined);
});

test('valid automatic starts use the first actual detected interval and malformed partial responses are rejected', () => {
  const automatic = { StartTicks: 800, Provenance: 'Detected' };
  assert.equal(decodeItemCredits({ ...base, ...detection, Automatic: automatic, Effective: automatic }, 'movie')?.Effective?.StartTicks, 800);
  for (const input of [{ ...base, Detected: [] }, { ...base, ...detection, DetectedStale: null },
    { ...base, ...detection, Effective: { StartTicks: 900, Provenance: 'Detected' } },
    { ...base, ...detection, Override: automatic },
    { ...base, ...detection, Detected: [{ StartTicks: 800, EndTicks: 850, Source: 'Guess' }] },
    { ...base, ...detection, Detected: [{ StartTicks: 800, EndTicks: 1_001, Source: 'Chromaprint' }] }]) assert.equal(decodeItemCredits(input, 'movie'), undefined);
  assert.equal(decodeItemCredits({ ...base, ...detection }, 'other-item'), undefined);
});

test('an empty completed detection is a no-result state rather than an automatic marker', () => {
  const value = decodeItemCredits({ ...base, ...detection, Detected: [], DetectedStatus: 'no_result', DetectedReason: 'no_credits_detected' }, 'movie');
  assert.equal(value?.Automatic, null);
  assert.equal(value && creditsDetectionLabel(value), '未识别到片尾区间');
  assert.equal(decodeItemCredits({ ...base, ...detection, DetectedStatus: 'no_result', Effective: { StartTicks: 800, Provenance: 'Detected' } }, 'movie'), undefined);
});
