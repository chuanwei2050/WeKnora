import { get } from '@/utils/request'

export type ApprovedEndpointCategory =
  | 'model'
  | 'search'
  | 'data-connector'
  | 'object-storage'
  | 'telemetry'

export interface ApprovedEndpoint {
  id: string
  scheme: string
  host: string
  port: number
  protocol?: string
  tls_required?: boolean
  category: ApprovedEndpointCategory
  allowed_uses: string[]
}

export function listApprovedEndpoints(category?: ApprovedEndpointCategory): Promise<{ data: ApprovedEndpoint[] }> {
  const suffix = category ? `?category=${encodeURIComponent(category)}` : ''
  return get(`/api/v1/approved-endpoints${suffix}`)
}
