<template>
  <section class="tools-surface">
    <div class="tools-section-header">
      <div>
        <h2 class="tools-title">账号计费策略</h2>
        <p class="tools-description">策略跟随实际选中的上游账号。贵者计费会比较请求模型、渠道映射模型和账号最终映射模型的 IQ 价格。</p>
      </div>
      <div class="tools-badges">
        <span class="tools-badge">账号 {{ accounts.length }}</span>
        <span class="tools-badge">已选 {{ selectedAccountIds.length }}</span>
      </div>
    </div>

    <div v-if="billingError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
      {{ billingError }}
    </div>

    <div class="grid grid-cols-1 gap-4 2xl:grid-cols-[minmax(0,0.82fr)_minmax(0,1.18fr)]">
      <div class="tools-panel space-y-4">
        <div>
          <label class="input-label">批量设置计费策略</label>
          <select v-model="selectedStrategy" class="input">
            <option v-for="strategy in billingStrategies" :key="strategy.value" :value="strategy.value">
              {{ strategy.label }}
            </option>
          </select>
          <p class="input-hint">{{ strategyHint(selectedStrategy) }}</p>
        </div>

        <div>
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <label class="input-label mb-0">目标账号</label>
            <span class="text-xs text-gray-500 dark:text-dark-400">{{ selectedAccountIds.length }} 个</span>
          </div>
          <details class="account-multi-select">
            <summary class="account-multi-trigger">
              <span class="min-w-0 truncate">{{ accountSelectionText }}</span>
              <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400" />
            </summary>
            <div class="account-multi-menu">
              <input v-model="accountQuery" class="input" type="search" placeholder="搜索账号名称、平台、分组或 ID" />
              <div class="mt-2 flex flex-wrap gap-2">
                <button type="button" class="btn btn-secondary btn-sm" :disabled="filteredAccounts.length === 0" @click="selectFilteredAccounts">
                  选择当前结果
                </button>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="accounts.length === 0" @click="selectAllAccounts">
                  全选账号
                </button>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="selectedAccountIds.length === 0" @click="clearAccountSelection">
                  清空
                </button>
              </div>
              <div class="mt-2 max-h-72 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
                <div v-if="accountsLoading" class="tools-empty min-h-[100px]">正在加载账号...</div>
                <div v-else-if="filteredAccounts.length === 0" class="tools-empty min-h-[100px]">没有匹配的账号。</div>
                <label
                  v-for="account in filteredAccounts"
                  v-else
                  :key="account.id"
                  class="flex min-w-0 cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2 text-sm last:border-b-0 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-800/70"
                >
                  <input
                    :checked="selectedAccountIds.includes(account.id)"
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    @change="toggleAccountSelection(account.id, $event)"
                  />
                  <span class="min-w-0 flex-1">
                    <span class="block truncate font-medium text-gray-900 dark:text-white">{{ account.name }}</span>
                    <span class="block truncate text-xs text-gray-500 dark:text-dark-400">#{{ account.id }} · {{ account.platform }} · {{ accountGroupText(account) }}</span>
                  </span>
                  <span class="shrink-0 text-xs text-gray-500 dark:text-dark-400">{{ strategyLabel(accountStrategy(account)) }}</span>
                </label>
              </div>
            </div>
          </details>
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn btn-primary"
            :disabled="accountsSaving || selectedAccountIds.length === 0"
            @click="applyBillingStrategy"
          >
            <Icon name="check" size="sm" />
            {{ accountsSaving ? '保存中...' : '应用到选中账号' }}
          </button>
          <button type="button" class="btn btn-secondary" :disabled="accountsLoading || accountsSaving" @click="loadAccounts">
            <Icon name="refresh" size="sm" :class="accountsLoading ? 'animate-spin' : ''" />
            刷新账号
          </button>
        </div>
      </div>

      <div class="min-w-0">
        <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">当前账号策略</h3>
            <p class="text-xs text-gray-500 dark:text-dark-400">账号设置优先于渠道设置；选择“跟随渠道/默认”即可恢复原有行为。</p>
          </div>
          <span v-if="maxCostAccountCount > 0" class="tools-badge text-emerald-700 dark:text-emerald-300">贵者计费 {{ maxCostAccountCount }}</span>
        </div>

        <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
          <div v-if="accountsLoading" class="tools-empty min-h-[180px]">正在加载账号...</div>
          <div v-else-if="accounts.length === 0" class="tools-empty min-h-[180px]">暂无账号。</div>
          <div v-else class="overflow-x-auto">
            <table class="min-w-[900px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
              <thead class="bg-gray-50 text-xs font-semibold text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
                <tr>
                  <th class="tools-th">账号</th>
                  <th class="tools-th">平台/分组</th>
                  <th class="tools-th">状态</th>
                  <th class="tools-th">当前计费策略</th>
                  <th class="tools-th text-right">操作</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/30">
                <tr v-for="account in accounts" :key="account.id" class="hover:bg-gray-50 dark:hover:bg-dark-800/60">
                  <td class="tools-td">
                    <div class="font-medium text-gray-900 dark:text-white">{{ account.name }}</div>
                    <div class="text-xs text-gray-500 dark:text-dark-400">#{{ account.id }}</div>
                  </td>
                  <td class="tools-td">
                    <div class="text-gray-700 dark:text-dark-200">{{ account.platform }}</div>
                    <div class="max-w-[240px] truncate text-xs text-gray-500 dark:text-dark-400">{{ accountGroupText(account) }}</div>
                  </td>
                  <td class="tools-td">
                    <span :class="account.status === 'active' ? 'status-ok' : 'status-muted'">
                      {{ account.status === 'active' ? '启用' : account.status === 'error' ? '错误' : '停用' }}
                    </span>
                  </td>
                  <td class="tools-td">
                    <span :class="accountStrategy(account) === 'max_cost' ? 'status-ok' : 'text-gray-700 dark:text-dark-200'">
                      {{ strategyLabel(accountStrategy(account)) }}
                    </span>
                  </td>
                  <td class="tools-td text-right">
                    <button type="button" class="btn btn-secondary btn-sm" @click="selectSingleAccount(account.id)">
                      <Icon name="check" size="sm" />
                      选择
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <div class="mt-5 border-t border-gray-200 pt-5 dark:border-dark-700">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">贵者计费审计</h3>
          <p class="text-xs text-gray-500 dark:text-dark-400">这里单独展示候选模型的原始费用、胜出模型、用户扣费和账号成本，不改变原有用量页面。</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <select v-model="auditAccountID" class="input min-w-[180px]" aria-label="按账号筛选贵者计费审计">
            <option value="">全部账号</option>
            <option v-for="account in accounts" :key="account.id" :value="account.id">
              {{ account.name }} (#{{ account.id }})
            </option>
          </select>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="auditsLoading" @click="loadAudits">
            <Icon name="refresh" size="sm" :class="auditsLoading ? 'animate-spin' : ''" />
            刷新审计
          </button>
        </div>
      </div>

      <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
        <div v-if="auditsLoading" class="tools-empty min-h-[150px]">正在加载贵者计费审计...</div>
        <div v-else-if="auditRecords.length === 0" class="tools-empty min-h-[150px]">暂无贵者计费记录。</div>
        <div v-else class="overflow-x-auto">
          <table class="min-w-[1120px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-xs font-semibold text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
              <tr>
                <th class="tools-th">时间 / 账号</th>
                <th class="tools-th">映射链</th>
                <th class="tools-th">胜出模型</th>
                <th class="tools-th text-right">原始 TotalCost</th>
                <th class="tools-th text-right">用户扣费</th>
                <th class="tools-th text-right">账号成本</th>
                <th class="tools-th text-right">倍率</th>
                <th class="tools-th text-right">明细</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/30">
              <template v-for="audit in auditRecords" :key="auditKey(audit)">
                <tr class="hover:bg-gray-50 dark:hover:bg-dark-800/60">
                  <td class="tools-td">
                    <div class="text-xs text-gray-500 dark:text-dark-400">{{ formatAuditTime(audit.created_at) }}</div>
                    <div class="font-medium text-gray-900 dark:text-white">{{ auditAccountLabel(audit.account_id) }}</div>
                    <div class="tools-code mt-1 max-w-[190px] truncate">{{ audit.requested_model }}</div>
                  </td>
                  <td class="tools-td max-w-[260px]">
                    <div class="truncate text-gray-800 dark:text-dark-100">{{ audit.mapping_chain || audit.requested_model }}</div>
                    <div class="text-xs text-gray-500 dark:text-dark-400">{{ audit.candidates.length }} 个候选</div>
                  </td>
                  <td class="tools-td">
                    <span class="font-semibold text-emerald-700 dark:text-emerald-300">{{ audit.selected_model }}</span>
                  </td>
                  <td class="tools-td text-right font-mono text-xs">{{ formatCost(audit.total_cost) }}</td>
                  <td class="tools-td text-right font-mono text-xs font-semibold">{{ formatCost(audit.actual_cost) }}</td>
                  <td class="tools-td text-right font-mono text-xs">{{ formatCost(audit.account_billed_cost) }}</td>
                  <td class="tools-td text-right text-xs">
                    <div>用户 {{ formatMultiplier(audit.group_rate_multiplier) }}</div>
                    <div class="text-gray-500 dark:text-dark-400">账号 {{ formatMultiplier(audit.account_rate_multiplier) }}</div>
                  </td>
                  <td class="tools-td text-right">
                    <button type="button" class="btn btn-secondary btn-sm" @click="toggleAudit(audit)">
                      <Icon :name="isAuditExpanded(audit) ? 'chevronUp' : 'chevronDown'" size="sm" />
                      {{ isAuditExpanded(audit) ? '收起' : '查看' }}
                    </button>
                  </td>
                </tr>
                <tr v-if="isAuditExpanded(audit)" class="bg-gray-50/70 dark:bg-dark-900/60">
                  <td colspan="8" class="px-3 py-3">
                    <div class="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-600 dark:text-dark-300">
                      <span>输入 {{ audit.input_tokens }}</span>
                      <span>输出 {{ audit.output_tokens }}</span>
                      <span>缓存写 {{ audit.cache_creation_tokens }}</span>
                      <span>缓存读 {{ audit.cache_read_tokens }}</span>
                      <span v-if="audit.image_count > 0">图片 {{ audit.image_count }}</span>
                      <span>账号统计基准 {{ formatCost(audit.account_stats_cost ?? audit.total_cost) }}</span>
                    </div>
                    <div class="overflow-x-auto rounded border border-gray-200 dark:border-dark-700">
                      <table class="min-w-[940px] divide-y divide-gray-200 text-xs dark:divide-dark-700">
                        <thead class="bg-white text-gray-500 dark:bg-dark-900 dark:text-dark-400">
                          <tr>
                            <th class="tools-th">候选模型</th>
                            <th class="tools-th">价格来源</th>
                            <th class="tools-th">状态</th>
                            <th class="tools-th text-right">输入</th>
                            <th class="tools-th text-right">输出</th>
                            <th class="tools-th text-right">缓存</th>
                            <th class="tools-th text-right">TotalCost</th>
                          </tr>
                        </thead>
                        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-900 dark:divide-dark-800">
                          <tr v-for="candidate in audit.candidates" :key="`${auditKey(audit)}-${candidate.model}`">
                            <td class="tools-td font-mono">{{ candidate.model }}</td>
                            <td class="tools-td">{{ pricingSourceLabel(candidate.pricing_source) }}</td>
                            <td class="tools-td">
                              <span v-if="candidate.available" class="status-ok">可计费</span>
                              <span v-else class="status-error" :title="candidate.error">无价格</span>
                            </td>
                            <td class="tools-td text-right font-mono">{{ formatCost(candidate.input_cost + candidate.image_input_cost) }}</td>
                            <td class="tools-td text-right font-mono">{{ formatCost(candidate.output_cost + candidate.image_output_cost) }}</td>
                            <td class="tools-td text-right font-mono">{{ formatCost(candidate.cache_write_cost + candidate.cache_read_cost) }}</td>
                            <td class="tools-td text-right font-mono font-semibold">{{ formatCost(candidate.total_cost) }}</td>
                          </tr>
                        </tbody>
                      </table>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import { useAppStore } from '@/stores/app'
import { getBillingAudits, type BillingAuditRecord } from './api'

type BillingStrategy = 'inherit' | 'channel_mapped' | 'requested' | 'upstream' | 'max_cost'

const billingStrategies: Array<{ value: BillingStrategy; label: string; hint: string }> = [
  { value: 'inherit', label: '跟随渠道/默认', hint: '账号不覆盖策略；有渠道时沿用渠道设置，没有渠道时按系统默认模型计费。' },
  { value: 'channel_mapped', label: '按渠道映射后模型计费', hint: '使用渠道映射后的模型查找 IQ 平台价格。' },
  { value: 'requested', label: '按请求模型计费', hint: '使用用户请求的源模型查找 IQ 平台价格。' },
  { value: 'upstream', label: '按最终上游模型计费', hint: '使用账号最终发送给上游的模型查找 IQ 平台价格。' },
  { value: 'max_cost', label: '贵者计费', hint: '比较请求、渠道映射和账号最终上游模型的 IQ 价格，选择同一次请求中更贵的模型。' }
]

const appStore = useAppStore()
const accounts = ref<Account[]>([])
const selectedAccountIds = ref<number[]>([])
const selectedStrategy = ref<BillingStrategy>('max_cost')
const accountQuery = ref('')
const accountsLoading = ref(false)
const accountsSaving = ref(false)
const billingError = ref('')
const auditAccountID = ref<number | ''>('')
const auditRecords = ref<BillingAuditRecord[]>([])
const auditsLoading = ref(false)
const expandedAuditKeys = ref<Set<string>>(new Set())

const accountGroupText = (account: Account): string => {
  const names = (account.groups || []).map((group) => group.name).filter(Boolean)
  if (names.length > 0) return names.join('、')
  if ((account.group_ids || []).length > 0) return (account.group_ids || []).map((id) => `#${id}`).join('、')
  return '未绑定分组'
}

const accountStrategy = (account: Account): BillingStrategy => {
  const value = account.extra?.billing_model_source
  if (typeof value === 'string' && billingStrategies.some((strategy) => strategy.value === value)) {
    return value as BillingStrategy
  }
  return 'inherit'
}

const filteredAccounts = computed(() => {
  const query = accountQuery.value.trim().toLowerCase()
  if (!query) return accounts.value
  return accounts.value.filter((account) => (
    account.name.toLowerCase().includes(query) ||
    account.platform.toLowerCase().includes(query) ||
    String(account.id).includes(query) ||
    accountGroupText(account).toLowerCase().includes(query)
  ))
})

const selectedAccountLabelList = computed(() => selectedAccountIds.value
  .slice(0, 2)
  .map((id) => accounts.value.find((account) => account.id === id)?.name || `#${id}`))

const accountSelectionText = computed(() => {
  if (selectedAccountIds.value.length === 0) return '请选择账号'
  const suffix = selectedAccountIds.value.length > selectedAccountLabelList.value.length
    ? ` 等 ${selectedAccountIds.value.length} 个`
    : ''
  return `${selectedAccountLabelList.value.join('，')}${suffix}`
})

const maxCostAccountCount = computed(() => accounts.value.filter((account) => accountStrategy(account) === 'max_cost').length)

const strategyHint = (strategy: BillingStrategy): string => billingStrategies.find((item) => item.value === strategy)?.hint || ''

const strategyLabel = (strategy: unknown): string => billingStrategies.find((item) => item.value === String(strategy))?.label || '跟随渠道/默认'

const auditKey = (audit: BillingAuditRecord): string => `${audit.request_id || audit.selected_model}-${audit.created_at}`

const auditAccountLabel = (accountID: number): string => accounts.value.find((account) => account.id === accountID)?.name || `账号 #${accountID}`

const formatCost = (value: number | undefined): string => {
  const normalized = Number.isFinite(value) ? Number(value) : 0
  return `$${normalized.toFixed(6)}`
}

const formatMultiplier = (value: number | undefined): string => {
  const normalized = Number.isFinite(value) ? Number(value) : 1
  return `${normalized.toFixed(2)}x`
}

const formatAuditTime = (value: string): string => {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

const pricingSourceLabel = (source: string | undefined): string => {
  switch (source) {
    case 'channel': return '渠道自定义'
    case 'litellm': return 'IQ/LiteLLM'
    case 'fallback': return '内置回退'
    default: return source || '未解析'
  }
}

const isAuditExpanded = (audit: BillingAuditRecord): boolean => expandedAuditKeys.value.has(auditKey(audit))

const toggleAudit = (audit: BillingAuditRecord) => {
  const key = auditKey(audit)
  const next = new Set(expandedAuditKeys.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expandedAuditKeys.value = next
}

const errorMessage = (error: unknown, fallback: string): string => {
  if (error && typeof error === 'object') {
    const maybe = error as { message?: unknown; response?: { data?: { detail?: unknown; message?: unknown } } }
    const detail = maybe.response?.data?.detail || maybe.response?.data?.message || maybe.message
    if (typeof detail === 'string' && detail.trim()) return detail
  }
  return fallback
}

const normalizeSelectedAccountIds = (ids: number[]): number[] => [...new Set(ids)].filter((id) => Number.isInteger(id) && id > 0)

const loadAccounts = async () => {
  accountsLoading.value = true
  billingError.value = ''
  try {
    const result = await adminAPI.accounts.list(1, 1000, { sort_by: 'id', sort_order: 'asc' })
    accounts.value = result.items || []
    const availableIds = new Set(accounts.value.map((account) => account.id))
    selectedAccountIds.value = selectedAccountIds.value.filter((id) => availableIds.has(id))
  } catch (error) {
    accounts.value = []
    billingError.value = errorMessage(error, '加载账号列表失败。')
  } finally {
    accountsLoading.value = false
  }
}

const loadAudits = async () => {
  auditsLoading.value = true
  try {
    auditRecords.value = await getBillingAudits(auditAccountID.value === '' ? undefined : auditAccountID.value)
  } catch (error) {
    billingError.value = errorMessage(error, '加载贵者计费审计失败。')
  } finally {
    auditsLoading.value = false
  }
}

const toggleAccountSelection = (accountId: number, event: Event) => {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  selectedAccountIds.value = checked
    ? normalizeSelectedAccountIds([...selectedAccountIds.value, accountId])
    : selectedAccountIds.value.filter((id) => id !== accountId)
}

const selectFilteredAccounts = () => {
  selectedAccountIds.value = normalizeSelectedAccountIds([
    ...selectedAccountIds.value,
    ...filteredAccounts.value.map((account) => account.id)
  ])
}

const selectAllAccounts = () => {
  selectedAccountIds.value = normalizeSelectedAccountIds(accounts.value.map((account) => account.id))
}

const clearAccountSelection = () => {
  selectedAccountIds.value = []
}

const selectSingleAccount = (accountId: number) => {
  selectedAccountIds.value = [accountId]
}

const applyBillingStrategy = async () => {
  if (selectedAccountIds.value.length === 0) return

  accountsSaving.value = true
  billingError.value = ''
  try {
    const result = await adminAPI.accounts.bulkUpdate(selectedAccountIds.value, {
      extra: { billing_model_source: selectedStrategy.value }
    })
    await loadAccounts()
    if (result.failed > 0) {
      billingError.value = `${result.failed} 个账号保存失败，请检查权限或重试。`
      return
    }
    appStore.showSuccess(`已将“${strategyLabel(selectedStrategy.value)}”应用到 ${result.success} 个账号。`)
  } catch (error) {
    billingError.value = errorMessage(error, '保存账号计费策略失败。')
  } finally {
    accountsSaving.value = false
  }
}

onMounted(() => {
  void loadAccounts()
  void loadAudits()
})

watch(auditAccountID, () => {
  void loadAudits()
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
