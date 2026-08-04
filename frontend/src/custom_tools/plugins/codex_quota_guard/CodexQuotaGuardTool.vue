<template>
  <section class="tools-surface">
    <div class="tools-section-header">
      <div>
        <h2 class="tools-title">上游额度保护</h2>
        <p class="tools-description">按插件账本中的每日、每周成本和 Token 用量暂停账号调度，支持多个限制器组。</p>
      </div>
      <div class="tools-badges">
        <span class="tools-badge">{{ codexQuotaGuardStatus?.running ? '运行中' : '未启动' }}</span>
        <span class="tools-badge">来源 {{ codexQuotaGuardSourceLabel }}</span>
        <span class="tools-badge">缓存 {{ codexQuotaGuardStatus?.admin_api_key_cached ? '已持有 Key' : '无 Key' }}</span>
      </div>
    </div>

    <div class="grid grid-cols-1 gap-4 2xl:grid-cols-[minmax(0,0.88fr)_minmax(0,1.12fr)]">
      <div class="space-y-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">限制器组</h3>
            <p class="text-xs text-gray-500 dark:text-dark-400">一个限制器可绑定多个账号；不同限制器可共享账号并独立设置上限。</p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="addCodexQuotaGuardPolicy">
            <Icon name="plus" size="sm" />
            添加限制器
          </button>
        </div>

        <div v-for="(policy, index) in codexQuotaGuardPolicies" :key="policy.id" class="tools-panel space-y-3">
          <div class="flex items-center justify-between gap-3">
            <div class="flex min-w-0 items-center gap-2">
              <span class="tools-badge">限制器 {{ index + 1 }}</span>
              <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ policy.name || policy.id }}</span>
            </div>
            <button
              v-if="codexQuotaGuardPolicies.length > 1"
              type="button"
              class="btn btn-secondary btn-sm"
              title="删除限制器"
              @click="removeCodexQuotaGuardPolicy(index)"
            >
              <Icon name="trash" size="sm" />
            </button>
          </div>

          <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
            <div>
              <label class="input-label">名称</label>
              <input v-model="policy.name" class="input" placeholder="例如 OpenAI 日限额" />
            </div>
            <div>
              <label class="input-label">标识</label>
              <input v-model="policy.id" class="input font-mono text-xs" placeholder="limiter-1" />
            </div>
          </div>

          <div class="grid grid-cols-1 gap-3 xl:grid-cols-4">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="policy.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              启用
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="policy.dry_run" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              Dry Run
            </label>
            <div>
              <label class="input-label">扫描间隔（秒）</label>
              <input v-model.number="policy.interval_seconds" class="input" type="number" min="10" max="3600" />
            </div>
            <div>
              <label class="input-label">本地时区</label>
              <input v-model="policy.daily_spend_timezone" class="input" placeholder="Asia/Shanghai" />
            </div>
          </div>

          <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
            <div>
              <label class="input-label">每日成本上限（美元）</label>
              <input v-model.number="policy.daily_spend_limit_usd" class="input" type="number" min="0" step="0.01" placeholder="0 = 不限制" />
            </div>
            <div>
              <label class="input-label">每日 Token 上限</label>
              <input v-model.number="policy.daily_token_limit" class="input" type="number" min="0" step="1" placeholder="0 = 不限制" />
            </div>
            <div>
              <label class="input-label">每周成本上限（美元）</label>
              <input v-model.number="policy.weekly_spend_limit_usd" class="input" type="number" min="0" step="0.01" placeholder="0 = 不限制" />
            </div>
            <div>
              <label class="input-label">每周 Token 上限</label>
              <input v-model.number="policy.weekly_token_limit" class="input" type="number" min="0" step="1" placeholder="0 = 不限制" />
            </div>
          </div>

          <div>
            <div class="mb-1.5 flex items-center justify-between gap-2">
              <label class="input-label mb-0">保护账号（可选）</label>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ quotaGuardPolicyScopeLabel(policy) }}</span>
            </div>
            <details class="quota-account-select">
              <summary class="quota-account-trigger">
                <span class="min-w-0 truncate">{{ quotaGuardPolicySelectionText(policy) }}</span>
                <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400" />
              </summary>
              <div class="quota-account-menu">
                <div class="flex min-w-0 gap-2">
                  <input
                    v-model="policy.account_query"
                    class="input min-w-0 flex-1"
                    placeholder="搜索账号名称、平台、类型或 ID"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm shrink-0"
                    :disabled="codexQuotaGuardAccountsLoading"
                    title="刷新账号列表"
                    @click="loadCodexQuotaGuardAccounts"
                  >
                    <Icon name="refresh" size="sm" :class="codexQuotaGuardAccountsLoading ? 'animate-spin' : ''" />
                  </button>
                </div>

                <div class="mt-2 flex flex-wrap gap-2">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="filteredQuotaGuardAccounts(policy).length === 0" @click="selectFilteredQuotaGuardAccounts(policy)">
                    选择当前结果
                  </button>
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="codexQuotaGuardAccounts.length === 0" @click="selectAllQuotaGuardAccounts(policy)">
                    全选账号
                  </button>
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="policy.account_ids.length === 0" @click="clearQuotaGuardAccounts(policy)">
                    清空
                  </button>
                </div>

                <div v-if="policy.account_ids.length" class="mt-2 flex max-h-20 flex-wrap gap-1.5 overflow-y-auto">
                  <button
                    v-for="accountId in policy.account_ids"
                    :key="accountId"
                    type="button"
                    class="inline-flex min-w-0 items-center gap-1 rounded-full border border-primary-200 bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:border-primary-900/70 dark:bg-primary-950/40 dark:text-primary-300"
                    :title="quotaGuardAccountLabel(accountId)"
                    @click="removeQuotaGuardAccount(policy, accountId)"
                  >
                    <span class="max-w-[180px] truncate">{{ quotaGuardAccountLabel(accountId) }}</span>
                    <Icon name="x" size="xs" />
                  </button>
                </div>

                <div class="mt-2 max-h-64 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
                  <div v-if="codexQuotaGuardAccountsLoading" class="tools-empty min-h-[96px]">正在加载账号列表...</div>
                  <div v-else-if="codexQuotaGuardAccountsError" class="tools-empty min-h-[96px] text-red-600 dark:text-red-300">
                    {{ codexQuotaGuardAccountsError }}
                  </div>
                  <div v-else-if="filteredQuotaGuardAccounts(policy).length === 0" class="tools-empty min-h-[96px]">没有匹配的账号。</div>
                  <template v-else>
                    <label
                      v-for="account in filteredQuotaGuardAccounts(policy)"
                      :key="account.id"
                      class="flex min-w-0 cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2 text-sm last:border-b-0 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-800/70"
                    >
                      <input
                        :checked="policy.account_ids.includes(account.id)"
                        type="checkbox"
                        class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                        @change="toggleQuotaGuardAccount(policy, account.id, $event)"
                      />
                      <span class="min-w-0 flex-1">
                        <span class="block truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</span>
                        <span class="block truncate text-xs text-gray-500 dark:text-dark-400">{{ account.platform }} / {{ account.type }} · #{{ account.id }}</span>
                      </span>
                      <span :class="account.status === 'active' ? 'status-ok' : account.status === 'error' ? 'status-error' : 'status-muted'">
                        {{ account.status }}
                      </span>
                    </label>
                  </template>
                </div>
              </div>
            </details>
            <p class="input-hint">不选择账号时，此限制器作用于全部账号；选择后只保护这些账号。</p>
          </div>
        </div>

        <div>
          <label class="input-label">x-api-key（可选）</label>
          <input
            v-model="codexQuotaGuardAPIKey"
            class="input font-mono text-xs"
            type="password"
            placeholder="sk-admin-..."
            autocomplete="off"
          />
          <p class="input-hint">仅本次启动请求使用；成功后立即清空，不回显、不持久化。</p>
        </div>

        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-primary" :disabled="codexQuotaGuardOperating" @click="startCodexQuotaGuardTask">
            <Icon name="play" size="sm" />
            启动 / 更新
          </button>
          <button type="button" class="btn btn-secondary" :disabled="codexQuotaGuardOperating" @click="scanCodexQuotaGuardTask">
            <Icon name="search" size="sm" />
            立即扫描
          </button>
          <button type="button" class="btn btn-secondary" :disabled="codexQuotaGuardOperating" @click="releaseCodexQuotaGuardTask">
            <Icon name="lock" size="sm" />
            释放保护器封禁
          </button>
          <button type="button" class="btn btn-secondary" :disabled="codexQuotaGuardOperating" @click="stopCodexQuotaGuardTask">
            <Icon name="ban" size="sm" />
            停止
          </button>
          <button type="button" class="btn btn-secondary" :disabled="codexQuotaGuardLoading || codexQuotaGuardOperating" @click="loadCodexQuotaGuardStatus">
            <Icon name="refresh" size="sm" />
            刷新状态
          </button>
        </div>
      </div>

      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
          <div>
            <label class="input-label">最近扫描</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_run_at || '-' }}</div>
          </div>
          <div>
            <label class="input-label">最近错误</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_error || '-' }}</div>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-3 xl:grid-cols-5">
          <div>
            <label class="input-label">扫描账号</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_scanned_accounts ?? 0 }}</div>
          </div>
          <div>
            <label class="input-label">候选账号</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_scan_candidates ?? 0 }}</div>
          </div>
          <div>
            <label class="input-label">最近封禁</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_blocked_count ?? 0 }}</div>
          </div>
          <div>
            <label class="input-label">最近恢复</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_released_count ?? 0 }}</div>
          </div>
          <div>
            <label class="input-label">当前封禁</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.current_managed_count ?? 0 }}</div>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-3 xl:grid-cols-3">
          <div>
            <label class="input-label">最近封禁账号 ID</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_blocked_ids?.length ? codexQuotaGuardStatus.last_blocked_ids.join(', ') : '-' }}</div>
          </div>
          <div>
            <label class="input-label">最近恢复账号 ID</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.last_released_ids?.length ? codexQuotaGuardStatus.last_released_ids.join(', ') : '-' }}</div>
          </div>
          <div>
            <label class="input-label">当前封禁账号 ID</label>
            <div class="tools-readout">{{ codexQuotaGuardStatus?.current_managed_ids?.length ? codexQuotaGuardStatus.current_managed_ids.join(', ') : '-' }}</div>
          </div>
        </div>

        <div class="tools-panel">
          <div class="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">映射后流量统计</h3>
              <p class="text-xs text-gray-500 dark:text-dark-400">按请求模型 -> 实际上游模型聚合，展开后查看用户明细。</p>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <span class="tools-badge">{{ codexQuotaGuardTrafficRangeLabel }}</span>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="codexQuotaGuardTrafficLoading" @click="loadCodexQuotaGuardTraffic">
                <Icon name="refresh" size="sm" :class="codexQuotaGuardTrafficLoading ? 'animate-spin' : ''" />
                刷新流量
              </button>
            </div>
          </div>
          <div v-if="codexQuotaGuardTrafficError" class="mb-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
            {{ codexQuotaGuardTrafficError }}
          </div>
          <div v-if="codexQuotaGuardTrafficLoading" class="tools-empty min-h-[120px]">正在加载映射后流量...</div>
          <div v-else-if="codexQuotaGuardTrafficRows.length === 0" class="tools-empty min-h-[120px]">今日暂无映射后流量。</div>
          <div v-else class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
            <div class="overflow-x-auto">
              <table class="min-w-[820px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
                <thead class="bg-gray-50 text-xs font-semibold text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
                  <tr>
                    <th class="tools-th">模型</th>
                    <th class="tools-th text-right">请求</th>
                    <th class="tools-th text-right">Token</th>
                    <th class="tools-th text-right">实际</th>
                    <th class="tools-th text-right">成本</th>
                    <th class="tools-th text-right">标准</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/30">
                  <template v-for="row in codexQuotaGuardTrafficRows" :key="row.model">
                    <tr class="hover:bg-gray-50 dark:hover:bg-dark-800/60">
                      <td class="tools-td">
                        <button type="button" class="inline-flex max-w-[360px] items-center gap-2 text-left font-medium text-primary-600 dark:text-primary-300" @click="toggleCodexQuotaGuardTrafficRow(row.model)">
                          <Icon name="chevronDown" size="xs" class="shrink-0 transition-transform" :class="isCodexQuotaGuardTrafficExpanded(row.model) ? '' : '-rotate-90'" />
                          <span class="truncate">{{ row.model }}</span>
                        </button>
                      </td>
                      <td class="tools-td text-right">{{ formatQuotaGuardTrafficNumber(row.requests) }}</td>
                      <td class="tools-td text-right">{{ formatQuotaGuardTrafficTokens(row.total_tokens) }}</td>
                      <td class="tools-td text-right font-mono text-emerald-600 dark:text-emerald-300">{{ formatQuotaGuardTrafficCost(row.actual_cost) }}</td>
                      <td class="tools-td text-right font-mono text-orange-600 dark:text-orange-300">{{ formatQuotaGuardTrafficCost(row.account_cost ?? row.actual_cost) }}</td>
                      <td class="tools-td text-right font-mono text-slate-500 dark:text-dark-400">{{ formatQuotaGuardTrafficCost(row.cost) }}</td>
                    </tr>
                    <tr v-if="isCodexQuotaGuardTrafficExpanded(row.model) && codexQuotaGuardTrafficBreakdownLoading[row.model]">
                      <td colspan="6" class="px-3 py-3 text-center text-xs text-gray-500 dark:text-dark-400">正在加载明细...</td>
                    </tr>
                    <tr
                      v-for="item in codexQuotaGuardTrafficBreakdownByModel[row.model] || []"
                      v-if="isCodexQuotaGuardTrafficExpanded(row.model)"
                      :key="`${row.model}-${item.user_id}`"
                      class="bg-gray-50/60 dark:bg-dark-800/40"
                    >
                      <td class="tools-td pl-10 text-gray-600 dark:text-dark-300">{{ quotaGuardTrafficUserLabel(item) }}</td>
                      <td class="tools-td text-right text-gray-500 dark:text-dark-400">{{ formatQuotaGuardTrafficNumber(item.requests) }}</td>
                      <td class="tools-td text-right text-gray-500 dark:text-dark-400">{{ formatQuotaGuardTrafficTokens(item.total_tokens) }}</td>
                      <td class="tools-td text-right font-mono text-emerald-600 dark:text-emerald-300">{{ formatQuotaGuardTrafficCost(item.actual_cost) }}</td>
                      <td class="tools-td text-right font-mono text-orange-600 dark:text-orange-300">{{ formatQuotaGuardTrafficCost(item.account_cost ?? item.actual_cost) }}</td>
                      <td class="tools-td text-right font-mono text-slate-500 dark:text-dark-400">{{ formatQuotaGuardTrafficCost(item.cost) }}</td>
                    </tr>
                    <tr v-if="isCodexQuotaGuardTrafficExpanded(row.model) && !codexQuotaGuardTrafficBreakdownLoading[row.model] && (codexQuotaGuardTrafficBreakdownByModel[row.model] || []).length === 0" class="bg-gray-50/60 dark:bg-dark-800/40">
                      <td colspan="6" class="px-3 py-3 text-center text-xs text-gray-500 dark:text-dark-400">暂无用户明细。</td>
                    </tr>
                  </template>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <div v-if="codexQuotaGuardUsageRows.length" class="tools-panel">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">本地账本用量</h3>
          <div class="mt-2 space-y-1 text-xs text-gray-700 dark:text-dark-200">
            <div v-for="row in codexQuotaGuardUsageRows" :key="row.accountID" class="flex items-center justify-between gap-3">
              <span>账号 #{{ row.accountID }}</span>
              <span class="font-mono">日 ${{ row.dailyCost.toFixed(4) }} / {{ row.dailyTokens }} tok · 周 ${{ row.weeklyCost.toFixed(4) }} / {{ row.weeklyTokens }} tok</span>
            </div>
          </div>
        </div>

        <div v-if="codexQuotaGuardLastAction" class="tools-panel">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">最近动作</h3>
          <p class="mt-2 text-sm text-gray-700 dark:text-dark-200">
            扫描 {{ codexQuotaGuardLastAction.scanned_accounts }}，候选 {{ codexQuotaGuardLastAction.candidate_count }}，封禁 {{ codexQuotaGuardLastAction.blocked_count }}，恢复 {{ codexQuotaGuardLastAction.released_count }}
          </p>
          <p v-if="codexQuotaGuardLastAction.blocked_ids?.length" class="mt-2 text-sm text-gray-700 dark:text-dark-200">
            最近封禁账号：{{ codexQuotaGuardLastAction.blocked_ids.join(', ') }}
          </p>
          <p v-if="codexQuotaGuardLastAction.released_ids?.length" class="mt-2 text-sm text-gray-700 dark:text-dark-200">
            最近恢复账号：{{ codexQuotaGuardLastAction.released_ids.join(', ') }}
          </p>
          <p v-if="codexQuotaGuardLastAction.errors?.length" class="mt-2 text-sm text-red-700 dark:text-red-300">
            {{ codexQuotaGuardLastAction.errors.join('；') }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { getModelStats, getUserBreakdown } from '@/api/admin/dashboard'
import { useAppStore } from '@/stores/app'
import {
  getCodexQuotaGuardStatus,
  releaseCodexQuotaGuard,
  scanCodexQuotaGuard,
  startCodexQuotaGuard,
  stopCodexQuotaGuard,
  type CodexQuotaGuardPolicy,
  type CodexQuotaGuardScanResponse,
  type CodexQuotaGuardStatus
} from '../../api'
import type { Account, ModelStat, UserBreakdownItem } from '@/types'

interface QuotaGuardPolicyDraft {
  id: string
  name: string
  enabled: boolean
  interval_seconds: number
  account_ids: number[]
  account_query: string
  daily_spend_limit_usd: number
  daily_token_limit: number
  weekly_spend_limit_usd: number
  weekly_token_limit: number
  daily_spend_timezone: string
  dry_run: boolean
}

const formatQuotaGuardLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createQuotaGuardPolicyDraft = (index: number): QuotaGuardPolicyDraft => ({
  id: `limiter-${index}`,
  name: `限制器 ${index}`,
  enabled: true,
  interval_seconds: 60,
  account_ids: [],
  account_query: '',
  daily_spend_limit_usd: 0,
  daily_token_limit: 0,
  weekly_spend_limit_usd: 0,
  weekly_token_limit: 0,
  daily_spend_timezone: 'Asia/Shanghai',
  dry_run: false
})

const appStore = useAppStore()
const codexQuotaGuardLoading = ref(false)
const codexQuotaGuardOperating = ref(false)
const codexQuotaGuardStatus = ref<CodexQuotaGuardStatus | null>(null)
const codexQuotaGuardLastAction = ref<CodexQuotaGuardScanResponse | null>(null)
const codexQuotaGuardPolicies = ref<QuotaGuardPolicyDraft[]>([createQuotaGuardPolicyDraft(1)])
const codexQuotaGuardAPIKey = ref('')
const codexQuotaGuardAccounts = ref<Account[]>([])
const codexQuotaGuardAccountsLoading = ref(false)
const codexQuotaGuardAccountsError = ref('')
const codexQuotaGuardTrafficLoading = ref(false)
const codexQuotaGuardTrafficError = ref('')
const codexQuotaGuardMappingStats = ref<ModelStat[]>([])
const codexQuotaGuardExpandedTrafficModels = ref<string[]>([])
const codexQuotaGuardTrafficBreakdownLoading = ref<Record<string, boolean>>({})
const codexQuotaGuardTrafficBreakdownByModel = ref<Record<string, UserBreakdownItem[]>>({})
const codexQuotaGuardTrafficStartDate = ref(formatQuotaGuardLocalDate(new Date()))
const codexQuotaGuardTrafficEndDate = ref(formatQuotaGuardLocalDate(new Date()))

const errorMessage = (error: unknown, fallback: string): string => {
  if (error && typeof error === 'object') {
    const maybe = error as { message?: unknown; response?: { data?: { detail?: unknown; message?: unknown } } }
    const detail = maybe.response?.data?.detail || maybe.response?.data?.message || maybe.message
    if (typeof detail === 'string' && detail.trim()) return detail
  }
  return fallback
}

const normalizeQuotaGuardAccountIDs = (values: number[]): number[] => {
  return [...new Set(values.map((value) => Number(value)).filter((value) => Number.isInteger(value) && value > 0))]
    .sort((left, right) => left - right)
    .slice(0, 500)
}

const codexQuotaGuardSourceLabel = computed(() => {
  switch (codexQuotaGuardStatus.value?.admin_api_key_source || 'missing') {
    case 'provided':
      return 'x-api-key'
    case 'auto_created':
      return '自动创建'
    default:
      return '缺失'
  }
})

const codexQuotaGuardAccountById = computed(() => new Map(codexQuotaGuardAccounts.value.map((account) => [account.id, account])))
const codexQuotaGuardUsageRows = computed(() => {
  const status = codexQuotaGuardStatus.value
  const dailySpend = status?.daily_spend_by_account || {}
  const dailyTokens = status?.daily_tokens_by_account || {}
  const weeklySpend = status?.weekly_spend_by_account || {}
  const weeklyTokens = status?.weekly_tokens_by_account || {}
  const accountIDs = new Set([...Object.keys(dailySpend), ...Object.keys(dailyTokens), ...Object.keys(weeklySpend), ...Object.keys(weeklyTokens)])
  return [...accountIDs]
    .map((accountID) => ({
      accountID,
      dailyCost: Number(dailySpend[accountID]) || 0,
      dailyTokens: Number(dailyTokens[accountID]) || 0,
      weeklyCost: Number(weeklySpend[accountID]) || 0,
      weeklyTokens: Number(weeklyTokens[accountID]) || 0
    }))
    .sort((left, right) => Number(left.accountID) - Number(right.accountID))
})

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const codexQuotaGuardTrafficRows = computed(() => [...codexQuotaGuardMappingStats.value]
  .filter((row) => toFiniteNumber(row.requests) > 0 || toFiniteNumber(row.total_tokens) > 0 || toFiniteNumber(row.actual_cost) > 0)
  .sort((left, right) => toFiniteNumber(right.total_tokens) - toFiniteNumber(left.total_tokens) || toFiniteNumber(right.requests) - toFiniteNumber(left.requests)))
const codexQuotaGuardTrafficRangeLabel = computed(() => (
  codexQuotaGuardTrafficStartDate.value === codexQuotaGuardTrafficEndDate.value
    ? `今日 ${codexQuotaGuardTrafficStartDate.value}`
    : `${codexQuotaGuardTrafficStartDate.value} 至 ${codexQuotaGuardTrafficEndDate.value}`
))

const formatQuotaGuardTrafficTokens = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1_000_000) return `${(safeValue / 1_000_000).toFixed(2)}M`
  if (safeValue >= 1_000) return `${(safeValue / 1_000).toFixed(2)}K`
  return safeValue.toLocaleString()
}

const formatQuotaGuardTrafficNumber = (value: number | null | undefined): string => toFiniteNumber(value).toLocaleString()
const formatQuotaGuardTrafficCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  return `$${safeValue >= 1 ? safeValue.toFixed(2) : safeValue >= 0.01 ? safeValue.toFixed(3) : safeValue.toFixed(4)}`
}
const quotaGuardTrafficUserLabel = (item: UserBreakdownItem): string => item.email?.trim() || `用户 #${item.user_id}`

const loadCodexQuotaGuardStatus = async () => {
  codexQuotaGuardLoading.value = true
  try {
    const status = await getCodexQuotaGuardStatus()
    codexQuotaGuardStatus.value = status
    const policies = status.policies?.length ? status.policies : [status.config]
    codexQuotaGuardPolicies.value = policies.map((policy, index) => ({
      id: policy.id || `limiter-${index + 1}`,
      name: policy.name || `限制器 ${index + 1}`,
      enabled: policy.enabled,
      interval_seconds: policy.interval_seconds || 60,
      account_ids: normalizeQuotaGuardAccountIDs(policy.account_ids || []),
      account_query: '',
      daily_spend_limit_usd: policy.daily_spend_limit_usd || 0,
      daily_token_limit: policy.daily_token_limit || 0,
      weekly_spend_limit_usd: policy.weekly_spend_limit_usd || 0,
      weekly_token_limit: policy.weekly_token_limit || 0,
      daily_spend_timezone: policy.daily_spend_timezone || 'Asia/Shanghai',
      dry_run: policy.dry_run
    }))
  } catch (error) {
    appStore.showError(errorMessage(error, '读取保护器状态失败'))
  } finally {
    codexQuotaGuardLoading.value = false
  }
}

const loadCodexQuotaGuardAccounts = async () => {
  codexQuotaGuardAccountsLoading.value = true
  codexQuotaGuardAccountsError.value = ''
  try {
    const result = await adminAPI.accounts.list(1, 500, { sort_by: 'id', sort_order: 'asc' })
    codexQuotaGuardAccounts.value = result.items || []
  } catch (error) {
    codexQuotaGuardAccounts.value = []
    codexQuotaGuardAccountsError.value = errorMessage(error, '账号列表加载失败')
  } finally {
    codexQuotaGuardAccountsLoading.value = false
  }
}

const loadCodexQuotaGuardTraffic = async () => {
  codexQuotaGuardTrafficLoading.value = true
  codexQuotaGuardTrafficError.value = ''
  try {
    const response = await getModelStats({
      start_date: codexQuotaGuardTrafficStartDate.value,
      end_date: codexQuotaGuardTrafficEndDate.value,
      model_source: 'mapping'
    })
    codexQuotaGuardMappingStats.value = Array.isArray(response.models) ? response.models : []
    const visibleModels = new Set(codexQuotaGuardTrafficRows.value.map((row) => row.model))
    codexQuotaGuardExpandedTrafficModels.value = codexQuotaGuardExpandedTrafficModels.value.filter((model) => visibleModels.has(model))
    codexQuotaGuardTrafficBreakdownByModel.value = Object.fromEntries(
      Object.entries(codexQuotaGuardTrafficBreakdownByModel.value).filter(([model]) => visibleModels.has(model))
    )
  } catch (error) {
    codexQuotaGuardMappingStats.value = []
    codexQuotaGuardTrafficError.value = errorMessage(error, '加载映射后流量统计失败')
  } finally {
    codexQuotaGuardTrafficLoading.value = false
  }
}

const loadCodexQuotaGuardTrafficBreakdown = async (model: string) => {
  codexQuotaGuardTrafficBreakdownLoading.value = { ...codexQuotaGuardTrafficBreakdownLoading.value, [model]: true }
  try {
    const response = await getUserBreakdown({
      start_date: codexQuotaGuardTrafficStartDate.value,
      end_date: codexQuotaGuardTrafficEndDate.value,
      model_source: 'mapping',
      model,
      limit: 10,
      sort_by: 'actual_cost'
    })
    codexQuotaGuardTrafficBreakdownByModel.value = {
      ...codexQuotaGuardTrafficBreakdownByModel.value,
      [model]: Array.isArray(response.users) ? response.users : []
    }
  } catch (error) {
    codexQuotaGuardTrafficError.value = errorMessage(error, `加载 ${model} 的用户明细失败`)
  } finally {
    codexQuotaGuardTrafficBreakdownLoading.value = { ...codexQuotaGuardTrafficBreakdownLoading.value, [model]: false }
  }
}

const isCodexQuotaGuardTrafficExpanded = (model: string): boolean => codexQuotaGuardExpandedTrafficModels.value.includes(model)
const toggleCodexQuotaGuardTrafficRow = async (model: string) => {
  const expanded = isCodexQuotaGuardTrafficExpanded(model)
  codexQuotaGuardExpandedTrafficModels.value = expanded
    ? codexQuotaGuardExpandedTrafficModels.value.filter((item) => item !== model)
    : [...codexQuotaGuardExpandedTrafficModels.value, model]
  if (!expanded && !codexQuotaGuardTrafficBreakdownByModel.value[model]) await loadCodexQuotaGuardTrafficBreakdown(model)
}

const quotaGuardAccountLabel = (accountId: number): string => {
  const account = codexQuotaGuardAccountById.value.get(accountId)
  return account ? `${account.name} · ${account.platform}/${account.type} · #${account.id}` : `#${accountId}`
}
const quotaGuardPolicyScopeLabel = (policy: QuotaGuardPolicyDraft): string => policy.account_ids.length ? `已选择 ${policy.account_ids.length} 个账号` : '全部账号'
const quotaGuardPolicySelectionText = (policy: QuotaGuardPolicyDraft): string => {
  if (!policy.account_ids.length) return '全部账号'
  const labels = policy.account_ids.slice(0, 3).map(quotaGuardAccountLabel)
  return `${labels.join('，')}${policy.account_ids.length > labels.length ? ` 等 ${policy.account_ids.length} 个` : ''}`
}
const filteredQuotaGuardAccounts = (policy: QuotaGuardPolicyDraft): Account[] => {
  const query = policy.account_query.trim().toLowerCase()
  if (!query) return codexQuotaGuardAccounts.value
  return codexQuotaGuardAccounts.value.filter((account) => [account.name, account.platform, account.type, String(account.id)].some((value) => value.toLowerCase().includes(query)))
}
const toggleQuotaGuardAccount = (policy: QuotaGuardPolicyDraft, accountId: number, event: Event) => {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  policy.account_ids = checked ? normalizeQuotaGuardAccountIDs([...policy.account_ids, accountId]) : policy.account_ids.filter((id) => id !== accountId)
}
const selectFilteredQuotaGuardAccounts = (policy: QuotaGuardPolicyDraft) => {
  policy.account_ids = normalizeQuotaGuardAccountIDs([...policy.account_ids, ...filteredQuotaGuardAccounts(policy).map((account) => account.id)])
}
const selectAllQuotaGuardAccounts = (policy: QuotaGuardPolicyDraft) => {
  policy.account_ids = normalizeQuotaGuardAccountIDs(codexQuotaGuardAccounts.value.map((account) => account.id))
}
const clearQuotaGuardAccounts = (policy: QuotaGuardPolicyDraft) => { policy.account_ids = [] }
const removeQuotaGuardAccount = (policy: QuotaGuardPolicyDraft, accountId: number) => { policy.account_ids = policy.account_ids.filter((id) => id !== accountId) }
const addCodexQuotaGuardPolicy = () => { codexQuotaGuardPolicies.value.push(createQuotaGuardPolicyDraft(codexQuotaGuardPolicies.value.length + 1)) }
const removeCodexQuotaGuardPolicy = (index: number) => {
  if (codexQuotaGuardPolicies.value.length > 1) codexQuotaGuardPolicies.value.splice(index, 1)
}

const startCodexQuotaGuardTask = async () => {
  codexQuotaGuardOperating.value = true
  try {
    codexQuotaGuardStatus.value = await startCodexQuotaGuard(
      {
        policies: codexQuotaGuardPolicies.value.map((policy): CodexQuotaGuardPolicy => ({
          id: policy.id.trim(),
          name: policy.name.trim(),
          enabled: policy.enabled,
          interval_seconds: policy.interval_seconds,
          account_ids: normalizeQuotaGuardAccountIDs(policy.account_ids),
          daily_spend_limit_usd: policy.daily_spend_limit_usd,
          daily_token_limit: policy.daily_token_limit,
          weekly_spend_limit_usd: policy.weekly_spend_limit_usd,
          weekly_token_limit: policy.weekly_token_limit,
          daily_spend_timezone: policy.daily_spend_timezone.trim() || 'Asia/Shanghai',
          dry_run: policy.dry_run
        }))
      },
      codexQuotaGuardAPIKey.value
    )
    codexQuotaGuardAPIKey.value = ''
    await loadCodexQuotaGuardTraffic()
    appStore.showSuccess('上游额度保护已启动')
  } catch (error) {
    appStore.showError(errorMessage(error, '启动保护器失败'))
  } finally {
    codexQuotaGuardOperating.value = false
  }
}

const stopCodexQuotaGuardTask = async () => {
  codexQuotaGuardOperating.value = true
  try {
    codexQuotaGuardStatus.value = await stopCodexQuotaGuard()
    appStore.showSuccess('上游额度保护已停止')
  } catch (error) {
    appStore.showError(errorMessage(error, '停止保护器失败'))
  } finally {
    codexQuotaGuardOperating.value = false
  }
}

const scanCodexQuotaGuardTask = async () => {
  codexQuotaGuardOperating.value = true
  try {
    codexQuotaGuardLastAction.value = await scanCodexQuotaGuard()
    await Promise.all([loadCodexQuotaGuardStatus(), loadCodexQuotaGuardTraffic()])
    appStore.showSuccess('已完成一次扫描')
  } catch (error) {
    appStore.showError(errorMessage(error, '扫描失败'))
  } finally {
    codexQuotaGuardOperating.value = false
  }
}

const releaseCodexQuotaGuardTask = async () => {
  codexQuotaGuardOperating.value = true
  try {
    codexQuotaGuardLastAction.value = await releaseCodexQuotaGuard()
    await Promise.all([loadCodexQuotaGuardStatus(), loadCodexQuotaGuardTraffic()])
    appStore.showSuccess('已释放 Guard 封禁账号')
  } catch (error) {
    appStore.showError(errorMessage(error, '释放失败'))
  } finally {
    codexQuotaGuardOperating.value = false
  }
}

onMounted(() => {
  void Promise.all([loadCodexQuotaGuardAccounts(), loadCodexQuotaGuardStatus(), loadCodexQuotaGuardTraffic()])
})
</script>

<style scoped>
.tools-surface { @apply min-w-0 rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900/70; }
.tools-section-header { @apply mb-4 flex min-w-0 flex-col gap-3 border-b border-gray-100 pb-4 dark:border-dark-800 lg:flex-row lg:items-start lg:justify-between; }
.tools-title { @apply text-base font-semibold text-gray-900 dark:text-white; }
.tools-description { @apply mt-1 text-sm text-gray-500 dark:text-dark-400; }
.tools-panel { @apply rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/40; }
.tools-badges { @apply flex flex-wrap items-center gap-2; }
.tools-badge { @apply inline-flex items-center rounded-full border border-gray-200 bg-gray-50 px-2.5 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300; }
.tools-empty { @apply flex min-h-[220px] items-center justify-center px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400; }
.tools-th { @apply whitespace-nowrap px-3 py-2 text-left font-semibold; }
.tools-td { @apply whitespace-nowrap px-3 py-2 align-middle text-gray-700 dark:text-dark-200; }
.tools-readout { @apply flex min-h-[38px] items-center rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200; }
.quota-account-select { @apply relative; }
.quota-account-trigger { @apply flex min-h-[42px] cursor-pointer list-none items-center justify-between gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 shadow-sm transition-colors hover:border-gray-400 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200 dark:hover:border-dark-500; }
.quota-account-trigger::-webkit-details-marker { display: none; }
.quota-account-menu { @apply absolute left-0 right-0 top-full z-30 mt-2 rounded-lg border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-700 dark:bg-dark-900; }
.status-ok { @apply rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300; }
.status-error { @apply rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium text-red-700 dark:bg-red-950/40 dark:text-red-300; }
.status-muted { @apply rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300; }
</style>
