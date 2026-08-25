import { describe, expect, it } from 'vitest'
import {
  folderMoveTargets,
  ordinaryFolderChildOrders,
  ordinaryFolderBranches,
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

  it('reorders root folders without changing their section', () => {
    const result = placeFolder([
      { id: 'root-a', name: 'A', is_public: false },
      { id: 'root-b', name: 'B', is_public: false },
      { id: 'public-a', name: 'C', is_public: true },
    ], 'root-b', 'root-a')

    expect(result.root.map(folder => folder.id)).toEqual(['root-b', 'root-a'])
    expect(result.public.map(folder => folder.id)).toEqual(['public-a'])
  })

  it('rejects dragging between root and public sections', () => {
    const result = placeFolder([
      { id: 'root-a', name: 'A', is_public: false },
      { id: 'root-b', name: 'B', is_public: false },
      { id: 'public-a', name: 'C', is_public: true },
    ], 'public-a', 'root-b')

    expect(result.root.map(folder => folder.id)).toEqual(['root-a', 'root-b'])
    expect(result.public.map(folder => folder.id)).toEqual(['public-a'])
  })

  it('groups ordinary second-level folders under their direct root', () => {
    const branches = ordinaryFolderBranches([
      { id: 'root-a', name: 'A' },
      { id: 'child-a', name: 'A-1', parent_id: 'root-a' },
      { id: 'root-b', name: 'B' },
      { id: 'public-a', name: '公共', is_public: true },
    ])

    expect(branches.map(branch => ({
      root: branch.root.id,
      children: branch.children.map(child => child.id),
    }))).toEqual([
      { root: 'root-a', children: ['child-a'] },
      { root: 'root-b', children: [] },
    ])
  })

  it('builds complete child orders without empty parents', () => {
    expect(ordinaryFolderChildOrders([
      { id: 'root-a', name: 'A' },
      { id: 'child-a', name: 'A-1', parent_id: 'root-a' },
      { id: 'root-b', name: 'B' },
      { id: 'public-a', name: '公共', is_public: true },
    ])).toEqual({
      'root-a': ['child-a'],
    })
  })

  it('reorders children only within the same parent', () => {
    const result = placeFolder([
      { id: 'root-a', name: 'A' },
      { id: 'child-a', name: 'A-1', parent_id: 'root-a' },
      { id: 'child-b', name: 'A-2', parent_id: 'root-a' },
      { id: 'root-b', name: 'B' },
      { id: 'child-c', name: 'B-1', parent_id: 'root-b' },
      { id: 'public-a', name: '公共', is_public: true },
    ], 'child-b', 'child-a')

    expect(result.root.map(folder => folder.id)).toEqual(['root-a', 'root-b'])
    expect(result.public.map(folder => folder.id)).toEqual(['public-a'])
    expect(result.children.map(folder => folder.id)).toEqual(['child-b', 'child-a', 'child-c'])

    const rejected = placeFolder(result.children, 'child-a', 'child-c')
    expect(rejected.children.map(folder => folder.id)).toEqual(['child-b', 'child-a', 'child-c'])
  })
})
