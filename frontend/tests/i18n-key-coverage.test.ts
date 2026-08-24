import { readFileSync, readdirSync, statSync } from 'node:fs'
import { resolve } from 'node:path'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import zhCN from '../src/i18n/locales/zh-CN'

const sourceRoot = resolve(process.cwd(), 'src')
const sourceExtensions = /\.(?:ts|tsx|js|jsx|vue)$/
const translationCall = /(?:\$t|\bt|i18n\.global\.t)\(\s*(['"])([^'"`]+)\1/g

function listSourceFiles(directory: string): string[] {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name)
    return statSync(path).isDirectory()
      ? listSourceFiles(path)
      : sourceExtensions.test(name) ? [path] : []
  })
}

function hasTranslation(messages: unknown, key: string): boolean {
  return key.split('.').reduce<unknown>((value, segment) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[segment]
  }, messages) !== undefined
}

describe('i18n key coverage', () => {
  it('兜底中文语言包包含源码中使用的静态翻译 key', () => {
    const missing = new Set<string>()

    for (const file of listSourceFiles(sourceRoot)) {
      const source = readFileSync(file, 'utf8')
      for (const match of source.matchAll(translationCall)) {
        if (!hasTranslation(zhCN, match[2])) missing.add(match[2])
      }
    }

    expect([...missing].sort()).toEqual([])
  })
})
