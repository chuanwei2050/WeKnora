import { get, post } from '../../utils/request'

export type AcceptanceGate = 'pending' | 'incomplete' | 'passed' | 'failed'
export type AcceptanceProfile = 'single-node' | 'server-load'

export interface AcceptanceSuiteVersion {
  id: string
  suite_id: string
  version: string
  kind: 'research_acceptance' | 'regular'
  routing_taxonomy_id: string
  routing_taxonomy_version: string
  frozen: boolean
  cases: unknown[]
}

export interface AcceptanceRun {
  id: string
  suite_version_id: string
  profile: AcceptanceProfile
  gate: AcceptanceGate
  snapshot: Record<string, unknown>
  metrics: Record<string, unknown>
  created_at?: string
}

export interface AcceptanceArtifact {
  id: string
  run_id: string
  kind?: 'source_code' | 'config' | 'report' | 'screenshot' | 'manual'
  uri: string
  sha256: string
  size: number
  content_type?: string
}

export interface AcceptanceMaterialChecklistItem {
  kind: 'source_code' | 'config' | 'report' | 'screenshot' | 'manual'
  required: boolean
  present: boolean
  uri?: string
  reason?: string
}

export function listAcceptanceSuites(suiteId?: string) {
  const query = suiteId ? `?suite_id=${encodeURIComponent(suiteId)}` : ''
  return get<{ data: AcceptanceSuiteVersion[] }>(`/api/v1/acceptance/suites${query}`)
}

export function getAcceptanceSuite(id: string) {
  return get<{ data: AcceptanceSuiteVersion }>(`/api/v1/acceptance/suites/${id}`)
}

export function freezeAcceptanceSuite(id: string) {
  return post<{ data: AcceptanceSuiteVersion }>(`/api/v1/acceptance/suites/${id}/freeze`, {})
}

export function createAcceptanceRun(payload: Partial<AcceptanceRun>) {
  return post<{ data: AcceptanceRun }>('/api/v1/acceptance/runs', payload)
}

export function getAcceptanceRun(id: string) {
  return get<{ data: AcceptanceRun }>(`/api/v1/acceptance/runs/${id}`)
}

export function listAcceptanceRuns(suiteVersionId?: string) {
  const query = suiteVersionId ? `?suite_version_id=${encodeURIComponent(suiteVersionId)}` : ''
  return get<{ data: AcceptanceRun[] }>(`/api/v1/acceptance/runs${query}`)
}

export function listAcceptanceCaseResults(runId: string) {
  return get<{ data: Array<{ id: string; run_id: string; case_id: string; payload: Record<string, unknown> }> }>(`/api/v1/acceptance/runs/${runId}/results`)
}

export function reviewAcceptanceCase(runId: string, caseId: string, passed: boolean) {
  return post(`/api/v1/acceptance/runs/${runId}/cases/${caseId}/review`, { passed })
}

export function submitAcceptanceCaseResult(runId: string, caseId: string, payload: Record<string, unknown>) {
  return post(`/api/v1/acceptance/runs/${runId}/cases/${caseId}`, payload)
}

export function listAcceptanceArtifacts(runId: string) {
  return get<{ data: AcceptanceArtifact[] }>(`/api/v1/acceptance/runs/${runId}/artifacts`)
}

export function listAcceptanceMaterials(runId: string) {
  return get<{ data: AcceptanceMaterialChecklistItem[] }>(`/api/v1/acceptance/runs/${runId}/materials`)
}

export function registerAcceptanceArtifact(runId: string, artifact: Omit<AcceptanceArtifact, 'id' | 'run_id'>) {
  return post<{ data: AcceptanceArtifact }>(`/api/v1/acceptance/runs/${runId}/artifacts`, artifact)
}
