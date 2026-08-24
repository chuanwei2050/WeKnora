import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function source(relativePath: string) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('embedded widget capabilities', () => {
  it('uses tenant-wide frequent questions without enabling the legacy suggestion endpoint', () => {
    const chatView = source('../src/views/chat/index.vue')
    const embeddedWidget = source('../src/views/embedded/EmbeddedWidget.vue')

    expect(chatView).toContain('suggestedQuestionsEnabled: { type: Boolean, default: true }')
    expect(chatView).toContain('if (!props.suggestedQuestionsEnabled)')
    expect(embeddedWidget).toContain(':suggestedQuestionsEnabled="false"')
    expect(embeddedWidget).toContain('listIntegrationFrequentQuestions')
    expect(embeddedWidget).toContain(':embeddedSuggestedQuestions="frequentQuestions"')
    expect(embeddedWidget).toContain('questions.slice(0, 3)')
    expect(embeddedWidget).toContain('frequentQuestions.value = []')
  })

  it('drops stale authentication before requesting a new host ticket', () => {
    const embeddedWidget = source('../src/views/embedded/EmbeddedWidget.vue')

    expect(embeddedWidget).toContain('function expireAuthentication()')
    expect(embeddedWidget).toContain('authenticated.value = false')
    expect(embeddedWidget).toContain('clearEmbeddedAuth()')
    expect(embeddedWidget).toContain("notifyEmbeddedHost('unauthorized')")
  })

  it('reuses the latest compatible empty conversation during initialization', () => {
    const embeddedWidget = source('../src/views/embedded/EmbeddedWidget.vue')

    expect(embeddedWidget).toContain('await refreshConversations()')
    expect(embeddedWidget).toContain('compatibleConversations.value.find((conversation) => !conversation.title.trim())')
    expect(embeddedWidget).toContain('sessionStorage.setItem(sessionStorageKey, chatSession.id)')
  })

  it('prevents duplicate submissions before the replying prop updates', () => {
    const inputField = source('../src/components/Input-field.vue')

    expect(inputField).toContain('let submissionPending = false;')
    expect(inputField).toContain('if (props.isReplying || submissionPending)')
    expect(inputField).toContain('submissionPending = true;')
    expect(inputField).toContain('if (!isReplying) submissionPending = false;')
  })

  it('uses the configured agent and lets the backend select its execution mode', () => {
    const embeddedWidget = source('../src/views/embedded/EmbeddedWidget.vue')
    const chatView = source('../src/views/chat/index.vue')

    expect(embeddedWidget).toContain("params.get('agent_id')")
    expect(embeddedWidget).toContain(':agentId="widgetAgentId"')
    expect(chatView).toContain('props.embeddedMode ? true : useSettingsStoreInstance.isAgentEnabled')
    expect(chatView).toContain('props.embeddedMode ? props.agentId :')
  })
})
