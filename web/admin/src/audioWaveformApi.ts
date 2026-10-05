import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { decodeAudioWaveformDetail, decodeAudioWaveformPage, decodeAudioWaveformReceipt } from './audioWaveform';
import type { AudioWaveformDetail, AudioWaveformPage, AudioWaveformRunInput, AudioWaveformRunReceipt, AudioWaveformState } from './audioWaveform';

const invalid = () => new ApiError('音轨波形响应不完整，请重新加载后再操作。', { code: 'invalid_response' });
export const audioWaveformApi = {
  async get(itemId: string, options: RequestOptions = {}): Promise<AudioWaveformDetail> {
    const value = decodeAudioWaveformDetail(await authenticatedRequest<unknown>(`/items/${encodeURIComponent(itemId)}/audio-waveforms`, options), itemId);
    if (!value) throw invalid();
    return value;
  },
  async items(input: { libraryId: string; search: string; state: AudioWaveformState | ''; start: number }, options: RequestOptions = {}): Promise<AudioWaveformPage> {
    const query = new URLSearchParams({ StartIndex: String(input.start), Limit: '25' });
    if (input.libraryId) query.set('LibraryId', input.libraryId);
    if (input.search) query.set('SearchTerm', input.search);
    if (input.state) query.set('State', input.state);
    const value = decodeAudioWaveformPage(await authenticatedRequest<unknown>(`/audio-waveforms/items?${query}`, options), input.start, 25, input.libraryId);
    if (!value) throw invalid();
    return value;
  },
  async start(input: AudioWaveformRunInput, options: RequestOptions = {}): Promise<AudioWaveformRunReceipt> {
    const value = decodeAudioWaveformReceipt(await authenticatedRequest<unknown>('/media-analysis/runs', { ...options, method: 'POST', body: input }));
    if (!value) throw invalid();
    return value;
  },
};
