import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { validAnalysisConfiguration, validAnalysisDetection, validAnalysisItem, validAnalysisItems, validAnalysisOverview } from './mediaAnalysis';
import type { AnalysisAction, AnalysisConfiguration, AnalysisDetection, AnalysisItem, AnalysisItems, AnalysisOverview, AnalysisProfile, AnalysisPrune, AnalysisRunInput, AnalysisRunReceipt } from './mediaAnalysis';

function invalid(): ApiError { return new ApiError('The media analysis response is incomplete. Reload before making changes.', { code: 'invalid_response' }); }
export const mediaAnalysisApi = {
  async overview(options: RequestOptions = {}): Promise<AnalysisOverview> {
    const value = await authenticatedRequest<AnalysisOverview>('/media-analysis', options);
    if (!validAnalysisOverview(value)) throw invalid();
    return value;
  },
  async configure(Revision: string, Profile: AnalysisProfile, options: RequestOptions = {}): Promise<AnalysisConfiguration> {
    const value = await authenticatedRequest<AnalysisConfiguration>('/media-analysis/configuration', { ...options, method: 'PUT', body: { Revision, Profile } });
    if (!validAnalysisConfiguration(value)) throw invalid();
    return value;
  },
  async items(libraryId: string, search: string, start: number, options: RequestOptions = {}): Promise<AnalysisItems> {
    const query = new URLSearchParams({ StartIndex: String(start), Limit: '25' });
    if (libraryId) query.set('LibraryId', libraryId);
    if (search) query.set('SearchTerm', search);
    const value = await authenticatedRequest<AnalysisItems>(`/media-analysis/items?${query}`, options);
    if (!validAnalysisItems(value, start, 25) || libraryId && value.Items.some((item) => item.LibraryId !== libraryId)) throw invalid();
    return value;
  },
  async item(id: string, options: RequestOptions = {}): Promise<AnalysisItem> {
    const value = await authenticatedRequest<AnalysisItem>(`/media-analysis/items/${encodeURIComponent(id)}`, options);
    if (!validAnalysisItem(value) || value.Id !== id) throw invalid();
    return value;
  },
  async decide(item: AnalysisItem, Action: AnalysisAction, options: RequestOptions = {}): Promise<AnalysisDetection> {
    const { Revision, ManualRevision } = item.Detection;
    const SourceRevision = item.SourceRevision;
    const value = await authenticatedRequest<AnalysisDetection>(`/media-analysis/items/${encodeURIComponent(item.Id)}/decision`, { ...options, method: 'POST', body: { Revision, SourceRevision, ManualRevision, Action } });
    if (!validAnalysisDetection(value) || value.ItemId !== item.Id || value.SourceRevision !== SourceRevision) throw invalid();
    return value;
  },
  async start(input: AnalysisRunInput, options: RequestOptions = {}): Promise<AnalysisRunReceipt> {
    const value = await authenticatedRequest<AnalysisRunReceipt>('/media-analysis/runs', { ...options, method: 'POST', body: input });
    if (!value || typeof value.RunId !== 'string' || value.RunId === '' || typeof value.TaskId !== 'string' || value.TaskId === '' || typeof value.Admitted !== 'boolean') throw invalid();
    return value;
  },
  async prune(Revision: string, options: RequestOptions = {}): Promise<AnalysisPrune> {
    const value = await authenticatedRequest<AnalysisPrune>('/media-analysis/cache/prune', { ...options, method: 'POST', body: { Revision } });
    if (!value || !['RemovedEntries', 'RemovedBytes', 'RemainingBytes', 'BusyEntries'].every((key) => Number.isSafeInteger(value[key as keyof AnalysisPrune]) && value[key as keyof AnalysisPrune] >= 0)) throw invalid();
    return value;
  },
};
