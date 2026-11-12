<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl px-4 py-10">
      <h1 class="mb-2 text-2xl font-bold text-gray-900 dark:text-white">
        {{ t('admin.tools.title') }}
      </h1>
      <p class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.tools.emptyHint') }}
      </p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
</script>
