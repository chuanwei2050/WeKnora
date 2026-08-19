<template>
  <div class="user-menu" :class="{ 'user-menu--collapsed': uiStore.sidebarCollapsed }" ref="menuRef">
    <!-- 用户按钮 -->
    <div class="user-button" @click="toggleMenu">
      <div class="user-avatar">
        <img v-if="userAvatar" :src="userAvatar" :alt="$t('common.avatar')" />
        <span v-else class="avatar-placeholder">{{ userInitial }}</span>
      </div>
      <template v-if="!uiStore.sidebarCollapsed">
        <div class="user-info">
          <div class="user-name">{{ userNickname }}</div>
          <div class="user-username">{{ username }}</div>
        </div>
        <t-icon :name="menuVisible ? 'chevron-up' : 'chevron-down'" class="dropdown-icon" />
      </template>
    </div>

    <!-- 下拉菜单 -->
    <Transition name="dropdown">
      <div v-if="menuVisible" class="user-dropdown" @click.stop>
        <div class="menu-item" @click="profileVisible = true; menuVisible = false">
          <t-icon name="user-circle" class="menu-icon" />
          <span>个人信息</span>
        </div>
        <div class="menu-item" @click="handleSettings">
          <t-icon name="setting" class="menu-icon" />
          <span>设置</span>
        </div>
        <div class="menu-item" @click="openPasswordDialog">
          <t-icon name="lock-on" class="menu-icon" />
          <span>修改密码</span>
        </div>
        <div class="menu-divider"></div>
        <div class="menu-item danger" @click="handleLogout">
          <t-icon name="logout" class="menu-icon" />
          <span>退出登录</span>
        </div>
      </div>
    </Transition>

    <t-dialog v-model:visible="profileVisible" header="个人信息" :footer="false" width="420px">
      <div class="profile-card">
        <div class="profile-avatar">{{ userInitial }}</div>
        <div class="profile-name">{{ userNickname }}</div>
        <div class="profile-role">{{ roleLabel }}</div>
        <dl class="profile-fields">
          <div><dt>昵称</dt><dd>{{ userNickname }}</dd></div>
          <div><dt>用户名</dt><dd>{{ username }}</dd></div>
          <div><dt>所属租户</dt><dd>{{ authStore.workspaceMode === 'platform' ? '平台级账号' : authStore.selectedTenantName || authStore.tenant?.name || '未分配' }}</dd></div>
        </dl>
      </div>
    </t-dialog>

    <t-dialog v-model:visible="passwordVisible" header="修改密码" :confirm-btn="{ content: '确认修改', loading: passwordSaving }" @confirm="savePassword">
      <t-form label-align="top">
        <t-form-item label="当前密码" required><t-input v-model="passwordForm.current" type="password" autocomplete="current-password" /></t-form-item>
        <t-form-item label="新密码" required><t-input v-model="passwordForm.next" type="password" autocomplete="new-password" /></t-form-item>
        <t-form-item label="确认新密码" required><t-input v-model="passwordForm.confirm" type="password" autocomplete="new-password" /></t-form-item>
      </t-form>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { MessagePlugin } from 'tdesign-vue-next'
import { changePassword, getCurrentUser, logout as logoutApi } from '@/api/auth'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const router = useRouter()
const uiStore = useUIStore()
const authStore = useAuthStore()

const menuRef = ref<HTMLElement>()
const menuVisible = ref(false)
const profileVisible = ref(false)
const passwordVisible = ref(false)
const passwordSaving = ref(false)
const passwordForm = ref({ current: '', next: '', confirm: '' })

const username = computed(() => authStore.user?.username || t('common.defaultUser'))
const userNickname = computed(() => authStore.user?.nickname || username.value)
const userAvatar = computed(() => authStore.user?.avatar || '')
const roleLabel = computed(() => {
  if (authStore.user?.role === 'platform_admin') return '平台管理员'
  if (authStore.user?.role === 'tenant_admin') return '租户管理员'
  return '普通用户'
})

// 昵称首字符（用于无头像时显示）
const userInitial = computed(() => {
  return userNickname.value.charAt(0).toUpperCase()
})

const refreshUserIdentity = async () => {
  if (!authStore.user) return
  const response = await getCurrentUser()
  if (!response.success || !response.data?.user) return
  const currentUser = response.data.user
  authStore.setUser({
    ...authStore.user,
    nickname: currentUser.nickname || currentUser.username,
    username: currentUser.username,
  })
}

// 切换菜单显示
const toggleMenu = () => {
  menuVisible.value = !menuVisible.value
}

// 注销
const handleLogout = async () => {
  menuVisible.value = false
  
  try {
    // 调用后端API注销
    await logoutApi()
  } catch (error) {
    // 即使API调用失败，也继续执行本地清理
    console.error('注销API调用失败:', error)
  }
  
  // 清理所有状态和本地存储
  authStore.logout()
  
  MessagePlugin.success(t('auth.logout'))
  
  // 跳转到登录页
  router.push('/login')
}

// 点击外部关闭菜单
const handleClickOutside = (e: MouseEvent) => {
  if (menuRef.value && !menuRef.value.contains(e.target as Node)) {
    menuVisible.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  void refreshUserIdentity()
})

const handleSettings = () => {
  menuVisible.value = false
  uiStore.openSettings()
  void router.push('/platform/settings')
}

const openPasswordDialog = () => {
  menuVisible.value = false
  passwordForm.value = { current: '', next: '', confirm: '' }
  passwordVisible.value = true
}

const savePassword = async () => {
  const { current, next, confirm } = passwordForm.value
  if (!current || next.length < 8 || next.length > 72) {
    MessagePlugin.warning('新密码长度应为 8 到 72 位')
    return
  }
  if (next !== confirm) {
    MessagePlugin.warning('两次输入的新密码不一致')
    return
  }
  if (current === next) {
    MessagePlugin.warning('新密码不能与当前密码相同')
    return
  }
  passwordSaving.value = true
  try {
    await changePassword(current, next)
    passwordVisible.value = false
    authStore.logout()
    MessagePlugin.success('密码已修改，请重新登录')
    await router.replace('/login')
  } catch (error) {
    MessagePlugin.error((error as { message?: string }).message || '当前密码错误或修改失败')
  } finally {
    passwordSaving.value = false
  }
}

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style lang="less" scoped>
.user-menu {
  position: relative;
  width: 100%;

  &--collapsed {
    .user-button {
      justify-content: center;
      padding: 8px;
      gap: 0;
    }

    .user-avatar {
      width: 32px;
      height: 32px;

      .avatar-placeholder {
        font-size: 13px;
      }
    }

    .user-dropdown {
      left: calc(100% + 8px);
      bottom: 0;
      right: auto;
      min-width: 200px;
    }
  }
}

.user-button {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
  background: transparent;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:active {
    transform: scale(0.98);
  }
}

.user-avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  overflow: hidden;
  flex-shrink: 0;
  background: linear-gradient(135deg, var(--td-brand-color) 0%, var(--td-brand-color-active) 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: width 0.2s ease, height 0.2s ease;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .avatar-placeholder {
    color: var(--td-text-color-anti);
    font-size: 16px;
    font-weight: 600;
  }
}

.user-info {
  flex: 1;
  min-width: 0;
  text-align: left;

  .user-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .user-username {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
}

.dropdown-icon {
  font-size: 16px;
  color: var(--td-text-color-secondary);
  flex-shrink: 0;
  transition: transform 0.2s;
}

.user-dropdown {
  position: absolute;
  bottom: 100%;
  left: 8px;
  right: 8px;
  margin-bottom: 8px;
  background: var(--td-bg-color-container);
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.12);
  border: 1px solid var(--td-component-stroke);
  overflow: hidden;
  z-index: 1000;
}

.menu-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 14px;
  color: var(--td-text-color-primary);

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &.danger {
    color: var(--td-error-color);

    &:hover {
      background: var(--td-error-color-light);
    }

    .menu-icon {
      color: var(--td-error-color);
    }
  }

  .menu-icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
    
    &.svg-icon {
      width: 16px;
      height: 16px;
      flex-shrink: 0;
    }

    &--emoji {
      width: 16px;
      height: 16px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      font-size: 15px;
      line-height: 1;
      flex-shrink: 0;
      color: inherit;
    }
  }

  .menu-text-with-icon {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    color: inherit;
    min-width: 0;

    > span:first-of-type {
      display: inline-flex;
      align-items: center;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .menu-new-badge {
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 600;
    line-height: 1.2;
    padding: 2px 5px;
    border-radius: 4px;
    background: var(--td-brand-color-light);
    color: var(--td-brand-color);
    letter-spacing: 0.02em;
  }

  .menu-github-star-icon {
    flex-shrink: 0;
    color: var(--td-warning-color);
  }

  .menu-external-icon {
    width: 14px;
    height: 14px;
    color: var(--td-text-color-disabled);
    flex-shrink: 0;
    transition: color 0.2s ease;
    pointer-events: none;
  }

  &:hover .menu-external-icon {
    color: var(--td-brand-color);
  }
}

.menu-divider {
  height: 1px;
  background: var(--td-component-stroke);
  margin: 4px 0;
}

.profile-card {
  padding: 8px 4px 4px;
  text-align: center;
}

.profile-avatar {
  width: 56px;
  height: 56px;
  margin: 0 auto 12px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(135deg, var(--td-brand-color), var(--td-brand-color-active));
  font-size: 20px;
  font-weight: 600;
}

.profile-name {
  color: var(--td-text-color-primary);
  font-size: 17px;
  font-weight: 600;
}

.profile-role {
  margin-top: 4px;
  color: var(--td-brand-color);
  font-size: 13px;
}

.profile-fields {
  margin: 20px 0 0;
  border-top: 1px solid var(--td-component-stroke);
  text-align: left;

  > div {
    display: grid;
    grid-template-columns: 88px 1fr;
    gap: 12px;
    padding: 12px 4px;
    border-bottom: 1px solid var(--td-component-stroke);
  }

  dt {
    color: var(--td-text-color-secondary);
  }

  dd {
    margin: 0;
    color: var(--td-text-color-primary);
    overflow-wrap: anywhere;
  }
}

// 下拉动画
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

.dropdown-enter-to,
.dropdown-leave-from {
  opacity: 1;
  transform: translateY(0);
}
</style>
