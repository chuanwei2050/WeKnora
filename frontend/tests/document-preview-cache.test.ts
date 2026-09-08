import { flushPromises, mount } from '@vue/test-utils';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import DocumentPreview from '../src/components/document-preview.vue';
import { buildPreviewCacheKey, buildPreviewContentRevision } from '../src/utils/documentPreviewCache';

const { previewKnowledgeFile, requestDocumentPreviewGeneration, getDocumentPreviewGenerationStatus, getGeneratedDocumentPreview } = vi.hoisted(() => ({
  previewKnowledgeFile: vi.fn(),
  requestDocumentPreviewGeneration: vi.fn(),
  getDocumentPreviewGenerationStatus: vi.fn(),
  getGeneratedDocumentPreview: vi.fn(),
}));

vi.mock('@/api/knowledge-base/index', () => ({
  previewKnowledgeFile,
  requestDocumentPreviewGeneration,
  getDocumentPreviewGenerationStatus,
  getGeneratedDocumentPreview,
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
    requestDocumentPreviewGeneration.mockResolvedValue({ data: { status: 'pending', error: '' } });
    getDocumentPreviewGenerationStatus.mockResolvedValue({ data: { status: 'failed', error: 'conversion stopped' } });
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:preview') });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() });
  });

  it('reloads the same knowledge when its content revision changes', async () => {
    const wrapper = mount(DocumentPreview, {
      props: {
        knowledgeId: 'knowledge-reloaded',
        fileType: 'pdf',
        fileName: 'report.pdf',
        fileSize: 7,
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

  it('does not download a large Office file into the browser', async () => {
    const wrapper = mount(DocumentPreview, {
      props: {
        knowledgeId: 'knowledge-large',
        fileType: 'docx',
        fileName: 'large.docx',
        fileSize: 504.7 * 1024 * 1024,
        parseStatus: 'failed',
        parseError: '',
        previewScope: 'partial',
        contentRevision: 'hash-large',
        active: true,
      },
      global: { mocks: { $t: (key: string) => key } },
    });
    await flushPromises();

    expect(previewKnowledgeFile).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain('preview.originalPartialPreviewHint');
    expect(requestDocumentPreviewGeneration).toHaveBeenCalledWith('knowledge-large', false);
    expect(getDocumentPreviewGenerationStatus).toHaveBeenCalledWith('knowledge-large', false);

    await wrapper.setProps({ previewScope: 'full' });
    await flushPromises();
    expect(requestDocumentPreviewGeneration).toHaveBeenCalledWith('knowledge-large', true);
    expect(getDocumentPreviewGenerationStatus).toHaveBeenCalledWith('knowledge-large', true);
    wrapper.unmount();
  });

  it('loads the generated partial PDF when the background task completes', async () => {
    getDocumentPreviewGenerationStatus.mockResolvedValue({ data: { status: 'completed', error: '' } });
    getGeneratedDocumentPreview.mockResolvedValue(new Blob(['pdf'], { type: 'application/pdf' }));
    const wrapper = mount(DocumentPreview, {
      props: {
        knowledgeId: 'knowledge-generated', fileType: 'docx', fileName: 'large.docx',
        fileSize: 300 * 1024 * 1024, previewScope: 'partial', contentRevision: 'hash-generated', active: true,
      },
      global: { mocks: { $t: (key: string) => key } },
    });
    await flushPromises();

    expect(getGeneratedDocumentPreview).toHaveBeenCalledWith('knowledge-generated', false);
    expect(wrapper.find('iframe').attributes('src')).toBe('blob:preview');
    wrapper.unmount();
  });
});
