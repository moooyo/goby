import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Request, Route } from '@playwright/test';
import type { MetadataDetail, MetadataValues } from '../src/api';
import type { MediaOperation, MediaOperationApplyRequest, MediaOperationCue, MediaOperationCueEdit, MediaOperationRequest, MediaProcessingStream, MediaProcessingTarget } from '../src/mediaOperationsApi';

// All administrator requests, including cue images and rejected writes, are synthetic.
// Unhandled requests never reach the server that serves the browser assets.
const timestamp = '2026-09-20T00:00:00Z';
const csrfToken = 'synthetic-media-processing-csrf';
const administrator = { Id: 'media-admin', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library = { Id: 'media-library', Name: 'Synthetic media library', CollectionType: 'movies', Paths: ['/synthetic/media'], CreatedAt: timestamp, LastScanAt: timestamp };
const itemId = 'media-movie';
const itemPath = `/admin/v1/items/${itemId}`;
const targetPath = `${itemPath}/media-processing`;
const preparePath = `${itemPath}/media-operations`;
const operationsPath = '/admin/v1/media-operations';
const resultHash = 'a'.repeat(64);
const editedResultHash = 'b'.repeat(64);
const cueImage = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
const operationId = (index: number) => index.toString(16).padStart(32, '0');
const operationPath = (id: string) => `${operationsPath}/${id}`;

function metadata(): MetadataDetail {
  const values: MetadataValues = {
    Name: 'Synthetic subtitle movie', SortName: '', Overview: '', OriginalTitle: '', OfficialRating: '', ProductionYear: 2026,
    PremiereDate: null, CommunityRating: null, ProviderIds: {}, Genres: [], Tags: [], Studios: [], People: [],
    IndexNumber: null, ParentIndexNumber: null, Album: '', Artists: [], AlbumArtists: [],
  };
  return {
    Item: { Id: itemId, LibraryId: library.Id, ParentId: '', ParentName: '', Name: values.Name, Type: 'Movie', Path: '/synthetic/media/movie.mkv', IsFolder: false },
    Revision: '1', Automatic: { ...values }, Effective: { ...values }, Overrides: {}, LockedValues: {},
    LockedFields: [], EditableFields: ['Name', 'Overview'], InactiveFields: [], LastEditedBy: '', LastEditedAt: null,
  };
}

function stream(Index: number, patch: Partial<MediaProcessingStream> = {}): MediaProcessingStream {
  return { Index, Codec: 'subrip', CodecType: 'subtitle', Language: 'en', Title: 'English text', IsDefault: false, IsForced: false, IsHearingImpaired: false, IsExternal: false, IsTextSubtitleStream: true, ...patch };
}

function target(): MediaProcessingTarget {
  return {
    ItemId: itemId, MediaSourceId: 'source-one', SourceRevision: 'source-revision-one', Container: 'mkv',
    Streams: [
      stream(0, { Codec: 'h264', CodecType: 'video', Language: '', Title: '', IsTextSubtitleStream: false }),
      stream(2),
      stream(5, { Codec: 'hdmv_pgs_subtitle', Title: 'English bitmap', IsTextSubtitleStream: false }),
      stream(8, { Title: 'External subtitle', IsExternal: true }),
    ],
    Capabilities: {
      Enabled: true, Available: true, UnavailableReason: '', MaxConcurrent: 1, MaxQueued: 8,
      WritableProfiles: ['matroska-v1', 'mp4-movtext-v1'],
      OCR: { Available: true, UnavailableReason: '', Models: [{ Id: 'eng', Language: 'eng' }, { Id: 'chi_sim', Language: 'chi_sim' }, { Id: 'chi_tra', Language: 'chi_tra' }], OutputFormats: ['srt', 'vtt'] },
    },
  };
}

function operation(index: number, patch: Partial<MediaOperation> = {}): MediaOperation {
  return {
    Id: operationId(index), Kind: 'remove_embedded_subtitle', State: 'ready', Revision: '5', ItemId: itemId, LibraryId: library.Id,
    MediaSourceId: 'source-one', SourceRevision: 'source-revision-one', StreamIndex: 5, Parameters: { Profile: 'matroska-v1' },
    Progress: { Stage: 'prepared', Processed: 1, Total: 1 },
    ResultSummary: { ContainerProfile: 'matroska-v1', RemovedStreamIndex: 5, PreservedStreamCount: 2, OriginalBytes: '1000000', CandidateBytes: '900000', BackupRetained: false, Models: [], Warnings: [], WarningCount: 0 },
    ResultHash: resultHash, PublicationPhase: 'none', CancelRequestedAt: null, CreatedAt: timestamp, UpdatedAt: timestamp,
    StartedAt: timestamp, FinishedAt: null, ErrorCode: '', ErrorMessage: '',
    CanCancel: true, CanReview: false, CanApply: true, CanRecover: false, Applied: false, TargetPresent: true, ...patch,
  };
}

function ocrOperation(index: number, count = 11, patch: Partial<MediaOperation> = {}): MediaOperation {
  return operation(index, {
    Kind: 'subtitle_ocr', Parameters: { ModelIds: ['eng'], OutputFormat: 'srt', Language: 'en', Title: 'Reviewed English', IsDefault: false, IsForced: false, IsHearingImpaired: false },
    ResultSummary: { CueCount: count, BackupRetained: false, Models: [{ ID: 'eng', SHA256: 'c'.repeat(64) }], Warnings: [], WarningCount: 0 },
    CanReview: true, ...patch,
  });
}

function cues(count: number): MediaOperationCue[] {
  return Array.from({ length: count }, (_, Ordinal) => ({
    Ordinal, OriginalStartTicks: String((Ordinal * 3 + 1) * 10_000_000), OriginalEndTicks: String((Ordinal * 3 + 2) * 10_000_000),
    OriginalText: `Original line ${Ordinal + 1}`, StartTicks: String((Ordinal * 3 + 1) * 10_000_000), EndTicks: String((Ordinal * 3 + 2) * 10_000_000),
    Text: `Recognized line ${Ordinal + 1}`, Included: true, Confidence: 91.5, Warnings: [], ImageSHA256: 'd'.repeat(64), IsForced: false, IsHearingImpaired: false,
  }));
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

interface CapturedRequest { method: string; path: string; query: string; body: unknown; csrf: string | undefined }

class MediaProcessingAPI {
  item = metadata();
  target = target();
  operations = new Map<string, MediaOperation>();
  pendingCompletions = new Map<string, MediaOperation>();
  reviews = new Map<string, MediaOperationCue[]>();
  admissions = new Map<string, MediaOperation>();
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  handlers = new Map<string, (route: Route, request: Request) => Promise<void>>();
  loseNextPreparationResponse = false;
  nextOperation = 1000;

  writes(path?: string): CapturedRequest[] { return this.requests.filter((request) => request.method !== 'GET' && (!path || request.path === path)); }
  reads(path: string): CapturedRequest[] { return this.requests.filter((request) => request.method === 'GET' && request.path === path); }
  add(value: MediaOperation, review: MediaOperationCue[] = []): MediaOperation {
    this.operations.set(value.Id, value); this.reviews.set(value.Id, review); return value;
  }

  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url());
      const captured: CapturedRequest = { method: request.method(), path: url.pathname, query: url.search, body: request.postData() ? request.postDataJSON() as unknown : null, csrf: request.headers()['x-csrf-token'] };
      this.requests.push(captured);
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler) return handler(route, request);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrfToken });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [library], TotalRecordCount: 1 });
        if (captured.path === `/admin/v1/libraries/${library.Id}/items`) return json(route, {
          Library: library, Items: [{ ...this.item.Item, ProductionYear: this.item.Effective.ProductionYear, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }],
          TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex')), Limit: Number(url.searchParams.get('Limit')),
        });
        if (captured.path === `${itemPath}/metadata`) return json(route, this.item);
        if (captured.path === targetPath) return json(route, this.target);
        if (captured.path === operationsPath) {
          const start = Number(url.searchParams.get('StartIndex')); const limit = Number(url.searchParams.get('Limit'));
          const items = [...this.operations.values()].filter((entry) => (!url.searchParams.get('Kind') || entry.Kind === url.searchParams.get('Kind')) && (!url.searchParams.get('State') || entry.State === url.searchParams.get('State')) && (!url.searchParams.get('ItemId') || entry.ItemId === url.searchParams.get('ItemId')));
          return json(route, { Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit });
        }
      }
      if (captured.method === 'POST' && captured.path === preparePath) {
        const input = captured.body as MediaOperationRequest;
        const existing = this.admissions.get(input.RequestId);
        if (existing) return json(route, { Operation: existing, Admitted: false }, 202);
        if (input.MediaSourceId !== this.target.MediaSourceId || input.SourceRevision !== this.target.SourceRevision) return json(route, { Error: { Code: 'source_changed', Message: 'The media source changed. Reload before preparing.' } }, 409);
        const created = this.add(operation(this.nextOperation++, {
          Kind: input.Kind, Revision: '1', State: 'queued', MediaSourceId: input.MediaSourceId, SourceRevision: input.SourceRevision, StreamIndex: input.StreamIndex, Parameters: input.Parameters,
          Progress: { Stage: '', Processed: 0, Total: 0 }, ResultSummary: { BackupRetained: false, Models: [], Warnings: [], WarningCount: 0 }, ResultHash: '', PublicationPhase: 'none', StartedAt: null, FinishedAt: null, CanApply: false,
        }));
        this.admissions.set(input.RequestId, created);
        if (this.loseNextPreparationResponse) { this.loseNextPreparationResponse = false; return route.abort('failed'); }
        return json(route, { Operation: created, Admitted: true }, 202);
      }
      const match = captured.path.match(/^\/admin\/v1\/media-operations\/([0-9a-f]{32})(?:\/(review|cancel|apply|recover|cues\/(\d+)\/image))?$/);
      const current = match ? this.operations.get(match[1]) : undefined;
      if (match && current) {
        const action = match[2];
        if (captured.method === 'GET' && !action) {
          const completed = this.pendingCompletions.get(current.Id);
          if (completed) { this.operations.set(current.Id, completed); this.pendingCompletions.delete(current.Id); }
          return json(route, { Operation: completed ?? current });
        }
        if (captured.method === 'GET' && action === 'review') {
          const start = Number(url.searchParams.get('StartIndex')); const limit = Number(url.searchParams.get('Limit')); const items = this.reviews.get(current.Id) ?? [];
          return json(route, { Operation: current, Items: items.slice(start, start + limit), TotalRecordCount: items.length, StartIndex: start, Limit: limit });
        }
        if (captured.method === 'GET' && match[3] !== undefined && this.reviews.get(current.Id)?.some((cue) => cue.Ordinal === Number(match[3]))) {
          return route.fulfill({ contentType: 'image/png', headers: { 'Cache-Control': 'no-store' }, body: cueImage });
        }
        if (captured.method === 'PUT' && action === 'review') {
          const input = captured.body as { Revision: string; Edits: MediaOperationCueEdit[] };
          if (input.Revision !== current.Revision) return json(route, { Error: { Code: 'media_operation_conflict', Message: 'The operation review changed. Reload the review.' } }, 409);
          this.reviews.set(current.Id, (this.reviews.get(current.Id) ?? []).map((cue) => ({ ...cue, ...input.Edits.find((edit) => edit.Ordinal === cue.Ordinal) })));
          const updated = { ...current, Revision: (BigInt(current.Revision) + 1n).toString(), ResultHash: editedResultHash };
          this.operations.set(current.Id, updated);
          return json(route, { Operation: updated });
        }
        if (captured.method === 'POST' && ['cancel', 'apply', 'recover'].includes(action ?? '')) {
          const input = captured.body as MediaOperationApplyRequest;
          if (input.Revision !== current.Revision || (action !== 'cancel' && (input.SourceRevision !== current.SourceRevision || input.ResultHash !== current.ResultHash))) return json(route, { Error: { Code: 'media_operation_conflict', Message: 'The operation changed. Reload before continuing.' } }, 409);
          const updated: MediaOperation = {
            ...current, Revision: (BigInt(current.Revision) + 1n).toString(), State: action === 'cancel' ? current.State : 'applying',
            CancelRequestedAt: action === 'cancel' ? timestamp : null, FinishedAt: null, CanCancel: false, CanReview: false, CanApply: false, CanRecover: false,
          };
          this.operations.set(current.Id, updated);
          // Admission only requests work. A later status read observes the worker's completion.
          this.pendingCompletions.set(current.Id, { ...updated, Revision: (BigInt(updated.Revision) + 1n).toString(), State: action === 'cancel' ? 'cancelled' : 'completed', PublicationPhase: action === 'cancel' ? current.PublicationPhase : 'done', Applied: action === 'cancel' ? current.Applied : true, FinishedAt: timestamp });
          return json(route, action === 'cancel' ? { Operation: updated } : { Operation: updated, Admitted: true }, 202);
        }
      }
      this.unexpected.push(`${captured.method} ${captured.path}${captured.query}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: MediaProcessingAPI }>({
  api: async ({ context }, use) => {
    const api = new MediaProcessingAPI();
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every administrator request must be explicitly mocked.').toEqual([]);
    expect(api.writes().every((request) => request.csrf === csrfToken), 'Every mutation must carry the administrator CSRF token.').toBe(true);
  },
});

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

async function choose(page: Page, parent: Locator, label: string, option: string): Promise<void> {
  await parent.getByRole('combobox', { name: label, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

async function openOperations(page: Page): Promise<Locator> {
  await page.addInitScript(() => window.history.replaceState({ ...window.history.state, tasksTab: 'media-processing' }, ''));
  const response = page.waitForResponse((entry) => entry.request().method() === 'GET' && new URL(entry.url()).pathname === operationsPath);
  await page.goto('/admin/tasks');
  expect((await response).status()).toBe(200);
  await expect(page.getByRole('tab', { name: 'Media processing', exact: true })).toHaveAttribute('aria-selected', 'true');
  const panel = page.getByRole('tabpanel', { name: 'Media processing', exact: true });
  await expect(panel.getByRole('button', { name: 'Refresh operations', exact: true })).toBeEnabled();
  return panel;
}

async function openOperation(page: Page, id: string): Promise<Locator> {
  const panel = await openOperations(page);
  await panel.getByRole('button', { name: `View media operation ${id}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Media operation', exact: true });
  await expect(dialog.getByText(`Operation ${id}`, { exact: true })).toBeVisible();
  return dialog;
}

async function openPreparation(page: Page): Promise<Locator> {
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: 'Edit metadata for Synthetic subtitle movie', exact: true }).click();
  await page.getByRole('dialog', { name: /^Edit metadata/ }).getByRole('button', { name: 'Media processing', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Prepare media processing', exact: true });
  await expect(dialog.getByRole('combobox', { name: 'Embedded subtitle stream', exact: true })).toBeEnabled();
  return dialog;
}

async function prepare(page: Page, dialog: Locator, status = 202): Promise<void> {
  const [response] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'POST' && new URL(entry.url()).pathname === preparePath),
    dialog.getByRole('button', { name: 'Prepare for review', exact: true }).click(),
  ]);
  expect(response.status()).toBe(status);
  if (status === 202) await expect(page.getByRole('dialog', { name: 'Media operation', exact: true }).getByText('Not published', { exact: true })).toBeVisible();
}

async function changeList(page: Page, panel: Locator, query: string, change: () => Promise<unknown>): Promise<void> {
  const [response] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'GET' && new URL(entry.url()).pathname === operationsPath && new URL(entry.url()).search === query),
    change(),
  ]);
  expect(response.status()).toBe(200);
  await expect(panel.getByRole('button', { name: 'Refresh operations', exact: true })).toBeEnabled();
}

test('operation filters and pagination send exact queries and retain the media processing tab', async ({ page, api }) => {
  for (let index = 1; index <= 56; index++) api.add(operation(index, { Kind: index % 2 === 0 ? 'subtitle_ocr' : 'remove_embedded_subtitle', State: index % 5 === 0 ? 'failed' : 'completed', CanCancel: false, CanApply: false, Applied: index % 5 !== 0, PublicationPhase: index % 5 === 0 ? 'none' : 'done' }));
  const panel = await openOperations(page);
  const rows = panel.getByRole('list', { name: 'Media processing operations', exact: true }).getByRole('listitem');
  await expect(rows).toHaveCount(25);
  expect(api.reads(operationsPath).at(-1)?.query).toBe('?StartIndex=0&Limit=25');
  await changeList(page, panel, '?StartIndex=25&Limit=25', () => panel.getByRole('button', { name: 'Go to next page', exact: true }).click());
  await expect(panel.getByRole('button', { name: `View media operation ${operationId(26)}`, exact: true })).toBeVisible();
  await changeList(page, panel, '?StartIndex=0&Limit=25&Kind=subtitle_ocr', () => choose(page, panel, 'Operation kind', 'Subtitle OCR'));
  await expect(panel.getByRole('button', { name: `View media operation ${operationId(2)}`, exact: true })).toBeVisible();
  await changeList(page, panel, '?StartIndex=0&Limit=25&Kind=subtitle_ocr&State=completed', () => choose(page, panel, 'Operation state', 'Published'));
  await expect(rows).toHaveCount(23);
  await changeList(page, panel, '?StartIndex=0&Limit=50&Kind=subtitle_ocr&State=completed', () => choose(page, panel, 'Operations per page', '50'));
  await changeList(page, panel, '?StartIndex=0&Limit=50', () => panel.getByRole('button', { name: 'Clear filters', exact: true }).click());
  await expect(rows).toHaveCount(50);
  await expect(panel.getByRole('combobox', { name: 'Operation kind', exact: true })).toHaveText('All kinds');
  await expect(panel.getByRole('combobox', { name: 'Operation state', exact: true })).toHaveText('All states');
  await changeList(page, panel, '?StartIndex=50&Limit=50', () => panel.getByRole('button', { name: 'Go to next page', exact: true }).click());
  await expect(rows).toHaveCount(6);
  await changeList(page, panel, '?StartIndex=50&Limit=50', () => panel.getByRole('button', { name: 'Refresh operations', exact: true }).click());
  expect(await page.evaluate(() => window.history.state?.tasksTab)).toBe('media-processing');
  expect(api.writes()).toEqual([]);
});

test('removal preparation targets the chosen embedded stream without applying it', async ({ page, api }) => {
  const dialog = await openPreparation(page);
  await dialog.getByRole('combobox', { name: 'Embedded subtitle stream', exact: true }).click();
  await expect(page.getByRole('option', { name: /External subtitle/ })).toHaveCount(0);
  await page.getByRole('option', { name: 'Stream 2 · subrip · en · English text', exact: true }).click();
  await choose(page, dialog, 'Writable container profile', 'Matroska');
  await prepare(page, dialog);
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
    method: 'POST', path: preparePath, body: { RequestId: expect.any(String), Kind: 'remove_embedded_subtitle', MediaSourceId: 'source-one', SourceRevision: 'source-revision-one', StreamIndex: 2, Parameters: { Profile: 'matroska-v1' } },
  }]);
  expect((api.writes()[0].body as MediaOperationRequest).RequestId).not.toBe('');
  await expect(page.getByRole('dialog', { name: 'Media operation', exact: true }).getByText('Preparation is queued. Review the result before applying it.', { exact: true })).toBeVisible();
  expect([...api.operations.values()][0].Applied).toBe(false);
});

test('OCR preparation sends selected models and subtitle properties for the bitmap stream', async ({ page, api }) => {
  const dialog = await openPreparation(page);
  await choose(page, dialog, 'Processing operation', 'Recognize bitmap subtitle (OCR)');
  await dialog.getByRole('combobox', { name: 'Embedded subtitle stream', exact: true }).click();
  await expect(page.getByRole('option', { name: /English text|External subtitle/ })).toHaveCount(0);
  await page.getByRole('option', { name: 'Stream 5 · hdmv_pgs_subtitle · en · English bitmap', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Prepare for review', exact: true })).toBeDisabled();
  await dialog.getByRole('checkbox', { name: 'English (eng)', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Simplified Chinese (chi_sim)', exact: true }).check();
  await choose(page, dialog, 'Subtitle output format', 'VTT');
  await dialog.getByRole('textbox', { name: 'Subtitle language', exact: true }).fill('zh');
  await dialog.getByRole('textbox', { name: 'Subtitle title', exact: true }).fill('Reviewed bilingual subtitles');
  await dialog.getByRole('checkbox', { name: 'Default subtitle', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Forced subtitle', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Hearing impaired subtitle', exact: true }).check();
  await prepare(page, dialog);
  expect(api.writes().map((request) => request.body)).toEqual([{
    RequestId: expect.any(String), Kind: 'subtitle_ocr', MediaSourceId: 'source-one', SourceRevision: 'source-revision-one', StreamIndex: 5,
    Parameters: { ModelIds: ['eng', 'chi_sim'], OutputFormat: 'vtt', Language: 'zh', Title: 'Reviewed bilingual subtitles', IsDefault: true, IsForced: true, IsHearingImpaired: true },
  }]);
  expect(api.writes().every((request) => request.path === preparePath)).toBe(true);
});

test('a source conflict preserves setup and requires the current source before preparing again', async ({ page, api }) => {
  const dialog = await openPreparation(page);
  await choose(page, dialog, 'Embedded subtitle stream', 'Stream 2 · subrip · en · English text');
  api.target = { ...api.target, MediaSourceId: 'source-two', SourceRevision: 'source-revision-two' };
  await prepare(page, dialog, 409);
  await expect(dialog.getByRole('alert').filter({ hasText: 'The source or operation state changed.' })).toBeVisible();
  await expect(dialog.getByRole('combobox', { name: 'Embedded subtitle stream', exact: true })).toHaveText('Stream 2 · subrip · en · English text');
  await expect(dialog.getByRole('button', { name: 'Prepare for review', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Reload source', exact: true }).last()).toBeEnabled();
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload source', exact: true }).last().click();
  await expect(dialog.getByText('Current source: source-two · Container: mkv', { exact: true })).toBeVisible();
  await choose(page, dialog, 'Embedded subtitle stream', 'Stream 5 · hdmv_pgs_subtitle · en · English bitmap');
  await prepare(page, dialog);
  expect(api.writes(preparePath).map(({ body }) => body)).toEqual([
    { RequestId: expect.any(String), Kind: 'remove_embedded_subtitle', MediaSourceId: 'source-one', SourceRevision: 'source-revision-one', StreamIndex: 2, Parameters: { Profile: 'matroska-v1' } },
    { RequestId: expect.any(String), Kind: 'remove_embedded_subtitle', MediaSourceId: 'source-two', SourceRevision: 'source-revision-two', StreamIndex: 5, Parameters: { Profile: 'matroska-v1' } },
  ]);
});

test('a lost preparation response retries the same request without admitting a duplicate', async ({ page, api }) => {
  api.loseNextPreparationResponse = true;
  const dialog = await openPreparation(page);
  await choose(page, dialog, 'Embedded subtitle stream', 'Stream 2 · subrip · en · English text');
  await dialog.getByRole('button', { name: 'Prepare for review', exact: true }).click();
  await expect(dialog.getByRole('alert').filter({ hasText: 'The preparation response could not be confirmed.' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Prepare for review', exact: true })).toBeDisabled();
  const [response] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'POST' && new URL(entry.url()).pathname === preparePath),
    dialog.getByRole('button', { name: 'Retry same request', exact: true }).click(),
  ]);
  expect(response.status()).toBe(202);
  await expect(page.getByRole('dialog', { name: 'Media operation', exact: true }).getByText('Not published', { exact: true })).toBeVisible();
  const writes = api.writes(preparePath);
  expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual(writes[0].body);
  expect(api.operations.size).toBe(1);
});

test('OCR review saves only edited cues, paginates images, and guards dirty task navigation', async ({ page, api }) => {
  const current = api.add(ocrOperation(1), cues(11));
  const dialog = await openOperation(page, current.Id);
  const review = dialog.getByRole('region', { name: 'OCR cue review', exact: true });
  const first = review.getByRole('region', { name: 'Cue 1', exact: true });
  await expect(first.getByRole('textbox', { name: 'Cue 1 text', exact: true })).toBeEnabled();
  const image = first.getByRole('img', { name: 'Original bitmap subtitle for cue 1', exact: true });
  await image.scrollIntoViewIfNeeded();
  await expect(image).toBeVisible();
  await expect(image).toHaveAttribute('src', `${operationPath(current.Id)}/cues/0/image`);
  await expect.poll(() => api.reads(`${operationPath(current.Id)}/cues/0/image`).length).toBeGreaterThan(0);
  await first.getByRole('textbox', { name: 'Cue 1 start (seconds)', exact: true }).fill('1.25');
  await first.getByRole('textbox', { name: 'Cue 1 end (seconds)', exact: true }).fill('2.75');
  await first.getByRole('textbox', { name: 'Cue 1 text', exact: true }).fill('Corrected first line');
  await review.getByRole('checkbox', { name: 'Include cue 2', exact: true }).uncheck();
  await expect(dialog.getByRole('button', { name: 'Apply reviewed result', exact: true })).toBeDisabled();

  const prompts: string[] = [];
  page.on('dialog', (prompt) => { prompts.push(prompt.message()); void prompt.dismiss(); });
  // Modal dialogs hide background tabs from accessibility. Dispatch only exercises
  // TasksPage's internal tab guard without pretending the background is clickable.
  await page.getByRole('tab', { name: 'Media processing', exact: true, includeHidden: true }).dispatchEvent('click');
  expect(prompts).toEqual([]);
  await page.getByRole('tab', { name: 'Scan history', exact: true, includeHidden: true }).dispatchEvent('click');
  expect(prompts).toEqual(['Discard unsaved OCR cue changes and leave this operation?']);
  expect(await page.evaluate(() => window.history.state?.tasksTab)).toBe('media-processing');
  await expect(first.getByRole('textbox', { name: 'Cue 1 text', exact: true })).toHaveValue('Corrected first line');

  const [response] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'PUT' && new URL(entry.url()).pathname === `${operationPath(current.Id)}/review`),
    review.getByRole('button', { name: 'Save cue changes', exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  await expect(review.getByRole('alert').filter({ hasText: 'Cue changes saved.' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Apply reviewed result', exact: true })).toBeEnabled();
  expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
    method: 'PUT', path: `${operationPath(current.Id)}/review`, body: { Revision: '5', Edits: [
      { Ordinal: 0, StartTicks: '12500000', EndTicks: '27500000', Text: 'Corrected first line', Included: true },
      { Ordinal: 1, StartTicks: '40000000', EndTicks: '50000000', Text: 'Recognized line 2', Included: false },
    ] },
  }]);
  await review.getByRole('button', { name: 'Go to next page', exact: true }).click();
  await expect(review.getByRole('textbox', { name: 'Cue 11 text', exact: true })).toHaveValue('Recognized line 11');
  expect(api.reads(`${operationPath(current.Id)}/review`).at(-1)?.query).toBe('?StartIndex=10&Limit=10');
  const lastImage = review.getByRole('img', { name: 'Original bitmap subtitle for cue 11', exact: true });
  await lastImage.scrollIntoViewIfNeeded();
  await expect(lastImage).toBeVisible();
  await expect.poll(() => api.reads(`${operationPath(current.Id)}/cues/10/image`).length).toBeGreaterThan(0);
  expect(api.operations.get(current.Id)?.ResultHash).toBe(editedResultHash);
});

test('review revision conflicts keep the draft and block applying until an explicit reload', async ({ page, api }) => {
  const current = api.add(ocrOperation(1, 1), cues(1));
  const dialog = await openOperation(page, current.Id);
  const review = dialog.getByRole('region', { name: 'OCR cue review', exact: true });
  const text = review.getByRole('textbox', { name: 'Cue 1 text', exact: true });
  await expect(text).toBeEnabled();
  await text.fill('Preserve this unsaved correction');
  api.operations.set(current.Id, { ...current, Revision: '6', ResultHash: editedResultHash });
  api.reviews.set(current.Id, [{ ...cues(1)[0], Text: 'Changed by another reviewer' }]);
  const [response] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'PUT' && new URL(entry.url()).pathname === `${operationPath(current.Id)}/review`),
    review.getByRole('button', { name: 'Save cue changes', exact: true }).click(),
  ]);
  expect(response.status()).toBe(409);
  await expect(review.getByRole('alert').filter({ hasText: 'Your draft is kept.' })).toBeVisible();
  await expect(review.getByRole('button', { name: 'Reload review', exact: true })).toBeEnabled();
  await expect(text).toHaveValue('Preserve this unsaved correction');
  await expect(text).toBeDisabled();
  await expect(review.getByRole('button', { name: 'Save cue changes', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Apply reviewed result', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  page.once('dialog', (prompt) => { void prompt.dismiss(); });
  await review.getByRole('button', { name: 'Reload review', exact: true }).click();
  await expect(text).toHaveValue('Preserve this unsaved correction');
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await review.getByRole('button', { name: 'Reload review', exact: true }).click();
  await expect(text).toHaveValue('Changed by another reviewer');
  await expect(text).toBeEnabled();
  await expect(dialog.getByRole('button', { name: 'Apply reviewed result', exact: true })).toBeEnabled();
  expect(api.writes()).toHaveLength(1);
});

test('a background revision change requires reloading cues and invalidates an open apply confirmation', async ({ page, api }) => {
  const current = api.add(ocrOperation(1, 1), cues(1));
  const dialog = await openOperation(page, current.Id);
  const review = dialog.getByRole('region', { name: 'OCR cue review', exact: true });
  const text = review.getByRole('textbox', { name: 'Cue 1 text', exact: true });
  const apply = dialog.getByRole('button', { name: 'Apply reviewed result', exact: true });
  await expect(text).toHaveValue('Recognized line 1');
  await expect(apply).toBeEnabled();
  await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'false');
  api.operations.set(current.Id, { ...current, Revision: '6', ResultHash: editedResultHash });
  api.reviews.set(current.Id, [{ ...cues(1)[0], Text: 'New recognition from another reviewer' }]);
  // Exercise the visibility refresh path, which reloads the operation without
  // explicitly discarding or reloading the currently displayed cue page.
  const [refresh] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'GET' && new URL(entry.url()).pathname === operationPath(current.Id)),
    page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))),
  ]);
  expect(refresh.status()).toBe(200);
  await expect(review.getByRole('alert').filter({ hasText: 'This operation changed after these cues were loaded.' })).toBeVisible();
  await expect(text).toHaveValue('Recognized line 1');
  await expect(apply).toBeDisabled();
  await review.getByRole('button', { name: 'Reload review', exact: true }).click();
  await expect(text).toHaveValue('New recognition from another reviewer');
  await expect(apply).toBeEnabled();
  await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'false');
  await apply.click();
  const confirmation = page.getByRole('dialog', { name: 'Apply reviewed result?', exact: true });
  await expect(confirmation.getByRole('button', { name: 'Confirm apply', exact: true })).toBeEnabled();
  api.operations.set(current.Id, { ...current, Revision: '7', ResultHash: 'e'.repeat(64) });
  const [confirmationRefresh] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'GET' && new URL(entry.url()).pathname === operationPath(current.Id)),
    page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))),
  ]);
  expect(confirmationRefresh.status()).toBe(200);
  await expect(confirmation.getByRole('alert').filter({ hasText: 'The operation changed while this confirmation was open.' })).toBeVisible();
  await expect(confirmation.getByRole('button', { name: 'Confirm apply', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
});

for (const action of [
  { kind: 'apply', button: 'Apply reviewed result', title: 'Apply reviewed result?', confirm: 'Confirm apply', notice: 'Apply requested. Follow the final publication status.' },
  { kind: 'cancel', button: 'Cancel operation', title: 'Cancel media operation?', confirm: 'Confirm cancellation', notice: 'Cancellation requested. Follow the final operation status.' },
  { kind: 'recover', button: 'Recover publication', title: 'Recover publication?', confirm: 'Confirm recovery', notice: 'Recovery requested. Follow the final publication status.' },
] as const) {
  test(`${action.kind} requires explicit confirmation and sends the displayed operation revision`, async ({ page, api }) => {
    const current = api.add(operation(1, action.kind === 'cancel'
      ? { State: 'running', CanApply: false, ResultHash: '', PublicationPhase: 'none', FinishedAt: null, Progress: { Stage: 'preparing', Processed: 1, Total: 2 } }
      : action.kind === 'recover' ? { State: 'recovery_required', CanCancel: false, CanApply: false, CanRecover: true, Applied: true, PublicationPhase: 'catalog_committed' } : {}));
    const dialog = await openOperation(page, current.Id);
    await expect(dialog.getByText(action.kind === 'recover' ? 'Published to catalog' : 'Not published', { exact: true })).toBeVisible();
    await dialog.getByRole('button', { name: action.button, exact: true }).click();
    const confirmation = page.getByRole('dialog', { name: action.title, exact: true });
    await expect(confirmation).toBeVisible();
    expect(api.writes()).toEqual([]);
    await confirmation.getByRole('button', { name: 'Keep reviewing', exact: true }).click();
    await expect(confirmation).not.toBeVisible();
    expect(api.writes()).toEqual([]);
    await dialog.getByRole('button', { name: action.button, exact: true }).click();
    const [response] = await Promise.all([
      page.waitForResponse((entry) => entry.request().method() === 'POST' && new URL(entry.url()).pathname === `${operationPath(current.Id)}/${action.kind}`),
      confirmation.getByRole('button', { name: action.confirm, exact: true }).click(),
    ]);
    expect(response.status()).toBe(202);
    await expect(confirmation).not.toBeVisible();
    await expect(dialog.getByRole('alert').filter({ hasText: action.notice })).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Refresh operation', exact: true })).toBeEnabled();
    expect(api.writes().map(({ method, path, body }) => ({ method, path, body }))).toEqual([{
      method: 'POST', path: `${operationPath(current.Id)}/${action.kind}`,
      body: action.kind === 'cancel' ? { Revision: '5' } : { Revision: '5', SourceRevision: 'source-revision-one', ResultHash: resultHash, RequestId: expect.any(String) },
    }]);
    await expect(dialog.getByRole('alert').filter({ hasText: action.kind === 'cancel' ? 'The operation was cancelled.' : 'The selected embedded subtitle was removed and the replacement media was published.' })).toBeVisible();
    expect(api.operations.get(current.Id)?.State).toBe(action.kind === 'cancel' ? 'cancelled' : 'completed');
    if (action.kind === 'cancel') {
      expect(api.operations.get(current.Id)?.CancelRequestedAt).toBe(timestamp);
      await expect(dialog.getByText('Cancelled', { exact: true }).first()).toBeVisible();
      await expect(dialog.getByText('Cancellation requested', { exact: true })).toHaveCount(0);
    }
    if (action.kind !== 'cancel') await expect(dialog.getByText('Published to catalog', { exact: true })).toBeVisible();
  });
}

test('reloading after an apply conflict blocks another apply until the new detail request completes', async ({ page, api }) => {
  const current = api.add(operation(1));
  const detailPath = operationPath(current.Id);
  const applyPath = `${detailPath}/apply`;
  const dialog = await openOperation(page, current.Id);
  const apply = dialog.getByRole('button', { name: 'Apply reviewed result', exact: true });
  await expect(apply).toBeEnabled();
  await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'false');
  await apply.click();
  const confirmation = page.getByRole('dialog', { name: 'Apply reviewed result?', exact: true });
  api.operations.set(current.Id, { ...current, Revision: '6', ResultHash: editedResultHash });
  const automaticRefresh = page.waitForResponse((entry) => entry.request().method() === 'GET' && new URL(entry.url()).pathname === detailPath);
  const [conflict] = await Promise.all([
    page.waitForResponse((entry) => entry.request().method() === 'POST' && new URL(entry.url()).pathname === applyPath),
    confirmation.getByRole('button', { name: 'Confirm apply', exact: true }).click(),
  ]);
  expect(conflict.status()).toBe(409);
  expect((await automaticRefresh).status()).toBe(200);
  await expect(confirmation).not.toBeVisible();
  await expect(dialog.getByRole('alert').filter({ hasText: 'The operation changed or the request could not be confirmed.' })).toBeVisible();
  await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'false');

  let release!: () => void;
  let deferredReadStarted = false;
  let deferredReadCompleted = false;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  const handlerKey = `GET ${detailPath}`;
  api.handlers.set(handlerKey, async (route) => {
    deferredReadStarted = true;
    await gate;
    await json(route, { Operation: api.operations.get(current.Id) });
    deferredReadCompleted = true;
  });
  try {
    await dialog.getByRole('button', { name: 'Reload operation', exact: true }).click();
    await expect.poll(() => deferredReadStarted).toBe(true);
    await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'true');
    await expect(apply).toBeDisabled();
    expect(api.writes(applyPath)).toHaveLength(1);

    release();
    await expect.poll(() => deferredReadCompleted).toBe(true);
    await expect(dialog.locator('[aria-busy]')).toHaveAttribute('aria-busy', 'false');
    await expect(apply).toBeEnabled();
    api.handlers.delete(handlerKey);
    await apply.click();
    await expect(confirmation.getByRole('button', { name: 'Confirm apply', exact: true })).toBeEnabled();
    const [accepted] = await Promise.all([
      page.waitForResponse((entry) => entry.request().method() === 'POST' && new URL(entry.url()).pathname === applyPath),
      confirmation.getByRole('button', { name: 'Confirm apply', exact: true }).click(),
    ]);
    expect(accepted.status()).toBe(202);
    await expect(confirmation).not.toBeVisible();
    await expect(dialog.getByText('Published to catalog', { exact: true })).toBeVisible();
    expect(api.writes(applyPath).map((request) => request.body)).toEqual([
      { Revision: '5', SourceRevision: 'source-revision-one', ResultHash: resultHash, RequestId: expect.any(String) },
      { Revision: '6', SourceRevision: 'source-revision-one', ResultHash: editedResultHash, RequestId: expect.any(String) },
    ]);
  } finally {
    release();
    api.handlers.delete(handlerKey);
  }
});
