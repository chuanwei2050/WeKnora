import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.ts'

localStorage.removeItem('locale')

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  globalInjection: true,
  messages: {
    'zh-CN': zhCN
  }
})

export default i18n
