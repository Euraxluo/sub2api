import { apiClient } from '@/api/client'

export interface ModelReasoningMapping {
  from_model: string
  from_effort?: string
  to_model?: string
  to_effort: string
}

export interface ModelReasoningAccountConfig {
  mappings: ModelReasoningMapping[]
}

export async function getModelReasoningConfig(accountID: number): Promise<ModelReasoningAccountConfig> {
  const { data } = await apiClient.get<ModelReasoningAccountConfig>(`/admin/model-reasoning-effort/config/${accountID}`)
  return data
}

export async function updateModelReasoningConfig(accountID: number, config: ModelReasoningAccountConfig): Promise<ModelReasoningAccountConfig> {
  const { data } = await apiClient.put<ModelReasoningAccountConfig>(`/admin/model-reasoning-effort/config/${accountID}`, config)
  return data
}

export interface ModelReasoningBatchConfigResponse {
  updated_account_ids: number[]
  config: ModelReasoningAccountConfig
}

export async function updateModelReasoningConfigBatch(
  accountIDs: number[],
  config: ModelReasoningAccountConfig
): Promise<ModelReasoningBatchConfigResponse> {
  const { data } = await apiClient.put<ModelReasoningBatchConfigResponse>('/admin/model-reasoning-effort/config/batch', {
    account_ids: accountIDs,
    config
  })
  return data
}

export interface ModelReasoningAutoConfig {
  enabled: boolean
  account_ids: number[]
  cron_schedules: string[]
  refresh_times?: string[]
  schedule_timezone: string
  radar_base_url?: string
  refresh_interval_minutes?: number
  timeout_seconds?: number
  iq_aggregation?: string
  iq_window_hours?: number
  formula_mode?: 'natural_gap' | 'iq_cost'
  baseline_model?: string
  baseline_gap?: number
}

export interface ModelReasoningMetric {
  model: string
  effort: string
  iq: number
  cost_usd?: number
  has_cost: boolean
  iq_samples?: number
  cost_samples?: number
}

export interface ModelReasoningAutoStatus {
  plan?: {
    generated_at?: string
    baseline?: ModelReasoningMetric
    baseline_limit?: number
    max_iq?: number
    bands?: Array<{ name: string; min_iq: number; max_iq: number; target_model?: string; target_effort?: string }>
    mappings?: ModelReasoningMapping[]
  }
  metrics?: ModelReasoningMetric[]
  last_refresh_at?: string
  last_error?: string
}

export interface ModelReasoningAutoResponse {
  config?: Partial<ModelReasoningAutoConfig> | null
  status?: ModelReasoningAutoStatus | null
  mappings?: ModelReasoningMapping[] | null
}

export interface ModelReasoningUsageStat {
  account_id: number
  from_model: string
  from_effort: string
  to_model: string
  to_effort: string
  requests: number
  input_tokens: number
  output_tokens: number
  last_seen_at: string
}

export async function getModelReasoningAuto(): Promise<ModelReasoningAutoResponse> {
  const { data } = await apiClient.get<ModelReasoningAutoResponse>('/admin/model-reasoning-effort/auto')
  return data
}

export async function updateModelReasoningAuto(config: ModelReasoningAutoConfig): Promise<ModelReasoningAutoConfig> {
  const { data } = await apiClient.put<ModelReasoningAutoConfig>('/admin/model-reasoning-effort/auto', config)
  return data
}

export async function refreshModelReasoningAuto(): Promise<ModelReasoningAutoStatus> {
  const { data } = await apiClient.post<ModelReasoningAutoStatus>('/admin/model-reasoning-effort/auto/refresh')
  return data
}

export async function getModelReasoningStats(): Promise<ModelReasoningUsageStat[]> {
  const { data } = await apiClient.get<ModelReasoningUsageStat[]>('/admin/model-reasoning-effort/stats')
  return data
}
