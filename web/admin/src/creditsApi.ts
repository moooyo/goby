import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { decodeItemCredits } from './credits';
import type { ItemCredits } from './credits';
export type { CreditsPoint, ItemCredits } from './credits';

export interface CreditsInput {
  Revision: string;
  SourceRevision: string;
  StartTicks: number;
  Provenance: 'Manual' | 'Import';
}

function decode(input: unknown, itemId: string): ItemCredits {
  const value = decodeItemCredits(input, itemId);
  if (!value) throw new ApiError('片尾标记响应不完整，请重新加载后再编辑。', { code: 'invalid_response' });
  return value;
}

export const creditsApi = {
  async get(itemId: string, options: RequestOptions = {}): Promise<ItemCredits> {
    return decode(await authenticatedRequest<ItemCredits>(`/items/${encodeURIComponent(itemId)}/credits`, options), itemId);
  },
  async update(itemId: string, input: CreditsInput): Promise<ItemCredits> {
    return decode(await authenticatedRequest<ItemCredits>(`/items/${encodeURIComponent(itemId)}/credits`, { method: 'PUT', body: input }), itemId);
  },
  async reset(itemId: string, input: Pick<CreditsInput, 'Revision' | 'SourceRevision'>): Promise<ItemCredits> {
    return decode(await authenticatedRequest<ItemCredits>(`/items/${encodeURIComponent(itemId)}/credits`, { method: 'DELETE', body: input }), itemId);
  },
};
