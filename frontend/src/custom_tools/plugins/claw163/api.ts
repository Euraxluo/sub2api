import { apiClient } from '@/api/client'

export const DEFAULT_CLAW163_AUTH_URL = 't1/qzFVTmZuR8c77xz4Cq86yFNaQew'

export interface Claw163Account {
  id: string
  name: string
  email: string
  transport: string
}

export interface Claw163Status {
  provider: 'claw163'
  cli_installed: boolean
  cli_path?: string
  cli_version?: string
  initialized: boolean
  ready: boolean
  phase: 'idle' | 'connected' | 'ready' | 'error'
  status_message: string
  profile?: string
  sender?: string
  accounts: Claw163Account[]
  recipients: string[]
  setup_auth_url?: string
  setup_command?: string
  error?: string
}

export async function getClaw163Status(): Promise<Claw163Status> {
  const { data } = await apiClient.get<Claw163Status>('/admin/claw163/status')
  return data
}

export async function initializeClaw163(authUrl: string, recipients: string[]): Promise<Claw163Status> {
  const { data } = await apiClient.post<Claw163Status>('/admin/claw163/initialize', {
    auth_url: authUrl,
    recipients
  }, { timeout: 95_000 })
  return data
}

export async function setClaw163Recipients(recipients: string[]): Promise<Claw163Status> {
  const { data } = await apiClient.put<Claw163Status>('/admin/claw163/target', { recipients })
  return data
}

export async function sendClaw163Test(subject: string, body: string): Promise<void> {
  await apiClient.post('/admin/claw163/send-test', { subject, body }, { timeout: 35_000 })
}
