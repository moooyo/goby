import assert from 'node:assert/strict';
import test from 'node:test';
import { analysisCurrentCandidate, analysisDraft, analysisRevision, parseAnalysisDraft, validAnalysisConfiguration, validAnalysisDetection, validAnalysisItems, validAnalysisOverview, validAnalysisProfile } from './mediaAnalysis.ts';
import type { AnalysisCandidate, AnalysisDetection, AnalysisItem, AnalysisMetrics, AnalysisProfile } from './mediaAnalysis.ts';

const profile: AnalysisProfile = { AutoPublishIntros: true, PreviewIntervalSeconds: 10, PreviewQuality: 80, MaxSourceBytes: 128 * 2 ** 30, MaxItemRuntimeSeconds: 1200, FeatureCacheMaxBytes: 128 * 2 ** 20 };
const stamp = '2026-09-21T00:00:00Z';
function detection(): AnalysisDetection {
  return { ItemId: 'episode-1', Revision: '9007199254740993', ManualRevision: '9007199254740994', SourceRevision: 'source-current', Status: 'not_analyzed', Reasons: ['not_analyzed'], Candidate: null, Effective: null, Suppressed: false, UpdatedAt: '0001-01-01T00:00:00Z' };
}
function item(): AnalysisItem { return { Id: 'episode-1', Name: 'Pilot', Type: 'Episode', LibraryId: 'library-1', MediaSourceId: 'media-source-1', SourceRevision: 'source-current', Detection: detection(), Previews: [] }; }
function candidate(): AnalysisCandidate {
  return { Interval: { StartTicks: 50_000_000, EndTicks: 400_000_000 }, GroupID: 'group', Status: 'review', Reasons: [], Support: [], Metrics: {
    AudioAgreementPermille: 900, AudioInformativePermille: 700, AudioSimilarityPermille: 910, AudioSamples: 100, AudioDistinct: 40,
    VisualAgreementPermille: 920, VisualSimilarityPermille: 930, VisualCoveragePermille: 880, VisualSamples: 50, VisualTransitions: 7,
    VisualChangeCoveragePermille: 500, VisualDominancePermille: 300, BoundaryUncertaintyTicks: 10_000_000, PairCount: 3,
    VisualAnchorCount: 24, VisualMinBandMatchedPermille: 600, VisualMatchedTimePermille: 875,
    VisualContradictedTimePermille: 50, VisualUnobservableTimePermille: 125, VisualMaxUnconfirmedGapTicks: 25_000_000,
    VisualStartAnchorGapTicks: 10_000_000, VisualEndAnchorGapTicks: 15_000_000, VisualDistinctStates: 6, VisualDominantStatePermille: 300,
  } };
}

test('profile drafts preserve exact saved values and explicit false without client defaults', () => {
  const value = { ...profile, AutoPublishIntros: false, MaxSourceBytes: 1, PreviewQuality: 95, FeatureCacheMaxBytes: 512 * 2 ** 20 };
  const draft = analysisDraft(value);
  assert.deepEqual(parseAnalysisDraft(draft), { errors: {}, profile: value });
  draft.MaxSourceBytes = '4096';
  assert.equal(value.MaxSourceBytes, 1);
  assert.equal(parseAnalysisDraft(draft).profile?.MaxSourceBytes, 4096);
});

test('processing bounds reject empty, fractional, exponential and out-of-range edits atomically', () => {
  for (const [key, values] of Object.entries({ PreviewIntervalSeconds: ['', '0', '1', '121', '2e1', '10.0'], PreviewQuality: ['39', '96'], MaxSourceBytes: ['0', String(2 ** 40 + 1)], MaxItemRuntimeSeconds: ['0', '7201'], FeatureCacheMaxBytes: [String(2 ** 20 - 1), String(512 * 2 ** 20 + 1)] })) {
    for (const value of values) {
      const result = parseAnalysisDraft({ ...analysisDraft(profile), [key]: value });
      assert.equal(result.profile, undefined, `${key}=${value}`);
      assert.ok(result.errors[key as keyof typeof result.errors]);
    }
  }
  assert.equal(validAnalysisProfile({ ...profile, AutoPublishIntros: null }), false);
  assert.equal(validAnalysisProfile({ ...profile, PreviewQuality: '80' }), false);
});

test('configuration and decision revisions remain exact decimal strings across the safe-number boundary', () => {
  assert.equal(analysisRevision('9007199254740993'), true);
  assert.equal(analysisRevision('9223372036854775807'), true);
  assert.equal(analysisRevision('9223372036854775808'), false);
  assert.equal(analysisRevision('01'), false);
  assert.equal(analysisRevision(9007199254740993), false);
  assert.equal(analysisRevision('0'), false);
  assert.equal(analysisRevision('0', true), true);
  assert.equal(validAnalysisConfiguration({ Revision: '9007199254740993', Profile: profile, Defaults: profile, UpdatedAt: stamp }), true);
  assert.equal(validAnalysisDetection(detection()), true);
  assert.equal(validAnalysisDetection({ ...detection(), ManualRevision: undefined }), false);
  assert.equal(validAnalysisDetection({ ...detection(), ManualRevision: 2 }), false);
});

test('unavailable runtime and absent detection are data, but malformed accounting and mixed item identities are rejected', () => {
  const configuration = { Revision: '1', Profile: profile, Defaults: profile, UpdatedAt: stamp };
  assert.equal(validAnalysisOverview({ Configuration: configuration, Runtime: { Configured: false, IntroAvailable: false, PreviewAvailable: false, Reasons: ['missing_ffmpeg'], Cache: null } }), true);
  assert.equal(validAnalysisOverview({ Configuration: configuration, Runtime: { Configured: false, IntroAvailable: false, PreviewAvailable: false, Reasons: [], Cache: {} } }), false);
  assert.equal(validAnalysisItems({ Items: [item()], TotalRecordCount: 1, StartIndex: 0, Limit: 25 }, 0, 25), true);
  assert.equal(validAnalysisItems({ Items: [item(), item()], TotalRecordCount: 2, StartIndex: 0, Limit: 25 }, 0, 25), false);
  assert.equal(validAnalysisItems({ Items: [{ ...item(), Detection: { ...detection(), ItemId: 'another-item' } }], TotalRecordCount: 1, StartIndex: 0, Limit: 25 }, 0, 25), false);
  assert.equal(validAnalysisItems({ Items: [item()], TotalRecordCount: 1, StartIndex: 25, Limit: 25 }, 0, 25), false);
});

test('manual priority is preserved and stale or unsupported candidates cannot be accepted by the UI', () => {
  const value = item();
  value.Detection.Status = 'review';
  value.Detection.Effective = { StartTicks: 0, EndTicks: 100_000_000, Provenance: 'Manual' };
  value.Detection.Candidate = candidate();
  assert.equal(analysisCurrentCandidate(value), true);
  assert.equal(value.Detection.Effective.Provenance, 'Manual');
  assert.equal(analysisCurrentCandidate({ ...value, Type: 'Movie' }), false);
  assert.equal(analysisCurrentCandidate({ ...value, Detection: { ...value.Detection, Status: 'stale' } }), false);
  assert.equal(analysisCurrentCandidate({ ...value, SourceRevision: 'replaced' }), false);
  assert.equal(analysisCurrentCandidate({ ...value, Detection: { ...value.Detection, Candidate: null } }), false);
});

test('current candidates require finite visual anchor evidence while stale empty results remain readable', () => {
  const current = { ...detection(), Status: 'review', Candidate: candidate() };
  assert.equal(validAnalysisDetection(current), true);
  const incomplete: Partial<AnalysisMetrics> = { ...current.Candidate.Metrics };
  delete incomplete.VisualAnchorCount;
  assert.equal(validAnalysisDetection({ ...current, Candidate: { ...current.Candidate, Metrics: incomplete } }), false);
  assert.equal(validAnalysisDetection({ ...current, Candidate: { ...current.Candidate, Metrics: { ...current.Candidate.Metrics, VisualAnchorCount: Infinity } } }), false);
  assert.equal(validAnalysisDetection({ ...detection(), Status: 'stale', Reasons: ['algorithm_changed'] }), true);
});
