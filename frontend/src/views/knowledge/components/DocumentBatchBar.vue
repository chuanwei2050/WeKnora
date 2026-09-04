<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import type { DocumentBatchAction, GovernanceRowAction } from './knowledge-governance-actions';
import FolderMoveCascader from './FolderMoveCascader.vue';
import type { FolderCascaderOption } from './document-folder-organization';

const props = defineProps<{
  count: number;
  actions: DocumentBatchAction[];
  loadingAction?: GovernanceRowAction | null;
  folderTargets?: FolderCascaderOption[];
  movingFolder?: boolean;
  documentCount?: number;
  directoryCount?: number;
  movingDirectory?: boolean;
  directoryTargets?: FolderCascaderOption[];
  directoryTargetsLoading?: boolean;
  canEdit?: boolean;
  canManage?: boolean;
}>();

const emit = defineEmits<{
  (e: 'clear'): void;
  (e: 'action', action: GovernanceRowAction): void;
  (e: 'move-folder', tagId: string): void;
  (e: 'move-directory', directoryId: string): void;
  (e: 'download-selection'): void;
  (e: 'delete-selection'): void;
}>();

const { t } = useI18n();

const nonDeleteActions = computed(() => props.actions.filter(item => item.action !== 'delete'));
const documentDeleteCount = computed(() => props.actions.find(item => item.action === 'delete')?.count || 0);
const deleteSelectionCount = computed(() => documentDeleteCount.value + (props.directoryCount || 0));
const canDeleteSelection = computed(() => (props.directoryCount || 0) > 0
  ? Boolean(props.canManage) && documentDeleteCount.value === (props.documentCount || 0)
  : documentDeleteCount.value > 0);

const actionLabelKeys: Record<GovernanceRowAction, string> = {
  submit: 'knowledgeBase.governanceSubmit',
  withdraw: 'knowledgeBase.governanceWithdraw',
  approve: 'knowledgeBase.governanceApprove',
  reject: 'knowledgeBase.governanceReject',
  delete: 'knowledgeBase.governanceDelete',
};
</script>

<template>
  <transition name="batch-bar-fade">
    <div v-if="count > 0" class="doc-batch-bar" role="region" :aria-label="t('knowledgeBase.selectedCount', { count })">
      <div class="batch-bar-info">
        <span class="batch-bar-count">{{ t('knowledgeBase.selectedCount', { count }) }}</span>
        <button class="batch-bar-link" type="button" @click="emit('clear')">
          {{ t('knowledgeBase.clearSelection') }}
        </button>
      </div>
      <div class="batch-bar-actions">
        <t-button v-if="canEdit" variant="outline" size="small" @click="emit('download-selection')">
          {{ t('knowledgeBase.downloadDocumentDirectory') }}
        </t-button>
        <FolderMoveCascader
          v-if="canEdit"
          :options="directoryTargets || []"
          :loading="directoryTargetsLoading"
          :disabled="Boolean(loadingAction) || Boolean(movingFolder)"
          placement="top-right"
          @select="(directoryId: string) => emit('move-directory', directoryId)"
        >
          <t-button
            theme="primary"
            variant="outline"
            size="small"
            :loading="movingDirectory"
            :disabled="Boolean(loadingAction) || Boolean(movingFolder)"
          >
            {{ t('knowledgeBase.moveToDocumentDirectory') }}（{{ count }}）
          </t-button>
        </FolderMoveCascader>
        <FolderMoveCascader
          v-if="folderTargets?.length"
          :options="folderTargets"
          placement="top-right"
          @select="(folderId: string) => emit('move-folder', folderId)"
        >
          <t-button
            theme="primary"
            variant="outline"
            size="small"
            :loading="movingFolder"
            :disabled="Boolean(loadingAction)"
          >
            {{ t('knowledgeBase.adjustKnowledgeCategory') }}（{{ count }}）
          </t-button>
        </FolderMoveCascader>
        <t-button
          v-for="item in nonDeleteActions"
          :key="item.action"
          :theme="item.action === 'delete' ? 'danger' : 'primary'"
          :variant="item.action === 'submit' || item.action === 'approve' ? 'base' : 'outline'"
          size="small"
          :loading="loadingAction === item.action"
          :disabled="Boolean(movingFolder) || (Boolean(loadingAction) && loadingAction !== item.action)"
          @click="emit('action', item.action)"
        >
          {{ t(actionLabelKeys[item.action]) }}（{{ item.count }}）
        </t-button>
        <t-button
          v-if="canDeleteSelection"
          theme="danger"
          variant="outline"
          size="small"
          :disabled="Boolean(movingFolder) || Boolean(loadingAction)"
          @click="emit('delete-selection')"
        >
          {{ t('knowledgeBase.governanceDelete') }}（{{ deleteSelectionCount }}）
        </t-button>
      </div>
    </div>
  </transition>
</template>

<style scoped lang="less">
.doc-batch-bar {
  position: sticky;
  bottom: 12px;
  align-self: center;
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 8px 12px 8px 16px;
  margin: 12px auto 4px;
  min-width: 320px;
  max-width: min(920px, calc(100% - 24px));
  background: var(--td-bg-color-container, #fff);
  border: 1px solid var(--td-component-border, #e7e7e7);
  border-radius: 999px;
  box-shadow:
    0 6px 20px rgba(0, 0, 0, 0.08),
    0 2px 6px rgba(0, 0, 0, 0.06);
  z-index: 5;
}

.batch-bar-info {
  display: flex;
  align-items: center;
  gap: 12px;
}

.batch-bar-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-primary, #232323);
}

.batch-bar-link {
  background: transparent;
  border: 0;
  padding: 2px 4px;
  font-size: 12px;
  color: var(--td-brand-color, #0052d9);
  cursor: pointer;
  border-radius: 4px;

  &:hover { background: var(--td-brand-color-1, #f0f6ff); }
}

.batch-bar-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  margin-left: auto;
}

.batch-bar-fade-enter-active,
.batch-bar-fade-leave-active {
  transition: transform 0.18s ease, opacity 0.18s ease;
}
.batch-bar-fade-enter-from,
.batch-bar-fade-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
</style>
