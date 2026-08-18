<template>
  <section class="admin-page">
    <header class="page-header">
      <div>
        <p class="eyebrow">{{ isPlatformContext ? 'PLATFORM ADMINISTRATION' : 'TENANT ADMINISTRATION' }}</p>
        <h1>用户管理</h1>
        <p>{{ tenantLabel }} · 创建账号、编辑访问权限和控制账号状态。</p>
      </div>
      <div class="header-actions">
        <t-button v-if="isPlatformContext" variant="outline" @click="router.push('/platform/admin/tenants')">返回租户列表</t-button>
        <t-button theme="primary" @click="openCreate">新增用户</t-button>
      </div>
    </header>

    <div class="toolbar">
      <t-input v-model="keyword" clearable placeholder="搜索用户名" @enter="search" @clear="search" />
      <t-button variant="outline" @click="search">查询</t-button>
    </div>

    <div class="table-card">
      <div v-if="!tenantId" class="state">请先选择要管理的租户</div>
      <div v-else-if="loading" class="state">正在加载用户…</div>
      <div v-else-if="users.length === 0" class="state">暂无用户</div>
      <div v-else class="table-scroll">
        <table>
          <thead><tr><th>用户</th><th>角色</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td><strong>{{ user.username }}</strong></td>
              <td><t-tag variant="light">{{ roleLabel(user.role) }}</t-tag></td>
              <td><t-tag :theme="user.is_active ? 'success' : 'default'" variant="light">{{ user.is_active ? '已启用' : '已禁用' }}</t-tag></td>
              <td>{{ formatDate(user.created_at) }}</td>
              <td class="actions">
                <t-link :disabled="user.role === 'platform_admin'" theme="primary" @click="openEdit(user)">编辑</t-link>
                <t-link :disabled="user.role === 'platform_admin' || user.id === authStore.currentUserId" :theme="user.is_active ? 'danger' : 'success'" @click="toggleUser(user)">{{ user.is_active ? '禁用' : '启用' }}</t-link>
                <t-tooltip :content="user.can_delete ? '删除用户' : '管理员、当前用户或已有文档操作记录的用户不能删除'">
                  <t-link :disabled="!user.can_delete" theme="danger" @click="removeUser(user)">删除</t-link>
                </t-tooltip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="pagination">
        <t-pagination v-model="page" :total="total" :page-size="pageSize" :show-page-size="false" @change="loadUsers" />
      </footer>
    </div>

    <t-dialog v-model:visible="createVisible" header="新增用户" :confirm-btn="{ content: '创建账号', loading: saving }" @confirm="saveUser">
      <t-form label-align="top">
        <t-form-item label="用户名" required><t-input v-model="createForm.username" class="form-control" maxlength="100" autocomplete="off" /></t-form-item>
        <t-form-item label="初始密码" required>
          <div class="field-control">
            <t-input v-model="createForm.password" class="form-control" type="password" maxlength="72" autocomplete="new-password" />
            <div class="field-hint">8–72 位，须包含英文字母和数字，可使用英文特殊字符，不能包含空白或中文</div>
          </div>
        </t-form-item>
        <t-form-item label="角色" required>
          <t-select v-model="createForm.role" class="form-control" :disabled="!authStore.canAccessAllTenants">
            <t-option value="member" label="普通成员" />
            <t-option v-if="authStore.canAccessAllTenants" value="tenant_admin" label="租户管理员" />
          </t-select>
        </t-form-item>
        <t-form-item label="知识库权限" required>
          <div class="field-control">
            <t-select v-model="createForm.knowledgeBaseAccessMode" class="form-control" :disabled="createForm.role !== 'member'">
              <t-option value="all" label="全部知识库" />
              <t-option value="selected" label="指定知识库" />
            </t-select>
            <t-select v-if="createForm.role === 'member' && createForm.knowledgeBaseAccessMode === 'selected'" v-model="createForm.knowledgeBaseIds" class="knowledge-select form-control" multiple clearable filterable placeholder="请选择可查看的知识库">
              <t-option v-for="kb in knowledgeBases" :key="kb.id" :value="kb.id" :label="kb.name" />
            </t-select>
            <div class="field-hint">“全部知识库”也会自动包含以后新建的知识库</div>
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>

    <t-dialog v-model:visible="editVisible" header="编辑用户" :confirm-btn="{ content: '保存', loading: saving }" @confirm="saveEdit">
      <t-form label-align="top">
        <t-form-item label="用户名" required><t-input v-model="editForm.username" class="form-control" maxlength="100" autocomplete="off" /></t-form-item>
        <t-form-item label="重置密码">
          <div class="field-control">
            <t-input v-model="editForm.password" class="form-control" type="password" maxlength="72" placeholder="留空表示不修改密码" autocomplete="new-password" />
            <div class="field-hint">8–72 位，须包含英文字母和数字，可使用英文特殊字符，不能包含空白或中文</div>
          </div>
        </t-form-item>
        <t-form-item label="角色" required>
          <div class="field-control">
            <t-select v-model="editForm.role" class="form-control" :disabled="editTarget?.role !== 'member'">
              <t-option value="member" label="普通成员" />
              <t-option value="tenant_admin" label="租户管理员" />
            </t-select>
            <div v-if="editTarget?.role !== 'member'" class="field-hint">管理员角色不可修改</div>
          </div>
        </t-form-item>
        <t-form-item label="知识库权限" required>
          <div class="field-control">
            <t-select v-model="editForm.knowledgeBaseAccessMode" class="form-control" :disabled="editForm.role !== 'member'">
              <t-option value="all" label="全部知识库" />
              <t-option value="selected" label="指定知识库" />
            </t-select>
            <t-select v-if="editForm.role === 'member' && editForm.knowledgeBaseAccessMode === 'selected'" v-model="editForm.knowledgeBaseIds" class="knowledge-select form-control" multiple clearable filterable placeholder="请选择可查看的知识库">
              <t-option v-for="kb in knowledgeBases" :key="kb.id" :value="kb.id" :label="kb.name" />
            </t-select>
            <div class="field-hint">“全部知识库”也会自动包含以后新建的知识库</div>
          </div>
        </t-form-item>
      </t-form>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { createTenantUser, deleteTenantUser, listTenantKnowledgeBases, listTenantUsers, updateTenantUser, updateTenantUserStatus, type AdminKnowledgeBase, type AdminUser, type KnowledgeBaseAccessMode, type TenantUserRole, type TenantUserUpdateInput } from '@/api/admin'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const isPlatformContext = computed(() => authStore.user?.role === 'platform_admin' && authStore.workspaceMode === 'platform')
const users = ref<AdminUser[]>([])
const loading = ref(false)
const saving = ref(false)
const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const createVisible = ref(false)
const editVisible = ref(false)
const editTarget = ref<AdminUser | null>(null)
const knowledgeBases = ref<AdminKnowledgeBase[]>([])
const knowledgeBasesLoaded = ref(false)
const createForm = reactive<{ username: string; password: string; role: TenantUserRole; knowledgeBaseAccessMode: KnowledgeBaseAccessMode; knowledgeBaseIds: string[] }>({ username: '', password: '', role: 'member', knowledgeBaseAccessMode: 'all', knowledgeBaseIds: [] })
const editForm = reactive<{ username: string; password: string; role: TenantUserRole; knowledgeBaseAccessMode: KnowledgeBaseAccessMode; knowledgeBaseIds: string[] }>({ username: '', password: '', role: 'member', knowledgeBaseAccessMode: 'all', knowledgeBaseIds: [] })

const tenantId = computed(() => {
  const queryTenant = Number(route.query.tenant_id)
  return Number.isSafeInteger(queryTenant) && queryTenant > 0 ? queryTenant : authStore.effectiveTenantId
})
const tenantLabel = computed(() => String(route.query.tenant_name || authStore.selectedTenantName || authStore.tenant?.name || `租户 #${tenantId.value || '—'}`))
const formatDate = (value: string) => value ? new Date(value).toLocaleString('zh-CN') : '—'
const roleLabel = (role: AdminUser['role']) => role === 'platform_admin' ? '平台管理员' : role === 'tenant_admin' ? '租户管理员' : '普通成员'

async function loadUsers() {
  if (!tenantId.value) return
  loading.value = true
  try {
    const result = await listTenantUsers(tenantId.value, { keyword: keyword.value, page: page.value, pageSize })
    users.value = result.items
    total.value = result.total
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '用户列表加载失败') }
  finally { loading.value = false }
}

function search() { page.value = 1; void loadUsers() }
async function loadKnowledgeBaseOptions() {
  if (!tenantId.value) return false
  try {
    knowledgeBases.value = await listTenantKnowledgeBases(tenantId.value)
    knowledgeBasesLoaded.value = true
    return true
  } catch (error) {
    knowledgeBases.value = []
    knowledgeBasesLoaded.value = false
    MessagePlugin.error((error as { message?: string }).message || '知识库列表加载失败')
    return false
  }
}

function openCreate() {
  createForm.username = ''; createForm.password = ''; createForm.role = 'member'; createForm.knowledgeBaseAccessMode = 'all'; createForm.knowledgeBaseIds = []
  createVisible.value = true
}

function passwordError(password: string): string | null {
  if (password.length < 8 || password.length > 72) return '密码长度必须为 8–72 位'
  if (/\s/u.test(password)) return '密码不能包含空格、换行等空白字符'
  if (!/^[\x21-\x7E]+$/.test(password)) return '密码仅支持英文字母、数字和英文特殊字符'
  if (!/[A-Za-z]/.test(password) || !/\d/.test(password)) return '密码必须同时包含英文字母和数字'
  return null
}

function usernameError(username: string): string | null {
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{1,99}$/.test(username.trim())) {
    return '用户名必须为 2–100 位，且只能包含英文字母、数字、点、下划线和短横线'
  }
  return null
}

async function saveUser() {
  if (!tenantId.value || !createForm.username.trim()) { MessagePlugin.warning('请填写完整信息'); return }
  const invalidUsername = usernameError(createForm.username)
  if (invalidUsername) { MessagePlugin.warning(invalidUsername); return }
  const invalidPassword = passwordError(createForm.password)
  if (invalidPassword) { MessagePlugin.warning(invalidPassword); return }
  saving.value = true
  try {
    await createTenantUser(tenantId.value, {
      username: createForm.username.trim(), password: createForm.password, role: createForm.role,
      knowledge_base_access_mode: createForm.role === 'member' ? createForm.knowledgeBaseAccessMode : 'all',
      knowledge_base_ids: createForm.role === 'member' && createForm.knowledgeBaseAccessMode === 'selected' ? [...createForm.knowledgeBaseIds] : [],
    })
    MessagePlugin.success('用户已创建')
    createVisible.value = false
    await loadUsers()
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '创建失败') }
  finally { saving.value = false }
}

async function openEdit(user: AdminUser) {
  if (!tenantId.value || user.role === 'platform_admin') return
  editTarget.value = user
  editForm.username = user.username
  editForm.password = ''
  editForm.role = user.role
  editForm.knowledgeBaseAccessMode = user.role === 'member' ? user.knowledge_base_access_mode : 'all'
  editForm.knowledgeBaseIds = user.role === 'member' ? [...(user.knowledge_base_ids || [])] : []
  editVisible.value = true
  if (user.role === 'member' && user.knowledge_base_access_mode === 'selected' && !knowledgeBasesLoaded.value) void loadKnowledgeBaseOptions()
}

async function saveEdit() {
  if (!tenantId.value || !editTarget.value || !editForm.username.trim()) { MessagePlugin.warning('请输入用户名'); return }
  const invalidUsername = usernameError(editForm.username)
  if (invalidUsername) { MessagePlugin.warning(invalidUsername); return }
  if (editForm.password) {
    const invalidPassword = passwordError(editForm.password)
    if (invalidPassword) { MessagePlugin.warning(invalidPassword); return }
  }
  const input: TenantUserUpdateInput = editForm.knowledgeBaseAccessMode === 'all'
    ? { username: editForm.username.trim(), password: editForm.password, role: editForm.role, knowledge_base_access_mode: 'all', knowledge_base_ids: [] }
    : { username: editForm.username.trim(), password: editForm.password, role: editForm.role, knowledge_base_access_mode: 'selected', knowledge_base_ids: [...editForm.knowledgeBaseIds] }
  saving.value = true
  try {
    await updateTenantUser(tenantId.value, editTarget.value.id, input)
    MessagePlugin.success('用户已更新')
    editVisible.value = false
    await loadUsers()
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '用户更新失败') }
  finally { saving.value = false }
}

async function toggleUser(user: AdminUser) {
  if (!tenantId.value || user.id === authStore.currentUserId) return
  try {
    await updateTenantUserStatus(tenantId.value, user.id, !user.is_active)
    MessagePlugin.success(user.is_active ? '用户已禁用' : '用户已启用')
    await loadUsers()
  } catch (error) { MessagePlugin.error((error as { message?: string }).message || '状态更新失败') }
}

function removeUser(user: AdminUser) {
  if (!tenantId.value || !user.can_delete) return
  const targetTenantID = tenantId.value
  const dialog = DialogPlugin.confirm({ header: '删除用户', body: `确认删除用户“${user.username}”？该用户将无法继续登录。`, theme: 'danger', confirmBtn: '确认删除', onConfirm: async () => {
    try { await deleteTenantUser(targetTenantID, user.id); MessagePlugin.success('用户已删除'); dialog.destroy(); await loadUsers() }
    catch (error) { MessagePlugin.error((error as { message?: string }).message || '删除失败') }
  } })
}

watch(() => [createForm.role, createForm.knowledgeBaseAccessMode], ([role, mode]) => {
  if (createVisible.value && role === 'member' && mode === 'selected' && !knowledgeBasesLoaded.value) void loadKnowledgeBaseOptions()
})
watch(() => [editForm.role, editForm.knowledgeBaseAccessMode], ([role, mode]) => {
  if (editVisible.value && role === 'member' && mode === 'selected' && !knowledgeBasesLoaded.value) void loadKnowledgeBaseOptions()
})
watch(tenantId, () => { page.value = 1; knowledgeBases.value = []; knowledgeBasesLoaded.value = false; void loadUsers() })
onMounted(loadUsers)
</script>

<style scoped lang="less">
.admin-page { flex: 1; min-width: 0; overflow: auto; padding: 40px 48px; background: #f5f7fb; color: #17233d; }
.page-header { display: flex; justify-content: space-between; gap: 24px; align-items: flex-end; margin-bottom: 28px; h1 { margin: 4px 0 8px; font-size: 30px; } p { margin: 0; color: #65728a; } }
.header-actions { display: flex; gap: 12px; }
.eyebrow { color: #2468bd !important; font-size: 12px; letter-spacing: .12em; font-weight: 700; }
.toolbar { display: flex; gap: 12px; width: min(520px, 100%); margin-bottom: 18px; }
.table-card { background: #fff; border: 1px solid #e1e6ef; border-radius: 8px; overflow: hidden; }
.table-scroll { overflow-x: auto; }
table { width: 100%; min-width: 800px; border-collapse: collapse; th, td { padding: 16px 18px; border-bottom: 1px solid #edf0f5; text-align: left; } th { background: #f8fafd; color: #596780; font-size: 13px; } td { font-size: 14px; } strong { display: block; } }
.actions { display: flex; gap: 16px; white-space: nowrap; }
.state { padding: 64px; text-align: center; color: #7b879b; }
.pagination { display: flex; justify-content: space-between; align-items: center; padding: 14px 18px; color: #7b879b; }
.field-control { width: 100%; min-width: 0; }
.form-control { width: 100%; }
.field-hint { margin-top: 6px; color: #7b879b; font-size: 12px; line-height: 1.5; }
.knowledge-select { margin-top: 10px; }
@media (max-width: 900px) { .admin-page { padding: 24px; } .page-header { align-items: flex-start; } }
</style>
