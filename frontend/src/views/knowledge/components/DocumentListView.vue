<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { formatFileSize, getFileIcon } from '@/utils/files';
import FolderMoveCascader from './FolderMoveCascader.vue';
import type { FolderCascaderOption } from './document-folder-organization';
import {
  canOperateGovernanceRow,
  getGovernanceRowActions,
  isKnowledgeDeleteDisabled,
  isGovernanceRowActionDisabled,
  type GovernanceRowAction,
} from './knowledge-governance-actions';

interface Tag {
  id: string;
  name: string;
  color?: string;
}

interface KnowledgeItem {
  id: string;
  file_name: string;
  file_type?: string;
  file_size?: number | string;
  type?: string;
  tag_id?: string | number;
  parse_status?: string;
  summary_status?: string;
  updated_at?: string;
  source?: string;
  isMore?: boolean;
  created_by?: string;
  current_version_id?: string;
  pending_version_id?: string;
}

type DocumentAction = 'edit' | 'reparse' | 'delete' | 'submit' | 'withdraw' | 'approve' | 'reject';

const props = defineProps<{
  items: KnowledgeItem[];
  selectedIds: Set<string>;
  canEdit: boolean;
  canManage?: boolean;
  tagList: Tag[];
  folderTargets: FolderCascaderOption[];
  loading?: boolean;
  canGenerateSummary?: boolean;
  governanceEnabled?: boolean;
  canContribute?: boolean;
  canReview?: boolean;
  currentUserId?: string;
  governanceBusyId?: string;
}>();

const emit = defineEmits<{
  (e: 'open', item: KnowledgeItem): void;
  (e: 'toggle-row', id: string, checked: boolean, shiftKey: boolean, selectableIds: string[]): void;
  (e: 'toggle-all', checked: boolean, selectableIds: string[]): void;
  (e: 'action', action: DocumentAction, item: KnowledgeItem): void;
  (e: 'move-folder', item: KnowledgeItem, tagId: string): void;
}>();

const { t } = useI18n();

const tagMap = computed(() => {
  const map: Record<string, Tag> = {};
  for (const tag of props.tagList) map[String(tag.id)] = tag;
  return map;
});
const getTagName = (tagId?: string | number) => {
  if (!tagId && tagId !== 0) return '';
  return tagMap.value[String(tagId)]?.name || '';
};

const formatTime = (time?: string) => {
  if (!time) return '--';
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) return '--';
  const yy = String(d.getFullYear()).slice(2);
  const MM = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${yy}-${MM}-${dd} ${hh}:${mm}`;
};

const getTypeLabel = (item: KnowledgeItem) => {
  if (item.type === 'url') return 'URL';
  if (item.type === 'manual') return t('knowledgeBase.typeManual');
  if (item.file_type) return item.file_type.toUpperCase();
  return '--';
};

interface StatusInfo {
  label: string;
  theme: 'success' | 'warning' | 'danger' | 'primary' | 'default';
  icon?: string;
  spin?: boolean;
}
const computeStatus = (item: KnowledgeItem): StatusInfo => {
  if (item.parse_status === 'draft') {
    return { label: t('knowledgeBase.statusDraft'), theme: 'warning' };
  }
  if (item.parse_status === 'pending_review') {
    return { label: t('knowledgeBase.statusPendingReview'), theme: 'warning' };
  }
  if (item.parse_status === 'pending' || item.parse_status === 'processing') {
    return { label: t('knowledgeBase.statusProcessing'), theme: 'primary', icon: 'loading', spin: true };
  }
  if (item.parse_status === 'rejected') {
    return { label: t('knowledgeBase.statusRejected'), theme: 'danger', icon: 'close-circle' };
  }
  if (item.parse_status === 'failed') {
    return { label: t('knowledgeBase.statusFailed'), theme: 'danger', icon: 'close-circle' };
  }
  if (
    item.parse_status === 'completed' &&
    props.canGenerateSummary !== false &&
    (item.summary_status === 'pending' || item.summary_status === 'processing')
  ) {
    return { label: t('knowledgeBase.generatingSummary'), theme: 'primary', icon: 'loading', spin: true };
  }
  if (item.parse_status === 'completed') {
    return { label: t('knowledgeBase.statusCompleted'), theme: 'success' };
  }
  return { label: '--', theme: 'default' };
};

const statusByRow = computed(() => {
  const map = new Map<string, StatusInfo>();
  for (const item of props.items) map.set(item.id, computeStatus(item));
  return map;
});

// Show the actions column whenever the user can manage documents or participate in governance.
const showActions = computed(() => props.canEdit || props.canManage || props.canContribute || props.canReview);
const canManage = computed(() => Boolean(props.canManage));
const governanceContext = () => ({
  enabled: Boolean(props.governanceEnabled),
  canContribute: Boolean(props.canContribute),
  canReview: Boolean(props.canReview),
  currentUserId: props.currentUserId || '',
});
const deleteOptions = () => ({
  canManage: Boolean(props.canManage),
  currentUserId: props.currentUserId || '',
});
const governanceActions = (item: KnowledgeItem) => getGovernanceRowActions(item, governanceContext());
const hasGovernanceAction = (item: KnowledgeItem, action: GovernanceRowAction) => governanceActions(item).includes(action);
const canOperateItem = (item: KnowledgeItem) => props.canEdit || canOperateGovernanceRow(item, governanceContext());
const selectableIds = computed(() => props.items.filter(canOperateItem).map(item => item.id));
const allSelected = computed(() => (
  selectableIds.value.length > 0 && selectableIds.value.every(id => props.selectedIds.has(id))
));
const someSelected = computed(() => (
  selectableIds.value.some(id => props.selectedIds.has(id)) && !allSelected.value
));

const onHeaderToggle = (e: Event) => {
  const checked = (e.target as HTMLInputElement).checked;
  emit('toggle-all', checked, selectableIds.value);
};
const onRowToggle = (item: KnowledgeItem, e: MouseEvent) => {
  if (!canOperateItem(item)) return;
  const checked = !props.selectedIds.has(item.id);
  emit('toggle-row', item.id, checked, e.shiftKey, selectableIds.value);
};

const handleAction = (action: DocumentAction, item: KnowledgeItem) => {
  item.isMore = false;
  emit('action', action, item);
};
</script>

<template>
  <div class="doc-list-view" :class="{ 'is-loading': loading }" :style="{ '--doc-list-actions-width': showActions ? (canEdit ? '320px' : '220px') : '0px' }">
    <div class="doc-list-header" role="row">
      <div class="cell cell-check" role="columnheader">
        <label class="checkbox-wrap" @click.stop>
          <input
            type="checkbox"
            :checked="allSelected"
            :indeterminate.prop="someSelected"
            :disabled="!selectableIds.length"
            @change="onHeaderToggle"
            :aria-label="t('knowledgeBase.selectAll')"
          />
        </label>
      </div>
      <div class="cell cell-name" role="columnheader">{{ t('knowledgeBase.columnName') }}</div>
      <div class="cell cell-tag" role="columnheader">{{ t('knowledgeBase.columnTag') }}</div>
      <div class="cell cell-size" role="columnheader">{{ t('knowledgeBase.columnSize') }}</div>
      <div class="cell cell-type" role="columnheader">{{ t('knowledgeBase.columnType') }}</div>
      <div class="cell cell-status" role="columnheader">{{ t('knowledgeBase.columnStatus') }}</div>
      <div class="cell cell-time" role="columnheader">{{ t('knowledgeBase.columnUpdatedAt') }}</div>
      <div class="cell cell-actions" role="columnheader" v-if="showActions">{{ t('knowledgeBase.columnActions') }}</div>
    </div>

    <div class="doc-list-body">
      <div
        v-for="item in items"
        :key="item.id"
        class="doc-list-row"
        :class="{ selected: selectedIds.has(item.id) }"
        role="row"
        @click="emit('open', item)"
      >
        <div class="cell cell-check" @click.stop>
          <label class="checkbox-wrap">
            <input
              type="checkbox"
              :checked="selectedIds.has(item.id)"
              :disabled="!canOperateItem(item)"
              @click="onRowToggle(item, $event as unknown as MouseEvent)"
              :aria-label="item.file_name"
            />
          </label>
        </div>

        <div class="cell cell-name">
          <t-icon :name="getFileIcon(item)" class="row-file-icon" />
          <span class="row-file-name" :title="item.file_name">{{ item.file_name }}</span>
        </div>


        <div class="cell cell-tag">
          <t-tag v-if="getTagName(item.tag_id)" size="small" variant="light-outline" class="row-tag">
            {{ getTagName(item.tag_id) }}
          </t-tag>
          <span v-else class="row-muted">--</span>
        </div>

        <div class="cell cell-size">
          <span class="row-mono">{{ formatFileSize(item.file_size) || '--' }}</span>
        </div>

        <div class="cell cell-type">
          <span class="row-mono">{{ getTypeLabel(item) }}</span>
        </div>

        <div class="cell cell-status">
          <template v-if="statusByRow.get(item.id) as StatusInfo | undefined">
            <t-tag
              v-if="statusByRow.get(item.id)!.label !== '--'"
              size="small"
              :theme="statusByRow.get(item.id)!.theme"
              variant="light"
              class="row-status-tag"
            >
              <template v-if="statusByRow.get(item.id)!.icon" #icon>
                <t-icon
                  :name="statusByRow.get(item.id)!.icon!"
                  :class="{ 'icon-spin': statusByRow.get(item.id)!.spin }"
                />
              </template>
              {{ statusByRow.get(item.id)!.label }}
            </t-tag>
            <span v-else class="row-muted">--</span>
          </template>
        </div>

        <div class="cell cell-time">
          <span class="row-mono">{{ formatTime(item.updated_at) }}</span>
        </div>

        <div class="cell cell-actions" v-if="showActions" @click.stop>
          <div class="row-inline-actions">
            <button v-if="hasGovernanceAction(item, 'submit')" class="row-action-btn primary" type="button" :disabled="governanceBusyId === item.id" @click="handleAction('submit', item)">
              {{ t('knowledgeBase.governanceSubmit') }}
            </button>
            <button v-if="hasGovernanceAction(item, 'withdraw')" class="row-action-btn primary" type="button" :disabled="governanceBusyId === item.id" @click="handleAction('withdraw', item)">
              {{ t('knowledgeBase.governanceWithdraw') }}
            </button>
            <button v-if="hasGovernanceAction(item, 'approve')" class="row-action-btn primary" type="button" :disabled="governanceBusyId === item.id" @click="handleAction('approve', item)">
              {{ t('knowledgeBase.governanceApprove') }}
            </button>
            <button v-if="hasGovernanceAction(item, 'reject')" class="row-action-btn danger" type="button" :disabled="governanceBusyId === item.id" @click="handleAction('reject', item)">
              {{ t('knowledgeBase.governanceReject') }}
            </button>
            <button v-if="hasGovernanceAction(item, 'delete')" class="row-action-btn danger" type="button" :disabled="governanceBusyId === item.id || isGovernanceRowActionDisabled(item, 'delete', deleteOptions())" @click="handleAction('delete', item)">
              {{ t('knowledgeBase.governanceDelete') }}
            </button>
            <button v-if="canEdit && item.type === 'manual'" class="row-action-btn" type="button" @click="handleAction('edit', item)">
              {{ t('knowledgeBase.rowEdit') }}
            </button>
            <button v-if="canEdit && item.parse_status !== 'pending_review'" class="row-action-btn" type="button" @click="handleAction('reparse', item)">
              {{ t('knowledgeBase.rowRebuild') }}
            </button>
            <FolderMoveCascader
              v-if="canEdit && folderTargets.length"
              :options="folderTargets"
              @select="(folderId: string) => emit('move-folder', item, folderId)"
            >
              <button class="row-action-btn" type="button">
                {{ t('knowledgeBase.rowMove') }}
              </button>
            </FolderMoveCascader>
            <button
              v-if="canManage && !hasGovernanceAction(item, 'delete')"
              class="row-action-btn danger"
              type="button"
              :disabled="isKnowledgeDeleteDisabled(item, deleteOptions())"
              @click="handleAction('delete', item)"
            >
              {{ t('knowledgeBase.governanceDelete') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped lang="less">
.doc-list-view {
  display: flex;
  flex-direction: column;
  width: 100%;
  background: var(--td-bg-color-container, #fff);
  border: 1px solid var(--td-component-stroke, #f0f0f0);
  border-radius: 8px;
  overflow: hidden;
}

.doc-list-header,
.doc-list-row {
  display: grid;
  grid-template-columns:
    44px                       // checkbox
    minmax(220px, 2.4fr)       // name
    minmax(100px, 0.9fr)       // tag
    96px                       // size
    72px                       // type
    minmax(96px, 0.7fr)        // status
    140px                      // updated_at
    var(--doc-list-actions-width); // actions
  align-items: center;
  column-gap: 0;
  padding: 0 12px;
}

.doc-list-header {
  position: sticky;
  top: 0;
  z-index: 2;
  height: 36px;
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--td-text-color-placeholder, #a6a6a6);
  background: var(--td-bg-color-page, #fafbfc);
  border-bottom: 1px solid var(--td-component-stroke, #f0f0f0);
}

.doc-list-body {
  display: flex;
  flex-direction: column;
}

.doc-list-row {
  position: relative;
  height: 48px;
  font-size: 13px;
  color: var(--td-text-color-primary, #232323);
  border-bottom: 1px solid var(--td-component-stroke, #f3f3f3);
  cursor: pointer;
  transition: background-color 0.12s ease, box-shadow 0.12s ease;

  &:last-child { border-bottom: 0; }

  &:hover:not(.selected) {
    background: var(--td-bg-color-page, #f7f8fa);
  }

  &.selected {
    background: var(--td-brand-color-1, #f2f5fc);
    box-shadow: inset 3px 0 0 var(--td-brand-color, #0052d9);

    // brand-color-light alias maps back to brand-color-1, so a plain
    // var() swap produces no visible hover delta. Mix in a touch of
    // brand-color so the hover state is perceptible in both light and
    // dark themes without falling back to the saturated brand-color-2.
    &:hover {
      background: color-mix(in srgb, var(--td-brand-color-1) 75%, var(--td-brand-color));
    }
  }
}

.cell {
  display: flex;
  align-items: center;
  min-width: 0;
  padding: 0 8px;
  &:first-child { padding-left: 0; }
  &:last-child { padding-right: 0; }
}

.cell-check {
  justify-content: center;
  padding: 0;
}

.cell-name {
  gap: 8px;
  font-weight: 500;
}

.cell-size,
.cell-time {
  justify-content: flex-end;
}

.cell-actions {
  gap: 6px;
  justify-content: flex-end;
}

.row-inline-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
}

.row-action-btn {
  border: 0;
  padding: 3px 5px;
  background: transparent;
  color: var(--td-text-color-primary, #232323);
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;

  &:hover:not(:disabled) { color: var(--td-brand-color, #0052d9); }
  &.primary { color: var(--td-brand-color, #0052d9); }
  &.danger { color: var(--td-error-color, #d54941); }
  &:disabled { cursor: not-allowed; opacity: 0.45; }
}

.checkbox-wrap {
  display: inline-flex;
  align-items: center;
  cursor: pointer;
  input[type='checkbox'] {
    width: 14px;
    height: 14px;
    accent-color: var(--td-brand-color, #0052d9);
    cursor: pointer;
    margin: 0;

    &:disabled {
      cursor: not-allowed;
      opacity: 0.45;
    }
  }
}

.row-file-icon {
  flex-shrink: 0;
  font-size: 16px;
  color: var(--td-text-color-secondary, #888);
}

.row-file-name {
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row-tag {
  max-width: 100%;
  :deep(.t-tag__text) {
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 120px;
    display: inline-block;
  }
}

.row-muted {
  color: var(--td-text-color-disabled, #bbb);
}

.row-mono {
  font-variant-numeric: tabular-nums;
  font-size: 12px;
  color: var(--td-text-color-secondary, #666);
}

.row-status-tag :deep(.t-icon) {
  margin-right: 2px;
}
.icon-spin {
  animation: doc-list-spin 0.9s linear infinite;
}
@keyframes doc-list-spin {
  to { transform: rotate(360deg); }
}

</style>
