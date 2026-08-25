export const UNTAGGED_TAG_NAME = '未分类'

export interface DocumentFolderRef {
  id: string
  name: string
  is_public?: boolean
  parent_id?: string | null
}

export interface FolderSections<T> {
  root: T[]
  public: T[]
  children: T[]
}

export interface OrdinaryFolderBranch<T> {
  root: T
  children: T[]
}

export function folderSiblingKey(folder?: DocumentFolderRef): string {
  if (!folder) return ''
  if (folder.parent_id) return `child:${folder.parent_id}`
  return folder.is_public ? 'public' : 'root'
}

export function ordinaryFolderBranches<T extends DocumentFolderRef>(
  folders: readonly T[],
): Array<OrdinaryFolderBranch<T>> {
  const roots = folders.filter(folder => !folder.is_public && !folder.parent_id)
  const childrenByParent = new Map<string, T[]>()
  folders.forEach((folder) => {
    if (folder.is_public || !folder.parent_id) return
    const siblings = childrenByParent.get(folder.parent_id) || []
    siblings.push(folder)
    childrenByParent.set(folder.parent_id, siblings)
  })
  return roots.map(root => ({ root, children: childrenByParent.get(root.id) || [] }))
}

export function ordinaryFolderChildOrders<T extends DocumentFolderRef>(
  folders: readonly T[],
): Record<string, string[]> {
  return Object.fromEntries(
    ordinaryFolderBranches(folders)
      .filter(branch => branch.children.length > 0)
      .map(branch => [branch.root.id, branch.children.map(child => child.id)]),
  )
}

export function resolveUploadTarget(folder?: DocumentFolderRef): { name: string; tagId?: string } {
  if (!folder || folder.name === UNTAGGED_TAG_NAME) {
    return { name: UNTAGGED_TAG_NAME }
  }
  return { name: folder.name, tagId: folder.id }
}

export function reorderFolders<T extends DocumentFolderRef>(
  folders: readonly T[],
  sourceId: string,
  targetId: string,
): T[] {
  const reordered = [...folders]
  const sourceIndex = reordered.findIndex(folder => folder.id === sourceId)
  const targetIndex = reordered.findIndex(folder => folder.id === targetId)
  if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex) return reordered
  const [moved] = reordered.splice(sourceIndex, 1)
  reordered.splice(targetIndex, 0, moved)
  return reordered
}

export function placeFolder<T extends DocumentFolderRef>(
  folders: readonly T[],
  sourceId: string,
  targetId: string,
): FolderSections<T> {
  const source = folders.find(folder => folder.id === sourceId)
  const target = folders.find(folder => folder.id === targetId)
  if (!source || !target || folderSiblingKey(source) !== folderSiblingKey(target)) {
    return {
      root: folders.filter(folder => !folder.is_public && !folder.parent_id),
      public: folders.filter(folder => folder.is_public && !folder.parent_id),
      children: folders.filter(folder => Boolean(folder.parent_id)),
    }
  }

  const key = folderSiblingKey(source)
  const siblings = reorderFolders(folders.filter(folder => folderSiblingKey(folder) === key), sourceId, targetId)
  let siblingIndex = 0
  const reordered = folders.map(folder => folderSiblingKey(folder) === key ? siblings[siblingIndex++] : folder)
  return {
    root: reordered.filter(folder => !folder.is_public && !folder.parent_id),
    public: reordered.filter(folder => folder.is_public && !folder.parent_id),
    children: reordered.filter(folder => Boolean(folder.parent_id)),
  }
}

export function folderMoveTargets<T extends DocumentFolderRef>(
  folders: readonly T[],
  currentFolderId: string,
): Array<{ content: string; value: string }> {
  return folders
    .filter(folder => folder.id !== currentFolderId)
    .map(folder => ({ content: folder.name, value: folder.id }))
}
