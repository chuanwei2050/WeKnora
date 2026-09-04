import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const view = readFileSync(
  resolve(__dirname, '../src/views/knowledge/KnowledgeBase.vue'),
  'utf8',
)

describe('batch duplicate upload dialog', () => {
  it('collects duplicate files from multi-file and folder uploads', () => {
    expect(view.match(/duplicates\.push\(/g)).toHaveLength(3)
    expect(view).toContain('showBatchUploadResult(successCount, failCount, duplicateCount, deletingCount, duplicates)')
  })

  it('shows one list with a locate action for each duplicate', () => {
    expect(view).toContain('v-model:visible="batchDuplicateDialogVisible"')
    expect(view).toContain('v-for="item in batchDuplicates"')
    expect(view).toContain('@click="locateDuplicateKnowledge(item.knowledge)"')
    expect(view).toContain("$t('knowledgeBase.uploadBatchSummary', batchUploadSummary)")
  })
})
