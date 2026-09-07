import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function source(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('platform retrieval batch budgets', () => {
  it('loads, edits, and saves the per-query and batch-wide limits', () => {
    const settings = source('../src/views/settings/RetrievalSettings.vue')
    const api = source('../src/api/retrieval.ts')

    for (const field of ['batch_rerank_top_k', 'batch_max_results', 'batch_max_content_chars']) {
      expect(api).toContain(`${field}: number`)
      expect(settings).toContain(`v-model="localConfig.${field}"`)
      expect(settings).toContain(`${field}: cfg.${field} ?? defaultConfig.${field}`)
    }
    expect(settings).toContain('batch_rerank_top_k: 5')
    expect(settings).toContain('batch_max_results: 200')
    expect(settings).toContain('batch_max_content_chars: 200000')

    const batchSection = settings.indexOf("retrievalSettings.batchProtectionTitle")
    const batchRerankInput = settings.indexOf('v-model="localConfig.batch_rerank_top_k"')
    const batchMaxResultsInput = settings.indexOf('v-model="localConfig.batch_max_results"')
    expect(batchRerankInput).toBeGreaterThan(batchSection)
    expect(batchRerankInput).toBeLessThan(batchMaxResultsInput)
  })
})
