import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Route } from '@playwright/test';
import type { MetadataDetail, MetadataValues } from '../src/api';
import { providerConfigurationFixture } from './provider-configuration-fixture';
import { settings } from './dashboard-fixture';

// Every administrator API request is synthetic. Unexpected reads and writes
// are rejected instead of reaching the server that serves the browser assets.
const timestamp = '2026-09-20T00:00:00Z';
const csrfToken = 'synthetic-selected-playback-csrf';
const administrator = { Id: 'selected-admin', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const member = { Id: 'selected-member', Name: 'Selected playback member', IsAdministrator: false, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library = { Id: 'selected-library', Name: 'Selected playback library', CollectionType: 'movies', Paths: ['/synthetic/selected'], CreatedAt: timestamp, LastScanAt: timestamp };
const userPath = `/admin/v1/users/${member.Id}`;
const credentialsPath = `${userPath}/local-credentials`;
const itemPath = '/admin/v1/items/selected-movie';
const introPath = `${itemPath}/intro`;

interface CredentialsStatus {
  UserId: string; Revision: string; HasLocalPassword: boolean; HasProfilePin: boolean; EnableLocalPassword: boolean;
}
interface CredentialsInput {
  Revision: string; EnableLocalPassword: boolean; LocalPassword?: string; ProfilePin?: string;
}
interface IntroInterval { StartTicks: number; EndTicks: number; Provenance: 'Manual' | 'Import' | 'Chapter' }
interface IntroStatus {
  ItemId: string; MediaSourceId: string; SourceRevision: string; Revision: string; DurationTicks: number;
  Automatic: IntroInterval | null; Effective: IntroInterval | null; Override: IntroInterval | null;
  OverrideSource: string; OverrideStale: boolean; LastEditedBy: string; LastEditedAt: string | null;
}
interface IntroInput {
  Revision: string; SourceRevision: string; StartTicks?: number; EndTicks?: number; Provenance?: 'Manual' | 'Import';
}
interface CapturedRequest { method: string; path: string; body: unknown; csrf: string | undefined }

function metadata(): MetadataDetail {
  const values: MetadataValues = {
    Status: null, EndDate: null, AirsBeforeSeasonNumber: null, AirsAfterSeasonNumber: null, AirsBeforeEpisodeNumber: null,
    Name: 'Selected playback movie', SortName: '', Overview: '', OriginalTitle: '', OfficialRating: '', ProductionYear: 2026,
    PremiereDate: null, CommunityRating: null, ProviderIds: {}, Genres: [], Tags: [], Studios: [], People: [],
    IndexNumber: null, ParentIndexNumber: null, Album: '', Artists: [], AlbumArtists: [],
  };
  return {
    Item: { Id: 'selected-movie', LibraryId: library.Id, ParentId: '', ParentName: '', Name: values.Name, Type: 'Movie', Path: '/synthetic/selected/movie.mkv', IsFolder: false },
    Revision: '1', Automatic: { ...values }, Effective: { ...values }, Overrides: {}, LockedValues: {},
    LockedFields: [], EditableFields: ['Name', 'Overview'], InactiveFields: [], LastEditedBy: '', LastEditedAt: null,
  };
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

class SelectedPlaybackAPI {
  user = { ...member };
  credentials: CredentialsStatus = { UserId: member.Id, Revision: '1', HasLocalPassword: true, HasProfilePin: true, EnableLocalPassword: true };
  item = metadata();
  intro: IntroStatus = {
    ItemId: 'selected-movie', MediaSourceId: 'selected-source', SourceRevision: 'source-revision-one', Revision: '0', DurationTicks: 1_200_000_000,
    Automatic: { StartTicks: 10_000_000, EndTicks: 60_000_000, Provenance: 'Chapter' },
    Effective: { StartTicks: 10_000_000, EndTicks: 60_000_000, Provenance: 'Chapter' },
    Override: null, OverrideSource: '', OverrideStale: false, LastEditedBy: '', LastEditedAt: null,
  };
  requests: CapturedRequest[] = [];
  unexpected: string[] = [];

  writes(path?: string): CapturedRequest[] { return this.requests.filter((request) => request.method !== 'GET' && (!path || request.path === path)); }

  async install(context: BrowserContext): Promise<void> {
    await context.route(/\/admin\/v1(?:\/|\?|$)/, async (route, request) => {
      const url = new URL(request.url());
      const captured: CapturedRequest = {
        method: request.method(), path: url.pathname, body: request.postData() ? request.postDataJSON() as unknown : null,
        csrf: request.headers()['x-csrf-token'],
      };
      this.requests.push(captured);
      if (captured.method === 'GET') {
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true, UICulture: 'en-US' });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrfToken });
        if (captured.path === '/admin/v1/overview') return json(route, { Server: { Id: 'selected-server', Name: 'Synthetic server', Version: '0.1.0' }, Database: { Status: 'connected' }, Counts: { Users: 1, Libraries: 1 } });
        if (captured.path === '/admin/v1/system/status') return json(route, { Timestamp: timestamp, UptimeSeconds: 100, Host: { OS: 'linux', Architecture: 'amd64', CPUCount: 1 }, CPU: { UsagePercent: null, Load1: null, Load5: null, Load15: null }, Memory: { TotalBytes: null, UsedBytes: null, CachedBytes: null, SwapUsedBytes: null }, Storage: { TotalBytes: null, UsedBytes: null, Complete: true, Volumes: [] }, Transcoding: { Available: false, Active: null, Limit: null, HardwareActive: null, SoftwareActive: null } });
        if (captured.path === '/admin/v1/storage/library-directories') return json(route, { Items: [], TotalRecordCount: 0 });
        if (captured.path === '/admin/v1/backups/policy') return json(route, { Revision: '1', Enabled: false, Schedule: { Kind: 'daily', DayOfWeek: null, TimeOfDay: '03:00', Timezone: 'UTC' }, KeepCount: 7, IncludeImages: true, HasPassphrase: false, NextRunAt: null, Available: true, UnavailableReason: '', TaskId: '', LastRunAt: null, LastRunState: '', LastBackupId: '' });
        if (captured.path === '/admin/v1/settings') return json(route, settings());
        if (captured.path === '/admin/v1/users/activity') return json(route, { Items: (url.searchParams.get('UserIds') ?? '').split(',').filter(Boolean).map((Id) => ({ Id, LastLoginDate: null, LastActivityDate: null, LastPlayedDate: null, PlayedItemCount: 0 })), ObservedAt: timestamp });
        if (captured.path === '/admin/v1/providers/configuration') return json(route, providerConfigurationFixture());
        if (captured.path === '/admin/v1/users') return json(route, { Items: [this.user], TotalRecordCount: 1 });
        if (captured.path === userPath) return json(route, { User: { ...this.user, Revision: '1', Policy: {
          EnableAllFolders: true, EnabledFolders: [], EnableMediaPlayback: true,
          EnablePlaybackRemuxing: true, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
        } } });
        if (captured.path === credentialsPath) return json(route, this.credentials);
        if (captured.path === '/admin/v1/features') return json(route, { Items: [
          { Id: 'playback', Name: 'Playback', FeatureType: 'User' }, { Id: 'preferences', Name: 'Preferences', FeatureType: 'User' },
        ] });
        if (captured.path === '/admin/v1/libraries') return json(route, { Items: [library], TotalRecordCount: 1 });
        if (captured.path === `/admin/v1/libraries/${library.Id}/items`) return json(route, {
          Library: library, Items: [{ ...this.item.Item, ProductionYear: this.item.Effective.ProductionYear, IndexNumber: null, ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0 }],
          TotalRecordCount: 1, StartIndex: Number(url.searchParams.get('StartIndex')), Limit: Number(url.searchParams.get('Limit')),
        });
        if (captured.path === `${itemPath}/metadata`) return json(route, this.item);
        if (captured.path === introPath) return json(route, this.intro);
      }
      if (captured.method === 'PUT' && captured.path === credentialsPath) {
        const input = captured.body as CredentialsInput;
        if (input.Revision !== this.credentials.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Local credentials changed. Reload before saving.' } }, 409);
        this.credentials = {
          ...this.credentials, Revision: (BigInt(this.credentials.Revision) + 1n).toString(), EnableLocalPassword: input.EnableLocalPassword,
          ...(Object.hasOwn(input, 'LocalPassword') ? { HasLocalPassword: Boolean(input.LocalPassword) } : {}),
          ...(Object.hasOwn(input, 'ProfilePin') ? { HasProfilePin: Boolean(input.ProfilePin) } : {}),
        };
        return json(route, { Credentials: this.credentials, CurrentSessionRevoked: false });
      }
      if (['PUT', 'DELETE'].includes(captured.method) && captured.path === introPath) {
        const input = captured.body as IntroInput;
        if (input.Revision !== this.intro.Revision || input.SourceRevision !== this.intro.SourceRevision) return json(route, { Error: { Code: 'intro_revision_conflict', Message: 'The intro or media source changed. Reload before saving.' } }, 409);
        const override: IntroInterval | null = captured.method === 'DELETE' ? null : { StartTicks: input.StartTicks!, EndTicks: input.EndTicks!, Provenance: input.Provenance! };
        this.intro = {
          ...this.intro, Revision: (BigInt(this.intro.Revision) + 1n).toString(), Override: override, Effective: override ?? this.intro.Automatic,
          OverrideSource: override?.Provenance ?? '', OverrideStale: false, LastEditedBy: administrator.Id, LastEditedAt: timestamp,
        };
        return json(route, this.intro);
      }
      this.unexpected.push(`${captured.method} ${captured.path}${url.search}`);
      return json(route, { Error: { Code: 'unexpected_synthetic_request', Message: 'No synthetic response was configured.' } }, 501);
    });
  }
}

const test = base.extend<{ api: SelectedPlaybackAPI }>({
  api: async ({ context }, use) => {
    const api = new SelectedPlaybackAPI();
    await api.install(context);
    await use(api);
    expect(api.unexpected, 'Every administrator API request must be explicitly mocked.').toEqual([]);
    expect(api.writes().every((request) => request.csrf === csrfToken), 'Every mutation must carry the administrator CSRF token.').toBe(true);
  },
});

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off' });

async function openUser(page: Page): Promise<Locator> {
  await page.goto('/admin/users');
  await page.getByRole('button', { name: `Manage ${member.Name}`, exact: true }).click();
  const dialog = page.getByRole('dialog', { name: `Manage user ${member.Name}`, exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function openCredentials(page: Page, visit = true): Promise<Locator> {
  const user = visit ? await openUser(page) : page.getByRole('dialog', { name: `Manage user ${member.Name}`, exact: true });
  await user.getByRole('tab', { name: 'Password', exact: true }).click();
  await user.getByRole('button', { name: 'Manage local credentials', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Local credentials', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('switch', { name: 'Enable local password', exact: true })).toBeVisible();
  return dialog;
}

async function expectEmptyCredentials(dialog: Locator): Promise<void> {
  for (const label of ['New local password', 'Confirm local password', 'New profile PIN', 'Confirm profile PIN']) {
    const input = dialog.getByLabel(label, { exact: true });
    await expect(input).toHaveAttribute('type', 'password');
    await expect(input).toHaveValue('');
  }
}

async function saveCredentials(page: Page, dialog: Locator, options: { status?: number; profilePinOnly?: boolean } = {}): Promise<void> {
  const status = options.status ?? 200;
  const sessionRevocationClaim = /(?:sign-ins|sessions)[^.]* (?:will end|have ended|will be revoked|have been revoked)/i;
  await dialog.getByRole('button', { name: 'Save local credentials', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Save local credentials?', exact: true });
  await expect(confirmation).toBeVisible();
  if (options.profilePinOnly) {
    await expect(confirmation.getByRole('button', { name: 'Save and end sign-ins', exact: true })).toHaveCount(0);
    await expect(confirmation).not.toContainText(sessionRevocationClaim);
  }
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === credentialsPath),
    confirmation.getByRole('button', { name: options.profilePinOnly ? 'Save profile PIN' : 'Save and end sign-ins', exact: true }).click(),
  ]);
  expect(response.status()).toBe(status);
  await expect(confirmation).not.toBeVisible();
  if (status === 409) {
    await expect(dialog.getByRole('alert').filter({ hasText: /account changed|reload/i }).first()).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Reload', exact: true }).last()).toBeEnabled();
  }
  if (options.profilePinOnly && status === 200) {
    await expect(dialog.getByText('Profile PIN saved for compatible client profile locks.', { exact: true })).toBeVisible();
    await expect(dialog.getByRole('status').filter({ hasText: sessionRevocationClaim })).toHaveCount(0);
  }
}

test('local credential changes omit retained secrets and never refill saved secrets', async ({ page, api }) => {
  const dialog = await openCredentials(page);
  await expectEmptyCredentials(dialog);
  await dialog.getByRole('switch', { name: 'Enable local password', exact: true }).uncheck();
  await saveCredentials(page, dialog);
  await expectEmptyCredentials(dialog);
  expect(api.writes(credentialsPath).map((request) => request.body)).toEqual([{ Revision: '1', EnableLocalPassword: false }]);
  expect(api.credentials.HasLocalPassword).toBe(true);
  expect(api.credentials.HasProfilePin).toBe(true);

  const localPassword = 'synthetic-local-password';
  await dialog.getByLabel('New local password', { exact: true }).fill(localPassword);
  await dialog.getByLabel('Confirm local password', { exact: true }).fill(localPassword);
  await dialog.getByRole('switch', { name: 'Enable local password', exact: true }).check();
  await saveCredentials(page, dialog);
  await expectEmptyCredentials(dialog);
  expect(api.writes(credentialsPath).at(-1)?.body).toEqual({ Revision: '2', EnableLocalPassword: true, LocalPassword: localPassword });

  await dialog.getByLabel('New profile PIN', { exact: true }).fill('4826');
  await dialog.getByLabel('Confirm profile PIN', { exact: true }).fill('4826');
  await saveCredentials(page, dialog, { profilePinOnly: true });
  await expectEmptyCredentials(dialog);
  expect(api.writes(credentialsPath).at(-1)?.body).toEqual({ Revision: '3', EnableLocalPassword: true, ProfilePin: '4826' });
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expectEmptyCredentials(await openCredentials(page, false));
  expect(api.writes(credentialsPath)).toHaveLength(3);
});

test('clearing local credentials sends only the explicitly cleared secret', async ({ page, api }) => {
  const dialog = await openCredentials(page);
  await dialog.getByRole('button', { name: 'Clear local password', exact: true }).click();
  await expect(dialog.getByRole('switch', { name: 'Enable local password', exact: true })).not.toBeChecked();
  expect(api.writes()).toEqual([]);
  await saveCredentials(page, dialog);
  expect(api.writes(credentialsPath).at(-1)?.body).toEqual({ Revision: '1', EnableLocalPassword: false, LocalPassword: '' });
  expect(api.credentials.HasLocalPassword).toBe(false);
  expect(api.credentials.HasProfilePin).toBe(true);

  await dialog.getByRole('button', { name: 'Clear profile PIN', exact: true }).click();
  expect(api.writes()).toHaveLength(1);
  await saveCredentials(page, dialog, { profilePinOnly: true });
  expect(api.writes(credentialsPath).at(-1)?.body).toEqual({ Revision: '2', EnableLocalPassword: false, ProfilePin: '' });
  expect(api.credentials.HasProfilePin).toBe(false);
  await expectEmptyCredentials(dialog);
});

test('a local credential revision conflict blocks writes until the latest status is loaded', async ({ page, api }) => {
  const dialog = await openCredentials(page);
  await dialog.getByLabel('New profile PIN', { exact: true }).fill('4826');
  await dialog.getByLabel('Confirm profile PIN', { exact: true }).fill('4826');
  api.credentials = { ...api.credentials, Revision: '2', EnableLocalPassword: false };
  await saveCredentials(page, dialog, { status: 409, profilePinOnly: true });
  await expect(dialog.getByRole('button', { name: 'Save local credentials', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Clear local password', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Clear profile PIN', exact: true })).toBeDisabled();
  expect(api.writes(credentialsPath)).toHaveLength(1);

  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(dialog.getByRole('switch', { name: 'Enable local password', exact: true })).not.toBeChecked();
  await expectEmptyCredentials(dialog);
  await dialog.getByRole('switch', { name: 'Enable local password', exact: true }).check();
  await saveCredentials(page, dialog);
  expect(api.writes(credentialsPath).at(-1)?.body).toEqual({ Revision: '2', EnableLocalPassword: true });
  expect(api.writes(credentialsPath)).toHaveLength(2);
});

test('a profile PIN requires four ASCII digits and its save never claims to end sign-ins', async ({ page, api }) => {
  const dialog = await openCredentials(page);
  const pin = dialog.getByLabel('New profile PIN', { exact: true });
  const confirmation = dialog.getByLabel('Confirm profile PIN', { exact: true });
  const save = dialog.getByRole('button', { name: 'Save local credentials', exact: true });
  await expect(pin).toHaveAttribute('maxlength', '4');
  await expect(confirmation).toHaveAttribute('maxlength', '4');
  for (const invalid of ['123', '12a4', '\uFF11\uFF12\uFF13\uFF14']) {
    await pin.fill(invalid);
    await confirmation.fill(invalid);
    await expect(save).toBeDisabled();
  }
  expect(api.writes()).toEqual([]);
  await pin.fill('1234');
  await pin.press('End');
  await pin.press('5');
  await expect(pin).toHaveValue('1234');
  await pin.fill('4826');
  await confirmation.fill('4826');
  await saveCredentials(page, dialog, { profilePinOnly: true });
  await expectEmptyCredentials(dialog);
  await expect(page.getByRole('heading', { name: 'Sign in to Goby', exact: true })).toHaveCount(0);
  expect(api.writes(credentialsPath).map((request) => request.body)).toEqual([{ Revision: '1', EnableLocalPassword: true, ProfilePin: '4826' }]);
});

test('a user without a normal account password cannot enter a new profile PIN', async ({ page, api }) => {
  api.user.HasPassword = false;
  api.credentials = { ...api.credentials, HasLocalPassword: false, HasProfilePin: false, EnableLocalPassword: false };
  const dialog = await openCredentials(page);
  await expect(dialog.getByLabel('New profile PIN', { exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Confirm profile PIN', { exact: true })).toBeDisabled();
  await expect(dialog.getByText('Set a normal account password before setting a profile PIN.', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save local credentials', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
});

test('metadata editing has no manual intro correction flow and leaves stored intervals unchanged', async ({ page, api }) => {
  const retained = structuredClone(api.intro);
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: 'Edit metadata for Selected playback movie', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: /^Edit metadata/ });
  await expect(dialog.getByRole('tab', { name: 'Images', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Manage intro', exact: true })).toHaveCount(0);
  await expect(page.getByRole('dialog', { name: 'Intro interval', exact: true })).toHaveCount(0);
  expect(api.writes(introPath)).toHaveLength(0);
  expect(api.intro).toEqual(retained);
});
