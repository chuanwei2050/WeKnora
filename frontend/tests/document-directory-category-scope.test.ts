import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = (path: string) => readFileSync(resolve(__dirname, path), 'utf8')

describe('document directory category scope', () => {
  it('sends the selected tag through directory APIs', () => {
    const api = source('../src/api/knowledge-base/index.ts')
    expect(api).toContain("page_size: String(pageSize), tag_id: tagId")
    expect(api).toContain("{ tag_id: tagId, name, parent_id: parentId || null }")
    expect(api).toContain('directory-entries?tag_id=${encodeURIComponent(tagId)}')
  })

  it('keeps directory selection and movement aligned with paged list state', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    expect(view).toContain('if (currentPage * pageSize >= Number(result.total || 0)) break')
    expect(view).toContain('for (const id of selectedDirectoryIds.value)')
    expect(view).toContain("target.closest('.doc-card-list, .doc-list-body, .doc-empty-state')")
    expect(view).toContain('clearSelection();\n  loadKnowledgeFiles(kbId.value);')
  })

  it('resets directory-local state before loading another category', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const watcher = view.slice(view.indexOf('watch(selectedTagId'), view.indexOf('// Keep the folder selection'))
    expect(watcher).toContain('currentDirectoryId.value = undefined')
    expect(watcher).toContain('directoryBreadcrumb.value = []')
    expect(watcher).toContain('selectedDirectoryIds.value.clear()')
    expect(watcher.indexOf('currentDirectoryId.value = undefined')).toBeLessThan(watcher.indexOf('loadKnowledgeFiles(kbId.value)'))
  })

  it('uses selection batch actions without custom context menu or cut state', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    const batch = source('../src/views/knowledge/components/DocumentBatchBar.vue')
    expect(view).not.toContain('@contextmenu')
    expect(list).not.toContain('@contextmenu')
    expect(view).not.toContain('cutEntries')
    expect(view).not.toContain('pasteCutEntries')
    expect(batch).toContain("emit('move-directory', directoryId)")
    expect(batch).toContain("documentCount === count")
    expect(batch).toContain("knowledgeBase.adjustKnowledgeCategory")
  })

  it('provides complete single-directory actions and selects documents with directories', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    expect(list).toContain("type DirectoryAction = 'move' | 'download' | 'rename' | 'delete'")
    expect(list).toContain("emit('directory-action', 'move', item, directoryId)")
    expect(list).toContain("emit('directory-action', 'download', item)")
    expect(list).toContain("emit('directory-action', 'rename', item)")
    expect(list).toContain("emit('directory-action', 'delete', item)")
    expect(list).toContain("emit('toggle-all', checked, selectableIds.value, selectableDirectoryIds.value)")
    expect(view).toContain('@directory-action="(action: any, item: any, directoryId?: string) => handleDirectoryItemAction(action, item, directoryId)"')
    expect(view).toContain('for (const id of selectableDirectoryIds) selectedDirectoryIds.value.add(id)')
  })

  it('moves one document into a directory without reusing category movement', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    expect(list).toContain("emit('move-directory', item, directoryId)")
    expect(list).toContain("knowledgeBase.rowMoveToDirectory")
    expect(list).toContain("knowledgeBase.rowMoveToCategory")
    expect(list).toContain("@select=\"(folderId: string) => emit('move-folder', item, folderId)\"")
    expect(view).toContain("@move-directory=\"(item: any, directoryId: string) => handleDirectoryItemAction('move', item, directoryId)\"")
  })

  it('uses the same cascader interaction for directory moves and keeps uploads in the current directory', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    const batch = source('../src/views/knowledge/components/DocumentBatchBar.vue')
    expect(list).toContain('<FolderMoveCascader')
    expect(batch).toContain(':options="directoryTargets || []"')
    expect(view).toContain('directory_id: currentDirectoryId.value')
    expect(view).toContain('parent_directory_id: currentDirectoryId.value')
    expect(view).toContain('directory_path: directoryPath')
  })

  it('renders new directory as the leading primary text action', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    expect(view).toContain('class="document-toolbar-primary-button new-document-directory-button"')
    expect(view).toContain('theme="primary"')
    expect(view).toContain('variant="base"')
    expect(view).toContain("{{ $t('knowledgeBase.newDocumentDirectory') }}")
    expect(view).toContain('class="document-toolbar-primary-button add-document-button"')
    expect(view.indexOf('new-document-directory-button')).toBeLessThan(view.indexOf('add-document-button'))
  })

  it('shows the read-only directory breadcrumb on search results in both views', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    expect(view).toContain('item.directory_breadcrumb')
    expect(view).toContain('document-directory-location')
    expect(view).toContain(':search-mode="Boolean(docSearchKeyword.trim())"')
    expect(list).toContain('!props.searchMode || item.directory_breadcrumb == null')
    expect(list).toContain('document-directory-location')
  })

  it('keeps folder management separate from contributor file uploads', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    expect(view).toContain('v-if="canEdit"\n                  theme="primary"')
    expect(view).toContain("item.kind === 'directory' && canEdit")
    expect(view).toContain('if (!canEdit.value) return [upload]')
    expect(view).toContain("if (!canEdit.value) {")
    expect(list).toContain(':draggable="canEdit"')
    expect(list).toContain(':disabled="item.kind === \'directory\' ? !canEdit')
  })

  it('carries directory context through search and reference navigation', () => {
    const search = source('../src/views/knowledge/KnowledgeSearch.vue')
    const references = source('../src/views/chat/components/docInfo.vue')
    const stream = source('../src/views/chat/components/AgentStreamDisplay.vue')
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    expect(search).toContain('query.directory_id = group.directoryId')
    expect(references).toContain('query.directory_id = directoryId')
    expect(stream).toContain('navigateToKnowledgeFromCitation')
    expect(view).toContain('route.query.directory_id')
  })

  it('locates duplicate uploads in their original category and directory', () => {
    const view = source('../src/views/knowledge/KnowledgeBase.vue')
    expect(view).toContain('handleTagFilterChange(existing.tag_id)')
    expect(view).toContain('enterDocumentDirectory(existing.directory_id || undefined)')
    expect(view).toContain('existing.directory_breadcrumb')
  })

  it('shows a visible sort affordance for every sortable list header', () => {
    const list = source('../src/views/knowledge/components/DocumentListView.vue')
    expect(list).toContain(": 'chevron-down';")
    expect((list.match(/class=\"column-sort-button\"/g) || []).length).toBe(5)
  })
})
