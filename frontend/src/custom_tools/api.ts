import { apiClient } from '@/api/client'
import type { AdminDataPayload } from '@/types'

export interface ClashBatchProxy {
  protocol: string
  host: string
  port: number
  username?: string
  password?: string
}

export interface ClashProxyPreviewRow {
  index: number
  name: string
  duplicate: boolean
  valid: boolean
  errors: string[]
  proxy: {
    protocol?: string
    host?: string
    port?: number
    username?: string
    status?: string
    name?: string
  }
}

export interface ClashProxyPreviewSummary {
  total: number
  valid: number
  invalid: number
  duplicates: number
}

export interface ClashBatchPayload {
  proxies: ClashBatchProxy[]
}

export interface ClashProxyPreviewResponse {
  source: string
  rows: ClashProxyPreviewRow[]
  summary: ClashProxyPreviewSummary
  data_payload: AdminDataPayload
  batch_payload: ClashBatchPayload
}

export async function previewClashImport(payload: { url?: string; content?: string }): Promise<ClashProxyPreviewResponse> {
  const { data } = await apiClient.post<ClashProxyPreviewResponse>('/admin/proxies/import/clash', payload)
  return data
}

export interface CodexQuotaGuardConfig {
  enabled: boolean
  reserve_percent: number
  windows: string[]
  interval_seconds: number
  account_types: string[]
  dry_run: boolean
}

export interface CodexQuotaGuardStatus {
  running: boolean
  config: CodexQuotaGuardConfig
  admin_api_key_source: 'provided' | 'auto_created' | 'missing' | string
  admin_api_key_cached: boolean
  last_run_at?: string
  last_error?: string
  last_blocked_count: number
  last_blocked_ids?: number[]
  last_released_count: number
  last_released_ids?: number[]
  last_scan_candidates: number
  last_scanned_accounts: number
  current_managed_count: number
  current_managed_ids?: number[]
}

export interface CodexQuotaGuardScanResponse {
  blocked_count: number
  blocked_ids?: number[]
  released_count: number
  released_ids?: number[]
  candidate_count: number
  scanned_accounts: number
  dry_run: boolean
  errors?: string[]
  stopped_on_auth_error: boolean
}

export interface CodexQuotaGuardStartPayload {
  enabled: boolean
  reserve_percent: number
  windows: string[]
  interval_seconds: number
  account_types: string[]
  dry_run: boolean
}

export async function startCodexQuotaGuard(
  payload: CodexQuotaGuardStartPayload,
  adminApiKey?: string
): Promise<CodexQuotaGuardStatus> {
  const headers = adminApiKey?.trim() ? { 'x-api-key': adminApiKey.trim() } : undefined
  const { data } = await apiClient.post<CodexQuotaGuardStatus>('/admin/codex-quota-guard/start', payload, { headers })
  return data
}

export async function stopCodexQuotaGuard(): Promise<CodexQuotaGuardStatus> {
  const { data } = await apiClient.post<CodexQuotaGuardStatus>('/admin/codex-quota-guard/stop', {})
  return data
}

export async function getCodexQuotaGuardStatus(): Promise<CodexQuotaGuardStatus> {
  const { data } = await apiClient.get<CodexQuotaGuardStatus>('/admin/codex-quota-guard/status')
  return data
}

export async function scanCodexQuotaGuard(): Promise<CodexQuotaGuardScanResponse> {
  const { data } = await apiClient.post<CodexQuotaGuardScanResponse>('/admin/codex-quota-guard/scan', {})
  return data
}

export async function releaseCodexQuotaGuard(): Promise<CodexQuotaGuardScanResponse> {
  const { data } = await apiClient.post<CodexQuotaGuardScanResponse>('/admin/codex-quota-guard/release', {})
  return data
}
