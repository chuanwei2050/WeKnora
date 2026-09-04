<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
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
  kind?: 'document' | 'directory';
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
  document_count?: number;
  directory_breadcrumb?: Array<{ id: string; name: string }>;
}

type DocumentAction = 'edit' | 'reparse' | 'delete' | 'submit' | 'withdraw' | 'approve' | 'reject';
type DirectoryAction = 'move' | 'download' | 'rename' | 'delete';
type SortField = 'name' | 'updated_at' | 'size' | 'type' | 'status';
type SortOrder = 'asc' | 'desc';

const props = defineProps<{
  items: KnowledgeItem[];
  selectedIds: Set<string>;
  selectedDirectoryIds: Set<string>;
  canEdit: boolean;
  canManage?: boolean;
  tagList: Tag[];
  folderTargets: FolderCascaderOption[];
  directoryTargets: FolderCascaderOption[];
  directoryTargetsLoading?: boolean;
  loading?: boolean;
  canGenerateSummary?: boolean;
  governanceEnabled?: boolean;
  canContribute?: boolean;
  canReview?: boolean;
  currentUserId?: string;
  governanceBusyId?: string;
  sortBy: SortField;
  sortOrder: SortOrder;
  searchMode?: boolean;
}>();

const emit = defineEmits<{
  (e: 'open', item: KnowledgeItem): void;
  (e: 'toggle-row', id: string, checked: boolean, shiftKey: boolean, selectableIds: string[]): void;
  (e: 'toggle-directory', id: string, checked: boolean): void;
  (e: 'toggle-all', checked: boolean, documentIds: string[], directoryIds: string[]): void;
  (e: 'action', action: DocumentAction, item: KnowledgeItem): void;
  (e: 'directory-action', action: DirectoryAction, item: KnowledgeItem, directoryId?: string): void;
  (e: 'move-directory', item: KnowledgeItem, directoryId: string): void;
  (e: 'move-folder', item: KnowledgeItem, tagId: string): void;
  (e: 'entry-dragstart', item: KnowledgeItem, event: DragEvent): void;
  (e: 'entry-drop', directoryId: string, event: DragEvent): void;
  (e: 'sort', field: SortField): void;
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
const showActions = computed(() => props.items.some(item => item.kind === 'directory') || props.canEdit || props.canManage || props.canContribute || props.canReview);
const canManage = computed(() => Boolean(props.canManage));
const directoryLocation = (item: KnowledgeItem) => {
  if (!props.searchMode || item.directory_breadcrumb == null) return '';
  const names = (item.directory_breadcrumb || []).map(node => node.name);
  return [t('knowledgeBase.documentDirectoryRoot'), ...names].join(' / ');
};
const directoryTargetsFor = (item: KnowledgeItem) => {
  if (item.kind !== 'directory') return props.directoryTargets;
  const disableBranch = (options: FolderCascaderOption[], blocked: boolean): FolderCascaderOption[] => options.map(option => {
    const optionBlocked = blocked || option.value === item.id;
    return {
      ...option,
      selectable: option.value === '__root__' ? option.selectable : !optionBlocked && option.selectable,
      ...(option.children ? { children: disableBranch(option.children, optionBlocked) } : {}),
    };
  });
  return disableBranch(props.directoryTargets, false);
};
const categoryTargetsFor = (item: KnowledgeItem) => {
  if (item.kind !== 'directory') return props.folderTargets;
  const currentTagID = String(item.tag_id ?? '');
  const disableCurrent = (options: FolderCascaderOption[]): FolderCascaderOption[] => options.map(option => ({
    ...option,
    selectable: option.value === '__root__' ? option.selectable : option.value !== currentTagID && option.selectable,
    ...(option.children ? { children: disableCurrent(option.children) } : {}),
  }));
  return disableCurrent(props.folderTargets);
};
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
const canOperateItem = (item: KnowledgeItem) => item.kind !== 'directory' && (props.canEdit || canOperateGovernanceRow(item, governanceContext()));
const selectableIds = computed(() => props.items.filter(canOperateItem).map(item => item.id));
const selectableDirectoryIds = computed(() => props.canEdit
  ? props.items.filter(item => item.kind === 'directory').map(item => item.id)
  : []);
const allSelected = computed(() => (
  selectableIds.value.length + selectableDirectoryIds.value.length > 0
  && selectableIds.value.every(id => props.selectedIds.has(id))
  && selectableDirectoryIds.value.every(id => props.selectedDirectoryIds.has(id))
));
const someSelected = computed(() => (
  (selectableIds.value.some(id => props.selectedIds.has(id))
    || selectableDirectoryIds.value.some(id => props.selectedDirectoryIds.has(id)))
  && !allSelected.value
));

const MIN_NAME_COLUMN_WIDTH = 220;
const MAX_NAME_COLUMN_WIDTH = 1600;
const NAME_COLUMN_KEYBOARD_STEP = 20;
const nameColumnHeader = ref<HTMLElement>();
const nameColumnWidth = ref(MIN_NAME_COLUMN_WIDTH);
let stopNameColumnResize: (() => void) | undefined;

const startNameColumnResize = (event: PointerEvent) => {
  stopNameColumnResize?.();
  const resizeHandle = event.currentTarget as HTMLElement;
  const headerCell = resizeHandle.parentElement;
  if (!headerCell) return;

  event.preventDefault();
  resizeHandle.setPointerCapture(event.pointerId);
  const startX = event.clientX;
  const startWidth = headerCell.getBoundingClientRect().width;
  const onPointerMove = (moveEvent: PointerEvent) => {
    nameColumnWidth.value = Math.min(
      MAX_NAME_COLUMN_WIDTH,
      Math.max(MIN_NAME_COLUMN_WIDTH, startWidth + moveEvent.clientX - startX),
    );
  };
  const onPointerUp = () => stopNameColumnResize?.();

  stopNameColumnResize = () => {
    stopNameColumnResize = undefined;
    resizeHandle.removeEventListener('pointermove', onPointerMove);
    resizeHandle.removeEventListener('pointerup', onPointerUp);
    resizeHandle.removeEventListener('pointercancel', onPointerUp);
    resizeHandle.removeEventListener('lostpointercapture', onPointerUp);
    window.removeEventListener('blur', onPointerUp);
    if (resizeHandle.hasPointerCapture(event.pointerId)) {
      resizeHandle.releasePointerCapture(event.pointerId);
    }
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
  };

  document.body.style.cursor = 'col-resize';
  document.body.style.userSelect = 'none';
  resizeHandle.addEventListener('pointermove', onPointerMove);
  resizeHandle.addEventListener('pointerup', onPointerUp);
  resizeHandle.addEventListener('pointercancel', onPointerUp);
  resizeHandle.addEventListener('lostpointercapture', onPointerUp);
  window.addEventListener('blur', onPointerUp);
};

const resizeNameColumnWithKeyboard = (event: KeyboardEvent) => {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
  event.preventDefault();
  const delta = event.key === 'ArrowRight' ? NAME_COLUMN_KEYBOARD_STEP : -NAME_COLUMN_KEYBOARD_STEP;
  nameColumnWidth.value = Math.min(
    MAX_NAME_COLUMN_WIDTH,
    Math.max(MIN_NAME_COLUMN_WIDTH, nameColumnWidth.value + delta),
  );
};

onMounted(() => {
  if (nameColumnHeader.value) {
    nameColumnWidth.value = Math.min(
      MAX_NAME_COLUMN_WIDTH,
      Math.max(MIN_NAME_COLUMN_WIDTH, Math.round(nameColumnHeader.value.getBoundingClientRect().width)),
    );
  }
});
onBeforeUnmount(() => {
  stopNameColumnResize?.();
});

const onHeaderToggle = (e: Event) => {
  const checked = (e.target as HTMLInputElement).checked;
  emit('toggle-all', checked, selectableIds.value, selectableDirectoryIds.value);
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
const sortIcon = (field: SortField) => props.sortBy === field
  ? (props.sortOrder === 'asc' ? 'chevron-up' : 'chevron-down')
  : 'chevron-down';
const ariaSort = (field: SortField): 'ascending' | 'descending' | 'none' => {
  if (props.sortBy !== field) return 'none';
  return props.sortOrder === 'asc' ? 'ascending' : 'descending';
};
</script>

<template>
  <div
    class="doc-list-view"
    :class="{ 'is-loading': loading }"
    :style="{
      '--doc-list-actions-width': showActions ? (canEdit ? '320px' : '220px') : '0px',
      '--doc-list-name-width': `${nameColumnWidth}px`,
    }"
  >
    <div class="doc-list-header" role="row">
      <div class="cell cell-check" role="columnheader">
        <label class="checkbox-wrap" @click.stop>
          <input
            type="checkbox"
            :checked="allSelected"
            :indeterminate.prop="someSelected"
            :disabled="selectableIds.length + selectableDirectoryIds.length === 0"
            @change="onHeaderToggle"
            :aria-label="t('knowledgeBase.selectAll')"
          />
        </label>
      </div>
      <div ref="nameColumnHeader" class="cell cell-name" role="columnheader" :aria-sort="ariaSort('name')">
        <button class="column-sort-button" type="button" @click="emit('sort', 'name')">
          {{ t('knowledgeBase.columnName') }}
          <t-icon v-if="sortIcon('name')" :name="sortIcon('name')" size="14px" />
        </button>
        <span
          class="column-resize-handle"
          role="separator"
          tabindex="0"
          aria-orientation="vertical"
          :aria-label="t('knowledgeBase.columnName')"
          :aria-valuemin="MIN_NAME_COLUMN_WIDTH"
          :aria-valuemax="MAX_NAME_COLUMN_WIDTH"
          :aria-valuenow="nameColumnWidth"
          @pointerdown="startNameColumnResize"
          @keydown="resizeNameColumnWithKeyboard"
        />
      </div>
      <div class="cell cell-tag" role="columnheader">{{ t('knowledgeBase.columnTag') }}</div>
      <div class="cell cell-size" role="columnheader" :aria-sort="ariaSort('size')">
        <button class="column-sort-button" type="button" @click="emit('sort', 'size')">{{ t('knowledgeBase.columnSize') }}<t-icon v-if="sortIcon('size')" :name="sortIcon('size')" size="14px" /></button>
      </div>
      <div class="cell cell-type" role="columnheader" :aria-sort="ariaSort('type')">
        <button class="column-sort-button" type="button" @click="emit('sort', 'type')">{{ t('knowledgeBase.columnType') }}<t-icon v-if="sortIcon('type')" :name="sortIcon('type')" size="14px" /></button>
      </div>
      <div class="cell cell-status" role="columnheader" :aria-sort="ariaSort('status')">
        <button class="column-sort-button" type="button" @click="emit('sort', 'status')">{{ t('knowledgeBase.columnStatus') }}<t-icon v-if="sortIcon('status')" :name="sortIcon('status')" size="14px" /></button>
      </div>
      <div class="cell cell-time" role="columnheader" :aria-sort="ariaSort('updated_at')">
        <button class="column-sort-button" type="button" @click="emit('sort', 'updated_at')">{{ t('knowledgeBase.columnUpdatedAt') }}<t-icon v-if="sortIcon('updated_at')" :name="sortIcon('updated_at')" size="14px" /></button>
      </div>
      <div class="cell cell-actions" role="columnheader" v-if="showActions">{{ t('knowledgeBase.columnActions') }}</div>
    </div>

    <div class="doc-list-body">
      <div
        v-for="item in items"
        :key="item.id"
        class="doc-list-row"
        :class="{ selected: selectedIds.has(item.id) || selectedDirectoryIds.has(item.id) }"
        role="row"
        tabindex="0"
        :data-entry-id="item.id"
        :draggable="canEdit"
        @click="emit('open', item)"
        @dragstart="emit('entry-dragstart', item, $event)"
        @dragover="canEdit && item.kind === 'directory' ? $event.preventDefault() : undefined"
        @drop.stop="canEdit && item.kind === 'directory' ? emit('entry-drop', item.id, $event) : undefined"
      >
        <div class="cell cell-check" @click.stop>
          <label class="checkbox-wrap">
            <input
              type="checkbox"
              :checked="item.kind === 'directory' ? selectedDirectoryIds.has(item.id) : selectedIds.has(item.id)"
              :disabled="item.kind === 'directory' ? !canEdit : !canOperateItem(item)"
              @click="item.kind === 'directory' ? emit('toggle-directory', item.id, !selectedDirectoryIds.has(item.id)) : onRowToggle(item, $event as unknown as MouseEvent)"
              :aria-label="item.file_name"
            />
          </label>
        </div>

        <div class="cell cell-name">
          <t-icon :name="item.kind === 'directory' ? 'folder-open' : getFileIcon(item)" :size="item.kind === 'directory' ? '20px' : undefined" :class="item.kind === 'directory' ? 'document-directory-icon' : 'row-file-icon'" />
          <div class="row-file-name-group">
            <span class="row-file-name" :title="item.file_name">{{ item.file_name }}</span>
            <span v-if="directoryLocation(item)" class="document-directory-location" :title="directoryLocation(item)">
              {{ directoryLocation(item) }}
            </span>
          </div>
        </div>


        <div class="cell cell-tag">
          <t-tag
            v-if="item.kind !== 'directory' && getTagName(item.tag_id)"
            size="small"
            variant="light-outline"
            class="row-tag"
            max-width="100%"
            :title="getTagName(item.tag_id)"
          >
            {{ getTagName(item.tag_id) }}
          </t-tag>
          <span v-else class="row-muted">--</span>
        </div>

        <div class="cell cell-size">
          <span class="row-mono">{{ item.kind === 'directory' ? '--' : (formatFileSize(item.file_size) || '--') }}</span>
        </div>

        <div class="cell cell-type">
          <span class="row-mono">{{ item.kind === 'directory' ? t('knowledgeBase.documentDirectory') : getTypeLabel(item) }}</span>
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
            <template v-if="item.kind === 'directory'">
              <button v-if="canEdit" class="row-action-btn" type="button" @click="emit('directory-action', 'download', item)">
                {{ t('knowledgeBase.downloadDocumentDirectory') }}
              </button>
              <button v-if="canEdit" class="row-action-btn" type="button" @click="emit('directory-action', 'rename', item)">
                {{ t('knowledgeBase.renameDocumentDirectory') }}
              </button>
              <FolderMoveCascader
                v-if="canEdit"
                :options="directoryTargetsFor(item)"
                :loading="directoryTargetsLoading"
                placement="top-right"
                @select="(directoryId: string) => emit('directory-action', 'move', item, directoryId)"
              >
                <button class="row-action-btn" type="button">
                  {{ t('knowledgeBase.rowMoveToDirectory') }}
                </button>
              </FolderMoveCascader>
              <FolderMoveCascader
                v-if="canEdit && folderTargets.length"
                :options="categoryTargetsFor(item)"
                placement="top-right"
                @select="(folderId: string) => emit('move-folder', item, folderId)"
              >
                <button class="row-action-btn" type="button">
                  {{ t('knowledgeBase.rowMoveToCategory') }}
                </button>
              </FolderMoveCascader>
              <button v-if="canManage" class="row-action-btn danger" type="button" @click="emit('directory-action', 'delete', item)">
                {{ t('knowledgeBase.governanceDelete') }}
              </button>
            </template>
            <template v-else>
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
              v-if="canEdit"
              :options="directoryTargetsFor(item)"
              :loading="directoryTargetsLoading"
              placement="top-right"
              @select="(directoryId: string) => emit('move-directory', item, directoryId)"
            >
              <button class="row-action-btn" type="button">
                {{ t('knowledgeBase.rowMoveToDirectory') }}
              </button>
            </FolderMoveCascader>
            <FolderMoveCascader
              v-if="canEdit && folderTargets.length"
              :options="folderTargets"
              @select="(folderId: string) => emit('move-folder', item, folderId)"
            >
              <button class="row-action-btn" type="button">
                {{ t('knowledgeBase.rowMoveToCategory') }}
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
            </template>
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
  overflow-x: auto;
  overflow-y: hidden;
}

.doc-list-header,
.doc-list-row {
  display: grid;
  grid-template-columns:
    44px                       // checkbox
    minmax(var(--doc-list-name-width), 2.4fr) // name
    minmax(160px, 1.1fr)       // tag
    96px                       // size
    72px                       // type
    minmax(96px, 0.7fr)        // status
    120px                      // updated_at
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
  position: relative;
  gap: 8px;
  font-weight: 500;
}

.column-sort-button {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  min-width: 0;
  padding: 4px 0;
  color: inherit;
  font: inherit;
  background: transparent;
  border: 0;
  cursor: pointer;

  &:hover {
    color: var(--td-text-color-primary);
  }

  &:focus-visible {
    border-radius: 3px;
    outline: 2px solid var(--td-brand-color);
    outline-offset: 2px;
  }
}

.document-directory-icon {
  flex-shrink: 0;
  color: #4d9bea;
}

.column-resize-handle {
  position: absolute;
  top: 0;
  right: -4px;
  width: 8px;
  height: 100%;
  cursor: col-resize;
  touch-action: none;

  &::after {
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 3px;
    width: 1px;
    content: '';
    background: transparent;
  }

  &:hover::after {
    background: var(--td-brand-color, #0052d9);
  }
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

.row-file-name-group {
  min-width: 0;
  display: flex;
  flex: 1;
  flex-direction: column;
}

.document-directory-location {
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  font-weight: 400;
  line-height: 16px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-tag {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
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
