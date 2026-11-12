<template>
  <div
    v-if="open"
    class="fixed inset-0 z-[10000] flex items-center justify-center bg-black/50 p-4"
    @click.self="$emit('close')"
  >
    <div class="max-h-[85vh] w-full max-w-xl overflow-y-auto rounded-lg bg-white p-5 shadow-xl dark:bg-dark-800">
      <div class="mb-3 flex items-center justify-between">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.cnProviders.zhipuResetCards.title') }}
          <span class="ml-1 text-xs font-normal text-gray-500">{{ account?.name }}</span>
        </h3>
        <button class="text-gray-400 hover:text-gray-600" @click="$emit('close')">✕</button>
      </div>

      <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.cnProviders.zhipuResetCards.hint') }}
      </p>

      <div v-if="error" class="mb-3 rounded border border-red-200 bg-red-50 p-2 text-xs text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
        {{ error }}
      </div>

      <!-- 窗口用量 + 刷新 -->
      <div v-if="cards" class="mb-4">
        <div class="mb-2 flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
          <span>
            {{ t('admin.accounts.cnProviders.zhipuResetCards.lastUsed') }}:
            5h {{ cards.lastFiveHourResetTime || '—' }} ·
            {{ t('admin.accounts.cnProviders.zhipuResetCards.week') }} {{ cards.lastWeekResetTime || '—' }}
          </span>
          <button
            class="text-blue-600 hover:underline disabled:opacity-50"
            :disabled="loading"
            @click="refresh()"
          >
            {{ t('admin.accounts.cnProviders.zhipuResetCards.refresh') }}
          </button>
        </div>

        <div
          v-for="group in [
            { key: '5h', list: cards.fiveHourCards },
            { key: 'week', list: cards.weekCards },
          ]"
          :key="group.key"
          class="mb-3"
        >
          <div class="mb-1 flex items-center justify-between">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ group.key === '5h' ? t('admin.accounts.cnProviders.zhipuResetCards.fiveHourCards') : t('admin.accounts.cnProviders.zhipuResetCards.weekCards') }}
              ({{ availableCount(group.list) }})
            </span>
            <button
              v-if="availableCount(group.list) > 0"
              class="rounded bg-amber-500 px-2 py-0.5 text-xs font-medium text-white hover:bg-amber-600 disabled:opacity-50"
              :disabled="using"
              @click="useCard(group.key as '5h' | 'week', 0)"
            >
              {{ t('admin.accounts.cnProviders.zhipuResetCards.useEarliest') }}
            </button>
          </div>
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b text-left text-gray-500 dark:border-dark-600 dark:text-gray-400">
                <th class="py-1 pr-2 font-medium">ID</th>
                <th class="py-1 pr-2 font-medium">{{ t('admin.accounts.cnProviders.zhipuResetCards.grantType') }}</th>
                <th class="py-1 pr-2 font-medium">{{ t('admin.accounts.cnProviders.zhipuResetCards.expires') }}</th>
                <th class="py-1 font-medium">{{ t('admin.accounts.cnProviders.zhipuResetCards.status') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="c in group.list"
                :key="c.recordId"
                class="border-b last:border-0 dark:border-dark-700"
                :class="{ 'opacity-50': c.expired || !c.available }"
              >
                <td class="py-1 pr-2 font-mono">{{ c.recordId }}</td>
                <td class="py-1 pr-2">{{ c.grantType }}</td>
                <td class="py-1 pr-2">{{ c.expireTime }}</td>
                <td class="py-1">
                  <span v-if="c.expired" class="text-gray-400">{{ t('admin.accounts.cnProviders.zhipuResetCards.expired') }}</span>
                  <span v-else-if="!c.available" class="text-gray-400">{{ t('admin.accounts.cnProviders.zhipuResetCards.used') }}</span>
                  <span v-else-if="c.expiringInH <= 48" class="font-medium text-amber-600 dark:text-amber-400">
                    {{ t('admin.accounts.cnProviders.zhipuResetCards.expiringIn', { h: c.expiringInH }) }}
                  </span>
                  <span v-else class="text-emerald-600 dark:text-emerald-400">{{ t('admin.accounts.cnProviders.zhipuResetCards.ready') }}</span>
                  <button
                    v-if="!c.expired && c.available"
                    class="ml-2 text-blue-600 hover:underline disabled:opacity-50"
                    :disabled="using"
                    @click="useCard(c.window, c.recordId)"
                  >
                    {{ t('admin.accounts.cnProviders.zhipuResetCards.use') }}
                  </button>
                </td>
              </tr>
              <tr v-if="!group.list.length">
                <td colspan="4" class="py-2 text-center text-gray-400">{{ t('admin.accounts.cnProviders.zhipuResetCards.none') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-else class="flex items-center justify-center py-8">
        <svg class="h-5 w-5 animate-spin text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { ZhipuResetCardView, ZhipuResetCardsResult } from '@/api/admin/cnProviders'
import type { Account } from '@/types'

const props = defineProps<{
  open: boolean
  account: Account | null
}>()

defineEmits<{ close: [] }>()

const { t } = useI18n()
const loading = ref(false)
const using = ref(false)
const error = ref<string | null>(null)
const cards = ref<ZhipuResetCardsResult | null>(null)

const availableCount = (list: ZhipuResetCardView[]) =>
  list.filter((c) => c.available && !c.expired).length

const extractErrorMessage = (e: unknown): string => {
  if (e && typeof e === 'object' && 'response' in e) {
    const resp = (e as { response?: { data?: { message?: string; error?: string } } }).response?.data
    return resp?.message || resp?.error || String(e)
  }
  return e instanceof Error ? e.message : String(e)
}

const refresh = async () => {
  if (!props.account) return
  loading.value = true
  error.value = null
  try {
    cards.value = await adminAPI.cnProviders.queryZhipuResetCards(props.account.id)
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    loading.value = false
  }
}

const useCard = async (cardWindow: '5h' | 'week', recordId: number) => {
  if (!props.account) return
  if (recordId === 0 && !globalThis.confirm(t('admin.accounts.cnProviders.zhipuResetCards.useEarliestConfirm'))) return
  if (recordId !== 0 && !globalThis.confirm(t('admin.accounts.cnProviders.zhipuResetCards.useConfirm'))) return
  error.value = null
  try {
    cards.value = await adminAPI.cnProviders.useZhipuResetCard(props.account.id, cardWindow, recordId)
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    using.value = false
  }
}

watch(
  () => props.open,
  (open) => {
    if (open && props.account) {
      cards.value = null
      error.value = null
      void refresh()
    }
  }
)
</script>
