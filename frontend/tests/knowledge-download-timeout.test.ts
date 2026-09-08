import { describe, expect, it, vi } from 'vitest';

const { getDown } = vi.hoisted(() => ({
  getDown: vi.fn().mockResolvedValue(new Blob(['file'])),
}));

vi.mock('../src/utils/request', () => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
  postUpload: vi.fn(),
  getDown,
}));

import { downKnowledgeDetails } from '../src/api/knowledge-base/index';

describe('downKnowledgeDetails', () => {
  it('does not abort a large download after the regular 30 second timeout', async () => {
    await downKnowledgeDetails('knowledge-large');

    expect(getDown).toHaveBeenCalledWith(
      '/api/v1/knowledge/knowledge-large/download',
      { timeout: 0 },
    );
  });
});
