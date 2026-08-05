import { apiClient } from '@/api/client'

export type AdminJobKind = 'builtin' | 'javascript'
export type AdminJobNotifyPolicy = 'never' | 'failure' | 'failure_recovery' | 'always'
export type AdminJobStatus = 'ok' | 'warning' | 'critical' | 'error' | ''

export interface AdminJobFieldDefinition {
  key: string
  label: string
  type: 'text' | 'url' | 'number' | 'password' | string
  required?: boolean
  secret?: boolean
  default?: unknown
  placeholder?: string
  help?: string
}

export interface AdminJobBuiltinDefinition {
  id: string
  name: string
  description: string
  fields?: AdminJobFieldDefinition[]
}

export interface AdminJobTask {
  id: string
  name: string
  description?: string
  kind: AdminJobKind
  builtin_id?: string
  script?: string
  input?: Record<string, unknown>
  secret_keys?: string[]
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  notify_policy: AdminJobNotifyPolicy
  notify_manual: boolean
  cooldown_seconds: number
  running: boolean
  last_status?: AdminJobStatus
  last_message?: string
  last_run_at?: string
  next_run_at?: string
  last_notification_at?: string
  created_at: string
  updated_at: string
}

export interface AdminJobDraft {
  name: string
  description?: string
  kind: AdminJobKind
  builtin_id?: string
  script?: string
  input?: Record<string, unknown>
  secrets?: Record<string, string>
  clear_secret_keys?: string[]
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  notify_policy: AdminJobNotifyPolicy
  notify_manual: boolean
  cooldown_seconds: number
}

export interface AdminJobRun {
  id: string
  task_id: string
  task_name: string
  trigger: 'manual' | 'scheduled' | string
  status: AdminJobStatus
  title?: string
  message?: string
  data?: Record<string, unknown>
  stdout?: string
  stderr?: string
  error?: string
  started_at: string
  finished_at: string
  duration_ms: number
  notified: boolean
  notify_error?: string
}

export async function listAdminJobBuiltins(): Promise<AdminJobBuiltinDefinition[]> {
  const { data } = await apiClient.get<AdminJobBuiltinDefinition[]>('/admin/admin-jobs/builtins')
  return data
}

export async function listAdminJobs(): Promise<AdminJobTask[]> {
  const { data } = await apiClient.get<AdminJobTask[]>('/admin/admin-jobs/tasks')
  return data
}

export async function createAdminJob(draft: AdminJobDraft): Promise<AdminJobTask> {
  const { data } = await apiClient.post<AdminJobTask>('/admin/admin-jobs/tasks', draft)
  return data
}

export async function updateAdminJob(id: string, draft: AdminJobDraft): Promise<AdminJobTask> {
  const { data } = await apiClient.put<AdminJobTask>(`/admin/admin-jobs/tasks/${encodeURIComponent(id)}`, draft)
  return data
}

export async function deleteAdminJob(id: string): Promise<void> {
  await apiClient.delete(`/admin/admin-jobs/tasks/${encodeURIComponent(id)}`)
}

export async function runAdminJob(id: string, notify = false): Promise<AdminJobRun> {
  const { data } = await apiClient.post<AdminJobRun>(`/admin/admin-jobs/tasks/${encodeURIComponent(id)}/run`, { notify })
  return data
}

export async function listAdminJobRuns(taskId = '', limit = 50): Promise<AdminJobRun[]> {
  const { data } = await apiClient.get<AdminJobRun[]>('/admin/admin-jobs/runs', {
    params: { task_id: taskId || undefined, limit }
  })
  return data
}
