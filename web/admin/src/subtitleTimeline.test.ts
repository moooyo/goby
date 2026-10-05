import assert from 'node:assert/strict';
import test from 'node:test';
import { decodeSubtitleTimelineArtifact, decodeSubtitleTimelineDetail, decodeSubtitleTimelinePage, decodeSubtitleTimelineReceipt, validSubtitleTimelineRunInput } from './subtitleTimeline.ts';

const item = {
  ItemId: 'movie-1', LibraryId: 'library-1', Name: 'Example', SourceRevision: 'source-v1', DurationTicks: 600_000_000, SubtitleStreamCount: 2,
  State: 'missing', RequestedRevision: '0', CompletedRevision: '0', RunId: '', Reused: false, ErrorCode: '', RequestedAt: null, StartedAt: null, FinishedAt: null,
};
const track = { StreamIndex: 0, Codec: 'hdmv_pgs_subtitle', IntervalCount: 12, Warnings: [] };

test('missing subtitle artifacts remain empty without invented timeline tracks', () => {
  const input = { ...item, Artifact: { Available: false } };
  assert.deepEqual(decodeSubtitleTimelineDetail(input, item.ItemId)?.Artifact,
    { Available: false, Stale: false, Profile: '', Generation: '', DurationTicks: 0, Size: 0, Tracks: [] });
  assert.deepEqual(input.Artifact, { Available: false });
  assert.equal(decodeSubtitleTimelineDetail(input, 'another-item'), undefined);
  assert.equal(decodeSubtitleTimelineDetail({ ...input, DurationTicks: 0, SubtitleStreamCount: 0 }, item.ItemId)?.SubtitleStreamCount, 0);
});

test('subtitle artifacts preserve stream identity, codec, empty interval counts and warnings', () => {
  const artifact = { Available: true, DurationTicks: item.DurationTicks, Profile: 'bitmap-timeline-v1', Generation: 'gen-test.gstl', Size: 1500,
    Tracks: [track, { StreamIndex: 3, Codec: 'dvd_subtitle', IntervalCount: 0, Warnings: ['open_end_clamped'] }] };
  const value = decodeSubtitleTimelineArtifact(artifact);
  assert.equal(value?.Generation, artifact.Generation);
  assert.deepEqual(value?.Tracks.map((entry) => [entry.StreamIndex, entry.Codec, entry.IntervalCount]), [[0, 'hdmv_pgs_subtitle', 12], [3, 'dvd_subtitle', 0]]);
  assert.deepEqual(value?.Tracks[1].Warnings, ['open_end_clamped']);
  value?.Tracks[1].Warnings.push('another_warning');
  assert.deepEqual(artifact.Tracks[1].Warnings, ['open_end_clamped']);
});

test('completed subtitle details normalize Go nil warning slices without hiding generated tracks', () => {
  const completed = { ...item, State: 'ready', RequestedRevision: '1', CompletedRevision: '1', RunId: 'run-completed',
    RequestedAt: '2026-10-05T01:00:00Z', StartedAt: '2026-10-05T01:00:01Z', FinishedAt: '2026-10-05T01:00:02Z',
    Artifact: { Available: true, Stale: false, Profile: 'bitmap-timeline-v1', Generation: 'gen-test.gstl', DurationTicks: item.DurationTicks, Size: 1500,
      Tracks: [{ ...track, Warnings: null }, { StreamIndex: 3, Codec: 'dvd_subtitle', IntervalCount: 4, Warnings: [] }] } };
  const detail = decodeSubtitleTimelineDetail(completed, item.ItemId);
  assert.equal(detail?.State, 'ready');
  assert.equal(detail?.RunId, 'run-completed');
  assert.equal(detail?.Artifact.Available, true);
  assert.deepEqual(detail?.Artifact.Tracks.map((entry) => [entry.StreamIndex, entry.IntervalCount, entry.Warnings]), [[0, 12, []], [3, 4, []]]);
  assert.equal(completed.Artifact.Tracks[0].Warnings, null);
  const page = decodeSubtitleTimelinePage({ Items: [completed], TotalRecordCount: 1, StartIndex: 0, Limit: 25 }, 0, 25, item.LibraryId);
  assert.equal(page?.Items[0].State, 'ready');
  assert.equal(page?.Items[0].RunId, 'run-completed');
});

test('old subtitle artifacts remain inspectable after their source duration or track count changes', () => {
  const value = decodeSubtitleTimelineDetail({ ...item, DurationTicks: 120_000_000, SubtitleStreamCount: 0,
    Artifact: { Available: false, Stale: true, DurationTicks: item.DurationTicks, Tracks: [track] } }, item.ItemId);
  assert.equal(value?.Artifact.Available, false);
  assert.equal(value?.Artifact.Stale, true);
  assert.equal(value?.Artifact.Tracks[0].IntervalCount, 12);
  assert.equal(value?.DurationTicks, 120_000_000);
});

test('invalid subtitle artifacts cannot become apparently valid empty results', () => {
  for (const input of [{ Available: false, Stale: null }, { Available: false, Tracks: null }, { Available: false, DurationTicks: '0' },
    { Available: false, Generation: 1 }, { Available: false, Size: -1 }, { Available: false, Profile: null },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [] }, { Available: true, Tracks: [track] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [track, track] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, StreamIndex: -1 }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, IntervalCount: 1.5 }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: undefined }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: 'warning' }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: {} }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: 0 }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: [''] }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, Warnings: [null] }] }]) {
    assert.equal(decodeSubtitleTimelineArtifact(input), undefined);
  }
});

test('subtitle inventory rejects foreign identities, duplicate rows and unknown task states', () => {
  const page = { Items: [item], TotalRecordCount: 40, StartIndex: 25, Limit: 25 };
  assert.equal(decodeSubtitleTimelinePage(page, 25, 25, 'library-1')?.Items[0].ItemId, item.ItemId);
  assert.equal(decodeSubtitleTimelinePage(page, 0, 25), undefined);
  assert.equal(decodeSubtitleTimelinePage(page, 25, 25, 'library-2'), undefined);
  assert.equal(decodeSubtitleTimelinePage({ ...page, Items: [item, item] }, 25, 25), undefined);
  assert.equal(decodeSubtitleTimelinePage({ ...page, Items: [{ ...item, State: 'invented' }] }, 25, 25), undefined);
});

test('subtitle regeneration receipts retain their kind and request identity on replay', () => {
  const input = { Kind: 'subtitle-timeline', RequestId: 'b7145d27-8577-4c91-a3ce-13e8d08c45fe', LibraryIds: ['library-1'], ItemIds: [], Force: true };
  assert.equal(validSubtitleTimelineRunInput(input), true);
  assert.equal(validSubtitleTimelineRunInput({ ...input, Kind: 'waveform' }), false);
  assert.equal(validSubtitleTimelineRunInput({ ...input, Force: 'true' }), false);
  const receipt = { RunId: 'run-1', TaskId: 'task-1', Admitted: false, Queued: 0 };
  assert.deepEqual(decodeSubtitleTimelineReceipt(receipt), receipt);
  assert.equal(decodeSubtitleTimelineReceipt({ ...receipt, Queued: -1 }), undefined);
});
