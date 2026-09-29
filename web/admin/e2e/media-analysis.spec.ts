import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Page, Request, Route } from '@playwright/test';
import type { TaskChild, TaskDefinition, TaskRun } from '../src/api';
import type { AnalysisItem, AnalysisOverview, AnalysisProfile, AnalysisRunInput } from '../src/mediaAnalysis';

// These browser checks use an explicit synthetic API. They cover the native
// workflow and request boundaries, not detector accuracy or media execution.
const stamp = '2026-09-21T00:00:00Z';
const csrf = 'synthetic-analysis-csrf';
const profile: AnalysisProfile = { AutoPublishIntros: true, PreviewIntervalSeconds: 10, PreviewQuality: 80, MaxSourceBytes: 128 * 2 ** 30, MaxItemRuntimeSeconds: 1200, FeatureCacheMaxBytes: 128 * 2 ** 20 };
const administrator = { Id: 'analysis-admin', Name: 'Analysis administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: stamp };
function overview(): AnalysisOverview {
  return { Configuration: { Revision: '9007199254740993', Profile: { ...profile }, Defaults: { ...profile }, UpdatedAt: stamp }, Runtime: {
    Configured: true, IntroAvailable: true, PreviewAvailable: true, Reasons: [],
    Cache: { ReadyEntries: 3, BuildingEntries: 1, PendingPublications: 1, Readers: 2, ReadyBytes: 4 * 2 ** 20, ReservedBytes: 2 ** 20, ControlBytes: 1024, TotalBytes: 5 * 2 ** 20 + 1024, MaxBytes: 128 * 2 ** 20 },
  } };
}
function item(): AnalysisItem {
  return { Id: 'episode-1', Name: 'Opening episode', Type: 'Episode', LibraryId: 'library-series', MediaSourceId: 'media-1', SourceRevision: 'source-current', Previews: [
    { Width: 320, Height: 180, Size: 32768, FrameCount: 12, Status: 'ready', FailureCode: '', UpdatedAt: stamp },
  ], Detection: { ItemId: 'episode-1', Revision: '9007199254740993', ManualRevision: '9007199254740994', SourceRevision: 'source-current', Status: 'qualified', Reasons: [], Suppressed: false, UpdatedAt: stamp,
    Effective: { StartTicks: 100_000_000, EndTicks: 400_000_000, Provenance: 'Detected' },
    Candidate: { Interval: { StartTicks: 100_000_000, EndTicks: 400_000_000 }, GroupID: 'comparison-group', Status: 'qualified', Reasons: [], Metrics: {
      AudioAgreementPermille: 920, AudioInformativePermille: 750, AudioSimilarityPermille: 930, AudioSamples: 100, AudioDistinct: 40,
      VisualAgreementPermille: 940, VisualSimilarityPermille: 950, VisualCoveragePermille: 900, VisualSamples: 40, VisualTransitions: 8,
      VisualChangeCoveragePermille: 500, VisualDominancePermille: 200, BoundaryUncertaintyTicks: 10_000_000, PairCount: 3,
      VisualAnchorCount: 24, VisualMinBandMatchedPermille: 600, VisualMatchedTimePermille: 875,
      VisualContradictedTimePermille: 50, VisualUnobservableTimePermille: 125, VisualMaxUnconfirmedGapTicks: 25_000_000,
      VisualStartAnchorGapTicks: 10_000_000, VisualEndAnchorGapTicks: 15_000_000, VisualDistinctStates: 6, VisualDominantStatePermille: 300,
    }, Support: [1, 2, 3].map((index) => ({ EpisodeKey: `episode-support-${index}`, SourceKey: `source-support-${index}`, ContentIdentity: `content-${index}`, Interval: { StartTicks: 100_000_000, EndTicks: 400_000_000 } })) },
  } };
}
function run(): TaskRun {
  return { Id: 'analysis-run-1', TaskId: 'analysis-task-1', State: 'running', Source: 'manual', RequestId: null, CreatedAt: stamp, StartedAt: stamp, FinishedAt: null,
    StopRequestedAt: null, StopReason: '', ErrorCode: '', ErrorMessage: '', TotalChildren: 3, TerminalChildren: 1, CompletedChildren: 1,
    FailedChildren: 0, CancelledChildren: 0, InterruptedChildren: 0, UnavailableChildren: 0, Scanned: 1, Added: 0, Updated: 1 };
}
async function json(route: Route, value: unknown, status = 200): Promise<void> { await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) }); }
interface Captured { method: string; path: string; body: unknown; csrf?: string }
class AnalysisAPI {
  overview = overview(); item = item(); run = run(); children: TaskChild[] = []; showTask = false; taskKind: 'intro' | 'previews' = 'intro'; requests: Captured[] = []; unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  writes(path?: string): Captured[] { return this.requests.filter((value) => value.method !== 'GET' && (!path || value.path === path)); }
  task(): TaskDefinition {
    const active = ['pending', 'running', 'stopping'].includes(this.run.State);
    return { Id: this.run.TaskId, Key: this.taskKind === 'intro' ? 'media.intro_analysis' : 'media.preview_generation', Name: this.taskKind === 'intro' ? 'Automatic intro detection' : 'Automatic seek previews', Description: 'Process enabled libraries in the background.', Category: 'Media analysis',
      IsHidden: false, Enabled: true, Revision: '1', ScheduleTimezone: 'UTC', CurrentRun: active ? this.run : null, LastRun: active ? null : this.run, NextRunAt: null,
      Triggers: [{ Id: 'intro-trigger', Kind: 'system_event', SystemEvent: this.taskKind === 'intro' ? 'IntroAnalysisRequested' : 'PreviewGenerationRequested', IntervalTicks: null, TimeOfDayTicks: null, DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: null, CalculationError: '' }],
    };
  }
  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url()); const path = url.pathname; const method = request.method();
      const captured = { method, path, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${method} ${path}`); if (handler) return handler(route, request);
      if (method === 'GET') {
        if (path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrf });
        if (path === '/admin/v1/libraries') return json(route, { Items: [{ Id: 'library-series', Name: 'Series library', CollectionType: 'tvshows', Paths: [], CreatedAt: stamp, LastScanAt: stamp }], TotalRecordCount: 1 });
        if (path === '/admin/v1/storage/roots') return json(route, { Configured: true, Items: [{ Path: '/synthetic/media', Available: true }] });
        if (path === '/admin/v1/media-analysis') return json(route, this.overview);
        if (path === '/admin/v1/media-analysis/items') return json(route, { Items: [this.item], TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex')), Limit: Number(url.searchParams.get('Limit')) });
        if (path === '/admin/v1/media-analysis/items/episode-1') return json(route, this.item);
        if (path === '/admin/v1/task-runs/analysis-run-1') {
          const start = Number(url.searchParams.get('StartIndex') ?? '0');
          const limit = Number(url.searchParams.get('Limit') ?? '50');
          return json(route, { Run: this.run, Children: { Items: this.children.slice(start, start + limit), TotalRecordCount: this.children.length, StartIndex: start, Limit: limit } });
        }
        if (path === '/admin/v1/tasks') return json(route, { Items: this.showTask ? [this.task()] : [], TotalRecordCount: this.showTask ? 1 : 0 });
        if (path === '/admin/v1/tasks/analysis-task-1') return json(route, { Task: this.task() });
      }
      if (method === 'PUT' && path === '/admin/v1/media-analysis/configuration') {
        const input = captured.body as { Revision: string; Profile: AnalysisProfile };
        if (input.Revision !== this.overview.Configuration.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Configuration changed.' } }, 409);
        this.overview.Configuration = { ...this.overview.Configuration, Revision: (BigInt(input.Revision) + 1n).toString(), Profile: input.Profile };
        return json(route, this.overview.Configuration);
      }
      if (method === 'POST' && path === '/admin/v1/media-analysis/runs') {
        const input = captured.body as AnalysisRunInput; this.run.RequestId = input.RequestId;
        return json(route, { RunId: this.run.Id, TaskId: this.run.TaskId, Admitted: true }, 202);
      }
      if (method === 'POST' && path === '/admin/v1/task-runs/analysis-run-1/cancel') { this.run.State = 'stopping'; this.run.StopRequestedAt = stamp; return json(route, { Run: this.run }); }
      if (method === 'POST' && path === '/admin/v1/media-analysis/cache/prune') {
        if ((captured.body as { Revision: string }).Revision !== this.overview.Configuration.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Configuration changed.' } }, 409);
        return json(route, { RemovedEntries: 1, RemovedBytes: 1024, RemainingBytes: 5 * 2 ** 20, BusyEntries: 2 });
      }
      this.unexpected.push(`${method} ${path}`); return json(route, { Error: { Code: 'unconfigured_synthetic_request', Message: 'No fixture response exists.' } }, 501);
    });
  }
}
const test = base.extend<{ api: AnalysisAPI }>({ api: async ({ context, page }, use) => {
  const api = new AnalysisAPI(); const errors: string[] = []; page.on('pageerror', (error) => errors.push(error.message)); await api.install(context); await use(api);
  expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); expect(api.writes().every((value) => value.csrf === csrf)).toBe(true);
} });
test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });
async function open(page: Page): Promise<void> { await page.goto('/admin/media-analysis'); await expect(page.getByRole('heading', { name: 'Media analysis', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'View analysis for Opening episode' })).toBeEnabled(); }

test('configuration conflicts preserve preview edits and publication stays automatic', async ({ page, api }) => {
  api.overview.Configuration.Profile.AutoPublishIntros = false;
  await open(page); await page.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true }).fill('1');
  await expect(page.getByRole('button', { name: 'Save analysis configuration' })).toBeDisabled(); expect(api.writes()).toHaveLength(0);
  await page.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true }).fill('20');
  await expect(page.getByRole('switch', { name: 'Automatically publish qualified detected intros' })).toHaveCount(0);
  api.overview.Configuration.Revision = '9007199254740995';
  await page.getByRole('button', { name: 'Save analysis configuration' }).click();
  await expect(page.getByRole('button', { name: 'Reload latest and keep draft' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Preview interval (seconds)', exact: true })).toHaveValue('20');
  await page.getByRole('button', { name: 'Reload latest and keep draft' }).click();
  await expect(page.getByText('Revision 9007199254740995.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Save analysis configuration' }).click();
  await expect(page.getByText('Analysis configuration saved. New runs use this profile.')).toBeVisible();
  const writes = api.writes('/admin/v1/media-analysis/configuration'); expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual({ Revision: '9007199254740995', Profile: { ...profile, PreviewIntervalSeconds: 20, AutoPublishIntros: true } });
});

test('seek previews have a library automation entry instead of manual generation controls', async ({ page, api }) => {
  await open(page);
  const automation = page.getByRole('region', { name: 'Automatic media processing', exact: true });
  const previews = page.getByRole('region', { name: 'Automatic seek previews', exact: true });
  await expect(previews).toContainText('Movies, TV shows, or Mixed media library');
  await expect(previews).toContainText('Turning the option off keeps existing valid previews.');
  await expect(page.getByRole('button', { name: /Build library previews|Build selected previews/ })).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: /Rebuild existing previews|Select Opening episode|Series library/ })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Previous run request', exact: true })).toHaveCount(0);
  await automation.getByRole('button', { name: 'Open library settings', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/libraries$/);
  expect(api.writes()).toHaveLength(0);
});
test('available intros show only the playback interval and preserve seek previews', async ({ page, api }) => {
  await open(page); await page.getByRole('button', { name: 'View analysis for Opening episode' }).click();
  const detail = page.getByRole('dialog', { name: 'Opening episode', exact: true });
  await expect(detail).toContainText('Intro available');
  await expect(detail).toContainText('0:10.00–0:40.00');
  await expect(detail.getByRole('table', { name: 'Preview outputs' })).toContainText('320 × 180');
  await expect(detail.getByRole('button', { name: /Accept|Reject|Reset/ })).toHaveCount(0);
  await expect(detail.getByText(/Supporting episodes|similarity|visual coverage|manual intro/i)).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});

test('unreliable and stale results leave playback alone while failures stay visible', async ({ page, api }) => {
  api.item.Detection.Status = 'review'; api.item.Detection.Effective = null;
  await open(page); await page.getByRole('button', { name: 'View analysis for Opening episode' }).click();
  const detail = page.getByRole('dialog', { name: 'Opening episode', exact: true });
  await expect(detail).toContainText('No intro detected');
  await expect(detail).toContainText('No intro is applied. Playback stays unchanged.');
  await expect(detail.getByText('0:10.00–0:40.00', { exact: true })).toHaveCount(0);
  api.item.SourceRevision = 'replaced-source'; api.item.Detection.SourceRevision = 'replaced-source'; api.item.Detection.Status = 'stale'; api.item.Detection.Candidate = null;
  await detail.getByRole('button', { name: 'Refresh result', exact: true }).click();
  await expect(detail).toContainText('Needs analysis');
  await expect(detail).toContainText('The previous analysis is no longer current.');
  api.item.Detection.Status = 'failed'; api.item.Detection.Reasons = ['source_unavailable'];
  await detail.getByRole('button', { name: 'Refresh result', exact: true }).click();
  await expect(detail).toContainText('Analysis failed');
  await expect(detail.getByRole('alert')).toContainText('source unavailable');
  await expect(detail.getByRole('button', { name: /Accept|Reject|Reset/ })).toHaveCount(0);
  expect(api.writes()).toHaveLength(0);
});
test('cache cleanup reports retained busy entries and uses the saved configuration CAS', async ({ page, api }) => {
  await open(page); const cache = page.getByRole('region', { name: 'Analysis cache', exact: true });
  await expect(cache).toContainText('Active readers: 2'); await cache.getByRole('button', { name: 'Prune analysis cache' }).click();
  await page.getByRole('dialog', { name: 'Prune the analysis cache?' }).getByRole('button', { name: 'Prune eligible entries' }).click();
  await expect(cache).toContainText('2 busy entries were retained'); expect(api.writes('/admin/v1/media-analysis/cache/prune')[0].body).toEqual({ Revision: '9007199254740993' });
});

test('missing tools disable work without hiding configuration or source results', async ({ page, api }) => {
  api.overview.Runtime = { Configured: false, IntroAvailable: false, PreviewAvailable: false, Reasons: ['missing_ffmpeg', 'fingerprint_tool_unavailable'], Cache: null };
  await open(page); await expect(page.getByText('missing ffmpeg', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Build library previews' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Open library settings', exact: true })).toBeEnabled();
  await expect(page.getByRole('textbox', { name: 'Preview image quality', exact: true })).toBeEnabled(); expect(api.writes()).toHaveLength(0);
});

test('a historical unconfirmed request survives reload and retries only its original receipt', async ({ page, api }) => {
  const pending: AnalysisRunInput = { Kind: 'previews', RequestId: '12345678-1234-4234-8234-123456789abc', LibraryIds: [], ItemIds: ['episode-1'], Force: false };
  await page.addInitScript(({ key, pending }) => { if (!sessionStorage.getItem('receipt-seeded')) { sessionStorage.setItem(key, JSON.stringify(pending)); sessionStorage.setItem('receipt-seeded', 'true'); } },
    { key: `goby.media-analysis.admission.${encodeURIComponent(administrator.Id)}`, pending });
  let attempts = 0;
  api.handlers.set('POST /admin/v1/media-analysis/runs', async (route, request) => {
    const input = request.postDataJSON() as AnalysisRunInput; api.run.RequestId = input.RequestId;
    if (attempts++ === 0) return route.abort('failed');
    return json(route, { RunId: api.run.Id, TaskId: api.run.TaskId, Admitted: false });
  });
  await open(page); await expect(page.getByRole('button', { name: 'Check run request' })).toBeVisible();
  expect(api.writes('/admin/v1/media-analysis/runs')).toHaveLength(0);
  await page.getByRole('button', { name: 'Check run request' }).click();
  await expect(page.getByRole('button', { name: 'Check run request' })).toBeEnabled();
  page.once('dialog', (dialog) => void dialog.accept());
  await page.reload(); await expect(page.getByRole('button', { name: 'Check run request' })).toBeEnabled(); expect(api.writes('/admin/v1/media-analysis/runs')).toHaveLength(1);
  await page.getByRole('button', { name: 'Check run request' }).click();
  await expect(page.getByRole('region', { name: 'Analysis task progress' })).toContainText('1 of 3 work items finished');
  const writes = api.writes('/admin/v1/media-analysis/runs'); expect(writes).toHaveLength(2); expect(writes[0].body).toEqual(pending); expect(writes[1].body).toEqual(pending);
  expect(writes.every((value) => (value.body as AnalysisRunInput).RequestId === pending.RequestId)).toBe(true);
});
test('intro analysis directs users to TV library settings without manual admission or review', async ({ page, api }) => {
  await open(page);
  await expect(page.getByRole('button', { name: 'Analyze library intros' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Analyze selected episodes' })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Automatically publish qualified detected intros' })).toHaveCount(0);
  const automatic = page.getByRole('region', { name: 'Automatic media processing', exact: true });
  await expect(automatic).toContainText('If no intro is found, playback stays unchanged.');
  await automatic.getByRole('button', { name: 'Open library settings', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/libraries$/);
  await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible();
  expect(api.writes()).toHaveLength(0);
});

test('automatic intro tasks expose the new event, progress, failures and stop control', async ({ page, api }) => {
  api.showTask = true; api.run.Source = 'system_event'; api.run.CompletedChildren = 0; api.run.FailedChildren = 1;
  api.children = [{ Id: 'intro-child-1', RunId: api.run.Id, LibraryId: 'library-series', LibraryName: 'Series library', Ordinal: 0, State: 'failed', ScanJobId: null,
    Scanned: 0, Added: 0, Updated: 0, ErrorCode: 'source_unavailable', ErrorMessage: 'An episode could not be read.', CreatedAt: stamp, StartedAt: stamp, FinishedAt: stamp }];
  for (const ordinal of [1, 2]) api.children.push({ Id: `intro-child-${ordinal + 1}`, RunId: api.run.Id, LibraryId: 'library-series', LibraryName: 'Series library',
    Ordinal: ordinal, State: ordinal === 1 ? 'running' : 'queued', ScanJobId: null, Scanned: 0, Added: 0, Updated: 0, ErrorCode: '', ErrorMessage: '',
    CreatedAt: stamp, StartedAt: ordinal === 1 ? stamp : null, FinishedAt: null });
  await open(page); await page.getByRole('button', { name: 'View background tasks', exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/tasks$/);
  await expect(page.getByText('When intro detection requested', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'View current run', exact: true }).click();
  const detail = page.getByRole('dialog', { name: 'Run details · Automatic intro detection', exact: true });
  await expect(detail).toContainText('1 of 3 work items finished');
  await expect(detail).toContainText('An episode could not be read.');
  await detail.getByRole('button', { name: 'Stop run', exact: true }).click();
  await page.getByRole('dialog', { name: 'Stop this run?', exact: true }).getByRole('button', { name: 'Stop run', exact: true }).click();
  await expect(detail).toContainText('Stopping the remaining work.');
  api.run.State = 'cancelled'; api.run.TerminalChildren = 3; api.run.CancelledChildren = 2; api.run.FinishedAt = stamp;
  api.children = api.children.map((child) => child.State === 'failed' ? child : { ...child, State: 'cancelled', FinishedAt: stamp });
  await detail.getByRole('button', { name: 'Refresh run', exact: true }).click();
  await expect(detail).toContainText('3 of 3 work items finished');
  await expect(detail.getByRole('button', { name: 'Stop run', exact: true })).toHaveCount(0);
  expect(api.writes('/admin/v1/media-analysis/runs')).toHaveLength(0);
  expect(api.writes('/admin/v1/task-runs/analysis-run-1/cancel')).toHaveLength(1);
});

test('automatic preview tasks expose the new event and generated outputs without manual admission', async ({ page, api }) => {
  api.showTask = true; api.taskKind = 'previews'; api.run.Source = 'system_event';
  api.run.TotalChildren = 1; api.run.TerminalChildren = 0; api.run.CompletedChildren = 0;
  api.item.Previews = [];
  api.children = [{ Id: 'preview-child-1', RunId: api.run.Id, LibraryId: 'library-series', LibraryName: 'Series library', Ordinal: 0, State: 'running', ScanJobId: null,
    Scanned: 0, Added: 0, Updated: 0, ErrorCode: '', ErrorMessage: '', CreatedAt: stamp, StartedAt: stamp, FinishedAt: null }];
  await open(page); await page.getByRole('button', { name: 'View background tasks', exact: true }).click();
  await expect(page.getByText('When seek preview generation requested', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'View current run', exact: true }).click();
  const detail = page.getByRole('dialog', { name: 'Run details · Automatic seek previews', exact: true });
  await expect(detail).toContainText('0 of 1 work items finished');
  api.run.State = 'completed'; api.run.TerminalChildren = 1; api.run.CompletedChildren = 1; api.run.FinishedAt = stamp;
  api.children[0] = { ...api.children[0], State: 'completed', FinishedAt: stamp };
  api.item.Previews = [{ Width: 320, Height: 180, Size: 32768, FrameCount: 12, Status: 'ready', FailureCode: '', UpdatedAt: stamp },
    { Width: 400, Height: 225, Size: 0, FrameCount: 0, Status: 'failed', FailureCode: 'source_unavailable', UpdatedAt: stamp }];
  await detail.getByRole('button', { name: 'Refresh run', exact: true }).click();
  await expect(detail).toContainText('1 of 1 work items finished');
  await expect(detail.getByRole('button', { name: 'Stop run', exact: true })).toHaveCount(0);
  await page.goto('/admin/media-analysis');
  await page.getByRole('button', { name: 'View analysis for Opening episode' }).click();
  const output = page.getByRole('dialog', { name: 'Opening episode', exact: true }).getByRole('table', { name: 'Preview outputs', exact: true });
  await expect(output).toContainText('320 × 180'); await expect(output).toContainText('ready');
  await expect(output).toContainText('source unavailable');
  expect(api.writes('/admin/v1/media-analysis/runs')).toHaveLength(0);
});
