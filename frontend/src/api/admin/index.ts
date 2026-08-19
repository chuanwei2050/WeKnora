import { del, get, patch, post, put } from '@/utils/request'

export type TenantStatus = 'active' | 'suspended'
export type TenantUserRole = 'tenant_admin' | 'member'
export type KnowledgeBaseAccessMode = 'all' | 'selected'

export interface AdminTenant {
  id: number
  name: string
  description: string
  status: TenantStatus
  business: string
  storage_quota: number
  storage_used: number
  can_delete: boolean
  admin_username?: string
  created_at: string
  updated_at: string
}

export interface AdminUser {
  id: string
  username: string
  nickname: string
  email: string
  tenant_id: number
  role: TenantUserRole | 'platform_admin'
  knowledge_base_access_mode: KnowledgeBaseAccessMode
  knowledge_base_ids: string[]
  can_delete: boolean
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface PageResult<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

interface ApiResponse<T> {
  success: boolean
  data: T
}

export interface TenantInput {
  name: string
  description: string
  business: string
  storage_quota: number
  admin_username: string
  admin_password: string
}

export interface TenantUserInput {
  username: string
  nickname: string
  password: string
  role: TenantUserRole
  knowledge_base_access_mode: KnowledgeBaseAccessMode
  knowledge_base_ids: string[]
}

interface TenantUserUpdateBase {
  username: string
  nickname: string
  password: string
  role: TenantUserRole
}

export type TenantUserUpdateInput = TenantUserUpdateBase & (
  | { knowledge_base_access_mode: 'all'; knowledge_base_ids: [] }
  | { knowledge_base_access_mode: 'selected'; knowledge_base_ids: string[] }
)

export interface AdminKnowledgeBase {
  id: string
  name: string
}

export interface CreatedTenant {
  tenant: AdminTenant
  initial_admin: { username: string; password: string }
}

function pageQuery(params: { keyword?: string; page: number; pageSize: number }): string {
  const query = new URLSearchParams({ page: String(params.page), page_size: String(params.pageSize) })
  if (params.keyword?.trim()) query.set('keyword', params.keyword.trim())
  return query.toString()
}

export async function listAdminTenants(params: { keyword?: string; page: number; pageSize: number }): Promise<PageResult<AdminTenant>> {
  return (await get<ApiResponse<PageResult<AdminTenant>>>(`/api/v1/admin/tenants?${pageQuery(params)}`)).data
}

export async function createAdminTenant(input: TenantInput): Promise<CreatedTenant> {
  return (await post<ApiResponse<CreatedTenant>>('/api/v1/admin/tenants', input)).data
}

export async function updateAdminTenant(tenantId: number, input: TenantInput): Promise<AdminTenant> {
  return (await put<ApiResponse<AdminTenant>>(`/api/v1/admin/tenants/${tenantId}`, input)).data
}

export async function updateAdminTenantStatus(tenantId: number, status: TenantStatus): Promise<AdminTenant> {
  return (await patch<ApiResponse<AdminTenant>>(`/api/v1/admin/tenants/${tenantId}/status`, { status })).data
}

export async function deleteAdminTenant(tenantId: number): Promise<void> {
  await del(`/api/v1/admin/tenants/${tenantId}`)
}

export async function listTenantUsers(tenantId: number, params: { keyword?: string; page: number; pageSize: number }): Promise<PageResult<AdminUser>> {
  return (await get<ApiResponse<PageResult<AdminUser>>>(`/api/v1/admin/tenants/${tenantId}/users?${pageQuery(params)}`)).data
}

export async function createTenantUser(tenantId: number, input: TenantUserInput): Promise<AdminUser> {
  return (await post<ApiResponse<AdminUser>>(`/api/v1/admin/tenants/${tenantId}/users`, input)).data
}

export async function updateTenantUser(tenantId: number, userId: string, input: TenantUserUpdateInput): Promise<AdminUser> {
  return (await put<ApiResponse<AdminUser>>(`/api/v1/admin/tenants/${tenantId}/users/${userId}`, input)).data
}

export async function deleteTenantUser(tenantId: number, userId: string): Promise<void> {
  await del(`/api/v1/admin/tenants/${tenantId}/users/${userId}`)
}

export async function listTenantKnowledgeBases(tenantId: number): Promise<AdminKnowledgeBase[]> {
  return (await get<ApiResponse<AdminKnowledgeBase[]>>(`/api/v1/admin/tenants/${tenantId}/knowledge-bases`)).data
}

export async function resetTenantUserPassword(tenantId: number, userId: string, password: string): Promise<void> {
  await put(`/api/v1/admin/tenants/${tenantId}/users/${userId}/password`, { password })
}

export async function updateTenantUserRole(tenantId: number, userId: string, role: TenantUserRole): Promise<AdminUser> {
  return (await patch<ApiResponse<AdminUser>>(`/api/v1/admin/tenants/${tenantId}/users/${userId}/role`, { role })).data
}

export async function updateTenantUserStatus(tenantId: number, userId: string, isActive: boolean): Promise<AdminUser> {
  return (await patch<ApiResponse<AdminUser>>(`/api/v1/admin/tenants/${tenantId}/users/${userId}/status`, { is_active: isActive })).data
}
