import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import router from "./router";
import "./assets/fonts.css";
import TDesign from "tdesign-vue-next";
// 引入组件库的少量全局样式变量
import "tdesign-vue-next/es/style/index.css";
import "@/assets/theme/theme.css";
import "@/assets/dropdown-menu.less";
import i18n from "./i18n";
import { initTheme } from "@/composables/useTheme";
import { installTDesignIconOfflineGuard } from "@/utils/tdesign-icon-offline";
import { ensureBidReviewSession } from "@/utils/bidreview-sso";
import {
  clearEmbeddedAuth,
  getEmbeddedParentOrigin,
  getRuntimeMode,
  isIntegrationAuthFailure,
  notifyEmbeddedHost,
  parseEmbeddedMessage,
  restoreEmbeddedAuth,
} from '@/utils/embedded-runtime';
import { exchangeBootstrapTicket, refreshIntegrationSession, type ExchangeResponse } from '@/api/integration';
import { useAuthStore } from '@/stores/auth';

// 必须在 Vue 组件挂载之前执行，避免 tdesign-icons 运行时请求 tdesign.gtimg.com
installTDesignIconOfflineGuard();

initTheme();

const app = createApp(App);
const pinia = createPinia();
app.use(TDesign);
app.use(pinia);
app.use(router);
app.use(i18n);

function applyEmbeddedUser(sessionUser: ExchangeResponse['user']) {
  const authStore = useAuthStore(pinia)
  authStore.setUser({
    id: sessionUser.id,
    username: sessionUser.username,
    nickname: sessionUser.username,
    email: '',
    tenant_id: String(sessionUser.tenant_id),
    can_access_all_tenants: false,
    role: (sessionUser.role || 'member') as 'platform_admin' | 'tenant_admin' | 'member',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  })
}

function startEmbeddedSessionRefresh() {
  const sync = () => {
    refreshIntegrationSession().then((refreshed) => {
      if (refreshed.user) applyEmbeddedUser(refreshed.user)
    }).catch((error) => {
      if (isIntegrationAuthFailure(error)) {
        clearEmbeddedAuth()
        notifyEmbeddedHost('unauthorized')
      }
    })
  }
  window.setInterval(sync, 10 * 60 * 1000)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') sync()
  })
}

async function tryResumeEmbeddedPageSession(): Promise<boolean> {
  if (!restoreEmbeddedAuth()) return false
  try {
    const refreshed = await refreshIntegrationSession()
    if (!refreshed.user?.id) {
      clearEmbeddedAuth()
      return false
    }
    applyEmbeddedUser(refreshed.user)
    startEmbeddedSessionRefresh()
    notifyEmbeddedHost('ready')
    return true
  } catch {
    clearEmbeddedAuth()
    return false
  }
}

async function prepareEmbeddedPageSession() {
  if (await tryResumeEmbeddedPageSession()) return

  const embeddedParentOrigin = getEmbeddedParentOrigin()
  const session = await new Promise<Awaited<ReturnType<typeof exchangeBootstrapTicket>>>((resolve, reject) => {
    let timeoutTimer = 0
    let readyTimer = 0
    let authenticating = false
    let lastError: unknown
    const cleanup = () => {
      window.removeEventListener('message', receive)
      window.clearTimeout(timeoutTimer)
      window.clearInterval(readyTimer)
    }
    const receive = async (event: MessageEvent) => {
      if (event.origin !== embeddedParentOrigin || event.source !== window.parent) return
      const message = parseEmbeddedMessage(event.data)
      if (!message || message.type !== 'auth-ready' || authenticating) return
      authenticating = true
      try {
        const exchange = await exchangeBootstrapTicket(message.ticket)
        cleanup()
        resolve(exchange)
      } catch (error) {
        lastError = error
        authenticating = false
      }
    }
    timeoutTimer = window.setTimeout(() => {
      cleanup()
      reject(lastError ?? new Error('等待宿主认证超时'))
    }, 60_000)
    window.addEventListener('message', receive)
    notifyEmbeddedHost('ready')
    readyTimer = window.setInterval(() => notifyEmbeddedHost('ready'), 1_500)
  })
  applyEmbeddedUser(session.user)
  startEmbeddedSessionRefresh()
}

const mode = getRuntimeMode()
if (mode === 'embedded-page') {
  const embeddedParentOrigin = getEmbeddedParentOrigin()
  window.addEventListener('message', (event) => {
    if (event.origin !== embeddedParentOrigin || event.source !== window.parent) return
    const message = parseEmbeddedMessage(event.data)
    if (!message) return
    if (message.type === 'set-theme') document.documentElement.dataset.theme = message.theme
    if (message.type === 'set-locale') i18n.global.locale.value = message.locale as typeof i18n.global.locale.value
    if (message.type === 'open-knowledge-base') router.push(`/platform/knowledge-bases/${encodeURIComponent(message.knowledgeBaseId)}`)
  })
  router.afterEach((to) => notifyEmbeddedHost('route-change', { path: to.fullPath }))
  window.addEventListener('weknora:document-published', ((event: CustomEvent<{ knowledgeBaseId: string; documentId: string }>) => {
    notifyEmbeddedHost('document-published', event.detail)
  }) as EventListener)
}
const prepareSession = mode === 'embedded-page'
  ? prepareEmbeddedPageSession()
  : mode === 'embedded-widget'
    ? Promise.resolve()
    : ensureBidReviewSession();

const mountApp = () => {
  // 等首屏路由（含导航守卫、Lite 自动登录）完成后再挂载，避免先闪默认页再跳转
  router.isReady().finally(() => {
    app.mount("#app");
  });
}

prepareSession.then(mountApp).catch(() => {
  clearEmbeddedAuth()
  notifyEmbeddedHost('unauthorized')
  if (mode !== 'embedded-page') {
    mountApp()
    return
  }
  const root = document.querySelector<HTMLElement>('#app')
  if (root) {
    root.setAttribute('role', 'alert')
    root.textContent = '嵌入认证失败，请从宿主重新打开页面。'
  }
})
