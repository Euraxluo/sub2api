<template>
  <section class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900 sm:p-5">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">任务中心</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          运行内置 Go 功能或可信 JavaScript，支持定时执行、异常去重和恢复通知。
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="refreshAll">刷新</button>
        <button type="button" class="btn btn-primary" @click="openCreate">新建任务</button>
      </div>
    </div>

    <div class="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/30 dark:text-amber-200">
      JavaScript 以独立 Node.js 子进程运行，但不是安全沙箱。只保存和执行你信任的管理员脚本；任务不会继承 Sub2API 主进程的环境变量。
    </div>

    <div v-if="notice" class="mt-4 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:border-emerald-800/60 dark:bg-emerald-950/30 dark:text-emerald-200">
      {{ notice }}
    </div>
    <div v-if="error" class="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/30 dark:text-red-200">
      {{ error }}
    </div>

    <div class="mt-5 grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.1fr)_minmax(380px,0.9fr)]">
      <div class="min-w-0 space-y-3">
        <div v-if="loading" class="rounded-lg border border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">
          正在读取任务…
        </div>
        <div v-else-if="tasks.length === 0" class="rounded-lg border border-dashed border-gray-300 p-8 text-center dark:border-dark-600">
          <p class="text-sm font-medium text-gray-700 dark:text-dark-200">还没有任务</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">创建一个 Sub2API 上游余额监控，或保存一段可信 JavaScript。</p>
        </div>

        <article
          v-for="taskItem in tasks"
          :key="taskItem.id"
          class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"
        >
          <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h3 class="truncate font-medium text-gray-900 dark:text-white">{{ taskItem.name }}</h3>
                <span class="rounded-full px-2 py-0.5 text-xs" :class="statusClass(taskItem.last_status || '')">
                  {{ taskItem.running ? '运行中' : statusLabel(taskItem.last_status || '') }}
                </span>
                <span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-800 dark:text-dark-300">
                  {{ taskItem.kind === 'builtin' ? '内置任务' : 'JavaScript' }}
                </span>
                <span v-if="taskItem.enabled" class="rounded-full bg-blue-50 px-2 py-0.5 text-xs text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
                  每 {{ formatInterval(taskItem.interval_seconds) }}
                </span>
                <span v-else class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400">未调度</span>
              </div>
              <p v-if="taskItem.description" class="mt-1 line-clamp-2 text-sm text-gray-500 dark:text-dark-400">{{ taskItem.description }}</p>
              <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">
                上次：{{ formatTime(taskItem.last_run_at) }}
                <span v-if="taskItem.last_message"> · {{ taskItem.last_message }}</span>
              </p>
              <p v-if="taskItem.enabled" class="mt-1 text-xs text-gray-400 dark:text-dark-500">下次：{{ formatTime(taskItem.next_run_at) }}</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :disabled="taskItem.running || actionTaskId === taskItem.id" @click="runNow(taskItem, false)">运行</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="taskItem.running || actionTaskId === taskItem.id" @click="runNow(taskItem, true)">运行并通知</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="taskItem.running" @click="openEdit(taskItem)">编辑</button>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="taskItem.running || actionTaskId === taskItem.id" @click="toggleTask(taskItem)">
                {{ taskItem.enabled ? '暂停' : '启用' }}
              </button>
              <button type="button" class="btn btn-danger btn-sm" :disabled="taskItem.running || actionTaskId === taskItem.id" @click="removeTask(taskItem)">删除</button>
            </div>
          </div>
        </article>
      </div>

      <div class="min-w-0 space-y-4">
        <form v-if="editing" class="rounded-lg border border-gray-200 p-4 dark:border-dark-700" @submit.prevent="saveTask">
          <div class="flex items-center justify-between gap-3">
            <h3 class="font-medium text-gray-900 dark:text-white">{{ editingId ? '编辑任务' : '新建任务' }}</h3>
            <button type="button" class="text-sm text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white" @click="closeEditor">关闭</button>
          </div>

          <div class="mt-4 space-y-4">
            <div>
              <label class="input-label">任务名称</label>
              <input v-model="form.name" class="input" maxlength="128" required placeholder="例如：香港上游余额" />
            </div>
            <div>
              <label class="input-label">说明</label>
              <textarea v-model="form.description" class="input min-h-[72px] resize-y" maxlength="1024" placeholder="这个任务检查什么，以及异常时该怎么处理"></textarea>
            </div>
            <div>
              <label class="input-label">任务类型</label>
              <select v-model="form.kind" class="input" @change="handleKindChange">
                <option value="builtin">内置任务</option>
                <option value="javascript">可信 JavaScript</option>
              </select>
            </div>

            <template v-if="form.kind === 'builtin'">
              <div>
                <label class="input-label">内置功能</label>
                <select v-model="form.builtin_id" class="input" required @change="applySelectedBuiltinDefaults">
                  <option v-for="builtin in builtins" :key="builtin.id" :value="builtin.id">{{ builtin.name }}</option>
                </select>
                <p v-if="selectedBuiltin" class="input-hint">{{ selectedBuiltin.description }}</p>
              </div>

              <div v-for="field in selectedBuiltin?.fields || []" :key="field.key">
                <label class="input-label">
                  {{ field.label }}
                  <span v-if="field.required" class="text-red-500">*</span>
                </label>
                <input
                  v-if="field.secret"
                  v-model="secretDrafts[field.key]"
                  class="input"
                  type="password"
                  :placeholder="configuredSecretKeys.includes(field.key) ? '已配置；留空保持不变' : field.placeholder || ''"
                  autocomplete="new-password"
                />
                <input
                  v-else-if="field.type === 'number'"
                  v-model.number="form.input[field.key]"
                  class="input"
                  type="number"
                  step="any"
                  :required="field.required"
                  :placeholder="field.placeholder || ''"
                />
                <input
                  v-else
                  v-model="form.input[field.key]"
                  class="input"
                  :type="field.type === 'url' ? 'url' : 'text'"
                  :required="field.required"
                  :placeholder="field.placeholder || ''"
                />
                <p v-if="field.help" class="input-hint">{{ field.help }}</p>
              </div>
            </template>

            <template v-else>
              <div>
                <label class="input-label">JavaScript</label>
                <textarea v-model="form.script" class="input min-h-[260px] resize-y font-mono text-xs leading-relaxed" spellcheck="false"></textarea>
                <p class="input-hint">可直接使用 <code>input</code>、<code>secrets</code>、<code>task</code> 和 Node.js 全局 <code>fetch</code>；返回统一结果对象。</p>
              </div>
              <div>
                <label class="input-label">输入 JSON</label>
                <textarea v-model="inputJSON" class="input min-h-[100px] resize-y font-mono text-xs" spellcheck="false"></textarea>
              </div>
              <div>
                <label class="input-label">新增或替换的密钥 JSON</label>
                <textarea v-model="secretsJSON" class="input min-h-[88px] resize-y font-mono text-xs" spellcheck="false" placeholder='{"token":"..."}'></textarea>
                <p class="input-hint">已保存字段：{{ configuredSecretKeys.length ? configuredSecretKeys.join(', ') : '无' }}。留空不会删除已有密钥。</p>
              </div>
            </template>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div>
                <label class="input-label">间隔（秒）</label>
                <input v-model.number="form.interval_seconds" class="input" type="number" min="30" max="604800" />
              </div>
              <div>
                <label class="input-label">超时（秒）</label>
                <input v-model.number="form.timeout_seconds" class="input" type="number" min="1" max="300" />
              </div>
              <div>
                <label class="input-label">通知冷却（秒）</label>
                <input v-model.number="form.cooldown_seconds" class="input" type="number" min="60" max="604800" />
              </div>
            </div>

            <div>
              <label class="input-label">通知策略</label>
              <select v-model="form.notify_policy" class="input">
                <option value="failure_recovery">异常与恢复</option>
                <option value="failure">仅异常</option>
                <option value="always">每次运行</option>
                <option value="never">不通知</option>
              </select>
            </div>

            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="form.enabled" type="checkbox" class="rounded border-gray-300" />
              保存后启用定时运行
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="form.notify_manual" type="checkbox" class="rounded border-gray-300" />
              手动运行也遵循通知策略
            </label>

            <div class="flex flex-wrap justify-end gap-2">
              <button type="button" class="btn btn-secondary" @click="closeEditor">取消</button>
              <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存任务' }}</button>
            </div>
          </div>
        </form>

        <div v-if="lastRun" class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
          <div class="flex items-center justify-between gap-3">
            <h3 class="font-medium text-gray-900 dark:text-white">最近手动运行结果</h3>
            <span class="rounded-full px-2 py-0.5 text-xs" :class="statusClass(lastRun.status)">{{ statusLabel(lastRun.status) }}</span>
          </div>
          <p class="mt-2 text-sm text-gray-700 dark:text-dark-200">{{ lastRun.message || lastRun.error || '-' }}</p>
          <pre v-if="lastRun.data" class="mt-3 max-h-48 overflow-auto rounded bg-gray-950 p-3 text-xs text-gray-100">{{ JSON.stringify(lastRun.data, null, 2) }}</pre>
          <pre v-if="lastRun.stderr" class="mt-3 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-red-950 p-3 text-xs text-red-100">{{ lastRun.stderr }}</pre>
        </div>
      </div>
    </div>

    <div class="mt-6 rounded-lg border border-gray-200 dark:border-dark-700">
      <div class="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-dark-700">
        <h3 class="font-medium text-gray-900 dark:text-white">最近执行记录</h3>
        <span class="text-xs text-gray-500 dark:text-dark-400">最多显示 50 条</span>
      </div>
      <div v-if="runs.length === 0" class="p-6 text-center text-sm text-gray-500 dark:text-dark-400">暂无执行记录</div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 dark:bg-dark-800/60">
            <tr>
              <th class="px-4 py-2 text-left font-medium text-gray-500">任务</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">状态</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">触发</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">消息</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">耗时</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">时间</th>
              <th class="px-4 py-2 text-left font-medium text-gray-500">通知</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
            <tr v-for="run in runs" :key="run.id">
              <td class="px-4 py-2 text-gray-800 dark:text-dark-100">{{ run.task_name }}</td>
              <td class="px-4 py-2"><span class="rounded-full px-2 py-0.5 text-xs" :class="statusClass(run.status)">{{ statusLabel(run.status) }}</span></td>
              <td class="px-4 py-2 text-gray-500">{{ run.trigger === 'scheduled' ? '定时' : '手动' }}</td>
              <td class="max-w-[420px] truncate px-4 py-2 text-gray-600 dark:text-dark-300" :title="run.error || run.message">{{ run.error || run.message || '-' }}</td>
              <td class="px-4 py-2 text-gray-500">{{ run.duration_ms }} ms</td>
              <td class="whitespace-nowrap px-4 py-2 text-gray-500">{{ formatTime(run.finished_at) }}</td>
              <td class="px-4 py-2 text-gray-500">{{ run.notified ? '已发送' : run.notify_error ? '失败' : '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  createAdminJob,
  deleteAdminJob,
  listAdminJobBuiltins,
  listAdminJobRuns,
  listAdminJobs,
  runAdminJob,
  updateAdminJob,
  type AdminJobBuiltinDefinition,
  type AdminJobDraft,
  type AdminJobKind,
  type AdminJobNotifyPolicy,
  type AdminJobRun,
  type AdminJobStatus,
  type AdminJobTask
} from './api'

interface TaskForm {
  name: string
  description: string
  kind: AdminJobKind
  builtin_id: string
  script: string
  input: Record<string, unknown>
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  notify_policy: AdminJobNotifyPolicy
  notify_manual: boolean
  cooldown_seconds: number
}

const defaultScript = `// input：普通参数；secrets：敏感参数；task：任务信息
// 可以使用 Node.js 的全局 fetch。
return {
  status: 'ok',
  title: task.name,
  message: '脚本执行成功',
  data: { received: input }
}`

const builtins = ref<AdminJobBuiltinDefinition[]>([])
const tasks = ref<AdminJobTask[]>([])
const runs = ref<AdminJobRun[]>([])
const loading = ref(false)
const saving = ref(false)
const editing = ref(false)
const editingId = ref('')
const configuredSecretKeys = ref<string[]>([])
const secretDrafts = reactive<Record<string, string>>({})
const inputJSON = ref('{}')
const secretsJSON = ref('{}')
const notice = ref('')
const error = ref('')
const actionTaskId = ref('')
const lastRun = ref<AdminJobRun | null>(null)

const form = reactive<TaskForm>(emptyForm())

const selectedBuiltin = computed(() => builtins.value.find((item) => item.id === form.builtin_id) || null)

function emptyForm(): TaskForm {
  return {
    name: '',
    description: '',
    kind: 'builtin',
    builtin_id: '',
    script: defaultScript,
    input: {},
    enabled: false,
    interval_seconds: 600,
    timeout_seconds: 30,
    notify_policy: 'failure_recovery',
    notify_manual: false,
    cooldown_seconds: 1800
  }
}

function resetSecretDrafts() {
  for (const key of Object.keys(secretDrafts)) delete secretDrafts[key]
}

function assignForm(next: TaskForm) {
  Object.assign(form, next)
}

async function refreshAll() {
  loading.value = true
  error.value = ''
  try {
    const [builtinList, taskList, runList] = await Promise.all([
      listAdminJobBuiltins(),
      listAdminJobs(),
      listAdminJobRuns('', 50)
    ])
    builtins.value = builtinList
    tasks.value = taskList
    runs.value = runList
  } catch (cause) {
    error.value = errorMessage(cause, '读取任务中心失败。')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = ''
  configuredSecretKeys.value = []
  resetSecretDrafts()
  const firstBuiltin = builtins.value[0]
  assignForm({ ...emptyForm(), builtin_id: firstBuiltin?.id || '' })
  inputJSON.value = '{}'
  secretsJSON.value = '{}'
  applySelectedBuiltinDefaults()
  editing.value = true
  notice.value = ''
  error.value = ''
}

function openEdit(taskItem: AdminJobTask) {
  editingId.value = taskItem.id
  configuredSecretKeys.value = [...(taskItem.secret_keys || [])]
  resetSecretDrafts()
  assignForm({
    name: taskItem.name,
    description: taskItem.description || '',
    kind: taskItem.kind,
    builtin_id: taskItem.builtin_id || '',
    script: taskItem.script || defaultScript,
    input: { ...(taskItem.input || {}) },
    enabled: taskItem.enabled,
    interval_seconds: taskItem.interval_seconds,
    timeout_seconds: taskItem.timeout_seconds,
    notify_policy: taskItem.notify_policy,
    notify_manual: taskItem.notify_manual,
    cooldown_seconds: taskItem.cooldown_seconds
  })
  inputJSON.value = JSON.stringify(taskItem.input || {}, null, 2)
  secretsJSON.value = '{}'
  editing.value = true
  notice.value = ''
  error.value = ''
}

function closeEditor() {
  editing.value = false
  editingId.value = ''
  resetSecretDrafts()
}

function handleKindChange() {
  if (form.kind === 'builtin') {
    if (!form.builtin_id) form.builtin_id = builtins.value[0]?.id || ''
    applySelectedBuiltinDefaults()
  }
}

function applySelectedBuiltinDefaults() {
  const builtin = selectedBuiltin.value
  if (!builtin) return
  if (!form.name || !editingId.value) form.name = builtin.name
  if (!form.description || !editingId.value) form.description = builtin.description
  for (const field of builtin.fields || []) {
    if (!field.secret && form.input[field.key] === undefined && field.default !== undefined) {
      form.input[field.key] = field.default
    }
  }
}

function parseObjectJSON(value: string, label: string): Record<string, unknown> {
  const parsed = JSON.parse(value || '{}')
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') throw new Error(`${label}必须是 JSON 对象`)
  return parsed as Record<string, unknown>
}

function buildDraft(enabledOverride?: boolean): AdminJobDraft {
  let input: Record<string, unknown>
  let secrets: Record<string, string>
  if (form.kind === 'javascript') {
    input = parseObjectJSON(inputJSON.value, '输入')
    const parsedSecrets = parseObjectJSON(secretsJSON.value, '密钥')
    secrets = Object.fromEntries(Object.entries(parsedSecrets).map(([key, value]) => [key, String(value ?? '')]))
  } else {
    input = { ...form.input }
    secrets = Object.fromEntries(Object.entries(secretDrafts).filter(([, value]) => value !== ''))
  }
  return {
    name: form.name.trim(),
    description: form.description.trim(),
    kind: form.kind,
    builtin_id: form.kind === 'builtin' ? form.builtin_id : undefined,
    script: form.kind === 'javascript' ? form.script : undefined,
    input,
    secrets,
    enabled: enabledOverride ?? form.enabled,
    interval_seconds: Number(form.interval_seconds),
    timeout_seconds: Number(form.timeout_seconds),
    notify_policy: form.notify_policy,
    notify_manual: form.notify_manual,
    cooldown_seconds: Number(form.cooldown_seconds)
  }
}

async function saveTask() {
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const draft = buildDraft()
    if (editingId.value) await updateAdminJob(editingId.value, draft)
    else await createAdminJob(draft)
    notice.value = editingId.value ? '任务已更新。' : '任务已创建。'
    closeEditor()
    await refreshAll()
  } catch (cause) {
    error.value = errorMessage(cause, '保存任务失败。')
  } finally {
    saving.value = false
  }
}

async function runNow(taskItem: AdminJobTask, notify: boolean) {
  actionTaskId.value = taskItem.id
  error.value = ''
  notice.value = ''
  try {
    lastRun.value = await runAdminJob(taskItem.id, notify)
    notice.value = `${taskItem.name} 执行完成：${statusLabel(lastRun.value.status)}`
    await refreshAll()
  } catch (cause) {
    error.value = errorMessage(cause, '运行任务失败。')
  } finally {
    actionTaskId.value = ''
  }
}

async function toggleTask(taskItem: AdminJobTask) {
  actionTaskId.value = taskItem.id
  error.value = ''
  try {
    const draft: AdminJobDraft = {
      name: taskItem.name,
      description: taskItem.description || '',
      kind: taskItem.kind,
      builtin_id: taskItem.builtin_id,
      script: taskItem.script,
      input: taskItem.input || {},
      secrets: {},
      enabled: !taskItem.enabled,
      interval_seconds: taskItem.interval_seconds,
      timeout_seconds: taskItem.timeout_seconds,
      notify_policy: taskItem.notify_policy,
      notify_manual: taskItem.notify_manual,
      cooldown_seconds: taskItem.cooldown_seconds
    }
    await updateAdminJob(taskItem.id, draft)
    notice.value = taskItem.enabled ? '任务已暂停。' : '任务已启用。'
    await refreshAll()
  } catch (cause) {
    error.value = errorMessage(cause, '更新任务状态失败。')
  } finally {
    actionTaskId.value = ''
  }
}

async function removeTask(taskItem: AdminJobTask) {
  if (!window.confirm(`确认删除任务“${taskItem.name}”吗？执行历史会保留。`)) return
  actionTaskId.value = taskItem.id
  error.value = ''
  try {
    await deleteAdminJob(taskItem.id)
    notice.value = '任务已删除。'
    if (editingId.value === taskItem.id) closeEditor()
    await refreshAll()
  } catch (cause) {
    error.value = errorMessage(cause, '删除任务失败。')
  } finally {
    actionTaskId.value = ''
  }
}

function statusLabel(status: AdminJobStatus | string): string {
  if (status === 'ok') return '正常'
  if (status === 'warning') return '警告'
  if (status === 'critical') return '严重'
  if (status === 'error') return '失败'
  return '未运行'
}

function statusClass(status: AdminJobStatus | string): string {
  if (status === 'ok') return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
  if (status === 'warning') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
  if (status === 'critical' || status === 'error') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
}

function formatInterval(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

function formatTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function errorMessage(cause: unknown, fallback: string): string {
  const candidate = cause as { response?: { data?: { message?: string } }; message?: string }
  return candidate?.response?.data?.message || candidate?.message || fallback
}

onMounted(() => {
  void refreshAll()
})
</script>
