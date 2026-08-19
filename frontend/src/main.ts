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
import { getRuntimeMode, notifyEmbeddedHost, parseEmbeddedMessage } from '@/utils/embedded-runtime';
import { exchangeBootstrapTicket, refreshIntegrationSession } from '@/api/integration';
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

async function prepareEmbeddedPageSession() {
  const session = await new Promise<Awaited<ReturnType<typeof exchangeBootstrapTicket>>>((resolve, reject) => {
    let timer = 0
    const receive = async (event: MessageEvent) => {
      if (event.origin !== window.location.origin || event.source !== window.parent) return
      const message = parseEmbeddedMessage(event.data)
      if (!message || message.type !== 'auth-ready') return
      window.removeEventListener('message', receive)
      window.clearTimeout(timer)
      try { resolve(await exchangeBootstrapTicket(message.ticket)) } catch (error) { reject(error) }
    }
    timer = window.setTimeout(() => {
      window.removeEventListener('message', receive)
      reject(new Error('等待宿主认证超时'))
    }, 60_000)
    window.addEventListener('message', receive)
    notifyEmbeddedHost('ready')
  })
  const authStore = useAuthStore(pinia)
  authStore.setUser({
    id: session.user.id,
    username: session.user.username,
    nickname: session.user.username,
    email: '',
    tenant_id: String(session.user.tenant_id),
    can_access_all_tenants: false,
    role: (session.user.role || 'member') as 'platform_admin' | 'tenant_admin' | 'member',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  })
  window.setInterval(() => {
    refreshIntegrationSession().catch(() => notifyEmbeddedHost('unauthorized'))
  }, 10 * 60 * 1000)
}

const mode = getRuntimeMode()
if (mode === 'embedded-page') {
  window.addEventListener('message', (event) => {
    if (event.origin !== window.location.origin || event.source !== window.parent) return
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
