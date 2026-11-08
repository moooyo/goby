import type { DashboardAPI } from './dashboard-fixture';
import { csrf, expect, json, movieMetadata, seriesLibrary, stamp, test } from './dashboard-fixture';
import { evidence, noOverflow } from './dashboard-review';
import type { SeriesOrderDetail, SeriesOrderPreview } from '../src/seriesOrderApi';

test.use({ serviceWorkers: 'block', trace: 'off', video: 'off', screenshot: 'off', preferences: { language: 'en', theme: 'light' } });
const seriesId = 'series-one';
const orderPath = `/admin/v1/series/${seriesId}/display-order`;
const sourceHash = 'a'.repeat(64);
const mapping = [
  { ProviderEpisodeId: '102', SeasonNumber: 2, EpisodeNumber: 1, GroupOrder: 0, EpisodeOrder: 0, Ordinal: 0, GroupName: 'DVD disc one' },
  { ProviderEpisodeId: '101', SeasonNumber: 1, EpisodeNumber: 1, GroupOrder: 0, EpisodeOrder: 1, Ordinal: 1, GroupName: 'DVD disc one' },
];

function installSeriesOrder(api: DashboardAPI, incomplete = false) {
  const metadata = structuredClone(movieMetadata);
  metadata.Item = { ...metadata.Item, Id: seriesId, LibraryId: seriesLibrary.Id, Type: 'Series', Name: 'Opening series', IsFolder: true, Path: '/media/series/Opening series' };
  metadata.Effective = { ...metadata.Effective, Name: 'Opening series', ProviderIds: { Tmdb: '50' } };
  metadata.Automatic = structuredClone(metadata.Effective);
  metadata.EditableFields.push('Status');
  api.handlers.set(`GET /admin/v1/items/${seriesId}/metadata`, (route) => json(route, metadata));
  api.handlers.set(`GET /admin/v1/items/${seriesId}/media-info`, (route) => json(route, {
    Id: seriesId, LibraryId: seriesLibrary.Id, ProbeVersion: null, UpdatedAt: stamp, Streams: [], Chapters: [], StreamCount: 0, ChapterCount: 0, StreamsTruncated: false, ChaptersTruncated: false,
    Summary: { State: 'not_applicable', DurationTicks: null, Size: null, Bitrate: null, Container: '', Video: null, Audio: null, Images: { Count: 0, Types: [] }, Analysis: { Intro: null, Credits: null, PreviewCount: 0, BackgroundPreview: null, AudioWaveform: null, SubtitleTimeline: null } },
  }));
  let saved: SeriesOrderDetail = { SeriesId: seriesId, SeriesName: 'Opening series', Revision: '9007199254741009', DisplayOrder: 'Aired', ProviderSeriesId: '50',
    GroupId: '', GroupName: '', MappingSHA256: '', Entries: [], Coverage: { PhysicalEpisodeCount: 2, ExpectedEpisodeCount: 0, UnmappedCount: 0, Unmapped: [] }, SourceMatches: true, FetchedAt: null, UpdatedAt: null };
  const preview: SeriesOrderPreview = { Revision: saved.Revision, DisplayOrder: 'Dvd', GroupId: 'dvd-one', GroupName: 'DVD release', MappingSHA256: sourceHash,
    Entries: mapping, Coverage: { PhysicalEpisodeCount: 2, ExpectedEpisodeCount: incomplete ? 1 : 0, UnmappedCount: incomplete ? 1 : 0,
      Unmapped: incomplete ? [{ Kind: 'expected', Id: 'missing-one', Name: 'Expected extra episode', SeasonNumber: 3, EpisodeNumber: 4 }] : [] } };
  api.handlers.set(`GET ${orderPath}`, (route) => json(route, saved));
  api.handlers.set(`GET ${orderPath}/groups`, (route) => json(route, { Items: [
    { Id: 'dvd-one', Name: 'DVD release', DisplayOrder: 'Dvd', EpisodeCount: 2, GroupCount: 1 },
    { Id: 'dvd-two', Name: 'Alternative DVD release', DisplayOrder: 'Dvd', EpisodeCount: 2, GroupCount: 1 },
  ] }));
  api.handlers.set(`GET ${orderPath}/groups/dvd-one`, (route) => json(route, preview));
  const commit = () => { saved = { ...saved, Revision: (BigInt(saved.Revision) + 1n).toString(), DisplayOrder: 'Dvd', GroupId: preview.GroupId,
    GroupName: preview.GroupName, MappingSHA256: preview.MappingSHA256, Entries: mapping, Coverage: preview.Coverage, FetchedAt: stamp, UpdatedAt: stamp }; };
  api.handlers.set(`PUT ${orderPath}`, (route, request) => {
    const body = request.postDataJSON() as { Revision: string };
    if (body.Revision !== saved.Revision) return json(route, { Error: { Code: 'revision_conflict', Message: 'The series order changed. Reload before editing.' } }, 409);
    commit(); return json(route, saved);
  });
  return { preview, commit, get saved() { return saved; } };
}

async function openSeries(page: import('@playwright/test').Page) {
  await page.goto(`/admin/media/libraries/${seriesLibrary.Id}/items`);
  await page.getByRole('button', { name: 'Edit metadata for Opening series', exact: true }).first().click();
  const panel = page.locator('.series-display-order');
  await expect(panel.getByRole('combobox', { name: 'Episode display order', exact: true })).toHaveValue('Aired');
  return panel;
}

test('series order requires an explicit provider group and saves its own exact revision without submitting metadata', async ({ page, api }, testInfo) => {
  const fixture = installSeriesOrder(api);
  const panel = await openSeries(page);
  await panel.getByRole('combobox', { name: 'Episode display order', exact: true }).selectOption('Dvd');
  await panel.getByRole('button', { name: 'Load provider groups', exact: true }).click();
  const groups = panel.getByRole('combobox', { name: 'Provider group', exact: true });
  await expect(groups).toHaveValue('');
  await expect(panel.getByRole('button', { name: 'Save episode order', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([]);
  await groups.selectOption('dvd-one');
  await expect(panel.getByRole('table')).toContainText('S02E01');
  await expect(panel.getByRole('table').getByRole('row').nth(1)).toContainText('S02E01');
  const positionHeader = panel.getByRole('columnheader', { name: 'Position', exact: true });
  await expect.poll(() => positionHeader.evaluate((header) => {
    const text = document.createRange();
    text.selectNodeContents(header);
    return { lines: text.getClientRects().length, overflow: header.scrollWidth > header.clientWidth };
  }), 'The compact order heading must remain one complete visible word.').toEqual({ lines: 1, overflow: false });
  await evidence(page, testInfo, 'series-order-dvd-preview');
  await panel.getByRole('button', { name: 'Save episode order', exact: true }).click();
  await expect(panel).toContainText('Episode order saved. Playback queues now use this order.');
  expect(api.writes()).toEqual([{ method: 'PUT', path: orderPath, body: { Revision: '9007199254741009', DisplayOrder: 'Dvd', GroupId: 'dvd-one', MappingSHA256: sourceHash }, csrf }]);
  expect(fixture.saved.Revision).toBe('9007199254741010');
  await panel.getByRole('button', { name: 'Reload order', exact: true }).click();
  await expect(panel.getByRole('combobox', { name: 'Episode display order', exact: true })).toHaveValue('Dvd');
  await expect(panel.getByRole('table').getByRole('row').nth(1)).toContainText('S02E01');
  await noOverflow(page);
  await evidence(page, testInfo, 'series-order-dvd-saved');
});

test('an incomplete group exposes the missing episode and cannot be saved', async ({ page, api }, testInfo) => {
  installSeriesOrder(api, true);
  const panel = await openSeries(page);
  await panel.getByRole('combobox', { name: 'Episode display order', exact: true }).selectOption('Dvd');
  await panel.getByRole('button', { name: 'Load provider groups', exact: true }).click();
  await panel.getByRole('combobox', { name: 'Provider group', exact: true }).selectOption('dvd-one');
  await expect(panel).toContainText('Expected extra episode');
  await expect(panel).toContainText('This group is incomplete');
  await expect(panel.getByRole('button', { name: 'Save episode order', exact: true })).toBeDisabled();
  await panel.getByRole('button', { name: 'Discard order changes', exact: true }).click();
  await expect(panel.getByRole('combobox', { name: 'Episode display order', exact: true })).toHaveValue('Aired');
  expect(api.writes()).toEqual([]);
  await evidence(page, testInfo, 'series-order-incomplete-group');
});

test('a lost save response blocks replay until a read confirms the stored order', async ({ page, api }, testInfo) => {
  const fixture = installSeriesOrder(api);
  const originalOrderRevision = fixture.saved.Revision;
  api.handlers.set(`PUT ${orderPath}`, async (route) => { fixture.commit(); await route.abort('failed'); });
  const panel = await openSeries(page);
  await panel.getByRole('combobox', { name: 'Episode display order', exact: true }).selectOption('Dvd');
  await panel.getByRole('button', { name: 'Load provider groups', exact: true }).click();
  await panel.getByRole('combobox', { name: 'Provider group', exact: true }).selectOption('dvd-one');
  await panel.getByRole('button', { name: 'Save episode order', exact: true }).click();
  await expect(panel).toContainText('the save result could not be confirmed');
  await expect(panel.getByRole('button', { name: 'Save episode order', exact: true })).toBeDisabled();
  expect(api.writes()).toEqual([{ method: 'PUT', path: orderPath, body: { Revision: originalOrderRevision, DisplayOrder: 'Dvd', GroupId: 'dvd-one', MappingSHA256: sourceHash }, csrf }]);
  page.once('dialog', (dialog) => dialog.accept());
  const readback = page.waitForResponse((response) => response.request().method() === 'GET'
    && new URL(response.url()).pathname === orderPath && response.status() === 200);
  await panel.getByRole('button', { name: 'Reload order', exact: true }).click();
  const confirmed: SeriesOrderDetail = await (await readback).json();
  expect(confirmed.Revision).toBe((BigInt(originalOrderRevision) + 1n).toString());
  await expect(panel.getByRole('combobox', { name: 'Episode display order', exact: true })).toBeEnabled();
  await expect(panel.getByRole('combobox', { name: 'Episode display order', exact: true })).toHaveValue('Dvd');
  await expect(panel).not.toContainText('the save result could not be confirmed');
  const sheet = page.getByRole('dialog', { name: /^Edit metadata/ });
  await expect(sheet.getByText('Reload episode order before saving metadata', { exact: true })).toHaveCount(0);
  await expect(sheet.locator('#metadata-Name-input')).toBeEnabled();
  await expect(panel.getByRole('button', { name: 'Save episode order', exact: true })).toBeDisabled();
  expect(api.writes()).toHaveLength(1);
  await evidence(page, testInfo, 'series-order-unknown-result-recovered');
});
