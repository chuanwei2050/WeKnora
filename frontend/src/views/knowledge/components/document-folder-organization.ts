export const UNTAGGED_TAG_NAME = '未分类'

export interface DocumentFolderRef {
  id: string
  name: string
  is_public?: boolean
}

export interface FolderSections<T> {
  root: T[]
  public: T[]
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
  targetPublic: boolean,
): FolderSections<T> {
  const source = folders.find(folder => folder.id === sourceId)
  const root = folders.filter(folder => !folder.is_public && folder.id !== sourceId)
  const publicFolders = folders.filter(folder => folder.is_public && folder.id !== sourceId)
  if (!source) return { root, public: publicFolders }

  const target = targetPublic ? publicFolders : root
  const targetIndex = targetId ? target.findIndex(folder => folder.id === targetId) : target.length
  target.splice(targetIndex < 0 ? target.length : targetIndex, 0, { ...source, is_public: targetPublic })
  return { root, public: publicFolders }
}

export function folderMoveTargets<T extends DocumentFolderRef>(
  folders: readonly T[],
  currentFolderId: string,
): Array<{ content: string; value: string }> {
  return folders
    .filter(folder => folder.id !== currentFolderId)
    .map(folder => ({ content: folder.name, value: folder.id }))
}
