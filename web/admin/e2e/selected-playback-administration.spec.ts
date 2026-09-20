import { expect, test as base } from '@playwright/test';
import type { BrowserContext, Locator, Page, Route } from '@playwright/test';
import type { MetadataDetail, MetadataValues } from '../src/api';
import type { UserConfiguration, UserPreferences } from '../src/userPreferencesApi';

// Every administrator API request is synthetic. Unexpected reads and writes
// are rejected instead of reaching the server that serves the browser assets.
const timestamp = '2026-09-20T00:00:00Z';
const csrfToken = 'synthetic-selected-playback-csrf';
const administrator = { Id: 'selected-admin', Name: 'Synthetic administrator', IsAdministrator: true, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const member = { Id: 'selected-member', Name: 'Selected playback member', IsAdministrator: false, IsDisabled: false, HasPassword: true, CreatedAt: timestamp };
const library = { Id: 'selected-library', Name: 'Selected playback library', CollectionType: 'movies', Paths: ['/synthetic/selected'], CreatedAt: timestamp, LastScanAt: timestamp };
const userPath = `/admin/v1/users/${member.Id}`;
const credentialsPath = `${userPath}/local-credentials`;
const preferencesPath = `${userPath}/preferences`;
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

function preferences(): UserPreferences {
  return {
    UserId: member.Id, Revision: '1', Configuration: {
      AudioLanguagePreference: 'en', SubtitleLanguagePreference: 'en', PlayDefaultAudioTrack: true,
      RememberAudioSelections: true, RememberSubtitleSelections: true, EnableNextEpisodeAutoPlay: true,
      HidePlayedInLatest: false, HidePlayedInMoreLikeThis: false, HidePlayedInSuggestions: false, DisplayMissingEpisodes: false,
      SubtitleMode: 'Default', ResumeRewindSeconds: 5, OrderedViews: [], LatestItemsExcludes: [], MyMediaExcludes: [],
      IntroSkipMode: 'None', EnableLocalPassword: true, ProfilePin: '',
    },
  };
}

async function json(route: Route, value: unknown, status = 200): Promise<void> {
  await route.fulfill({ status, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' }, body: JSON.stringify(value) });
}

class SelectedPlaybackAPI {
  user = { ...member };
  credentials: CredentialsStatus = { UserId: member.Id, Revision: '1', HasLocalPassword: true, HasProfilePin: true, EnableLocalPassword: true };
  preferences = preferences();
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
        if (captured.path === '/admin/v1/bootstrap') return json(route, { Initialized: true });
        if (captured.path === '/admin/v1/session') return json(route, { User: administrator, CSRFToken: csrfToken });
        if (captured.path === '/admin/v1/users') return json(route, { Items: [this.user], TotalRecordCount: 1 });
        if (captured.path === userPath) return json(route, { User: { ...this.user, Revision: '1', Policy: {
          EnableAllFolders: true, EnabledFolders: [], EnableMediaPlayback: true,
          EnablePlaybackRemuxing: true, EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
        } } });
        if (captured.path === credentialsPath) return json(route, this.credentials);
        if (captured.path === preferencesPath) return json(route, this.preferences);
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
      if (captured.method === 'PUT' && captured.path === preferencesPath) {
        const input = captured.body as { Revision: string; Configuration: Partial<UserConfiguration> };
        if (input.Revision !== this.preferences.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'Preferences changed. Reload before saving.' } }, 409);
        this.preferences = { ...this.preferences, Revision: (BigInt(this.preferences.Revision) + 1n).toString(), Configuration: { ...this.preferences.Configuration, ...input.Configuration } };
        return json(route, this.preferences);
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
  const dialog = page.getByRole('dialog', { name: 'Manage user', exact: true });
  await expect(dialog.getByRole('textbox', { name: /^Username/ })).toHaveValue(member.Name);
  return dialog;
}

async function openCredentials(page: Page, visit = true): Promise<Locator> {
  const user = visit ? await openUser(page) : page.getByRole('dialog', { name: 'Manage user', exact: true });
  await user.getByRole('button', { name: 'Manage local credentials', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Local credentials', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('checkbox', { name: 'Enable local password', exact: true })).toBeVisible();
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
    await expect(dialog.getByRole('alert').filter({ hasText: /^Profile PIN saved\b/ })).toBeVisible();
    await expect(dialog.getByRole('alert').filter({ hasText: sessionRevocationClaim })).toHaveCount(0);
  }
}

async function openIntro(page: Page): Promise<Locator> {
  await page.goto(`/admin/libraries/${library.Id}/items`);
  await page.getByRole('button', { name: 'Edit metadata for Selected playback movie', exact: true }).click();
  await page.getByRole('dialog', { name: /^Edit metadata/ }).getByRole('button', { name: 'Manage intro', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Intro interval', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Intro start (seconds)', { exact: true })).toBeVisible();
  return dialog;
}

async function saveIntro(page: Page, dialog: Locator, status = 200): Promise<void> {
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === introPath),
    dialog.getByRole('button', { name: 'Save intro', exact: true }).click(),
  ]);
  expect(response.status()).toBe(status);
  await expect(dialog.getByRole('button', { name: 'Reload', exact: true }).last()).toBeEnabled();
  if (status === 200) await expect(dialog.getByLabel('Intro start (seconds)', { exact: true })).toBeEnabled();
  else await expect(dialog.getByRole('alert').filter({ hasText: /changed|reload/i }).first()).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save intro', exact: true })).toBeDisabled();
}

test('local credential changes omit retained secrets and never refill saved secrets', async ({ page, api }) => {
  const dialog = await openCredentials(page);
  await expectEmptyCredentials(dialog);
  await dialog.getByRole('checkbox', { name: 'Enable local password', exact: true }).uncheck();
  await saveCredentials(page, dialog);
  await expectEmptyCredentials(dialog);
  expect(api.writes(credentialsPath).map((request) => request.body)).toEqual([{ Revision: '1', EnableLocalPassword: false }]);
  expect(api.credentials.HasLocalPassword).toBe(true);
  expect(api.credentials.HasProfilePin).toBe(true);

  const localPassword = 'synthetic-local-password';
  await dialog.getByLabel('New local password', { exact: true }).fill(localPassword);
  await dialog.getByLabel('Confirm local password', { exact: true }).fill(localPassword);
  await dialog.getByRole('checkbox', { name: 'Enable local password', exact: true }).check();
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
  await expect(dialog.getByRole('checkbox', { name: 'Enable local password', exact: true })).not.toBeChecked();
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
  await expect(dialog.getByRole('checkbox', { name: 'Enable local password', exact: true })).not.toBeChecked();
  await expectEmptyCredentials(dialog);
  await dialog.getByRole('checkbox', { name: 'Enable local password', exact: true }).check();
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

test('playback preferences save every intro mode and next episode choice without credential fields', async ({ page, api }) => {
  const user = await openUser(page);
  await user.getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Playback and display preferences', exact: true });
  const intro = dialog.getByRole('combobox', { name: 'Intro skipping', exact: true });
  const nextEpisode = dialog.getByRole('checkbox', { name: 'Automatically play the next episode', exact: true });
  await expect(intro).toHaveText('Off');
  await expect(nextEpisode).toBeChecked();
  const cases = [
    { label: 'Show skip button', mode: 'ShowButton', next: false },
    { label: 'Skip automatically', mode: 'AutoSkip', next: true },
    { label: 'Off', mode: 'None', next: false },
  ] as const;
  for (const [index, choice] of cases.entries()) {
    await intro.click();
    await page.getByRole('option', { name: choice.label, exact: true }).click();
    await nextEpisode.setChecked(choice.next);
    const save = dialog.getByRole('button', { name: 'Save preferences', exact: true });
    const [response] = await Promise.all([
      page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === preferencesPath),
      save.click(),
    ]);
    expect(response.status()).toBe(200);
    await expect(intro).toBeEnabled();
    await expect(save).toBeDisabled();
    expect(api.writes(preferencesPath).at(-1)?.body).toEqual({
      Revision: String(index + 1), Configuration: {
        AudioLanguagePreference: 'en', SubtitleLanguagePreference: 'en', PlayDefaultAudioTrack: true,
        RememberAudioSelections: true, RememberSubtitleSelections: true, SubtitleMode: 'Default', ResumeRewindSeconds: 5,
        HidePlayedInLatest: false, HidePlayedInMoreLikeThis: false, OrderedViews: [], LatestItemsExcludes: [], MyMediaExcludes: [],
        HidePlayedInSuggestions: false, DisplayMissingEpisodes: false,
        IntroSkipMode: choice.mode, EnableNextEpisodeAutoPlay: choice.next,
      },
    });
  }
  expect(api.writes(preferencesPath)).toHaveLength(cases.length);
});

test('discovery preferences save and reload missing episode and played suggestion choices', async ({ page, api }) => {
  const user = await openUser(page);
  await user.getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Playback and display preferences', exact: true });
  await dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Hide played items from Suggestions', exact: true }).check();
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === preferencesPath),
    dialog.getByRole('button', { name: 'Save preferences', exact: true }).click(),
  ]);
  expect(response.status()).toBe(200);
  await expect(dialog.getByRole('button', { name: 'Save preferences', exact: true })).toBeDisabled();
  expect(api.writes(preferencesPath)).toHaveLength(1);
  const input = api.writes(preferencesPath)[0].body as { Configuration: Record<string, unknown> };
  expect(input.Configuration).toMatchObject({ DisplayMissingEpisodes: true, HidePlayedInSuggestions: true, HidePlayedInLatest: false, HidePlayedInMoreLikeThis: false });
  expect(input.Configuration).not.toHaveProperty('ProfilePin');
  expect(input.Configuration).not.toHaveProperty('EnableLocalPassword');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await user.getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  await expect(dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true })).toBeChecked();
  await expect(dialog.getByRole('checkbox', { name: 'Hide played items from Suggestions', exact: true })).toBeChecked();
});

test('conflicted discovery preferences keep the draft and require reload before another write', async ({ page, api }) => {
  const user = await openUser(page);
  await user.getByRole('button', { name: 'Playback and display preferences', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Playback and display preferences', exact: true });
  const missing = dialog.getByRole('checkbox', { name: 'Display missing episodes', exact: true });
  await missing.check();
  api.preferences = { ...api.preferences, Revision: '9007199254740993' };
  const [response] = await Promise.all([
    page.waitForResponse((result) => result.request().method() === 'PUT' && new URL(result.url()).pathname === preferencesPath),
    dialog.getByRole('button', { name: 'Save preferences', exact: true }).click(),
  ]);
  expect(response.status()).toBe(409);
  await expect(missing).toBeChecked();
  await expect(missing).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Save preferences', exact: true })).toBeDisabled();
  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(missing).not.toBeChecked();
  await expect(missing).toBeEnabled();
  expect(api.writes(preferencesPath)).toHaveLength(1);
});

test('manual intro edits, JSON imports, and chapter resets bind every mutation to its source', async ({ page, api }) => {
  const dialog = await openIntro(page);
  const start = dialog.getByLabel('Intro start (seconds)', { exact: true });
  const end = dialog.getByLabel('Intro end (seconds)', { exact: true });
  await expect(start).toHaveValue('1');
  await expect(end).toHaveValue('6');
  await start.fill('2.5');
  await end.fill('12.75');
  await saveIntro(page, dialog);
  expect(api.writes(introPath).at(-1)?.body).toEqual({ Revision: '0', SourceRevision: 'source-revision-one', StartTicks: 25_000_000, EndTicks: 127_500_000, Provenance: 'Manual' });

  await dialog.getByLabel('Import intro JSON', { exact: true }).setInputFiles({
    name: 'intro.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ StartTicks: 50_000_000, EndTicks: 100_000_000 })),
  });
  await expect(start).toHaveValue('5');
  await expect(end).toHaveValue('10');
  expect(api.writes(introPath)).toHaveLength(1);
  await saveIntro(page, dialog);
  expect(api.writes(introPath).at(-1)?.body).toEqual({ Revision: '1', SourceRevision: 'source-revision-one', StartTicks: 50_000_000, EndTicks: 100_000_000, Provenance: 'Import' });

  await dialog.getByRole('button', { name: 'Reset to chapter markers', exact: true }).click();
  const confirmation = page.getByRole('dialog', { name: 'Reset intro override?', exact: true });
  await expect(confirmation).toBeVisible();
  expect(api.writes(introPath)).toHaveLength(2);
  await confirmation.getByRole('button', { name: 'Reset intro', exact: true }).click();
  await expect(start).toHaveValue('1');
  await expect(end).toHaveValue('6');
  expect(api.writes(introPath).map(({ method, body }) => ({ method, body }))).toEqual([
    { method: 'PUT', body: { Revision: '0', SourceRevision: 'source-revision-one', StartTicks: 25_000_000, EndTicks: 127_500_000, Provenance: 'Manual' } },
    { method: 'PUT', body: { Revision: '1', SourceRevision: 'source-revision-one', StartTicks: 50_000_000, EndTicks: 100_000_000, Provenance: 'Import' } },
    { method: 'DELETE', body: { Revision: '2', SourceRevision: 'source-revision-one' } },
  ]);
  expect(api.intro.Override).toBeNull();
  expect(api.intro.Effective?.Provenance).toBe('Chapter');
});

test('invalid intro imports preserve the existing draft and never write automatically', async ({ page, api }) => {
  const dialog = await openIntro(page);
  const start = dialog.getByLabel('Intro start (seconds)', { exact: true });
  const end = dialog.getByLabel('Intro end (seconds)', { exact: true });
  const file = dialog.getByLabel('Import intro JSON', { exact: true });
  const invalidInterval = 'The import must contain one interval with integer StartTicks and EndTicks within this media duration. An optional Provenance must be Import.';
  const cases = [
    { name: 'malformed.json', text: '{', error: 'Choose a JSON file containing one object with StartTicks and EndTicks.' },
    { name: 'array.json', text: JSON.stringify([{ StartTicks: 10_000_000, EndTicks: 60_000_000 }]), error: invalidInterval },
    { name: 'outside-duration.json', text: JSON.stringify({ StartTicks: 10_000_000, EndTicks: 1_200_000_001 }), error: invalidInterval },
  ];
  for (const [index, input] of cases.entries()) {
    const draftStart = `${index + 2}.5`;
    await start.fill(draftStart);
    await end.fill('12.75');
    const error = dialog.getByRole('alert').filter({ hasText: input.error });
    await expect(error).toHaveCount(0);
    await file.setInputFiles({ name: input.name, mimeType: 'application/json', buffer: Buffer.from(input.text) });
    await expect(error).toBeVisible();
    await expect(file).toBeEnabled();
    await expect(start).toHaveValue(draftStart);
    await expect(end).toHaveValue('12.75');
    await expect(dialog.getByRole('button', { name: 'Save intro', exact: true })).toBeEnabled();
    expect(api.writes()).toEqual([]);
  }
  await saveIntro(page, dialog);
  expect(api.writes(introPath).map((request) => request.body)).toEqual([
    { Revision: '0', SourceRevision: 'source-revision-one', StartTicks: 45_000_000, EndTicks: 127_500_000, Provenance: 'Manual' },
  ]);
});

test('a stale override beyond the current duration stays inactive while current chapter markers load', async ({ page, api }) => {
  api.intro = {
    ...api.intro, MediaSourceId: 'replacement-source', SourceRevision: 'source-revision-two', Revision: '3', DurationTicks: 100_000_000,
    Override: { StartTicks: 100_000_000, EndTicks: 200_000_000, Provenance: 'Manual' },
    OverrideSource: 'Manual', OverrideStale: true, LastEditedBy: administrator.Id, LastEditedAt: timestamp,
  };
  const dialog = await openIntro(page);
  const source = dialog.getByRole('region', { name: 'Current intro source', exact: true });
  await expect(source.getByText('Duration: 10 seconds', { exact: true })).toBeVisible();
  await expect(source.getByText('Active intro: 1\u20136 seconds', { exact: true })).toBeVisible();
  await expect(source.getByText('Explicit chapter markers: 1\u20136 seconds', { exact: true })).toBeVisible();
  await expect(source.getByText('Manual override: 10\u201320 seconds', { exact: true })).toBeVisible();
  await expect(dialog.getByRole('alert').filter({ hasText: 'belongs to an older media source and is inactive' })).toBeVisible();
  await expect(dialog.getByLabel('Intro start (seconds)', { exact: true })).toHaveValue('1');
  await expect(dialog.getByLabel('Intro end (seconds)', { exact: true })).toHaveValue('6');
  await expect(dialog.getByRole('button', { name: 'Save intro', exact: true })).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Reset to chapter markers', exact: true })).toBeEnabled();
  expect(api.writes()).toEqual([]);
});

test('a changed media source blocks intro writes and reload replaces the stale source token', async ({ page, api }) => {
  const dialog = await openIntro(page);
  const start = dialog.getByLabel('Intro start (seconds)', { exact: true });
  const end = dialog.getByLabel('Intro end (seconds)', { exact: true });
  await expect(start).toHaveValue('1');
  await start.fill('2');
  await end.fill('8');
  const chapter: IntroInterval = { StartTicks: 30_000_000, EndTicks: 90_000_000, Provenance: 'Chapter' };
  api.intro = { ...api.intro, MediaSourceId: 'replacement-source', SourceRevision: 'source-revision-two', Automatic: chapter, Effective: chapter };
  await saveIntro(page, dialog, 409);
  await expect(start).toHaveValue('2');
  await expect(end).toHaveValue('8');
  await expect(dialog.getByRole('button', { name: 'Reset to chapter markers', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Import intro JSON', { exact: true })).toBeDisabled();
  expect(api.writes(introPath)).toHaveLength(1);

  page.once('dialog', (prompt) => { void prompt.accept(); });
  await dialog.getByRole('button', { name: 'Reload', exact: true }).last().click();
  await expect(start).toHaveValue('3');
  await expect(end).toHaveValue('9');
  await start.fill('4');
  await end.fill('10');
  await saveIntro(page, dialog);
  expect(api.writes(introPath).map((request) => request.body)).toEqual([
    { Revision: '0', SourceRevision: 'source-revision-one', StartTicks: 20_000_000, EndTicks: 80_000_000, Provenance: 'Manual' },
    { Revision: '0', SourceRevision: 'source-revision-two', StartTicks: 40_000_000, EndTicks: 100_000_000, Provenance: 'Manual' },
  ]);
});
