export const UNTAGGED_TAG_NAME = '未分类'

export interface DocumentFolderRef {
  id: string
  name: string
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

export function folderMoveTargets<T extends DocumentFolderRef>(
  folders: readonly T[],
  currentFolderId: string,
): Array<{ content: string; value: string }> {
  return folders
    .filter(folder => folder.id !== currentFolderId)
    .map(folder => ({ content: folder.name, value: folder.id }))
}
