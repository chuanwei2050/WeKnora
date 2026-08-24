import { describe, expect, it } from 'vitest'
import {
  folderMoveTargets,
  placeFolder,
  reorderFolders,
  resolveUploadTarget,
} from '../src/views/knowledge/components/document-folder-organization'

const folders = [
  { id: 'untagged', name: '未分类' },
  { id: 'a', name: '1.1-投标文件' },
  { id: 'b', name: '产品资料' },
]

describe('document folder organization', () => {
  it('keeps a tag name intact as one folder', () => {
    expect(folderMoveTargets(folders, '').map(folder => folder.content)).toContain('1.1-投标文件')
  })

  it('uses the selected ordinary folder as the upload target', () => {
    expect(resolveUploadTarget(folders[1])).toEqual({ name: '1.1-投标文件', tagId: 'a' })
    expect(resolveUploadTarget()).toEqual({ name: '未分类' })
    expect(resolveUploadTarget(folders[0])).toEqual({ name: '未分类' })
  })

  it('reorders folders without mutating the source array', () => {
    const source = folders.slice(1)
    expect(reorderFolders(source, 'b', 'a').map(folder => folder.id)).toEqual(['b', 'a'])
    expect(reorderFolders(source, 'a', 'b').map(folder => folder.id)).toEqual(['b', 'a'])
    expect(source.map(folder => folder.id)).toEqual(['a', 'b'])
  })

  it('excludes the current folder from batch move targets', () => {
    expect(folderMoveTargets(folders, 'untagged')).toEqual([
      { content: '1.1-投标文件', value: 'a' },
      { content: '产品资料', value: 'b' },
    ])
  })

  it('moves a root folder into public files', () => {
    const result = placeFolder([
      { id: 'root-a', name: 'A', is_public: false },
      { id: 'root-b', name: 'B', is_public: false },
      { id: 'public-a', name: 'C', is_public: true },
    ], 'root-a', 'public-a', true)

    expect(result.root.map(folder => folder.id)).toEqual(['root-b'])
    expect(result.public.map(folder => folder.id)).toEqual(['root-a', 'public-a'])
    expect(result.public[0].is_public).toBe(true)
  })

  it('moves a public folder back to the root', () => {
    const result = placeFolder([
      { id: 'root-a', name: 'A', is_public: false },
      { id: 'root-b', name: 'B', is_public: false },
      { id: 'public-a', name: 'C', is_public: true },
    ], 'public-a', 'root-b', false)

    expect(result.root.map(folder => folder.id)).toEqual(['root-a', 'public-a', 'root-b'])
    expect(result.public).toEqual([])
    expect(result.root[1].is_public).toBe(false)
  })
})
