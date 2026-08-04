<template>
  <div class="space-y-4">
    <div class="flex min-w-0 flex-col gap-3 border-b border-gray-100 pb-4 dark:border-dark-800 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Claw163 邮箱 CLI</h3>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <span :class="phaseClass">{{ phaseLabel }}</span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" title="刷新 Claw163 状态" @click="refreshStatus">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>
    </div>

    <p v-if="statusMessage" class="rounded border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">
      {{ statusMessage }}
    </p>
    <p v-if="error" class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </p>
    <p v-if="notice" class="rounded border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300">
      {{ notice }}
    </p>

    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <div class="space-y-4 rounded border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/40">
        <div>
          <label class="input-label">Claw163 初始化链接</label>
          <input v-model="authUrl" class="input font-mono text-xs" autocomplete="off" spellcheck="false" :disabled="initializing" />
        </div>

        <div>
          <label class="input-label">初始化命令</label>
          <code class="block break-all rounded border border-gray-200 bg-white p-2 text-xs text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200">{{ setupCommand }}</code>
        </div>

        <button type="button" class="btn btn-primary" :disabled="initializing || !authUrl.trim()" @click="initialize">
          <Icon name="play" size="sm" />
          {{ initializing ? '初始化中...' : '初始化 Claw163' }}
        </button>

        <div v-if="status?.initialized" class="rounded border border-gray-200 bg-white p-3 text-xs text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300">
          <p>发件邮箱：{{ status.sender || '-' }}</p>
          <p class="mt-1">官方 mail-cli：{{ status.cli_version || '已安装' }}</p>
        </div>
      </div>

      <div class="space-y-4 rounded border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/40">
        <div>
          <label class="input-label">通知收件人</label>
          <textarea v-model="recipientsText" class="input min-h-[92px] resize-y" placeholder="ops@example.com，每行或逗号分隔" :disabled="!status?.initialized"></textarea>
          <button type="button" class="btn btn-secondary mt-2" :disabled="savingTarget || !status?.initialized || recipients.length === 0" @click="saveRecipients">
            <Icon name="check" size="sm" />
            {{ savingTarget ? '保存中...' : '保存收件人' }}
          </button>
        </div>

        <div class="border-t border-gray-200 pt-4 dark:border-dark-700">
          <label class="input-label">测试主题</label>
          <input v-model="testSubject" class="input" :disabled="!status?.ready" />
          <label class="input-label mt-3">测试正文</label>
          <textarea v-model="testBody" class="input min-h-[92px] resize-y" :disabled="!status?.ready"></textarea>
          <button type="button" class="btn btn-primary mt-2" :disabled="sending || !status?.ready" @click="sendTest">
            <Icon name="mail" size="sm" />
            {{ sending ? '发送中...' : '发送测试邮件' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import {
  DEFAULT_CLAW163_AUTH_URL,
  getClaw163Status,
  initializeClaw163,
  sendClaw163Test,
  setClaw163Recipients,
  type Claw163Status
} from './api'

const status = ref<Claw163Status | null>(null)
const authUrl = ref(DEFAULT_CLAW163_AUTH_URL)
const recipientsText = ref('')
const testSubject = ref('Sub2API Claw163 通知测试')
const testBody = ref('如果你看到这封邮件，说明 Claw163 通知通道已连通。')
const loading = ref(false)
const initializing = ref(false)
const savingTarget = ref(false)
const sending = ref(false)
const error = ref('')
const notice = ref('')

const recipients = computed(() => [...new Set(recipientsText.value.split(/[\n,;]/).map((item) => item.trim()).filter(Boolean))])
const setupCommand = computed(() => status.value?.setup_command || `npx "@clawemail/claw-setup@latest" --auth-url "${authUrl.value.trim()}"`)
const statusMessage = computed(() => status.value?.status_message || '正在读取 Claw163 状态...')
const phaseLabel = computed(() => {
  if (!status.value?.cli_installed) return 'CLI 未安装'
  if (status.value.ready) return '已就绪'
  if (status.value.initialized) return '已初始化'
  return '待初始化'
})
const phaseClass = computed(() => {
  if (status.value?.ready) return 'rounded-full bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
  if (status.value?.phase === 'error' || status.value?.error) return 'rounded-full bg-red-50 px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300'
})

function applyStatus(next: Claw163Status) {
  status.value = next
  if (next.setup_auth_url) authUrl.value = next.setup_auth_url
  recipientsText.value = (next.recipients || []).join('\n')
  error.value = next.error || ''
}

async function refreshStatus() {
  loading.value = true
  error.value = ''
  try {
    applyStatus(await getClaw163Status())
  } catch (cause) {
    error.value = errorMessage(cause, '无法读取 Claw163 状态。')
  } finally {
    loading.value = false
  }
}

async function initialize() {
  initializing.value = true
  error.value = ''
  notice.value = ''
  try {
    applyStatus(await initializeClaw163(authUrl.value.trim(), recipients.value))
    notice.value = 'Claw163 邮箱初始化完成。'
  } catch (cause) {
    error.value = errorMessage(cause, 'Claw163 初始化失败，请检查授权链接和 mail-cli。')
  } finally {
    initializing.value = false
  }
}

async function saveRecipients() {
  savingTarget.value = true
  error.value = ''
  notice.value = ''
  try {
    applyStatus(await setClaw163Recipients(recipients.value))
    notice.value = 'Claw163 通知收件人已保存。'
  } catch (cause) {
    error.value = errorMessage(cause, '保存 Claw163 收件人失败。')
  } finally {
    savingTarget.value = false
  }
}

async function sendTest() {
  sending.value = true
  error.value = ''
  notice.value = ''
  try {
    await sendClaw163Test(testSubject.value, testBody.value)
    notice.value = '测试邮件已发送，后续上游告警会发送到这些收件人。'
  } catch (cause) {
    error.value = errorMessage(cause, 'Claw163 测试邮件发送失败。')
  } finally {
    sending.value = false
  }
}

function errorMessage(cause: unknown, fallback: string): string {
  if (cause && typeof cause === 'object') {
    const message = (cause as { message?: unknown }).message
    if (typeof message === 'string' && message.trim()) return message
  }
  return fallback
}

onMounted(() => void refreshStatus())
</script>
