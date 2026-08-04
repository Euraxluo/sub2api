import { apiClient } from '@/api/client'

export interface BillingAuditCandidate {
  model: string
  pricing_source?: string
  available: boolean
  error?: string
  billing_mode?: string
  input_cost: number
  image_input_cost: number
  output_cost: number
  image_output_cost: number
  cache_write_cost: number
  cache_read_cost: number
  total_cost: number
}

export interface BillingAuditRecord {
  request_id?: string
  account_id: number
  requested_model: string
  mapping_chain?: string
  strategy: string
  selected_model: string
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  image_input_tokens: number
  image_output_tokens: number
  image_count: number
  group_rate_multiplier: number
  account_rate_multiplier: number
  total_cost: number
  actual_cost: number
  account_stats_cost?: number
  account_billed_cost: number
  candidates: BillingAuditCandidate[]
  created_at: string
}

export async function getBillingAudits(accountID?: number, limit = 100): Promise<BillingAuditRecord[]> {
  const { data } = await apiClient.get<BillingAuditRecord[]>('/admin/billing-strategy/audits', {
    params: {
      account_id: accountID || undefined,
      limit
    }
  })
  return data || []
}
