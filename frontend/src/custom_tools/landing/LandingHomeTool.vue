<template>
  <section class="tools-surface">
    <div class="tools-section-header">
      <div>
        <h2 class="tools-title">Claude 风格首页</h2>
        <p class="tools-description">
          仅插件层实现：左侧调参数、右侧预览；满意后把渲染好的 HTML 写入站点设置 home_content（不改上游路由/CSP）。
        </p>
      </div>
      <button type="button" class="btn btn-secondary" @click="resetParams">
        <Icon name="refresh" size="sm" />
        重置参数
      </button>
    </div>

    <div class="tools-panel space-y-3">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">URL 参数说明</h3>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-dark-400">
          参数会直接改页面文案/样式/区块显示。调试时改左侧表单即可；也可用 query 导入导出同一套配置。
        </p>
      </div>

      <div class="rounded-lg bg-gray-50 p-3 font-mono text-[11px] leading-relaxed text-gray-700 dark:bg-dark-800/80 dark:text-dark-200">
        /custom/landing.html?site_name=MyAPI&amp;eyebrow=你好&amp;accent=%23c96442&amp;hide=pain,compare&amp;use_settings=0
      </div>

      <div class="overflow-x-auto">
        <table class="w-full min-w-[520px] text-left text-xs">
          <thead>
            <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400">
              <th class="py-2 pr-3 font-medium">参数</th>
              <th class="py-2 pr-3 font-medium">作用</th>
              <th class="py-2 font-medium">示例</th>
            </tr>
          </thead>
          <tbody class="align-top text-gray-700 dark:text-dark-200">
            <tr v-for="row in paramDocs" :key="row.key" class="border-b border-gray-100 dark:border-dark-800">
              <td class="py-2 pr-3 font-mono text-[11px] text-primary-700 dark:text-primary-300">{{ row.key }}</td>
              <td class="py-2 pr-3">{{ row.desc }}</td>
              <td class="py-2 font-mono text-[11px] text-gray-500 dark:text-dark-400">{{ row.example }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="text-xs text-gray-500 dark:text-dark-400">
        常用：
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">site_name</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">site_subtitle</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">eyebrow</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">lede</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">cta_primary</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">tag1..3</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">accent</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">hide</code>
        /
        <code class="rounded bg-gray-100 px-1 dark:bg-dark-800">use_settings=0</code>
      </p>
    </div>

    <div class="grid gap-4 xl:grid-cols-[minmax(320px,0.95fr)_minmax(0,1.25fr)]">
      <div class="min-w-0 space-y-4">
        <div class="tools-panel space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">调试参数</h3>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="loadingSettings" @click="loadFromPublicSettings">
              <Icon name="cloud" size="sm" :class="loadingSettings ? 'animate-spin' : ''" />
              填入公开设置
            </button>
          </div>

          <div class="grid gap-3 sm:grid-cols-2">
            <div v-for="field in textFields" :key="field.key" :class="field.span === 2 ? 'sm:col-span-2' : ''">
              <label class="input-label">
                {{ field.label }}
                <span class="font-mono text-[10px] text-gray-400">{{ field.key }}</span>
              </label>
              <input
                v-if="field.type !== 'textarea'"
                v-model="params[field.key]"
                class="input"
                type="text"
                :placeholder="field.placeholder || defaults[field.key]"
              />
              <textarea
                v-else
                v-model="params[field.key]"
                class="input min-h-[72px] resize-y text-sm"
                :placeholder="field.placeholder || defaults[field.key]"
              />
            </div>
          </div>

          <div>
            <label class="input-label">隐藏区块 <span class="font-mono text-[10px] text-gray-400">hide</span></label>
            <div class="flex flex-wrap gap-3">
              <label
                v-for="section in hideSections"
                :key="section.id"
                class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200"
              >
                <input
                  v-model="hiddenSet"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                  :value="section.id"
                />
                {{ section.label }}
              </label>
            </div>
          </div>
        </div>

        <div class="tools-panel space-y-3">
          <label class="input-label">参数 URL（便于分享/备份配置）</label>
          <textarea class="input min-h-[72px] resize-y font-mono text-xs leading-relaxed" readonly :value="paramURL" />
          <div class="flex min-w-0 flex-col gap-2 sm:flex-row">
            <input v-model="importRaw" class="input font-mono text-xs" type="text" placeholder="粘贴带 ?site_name=... 的 URL 或 query" />
            <button type="button" class="btn btn-secondary shrink-0" :disabled="!importRaw.trim()" @click="importFromURL">
              从 URL 导入
            </button>
          </div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary" @click="copyParamURL">
              <Icon name="copy" size="sm" />
              {{ copied ? '已复制' : '复制参数 URL' }}
            </button>
            <button type="button" class="btn btn-primary" :disabled="applying" @click="applyHTMLToHomeContent">
              <Icon name="check" size="sm" />
              {{ applying ? '写入中...' : '写入 HTML 到 home_content' }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="clearing" @click="clearHomeContent">
              <Icon name="trash" size="sm" />
              {{ clearing ? '恢复中...' : '恢复默认首页' }}
            </button>
          </div>
          <p class="input-hint">
            插件边界内不改 CSP/静态路由，因此正式生效走 home_content 的 HTML 模式（不是同域 iframe URL）。参数 URL 只用于调试与备份。
          </p>
          <p
            v-if="message"
            class="text-sm"
            :class="error ? 'text-red-600 dark:text-red-300' : 'text-emerald-700 dark:text-emerald-300'"
          >
            {{ message }}
          </p>
        </div>
      </div>

      <div class="tools-panel overflow-hidden p-0">
        <div
          class="flex items-center justify-between gap-2 border-b border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400"
        >
          <span>实时预览（srcdoc）</span>
          <span class="font-mono">custom_tools/landing</span>
        </div>
        <iframe class="block h-[78vh] min-h-[640px] w-full border-0 bg-white" title="Landing preview" :srcdoc="previewHTML" />
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import {
  LANDING_DEFAULTS,
  LANDING_HIDE_SECTIONS,
  LANDING_TEXT_FIELDS,
  buildLandingQuery,
  createLandingParams,
  parseLandingQuery,
} from './params'
import { renderLandingDocument, renderLandingFragment } from './render'

const appStore = useAppStore()
const defaults = LANDING_DEFAULTS
const textFields = LANDING_TEXT_FIELDS
const hideSections = LANDING_HIDE_SECTIONS

const paramDocs = [
  { key: 'site_name', desc: '站点名称（标题/导航品牌）', example: 'MyAPI' },
  { key: 'site_subtitle', desc: '副标题', example: 'AI API Gateway' },
  { key: 'site_logo', desc: 'Logo 图片 URL', example: 'https://.../logo.png' },
  { key: 'doc_url', desc: '文档链接（有值才显示）', example: 'https://docs.example.com' },
  { key: 'eyebrow', desc: '主标题上方眉标', example: '你好' },
  { key: 'lede', desc: '介绍文案', example: '一站式接入多模型…' },
  { key: 'cta_primary', desc: '主按钮文案', example: '立即开始' },
  { key: 'cta_secondary', desc: '次按钮文案', example: '免费注册' },
  { key: 'login_text', desc: '右上角登录文案', example: '登录' },
  { key: 'tag1 / tag2 / tag3', desc: 'Hero 下方三个标签；空值会隐藏', example: '订阅转 API' },
  { key: 'accent', desc: '强调色（#RGB / #RRGGBB，URL 里写 %23）', example: '%23c96442' },
  { key: 'hide', desc: '隐藏区块，逗号分隔：pain,features,compare,providers,cta', example: 'pain,compare' },
  { key: 'use_settings', desc: '设为 0 时不合并公开站点设置（调试推荐）', example: '0' },
  { key: 'bubble_user / bubble_ai', desc: '右侧对话气泡；bubble_ai 支持 HTML', example: 'Ready.' },
  { key: 'pain_title / features_title / …', desc: '各区块标题与说明文案', example: '我们帮你解决' },
  { key: 'cta_title / cta_desc / cta_button', desc: '底部 CTA 文案', example: '准备好开始了吗？' },
] as const

const params = reactive<Record<string, string>>(createLandingParams())
const hiddenSet = ref<string[]>([])
const importRaw = ref('')
const previewHTML = ref('')
const copied = ref(false)
const applying = ref(false)
const clearing = ref(false)
const loadingSettings = ref(false)
const message = ref('')
const error = ref(false)

let copyTimer: ReturnType<typeof setTimeout> | null = null
let previewTimer: ReturnType<typeof setTimeout> | null = null

const queryString = computed(() => buildLandingQuery(params, hiddenSet.value))
const paramURL = computed(() => {
  const qs = queryString.value
  return `${window.location.origin}/custom/landing.html${qs ? `?${qs}` : ''}`
})

watch(
  [params, hiddenSet],
  () => {
    if (previewTimer) clearTimeout(previewTimer)
    previewTimer = setTimeout(() => {
      previewHTML.value = renderLandingDocument({ ...params }, [...hiddenSet.value])
    }, 220)
  },
  { deep: true, immediate: true },
)

function resetParams() {
  Object.assign(params, createLandingParams())
  hiddenSet.value = []
  importRaw.value = ''
  message.value = ''
  error.value = false
}

function importFromURL() {
  try {
    const parsed = parseLandingQuery(importRaw.value)
    Object.assign(params, parsed.params)
    hiddenSet.value = parsed.hidden
    message.value = '已从 URL 导入参数'
    error.value = false
  } catch (e: any) {
    error.value = true
    message.value = e?.message || '导入失败'
  }
}

async function loadFromPublicSettings() {
  loadingSettings.value = true
  try {
    const settings = await adminAPI.settings.getSettings()
    if (settings.site_name) params.site_name = settings.site_name
    if (settings.site_subtitle) params.site_subtitle = settings.site_subtitle
    if (settings.site_logo) params.site_logo = settings.site_logo
    if (settings.doc_url) params.doc_url = settings.doc_url
    message.value = '已填入当前站点公开字段，可继续微调'
    error.value = false
  } catch (e: any) {
    error.value = true
    message.value = e?.message || '读取设置失败'
    appStore.showError(message.value)
  } finally {
    loadingSettings.value = false
  }
}

async function copyParamURL() {
  try {
    await navigator.clipboard.writeText(paramURL.value)
    copied.value = true
    if (copyTimer) clearTimeout(copyTimer)
    copyTimer = setTimeout(() => {
      copied.value = false
    }, 1600)
  } catch {
    appStore.showError('复制失败')
  }
}

async function applyHTMLToHomeContent() {
  applying.value = true
  error.value = false
  message.value = ''
  try {
    const html = renderLandingFragment({ ...params }, [...hiddenSet.value])
    await adminAPI.settings.updateSettings({ home_content: html })
    message.value = '已将渲染后的 HTML 写入 home_content，打开站点首页即可查看。'
    appStore.showSuccess('已写入 home_content')
  } catch (e: any) {
    error.value = true
    message.value = e?.message || '写入失败'
    appStore.showError(message.value)
  } finally {
    applying.value = false
  }
}

async function clearHomeContent() {
  clearing.value = true
  error.value = false
  message.value = ''
  try {
    await adminAPI.settings.updateSettings({ home_content: '' })
    message.value = '已清空 home_content，站点将恢复默认首页。'
    appStore.showSuccess('已恢复默认首页')
  } catch (e: any) {
    error.value = true
    message.value = e?.message || '恢复失败'
    appStore.showError(message.value)
  } finally {
    clearing.value = false
  }
}

onBeforeUnmount(() => {
  if (copyTimer) clearTimeout(copyTimer)
  if (previewTimer) clearTimeout(previewTimer)
})
</script>
