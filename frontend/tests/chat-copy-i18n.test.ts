import { describe, expect, it } from 'vitest'
import zhCN from '../src/i18n/locales/zh-CN'
import enUS from '../src/i18n/locales/en-US'
import koKR from '../src/i18n/locales/ko-KR'
import ruRU from '../src/i18n/locales/ru-RU'

describe('chat copy translations', () => {
  it.each([
    ['zh-CN', zhCN],
    ['en-US', enUS],
    ['ko-KR', koKR],
    ['ru-RU', ruRU],
  ])('%s defines copy result messages', (_locale, messages) => {
    expect(messages.chat.copySuccess).toBeTruthy()
    expect(messages.chat.copyFailed).toBeTruthy()
  })
})
