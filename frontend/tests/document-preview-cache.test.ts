import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import DocumentPreview from '../src/components/document-preview.vue';
import { buildPreviewCacheKey, buildPreviewContentRevision } from '../src/utils/documentPreviewCache';

const { previewKnowledgeFile } = vi.hoisted(() => ({
  previewKnowledgeFile: vi.fn(),
}));

vi.mock('@/api/knowledge-base/index', () => ({
  previewKnowledgeFile,
}));

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

describe('buildPreviewCacheKey', () => {
  it('changes when the authoritative content revision changes', () => {
    const first = buildPreviewCacheKey('knowledge-1', 'PDF', 'hash-1');
    const second = buildPreviewCacheKey('knowledge-1', 'PDF', 'hash-2');

    expect(first).not.toBe(second);
  });

  it('normalizes the file type without discarding the content revision', () => {
    expect(buildPreviewCacheKey('knowledge-1', 'PDF', 'version-1'))
      .toBe(buildPreviewCacheKey('knowledge-1', 'pdf', 'version-1'));
  });
});

describe('buildPreviewContentRevision', () => {
  it('changes when a pending governed version changes', () => {
    const base = { fileHash: 'hash', currentVersionId: 'current', updatedAt: 'time' };

    expect(buildPreviewContentRevision({ ...base, pendingVersionId: 'pending-1' }))
      .not.toBe(buildPreviewContentRevision({ ...base, pendingVersionId: 'pending-2' }));
  });
});

describe('DocumentPreview', () => {
  beforeEach(() => {
    previewKnowledgeFile.mockReset();
    previewKnowledgeFile.mockResolvedValue(new Blob(['preview'], { type: 'application/pdf' }));
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:preview') });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() });
  });

  it('reloads the same knowledge when its content revision changes', async () => {
    const wrapper = mount(DocumentPreview, {
      props: {
        knowledgeId: 'knowledge-reloaded',
        fileType: 'pdf',
        fileName: 'report.pdf',
        contentRevision: 'hash-1',
        active: true,
      },
      global: {
        mocks: { $t: (key: string) => key },
      },
    });
    await flushPromises();
    expect(previewKnowledgeFile).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ contentRevision: 'hash-2' });
    await flushPromises();
    expect(previewKnowledgeFile).toHaveBeenCalledTimes(2);

    wrapper.unmount();
  });
});
