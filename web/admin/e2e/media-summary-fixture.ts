import type { DashboardAPI } from './dashboard-fixture';
import { json, library, movieMetadata } from './dashboard-fixture';
import type { MediaTechnicalSummary } from '../src/mediaSummaryApi';
import { mediaInfoFixture, mediaSummaryFixture } from './media-summary-data';

export { audioStream, mediaInfoFixture, mediaSummaryFixture, subtitleStream, videoStream } from './media-summary-data';

export function installMediaSummaryFixture(api: DashboardAPI, summary: MediaTechnicalSummary = mediaSummaryFixture): void {
  api.handlers.set(`GET /admin/v1/libraries/${library.Id}/items`, async (route, request) => {
    const url = new URL(request.url());
    return json(route, { Library: library, Items: [{ ...movieMetadata.Item, ProductionYear: 2025, IndexNumber: null,
      ParentIndexNumber: null, HasOverrides: false, LockedFieldCount: 0, Media: summary }], TotalRecordCount: 1,
    StartIndex: Number(url.searchParams.get('StartIndex') ?? 0), Limit: Number(url.searchParams.get('Limit') ?? 50) });
  });
  api.handlers.set('GET /admin/v1/items/film-one/media-info', async (route) => json(route, { ...mediaInfoFixture, Summary: summary,
    ...(summary.State === 'available' ? {} : { Streams: [], Chapters: [], StreamCount: 0, ChapterCount: 0, ProbeVersion: null }) }));
}
