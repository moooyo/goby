import assert from 'node:assert/strict';
import test from 'node:test';
import { decodeAudioWaveformArtifact, decodeAudioWaveformDetail, decodeAudioWaveformPage, decodeAudioWaveformReceipt, validAudioWaveformRunInput } from './audioWaveform.ts';

const item = {
  ItemId: 'movie-1', LibraryId: 'library-1', Name: 'Example', SourceRevision: 'source-v1', DurationTicks: 600_000_000, AudioStreamCount: 2,
  State: 'missing', RequestedRevision: '0', CompletedRevision: '0', RunId: '', Reused: false, ErrorCode: '', RequestedAt: null, StartedAt: null, FinishedAt: null,
};
const track = { StreamIndex: 0, Channels: 2, SampleRate: 48000, ChannelLayout: 'stereo', SampleCount: 2_880_000, CoverageStartTicks: 0, CoverageEndTicks: 600_000_000 };

test('new items accept an omitted empty artifact payload without inventing generated tracks', () => {
  const input = { ...item, Artifact: { Available: false } };
  const value = decodeAudioWaveformDetail(input, item.ItemId);
  assert.deepEqual(value?.Artifact, { Available: false, Stale: false, DurationTicks: 0, Tracks: [] });
  assert.deepEqual(input.Artifact, { Available: false });
  assert.equal(decodeAudioWaveformDetail(input, 'another-item'), undefined);
  assert.equal(decodeAudioWaveformDetail({ ...input, DurationTicks: 0, AudioStreamCount: 0 }, item.ItemId)?.AudioStreamCount, 0);
});

test('available multi-track artifacts preserve stream zero and the identity of every audio stream', () => {
  const value = decodeAudioWaveformArtifact({ Available: true, DurationTicks: item.DurationTicks, Tracks: [track, { ...track, StreamIndex: 3, Channels: 6, ChannelLayout: '5.1' }] });
  assert.equal(value?.Stale, false);
  assert.deepEqual(value?.Tracks.map((entry) => [entry.StreamIndex, entry.Channels]), [[0, 2], [3, 6]]);
  assert.equal(value?.Tracks[0].CoverageStartTicks, 0);
  assert.equal(decodeAudioWaveformArtifact({ Available: true, DurationTicks: item.DurationTicks, Tracks: [track, track] }), undefined);
  assert.equal(decodeAudioWaveformArtifact({ Available: true, Tracks: [track] }), undefined);
  assert.equal(decodeAudioWaveformArtifact({ Available: true, DurationTicks: item.DurationTicks, Tracks: [] }), undefined);
});

test('stale artifacts remain inspectable even after the replacement source changes duration or track count', () => {
  const value = decodeAudioWaveformDetail({ ...item, DurationTicks: 120_000_000, AudioStreamCount: 1,
    Artifact: { Available: false, Stale: true, DurationTicks: item.DurationTicks, Tracks: [track, { ...track, StreamIndex: 2 }] } }, item.ItemId);
  assert.equal(value?.Artifact.Available, false);
  assert.equal(value?.Artifact.Stale, true);
  assert.equal(value?.Artifact.Tracks.length, 2);
  assert.equal(value?.DurationTicks, 120_000_000);
});

test('malformed explicit values are rejected rather than normalized as empty waveform artifacts', () => {
  for (const input of [{ Available: false, Stale: null }, { Available: false, Tracks: null }, { Available: false, DurationTicks: '0' },
    { Available: false, Stale: undefined }, { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, SampleCount: 0 }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, CoverageEndTicks: item.DurationTicks + 1 }] },
    { Available: true, DurationTicks: item.DurationTicks, Tracks: [{ ...track, StreamIndex: -1 }] }]) assert.equal(decodeAudioWaveformArtifact(input), undefined);
});

test('inventory decoding accepts rows without filesystem artifacts and rejects foreign or duplicate identities', () => {
  const page = { Items: [item], TotalRecordCount: 40, StartIndex: 25, Limit: 25 };
  assert.equal(decodeAudioWaveformPage(page, 25, 25, 'library-1')?.Items[0].ItemId, item.ItemId);
  assert.equal(decodeAudioWaveformPage(page, 0, 25), undefined);
  assert.equal(decodeAudioWaveformPage(page, 25, 25, 'library-2'), undefined);
  assert.equal(decodeAudioWaveformPage({ ...page, Items: [item, item] }, 25, 25), undefined);
  assert.equal(decodeAudioWaveformPage({ ...page, Items: [{ ...item, State: 'invented' }] }, 25, 25), undefined);
});

test('replayed admissions with no newly queued items remain valid and retain their request identity', () => {
  const input = { Kind: 'waveform', RequestId: 'b7145d27-8577-4c91-a3ce-13e8d08c45fe', LibraryIds: [], ItemIds: ['movie-1'], Force: true };
  assert.equal(validAudioWaveformRunInput(input), true);
  assert.equal(validAudioWaveformRunInput({ ...input, Kind: 'background' }), false);
  assert.equal(validAudioWaveformRunInput({ ...input, Force: 'true' }), false);
  const receipt = { RunId: 'run-1', TaskId: 'task-1', Admitted: false, Queued: 0 };
  assert.deepEqual(decodeAudioWaveformReceipt(receipt), receipt);
  assert.equal(decodeAudioWaveformReceipt({ ...receipt, Queued: -1 }), undefined);
});
