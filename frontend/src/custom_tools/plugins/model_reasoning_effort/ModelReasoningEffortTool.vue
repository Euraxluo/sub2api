<template>
  <section class="tools-surface">
    <div class="tools-section-header">
      <div>
        <h2 class="tools-title">模型映射与推理强度</h2>
        <p class="tools-description">按账号配置模型的推理强度，规则由插件保存并在统一上游请求入口执行。</p>
      </div>
      <button type="button" class="btn btn-secondary" :disabled="reasoningLoading" @click="loadReasoningAccounts">
        <Icon name="refresh" size="sm" :class="reasoningLoading ? 'animate-spin' : ''" />
        刷新账号
      </button>
    </div>

    <div class="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
      <div class="tools-panel">
        <label class="input-label">OpenAI 账号</label>
        <select v-model="selectedReasoningAccountId" class="input" :disabled="reasoningLoading">
          <option :value="null">请选择账号</option>
          <option v-for="account in reasoningAccounts" :key="account.id" :value="account.id">
            {{ account.name }} · {{ account.type }}
          </option>
        </select>
        <p v-if="reasoningAccounts.length === 0 && !reasoningLoading" class="input-hint">没有找到 OpenAI 账号。</p>
        <div v-if="reasoningAccounts.length" class="mt-4 border-t border-gray-200 pt-4 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label class="input-label mb-0">批量应用当前映射</label>
            <span class="text-xs text-gray-500 dark:text-dark-400">已选 {{ selectedReasoningBatchAccountIds.length }} 个</span>
          </div>
          <details class="account-multi-select mt-2">
            <summary class="account-multi-trigger">
              <span class="min-w-0 truncate">{{ reasoningAccountSelectionText(selectedReasoningBatchAccountIds, '请选择账号') }}</span>
              <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400" />
            </summary>
            <div class="account-multi-menu">
              <input
                v-model="reasoningBatchAccountQuery"
                class="input"
                type="search"
                placeholder="搜索账号名称、平台、类型或 ID"
              />
              <div class="mt-2 flex flex-wrap gap-2">
                <button type="button" class="btn btn-secondary btn-sm" :disabled="filteredReasoningAccounts(reasoningBatchAccountQuery).length === 0" @click="selectFilteredReasoningBatchAccounts">
                  选择当前结果
                </button>
                <button type="button" class="btn btn-secondary btn-sm" @click="selectAllReasoningBatchAccounts">全选账号</button>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="selectedReasoningBatchAccountIds.length === 0" @click="selectedReasoningBatchAccountIds = []">清空</button>
              </div>
              <div v-if="selectedReasoningBatchAccountIds.length" class="mt-2 flex max-h-20 flex-wrap gap-1.5 overflow-y-auto">
                <button
                  v-for="accountId in selectedReasoningBatchAccountIds"
                  :key="accountId"
                  type="button"
                  class="inline-flex min-w-0 items-center gap-1 rounded-full border border-primary-200 bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:border-primary-900/70 dark:bg-primary-950/40 dark:text-primary-300"
                  :title="reasoningAccountLabel(accountId)"
                  @click="removeReasoningBatchAccount(accountId)"
                >
                  <span class="max-w-[180px] truncate">{{ reasoningAccountLabel(accountId) }}</span>
                  <Icon name="x" size="xs" />
                </button>
              </div>
              <div class="mt-2 max-h-64 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
                <label
                  v-for="account in filteredReasoningAccounts(reasoningBatchAccountQuery)"
                  :key="account.id"
                  class="flex min-w-0 cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2 text-xs last:border-b-0 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-800/70"
                >
                  <input
                    :checked="selectedReasoningBatchAccountIds.includes(account.id)"
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    @change="toggleReasoningBatchAccount(account.id, $event)"
                  />
                  <span class="min-w-0 flex-1">
                    <span class="block truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</span>
                    <span class="block truncate text-gray-500 dark:text-dark-400">{{ account.platform }} / {{ account.type }} · #{{ account.id }}</span>
                  </span>
                </label>
                <div v-if="filteredReasoningAccounts(reasoningBatchAccountQuery).length === 0" class="tools-empty min-h-[96px]">
                  没有匹配的账号。
                </div>
              </div>
            </div>
          </details>
          <button
            type="button"
            class="btn btn-secondary btn-sm mt-2 w-full"
            :disabled="reasoningBatchSaving || selectedReasoningBatchAccountIds.length === 0 || !selectedReasoningAccountId"
            @click="applyReasoningToSelectedAccounts"
          >
            <Icon name="copy" size="sm" />
            {{ reasoningBatchSaving ? '应用中...' : '应用到选中账号' }}
          </button>
          <p class="input-hint">会覆盖所选账号的手工映射；自动 IQ 映射的账号范围仍单独配置。</p>
        </div>
      </div>

      <div class="min-w-0">
        <div v-if="reasoningLoading" class="tools-empty min-h-[180px]">正在加载模型映射...</div>
        <div v-else-if="!selectedReasoningAccountId" class="tools-empty min-h-[180px]">请选择账号开始配置。</div>
        <template v-else>
          <div class="mb-3 rounded-lg bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
            未设置推理强度按 medium 匹配。default 会统一按 medium 处理；映射只改发往上游的请求，用户账单仍按源模型和源 effort 计算。
          </div>
          <div v-if="reasoningRows.length" class="space-y-2">
            <div v-for="(row, index) in reasoningRows" :key="index" class="grid gap-2 md:grid-cols-[minmax(0,1fr)_150px_minmax(0,1fr)_150px_auto]">
              <input v-model="row.fromModel" class="input" placeholder="源模型，例如 gpt-5.6-sol" />
              <select v-model="row.fromEffort" class="input">
                <option v-for="effort in reasoningEffortOptions" :key="effort" :value="effort">源 {{ effort }}</option>
              </select>
              <input v-model="row.toModel" class="input" placeholder="目标模型，例如 gpt-5.5" />
              <select v-model="row.toEffort" class="input">
                <option value="">目标 effort</option>
                <option v-for="effort in reasoningEffortOptions" :key="effort" :value="effort">{{ effort }}</option>
              </select>
              <button type="button" class="btn btn-secondary" title="删除映射" @click="reasoningRows.splice(index, 1)">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <div v-else class="tools-empty min-h-[100px]">暂无模型映射。</div>
          <div class="mt-3 flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary" @click="reasoningRows.push({ fromModel: '', fromEffort: 'medium', toModel: '', toEffort: '' })">
              <Icon name="plus" size="sm" /> 添加映射
            </button>
            <button type="button" class="btn btn-primary" :disabled="reasoningSaving" @click="saveReasoningAccount">
              <Icon name="check" size="sm" /> {{ reasoningSaving ? '保存中...' : '保存配置' }}
            </button>
          </div>
          <p v-if="reasoningError" class="mt-3 text-sm text-red-600 dark:text-red-300">{{ reasoningError }}</p>
        </template>
      </div>
    </div>

    <div class="mt-4 tools-panel">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">自动 IQ 映射</h3>
          <p class="text-xs text-gray-500 dark:text-dark-400">只读取 GPT 系列 Radar IQ；自动规则保存在插件配置和内存快照中。</p>
        </div>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="reasoningAutoLoading" @click="refreshReasoningAuto">
            <Icon name="refresh" size="sm" :class="reasoningAutoLoading ? 'animate-spin' : ''" /> 刷新状态
          </button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="reasoningAutoSaving" @click="saveReasoningAuto">
            <Icon name="check" size="sm" /> 保存自动配置
          </button>
        </div>
      </div>
      <div class="grid gap-2 md:grid-cols-6">
        <label class="flex items-center gap-2 text-sm md:col-span-2">
          <input v-model="reasoningAutoConfig.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          启用定时自动映射
        </label>
        <input v-model="reasoningAutoConfig.baseline_model" class="input" placeholder="基准模型" />
        <input v-model.number="reasoningAutoConfig.baseline_gap" class="input" type="number" min="0" step="0.1" placeholder="基准 IQ +" />
        <input v-model.number="reasoningAutoConfig.refresh_interval_minutes" class="input" type="number" min="5" placeholder="无固定时刻时的刷新分钟" aria-label="无固定时刻时的刷新分钟" />
        <select v-model="reasoningAutoConfig.iq_aggregation" class="input">
          <option value="max">IQ 最高值</option>
          <option value="latest">最新 IQ</option>
          <option value="mean">IQ 均值</option>
        </select>
      </div>
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <span class="text-sm font-medium text-gray-900 dark:text-white">自动公式</span>
        <div class="inline-flex overflow-hidden rounded-lg border border-gray-200 bg-white text-xs dark:border-dark-700 dark:bg-dark-900">
          <button
            type="button"
            class="px-3 py-1.5 font-medium transition-colors"
            :class="reasoningAutoConfig.formula_mode === 'natural_gap' ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-800'"
            @click="setReasoningFormulaMode('natural_gap')"
          >
            自然分段
          </button>
          <button
            type="button"
            class="border-l border-gray-200 px-3 py-1.5 font-medium transition-colors dark:border-dark-700"
            :class="reasoningAutoConfig.formula_mode === 'iq_cost' ? 'bg-primary-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-800'"
            @click="setReasoningFormulaMode('iq_cost')"
          >
            费用+IQ
          </button>
        </div>
        <span class="text-xs text-gray-500 dark:text-dark-400">费用与 IQ 来自 Radar，通配兜底仍指向 Luna max。</span>
      </div>
      <div class="mt-3 grid gap-3 lg:grid-cols-[minmax(0,1fr)_220px]">
        <div class="min-w-0">
          <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
            <label class="text-sm font-medium text-gray-900 dark:text-white">Cron 刷新列表</label>
            <span class="text-xs text-gray-500 dark:text-dark-400">为空时使用上面的分钟间隔</span>
          </div>
          <div v-if="reasoningAutoConfig.cron_schedules?.length" class="space-y-2">
            <div v-for="(_, index) in reasoningAutoConfig.cron_schedules" :key="index" class="flex items-center gap-1">
              <input v-model="reasoningAutoConfig.cron_schedules[index]" class="input min-w-0 flex-1 font-mono" type="text" placeholder="30 7 * * *" :aria-label="`Cron 刷新规则 ${index + 1}`" />
              <button type="button" class="btn btn-secondary" title="删除 Cron 规则" @click="removeCronSchedule(index)">
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </div>
          <button type="button" class="btn btn-secondary btn-sm mt-2" @click="addCronSchedule">
            <Icon name="plus" size="sm" /> 添加 Cron 规则
          </button>
        </div>
        <label class="block">
          <span class="input-label">计划时区</span>
          <input v-model="reasoningAutoConfig.schedule_timezone" class="input" placeholder="Asia/Shanghai" />
        </label>
      </div>
      <div class="mt-3 border-t border-gray-200 pt-3 dark:border-dark-700">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
          <label class="text-sm font-medium text-gray-900 dark:text-white">自动映射账号</label>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ reasoningAutoConfig.account_ids?.length ? `已选择 ${reasoningAutoConfig.account_ids.length} 个账号` : '未选择时对全部 OpenAI 账号生效' }}
          </span>
        </div>
        <details v-if="reasoningAccounts.length" class="account-multi-select">
          <summary class="account-multi-trigger">
            <span class="min-w-0 truncate">{{ reasoningAccountSelectionText(reasoningAutoConfig.account_ids || [], '全部 OpenAI 账号') }}</span>
            <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400" />
          </summary>
          <div class="account-multi-menu">
            <input
              v-model="reasoningAutoAccountQuery"
              class="input"
              type="search"
              placeholder="搜索账号名称、平台、类型或 ID"
            />
            <div class="mt-2 flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="filteredReasoningAccounts(reasoningAutoAccountQuery).length === 0" @click="selectFilteredReasoningAutoAccounts">
                选择当前结果
              </button>
              <button type="button" class="btn btn-secondary btn-sm" @click="selectAllReasoningAutoAccounts">全选账号</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="(reasoningAutoConfig.account_ids || []).length === 0" @click="clearReasoningAutoAccounts">清空</button>
            </div>
            <div v-if="reasoningAutoConfig.account_ids?.length" class="mt-2 flex max-h-20 flex-wrap gap-1.5 overflow-y-auto">
              <button
                v-for="accountId in reasoningAutoConfig.account_ids"
                :key="accountId"
                type="button"
                class="inline-flex min-w-0 items-center gap-1 rounded-full border border-primary-200 bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:border-primary-900/70 dark:bg-primary-950/40 dark:text-primary-300"
                :title="reasoningAccountLabel(accountId)"
                @click="removeReasoningAutoAccount(accountId)"
              >
                <span class="max-w-[180px] truncate">{{ reasoningAccountLabel(accountId) }}</span>
                <Icon name="x" size="xs" />
              </button>
            </div>
            <div class="mt-2 max-h-64 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
              <label
                v-for="account in filteredReasoningAccounts(reasoningAutoAccountQuery)"
                :key="account.id"
                class="flex min-w-0 cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2 text-xs last:border-b-0 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-800/70"
              >
                <input
                  :checked="(reasoningAutoConfig.account_ids || []).includes(account.id)"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  @change="toggleReasoningAutoAccount(account.id, $event)"
                />
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</span>
                  <span class="block truncate text-gray-500 dark:text-dark-400">{{ account.platform }} / {{ account.type }} · #{{ account.id }}</span>
                </span>
              </label>
              <div v-if="filteredReasoningAccounts(reasoningAutoAccountQuery).length === 0" class="tools-empty min-h-[96px]">
                没有匹配的账号。
              </div>
            </div>
          </div>
        </details>
        <p v-else class="text-xs text-gray-500 dark:text-dark-400">正在加载 OpenAI 账号列表。</p>
      </div>
      <div v-if="reasoningAutoStatus.last_error" class="mt-2 text-xs text-red-600 dark:text-red-300">{{ reasoningAutoStatus.last_error }}</div>
      <div v-if="reasoningAutoStatus.plan?.baseline" class="mt-2 text-xs text-gray-500 dark:text-dark-400">
        Luna 基准 {{ formatAutoIQ(reasoningAutoStatus.plan.baseline.iq) }} · 直映上限 {{ formatAutoIQ(reasoningAutoStatus.plan.baseline_limit) }} · 当前自动规则 {{ reasoningAutoMappings.length }} 条
      </div>
      <div class="mt-3 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
        <div class="border-b border-gray-200 bg-gray-50 px-3 py-2 text-xs font-semibold text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200">
          当前自动规则
        </div>
        <div v-if="reasoningAutoMappings.length" class="overflow-x-auto">
          <table class="min-w-[720px] divide-y divide-gray-200 text-xs dark:divide-dark-700">
            <thead class="text-left text-gray-500 dark:text-dark-400">
              <tr>
                <th class="py-2 pl-3 pr-3">源模型</th>
                <th class="py-2 pr-3">源 effort</th>
                <th class="py-2 pr-3">目标模型</th>
                <th class="py-2 pr-3">目标 effort</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-for="mapping in reasoningAutoMappings" :key="`${mapping.from_model}-${mapping.from_effort || ''}-${mapping.to_model || ''}-${mapping.to_effort}`">
                <td class="py-2 pl-3 pr-3 font-mono">{{ mapping.from_model }}</td>
                <td class="py-2 pr-3 font-mono">{{ mapping.from_effort || 'medium' }}</td>
                <td class="py-2 pr-3 font-mono">{{ mapping.to_model || reasoningAutoConfig.baseline_model || '-' }}</td>
                <td class="py-2 pr-3 font-mono">{{ mapping.to_effort }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="tools-empty min-h-[96px]">暂无自动规则。</div>
      </div>
      <div v-if="reasoningStats.length" class="mt-3 overflow-x-auto">
        <table class="min-w-[720px] divide-y divide-gray-200 text-xs dark:divide-dark-700">
          <thead class="text-left text-gray-500 dark:text-dark-400"><tr><th class="py-2 pr-3">账号</th><th class="py-2 pr-3">源</th><th class="py-2 pr-3">目标</th><th class="py-2 pr-3">请求</th><th class="py-2 pr-3">Tokens</th></tr></thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="stat in reasoningStats.slice(0, 20)" :key="`${stat.account_id}-${stat.from_model}-${stat.from_effort}-${stat.to_model}-${stat.to_effort}`">
              <td class="py-2 pr-3">{{ stat.account_id }}</td>
              <td class="py-2 pr-3 font-mono">{{ stat.from_model }}@{{ stat.from_effort }}</td>
              <td class="py-2 pr-3 font-mono">{{ stat.to_model }}@{{ stat.to_effort }}</td>
              <td class="py-2 pr-3">{{ stat.requests }}</td>
              <td class="py-2 pr-3">{{ stat.input_tokens + stat.output_tokens }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import {
  getModelReasoningAuto,
  getModelReasoningConfig,
  getModelReasoningStats,
  refreshModelReasoningAuto,
  updateModelReasoningConfigBatch,
  updateModelReasoningAuto,
  updateModelReasoningConfig,
  type ModelReasoningAutoConfig,
  type ModelReasoningAutoStatus,
  type ModelReasoningUsageStat
} from './api'
import type { Account } from '@/types'

interface ReasoningMappingRow {
  fromModel: string
  fromEffort: string
  toModel: string
  toEffort: string
}

const reasoningEffortOptions = ['minimal', 'low', 'medium', 'high', 'xhigh', 'ultra', 'max']
const appStore = useAppStore()
const reasoningAccounts = ref<Account[]>([])
const selectedReasoningAccountId = ref<number | null>(null)
const selectedReasoningBatchAccountIds = ref<number[]>([])
const reasoningBatchAccountQuery = ref('')
const reasoningAutoAccountQuery = ref('')
const reasoningRows = ref<ReasoningMappingRow[]>([])
const reasoningLoading = ref(false)
const reasoningSaving = ref(false)
const reasoningBatchSaving = ref(false)
const reasoningError = ref('')
const defaultReasoningAutoConfig = (): ModelReasoningAutoConfig => ({
  enabled: false,
  radar_base_url: 'https://api.codexradar.com',
  refresh_interval_minutes: 360,
  timeout_seconds: 30,
  iq_aggregation: 'max',
  iq_window_hours: 0,
  formula_mode: 'natural_gap',
  baseline_model: 'gpt-5.6-luna',
  baseline_gap: 5,
  account_ids: [],
  cron_schedules: [],
  schedule_timezone: 'Asia/Shanghai'
})
const reasoningAutoConfig = ref<ModelReasoningAutoConfig>(defaultReasoningAutoConfig())
const reasoningAutoStatus = ref<ModelReasoningAutoStatus>({})
const reasoningAutoMappings = ref<Array<{ from_model: string; from_effort?: string; to_model?: string; to_effort: string }>>([])
const reasoningStats = ref<ModelReasoningUsageStat[]>([])
const reasoningAutoLoading = ref(false)
const reasoningAutoSaving = ref(false)

const errorMessage = (error: unknown, fallback: string) => {
  if (error && typeof error === 'object') {
    const maybe = error as { message?: unknown; response?: { data?: { detail?: unknown; message?: unknown } } }
    const detail = maybe.response?.data?.detail || maybe.response?.data?.message || maybe.message
    if (typeof detail === 'string' && detail.trim()) return detail
  }
  return fallback
}

async function loadReasoningAccounts() {
  reasoningLoading.value = true
  reasoningError.value = ''
  try {
    const result = await adminAPI.accounts.list(1, 500, { platform: 'openai' })
    reasoningAccounts.value = result.items || []
    if (selectedReasoningAccountId.value == null && reasoningAccounts.value.length > 0) {
      selectedReasoningAccountId.value = reasoningAccounts.value[0].id
    }
    if (selectedReasoningAccountId.value != null) await loadReasoningAccount(selectedReasoningAccountId.value)
  } catch (error) {
    reasoningError.value = errorMessage(error, '加载 OpenAI 账号失败。')
  } finally {
    reasoningLoading.value = false
  }
}

async function loadReasoningAccount(accountID: number) {
  reasoningError.value = ''
  try {
    const config = await getModelReasoningConfig(accountID)
    reasoningRows.value = (config.mappings || []).map((row) => ({
      fromModel: row.from_model,
      fromEffort: row.from_effort || 'medium',
      toModel: row.to_model || '',
      toEffort: row.to_effort
    }))
  } catch (error) {
    reasoningRows.value = []
    reasoningError.value = errorMessage(error, '加载模型映射失败。')
  }
}

async function loadReasoningAuto() {
  reasoningAutoLoading.value = true
  try {
    const [auto, stats] = await Promise.all([getModelReasoningAuto(), getModelReasoningStats()])
    reasoningAutoConfig.value = normalizeReasoningAutoConfig(auto?.config)
    reasoningAutoStatus.value = auto.status || {}
    reasoningAutoMappings.value = auto.mappings || []
    reasoningStats.value = stats || []
  } catch (error) {
    reasoningError.value = errorMessage(error, '加载自动 IQ 映射失败。')
  } finally {
    reasoningAutoLoading.value = false
  }
}

function normalizeReasoningAutoConfig(value: Partial<ModelReasoningAutoConfig> | null | undefined): ModelReasoningAutoConfig {
  const config = value || {}
  return {
    ...defaultReasoningAutoConfig(),
    ...config,
    account_ids: Array.isArray(config.account_ids) ? normalizeReasoningAccountIDs(config.account_ids) : [],
    cron_schedules: Array.isArray(config.cron_schedules)
      ? config.cron_schedules
      : convertRefreshTimesToCron(config.refresh_times || []),
    schedule_timezone: config.schedule_timezone || 'Asia/Shanghai',
    formula_mode: config.formula_mode === 'iq_cost' ? 'iq_cost' : 'natural_gap'
  }
}

function setReasoningFormulaMode(mode: 'natural_gap' | 'iq_cost') {
  reasoningAutoConfig.value.formula_mode = mode
}

function normalizeReasoningAccountIDs(values: number[]): number[] {
  return [...new Set(values.map((value) => Number(value)).filter((value) => Number.isInteger(value) && value > 0))]
    .sort((left, right) => left - right)
}

function reasoningAccountLabel(accountId: number): string {
  const account = reasoningAccounts.value.find((item) => item.id === accountId)
  if (!account) return `#${accountId}`
  return `${account.name} · ${account.platform}/${account.type} · #${account.id}`
}

function reasoningAccountSelectionText(accountIds: number[], emptyLabel: string): string {
  const ids = normalizeReasoningAccountIDs(accountIds || [])
  if (ids.length === 0) return emptyLabel
  const labels = ids.slice(0, 2).map(reasoningAccountLabel)
  const suffix = ids.length > labels.length ? ` 等 ${ids.length} 个` : ''
  return `${labels.join('，')}${suffix}`
}

function filteredReasoningAccounts(query: string): Account[] {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return reasoningAccounts.value
  return reasoningAccounts.value.filter((account) => (
    account.name.toLowerCase().includes(normalized) ||
    account.platform.toLowerCase().includes(normalized) ||
    account.type.toLowerCase().includes(normalized) ||
    String(account.id).includes(normalized)
  ))
}

function toggleReasoningBatchAccount(accountId: number, event: Event) {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  selectedReasoningBatchAccountIds.value = checked
    ? normalizeReasoningAccountIDs([...selectedReasoningBatchAccountIds.value, accountId])
    : selectedReasoningBatchAccountIds.value.filter((id) => id !== accountId)
}

function selectFilteredReasoningBatchAccounts() {
  selectedReasoningBatchAccountIds.value = normalizeReasoningAccountIDs([
    ...selectedReasoningBatchAccountIds.value,
    ...filteredReasoningAccounts(reasoningBatchAccountQuery.value).map((account) => account.id)
  ])
}

function removeReasoningBatchAccount(accountId: number) {
  selectedReasoningBatchAccountIds.value = selectedReasoningBatchAccountIds.value.filter((id) => id !== accountId)
}

function toggleReasoningAutoAccount(accountId: number, event: Event) {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  const current = reasoningAutoConfig.value.account_ids || []
  reasoningAutoConfig.value.account_ids = checked
    ? normalizeReasoningAccountIDs([...current, accountId])
    : current.filter((id) => id !== accountId)
}

function selectFilteredReasoningAutoAccounts() {
  reasoningAutoConfig.value.account_ids = normalizeReasoningAccountIDs([
    ...(reasoningAutoConfig.value.account_ids || []),
    ...filteredReasoningAccounts(reasoningAutoAccountQuery.value).map((account) => account.id)
  ])
}

function selectAllReasoningAutoAccounts() {
  reasoningAutoConfig.value.account_ids = normalizeReasoningAccountIDs(reasoningAccounts.value.map((account) => account.id))
}

function clearReasoningAutoAccounts() {
  reasoningAutoConfig.value.account_ids = []
}

function removeReasoningAutoAccount(accountId: number) {
  reasoningAutoConfig.value.account_ids = (reasoningAutoConfig.value.account_ids || []).filter((id) => id !== accountId)
}

function formatAutoIQ(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(1) : '-'
}

function convertRefreshTimesToCron(times: string[]) {
  return times.flatMap((value) => {
    const normalized = value.trim().replace('.', ':')
    const [hour, minute] = normalized.split(':')
    if (!hour || !minute) return []
    return [`${Number(minute)} ${Number(hour)} * * *`]
  })
}

function addCronSchedule() {
  const suggestions = ['30 7 * * *', '30 12 * * *', '30 20 * * *']
  reasoningAutoConfig.value.cron_schedules.push(suggestions[reasoningAutoConfig.value.cron_schedules.length] || '')
}

function removeCronSchedule(index: number) {
  reasoningAutoConfig.value.cron_schedules.splice(index, 1)
}

async function saveReasoningAuto() {
  reasoningAutoSaving.value = true
  reasoningError.value = ''
  try {
    await updateModelReasoningAuto(reasoningAutoConfig.value)
    await loadReasoningAuto()
    appStore.showSuccess('自动 IQ 映射配置已保存。')
  } catch (error) {
    reasoningError.value = errorMessage(error, '保存自动 IQ 映射配置失败。')
  } finally {
    reasoningAutoSaving.value = false
  }
}

async function refreshReasoningAuto() {
  reasoningAutoLoading.value = true
  reasoningError.value = ''
  try {
    reasoningAutoStatus.value = await refreshModelReasoningAuto()
    await loadReasoningAuto()
  } catch (error) {
    reasoningError.value = errorMessage(error, '刷新 Radar IQ 映射失败。')
  } finally {
    reasoningAutoLoading.value = false
  }
}

watch(selectedReasoningAccountId, (accountID) => {
  if (accountID != null) void loadReasoningAccount(accountID)
})

async function saveReasoningAccount() {
  const accountID = selectedReasoningAccountId.value
  if (accountID == null) return
  reasoningSaving.value = true
  reasoningError.value = ''
  try {
    await updateModelReasoningConfig(accountID, buildCurrentReasoningConfig())
    appStore.showSuccess('模型映射与推理强度已保存。')
  } catch (error) {
    reasoningError.value = errorMessage(error, '保存模型映射失败。')
  } finally {
    reasoningSaving.value = false
  }
}

function buildCurrentReasoningConfig() {
  const mappings: Array<{ from_model: string; from_effort: string; to_model?: string; to_effort: string }> = []
  for (const row of reasoningRows.value) {
    const fromModel = row.fromModel.trim()
    const fromEffort = row.fromEffort.trim().toLowerCase()
    const toModel = row.toModel.trim()
    const toEffort = row.toEffort.trim().toLowerCase()
    if (!fromModel || !fromEffort || !toEffort || !reasoningEffortOptions.includes(fromEffort) || !reasoningEffortOptions.includes(toEffort)) continue
    mappings.push(toModel
      ? { from_model: fromModel, from_effort: fromEffort, to_model: toModel, to_effort: toEffort }
      : { from_model: fromModel, from_effort: fromEffort, to_effort: toEffort })
  }
  return { mappings }
}

function selectAllReasoningBatchAccounts() {
  selectedReasoningBatchAccountIds.value = normalizeReasoningAccountIDs(reasoningAccounts.value.map((account) => account.id))
}

async function applyReasoningToSelectedAccounts() {
  if (selectedReasoningBatchAccountIds.value.length === 0 || selectedReasoningAccountId.value == null) return
  reasoningBatchSaving.value = true
  reasoningError.value = ''
  try {
    const result = await updateModelReasoningConfigBatch(selectedReasoningBatchAccountIds.value, buildCurrentReasoningConfig())
    appStore.showSuccess(`已将当前映射应用到 ${result.updated_account_ids.length} 个账号。`)
  } catch (error) {
    reasoningError.value = errorMessage(error, '批量应用模型映射失败。')
  } finally {
    reasoningBatchSaving.value = false
  }
}

onMounted(() => {
  void loadReasoningAccounts()
  void loadReasoningAuto()
})
</script>

<style scoped>
.account-multi-select {
  @apply relative;
}

.account-multi-trigger {
  @apply flex min-h-[38px] cursor-pointer list-none items-center justify-between gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 shadow-sm transition-colors hover:border-gray-400 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200 dark:hover:border-dark-500;
}

.account-multi-trigger::-webkit-details-marker {
  display: none;
}

.account-multi-menu {
  @apply absolute left-0 right-0 top-full z-30 mt-2 rounded-lg border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-700 dark:bg-dark-900;
}
</style>
