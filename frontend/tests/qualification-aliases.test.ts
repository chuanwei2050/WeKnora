import { describe, expect, it } from 'vitest'
import { validateQualificationAliases } from '@/views/knowledge/qualification-aliases'

describe('validateQualificationAliases', () => {
  it('trims valid mappings', () => {
    expect(validateQualificationAliases([{ alias: ' 建工一级 ', standard_name: ' 标准名称 ' }])).toEqual({
      kind: 'valid',
      mappings: [{ alias: '建工一级', standard_name: '标准名称' }],
    })
  })

  it('rejects normalized duplicate aliases', () => {
    expect(validateQualificationAliases([
      { alias: 'ABC', standard_name: '名称一' },
      { alias: ' abc ', standard_name: '名称二' },
    ])).toEqual({ kind: 'duplicate', alias: 'abc' })
  })

  it('rejects incomplete rows', () => {
    expect(validateQualificationAliases([{ alias: '', standard_name: '名称' }])).toEqual({ kind: 'empty-field' })
  })
})
