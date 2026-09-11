<template>
  <div class="workbench-container">
    <div class="workbench-content">
      <div class="header" style="--wails-draggable: drag">
        <div class="header-title">
          <h2>{{ t('workbench.title') }}</h2>
        </div>
      </div>

      <div class="workbench-tabs">
        <div
          v-for="tab in visibleTabs"
          :key="tab.key"
          :class="['workbench-tab', { active: activeTab === tab.key }]"
          @click="switchTab(tab.key)"
        >
          <t-icon :name="tab.icon" size="16px" />
          {{ tab.label }}
        </div>
      </div>

      <div class="workbench-panel">
        <FeedbackReview v-if="activeTab === 'feedback'" hide-header />
        <GraphTripleReview
          v-else-if="activeTab === 'graph-triples'"
          hide-header
          :allowed-knowledge-base-ids="reviewEnabledKbIds"
        />
        <AcceptanceReview v-else-if="activeTab === 'acceptance'" hide-header />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listKnowledgeBases } from '@/api/knowledge-base'
import FeedbackReview from '@/views/settings/FeedbackReview.vue'
import GraphTripleReview from '@/views/settings/GraphTripleReview.vue'
import AcceptanceReview from '@/views/settings/AcceptanceReview.vue'

type WorkbenchTab = 'feedback' | 'graph-triples' | 'acceptance'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const reviewEnabledKbIds = ref<string[]>([])
const kbReady = ref(false)
const activeTab = ref<WorkbenchTab>('feedback')

const allTabs = computed(() => [
  { key: 'feedback' as const, icon: 'check-circle', label: t('workbench.tabs.feedback') },
  { key: 'graph-triples' as const, icon: 'relation', label: t('workbench.tabs.graphTriples') },
  { key: 'acceptance' as const, icon: 'chart-line', label: t('workbench.tabs.acceptance') },
])

const visibleTabs = computed(() =>
  allTabs.value.filter((tab) => {
    if (tab.key === 'graph-triples') return reviewEnabledKbIds.value.length > 0
    return true
  }),
)

const parseTab = (value: unknown): WorkbenchTab | null => {
  if (value === 'feedback' || value === 'graph-triples' || value === 'acceptance') return value
  return null
}

const switchTab = (tab: WorkbenchTab) => {
  if (tab === 'graph-triples' && reviewEnabledKbIds.value.length === 0) return
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
}

const syncTabFromRoute = () => {
  const requested = parseTab(route.query.tab)
  const available = new Set(visibleTabs.value.map((tab) => tab.key))
  if (requested && available.has(requested)) {
    activeTab.value = requested
    return
  }
  const fallback = visibleTabs.value[0]?.key || 'feedback'
  activeTab.value = fallback
  if (requested !== fallback) {
    router.replace({ query: { ...route.query, tab: fallback } })
  }
}

const loadReviewEnabledKbs = async () => {
  try {
    const res: any = await listKnowledgeBases()
    const list = res?.data ?? res ?? []
    reviewEnabledKbIds.value = (Array.isArray(list) ? list : [])
      .filter((kb: any) => !!kb?.extract_config?.require_triple_review)
      .map((kb: any) => String(kb.id))
      .filter(Boolean)
  } catch {
    reviewEnabledKbIds.value = []
  } finally {
    kbReady.value = true
    syncTabFromRoute()
  }
}

watch(() => route.query.tab, () => {
  if (kbReady.value) syncTabFromRoute()
})

watch(visibleTabs, () => {
  if (kbReady.value) syncTabFromRoute()
})

onMounted(loadReviewEnabledKbs)
</script>

<style scoped lang="less">
.workbench-container {
  margin: 0 16px 0 0;
  height: calc(100vh);
  box-sizing: border-box;
  flex: 1;
  display: flex;
  position: relative;
  min-height: 0;
}

.workbench-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 24px 32px 24px 32px;
}

.header {
  margin-bottom: 16px;

  .header-title {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  h2 {
    margin: 0;
    color: var(--td-text-color-primary);
    font-family: "PingFang SC", -apple-system, sans-serif;
    font-size: 24px;
    font-weight: 600;
    line-height: 32px;
  }
}

.workbench-tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 16px;
  padding: 3px;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 8px;
  width: fit-content;
  flex-shrink: 0;
}

.workbench-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  transition: all 0.15s ease;
  user-select: none;

  &:hover {
    color: var(--td-text-color-primary);
  }

  &.active {
    background: var(--td-bg-color-container);
    color: var(--td-brand-color);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
  }
}

.workbench-panel {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
</style>
