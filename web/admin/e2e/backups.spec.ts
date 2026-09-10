import { expect, test as base, type BrowserContext, type Page, type Request, type Route } from '@playwright/test';

const timestamp = '2026-09-09T10:00:00Z';
const backupId = '11111111111111111111111111111111';
const operationId = '22222222222222222222222222222222';
const requestId = '33333333333333333333333333333333';
const csrfToken = 'backup-browser-csrf-token';
const digest = 'a'.repeat(64);
const generation = '9007199254740995';
const revision = '9007199254740993';
const rollbackGeneration = '88888888888888888888888888888888';

type Source = {
  ServerId: string; ServerName: string; GobyVersion: string; SchemaVersion: string;
  CreatedAt: string; Tables: { Name: string; Rows: string }[];
};
type Backup = {
  Id: string; Kind: string; State: string; CreatedAt: string; UpdatedAt: string;
  SizeBytes: string; SHA256: string; Verified: boolean; ErrorCode: string; Source: Source | null;
};
type Operation = {
  Id: string; RequestId: string; Revision: string; Kind: string; State: string; Phase: string;
  BackupId: string; CreatedAt: string; UpdatedAt: string; ErrorCode: string; Source: Source | null;
  RestoreDefaults: boolean; ReplaceRollback: boolean; CanCancel: boolean; CanApply: boolean;
  GenerationRevision: string;
};
type Status = {
  Available: boolean; UnavailableReason: string; RestoreAvailable: boolean; RestoreUnavailableReason: string;
  Busy: boolean; ActiveOperationId: string; GenerationRevision: string;
  Limits: { MaxBackupBytes: string; MaxStoredBytes: string; MaxBackups: number; MinPassphraseBytes: number; MaxPassphraseBytes: number };
  Storage: { Bytes: string; Objects: number };
  Rollback: { Available: boolean; MustReplace: boolean; CreatedAt: string | null; ServerName: string; Generation: string; UnavailableReason: string };
};
type CapturedRequest = {
  method: string; path: string; url: string; headers: Record<string, string>; body: Buffer | null;
  json: Record<string, unknown> | undefined;
};
type MockHandler = (route: Route, request: Request, captured: CapturedRequest) => Promise<void>;

function source(overrides: Partial<Source> = {}): Source {
  return {
    ServerId: '44444444444444444444444444444444', ServerName: 'Archive server', GobyVersion: '0.1.0',
    SchemaVersion: '42', CreatedAt: timestamp, Tables: [{ Name: 'users', Rows: '9007199254740997' }], ...overrides,
  };
}

function backup(overrides: Partial<Backup> = {}): Backup {
  return {
    Id: backupId, Kind: 'generated', State: 'ready', CreatedAt: timestamp, UpdatedAt: timestamp,
    SizeBytes: '1024', SHA256: digest, Verified: true, ErrorCode: '', Source: source(), ...overrides,
  };
}

function operation(overrides: Partial<Operation> = {}): Operation {
  return {
    Id: operationId, RequestId: requestId, Revision: revision, Kind: 'create', State: 'running', Phase: 'encryption',
    BackupId: backupId, CreatedAt: timestamp, UpdatedAt: timestamp, ErrorCode: '', Source: null,
    RestoreDefaults: false, ReplaceRollback: false, CanCancel: true, CanApply: false,
    GenerationRevision: generation, ...overrides,
  };
}

function status(overrides: Partial<Status> = {}): Status {
  return {
    Available: true, UnavailableReason: '', RestoreAvailable: true, RestoreUnavailableReason: '',
    Busy: false, ActiveOperationId: '', GenerationRevision: generation,
    Limits: { MaxBackupBytes: '1073741824', MaxStoredBytes: '10737418240', MaxBackups: 10, MinPassphraseBytes: 12, MaxPassphraseBytes: 1024 },
    Storage: { Bytes: '0', Objects: 0 },
    Rollback: { Available: false, MustReplace: false, CreatedAt: null, ServerName: '', Generation: '', UnavailableReason: '' },
    ...overrides,
  };
}

async function json(route: Route, body: unknown, responseStatus = 200): Promise<void> {
  await route.fulfill({ status: responseStatus, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(body) });
}

async function error(route: Route, code: string, message: string, responseStatus: number): Promise<void> {
  await json(route, { Error: { Code: code, Message: message }, RequestId: 'mock-http-request' }, responseStatus);
}

class BackupMock {
  status: unknown = status();
  backups: unknown[] = [];
  operations: unknown[] = [];
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];
  private handlers = new Map<string, MockHandler>();

  on(method: string, path: string, handler: MockHandler): void {
    this.handlers.set(`${method} ${path}`, handler);
  }

  mutations(path?: string): CapturedRequest[] {
    return this.requests.filter((request) => !['GET', 'HEAD'].includes(request.method) && (!path || request.path === path));
  }

  async install(context: BrowserContext): Promise<void> {
    // Every administrator API request is intercepted, including unexpected mutations.
    // No recovery operation in this suite can reach the server behind baseURL.
    await context.route('**/admin/v1/**', async (route, request) => {
      const url = new URL(request.url());
      const body = request.postDataBuffer();
      let payload: Record<string, unknown> | undefined;
      if (request.headers()['content-type']?.includes('application/json') && body) {
        payload = JSON.parse(body.toString('utf8')) as Record<string, unknown>;
      }
      const captured = { method: request.method(), path: url.pathname, url: request.url(), headers: request.headers(), body, json: payload };
      this.requests.push(captured);
      const paginated = captured.method === 'GET' && ['/admin/v1/backups', '/admin/v1/backup-operations'].includes(captured.path);
      const invalidQuery = url.search && (!paginated || [...url.searchParams].some(([name, value]) => {
        const number = Number(value);
        return !['StartIndex', 'Limit'].includes(name) || url.searchParams.getAll(name).length !== 1
          || !/^(0|[1-9][0-9]*)$/.test(value) || !Number.isSafeInteger(number) || number > 2147483647
          || (name === 'Limit' && (number < 1 || number > 100));
      }));
      if (invalidQuery) {
        this.unexpected.push(`${captured.method} ${captured.path}${url.search}`);
        return error(route, 'invalid_query', 'This route received unsupported query parameters.', 400);
      }
      const handler = this.handlers.get(`${captured.method} ${captured.path}`);
      if (handler) return handler(route, request, captured);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, {
          User: { Id: '55555555555555555555555555555555', Name: 'Browser administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp },
          CSRFToken: csrfToken,
        });
        if (captured.path === '/admin/v1/backups/status') return json(route, this.status);
        if (captured.path === '/admin/v1/backups' || captured.path === '/admin/v1/backup-operations') {
          const items = captured.path === '/admin/v1/backups' ? this.backups : this.operations;
          const startIndex = Number(url.searchParams.get('StartIndex') ?? 0);
          const limit = Number(url.searchParams.get('Limit') ?? 25);
          return json(route, { Items: items.slice(startIndex, startIndex + limit), TotalRecordCount: items.length, StartIndex: startIndex, Limit: limit });
        }
        if (captured.path.startsWith('/admin/v1/backup-operations/')) {
          const item = this.operations.find((value) => (value as Operation).Id === captured.path.split('/').at(-1));
          if (item) return json(route, { Operation: item });
        }
        if (captured.path.startsWith('/admin/v1/backups/')) {
          const item = this.backups.find((value) => (value as Backup).Id === captured.path.split('/').at(-1));
          if (item) return json(route, { Backup: item });
        }
      }
      this.unexpected.push(`${captured.method} ${captured.path}`);
      return error(route, 'unexpected_mock_request', 'This API request was not configured by the browser test.', 501);
    });
  }
}

const test = base.extend<{ api: BackupMock }>({
  api: async ({ context }, use) => {
    const api = new BackupMock();
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'All administrator API traffic must be handled by the mock.').toEqual([]);
  },
});

test.use({ serviceWorkers: 'block' });

async function openBackups(page: Page): Promise<void> {
  await page.goto('/admin/backups');
  await expect(page.getByRole('heading', { name: 'Backups', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create backup', exact: true })).toBeVisible();
}

function backupCard(page: Page, id = backupId) {
  return page.getByTestId(`backup-${id}`);
}

function jobCard(page: Page, id = operationId) {
  return page.getByTestId(`operation-${id}`);
}

function gate(): { reached: Promise<void>; release: () => void } {
  let release!: () => void;
  const reached = new Promise<void>((resolve) => { release = resolve; });
  return { reached, release };
}

function unadmittedCreate(api: BackupMock, latestBusy: boolean) {
  const tailRequested = gate();
  const releaseTail = gate();
  const latestStatusRequested = gate();
  const releaseLatestStatus = gate();
  const submittedIds: string[] = [];
  const reconciliation: string[] = [];
  let responseLost = false;
  let enumerationComplete = false;
  api.operations = Array.from({ length: 101 }, (_, index) => operation({
    Id: (index + 100).toString(16).padStart(32, '0'), RequestId: (index + 200).toString(16).padStart(32, '0'),
    State: 'completed', Phase: 'finished', CanCancel: false,
  }));
  api.on('GET', '/admin/v1/backup-operations', async (route, request) => {
    const query = new URL(request.url()).searchParams;
    const start = Number(query.get('StartIndex') ?? 0);
    const limit = Number(query.get('Limit') ?? 25);
    if (responseLost && start === 100) {
      reconciliation.push('last page requested');
      tailRequested.release();
      await releaseTail.reached;
      enumerationComplete = true;
      reconciliation.push('last page returned');
    }
    await json(route, { Items: api.operations.slice(start, start + limit), TotalRecordCount: api.operations.length, StartIndex: start, Limit: limit });
  });
  api.on('GET', '/admin/v1/backups/status', async (route) => {
    if (responseLost && enumerationComplete) {
      reconciliation.push('latest status requested');
      latestStatusRequested.release();
      await releaseLatestStatus.reached;
      reconciliation.push('latest status returned');
      await json(route, status({ Busy: latestBusy }));
    } else await json(route, status());
  });
  api.on('POST', '/admin/v1/backups', async (route, _request, captured) => {
    submittedIds.push(captured.json!.RequestId as string);
    if (submittedIds.length === 1) {
      responseLost = true;
      await route.abort('connectionreset');
      return;
    }
    const admitted = operation({ RequestId: captured.json!.RequestId as string, State: 'pending', Phase: 'admission' });
    api.operations = [admitted];
    await json(route, { Operation: admitted }, 202);
  });
  return { tailRequested, releaseTail, latestStatusRequested, releaseLatestStatus, submittedIds, reconciliation };
}

function assertJSONMutation(request: CapturedRequest): void {
  expect(request.headers['x-csrf-token']).toBe(csrfToken);
  expect(request.headers['content-type']).toContain('application/json');
  expect(request.headers.accept).toContain('application/json');
}

test('create preserves the exact passphrase, clears it, and treats 202 as admission', async ({ page, api }) => {
  api.on('POST', '/admin/v1/backups', async (route, _request, captured) => {
    const admitted = operation({ State: 'pending', Phase: 'admission', RequestId: captured.json!.RequestId as string });
    api.operations = [admitted];
    api.backups = [backup({ State: 'writing', SHA256: '', SizeBytes: '0', Verified: false, Source: null })];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  const exactPassphrase = '  a deliberately exact secret  ';
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill(exactPassphrase);
  await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill(exactPassphrase);
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(jobCard(page)).toContainText(/pending/i);
  await expect(backupCard(page)).toContainText(/writing/i);
  await expect(backupCard(page).getByRole('button', { name: 'Download', exact: true })).toBeDisabled();
  await expect(page.getByText(/backup (created|completed) successfully/i)).not.toBeVisible();
  const [request] = api.mutations('/admin/v1/backups');
  assertJSONMutation(request);
  expect(request.json).toEqual({ RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), Passphrase: exactPassphrase });
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} });
  const readsBeforeCompletion = api.requests.filter((item) => item.method === 'GET' && item.path === '/admin/v1/backup-operations').length;
  api.operations = [operation({ RequestId: request.json!.RequestId as string, State: 'completed', Phase: 'finished', CanCancel: false, Source: source() })];
  api.backups = [backup()];
  await expect(jobCard(page)).toContainText('Completed', { timeout: 10_000 });
  await expect(backupCard(page).getByRole('button', { name: 'Download', exact: true })).toBeEnabled();
  expect(api.requests.filter((item) => item.method === 'GET' && item.path === '/admin/v1/backup-operations').length).toBeGreaterThan(readsBeforeCompletion);
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog.getByLabel(/^Passphrase(?:\s*\*)?$/)).toHaveValue('');
  await expect(dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/)).toHaveValue('');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
});

test('a writing backup keeps polling after the operation snapshot already reports completion', async ({ page, api }) => {
  api.status = status({ Busy: false, ActiveOperationId: '' });
  api.operations = [operation({ State: 'completed', Phase: 'finished', CanCancel: false, Source: source() })];
  api.backups = [backup({ State: 'writing', SHA256: '', SizeBytes: '0', Source: null, Verified: false })];
  await openBackups(page);
  await expect(jobCard(page)).toContainText('Completed');
  await expect(backupCard(page)).toContainText('Writing');
  const download = backupCard(page).getByRole('button', { name: 'Download', exact: true });
  await expect(download).toBeDisabled();
  const readsBeforePublication = api.requests.filter((request) => request.method === 'GET' && request.path === '/admin/v1/backups').length;
  api.backups = [backup()];
  await expect(download).toBeEnabled({ timeout: 10_000 });
  await expect(backupCard(page).getByText('Ready', { exact: true })).toBeVisible();
  expect(api.requests.filter((request) => request.method === 'GET' && request.path === '/admin/v1/backups').length).toBeGreaterThan(readsBeforePublication);
  expect(api.mutations()).toHaveLength(0);
});

test('a completed delete keeps polling until its stale ready backup disappears', async ({ page, api }) => {
  api.status = status({ Busy: false, ActiveOperationId: '' });
  api.operations = [operation({ Kind: 'delete', State: 'completed', Phase: 'finished', CanCancel: false, BackupId: backupId })];
  api.backups = [backup()];
  await openBackups(page);
  await expect(jobCard(page)).toContainText('Completed');
  await expect(backupCard(page).getByText('Ready', { exact: true })).toBeVisible();
  const readsBeforeDeletion = api.requests.filter((request) => request.method === 'GET' && request.path === '/admin/v1/backups').length;
  api.backups = [];
  await expect(backupCard(page)).not.toBeVisible({ timeout: 10_000 });
  expect(api.requests.filter((request) => request.method === 'GET' && request.path === '/admin/v1/backups').length).toBeGreaterThan(readsBeforeDeletion);
  expect(api.mutations()).toHaveLength(0);
});

test('create validates confirmation and UTF-8 byte limits before sending a request', async ({ page, api }) => {
  await openBackups(page);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  const passphrase = dialog.getByLabel(/^Passphrase(?:\s*\*)?$/);
  const confirmation = dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/);
  const submit = dialog.getByRole('button', { name: 'Create backup', exact: true });
  await passphrase.fill('elevenbytes');
  await confirmation.fill('elevenbytes');
  await expect(submit).toBeDisabled();
  await passphrase.fill('\u5bc6'.repeat(4));
  await confirmation.fill('\u5bc6'.repeat(4));
  await expect(submit).toBeEnabled();
  await confirmation.fill('\u5bc6'.repeat(4) + ' ');
  await expect(submit).toBeDisabled();
  await passphrase.fill('\u5bc6'.repeat(342));
  await confirmation.fill('\u5bc6'.repeat(342));
  await expect(submit).toBeDisabled();
  await passphrase.fill('\u5bc6'.repeat(341) + 'a');
  await confirmation.fill('\u5bc6'.repeat(341) + 'a');
  await expect(submit).toBeEnabled();
  expect(api.mutations()).toHaveLength(0);
});

test('import sends file bytes with admission and CSRF headers without multipart encoding', async ({ page, api }) => {
  const bytes = Buffer.from([0x61, 0x67, 0x65, 0x2d, 0x65, 0x6e, 0x63, 0x00, 0xff, 0x0d, 0x0a]);
  api.on('POST', '/admin/v1/backups/import', async (route, _request, captured) => {
    const admitted = operation({ Kind: 'import', State: 'pending', Phase: 'upload', RequestId: captured.headers['x-backup-request-id'] });
    api.operations = [admitted];
    api.backups = [backup({ Kind: 'imported', State: 'writing', SHA256: '', SizeBytes: '0', Verified: false, Source: null })];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await page.getByRole('button', { name: 'Import backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Import backup', exact: true });
  await dialog.getByLabel(/^Backup file(?:\s*\*)?$/).setInputFiles({ name: 'encrypted-backup.age', mimeType: 'application/octet-stream', buffer: bytes });
  await dialog.getByRole('button', { name: 'Import backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(jobCard(page)).toContainText(/pending|running/i);
  const [request] = api.mutations('/admin/v1/backups/import');
  expect(request.body?.equals(bytes)).toBe(true);
  expect(request.headers['content-type']).toBe('application/octet-stream');
  expect(request.headers['x-backup-request-id']).toMatch(/^[0-9a-f]{32}$/);
  expect(request.headers['x-csrf-token']).toBe(csrfToken);
  expect(request.json).toBeUndefined();
  expect(api.mutations('/admin/v1/backups/import')).toHaveLength(1);
});

test('a lost create response is found by RequestId without resending its secret', async ({ page, api }) => {
  let admitted: Operation | undefined;
  api.on('POST', '/admin/v1/backups', async (route, _request, captured) => {
    admitted = operation({ RequestId: captured.json!.RequestId as string });
    await route.abort('connectionreset');
  });
  await openBackups(page);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('never replay this secret');
  await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill('never replay this secret');
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByText('Admission could not be confirmed. Check the jobs before starting another attempt.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create backup', exact: true })).toBeDisabled();
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  expect(admitted).toBeDefined();
  api.operations = [
    ...Array.from({ length: 101 }, (_, index) => operation({
      Id: (index + 100).toString(16).padStart(32, '0'), RequestId: (index + 200).toString(16).padStart(32, '0'),
      State: 'completed', Phase: 'finished', CanCancel: false,
    })),
    admitted!,
  ];
  await page.getByRole('button', { name: 'Check jobs again', exact: true }).click();
  await expect(jobCard(page)).toContainText(/running/i);
  await expect(page.getByText('Admission could not be confirmed. Check the jobs before starting another attempt.')).not.toBeVisible();
  const posts = api.mutations('/admin/v1/backups');
  expect(posts).toHaveLength(1);
  expect(posts[0].json!.RequestId).toBe(admitted!.RequestId);
  expect(api.requests.some((request) => request.method === 'GET' && request.path === '/admin/v1/backup-operations'
    && Number(new URL(request.url).searchParams.get('StartIndex')) > 0)).toBe(true);
});

test('a new create attempt requires a complete search and a fresh idle status, then uses a new RequestId', async ({ page, api }) => {
  const mock = unadmittedCreate(api, false);
  await openBackups(page);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('first manual attempt secret');
  await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill('first manual attempt secret');
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await mock.tailRequested.reached;
  const restart = page.getByRole('button', { name: 'Start a new attempt', exact: true });
  await expect(restart).not.toBeVisible();
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
  mock.releaseTail.release();
  await mock.latestStatusRequested.reached;
  await expect(restart).not.toBeVisible();
  expect(mock.reconciliation).toEqual(['last page requested', 'last page returned', 'latest status requested']);
  mock.releaseLatestStatus.release();
  await expect(page.getByRole('button', { name: 'Check jobs again', exact: true })).toBeEnabled();
  await expect(restart).toBeEnabled();
  expect(mock.reconciliation.slice(0, 4)).toEqual(['last page requested', 'last page returned', 'latest status requested', 'latest status returned']);
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
  await restart.click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel(/^Passphrase(?:\s*\*)?$/)).toHaveValue('');
  await expect(dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/)).toHaveValue('');
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('second manual attempt secret');
  await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill('second manual attempt secret');
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(jobCard(page)).toContainText('Pending');
  const requests = api.mutations('/admin/v1/backups');
  expect(requests).toHaveLength(2);
  expect(mock.submittedIds).toEqual(requests.map((request) => request.json!.RequestId));
  expect(mock.submittedIds.every((id) => /^[0-9a-f]{32}$/.test(id))).toBe(true);
  expect(new Set(mock.submittedIds).size).toBe(2);
  expect(requests.map((request) => request.json!.Passphrase)).toEqual(['first manual attempt secret', 'second manual attempt secret']);
  requests.forEach(assertJSONMutation);
});

test('an unadmitted create cannot start another attempt when the status becomes busy after enumeration', async ({ page, api }) => {
  const mock = unadmittedCreate(api, true);
  await openBackups(page);
  await page.getByRole('button', { name: 'Create backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('busy server attempt secret');
  await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill('busy server attempt secret');
  await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await mock.tailRequested.reached;
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  mock.releaseTail.release();
  await mock.latestStatusRequested.reached;
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  mock.releaseLatestStatus.release();
  await expect(page.getByText('Recovery job in progress', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Check jobs again', exact: true })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  await expect(page.getByRole('button', { name: 'Create backup', exact: true })).toBeDisabled();
  expect(mock.reconciliation.slice(0, 4)).toEqual(['last page requested', 'last page returned', 'latest status requested', 'latest status returned']);
  expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
});

test('cancel sends the unrounded revision of the operation whose CanCancel flag permits it', async ({ page, api }) => {
  api.operations = [operation()];
  api.on('POST', `/admin/v1/backup-operations/${operationId}/cancel`, async (route) => {
    const cancelled = operation({ Revision: '9007199254740994', State: 'cancelled', Phase: 'finished', CanCancel: false, ErrorCode: 'operation_cancelled' });
    api.operations = [cancelled];
    await json(route, { Operation: cancelled }, 202);
  });
  await openBackups(page);
  await jobCard(page).getByRole('button', { name: 'Cancel job', exact: true }).click();
  await expect(jobCard(page)).toContainText(/cancelled/i);
  const [request] = api.mutations(`/admin/v1/backup-operations/${operationId}/cancel`);
  assertJSONMutation(request);
  expect(request.json).toEqual({ Revision: revision });
  expect(api.mutations()).toHaveLength(1);
});

test('restore planning uses the selected digest, generation, exact secret, and explicit replacement consent', async ({ page, api }) => {
  api.backups = [backup({ Kind: 'imported', Verified: false, Source: null })];
  api.status = status({ Rollback: { Available: true, MustReplace: true, CreatedAt: timestamp, ServerName: 'Previous server', Generation: rollbackGeneration, UnavailableReason: '' } });
  api.on('POST', '/admin/v1/restores/plans', async (route, _request, captured) => {
    const admitted = operation({ Kind: 'restore', State: 'running', Phase: 'validation', RequestId: captured.json!.RequestId as string, RestoreDefaults: true, ReplaceRollback: true });
    api.operations = [admitted];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await expect(backupCard(page)).not.toContainText('Archive server');
  await backupCard(page).getByRole('button', { name: 'Plan restore', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Plan restore', exact: true });
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('  exact imported passphrase  ');
  await expect(dialog.getByRole('checkbox', { name: 'Restore saved defaults', exact: true })).not.toBeChecked();
  await expect(dialog.getByRole('checkbox', { name: 'Replace the previous rollback copy', exact: true })).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Plan restore', exact: true })).toBeDisabled();
  await dialog.getByRole('checkbox', { name: 'Restore saved defaults', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Replace the previous rollback copy', exact: true }).check();
  await dialog.getByRole('button', { name: 'Plan restore', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(jobCard(page)).toContainText('Checking backup');
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  const [request] = api.mutations('/admin/v1/restores/plans');
  assertJSONMutation(request);
  expect(request.json).toEqual({
    RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), BackupId: backupId, SHA256: digest,
    Passphrase: '  exact imported passphrase  ', RestoreDefaults: true, ReplaceRollback: true, GenerationRevision: generation,
  });
});

test('planning keeps both optional restore flags false when no consent is selected', async ({ page, api }) => {
  api.backups = [backup()];
  api.on('POST', '/admin/v1/restores/plans', async (route, _request, captured) => {
    const admitted = operation({ Kind: 'restore', Phase: 'validation', RequestId: captured.json!.RequestId as string });
    api.operations = [admitted];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await backupCard(page).getByRole('button', { name: 'Plan restore', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Plan restore', exact: true });
  await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('existing encrypted secret');
  await dialog.getByRole('button', { name: 'Plan restore', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(api.mutations('/admin/v1/restores/plans')[0].json).toEqual({
    RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), BackupId: backupId, SHA256: digest,
    Passphrase: 'existing encrypted secret', RestoreDefaults: false, ReplaceRollback: false, GenerationRevision: generation,
  });
});

test('a ready plan must be inspected and confirmed before applying its current revisions', async ({ page, api }) => {
  const ready = operation({ Kind: 'restore', State: 'ready', Phase: 'ready', CanApply: true, Source: source(), RestoreDefaults: true });
  api.operations = [{ ...ready, Revision: '8' }];
  api.on('GET', `/admin/v1/backup-operations/${operationId}`, async (route) => json(route, { Operation: ready }));
  api.on('POST', `/admin/v1/restores/${operationId}/apply`, async (route) => {
    const applying = { ...ready, Revision: '9007199254740994', State: 'applying', Phase: 'activation', CanApply: false, CanCancel: false };
    api.operations = [applying];
    api.on('GET', `/admin/v1/backup-operations/${operationId}`, async (detailRoute) => json(detailRoute, { Operation: applying }));
    await json(route, { Operation: applying }, 202);
  });
  await openBackups(page);
  await jobCard(page).getByRole('button', { name: 'Inspect plan', exact: true }).click();
  const inspection = page.getByRole('dialog', { name: 'Inspect plan', exact: true });
  await expect(inspection).toContainText('Archive server');
  await expect(inspection).toContainText('users');
  await expect(inspection).toContainText(/saved defaults/i);
  await expect(inspection.getByText('9,007,199,254,740,997', { exact: true })).toBeVisible();
  expect(api.mutations()).toHaveLength(0);
  await inspection.getByRole('button', { name: 'Apply restore', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Apply restore', exact: true });
  const apply = confirmation.getByRole('button', { name: 'Apply restore', exact: true });
  await expect(apply).toBeDisabled();
  await confirmation.getByRole('checkbox', { name: 'I understand that everyone must sign in again', exact: true }).check();
  expect(api.mutations()).toHaveLength(0);
  await apply.click();
  await expect(confirmation).not.toBeVisible();
  await expect(jobCard(page)).toContainText(/applying/i);
  await expect(page.getByText(/restore (completed|applied) successfully/i)).not.toBeVisible();
  const [request] = api.mutations(`/admin/v1/restores/${operationId}/apply`);
  assertJSONMutation(request);
  expect(request.json).toEqual({ Revision: revision, GenerationRevision: generation });
  expect(api.mutations()).toHaveLength(1);
});

test('a lost apply response remains uncertain while the old plan is still ready and is never replayed', async ({ page, api }) => {
  const ready = operation({ Kind: 'restore', State: 'ready', Phase: 'ready', CanApply: true, Source: source() });
  api.operations = [ready];
  api.on('POST', `/admin/v1/restores/${operationId}/apply`, async (route) => route.abort('connectionreset'));
  await openBackups(page);
  await jobCard(page).getByRole('button', { name: 'Inspect plan', exact: true }).click();
  await page.getByRole('dialog', { name: 'Inspect plan', exact: true }).getByRole('button', { name: 'Apply restore', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Apply restore', exact: true });
  await confirmation.getByRole('checkbox', { name: 'I understand that everyone must sign in again', exact: true }).check();
  await confirmation.getByRole('button', { name: 'Apply restore', exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  const warning = page.getByText(/Activation could not be confirmed\. A ready plan alone does not confirm that it was applied\./);
  await expect(warning).toBeVisible();
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  await page.getByRole('button', { name: 'Check jobs again', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Check jobs again', exact: true })).toBeEnabled();
  await expect(warning).toBeVisible();
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  await expect(jobCard(page)).toContainText('Ready');
  await jobCard(page).getByRole('button', { name: 'Inspect plan', exact: true }).click();
  const inspection = page.getByRole('dialog', { name: 'Inspect plan', exact: true });
  await expect(inspection.getByText('An earlier activation request still has an unknown outcome. Check the jobs before submitting another request.', { exact: true })).toBeVisible();
  await expect(inspection.getByRole('button', { name: 'Apply restore', exact: true })).toBeDisabled();
  await inspection.getByRole('button', { name: 'Close', exact: true }).click();
  expect(api.mutations(`/admin/v1/restores/${operationId}/apply`)).toHaveLength(1);
  api.operations = [];
  const statusReadsBeforeMissingPlan = api.requests.filter((request) => request.path === '/admin/v1/backups/status').length;
  await page.getByRole('button', { name: 'Check jobs again', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Check jobs again', exact: true })).toBeEnabled();
  await expect(warning).toBeVisible();
  await expect(page.getByRole('button', { name: 'Start a new attempt', exact: true })).not.toBeVisible();
  expect(api.requests.filter((request) => request.path === '/admin/v1/backups/status').length).toBeGreaterThanOrEqual(statusReadsBeforeMissingPlan + 2);
  expect(api.mutations(`/admin/v1/restores/${operationId}/apply`)).toHaveLength(1);
  api.operations = [{ ...ready, Revision: '9007199254740994', State: 'applying', Phase: 'activation', CanApply: false, CanCancel: false }];
  await page.getByRole('button', { name: 'Check jobs again', exact: true }).click();
  await expect(warning).not.toBeVisible();
  await expect(jobCard(page)).toContainText('Applying');
  expect(api.mutations(`/admin/v1/restores/${operationId}/apply`)).toHaveLength(1);
});

for (const blockedBy of ['server capability', 'changed generation'] as const) {
  test(`a ready plan cannot be applied when blocked by ${blockedBy}`, async ({ page, api }) => {
    api.operations = [operation({ Kind: 'restore', State: 'ready', Phase: 'ready', Source: source(), CanCancel: false, CanApply: blockedBy !== 'server capability' })];
    if (blockedBy === 'changed generation') api.status = status({ GenerationRevision: '9007199254740996' });
    await openBackups(page);
    await expect(jobCard(page).getByRole('button', { name: 'Cancel job', exact: true })).not.toBeVisible();
    await jobCard(page).getByRole('button', { name: 'Inspect plan', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Inspect plan', exact: true });
    await expect(dialog.getByText('This plan cannot be applied now. Refresh the details and check the current job and server state.', { exact: true })).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Apply restore', exact: true })).toBeDisabled();
    expect(api.mutations()).toHaveLength(0);
  });
}

test('rollback requires sign-in consent and submits the exact current generation', async ({ page, api }) => {
  api.status = status({ Rollback: { Available: true, MustReplace: true, CreatedAt: timestamp, ServerName: 'Retained server', Generation: rollbackGeneration, UnavailableReason: '' } });
  api.on('POST', '/admin/v1/restores/rollback', async (route, _request, captured) => {
    const admitted = operation({ Kind: 'rollback', State: 'applying', Phase: 'rollback', BackupId: '', CanCancel: false, RequestId: captured.json!.RequestId as string });
    api.operations = [admitted];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await page.getByRole('button', { name: 'Roll back', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Roll back', exact: true });
  const submit = dialog.getByRole('button', { name: 'Roll back', exact: true });
  await expect(dialog).toContainText('Retained server');
  await expect(submit).toBeDisabled();
  await dialog.getByRole('checkbox', { name: 'I understand that everyone must sign in again', exact: true }).check();
  expect(api.mutations()).toHaveLength(0);
  await submit.click();
  await expect(dialog).not.toBeVisible();
  await expect(jobCard(page)).toContainText(/applying/i);
  await expect(page.getByText(/rollback completed successfully/i)).not.toBeVisible();
  const [request] = api.mutations('/admin/v1/restores/rollback');
  assertJSONMutation(request);
  expect(request.json).toEqual({ RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), GenerationRevision: generation });
});

test('delete requires explicit consent and sends the selected backup digest', async ({ page, api }) => {
  api.backups = [backup()];
  api.on('DELETE', `/admin/v1/backups/${backupId}`, async (route, _request, captured) => {
    const admitted = operation({ Kind: 'delete', State: 'pending', Phase: 'admission', RequestId: captured.json!.RequestId as string });
    api.backups = [backup({ State: 'deleting' })];
    api.operations = [admitted];
    await json(route, { Operation: admitted }, 202);
  });
  await openBackups(page);
  await backupCard(page).getByRole('button', { name: 'Delete backup', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete backup', exact: true });
  const submit = dialog.getByRole('button', { name: 'Delete backup', exact: true });
  await expect(submit).toBeDisabled();
  await dialog.getByRole('checkbox', { name: 'I understand this backup will be permanently deleted', exact: true }).check();
  expect(api.mutations()).toHaveLength(0);
  await submit.click();
  await expect(dialog).not.toBeVisible();
  const [request] = api.mutations(`/admin/v1/backups/${backupId}`);
  assertJSONMutation(request);
  expect(request.method).toBe('DELETE');
  expect(request.json).toEqual({ RequestId: expect.stringMatching(/^[0-9a-f]{32}$/), SHA256: digest });
});

test('ready downloads use an authenticated browser attachment without buffering a Blob', async ({ page, api }) => {
  const bytes = Buffer.from('mock encrypted archive');
  api.backups = [backup({ SizeBytes: String(bytes.length) })];
  const headers = {
    'Content-Type': 'application/octet-stream', 'Content-Length': String(bytes.length),
    'Content-Disposition': 'attachment; filename="goby-backup.age"', ETag: `"${digest}"`, 'Cache-Control': 'no-store',
  };
  api.on('HEAD', `/admin/v1/backups/${backupId}/file`, async (route) => route.fulfill({ status: 200, headers }));
  api.on('GET', `/admin/v1/backups/${backupId}/file`, async (route) => route.fulfill({ status: 200, headers, body: bytes }));
  await page.addInitScript(() => {
    Reflect.set(window, '__backupBlobReads', 0);
    Reflect.set(window, '__backupObjectURLs', 0);
    const readBlob = Response.prototype.blob;
    Response.prototype.blob = function () {
      Reflect.set(window, '__backupBlobReads', Number(Reflect.get(window, '__backupBlobReads')) + 1);
      return readBlob.call(this);
    };
    const createObjectURL = URL.createObjectURL;
    URL.createObjectURL = function (value) {
      Reflect.set(window, '__backupObjectURLs', Number(Reflect.get(window, '__backupObjectURLs')) + 1);
      return createObjectURL.call(URL, value);
    };
  });
  await openBackups(page);
  const downloaded = page.waitForEvent('download');
  await backupCard(page).getByRole('button', { name: 'Download', exact: true }).click();
  const download = await downloaded;
  expect(new URL(download.url()).pathname).toBe(`/admin/v1/backups/${backupId}/file`);
  expect(download.suggestedFilename()).toBe('goby-backup.age');
  expect(api.requests.filter((request) => request.path === `/admin/v1/backups/${backupId}/file`).map((request) => request.method)).toEqual(['HEAD', 'GET']);
  expect(await page.evaluate(() => ({ blobs: Reflect.get(window, '__backupBlobReads'), urls: Reflect.get(window, '__backupObjectURLs') }))).toEqual({ blobs: 0, urls: 0 });
  expect(api.mutations()).toHaveLength(0);
});

for (const malformed of ['numeric generation', 'numeric backup size', 'invalid operation capability', 'ready backup without a digest', 'unverified imported source'] as const) {
  test(`malformed DTOs are rejected before enabling destructive actions: ${malformed}`, async ({ page, api }) => {
    if (malformed === 'numeric generation') api.status = { ...status(), GenerationRevision: 9007199254740992 };
    if (malformed === 'numeric backup size') api.backups = [{ ...backup(), SizeBytes: 1024 }];
    if (malformed === 'invalid operation capability') api.operations = [{ ...operation(), CanApply: 'true' }];
    if (malformed === 'ready backup without a digest') api.backups = [backup({ SHA256: '' })];
    if (malformed === 'unverified imported source') api.backups = [backup({ Kind: 'imported', Verified: false })];
    await page.goto('/admin/backups');
    await expect(page.getByText('The server returned an unexpected response. Refresh to check the actual job state.', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Create backup', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Import backup', exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Plan restore', exact: true })).not.toBeVisible();
    await expect(page.getByRole('button', { name: 'Delete backup', exact: true })).not.toBeVisible();
    await expect(page.getByRole('button', { name: 'Cancel job', exact: true })).not.toBeVisible();
    expect(api.mutations()).toHaveLength(0);
    await expect(page.getByRole('button', { name: 'Apply restore', exact: true })).not.toBeVisible();
  });
}

test('failed and interrupted jobs show their state without offering activation or cancellation', async ({ page, api }) => {
  const failedId = '66666666666666666666666666666666';
  api.operations = [
    operation({ Kind: 'restore', State: 'failed', Phase: 'finished', ErrorCode: 'invalid_archive', CanCancel: false }),
    operation({ Id: failedId, RequestId: '77777777777777777777777777777777', State: 'interrupted', Phase: 'finished', ErrorCode: 'operation_interrupted', CanCancel: false }),
  ];
  await openBackups(page);
  await expect(jobCard(page)).toContainText(/failed/i);
  await expect(jobCard(page, failedId)).toContainText(/interrupted/i);
  await expect(jobCard(page).getByRole('button', { name: 'Cancel job', exact: true })).not.toBeVisible();
  await expect(jobCard(page, failedId).getByRole('button', { name: 'Cancel job', exact: true })).not.toBeVisible();
  await expect(page.getByRole('button', { name: 'Apply restore', exact: true })).not.toBeVisible();
  expect(api.mutations()).toHaveLength(0);
});

for (const rejection of [{ status: 401, code: 'unauthorized' }, { status: 403, code: 'csrf_invalid' }]) {
  test(`a ${rejection.status} session rejection clears the form and never replays a backup mutation`, async ({ page, api }) => {
    api.on('POST', '/admin/v1/backups', async (route) => error(route, rejection.code, 'Sign in again.', rejection.status));
    await openBackups(page);
    await page.getByRole('button', { name: 'Create backup', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
    await dialog.getByLabel(/^Passphrase(?:\s*\*)?$/).fill('expired session passphrase');
    await dialog.getByLabel(/^Confirm passphrase(?:\s*\*)?$/).fill('expired session passphrase');
    await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
    await expect(page.getByText('Your session has changed or expired. If a restore or rollback was in progress, sign in with the restored account passwords and check its job status. A disconnected session does not confirm completion.', { exact: true })).toBeVisible();
    await expect(page.getByLabel(/^Password(?:\s*\*)?$/)).toHaveValue('');
    expect(api.mutations('/admin/v1/backups')).toHaveLength(1);
    expect(api.requests.filter((request) => request.path === '/admin/v1/session' && request.method === 'POST')).toHaveLength(0);
  });
}

test('mobile backup cards and confirmation dialogs fit the viewport and support keyboard access', async ({ page, api }, testInfo) => {
  api.backups = [backup({ Source: source({ ServerName: 'A deliberately long archive server name that must wrap on mobile screens' }) })];
  api.operations = [operation()];
  await page.setViewportSize({ width: 390, height: 844 });
  await openBackups(page);
  await expect(backupCard(page)).toBeVisible();
  await expect(jobCard(page)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
  const navigationLink = page.getByRole('link', { name: 'Backups & recovery', exact: true });
  await navigationLink.focus();
  await navigationLink.press('Enter');
  await expect(page.getByRole('button', { name: 'Open navigation', exact: true })).toBeFocused();
  const create = page.getByRole('button', { name: 'Create backup', exact: true });
  await create.focus();
  await create.press('Enter');
  const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel(/^Passphrase(?:\s*\*)?$/)).toBeFocused();
  await page.keyboard.press('Tab');
  expect(await dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('backups-mobile-create.png'), fullPage: true, animations: 'disabled' });
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  await expect(create).toBeFocused();
  expect(api.mutations()).toHaveLength(0);
});
