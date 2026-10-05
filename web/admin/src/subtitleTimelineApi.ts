import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { decodeSubtitleTimelineDetail, decodeSubtitleTimelinePage, decodeSubtitleTimelineReceipt } from './subtitleTimeline';
import type { SubtitleTimelineDetail, SubtitleTimelinePage, SubtitleTimelineRunInput, SubtitleTimelineRunReceipt, SubtitleTimelineState } from './subtitleTimeline';

const invalid = () => new ApiError('字幕时间轴响应不完整，请重新加载后再操作。', { code: 'invalid_response' });
export const subtitleTimelineApi = {
  async get(itemId: string, options: RequestOptions = {}): Promise<SubtitleTimelineDetail> {
    const value = decodeSubtitleTimelineDetail(await authenticatedRequest<unknown>(`/items/${encodeURIComponent(itemId)}/subtitle-timelines`, options), itemId);
    if (!value) throw invalid();
    return value;
  },
  async items(input: { libraryId: string; search: string; state: SubtitleTimelineState | ''; start: number }, options: RequestOptions = {}): Promise<SubtitleTimelinePage> {
    const query = new URLSearchParams({ StartIndex: String(input.start), Limit: '25' });
    if (input.libraryId) query.set('LibraryId', input.libraryId);
    if (input.search) query.set('SearchTerm', input.search);
    if (input.state) query.set('State', input.state);
    const value = decodeSubtitleTimelinePage(await authenticatedRequest<unknown>(`/subtitle-timelines?${query}`, options), input.start, 25, input.libraryId);
    if (!value) throw invalid();
    return value;
  },
  async start(input: SubtitleTimelineRunInput, options: RequestOptions = {}): Promise<SubtitleTimelineRunReceipt> {
    const value = decodeSubtitleTimelineReceipt(await authenticatedRequest<unknown>('/media-analysis/runs', { ...options, method: 'POST', body: input }));
    if (!value) throw invalid();
    return value;
  },
};
