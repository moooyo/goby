import { test, expect, type Page, type BrowserContext, type Request, type Response } from '@playwright/test';
import { createHash } from 'node:crypto';
import { promises as fs } from 'node:fs';
import path from 'node:path';

interface Fixture {
  Marker: string; RunId: string; Phase: 'create' | 'restore' | 'rollback'; Origin: string;
  AdminName: string; Password: string; RestoredPassword: string; Passphrase: string;
  ArchivePath: string; ResultPath: string; ArtifactDirectory: string; ExpectedTables: string[]; BackupId?: string;
  GateRequestPath?: string; GateAcquiredPath?: string; GateReleasePath?: string; GateReleasedPath?: string;
}
interface Source { ServerName: string; SchemaVersion: string; Tables: { Name: string; Rows: string }[]; }
interface Operation { Id: string; RequestId: string; Kind: string; State: string; Phase: string; ErrorCode: string; BackupId: string; CanApply: boolean; Revision: string; GenerationRevision: string; RestoreDefaults: boolean; ReplaceRollback: boolean; Source: Source | null; }
interface Backup { Id: string; State: string; SHA256: string; SizeBytes: string; Verified: boolean; }
interface FailureOperation { Id: string; Kind: string; State: string; Phase: string; ErrorCode: string; }
interface Result {
  Marker: string; RunId: string; Phase: string; Complete: boolean; Checks: Record<string, boolean>;
  BackupId?: string; SHA256?: string; OperationId?: string; BeforeCookie?: string; AfterCookie?: string;
  FailureOperation?: FailureOperation;
  AdmissionFallbacks?: AdmissionFallback[];
}
interface AdmissionFallback {
  Action: 'create' | 'import' | 'plan' | 'apply' | 'rollback';
  Reason: 'inspector_cache_evicted' | 'body_unavailable_after_navigation';
  Scope: 'durable_request_id' | 'durable_request_id_after_login' | 'known_plan_id_pending_completion';
  RequestId: string | null; OperationId: string | null; Resolved: boolean;
}
type CapturedBody = { Bytes: Buffer } | { Error: unknown };
const admissionBodies = new WeakMap<Response, Promise<CapturedBody>>();
type GateSignal = { Marker: 'goby-backup-recovery-gate-v1'; RunId: string; Phase: 'create' } & (
  { Action: 'acquire' | 'acquired' } |
  { Action: 'release' | 'released'; OperationId: string; FirstResponseSequence: number; SecondResponseSequence: number }
);
interface OperationResponse { Sequence: number; Items?: Operation[]; }

function requireValue(value: unknown, message: string): asserts value {
  if (!value) throw new Error(message);
}
function failedOperation(operation: Operation): boolean {
  return ['failed', 'cancelled', 'interrupted'].includes(operation.State);
}
function failureProjection(operation: Operation): FailureOperation {
  const safeWord = (value: unknown, empty = false) => typeof value === 'string' &&
    ((empty && value === '') || /^[a-z][a-z0-9_]{0,63}$/.test(value));
  requireValue(/^[0-9a-f]{32}$/.test(operation.Id) && failedOperation(operation) &&
    safeWord(operation.Kind) && safeWord(operation.Phase, true) && safeWord(operation.ErrorCode, true),
  'The failed operation did not contain a safe fixed-field projection.');
  return { Id: operation.Id, Kind: operation.Kind, State: operation.State, Phase: operation.Phase, ErrorCode: operation.ErrorCode };
}
function gatePath(value: string | undefined): string {
  requireValue(typeof value === 'string' && path.isAbsolute(value), 'An absolute private create gate path is required.');
  return value;
}
async function writeGateSignal(filename: string, signal: GateSignal) {
  requireValue(process.getuid?.() === 0, 'Only the root-owned runtime browser can signal the create gate.');
  try {
    await fs.lstat(filename);
    throw new Error('The exclusive create gate signal already exists.');
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
  }
  const temporary = `${filename}.next`;
  await fs.writeFile(temporary, JSON.stringify(signal), { mode: 0o600, flag: 'wx' });
  const information = await fs.lstat(temporary);
  requireValue(information.isFile() && !information.isSymbolicLink() && information.nlink === 1 &&
    (information.mode & 0o777) === 0o600 && information.uid === 0, 'The create gate signal is not private and root-owned.');
  await fs.rename(temporary, filename);
}
async function waitForGateSignal(filename: string, expected: GateSignal, onPoll?: () => Promise<void>) {
  let conditionFailure: unknown;
  await expect.poll(async () => {
    try { await onPoll?.(); }
    catch (error) { conditionFailure = error; return true; }
    let information;
    try { information = await fs.lstat(filename); }
    catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') return false;
      throw error;
    }
    requireValue(information.isFile() && !information.isSymbolicLink() && information.nlink === 1 &&
      (information.mode & 0o777) === 0o600 && information.uid === 0 && information.size <= 4096,
    'The operator create gate signal is not private and root-owned.');
    let actual: unknown;
    try { actual = JSON.parse(await fs.readFile(filename, 'utf8')); }
    catch (error) { if (error instanceof SyntaxError) return false; throw error; }
    if (!actual || typeof actual !== 'object' || Array.isArray(actual)) return false;
    const values = actual as Record<string, unknown>;
    return Object.keys(values).length === Object.keys(expected).length &&
      Object.entries(expected).every(([name, value]) => values[name] === value);
  }, { timeout: 45_000, intervals: [100, 250, 500] }).toBe(true);
  if (conditionFailure) throw conditionFailure;
}
function responseFor(page: Page, route: string, method = 'POST') {
  return page.waitForResponse((response) => new URL(response.url()).pathname === route && response.request().method() === method, { timeout: 120_000 });
}
function admissionFor(page: Page, route: string): Promise<Response> {
  return responseFor(page, route).then((response) => {
    // Capture as soon as the headers arrive, while click() may still be waiting
    // for the product to process its own response. Rejections are settled now.
    admissionBodies.set(response, response.body().then((Bytes) => ({ Bytes }), (Error: unknown) => ({ Error })));
    return response;
  });
}
function evictionReason(error: unknown): AdmissionFallback['Reason'] | undefined {
  if (!(error instanceof Error)) return undefined;
  if (/Request content was evicted from inspector cache/i.test(error.message)) return 'inspector_cache_evicted';
  if (/Response body (?:is )?(?:not available|unavailable)[\s\S]{0,500}navigated away/i.test(error.message)) return 'body_unavailable_after_navigation';
  return undefined;
}
async function durableReceipt(context: BrowserContext, requestId: string, kind: string): Promise<Operation> {
  const items: Operation[] = [];
  let total: number | undefined;
  for (let start = 0; total === undefined || start < total; start += 100) {
    const response = await context.request.get(`/admin/v1/backup-operations?StartIndex=${start}&Limit=100`);
    requireValue(response.status() === 200 && response.headers()['cache-control'] === 'no-store', 'The exact durable receipt lookup failed.');
    const page = await response.json() as { Items: Operation[]; TotalRecordCount: number; StartIndex: number; Limit: number };
    requireValue(Number.isSafeInteger(page.TotalRecordCount) && page.TotalRecordCount >= 0 && page.TotalRecordCount <= 1000 &&
      (total === undefined || total === page.TotalRecordCount) && page.StartIndex === start && page.Limit === 100 &&
      Array.isArray(page.Items) && page.Items.length === Math.min(100, page.TotalRecordCount - start),
    'The durable receipt inventory was incomplete or changed during pagination.');
    total = page.TotalRecordCount;
    items.push(...page.Items);
  }
  requireValue(items.every((item) => typeof item?.Id === 'string' && /^[0-9a-f]{32}$/.test(item.Id)) &&
    new Set(items.map((item) => item.Id)).size === items.length, 'The durable receipt inventory contains ambiguous operation identities.');
  const matches = items.filter((item) => item.RequestId === requestId);
  requireValue(matches.length === 1 && matches[0].Kind === kind, 'The originating request did not resolve to one exact durable operation.');
  return matches[0];
}
async function admitted(pending: Promise<Response>, context: BrowserContext,
  recordFallback: (value: AdmissionFallback) => Promise<void>, knownPlan?: Operation,
  reauthenticate?: () => Promise<void>): Promise<Operation> {
  const response = await pending;
  requireValue(response.status() === 202, 'The real server did not admit the requested recovery operation.');
  requireValue(response.headers()['cache-control'] === 'no-store', 'The recovery response was cacheable.');
  const request = response.request();
  const pathname = new URL(request.url()).pathname;
  const apply = /^\/admin\/v1\/restores\/([0-9a-f]{32})\/apply$/.exec(pathname);
  const action: AdmissionFallback['Action'] | undefined = pathname === '/admin/v1/backups' ? 'create'
    : pathname === '/admin/v1/backups/import' ? 'import' : pathname === '/admin/v1/restores/plans' ? 'plan'
      : pathname === '/admin/v1/restores/rollback' ? 'rollback' : apply ? 'apply' : undefined;
  requireValue(action && request.method() === 'POST', 'An unexpected request reached the admission helper.');
  const input = action === 'import' ? undefined : JSON.parse(request.postData() ?? '') as Record<string, unknown>;
  const requestId = action === 'apply' ? null : action === 'import' ? request.headers()['x-backup-request-id'] : input?.RequestId;
  requireValue(action === 'apply' || typeof requestId === 'string' && /^[0-9a-f]{32}$/.test(requestId), 'The originating request ID was invalid.');
  const kind = action === 'plan' || action === 'apply' ? 'restore' : action;
  if (action === 'apply') requireValue(knownPlan && knownPlan.Id === apply?.[1] && knownPlan.Kind === 'restore' &&
    /^[0-9a-f]{32}$/.test(knownPlan.RequestId) &&
    input?.Revision === knownPlan.Revision && input?.GenerationRevision === knownPlan.GenerationRevision,
  'The accepted apply request did not match the previously inspected plan.');
  const captured = await admissionBodies.get(response);
  requireValue(captured, 'The native POST response was not captured eagerly.');
  let operation: Operation;
  if ('Bytes' in captured) {
    // Malformed real JSON is a product/contract failure, never a cache fallback.
    operation = (JSON.parse(captured.Bytes.toString('utf8')) as { Operation: Operation }).Operation;
  } else {
    const reason = evictionReason(captured.Error);
    if (!reason) throw captured.Error;
    const evidence: AdmissionFallback = { Action: action, Reason: reason,
      Scope: action === 'apply' ? 'known_plan_id_pending_completion' : action === 'rollback' ? 'durable_request_id_after_login' : 'durable_request_id',
      RequestId: typeof requestId === 'string' ? requestId : null, OperationId: null, Resolved: false };
    await recordFallback(evidence);
    if (action === 'apply') {
      // This is the earlier real plan object, not a fabricated applying result.
      // Completion is still required after the normal restored-password login.
      operation = knownPlan!;
    } else {
      if (action === 'rollback') {
        requireValue(reauthenticate, 'Rollback receipt recovery requires the normal restored-password login.');
        await reauthenticate();
      }
      operation = await durableReceipt(context, requestId as string, kind);
    }
    evidence.OperationId = operation.Id;
    evidence.Resolved = true;
    await recordFallback(evidence);
  }
  requireValue(operation && /^[0-9a-f]{32}$/.test(operation.Id) && operation.Kind === kind &&
    (action === 'apply' ? operation.Id === knownPlan?.Id && operation.RequestId === knownPlan.RequestId : operation.RequestId === requestId),
  'The accepted operation does not match its exact originating request and kind.');
  if (action === 'plan') requireValue(operation.BackupId === input?.BackupId && operation.RestoreDefaults === input?.RestoreDefaults &&
    operation.ReplaceRollback === input?.ReplaceRollback, 'The durable restore receipt does not match the submitted plan arguments.');
  return operation;
}
async function cookie(context: BrowserContext): Promise<string> {
  const values = (await context.cookies()).filter((value) => value.name === 'goby_session');
  requireValue(values.length === 1 && values[0].httpOnly, 'A single native HttpOnly session was not established.');
  return `goby_session=${values[0].value}`;
}
async function login(page: Page, fixture: Fixture, password: string) {
  await page.goto('/admin/');
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toBeVisible();
  await page.getByLabel(/^Username/).fill(fixture.AdminName);
  await page.getByLabel(/^Password/).fill(password);
  const pending = responseFor(page, '/admin/v1/session');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  requireValue((await pending).status() === 200, 'The real native password login failed.');
  await page.getByRole('navigation', { name: 'Administration' }).getByRole('link', { name: 'Backups & recovery', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Backups & recovery', exact: true })).toBeVisible();
}
async function complete(page: Page, context: BrowserContext, id: string, state: string,
  onFailure: (operation: Operation) => Promise<void>): Promise<Operation> {
  let final: Operation | undefined;
  await expect.poll(async () => {
    const response = await context.request.get(`/admin/v1/backup-operations/${id}`);
    requireValue(response.status() === 200, 'A real operation poll failed.');
    final = (await response.json() as { Operation: Operation }).Operation;
    if (failedOperation(final)) {
      await onFailure(final);
      return true;
    }
    return final.State === state;
  }, { timeout: 180_000, intervals: [1000, 2000] }).toBe(true);
  requireValue(final && !failedOperation(final), 'A real recovery operation reached a failure state; its safe operation details were retained.');
  await expect(page.getByTestId(`operation-${id}`).getByText(state === 'ready' ? 'Ready' : 'Completed', { exact: true })).toBeVisible({ timeout: 15_000 });
  return final!;
}

test('real native backup, activation, and rollback phase', async ({ page, context }) => {
  test.setTimeout(300_000);
  const fixturePath = process.env.GOBY_BACKUP_RECOVERY_FIXTURE;
  test.skip(!fixturePath, 'Only the owned Linux runtime operator can supply this private fixture.');
  const information = await fs.lstat(fixturePath!);
  requireValue(information.isFile() && !information.isSymbolicLink() && (information.mode & 0o777) === 0o600 && information.uid === process.getuid?.(), 'The browser fixture is not private and owned.');
  const fixture = JSON.parse(await fs.readFile(fixturePath!, 'utf8')) as Fixture;
  requireValue(fixture.Marker === 'goby-backup-recovery-fixture-v1' && fixture.Origin === process.env.GOBY_SMOKE_BASE_URL && /^http:\/\/127\.0\.0\.1:[0-9]+$/.test(fixture.Origin), 'The browser fixture origin or marker changed.');
  const result: Result = { Marker: 'goby-backup-recovery-result-v1', RunId: fixture.RunId, Phase: fixture.Phase, Complete: false, Checks: {} };
  const save = () => fs.writeFile(fixture.ResultPath, JSON.stringify(result), { mode: 0o600 });
  const retainFailure = async (operation: Operation) => {
    result.FailureOperation ??= failureProjection(operation);
    await save();
  };
  const recordAdmissionFallback = async (value: AdmissionFallback) => {
    const values = result.AdmissionFallbacks ??= [];
    if (!values.includes(value)) values.push(value);
    requireValue(values.length <= 5, 'The admission fallback inventory exceeded its bounded scope.');
    await save();
  };
  const secrets = [fixture.Password, fixture.RestoredPassword, fixture.Passphrase];
  const reconnectAfterSwitch = async () => {
    if (result.AfterCookie) return;
    await expect.poll(async () => {
      try { return (await context.request.get('/admin/v1/session', { headers: { Cookie: result.BeforeCookie! }, timeout: 3000 })).status(); }
      catch { return 0; }
    }, { timeout: 90_000, intervals: [1000, 2000] }).toBe(401);
    result.Checks.OldNativeCookieRejected = true;
    await context.clearCookies();
    await login(page, fixture, fixture.RestoredPassword);
    result.AfterCookie = await cookie(context);
    secrets.push(result.AfterCookie, result.AfterCookie.split('=')[1]);
    await save();
  };
  let unsafeURL = false;
  let pageErrors = 0;
  let operationResponseSequence = 0;
  const operationResponses: OperationResponse[] = [];
  let gatedOperationId: string | undefined;
  let gatedFailure: Operation | undefined;
  let failurePersistence: Promise<void> | undefined;
  const observeGateFailure = () => {
    if (!gatedOperationId || gatedFailure) return;
    const failure = operationResponses.flatMap((observed) => observed.Items ?? []).find((operation) =>
      operation?.Id === gatedOperationId && failedOperation(operation));
    if (failure) {
      gatedFailure = failure;
      failurePersistence = retainFailure(failure);
      void failurePersistence.catch(() => { /* The gate condition reports persistence failures synchronously. */ });
    }
  };
  const assertGateHealthy = async () => {
    observeGateFailure();
    if (gatedFailure) {
      await failurePersistence;
      throw new Error('The gated real backup operation reached a failure state; its safe operation details were retained.');
    }
  };
  const onPageError = () => { pageErrors += 1; };
  const onRequest = (request: Request) => {
    const url = new URL(request.url());
    if (url.username || url.password || secrets.some((secret) => secret && (request.url().includes(secret) || request.url().includes(encodeURIComponent(secret))))) unsafeURL = true;
  };
  const onOperationResponse = (response: Response) => {
    const url = new URL(response.url());
    if (url.origin !== fixture.Origin || url.pathname !== '/admin/v1/backup-operations' || response.request().method() !== 'GET') return;
    const observed: OperationResponse = { Sequence: ++operationResponseSequence };
    operationResponses.push(observed);
    if (response.status() !== 200) return;
    void response.json().then((value: unknown) => {
      if (value && typeof value === 'object' && 'Items' in value && Array.isArray(value.Items)) {
        observed.Items = value.Items as Operation[];
        observeGateFailure();
      }
    }, () => { /* An aborted or unreadable response cannot prove a real poll. */ });
  };
  page.on('pageerror', onPageError);
  page.on('request', onRequest);
  page.on('response', onOperationResponse);
  const capture = async (name: string) => {
    const text = await page.locator('body').innerText();
    requireValue(!secrets.some((secret) => secret && text.includes(secret)) && !unsafeURL, 'Private values reached visible content or a request URL.');
    requireValue(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'The real recovery page overflows horizontally.');
    await page.screenshot({ path: path.join(fixture.ArtifactDirectory, name), fullPage: true });
  };
  try {
    await login(page, fixture, fixture.Password);
    result.BeforeCookie = await cookie(context);
    secrets.push(result.BeforeCookie, result.BeforeCookie.split('=')[1]);
    await save();
    result.Checks.NativeBrowserPasswordLogin = true;
    if (fixture.Phase === 'create') {
      await page.getByRole('button', { name: 'Create backup', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Create backup', exact: true });
      await dialog.getByLabel(/^Passphrase/).fill(fixture.Passphrase);
      await dialog.getByLabel(/^Confirm passphrase/).fill(fixture.Passphrase);
      const requestPath = gatePath(fixture.GateRequestPath);
      const acquiredPath = gatePath(fixture.GateAcquiredPath);
      const releasePath = gatePath(fixture.GateReleasePath);
      const releasedPath = gatePath(fixture.GateReleasedPath);
      requireValue(new Set([requestPath, acquiredPath, releasePath, releasedPath]).size === 4, 'Create gate signal paths must be distinct.');
      const gate = { Marker: 'goby-backup-recovery-gate-v1' as const, RunId: fixture.RunId, Phase: 'create' as const };
      await writeGateSignal(requestPath, { ...gate, Action: 'acquire' });
      await waitForGateSignal(acquiredPath, { ...gate, Action: 'acquired' });
      const pending = admissionFor(page, '/admin/v1/backups');
      await dialog.getByRole('button', { name: 'Create backup', exact: true }).click();
      const created = await admitted(pending, context, recordAdmissionFallback);
      gatedOperationId = created.Id;
      await assertGateHealthy();
      const runningResponseAfter = (sequence: number) => operationResponses.find((observed) => observed.Sequence > sequence &&
        observed.Items?.some((operation) => operation?.Id === created.Id && operation.State === 'running'));
      const waitForGatedCondition = async (condition: () => boolean | Promise<boolean>) => {
        await expect.poll(async () => {
          observeGateFailure();
          return gatedFailure !== undefined || await condition();
        }, { timeout: 45_000, intervals: [100, 250, 500] }).toBe(true);
        await assertGateHealthy();
      };
      await waitForGatedCondition(() => runningResponseAfter(0) !== undefined);
      const firstResponse = runningResponseAfter(0);
      requireValue(firstResponse, 'The real page did not receive the gated running backup operation.');
      const runningCard = page.getByTestId(`operation-${created.Id}`).getByText('Running', { exact: true });
      const requireRunningCard = () => waitForGatedCondition(() => runningCard.isVisible());
      await requireRunningCard();
      // The next qualifying response must arrive after Running was visible.
      // No page action or APIRequestContext read occurs while the gate is held.
      const afterRunningUI = operationResponseSequence;
      await waitForGatedCondition(() => runningResponseAfter(afterRunningUI) !== undefined);
      const secondResponse = runningResponseAfter(afterRunningUI);
      requireValue(secondResponse && Number.isSafeInteger(firstResponse.Sequence) && firstResponse.Sequence > 0 &&
        Number.isSafeInteger(secondResponse.Sequence) && secondResponse.Sequence > firstResponse.Sequence,
      'Two ordered real page responses did not prove the gated running operation.');
      await requireRunningCard();
      const release: GateSignal = { ...gate, Action: 'release', OperationId: created.Id,
        FirstResponseSequence: firstResponse.Sequence, SecondResponseSequence: secondResponse.Sequence };
      await writeGateSignal(releasePath, release);
      await waitForGateSignal(releasedPath, { ...release, Action: 'released' }, assertGateHealthy);
      result.Checks.DeterministicCreateGateAndRealUIPolling = true;
      result.Checks.RealUIPolling = true;
      await complete(page, context, created.Id, 'completed', retainFailure);
      const objectResponse = await context.request.get(`/admin/v1/backups/${created.BackupId}`);
      requireValue(objectResponse.status() === 200, 'The generated object is unavailable.');
      const backup = (await objectResponse.json() as { Backup: Backup }).Backup;
      requireValue(backup.State === 'ready' && backup.Verified && /^[0-9a-f]{64}$/.test(backup.SHA256), 'The generated backup was not ready and verified.');
      const head = responseFor(page, `/admin/v1/backups/${backup.Id}/file`, 'HEAD');
      const downloadEvent = page.waitForEvent('download');
      await page.getByTestId(`backup-${backup.Id}`).getByRole('button', { name: 'Download', exact: true }).click();
      const prepared = await head;
      requireValue(prepared.status() === 200 && prepared.headers()['content-length'] === backup.SizeBytes && prepared.headers()['etag'] === `"${backup.SHA256}"`, 'Native download preparation did not bind the advertised bytes.');
      const download = await downloadEvent;
      requireValue(new URL(download.url()).pathname === `/admin/v1/backups/${backup.Id}/file` && !new URL(download.url()).search, 'The native attachment used an unexpected route.');
      await download.saveAs(fixture.ArchivePath);
      await fs.chmod(fixture.ArchivePath, 0o600);
      const archive = await fs.readFile(fixture.ArchivePath);
      requireValue(String(archive.length) === backup.SizeBytes && createHash('sha256').update(archive).digest('hex') === backup.SHA256, 'The downloaded archive differs from the real server descriptor.');
      await download.delete();
      result.Checks.NativeCreatePollHEADAndAttachment = true;
      await page.getByRole('button', { name: 'Import backup', exact: true }).click();
      const importing = page.getByRole('dialog', { name: 'Import backup', exact: true });
      await importing.getByLabel(/^Backup file/).setInputFiles(fixture.ArchivePath);
      const importedResponse = admissionFor(page, '/admin/v1/backups/import');
      await importing.getByRole('button', { name: 'Import backup', exact: true }).click();
      const imported = await admitted(importedResponse, context, recordAdmissionFallback);
      await complete(page, context, imported.Id, 'completed', retainFailure);
      const importedObject = await context.request.get(`/admin/v1/backups/${imported.BackupId}`);
      requireValue(importedObject.status() === 200, 'The imported object is unavailable.');
      const importedBackup = (await importedObject.json() as { Backup: Backup }).Backup;
      requireValue(!importedBackup.Verified && importedBackup.SHA256 === backup.SHA256, 'Import prematurely claimed verification or changed the archive.');
      result.BackupId = imported.BackupId;
      result.SHA256 = backup.SHA256;
      result.Checks.NativeImportRemainsUnverified = true;
      await capture('backup-recovery-desktop.png');
      await page.setViewportSize({ width: 390, height: 844 });
      await capture('backup-recovery-mobile.png');
      result.Checks.DesktopAndMobileWithoutOverflow = true;
    } else {
      let operation: Operation;
      if (fixture.Phase === 'restore') {
        requireValue(fixture.BackupId && /^[0-9a-f]{32}$/.test(fixture.BackupId), 'The imported archive identity is missing.');
        await page.getByTestId(`backup-${fixture.BackupId}`).getByRole('button', { name: 'Plan restore', exact: true }).click();
        const planning = page.getByRole('dialog', { name: 'Plan restore', exact: true });
        await planning.getByLabel(/^Passphrase/).fill(fixture.Passphrase);
        await planning.getByLabel('Restore saved defaults', { exact: true }).check();
        const plannedResponse = admissionFor(page, '/admin/v1/restores/plans');
        await planning.getByRole('button', { name: 'Plan restore', exact: true }).click();
        const planned = await admitted(plannedResponse, context, recordAdmissionFallback);
        const ready = await complete(page, context, planned.Id, 'ready', retainFailure);
        const readyReceipt = planned.State === 'ready' && planned.CanApply && result.AdmissionFallbacks?.some((value) =>
          value.Action === 'plan' && value.Scope === 'durable_request_id' && value.Resolved && value.OperationId === planned.Id);
        requireValue(ready.CanApply && /^[1-9][0-9]*$/.test(ready.Revision) &&
          (ready.Revision !== planned.Revision || readyReceipt), 'The staged plan did not expose a verified applicable revision.');
        const source = ready.Source;
        requireValue(source?.SchemaVersion === '23' && source.ServerName &&
          JSON.stringify(source.Tables.map((table) => table.Name).sort()) === JSON.stringify(fixture.ExpectedTables) &&
          source.Tables.every((table) => /^(0|[1-9][0-9]*)$/.test(table.Rows)), 'The real plan did not expose the exact validated table inventory.');
        await page.getByTestId(`operation-${ready.Id}`).getByRole('button', { name: 'Inspect plan', exact: true }).click();
        const review = page.getByRole('dialog');
        await expect(review.getByText(source.ServerName, { exact: true })).toBeVisible();
        await expect(review.getByRole('heading', { name: 'Included records', exact: true })).toBeVisible();
        await expect(review.getByText('Restore from backup', { exact: true })).toBeVisible();
        await review.getByRole('button', { name: 'Apply restore', exact: true }).click();
        await review.getByLabel('I understand that everyone must sign in again', { exact: true }).check();
        const appliedResponse = admissionFor(page, `/admin/v1/restores/${ready.Id}/apply`);
        await review.getByRole('button', { name: 'Apply restore', exact: true }).click();
        operation = await admitted(appliedResponse, context, recordAdmissionFallback, ready);
        result.Checks.NativePlanInspectAndConfirmedApply = true;
      } else {
        await page.getByRole('button', { name: 'Roll back', exact: true }).click();
        const rolling = page.getByRole('dialog', { name: 'Roll back', exact: true });
        await rolling.getByLabel('I understand that everyone must sign in again', { exact: true }).check();
        const rolledResponse = admissionFor(page, '/admin/v1/restores/rollback');
        await rolling.getByRole('button', { name: 'Roll back', exact: true }).click();
        operation = await admitted(rolledResponse, context, recordAdmissionFallback, undefined, reconnectAfterSwitch);
        result.Checks.NativeConfirmedRollback = true;
      }
      result.OperationId = operation.Id;
      await save();
      await reconnectAfterSwitch();
      await complete(page, context, operation.Id, 'completed', retainFailure);
      result.Checks.CompletedOperationAndRestoredPasswordLogin = true;
    }
    requireValue(!unsafeURL && pageErrors === 0, 'The real browser reported unsafe URLs or an uncaught application error.');
    result.Checks.CredentialFreeURLsAndNoPageErrors = true;
    result.Complete = true;
  } finally {
    page.off('pageerror', onPageError);
    page.off('request', onRequest);
    page.off('response', onOperationResponse);
    await failurePersistence?.catch(() => { /* Final persistence below retries the complete safe result. */ });
    await save();
  }
});
