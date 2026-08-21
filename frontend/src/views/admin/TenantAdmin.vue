<template>
  <section class="admin-page">
    <header class="page-header">
      <div>
        <h1>租户管理</h1>
      </div>
      <t-button theme="primary" @click="openCreate">新增租户</t-button>
    </header>

    <div class="toolbar">
      <t-input v-model="keyword" clearable placeholder="搜索租户名称" @enter="search" @clear="search" />
      <t-button variant="outline" @click="search">查询</t-button>
    </div>

    <div class="table-card">
      <div v-if="loading" class="state">正在加载租户…</div>
      <div v-else-if="tenants.length === 0" class="state">暂无租户</div>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr><th>租户 ID</th><th>租户名称</th><th>用户名</th><th>存储配额</th><th>状态</th><th>创建时间</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="tenant in tenants" :key="tenant.id">
              <td>{{ tenant.id }}</td>
              <td><strong>{{ tenant.name }}</strong></td>
              <td>{{ tenant.admin_username || '—' }}</td>
              <td>{{ formatBytes(tenant.storage_used) }} / {{ formatBytes(tenant.storage_quota) }}</td>
              <td><t-tag :theme="tenant.status === 'active' ? 'success' : 'warning'" variant="light">{{ tenant.status === 'active' ? '已启用' : '已停用' }}</t-tag></td>
              <td>{{ formatDate(tenant.created_at) }}</td>
              <td><div class="actions">
                <t-link theme="primary" @click="manageUsers(tenant)">用户</t-link>
                <t-link theme="primary" @click="openEdit(tenant)">编辑</t-link>
                <t-link theme="primary" @click="openIntegration(tenant)">{{ tenantIntegrationClient(tenant) ? '接入信息' : '创建接入' }}</t-link>
                <t-link :disabled="isHomeTenant(tenant)" :theme="tenant.status === 'active' ? 'warning' : 'success'" @click="toggleStatus(tenant)">{{ tenant.status === 'active' ? '停用' : '启用' }}</t-link>
                <t-tooltip :content="tenant.can_delete ? '删除未使用租户' : '租户已投入使用，请改为停用'">
                  <t-link :disabled="!tenant.can_delete" theme="danger" @click="removeTenant(tenant)">删除</t-link>
                </t-tooltip>
              </div></td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="pagination">
        <t-pagination v-model="page" :total="total" :page-size="pageSize" :show-page-size="false" @change="loadTenants" />
      </footer>
    </div>

    <t-dialog v-model:visible="editorVisible" :header="editingId ? '编辑租户' : '新增租户'" :confirm-btn="{ content: '保存', loading: saving }" @confirm="saveTenant">
      <t-form label-align="top">
        <t-form-item label="租户名称" required><t-input v-model="form.name" maxlength="255" /></t-form-item>
        <t-form-item label="存储配额（GB）" required>
          <div class="field-control">
            <t-input-number v-model="form.storageQuotaGb" :min="0" :step="1" theme="column" style="width: 100%" />
            <div class="field-hint">请输入整数，0 表示不限制存储容量</div>
          </div>
        </t-form-item>
        <t-form-item label="登录用户名">
          <div class="field-control">
            <t-input v-model="form.adminUsername" :placeholder="editingId ? '请输入管理员用户名' : '留空则自动生成'" maxlength="100" />
            <div class="field-hint">仅支持英文字母、数字、点、下划线和短横线，不能使用中文</div>
          </div>
        </t-form-item>
        <t-form-item :label="editingId ? '重置登录密码' : '登录密码'">
          <div class="field-control">
            <t-input v-model="form.adminPassword" type="password" :placeholder="editingId ? '留空表示不修改密码' : ''" maxlength="72" autocomplete="new-password" />
            <div class="field-hint">8–72 位，仅支持字母、数字、特殊字符</div>
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="integrationVisible" header="创建第三方项目接入" :confirm-btn="{ content: '创建并生成密钥', loading: integrationSaving }" @confirm="createIntegration">
      <t-form label-align="top">
        <t-form-item label="接入项目名称" required><t-input v-model="integrationForm.projectName" /></t-form-item>
        <t-form-item label="身份提供方 ID" required><t-input v-model="integrationForm.providerId" /></t-form-item>
        <t-form-item label="绑定租户管理员" required><t-select v-model="integrationForm.administratorUserId" :options="integrationAdministratorOptions" :loading="integrationAdministratorsLoading" /></t-form-item>
        <t-form-item label="第三方项目 Origin" required>
          <div class="field-control"><t-input v-model="integrationForm.allowedOrigin" placeholder="例如：https://bidder.example.com" /><div class="field-hint">只填写协议、域名和端口，不要包含路径。</div></div>
        </t-form-item>
        <div class="field-hint">将绑定租户 {{ integrationTenant?.id }}，并授权该租户当前及未来创建的全部知识库。</div>
        <t-alert v-if="isInsecureDeployment" theme="warning" message="当前知识库使用 HTTP。跨站 iframe 可能拦截第三方 Cookie；请优先配置 HTTPS，或确认嵌入端已改用 Bearer session_token。" />
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="integrationCredentialVisible" header="第三方接入信息" :footer="false" width="640px">
      <p class="credential-note">可随时查看并复制完整接入配置（含 Client Secret），与模型 API Key 一样由平台管理员管理。{{ integrationSecretLoading ? '正在加载密钥…' : (integrationSecretRevealed ? '' : '当前密钥创建于可回看能力上线前，请先重新生成后再复制。') }}</p>
      <pre class="integration-package">{{ integrationPackageText }}</pre>
      <div class="credential-actions">
        <t-button theme="primary" :disabled="!integrationSecretRevealed || integrationSecretLoading" :loading="integrationSecretLoading" @click="copyIntegrationPackage">复制接入信息</t-button>
        <t-button v-if="viewingIntegrationClient" variant="outline" :loading="integrationRotating" @click="rotateIntegrationSecret">重新生成密钥</t-button>
      </div>
    </t-dialog>

    <t-dialog v-model:visible="credentialVisible" header="租户创建成功" :footer="false">
      <p class="credential-note">请将以下初始凭据交给租户管理员，首次登录后应立即重置密码。</p>
      <dl class="credentials">
        <div><dt>用户名</dt><dd><code>{{ createdCredential.username }}</code></dd></div>
        <div><dt>初始密码</dt><dd><code>{{ createdCredential.password }}</code></dd></div>
      </dl>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { createAdminTenant, createIntegrationClient, createIntegrationIdentityProvider, deleteAdminTenant, listAdminTenants, listIntegrationClients, listIntegrationIdentityProviders, listTenantUsers, revealIntegrationClientSecret, rotateIntegrationClientSecret, updateAdminTenant, updateAdminTenantStatus, type AdminTenant, type IntegrationClient } from '@/api/admin'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const tenants = ref<AdminTenant[]>([])
const integrationClients = ref<IntegrationClient[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const editorVisible = ref(false)
const credentialVisible = ref(false)
const createdCredential = reactive({ username: '', password: '' })
const editingId = ref<number | null>(null)
const integrationVisible = ref(false)
const integrationCredentialVisible = ref(false)
const integrationSaving = ref(false)
const integrationRotating = ref(false)
const integrationSecretRevealed = ref(false)
const integrationSecretLoading = ref(false)
const integrationTenant = ref<AdminTenant | null>(null)
const viewingIntegrationClient = ref<IntegrationClient | null>(null)
const integrationViewToken = ref(0)
const integrationForm = reactive({ projectName: 'Bidder Agent', providerId: 'bidder-agent', administratorUserId: '', allowedOrigin: '' })
const integrationAdministratorsLoading = ref(false)
const integrationAdministratorOptions = ref<Array<{ label: string; value: string }>>([])
const integrationPackageText = ref('')
const isInsecureDeployment = computed(() => window.location.protocol !== 'https:')
const form = reactive({ name: '', business: '', description: '', storageQuotaGb: 10, adminUsername: '', adminPassword: 'Admin@123456' })

const formatBytes = (value: number) => value > 0 ? `${(value / 1024 / 1024 / 1024).toFixed(value >= 10 * 1024 ** 3 ? 0 : 1)} GB` : '0 GB'
const formatDate = (value: string) => value ? new Date(value).toLocaleString('zh-CN') : '—'
const isHomeTenant = (tenant: AdminTenant) => String(tenant.id) === String(authStore.currentTenantId)
const tenantIntegrationClient = (tenant: AdminTenant) => integrationClients.value.find(client => Number(client.tenant_id) === Number(tenant.id) && client.enabled)

function buildIntegrationPackage(clientId: string, secret: string | null, integrationOrigin: string) {
  const secretLine = secret
    ? `WEKNORA_INTEGRATION_CLIENT_SECRET=${secret}`
    : 'WEKNORA_INTEGRATION_CLIENT_SECRET=<暂不可回看，请重新生成密钥>'
  return [
    'WEKNORA_ENABLED=true',
    `WEKNORA_INTEGRATION_BASE_URL=${window.location.origin}/api/integration/v1`,
    `WEKNORA_FRONTEND_URL=${window.location.origin}`,
    `WEKNORA_FRONTEND_ORIGIN=${window.location.origin}`,
    `WEKNORA_INTEGRATION_ORIGIN=${integrationOrigin || '<请填写第三方项目 Origin>'}`,
    'WEKNORA_INTEGRATION_TENANT_ID=<第三方项目内部租户ID>',
    `WEKNORA_INTEGRATION_CLIENT_ID=${clientId}`,
    secretLine,
    'WEKNORA_TIMEOUT_SECONDS=60',
  ].join('\n')
}

async function loadIntegrationClients() {
  try {
    integrationClients.value = await listIntegrationClients()
  } catch {
    integrationClients.value = []
  }
}

async function loadTenants() {
  loading.value = true
  try {
    const [result] = await Promise.all([
      listAdminTenants({ keyword: keyword.value, page: page.value, pageSize }),
      loadIntegrationClients(),
    ])
    tenants.value = result.items
    total.value = result.total
  } catch (error) {
    MessagePlugin.error((error as { message?: string }).message || '租户列表加载失败')
  } finally {
    loading.value = false
  }
}

function search() { page.value = 1; void loadTenants() }
function resetForm() { form.name = ''; form.business = ''; form.description = ''; form.storageQuotaGb = 10; form.adminUsername = ''; form.adminPassword = 'Admin@123456' }
function openCreate() { editingId.value = null; resetForm(); editorVisible.value = true }
function openEdit(tenant: AdminTenant) {
  editingId.value = tenant.id
  form.name = tenant.name
  form.business = tenant.business || ''
  form.description = tenant.description || ''
  form.storageQuotaGb = Math.max(0, Math.round(tenant.storage_quota / 1024 ** 3))
  form.adminUsername = tenant.admin_username || `tenant_admin_${tenant.id}`
  form.adminPassword = ''
  editorVisible.value = true
}

async function openIntegration(tenant: AdminTenant) {
  const existing = tenantIntegrationClient(tenant)
  if (existing) {
    const viewToken = ++integrationViewToken.value
    viewingIntegrationClient.value = existing
    integrationSecretRevealed.value = false
    integrationSecretLoading.value = true
    integrationPackageText.value = buildIntegrationPackage(existing.id, null, existing.allowed_origins?.[0] || '')
    integrationCredentialVisible.value = true
    try {
      const revealed = await revealIntegrationClientSecret(existing.id)
      if (viewToken !== integrationViewToken.value) return
      integrationSecretRevealed.value = true
      integrationPackageText.value = buildIntegrationPackage(existing.id, revealed.client_secret, existing.allowed_origins?.[0] || '')
    } catch {
      // Legacy clients created before recoverable secrets remain rotatable only.
    } finally {
      if (viewToken === integrationViewToken.value) integrationSecretLoading.value = false
    }
    return
  }
  viewingIntegrationClient.value = null
  integrationSecretLoading.value = false
  integrationTenant.value = tenant
  integrationForm.projectName = 'Bidder Agent'
  integrationForm.providerId = 'bidder-agent'
  integrationForm.administratorUserId = ''
  integrationForm.allowedOrigin = ''
  integrationAdministratorOptions.value = []
  integrationVisible.value = true
  integrationAdministratorsLoading.value = true
  try {
    const administrators: Array<{ label: string; value: string }> = []
    let currentPage = 1
    let total = 0
    do {
      const users = await listTenantUsers(tenant.id, { page: currentPage, pageSize: 100 })
      total = users.total
      administrators.push(...users.items.filter(user => user.role === 'tenant_admin' && user.is_active).map(user => ({ label: user.username, value: user.id })))
      currentPage += 1
    } while ((currentPage - 1) * 100 < total)
    integrationAdministratorOptions.value = administrators
    if (administrators.length === 1) integrationForm.administratorUserId = administrators[0].value
    if (administrators.length === 0) MessagePlugin.warning('该租户没有可用的租户管理员')
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '租户管理员加载失败') }
  finally { integrationAdministratorsLoading.value = false }
}

async function createIntegration() {
  const tenant = integrationTenant.value
  if (!tenant || !integrationForm.projectName.trim() || !integrationForm.providerId.trim() || !integrationForm.administratorUserId || !integrationForm.allowedOrigin.trim()) { MessagePlugin.warning('请填写完整接入信息'); return }
  if (integrationForm.projectName.trim().length > 128 || integrationForm.providerId.trim().length > 64) { MessagePlugin.warning('项目名称或身份提供方 ID 过长'); return }
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(integrationForm.providerId.trim())) { MessagePlugin.warning('身份提供方 ID 仅支持英文字母、数字、点、下划线和短横线'); return }
  let origin: URL
  try { origin = new URL(integrationForm.allowedOrigin.trim()) } catch { MessagePlugin.warning('第三方项目 Origin 格式不正确'); return }
  if (!['http:', 'https:'].includes(origin.protocol) || origin.pathname !== '/' || origin.search || origin.hash || origin.username || origin.password) { MessagePlugin.warning('Origin 只能包含协议、域名和端口'); return }
  integrationSaving.value = true
  try {
    const providerId = integrationForm.providerId.trim()
    const providers = await listIntegrationIdentityProviders()
    if (!providers.some(provider => provider.id === providerId)) await createIntegrationIdentityProvider({ id: providerId, name: `${integrationForm.projectName.trim()} 用户中心` })
    const created = await createIntegrationClient({ name: integrationForm.projectName.trim(), tenant_id: tenant.id, identity_provider_id: providerId, administrator_user_id: integrationForm.administratorUserId, scopes: ['kb:list', 'rag:search', 'table:analyze', 'chat:read', 'chat:write', 'knowledge:read', 'knowledge:write', 'file:read'], knowledge_base_access_mode: 'all', knowledge_base_ids: [], allowed_origins: [origin.origin], role_mappings: { tenant_admin: 'tenant_admin', member: 'member' }, max_role: 'tenant_admin' })
    viewingIntegrationClient.value = { id: created.client_id, name: integrationForm.projectName.trim(), tenant_id: tenant.id, identity_provider_id: providerId, administrator_user_id: integrationForm.administratorUserId, scopes: [], knowledge_base_access_mode: 'all', knowledge_base_ids: [], allowed_origins: [origin.origin], enabled: true }
    integrationSecretRevealed.value = true
    integrationPackageText.value = buildIntegrationPackage(created.client_id, created.client_secret, origin.origin)
    integrationVisible.value = false
    integrationCredentialVisible.value = true
    await loadIntegrationClients()
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '接入创建失败') }
  finally { integrationSaving.value = false }
}

async function rotateIntegrationSecret() {
  const client = viewingIntegrationClient.value
  if (!client) return
  const dialog = DialogPlugin.confirm({
    header: '重新生成密钥',
    body: '重新生成后旧密钥将逐步失效，请确认第三方已准备好替换新密钥。',
    confirmBtn: '重新生成',
    onConfirm: async () => {
      integrationRotating.value = true
      try {
        const rotated = await rotateIntegrationClientSecret(client.id)
        integrationSecretRevealed.value = true
        integrationPackageText.value = buildIntegrationPackage(client.id, rotated.client_secret, client.allowed_origins?.[0] || '')
        MessagePlugin.success('新密钥已生成，可复制接入信息')
        dialog.destroy()
      } catch (error) {
        MessagePlugin.error((error as { message?: string }).message || '重新生成密钥失败')
      } finally {
        integrationRotating.value = false
      }
    },
  })
}

async function copyIntegrationPackage() {
  try {
    if (navigator.clipboard) await navigator.clipboard.writeText(integrationPackageText.value)
    else {
      const textarea = document.createElement('textarea')
      textarea.value = integrationPackageText.value
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      textarea.remove()
    }
    MessagePlugin.success('接入信息已复制')
  } catch { MessagePlugin.error('复制失败，请手动选择接入信息复制') }
}

async function saveTenant() {
  if (!form.name.trim()) { MessagePlugin.warning('请输入租户名称'); return }
  if (!Number.isInteger(form.storageQuotaGb) || form.storageQuotaGb < 0) { MessagePlugin.warning('存储配额必须是大于或等于 0 的整数'); return }
  if (form.adminUsername.trim() && form.adminUsername.trim().length < 2) { MessagePlugin.warning('管理员用户名至少 2 位'); return }
  if (form.adminUsername.trim() && !/^[A-Za-z0-9][A-Za-z0-9._-]{1,99}$/.test(form.adminUsername.trim())) { MessagePlugin.warning('管理员用户名不能使用中文，仅支持英文字母、数字、点、下划线和短横线'); return }
  if (form.adminPassword && form.adminPassword.length < 8) { MessagePlugin.warning('管理员密码至少 8 位'); return }
  if (form.adminPassword.length > 72) { MessagePlugin.warning('管理员密码不能超过 72 位'); return }
  if (/\p{Script=Han}/u.test(form.adminPassword)) { MessagePlugin.warning('管理员密码不能包含中文'); return }
  if (/\s/u.test(form.adminPassword)) { MessagePlugin.warning('管理员密码不能包含空格、换行等空白字符'); return }
  if (form.adminPassword && !/^[\x21-\x7E]+$/.test(form.adminPassword)) { MessagePlugin.warning('管理员密码仅支持英文字母、数字和英文特殊字符'); return }
  if (form.adminPassword && (!/[A-Za-z]/.test(form.adminPassword) || !/\d/.test(form.adminPassword))) { MessagePlugin.warning('管理员密码必须同时包含英文字母和数字'); return }
  saving.value = true
  const input = { name: form.name.trim(), business: form.business.trim(), description: form.description.trim(), storage_quota: form.storageQuotaGb * 1024 ** 3, admin_username: form.adminUsername.trim(), admin_password: form.adminPassword }
  try {
    if (editingId.value) await updateAdminTenant(editingId.value, input)
    else {
      const created = await createAdminTenant(input)
      createdCredential.username = created.initial_admin.username
      createdCredential.password = created.initial_admin.password
      credentialVisible.value = true
    }
    MessagePlugin.success(editingId.value ? '租户已更新' : '租户已创建')
    editorVisible.value = false
    await loadTenants()
  } catch (error) {
    MessagePlugin.error((error as { message?: string }).message || '保存失败')
  } finally { saving.value = false }
}

async function toggleStatus(tenant: AdminTenant) {
  if (isHomeTenant(tenant)) return
  try {
    await updateAdminTenantStatus(tenant.id, tenant.status === 'active' ? 'suspended' : 'active')
    MessagePlugin.success(tenant.status === 'active' ? '租户已停用' : '租户已启用')
    await loadTenants()
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '状态更新失败') }
}

function removeTenant(tenant: AdminTenant) {
  if (!tenant.can_delete) return
  const dialog = DialogPlugin.confirm({ header: '删除租户', body: `确认删除租户“${tenant.name}”？数据将保留，但该租户将无法继续访问。`, theme: 'danger', confirmBtn: '确认删除', onConfirm: async () => {
    try { await deleteAdminTenant(tenant.id); MessagePlugin.success('租户已删除'); dialog.destroy(); await loadTenants() }
    catch (error) { MessagePlugin.error((error as { message?: string }).message || '删除失败') }
  } })
}

function manageUsers(tenant: AdminTenant) {
  authStore.setSelectedTenant(null, null)
  void router.push({ path: '/platform/admin/users', query: { tenant_id: String(tenant.id), tenant_name: tenant.name } })
}

onMounted(loadTenants)
</script>

<style scoped lang="less">
.admin-page { flex: 1; min-width: 0; overflow: auto; padding: 40px 48px; background: #f5f7fb; color: #17233d; }
.page-header { display: flex; justify-content: space-between; gap: 24px; align-items: flex-end; margin-bottom: 28px; h1 { margin: 4px 0 8px; font-size: 30px; } p { margin: 0; color: #65728a; } }
.eyebrow { color: #2468bd !important; font-size: 12px; letter-spacing: .12em; font-weight: 700; }
.toolbar { display: flex; gap: 12px; width: min(520px, 100%); margin-bottom: 18px; }
.table-card { background: #fff; border: 1px solid #e1e6ef; border-radius: 8px; overflow: hidden; }
.table-scroll { overflow-x: auto; }
table { width: 100%; min-width: 1120px; border-collapse: collapse; th, td { padding: 16px 18px; border-bottom: 1px solid #edf0f5; text-align: left; } th { background: #f8fafd; color: #596780; font-size: 13px; } td { font-size: 14px; } strong, small { display: block; } small { margin-top: 5px; color: #8a95a8; max-width: 280px; } }
.credential-note { color: #65728a; line-height: 1.7; }
.credential-actions { display: flex; gap: 12px; flex-wrap: wrap; }
.field-control { width: 100%; min-width: 0; }
.field-hint { margin-top: 6px; color: #7b879b; font-size: 12px; line-height: 1.5; }
.credentials { margin: 16px 0 0; border: 1px solid #e1e6ef; border-radius: 8px; overflow: hidden; > div { display: grid; grid-template-columns: 100px 1fr; padding: 14px 16px; &:not(:last-child) { border-bottom: 1px solid #edf0f5; } } dt { color: #65728a; } dd { margin: 0; } code { color: #174a7c; font-size: 14px; } }
.actions { display: flex; gap: 14px; white-space: nowrap; }
.integration-package { padding: 14px; overflow: auto; border-radius: 6px; background: #f5f7fb; white-space: pre-wrap; word-break: break-all; }
.state { padding: 64px; text-align: center; color: #7b879b; }
.pagination { display: flex; justify-content: space-between; align-items: center; padding: 14px 18px; color: #7b879b; }
@media (max-width: 900px) { .admin-page { padding: 24px; } .page-header { align-items: flex-start; } }
</style>
