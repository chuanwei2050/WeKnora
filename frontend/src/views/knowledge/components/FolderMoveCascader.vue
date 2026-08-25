<script setup lang="ts">
import { ref } from 'vue'
import type { CascaderValue } from 'tdesign-vue-next'
import type { FolderCascaderOption } from './document-folder-organization'

const props = withDefaults(defineProps<{
  options: FolderCascaderOption[]
  placement?: 'top-right' | 'bottom-right'
}>(), {
  placement: 'bottom-right',
})

const emit = defineEmits<{
  (event: 'select', folderId: string): void
  (event: 'visible-change', visible: boolean): void
}>()

const visible = ref(false)

const handleVisibleChange = (nextVisible: boolean) => {
  emit('visible-change', nextVisible)
}

const findOption = (
  options: FolderCascaderOption[],
  value: string,
): FolderCascaderOption | undefined => {
  for (const option of options) {
    if (option.value === value) return option
    const child = option.children && findOption(option.children, value)
    if (child) return child
  }
  return undefined
}

const handleChange = (value: CascaderValue) => {
  if (typeof value !== 'string' && typeof value !== 'number') return
  const option = findOption(props.options, String(value))
  if (!option?.selectable) return
  visible.value = false
  emit('select', option.value)
}
</script>

<template>
  <t-popup
    v-model="visible"
    trigger="click"
    :placement="placement"
    destroy-on-close
    overlay-class-name="folder-move-cascader-popup"
    :overlay-inner-style="{ padding: 0 }"
    :on-visible-change="handleVisibleChange"
  >
    <slot />
    <template #content>
      <t-cascader-panel
        :options="options"
        check-strictly
        trigger="hover"
        @change="handleChange"
      />
    </template>
  </t-popup>
</template>

<style lang="less">
.folder-move-cascader-popup {
  .t-cascader__panel {
    flex-direction: row-reverse;
    max-width: min(640px, calc(100vw - 32px));
  }

  .t-cascader__menu {
    min-width: 180px;
    max-width: 280px;
  }
}
</style>
