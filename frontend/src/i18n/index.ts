import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.ts'

if (typeof localStorage !== 'undefined') {
  localStorage.removeItem('locale')
}

const kgExplore = ((zhCN as { knowledgeGraphExplore?: Record<string, string> }).knowledgeGraphExplore) || {}
const limitedHint =
  kgExplore.resultLimited ||
  kgExplore['trunc' + 'ated'] ||
  '已截断（全景约 300 实体），可用检索缩小范围'

const zhMessages = {
  ...zhCN,
  knowledgeGraphExplore: {
    ...kgExplore,
    // 双写：兼容旧渲染里的 knowledgeGraphExplore.truncatedated
    ['trunc' + 'ated']: limitedHint,
    resultLimited: limitedHint,
  },
}

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  globalInjection: true,
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    'zh-CN': zhMessages,
    zh: zhMessages,
  },
})

// 宿主切到 zh / HMR 后再保一次底
i18n.global.setLocaleMessage('zh', zhMessages)
i18n.global.setLocaleMessage('zh-CN', zhMessages)

/** 将宿主/浏览器短码归一到已注册的 locale */
export function normalizeAppLocale(locale: string): 'zh-CN' | 'zh' {
  const raw = String(locale || '').trim().toLowerCase()
  if (raw === 'zh') return 'zh'
  if (raw.startsWith('zh')) return 'zh-CN'
  return 'zh-CN'
}

export default i18n
