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
