<template>
  <div class="general-settings">
    <div class="section-header">
      <h2>{{ $t('general.title') }}</h2>
      <p class="section-description">{{ $t('general.description') }}</p>
    </div>

    <div class="settings-group">
      <!-- 主题设置 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('theme.theme') }}</label>
          <p class="desc">{{ $t('theme.themeDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-select
            v-model="localTheme"
            style="width: 280px;"
            :placeholder="$t('theme.selectTheme')"
            @change="handleThemeChange"
          >
            <t-option value="light" :label="$t('theme.light')">{{ $t('theme.light') }}</t-option>
            <t-option value="dark" :label="$t('theme.dark')">{{ $t('theme.dark') }}</t-option>
            <t-option value="system" :label="$t('theme.system')">{{ $t('theme.system') }}</t-option>
          </t-select>
        </div>
      </div>

      <!-- 自动下载更新开关 (Lite edition only) -->
      <div class="setting-row" v-if="authStore.isLiteMode">
        <div class="setting-info">
          <label>{{ $t('settings.autoCheckUpdate') }}</label>
          <p class="desc">{{ $t('settings.autoCheckUpdateDesc') }}</p>
        </div>
        <div class="setting-control">
          <t-switch
            v-model="isAutoCheckUpdateEnabled"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useAuthStore } from '@/stores/auth'
import { useTheme, type ThemeMode } from '@/composables/useTheme'

const { t } = useI18n()
const settingsStore = useSettingsStore()
const authStore = useAuthStore()
const { currentTheme, setTheme } = useTheme()

// 本地状态
const localTheme = ref<ThemeMode>(currentTheme.value)

// 自动检查更新状态
const isAutoCheckUpdateEnabled = computed({
  get: () => settingsStore.isAutoCheckUpdateEnabled,
  set: (val) => {
    settingsStore.toggleAutoCheckUpdate(val)
    if (val) {
      // @ts-ignore
      if (window.go && window.go.main && window.go.main.App && window.go.main.App.AutoCheckForUpdates) {
        // @ts-ignore
        window.go.main.App.AutoCheckForUpdates()
      }
    }
  }
})

// 处理主题变化
const handleThemeChange = (val: ThemeMode) => {
  setTheme(val)
  MessagePlugin.success(t('common.success'))
}
</script>

<style lang="less" scoped>
.general-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 32px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 8px 0;
  }

  .section-description {
    font-size: 14px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 20px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }
}

.setting-info {
  flex: 1;
  max-width: 65%;
  padding-right: 24px;

  label {
    font-size: 15px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    display: block;
    margin-bottom: 4px;
  }

  .desc {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.setting-control {
  flex-shrink: 0;
  min-width: 280px;
  display: flex;
  justify-content: flex-end;
  align-items: center;
}
</style>
