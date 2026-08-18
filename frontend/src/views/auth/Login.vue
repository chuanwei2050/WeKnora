<template>
  <main class="login-page">
    <section class="identity-panel" aria-label="产品介绍">
      <div class="grid-texture" aria-hidden="true"></div>
      <div class="brand">
        <div class="brand-mark">知</div>
        <span>智信测评知识平台</span>
      </div>

      <div class="identity-content">
        <p class="eyebrow">SOFTWARE QUALITY INTELLIGENCE</p>
        <h1>让测评知识<br />成为可追溯的答案</h1>
        <p class="lead">
          汇聚标准、案例与项目资料，通过知识问答和关系图谱，快速定位依据、理解关联。
        </p>

        <div class="capabilities" aria-label="平台能力">
          <div class="capability">
            <span class="capability-index">01</span>
            <span>测评知识问答</span>
          </div>
          <div class="capability">
            <span class="capability-index">02</span>
            <span>知识关系图谱</span>
          </div>
          <div class="capability">
            <span class="capability-index">03</span>
            <span>多租户数据隔离</span>
          </div>
        </div>
      </div>

      <div class="graph-motif" aria-hidden="true">
        <span class="graph-line line-one"></span>
        <span class="graph-line line-two"></span>
        <span class="graph-line line-three"></span>
        <span class="graph-node node-one"></span>
        <span class="graph-node node-two"></span>
        <span class="graph-node node-three"></span>
        <span class="graph-node node-four"></span>
      </div>

      <p class="identity-footer">私有化部署 · 数据自主可控</p>
    </section>

    <section class="form-panel">
      <div class="form-shell">
        <header class="form-header">
          <p class="mobile-brand">智信测评知识平台</p>
          <h2>欢迎登录</h2>
          <p>使用管理员分配的账号进入工作空间</p>
        </header>

        <t-form
          :data="formData"
          :rules="formRules"
          layout="vertical"
        >
          <t-form-item label="用户名" name="username">
            <t-input
              v-model="formData.username"
              size="large"
              placeholder="请输入用户名"
              autocomplete="username"
              :disabled="loading"
            />
          </t-form-item>

          <t-form-item label="密码" name="password">
            <t-input
              v-model="formData.password"
              type="password"
              size="large"
              placeholder="请输入登录密码"
              autocomplete="current-password"
              :disabled="loading"
              @keyup.enter="handleLogin"
            />
          </t-form-item>

          <t-button
            type="button"
            theme="primary"
            size="large"
            block
            :loading="loading"
            class="login-button"
            @click="handleLogin"
          >
            进入平台
          </t-button>
        </t-form>

        <template v-if="oidcEnabled">
          <div class="divider"><span>或</span></div>
          <t-button
            variant="outline"
            size="large"
            block
            :loading="oidcLoading"
            @click="handleOIDCLogin"
          >
            使用{{ oidcProviderName || '企业账号' }}登录
          </t-button>
        </template>

        <div class="access-note">
          <span class="status-dot"></span>
          <span>平台已关闭公开注册，请联系管理员开通账号</span>
        </div>
      </div>

      <p class="form-footer">© 2026 智信测评知识平台</p>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  autoSetup,
  getOIDCAuthorizationURL,
  getOIDCConfig,
  login,
  type LoginResponse
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const loading = ref(false)
const oidcLoading = ref(false)
const oidcEnabled = ref(false)
const oidcProviderName = ref('')

const formData = reactive({
  username: '',
  password: ''
})

const formRules = computed(() => ({
  username: [
    { required: true, message: '请输入用户名', type: 'error' },
    { min: 2, max: 100, message: '用户名长度应为 2 到 100 个字符', type: 'error' }
  ],
  password: [
    { required: true, message: '请输入登录密码', type: 'error' },
    { min: 8, message: '密码至少 8 位', type: 'error' }
  ]
}))

const persistLoginResponse = async (response: LoginResponse) => {
  if (!response.user || !response.tenant || !response.token) return

  localStorage.removeItem('weknora_bidreview_embedded')
  localStorage.removeItem('weknora_bidreview_role')
  authStore.setLiteMode(false)

  authStore.setUser({
    id: response.user.id,
    username: response.user.username,
    email: response.user.email,
    avatar: response.user.avatar,
    tenant_id: String(response.tenant.id),
    can_access_all_tenants: response.user.can_access_all_tenants || false,
    role: response.user.role || 'member',
    created_at: response.user.created_at,
    updated_at: response.user.updated_at
  })
  authStore.setToken(response.token)
  if (response.refresh_token) authStore.setRefreshToken(response.refresh_token)
  authStore.setTenant({
    id: String(response.tenant.id),
    name: response.tenant.name,
    description: response.tenant.description,
    api_key: response.tenant.api_key,
    status: response.tenant.status,
    business: response.tenant.business,
    storage_quota: response.tenant.storage_quota,
    storage_used: response.tenant.storage_used,
    owner_id: response.user.id,
    created_at: response.tenant.created_at,
    updated_at: response.tenant.updated_at
  })

  await nextTick()
  await router.replace(authStore.user?.role === 'platform_admin' ? '/platform/admin/tenants' : '/platform/knowledge-bases')
}

const handleLogin = async () => {
  const username = formData.username.trim()
  if (username.length < 2) {
    MessagePlugin.warning('请输入有效用户名')
    return
  }
  if (formData.password.length < 8) {
    MessagePlugin.warning('密码至少 8 位')
    return
  }
  formData.username = username

  loading.value = true
  try {
    const response = await login(formData)
    if (!response.success) {
      MessagePlugin.error(response.message || '用户名或密码错误')
      return
    }
    MessagePlugin.success('登录成功')
    await persistLoginResponse(response)
  } catch (error) {
    console.error('登录失败:', error)
    MessagePlugin.error('登录失败，请稍后重试')
  } finally {
    loading.value = false
  }
}

const handleOIDCLogin = async () => {
  oidcLoading.value = true
  try {
    const redirectURI = `${window.location.origin}/api/v1/auth/oidc/callback`
    const response = await getOIDCAuthorizationURL(redirectURI)
    if (!response.success || !response.authorization_url) {
      MessagePlugin.error(response.message || '企业登录暂不可用')
      return
    }
    window.location.href = response.authorization_url
  } catch (error) {
    console.error('企业登录失败:', error)
    MessagePlugin.error('企业登录暂不可用')
  } finally {
    oidcLoading.value = false
  }
}

const loadOIDCConfig = async () => {
  try {
    const response = await getOIDCConfig()
    oidcEnabled.value = response.success && response.enabled
    oidcProviderName.value = response.provider_display_name || ''
  } catch {
    oidcEnabled.value = false
  }
}

onMounted(async () => {
  if (authStore.isLoggedIn) {
    await router.replace(authStore.user?.role === 'platform_admin' ? '/platform/admin/tenants' : '/platform/knowledge-bases')
    return
  }

  const failureKey = 'weknora_auto_setup_failed'
  if (localStorage.getItem(failureKey) !== 'true') {
    try {
      const response = await autoSetup()
      if (response.success) {
        authStore.setLiteMode(true)
        await persistLoginResponse(response)
        return
      }
      localStorage.setItem(failureKey, 'true')
    } catch {
      localStorage.setItem(failureKey, 'true')
    }
  }

  await loadOIDCConfig()
})
</script>

<style scoped lang="less">
.login-page {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(460px, 1.08fr) minmax(440px, 0.92fr);
  background: #f3f6fb;
  color: #14213d;
}

.identity-panel {
  position: relative;
  min-height: 100vh;
  padding: 48px clamp(48px, 6vw, 96px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  color: #f4f8ff;
  background:
    radial-gradient(circle at 82% 76%, rgba(84, 151, 232, 0.25), transparent 30%),
    linear-gradient(145deg, #0b2855 0%, #103d7a 56%, #0b2e63 100%);
}

.grid-texture {
  position: absolute;
  inset: 0;
  opacity: 0.08;
  background-image:
    linear-gradient(rgba(255, 255, 255, 0.8) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.8) 1px, transparent 1px);
  background-size: 48px 48px;
  mask-image: linear-gradient(to bottom right, black, transparent 72%);
}

.brand,
.identity-content,
.identity-footer {
  position: relative;
  z-index: 2;
}

.brand {
  display: flex;
  align-items: center;
  gap: 14px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0.06em;
}

.brand-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.42);
  background: rgba(255, 255, 255, 0.1);
  font-family: Georgia, serif;
  font-size: 20px;
}

.identity-content {
  width: min(610px, 100%);
  margin: auto 0;
  padding: 76px 0 92px;
}

.eyebrow {
  margin: 0 0 24px;
  color: #9fc8fb;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.22em;
}

h1 {
  margin: 0;
  font-family: "TencentSans", "Microsoft YaHei", sans-serif;
  font-size: clamp(44px, 5vw, 72px);
  font-weight: 650;
  line-height: 1.16;
  letter-spacing: -0.035em;
}

.lead {
  max-width: 520px;
  margin: 28px 0 0;
  color: rgba(231, 240, 254, 0.78);
  font-size: 16px;
  line-height: 1.85;
}

.capabilities {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 20px;
  margin-top: 56px;
}

.capability {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-top: 14px;
  border-top: 1px solid rgba(255, 255, 255, 0.2);
  font-size: 13px;
}

.capability-index {
  color: #8dbcf4;
  font-family: Georgia, serif;
  font-size: 11px;
}

.identity-footer {
  margin: 0;
  color: rgba(226, 238, 254, 0.55);
  font-size: 12px;
  letter-spacing: 0.08em;
}

.graph-motif {
  position: absolute;
  right: 5%;
  bottom: 7%;
  width: 250px;
  height: 220px;
  opacity: 0.35;
  transform: rotate(-8deg);
}

.graph-node {
  position: absolute;
  width: 12px;
  height: 12px;
  border: 2px solid #9bc4f5;
  border-radius: 50%;
  background: #103d7a;
}

.node-one { left: 12px; top: 118px; }
.node-two { left: 104px; top: 48px; }
.node-three { right: 22px; top: 100px; }
.node-four { left: 116px; bottom: 12px; }

.graph-line {
  position: absolute;
  height: 1px;
  background: #9bc4f5;
  transform-origin: left center;
}

.line-one { width: 118px; left: 21px; top: 123px; transform: rotate(-37deg); }
.line-two { width: 125px; left: 114px; top: 57px; transform: rotate(24deg); }
.line-three { width: 125px; left: 122px; top: 62px; transform: rotate(86deg); }

.form-panel {
  position: relative;
  min-height: 100vh;
  padding: 56px clamp(44px, 7vw, 112px);
  display: flex;
  align-items: center;
  justify-content: center;
}

.form-shell {
  width: min(420px, 100%);
}

.form-header {
  margin-bottom: 38px;
}

.form-header h2 {
  margin: 0 0 12px;
  font-size: 34px;
  font-weight: 650;
  letter-spacing: -0.04em;
}

.form-header p {
  margin: 0;
  color: #687892;
  font-size: 14px;
}

.mobile-brand {
  display: none;
}

:deep(.t-form__label) {
  padding-bottom: 9px;
  color: #2f3f5d;
  font-size: 13px;
  font-weight: 600;
}

:deep(.t-form__item) {
  margin-bottom: 28px;
}

:deep(.t-input) {
  height: 48px;
  border-radius: 4px;
  background: #fff;
}

:deep(.t-input--focused) {
  box-shadow: 0 0 0 2px rgba(38, 105, 201, 0.12);
}

.login-button {
  height: 50px;
  margin-top: 24px;
  border-radius: 4px;
  background: #1f63bd;
  border-color: #1f63bd;
  font-weight: 600;
  letter-spacing: 0.08em;
}

.login-button:hover {
  background: #174f9a;
  border-color: #174f9a;
}

.divider {
  display: flex;
  align-items: center;
  gap: 14px;
  margin: 26px 0;
  color: #8b98ac;
  font-size: 12px;
}

.divider::before,
.divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: #d9e1ed;
}

.access-note {
  margin-top: 32px;
  padding: 14px 16px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px solid #d6e0ed;
  background: rgba(255, 255, 255, 0.48);
  color: #66758c;
  font-size: 12px;
  line-height: 1.5;
}

.status-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: #4384d5;
  box-shadow: 0 0 0 4px rgba(67, 132, 213, 0.13);
}

.form-footer {
  position: absolute;
  right: clamp(44px, 7vw, 112px);
  bottom: 32px;
  margin: 0;
  color: #a2aaa6;
  font-size: 11px;
}

@media (max-width: 900px) {
  .login-page {
    grid-template-columns: 1fr;
  }

  .identity-panel {
    min-height: auto;
    padding: 28px 32px;
  }

  .identity-content,
  .identity-footer,
  .graph-motif {
    display: none;
  }

  .form-panel {
    min-height: calc(100vh - 94px);
    padding: 48px 28px 80px;
  }
}

@media (max-width: 560px) {
  .identity-panel {
    display: none;
  }

  .form-panel {
    min-height: 100vh;
    align-items: flex-start;
    padding-top: 14vh;
  }

  .mobile-brand {
    display: block;
    margin-bottom: 36px !important;
    color: #1f63bd !important;
    font-weight: 700;
    letter-spacing: 0.06em;
  }

  .form-footer {
    right: 28px;
  }
}
</style>
