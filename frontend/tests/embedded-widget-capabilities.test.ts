import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function source(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('embedded widget capabilities', () => {
  it('disables legacy suggested-question requests without changing the standalone default', () => {
    const chatView = source('../src/views/chat/index.vue')
    const embeddedWidget = source('../src/views/embedded/EmbeddedWidget.vue')

    expect(chatView).toContain('suggestedQuestionsEnabled: { type: Boolean, default: true }')
    expect(chatView).toContain('if (!props.suggestedQuestionsEnabled)')
    expect(embeddedWidget).toContain(':suggestedQuestionsEnabled="false"')
  })
})
