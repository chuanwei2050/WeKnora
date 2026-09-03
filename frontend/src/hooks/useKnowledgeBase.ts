import { ref, reactive } from "vue";
import { storeToRefs } from "pinia";
import { formatStringDate, kbFileTypeVerification } from "../utils/index";
import { MessagePlugin } from "tdesign-vue-next";
import {
  uploadKnowledgeFile,
  listKnowledgeFiles,
  getKnowledgeDetails,
  delKnowledgeDetails,
  getKnowledgeDetailsCon,
} from "@/api/knowledge-base/index";
import { knowledgeStore } from "@/stores/knowledge";
import { useUIStore } from "@/stores/ui";
import { useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '@/stores/auth';
import { buildKnowledgeUploadMetadata } from '@/utils/knowledge-upload-metadata';

export default function (knowledgeBaseId?: string) {
  const usemenuStore = knowledgeStore();
  const route = useRoute();
  const { t } = useI18n();
  const authStore = useAuthStore();
  const { cardList, total } = storeToRefs(usemenuStore);
  let moreIndex = ref(-1);
  const details = reactive({
    title: "",
    time: "",
    md: [] as any[],
    id: "",
    total: 0,
    type: "",
    source: "",
    channel: "",
    file_type: "",
    tag_id: "",
    description: "",
    summary_status: "",
    chunkLoading: false,
    chunkLoadError: "",
    detailLoadError: "",
  });
  let detailRequestId = 0;
  let activeDetailId = "";
	let listGeneration = 0;
	const inFlightPages = new Set<string>();
  const getKnowled = (
    query: { page: number; page_size: number; tag_id?: string; keyword?: string; file_type?: string; directory_id?: string; sort_by?: string; sort_order?: 'asc' | 'desc' } = { page: 1, page_size: 35 },
    kbId?: string,
  ): Promise<boolean> => {
    const targetKbId = kbId || knowledgeBaseId;
    if (!targetKbId) return Promise.resolve(false);
	if (query.page === 1) listGeneration += 1;
	const generation = listGeneration;
	const requestKey = `${generation}:${query.page}`;
	if (inFlightPages.has(requestKey)) return Promise.resolve(false);
	inFlightPages.add(requestKey);
    
    return listKnowledgeFiles(targetKbId, query)
      .then((result: any) => {
		if (generation !== listGeneration) return false;
        const { data, total: totalResult } = result;
    const cardList_ = data.map((entry: any) => {
      if (entry?.kind === 'directory' && entry.directory) {
        return {
          ...entry.directory,
          kind: 'directory',
          file_name: entry.directory.name,
          display_name: entry.directory.name,
          parse_status: '',
          file_type: '',
          isMore: false,
        };
      }
      const item = entry?.kind === 'document' && entry.document ? entry.document : entry;
      const rawName = item.file_name || item.title || item.source || t('knowledgeBase.untitledDocument')
      const dotIndex = rawName.lastIndexOf('.')
      const displayName = dotIndex > 0 ? rawName.substring(0, dotIndex) : rawName
      const fileTypeSource = item.file_type || (item.type === 'manual' ? 'MANUAL' : '')
      return {
        ...item,
        original_file_name: item.file_name,
        display_name: displayName,
        file_name: displayName,
        updated_at: formatStringDate(new Date(item.updated_at)),
        isMore: false,
        file_type: fileTypeSource ? String(fileTypeSource).toLocaleUpperCase() : '',
      }
    });
        
        if (query.page === 1) {
          cardList.value = cardList_;
        } else {
          cardList.value.push(...cardList_);
        }
        total.value = totalResult;
		return true;
      })
	  .catch(() => false)
	  .finally(() => { inFlightPages.delete(requestKey); });
  };
  const delKnowledge = (index: number, item: any, onSuccess?: () => void) => {
    cardList.value[index].isMore = false;
    moreIndex.value = -1;
    return delKnowledgeDetails(item.id)
      .then((result: any) => {
        if (result.success) {
          MessagePlugin.info(t('knowledgeBase.deleteSuccess'));
          if (onSuccess) {
            onSuccess();
          } else {
            getKnowled();
          }
          return true;
        } else {
          MessagePlugin.error(t('knowledgeBase.deleteFailed'));
          return false;
        }
      })
      .catch(() => {
        MessagePlugin.error(t('knowledgeBase.deleteFailed'));
        return false;
      });
  };
  const openMore = (index: number) => {
    moreIndex.value = index;
  };
  const onVisibleChange = (visible: boolean) => {
    if (!visible) {
      moreIndex.value = -1;
    }
  };
  const dispatchDuplicateFile = (kbId: string, error: unknown) => {
    window.dispatchEvent(new CustomEvent('knowledgeFileDuplicate', {
      detail: { kbId, error },
    }));
  };
  const requestMethod = (file: any, uploadInput: any, governanceEnabled = false) => {
    if (!(file instanceof File) || !uploadInput) {
      MessagePlugin.error(t('error.invalidFileType'));
      return;
    }
    
    if (kbFileTypeVerification(file)) {
      return;
    }
    
    // 获取当前知识库ID
    let currentKbId: string | undefined = (route.params as any)?.kbId as string;
    if (!currentKbId && typeof window !== 'undefined') {
      const match = window.location.pathname.match(/knowledge-bases\/([^/]+)/);
      if (match?.[1]) currentKbId = match[1];
    }
    if (!currentKbId) {
      currentKbId = knowledgeBaseId;
    }
    if (!currentKbId) {
      MessagePlugin.error(t('error.missingKbId'));
      return;
    }
    
    // 获取当前选中的分类ID（与文件夹选择保持一致）
    const uiStore = useUIStore();
    const tagIdToUpload = uiStore.uploadTargetTagId;
    
    const metadata = buildKnowledgeUploadMetadata(
      file.name,
      governanceEnabled,
      authStore.user?.role === 'member' ? 'member_contribution' : 'managed_upload',
    );
    uploadKnowledgeFile(currentKbId, { file, tag_id: tagIdToUpload, metadata })
      .then((result: any) => {
        if (result.success) {
          MessagePlugin.info(t('knowledgeBase.uploadSuccess'));
          window.dispatchEvent(new CustomEvent('knowledgeFileUploaded', {
            detail: { kbId: currentKbId }
          }));
          getKnowled({
            page: 1,
            page_size: 35,
            tag_id: uiStore.selectedTagId || undefined,
          }, currentKbId);
        } else {
          const errorMessage = result.error?.message || result.message || t('knowledgeBase.uploadFailed');
          if (result.code === 'duplicate_file') {
            dispatchDuplicateFile(currentKbId, result);
          } else {
            MessagePlugin.error(result.code === 'file_deleting'
              ? t('knowledgeBase.fileDeleting')
              : errorMessage);
          }
        }
        uploadInput.value.value = "";
      })
      .catch((err: any) => {
        const errorMessage = err.error?.message || err.message || t('knowledgeBase.uploadFailed');
        if (err.code === 'duplicate_file') {
          dispatchDuplicateFile(currentKbId, err);
        } else {
          MessagePlugin.error(err.code === 'file_deleting'
            ? t('knowledgeBase.fileDeleting')
            : errorMessage);
        }
        uploadInput.value.value = "";
      });
  };
  const getCardDetails = (item: any) => {
    const requestId = ++detailRequestId;
    activeDetailId = item.id;
    Object.assign(details, {
      title: "",
      time: "",
      md: [],
      id: "",
      total: 0,
      type: "",
      source: "",
      channel: "",
      file_type: "",
      tag_id: "",
      description: "",
      summary_status: "",
      chunkLoadError: "",
      detailLoadError: "",
    });
    getKnowledgeDetails(item.id)
      .then((result: any) => {
        if (requestId !== detailRequestId) return;
        if (result.success && result.data) {
          const { data } = result;
          Object.assign(details, {
            title: data.file_name || data.title || data.source || t('knowledgeBase.untitledDocument'),
            time: formatStringDate(new Date(data.updated_at)),
            id: data.id,
            type: data.type || 'file',
            source: data.source || '',
            channel: data.channel || '',
            file_type: data.file_type || '',
            tag_id: data.tag_id || '',
            description: data.description || '',
            summary_status: data.summary_status || '',
          });
        } else {
          details.detailLoadError = t('knowledgeBase.detailLoadFailed');
        }
      })
      .catch(() => {
        if (requestId === detailRequestId) {
          details.detailLoadError = t('knowledgeBase.detailLoadFailed');
        }
      });
    getfDetails(item.id, 1, requestId);
  };
  
  const getfDetails = (id: string, page: number, requestId = detailRequestId) => {
    if (requestId !== detailRequestId || id !== activeDetailId) return;
    details.chunkLoading = true;
    details.chunkLoadError = "";
    getKnowledgeDetailsCon(id, page)
      .then((result: any) => {
        if (requestId !== detailRequestId || id !== activeDetailId) return;
        if (result.success && result.data) {
          const { data, total: totalResult } = result;
          if (page === 1) {
            details.md = data;
          } else {
            details.md.push(...data);
          }
          details.total = totalResult;
        } else {
          details.chunkLoadError = t('knowledgeBase.detailLoadFailed');
          details.detailLoadError = t('knowledgeBase.detailLoadFailed');
        }
      })
      .catch((err: any) => {
        if (requestId !== detailRequestId || id !== activeDetailId) return;
        details.chunkLoadError = err?.message || t('knowledgeBase.chunkLoadFailed');
        details.detailLoadError = t('knowledgeBase.detailLoadFailed');
        console.error("[ChunkLoad] failed", {
          knowledgeId: id,
          page,
          error: err,
        });
      })
      .finally(() => {
        if (requestId === detailRequestId && id === activeDetailId) {
          details.chunkLoading = false;
        }
      });
  };
  return {
    cardList,
    moreIndex,
    getKnowled,
    details,
    delKnowledge,
    openMore,
    onVisibleChange,
    requestMethod,
    getCardDetails,
    total,
    getfDetails,
  };
}
