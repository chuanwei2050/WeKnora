<template>
  <section class="admin-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">PLATFORM ADMINISTRATION</p>
        <h1>租户管理</h1>
        <p>创建租户、调整资源信息，并控制租户启用状态。</p>
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
            <tr><th>租户</th><th>存储用量</th><th>状态</th><th>创建时间</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="tenant in tenants" :key="tenant.id">
              <td><strong>{{ tenant.name }}</strong><small>#{{ tenant.id }} · {{ tenant.description || '暂无说明' }}</small></td>
              <td>{{ formatBytes(tenant.storage_used) }} / {{ formatBytes(tenant.storage_quota) }}</td>
              <td><t-tag :theme="tenant.status === 'active' ? 'success' : 'warning'" variant="light">{{ tenant.status === 'active' ? '已启用' : '已停用' }}</t-tag></td>
              <td>{{ formatDate(tenant.created_at) }}</td>
              <td class="actions">
                <t-link theme="primary" @click="manageUsers(tenant)">用户</t-link>
                <t-link theme="primary" @click="openEdit(tenant)">编辑</t-link>
                <t-link :disabled="isHomeTenant(tenant)" :theme="tenant.status === 'active' ? 'warning' : 'success'" @click="toggleStatus(tenant)">{{ tenant.status === 'active' ? '停用' : '启用' }}</t-link>
                <t-tooltip :content="tenant.can_delete ? '删除未使用租户' : '租户已投入使用，请改为停用'">
                  <t-link :disabled="!tenant.can_delete" theme="danger" @click="removeTenant(tenant)">删除</t-link>
                </t-tooltip>
              </td>
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
            <div class="field-hint">8–72 位，须包含英文字母和数字，可使用英文特殊字符，不能包含空白或中文{{ editingId ? '；留空表示不修改密码' : '' }}</div>
          </div>
        </t-form-item>
      </t-form>
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
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { createAdminTenant, deleteAdminTenant, listAdminTenants, updateAdminTenant, updateAdminTenantStatus, type AdminTenant } from '@/api/admin'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const tenants = ref<AdminTenant[]>([])
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
const form = reactive({ name: '', business: '', description: '', storageQuotaGb: 10, adminUsername: '', adminPassword: 'Admin@123456' })

const formatBytes = (value: number) => value > 0 ? `${(value / 1024 / 1024 / 1024).toFixed(value >= 10 * 1024 ** 3 ? 0 : 1)} GB` : '0 GB'
const formatDate = (value: string) => value ? new Date(value).toLocaleString('zh-CN') : '—'
const isHomeTenant = (tenant: AdminTenant) => String(tenant.id) === String(authStore.currentTenantId)

async function loadTenants() {
  loading.value = true
  try {
    const result = await listAdminTenants({ keyword: keyword.value, page: page.value, pageSize })
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
table { width: 100%; min-width: 980px; border-collapse: collapse; th, td { padding: 16px 18px; border-bottom: 1px solid #edf0f5; text-align: left; } th { background: #f8fafd; color: #596780; font-size: 13px; } td { font-size: 14px; } strong, small { display: block; } small { margin-top: 5px; color: #8a95a8; max-width: 280px; } }
.credential-note { color: #65728a; line-height: 1.7; }
.field-control { width: 100%; min-width: 0; }
.field-hint { margin-top: 6px; color: #7b879b; font-size: 12px; line-height: 1.5; }
.credentials { margin: 16px 0 0; border: 1px solid #e1e6ef; border-radius: 8px; overflow: hidden; > div { display: grid; grid-template-columns: 100px 1fr; padding: 14px 16px; &:not(:last-child) { border-bottom: 1px solid #edf0f5; } } dt { color: #65728a; } dd { margin: 0; } code { color: #174a7c; font-size: 14px; } }
.actions { display: flex; gap: 14px; white-space: nowrap; }
.state { padding: 64px; text-align: center; color: #7b879b; }
.pagination { display: flex; justify-content: space-between; align-items: center; padding: 14px 18px; color: #7b879b; }
@media (max-width: 900px) { .admin-page { padding: 24px; } .page-header { align-items: flex-start; } }
</style>
