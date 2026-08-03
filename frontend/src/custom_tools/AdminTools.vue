<template>
  <AppLayout>
    <div class="mx-auto flex max-w-[1500px] flex-col gap-4">
      <div class="flex min-w-0 flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            type="button"
            class="tools-tab"
            :class="{ 'tools-tab-active': activeTab === tab.id }"
            @click="selectToolTab(tab.id)"
          >
            <Icon :name="tab.icon" size="sm" />
            <span>{{ tab.label }}</span>
          </button>
        </div>
        <div class="text-xs text-gray-500 dark:text-dark-400">
          写入使用 sub2api 现有 Admin API；RT 支持代理测试、轮询分配与 TLS 指纹。
        </div>
      </div>

      <section v-if="activeTab === 'proxy'" class="tools-surface">
        <div class="tools-section-header">
          <div>
            <h2 class="tools-title">代理导入</h2>
            <p class="tools-description">粘贴 Clash YAML 或输入订阅链接，预览后再导入到现有 IP 管理。</p>
          </div>
          <div v-if="proxyPreview" class="tools-badges">
            <span class="tools-badge">总数 {{ proxyPreview.summary.total }}</span>
            <span class="tools-badge text-emerald-700 dark:text-emerald-300">有效 {{ proxyPreview.summary.valid }}</span>
            <span class="tools-badge text-amber-700 dark:text-amber-300">重复 {{ proxyPreview.summary.duplicates }}</span>
            <span class="tools-badge text-red-700 dark:text-red-300">错误 {{ proxyPreview.summary.invalid }}</span>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 2xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)]">
          <div class="min-w-0 space-y-4">
            <div>
              <label class="input-label">Clash 订阅链接</label>
              <div class="flex min-w-0 flex-col gap-2 sm:flex-row">
                <input
                  v-model="proxySubscriptionUrl"
                  class="input"
                  type="url"
                  placeholder="https://example.com/subs/clash/..."
                />
                <button
                  type="button"
                  class="btn btn-secondary shrink-0"
                  :disabled="proxyPreviewLoading || !proxySubscriptionUrl.trim()"
                  @click="previewProxyFromUrl"
                >
                  <Icon name="cloud" size="sm" />
                  抓取预览
                </button>
              </div>
              <p class="input-hint">订阅由后端代抓，避免浏览器 CORS；内容大小限制 2 MiB。</p>
            </div>

            <div>
              <label class="input-label">Clash YAML</label>
              <textarea
                v-model="proxyYaml"
                class="input min-h-[230px] resize-y font-mono text-xs leading-relaxed"
                spellcheck="false"
                placeholder="proxies:&#10;  - name: proxy-a&#10;    type: http&#10;    server: 127.0.0.1&#10;    port: 8080"
              ></textarea>
              <div class="mt-2 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  class="btn btn-primary"
                  :disabled="proxyPreviewLoading || !proxyYaml.trim()"
                  @click="previewProxyFromYaml"
                >
                  <Icon name="search" size="sm" />
                  解析 YAML
                </button>
                <button
                  type="button"
                  class="btn btn-secondary"
                  :disabled="!proxyYaml && !proxySubscriptionUrl && !proxyPreview"
                  @click="resetProxyImport"
                >
                  <Icon name="x" size="sm" />
                  清空
                </button>
              </div>
            </div>
          </div>

          <div class="min-w-0">
            <div class="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">预览结果</h3>
                <p class="text-xs text-gray-500 dark:text-dark-400">
                  支持 http、https、socks5、socks5h；enabled:false 会保留为 inactive。
                </p>
              </div>
              <div class="flex flex-wrap gap-2">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  :disabled="proxyBatchTesting || selectedImportableProxyRows.length === 0"
                  @click="testSelectedProxyRows"
                >
                  <Icon :name="proxyBatchTesting ? 'refresh' : 'play'" size="sm" :class="proxyBatchTesting ? 'animate-spin' : ''" />
                  测试选中
                </button>
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="!canImportProxies || proxyImporting"
                  @click="importProxyData"
                >
                  <Icon name="upload" size="sm" />
                  导入选中
                </button>
              </div>
            </div>

            <div v-if="proxyImportResult" class="mb-3 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300">
              创建 {{ proxyImportResult.proxy_created }}，复用 {{ proxyImportResult.proxy_reused }}，失败 {{ proxyImportResult.proxy_failed }}
            </div>
            <div v-if="proxyBatchResult" class="mb-3 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-300">
              创建 {{ proxyBatchResult.created }}，跳过 {{ proxyBatchResult.skipped }}
            </div>

            <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
              <div v-if="proxyPreviewLoading" class="tools-empty">
                正在解析订阅...
              </div>
              <div v-else-if="!proxyPreview" class="tools-empty">
                先抓取订阅或解析 YAML，这里会显示可导入代理。
              </div>
              <div v-else class="overflow-x-auto">
                <table class="min-w-[920px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
                  <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
                    <tr>
                      <th class="tools-th w-10">
                        <input
                          type="checkbox"
                          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                          :checked="allImportableProxyRowsSelected"
                          :disabled="selectableProxyRows.length === 0"
                          @change="toggleAllProxyRows"
                        />
                      </th>
                      <th class="tools-th">#</th>
                      <th class="tools-th">名称</th>
                      <th class="tools-th">协议</th>
                      <th class="tools-th">地址</th>
                      <th class="tools-th">账号</th>
                      <th class="tools-th">状态</th>
                      <th class="tools-th">结果</th>
                      <th class="tools-th">测试</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/30">
                    <tr v-for="row in displayedProxyRows" :key="row.index" :class="{ 'bg-emerald-50/60 dark:bg-emerald-950/20': isProxyRowImported(row) }">
                      <td class="tools-td">
                        <input
                          type="checkbox"
                          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-60"
                          :checked="isProxyRowSelected(row) || isProxyRowImported(row)"
                          :disabled="!row.valid || isProxyRowImported(row)"
                          @change="toggleProxyRow(row, $event)"
                        />
                      </td>
                      <td class="tools-td text-gray-500">{{ row.index }}</td>
                      <td class="tools-td max-w-[220px] truncate font-medium text-gray-900 dark:text-white">
                        {{ row.proxy.name || row.name || '-' }}
                      </td>
                      <td class="tools-td">
                        <span class="tools-code">{{ row.proxy.protocol || '-' }}</span>
                      </td>
                      <td class="tools-td">
                        <span class="font-mono text-xs">{{ row.proxy.host || '-' }}:{{ row.proxy.port || '-' }}</span>
                      </td>
                      <td class="tools-td">{{ row.proxy.username ? '已设置' : '-' }}</td>
                      <td class="tools-td">
                        <span :class="row.proxy.status === 'active' ? 'status-ok' : 'status-muted'">
                          {{ row.proxy.status || '-' }}
                        </span>
                      </td>
                      <td class="tools-td">
                        <span v-if="isProxyRowImported(row)" class="status-ok">已导入</span>
                        <span v-else-if="row.valid" class="status-ok">可导入</span>
                        <span v-else class="status-error">{{ row.errors.join('；') }}</span>
                      </td>
                      <td class="tools-td">
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm"
                          :disabled="!row.valid || isProxyRowImported(row) || proxyTestingRows.has(proxyRowKey(row))"
                          @click="testProxyRow(row)"
                        >
                          <Icon
                            :name="proxyTestingRows.has(proxyRowKey(row)) ? 'refresh' : 'play'"
                            size="sm"
                            :class="proxyTestingRows.has(proxyRowKey(row)) ? 'animate-spin' : ''"
                          />
                          {{ proxyTestingRows.has(proxyRowKey(row)) ? '测试中' : '测试' }}
                        </button>
                        <span
                          v-if="proxyTestResults[proxyRowKey(row)]"
                          class="ml-2 text-xs"
                          :class="proxyTestResults[proxyRowKey(row)].success ? 'text-emerald-600 dark:text-emerald-300' : 'text-red-600 dark:text-red-300'"
                        >
                          {{ proxyTestLabel(proxyTestResults[proxyRowKey(row)]) }}
                        </span>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <div v-if="proxyPreview.rows.length > displayedProxyRows.length" class="border-t border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">
                  仅显示前 {{ displayedProxyRows.length }} 条，导入会使用全部有效代理。
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      <ModelReasoningEffortTool v-else-if="activeTab === 'reasoning'" />
      <LandingHomeTool v-else-if="activeTab === 'landing'" />

      <section v-else-if="activeTab === 'rt'" class="tools-surface">
        <div class="tools-section-header">
          <div>
            <h2 class="tools-title">OpenAI RT 导入</h2>
            <p class="tools-description">从混合文本中提取 email 和 rt_ / rt- token，逐条校验后创建 OpenAI OAuth 账号。</p>
          </div>
          <div class="tools-badges">
            <span class="tools-badge">账号并发 {{ normalizedRtConcurrency }}</span>
            <span class="tools-badge">{{ rtProxyAssignmentLabel }}</span>
            <span class="tools-badge">{{ rtFingerprintLabel }}</span>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 2xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
          <div class="min-w-0 space-y-4">
            <div>
              <label class="input-label">RT 文本</label>
              <textarea
                v-model="rtInput"
                class="input min-h-[260px] resize-y font-mono text-xs leading-relaxed"
                spellcheck="false"
                placeholder="user@example.com rt_...&#10;another@example.com rt-..."
              ></textarea>
              <div class="mt-2 flex flex-wrap items-center gap-2">
                <button type="button" class="btn btn-secondary" :disabled="!rtInput.trim()" @click="parseRtInput">
                  <Icon name="search" size="sm" />
                  预览提取
                </button>
                <button type="button" class="btn btn-primary" :disabled="importingRt || !rtInput.trim()" @click="importRtRows">
                  <Icon name="upload" size="sm" />
                  开始导入
                </button>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 xl:grid-cols-[minmax(180px,0.75fr)_minmax(420px,2fr)_minmax(120px,0.55fr)]">
              <div>
                <label class="input-label">代理分配模式</label>
                <select v-model="rtProxyMode" class="input">
                  <option value="none">不指定代理</option>
                  <option value="single">统一指定一个代理</option>
                  <option value="round_robin">多个代理自动轮询</option>
                </select>
              </div>
              <div>
                <label class="input-label">统一 proxy_id</label>
                <ProxySelector
                  v-model="rtProxyId"
                  :proxies="activeProxies"
                  :disabled="rtProxyMode !== 'single'"
                />
              </div>
              <div>
                <label class="input-label">账号并发</label>
                <input v-model.number="rtConcurrency" class="input" type="number" min="1" max="100" />
              </div>
            </div>

            <div v-if="rtProxyMode === 'round_robin'" class="tools-panel">
              <div class="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-white">多个代理自动分配</h3>
                  <p class="text-xs text-gray-500 dark:text-dark-400">
                    选中的代理会按 RT 顺序轮询写入账号，刷新 token 和创建账号都会使用同一个代理。
                  </p>
                </div>
                <div class="flex flex-wrap gap-2">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="rtBatchTestingProxies || activeProxies.length === 0"
                    @click="testAllRtProxies"
                  >
                    <Icon
                      :name="rtBatchTestingProxies ? 'refresh' : 'play'"
                      size="sm"
                      :class="rtBatchTestingProxies ? 'animate-spin' : ''"
                    />
                    {{ rtBatchTestingProxies ? '测试中' : '测试全部' }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-primary btn-sm"
                    :disabled="selectedRtProxyIds.length === 0 || rtRows.length === 0"
                    @click="assignRtProxies(true)"
                  >
                    <Icon name="swap" size="sm" />
                    自动分配到 RT
                  </button>
                </div>
              </div>

              <div class="mb-3">
                <input
                  v-model="rtProxySearch"
                  class="input"
                  type="search"
                  placeholder="搜索代理名称或地址"
                />
              </div>

              <div class="max-h-[260px] overflow-auto rounded-lg border border-gray-200 dark:border-dark-700">
                <label
                  v-for="proxy in filteredRtProxies"
                  :key="proxy.id"
                  class="flex cursor-pointer items-center gap-3 border-b border-gray-100 px-3 py-2 last:border-b-0 hover:bg-gray-50 dark:border-dark-800 dark:hover:bg-dark-800/60"
                >
                  <input
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    :checked="selectedRtProxyIds.includes(proxy.id)"
                    @change="toggleRtProxySelection(proxy.id, $event)"
                  />
                  <div class="min-w-0 flex-1">
                    <div class="flex min-w-0 flex-wrap items-center gap-2">
                      <span class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ proxy.name }}</span>
                      <span class="tools-code">{{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}</span>
                      <span v-if="typeof proxy.account_count === 'number'" class="tools-badge">{{ proxy.account_count }} 账号</span>
                      <span v-if="rtTestingProxyIds.has(proxy.id)" class="status-running">
                        测试中
                      </span>
                      <span v-if="rtProxyTestResults[proxy.id]" :class="rtProxyTestResults[proxy.id].success ? 'status-ok' : 'status-error'">
                        {{ rtProxyTestLabel(proxy.id) }}
                      </span>
                    </div>
                  </div>
                </label>
                <div v-if="filteredRtProxies.length === 0" class="tools-empty min-h-[120px]">
                  没有匹配的代理。
                </div>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 xl:grid-cols-3">
              <div>
                <label class="input-label">client_id</label>
                <input v-model="rtClientId" class="input font-mono text-xs" placeholder="留空使用后端默认" />
              </div>
              <label class="mt-7 flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
                <input
                  v-model="rtEnableFingerprint"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                添加 TLS 指纹
              </label>
              <div>
                <label class="input-label">指纹模板</label>
                <div class="flex gap-2">
                  <select v-model="rtFingerprintProfileRaw" class="input" :disabled="!rtEnableFingerprint">
                    <option value="default">系统默认 Node/Codex 兼容</option>
                    <option value="generated">系统生成 Codex rustls 模板</option>
                    <option v-if="tlsFingerprintProfiles.length > 0" value="random">随机已有模板</option>
                    <option v-for="profile in tlsFingerprintProfiles" :key="profile.id" :value="String(profile.id)">
                      {{ profile.name }}
                    </option>
                  </select>
                  <button
                    type="button"
                    class="btn btn-secondary shrink-0"
                    :disabled="!rtEnableFingerprint || generatingRtFingerprint"
                    @click="generateAndSelectCodexFingerprint"
                  >
                    <Icon name="sparkles" size="sm" :class="generatingRtFingerprint ? 'animate-pulse' : ''" />
                  </button>
                </div>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
              <div>
                <label class="input-label">已选多代理</label>
                <div class="tools-readout">{{ selectedRtProxyIds.length }} 个</div>
              </div>
              <div>
                <label class="input-label">指纹来源</label>
                <div class="tools-readout">{{ rtFingerprintSourceLabel }}</div>
              </div>
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="rtClientId = openAIMobileClientId">
                填入 Mobile client_id
              </button>
              <button type="button" class="btn btn-secondary btn-sm" @click="rtClientId = ''">
                使用默认 client_id
              </button>
            </div>
          </div>

          <div class="min-w-0">
            <div class="mb-3 flex flex-wrap items-center gap-2">
              <span class="tools-badge">提取 {{ rtRows.length }}</span>
              <span class="tools-badge text-emerald-700 dark:text-emerald-300">成功 {{ rtSuccessCount }}</span>
              <span class="tools-badge text-red-700 dark:text-red-300">失败 {{ rtFailedCount }}</span>
            </div>

            <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
              <div v-if="rtRows.length === 0" class="tools-empty">
                先预览或直接开始导入，这里会显示提取到的 RT。
              </div>
              <div v-else class="overflow-x-auto">
                <table class="min-w-[1000px] divide-y divide-gray-200 text-sm dark:divide-dark-700">
                  <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-900/60 dark:text-dark-400">
                    <tr>
                      <th class="tools-th">#</th>
                      <th class="tools-th">Email</th>
                      <th class="tools-th">RT</th>
                      <th class="tools-th">代理</th>
                      <th class="tools-th">状态</th>
                      <th class="tools-th">信息</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/30">
                    <tr v-for="row in rtRows" :key="row.id">
                      <td class="tools-td text-gray-500">{{ row.index }}</td>
                      <td class="tools-td">{{ row.email || '-' }}</td>
                      <td class="tools-td font-mono text-xs">{{ maskToken(row.refreshToken) }}</td>
                      <td class="tools-td max-w-[260px] truncate">{{ rtRowProxyLabel(row) }}</td>
                      <td class="tools-td">
                        <span :class="rtStatusClass(row.status)">{{ rtStatusLabel(row.status) }}</span>
                      </td>
                      <td class="tools-td max-w-[360px] truncate">{{ row.message || '-' }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section v-else-if="activeTab === 'guard'" class="tools-surface">
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
                <label class="input-label">保护账号（可选）</label>
                <input v-model="policy.account_ids_text" class="input font-mono text-xs" placeholder="留空=全部 provider 账号；例如 12, 15, 19" />
                <p class="input-hint">限制器只作用于列出的账号；留空表示所有 provider 和账号。</p>
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
              <button
                type="button"
                class="btn btn-primary"
                :disabled="codexQuotaGuardOperating"
                @click="startCodexQuotaGuardTask"
              >
                <Icon name="play" size="sm" />
                启动 / 更新
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="codexQuotaGuardOperating"
                @click="scanCodexQuotaGuardTask"
              >
                <Icon name="search" size="sm" />
                立即扫描
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="codexQuotaGuardOperating"
                @click="releaseCodexQuotaGuardTask"
              >
                <Icon name="lock" size="sm" />
                释放保护器封禁
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="codexQuotaGuardOperating"
                @click="stopCodexQuotaGuardTask"
              >
                <Icon name="ban" size="sm" />
                停止
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="codexQuotaGuardLoading || codexQuotaGuardOperating"
                @click="loadCodexQuotaGuardStatus"
              >
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

      <!-- 通知通道 -->
      <section v-else-if="activeTab === 'feishu'" class="tools-surface">
	        <div class="tools-section-header">
	          <div>
	            <h2 class="tools-title">通知通道</h2>
	            <p class="tools-description">选择一种通知方案，完成授权、目标配置和测试发送。</p>
	          </div>
	          <span :class="notificationProvider === 'feishu' ? feishuPhaseClass : notificationProvider === 'dingtalk' ? dingTalkPhaseClass : wechatPhaseClass">{{ notificationProvider === 'feishu' ? feishuPhaseLabel : notificationProvider === 'dingtalk' ? dingTalkPhaseLabel : wechatPhaseLabel }}</span>
	        </div>

	        <div class="mb-4 flex flex-wrap gap-2">
	          <button class="tools-tab" :class="notificationProvider === 'feishu' ? 'tools-tab-active' : ''" :disabled="providerLoading" @click="selectNotificationProvider('feishu')">飞书</button>
	          <button class="tools-tab" :class="notificationProvider === 'dingtalk' ? 'tools-tab-active' : ''" :disabled="providerLoading" @click="selectNotificationProvider('dingtalk')">钉钉（dws CLI）</button>
	          <button class="tools-tab" :class="notificationProvider === 'wechat' ? 'tools-tab-active' : ''" :disabled="providerLoading" @click="selectNotificationProvider('wechat')">微信</button>
	        </div>

        <div v-if="notificationProvider === 'feishu'">
        <div class="mb-4 grid grid-cols-1 gap-2 md:grid-cols-4">
	          <div v-for="step in feishuSteps" :key="step.key" class="rounded border p-3" :class="step.key === feishuStepKey ? 'border-primary-400 bg-primary-50 dark:bg-primary-950/30' : 'border-gray-200 dark:border-dark-700'">
	            <p class="text-xs font-semibold text-gray-900 dark:text-white">{{ step.index }}. {{ step.label }}</p>
	            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ stepHint(step.key) }}</p>
	          </div>
	        </div>

	        <p v-if="feishuStatusMessage" class="mb-4 rounded border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">{{ feishuStatusMessage }}</p>
	        <p v-if="feishuNotice" class="mb-4 rounded border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200">{{ feishuNotice }}</p>

	        <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
	          <div class="space-y-4">
	            <div>
	              <h3 class="mb-2 text-sm font-medium">1. 应用授权</h3>
	              <div v-if="feishuVerificationUrl && (feishuPhase === 'starting' || feishuPhase === 'awaiting_authorization')" class="mb-3">
	                <p class="mb-2 text-xs">扫描二维码完成飞书应用创建和授权。授权后不要重新点击“开始连接”，页面会自动进入下一步。</p>
	                <div class="flex flex-col items-center gap-3 rounded-lg border p-4">
	                  <img v-if="feishuQRBase64" :src="feishuQRBase64" alt="二维码" class="h-48 w-48 rounded border" />
	                  <a :href="feishuVerificationUrl" target="_blank" class="text-xs text-blue-600 underline">打开授权链接</a>
	                </div>
	              </div>
	              <div class="flex flex-wrap gap-2">
	                <button class="btn btn-primary" :disabled="feishuConnecting" @click="feishuStartConnect">{{ feishuPhase === 'error' ? '重新开始连接' : '开始连接' }}</button>
	                <button class="btn btn-secondary" :disabled="!feishuConnecting" @click="feishuCancelConnect">取消</button>
	                <button class="btn btn-secondary" :disabled="feishuStatusLoading" @click="feishuRefreshStatus">刷新状态</button>
	              </div>
	              <p v-if="feishuConnectError" class="mt-2 text-xs text-red-600">{{ feishuConnectError }}</p>
	            </div>

	            <div v-if="feishuConnected">
	              <h3 class="mb-2 text-sm font-medium">2. 选择告警群聊</h3>
	              <div class="space-y-2">
	                <div class="flex flex-wrap gap-2">
	                  <select v-if="feishuChats.length" v-model="feishuChatId" class="input min-w-0 flex-1" aria-label="选择告警群聊">
	                    <option value="">选择机器人所在群聊</option>
	                    <option v-for="chat in feishuChats" :key="chat.chat_id" :value="chat.chat_id">{{ chat.name || '未命名群聊' }} ({{ chat.chat_id }})</option>
	                  </select>
	                  <input v-else v-model="feishuChatId" class="input min-w-0 flex-1" placeholder="机器人未发现群聊，输入 oc_xxxxxxxx" />
	                  <button class="btn btn-secondary" :disabled="feishuChatsLoading" @click="feishuLoadChats">{{ feishuChatsLoading ? '查找中...' : '刷新群聊' }}</button>
	                </div>
	                <p v-if="!feishuChatsLoading && !feishuChats.length" class="text-xs text-amber-700 dark:text-amber-300">没有发现机器人所在的群。请先把这个应用的机器人加入群，再刷新群聊。</p>
	                <button class="btn btn-primary" :disabled="!feishuChatId.trim() || feishuTargetSaving" @click="feishuSaveTarget">{{ feishuTargetSaving ? '保存中...' : '保存告警目标' }}</button>
	              </div>
	            </div>
          </div>

          <div class="space-y-4">
            <div>
              <h3 class="mb-2 text-sm font-medium">3. 测试推送</h3>
              <div class="space-y-2">
                <input v-model="feishuTestMessage" class="input" placeholder="输入测试消息（可选）" :disabled="!feishuCanSend" />
                <button class="btn btn-primary" :disabled="!feishuCanSend || feishuSending" @click="feishuSendTest">{{ feishuSending ? '发送中...' : '发送测试消息' }}</button>
                <p v-if="!feishuCanSend && feishuConnected" class="text-xs text-gray-500 dark:text-dark-400">先选择并保存告警群聊，测试按钮才会启用。</p>
              </div>
            </div>
            <div v-if="feishuDoctorOut">
              <h3 class="mb-2 text-sm font-medium">健康检查</h3>
              <pre class="max-h-40 overflow-auto rounded bg-gray-100 p-2 text-xs">{{ feishuDoctorOut }}</pre>
            </div>
          </div>
	        </div>
	        </div>

	        <div v-else-if="notificationProvider === 'wechat'">
          <div class="mb-4 grid grid-cols-1 gap-2 md:grid-cols-4">
            <div v-for="step in wechatSteps" :key="step.key" class="rounded border p-3" :class="step.key === wechatStepKey ? 'border-primary-400 bg-primary-50 dark:bg-primary-950/30' : 'border-gray-200 dark:border-dark-700'">
              <p class="text-xs font-semibold text-gray-900 dark:text-white">{{ step.index }}. {{ step.label }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ wechatStepHint(step.key) }}</p>
            </div>
          </div>

          <p v-if="wechatStatusMessage" class="mb-4 rounded border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">{{ wechatStatusMessage }}</p>
          <p v-if="wechatNotice" class="mb-4 rounded border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200">{{ wechatNotice }}</p>

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div class="space-y-4">
              <div>
                <h3 class="mb-2 text-sm font-medium">1. 微信扫码授权</h3>
                <div v-if="wechatVerificationUrl && (wechatPhase === 'starting' || wechatPhase === 'awaiting_authorization')" class="mb-3">
                  <p class="mb-2 text-xs">使用手机微信扫描二维码，授权后会自动绑定扫码用户，不需要配置群聊。</p>
                  <div class="flex flex-col items-center gap-3 rounded-lg border p-4">
                    <img v-if="wechatQRBase64" :src="wechatQRBase64" alt="微信授权二维码" class="h-48 w-48 rounded border" />
                    <a :href="wechatVerificationUrl" target="_blank" class="text-xs text-blue-600 underline">打开微信授权链接</a>
                  </div>
                </div>
                <div class="flex flex-wrap gap-2">
                  <button class="btn btn-primary" :disabled="wechatConnecting" @click="wechatStartConnect">{{ wechatPhase === 'error' ? '重新开始连接' : '开始连接' }}</button>
                  <button class="btn btn-secondary" :disabled="!wechatConnecting" @click="wechatCancelConnect">取消</button>
                  <button class="btn btn-secondary" :disabled="wechatStatusLoading" @click="wechatRefreshStatus">刷新状态</button>
                </div>
                <p v-if="wechatConnectError" class="mt-2 text-xs text-red-600">{{ wechatConnectError }}</p>
              </div>

              <div v-if="wechatConnected">
                <h3 class="mb-2 text-sm font-medium">2. 已绑定微信用户</h3>
                <p class="rounded border border-gray-200 p-3 text-sm dark:border-dark-700">{{ wechatUserID || '当前扫码用户' }}</p>
              </div>
            </div>

            <div class="space-y-4">
              <div>
                <h3 class="mb-2 text-sm font-medium">3. 测试推送</h3>
                <div class="space-y-2">
                  <input v-model="wechatTestMessage" class="input" placeholder="输入测试消息（可选）" :disabled="!wechatCanSend" />
                  <button class="btn btn-primary" :disabled="!wechatCanSend || wechatSending" @click="wechatSendTest">{{ wechatSending ? '发送中...' : '发送测试消息' }}</button>
                  <p v-if="!wechatCanSend && wechatConnected" class="text-xs text-gray-500 dark:text-dark-400">请先在微信里给机器人发一条消息，完成会话绑定后再发送测试消息。</p>
                </div>
              </div>
              <p class="rounded border border-gray-200 p-3 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">4. 测试成功后，上游监控告警会通过 wxclawbot CLI 发到已绑定的微信用户。</p>
            </div>
          </div>
        </div>

        <div v-else>
          <div class="mb-4 grid grid-cols-1 gap-2 md:grid-cols-4">
            <div v-for="step in feishuSteps" :key="`ding-${step.key}`" class="rounded border p-3" :class="step.key === dingTalkStepKey ? 'border-primary-400 bg-primary-50 dark:bg-primary-950/30' : 'border-gray-200 dark:border-dark-700'">
              <p class="text-xs font-semibold text-gray-900 dark:text-white">{{ step.index }}. {{ step.label }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ dingTalkStepHint(step.key) }}</p>
            </div>
          </div>

          <p v-if="dingTalkStatusMessage" class="mb-4 rounded border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-900 dark:bg-blue-950/30 dark:text-blue-200">{{ dingTalkStatusMessage }}</p>
          <p v-if="dingTalkNotice" class="mb-4 rounded border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200">{{ dingTalkNotice }}</p>

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div class="space-y-4">
              <div>
                <h3 class="mb-2 text-sm font-medium">1. 钉钉授权</h3>
                <div v-if="dingTalkVerificationUrl && (dingTalkPhase === 'starting' || dingTalkPhase === 'awaiting_authorization')" class="mb-3">
                  <p class="mb-2 text-xs">扫描二维码完成钉钉设备授权。授权后页面会自动进入下一步。</p>
                  <div class="flex flex-col items-center gap-3 rounded-lg border p-4">
                    <img v-if="dingTalkQRBase64" :src="dingTalkQRBase64" alt="钉钉授权二维码" class="h-48 w-48 rounded border" />
                    <a :href="dingTalkVerificationUrl" target="_blank" class="text-xs text-blue-600 underline">打开钉钉授权链接</a>
                    <p v-if="dingTalkAuthorizationCode" class="text-xs text-gray-500">授权码：{{ dingTalkAuthorizationCode }}</p>
                  </div>
                </div>
                <div class="flex flex-wrap gap-2">
                  <button class="btn btn-primary" :disabled="dingTalkConnecting" @click="dingTalkStartConnect">{{ dingTalkPhase === 'error' ? '重新开始连接' : '开始连接' }}</button>
                  <button class="btn btn-secondary" :disabled="!dingTalkConnecting" @click="dingTalkCancelConnect">取消</button>
                  <button class="btn btn-secondary" :disabled="dingTalkStatusLoading" @click="dingTalkRefreshStatus">刷新状态</button>
                </div>
                <p v-if="dingTalkConnectError" class="mt-2 text-xs text-red-600">{{ dingTalkConnectError }}</p>
              </div>

              <div v-if="dingTalkConnected">
                <h3 class="mb-2 text-sm font-medium">2. 创建或选择机器人</h3>
                <div class="space-y-2">
                  <div class="space-y-2 rounded border border-primary-200 bg-primary-50 p-3 dark:border-primary-900 dark:bg-primary-950/20">
                    <p class="text-xs font-medium">一键创建新的钉钉应用机器人</p>
                    <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
                      <input v-model="dingTalkCreateAppName" class="input" placeholder="应用名称（2-20 个字符）" :disabled="dingTalkCreating" />
                      <input v-model="dingTalkCreateRobotName" class="input" placeholder="机器人名称" :disabled="dingTalkCreating" />
                    </div>
                    <input v-model="dingTalkCreateDescription" class="input" placeholder="机器人功能描述（不超过 200 字）" :disabled="dingTalkCreating" />
                    <button class="btn btn-primary" :disabled="dingTalkCreating || !dingTalkCreateAppName.trim() || !dingTalkCreateRobotName.trim() || !dingTalkCreateDescription.trim()" @click="dingTalkCreateRobot">
                      {{ dingTalkCreating ? '创建中...' : '一键创建新机器人' }}
                    </button>
                    <p v-if="dingTalkCreatePhase === 'submitting_robot' || dingTalkCreatePhase === 'awaiting_robot_result'" class="text-xs text-blue-700 dark:text-blue-300">{{ dingTalkCreateStatusMessage }}</p>
                    <p v-if="dingTalkCreatePhase === 'robot_created'" class="text-xs text-emerald-700 dark:text-emerald-300">已创建 {{ dingTalkCreateRobotName }}，下面选择群聊后绑定。</p>
                  </div>

                  <p class="pt-1 text-xs font-medium text-gray-600 dark:text-dark-300">选择已有机器人</p>
                  <div class="flex flex-wrap gap-2">
                    <input v-model="dingTalkGroupQuery" class="input min-w-0 flex-1" placeholder="输入钉钉群名称关键词" />
                    <button class="btn btn-secondary" :disabled="dingTalkGroupsLoading || !dingTalkGroupQuery.trim()" @click="dingTalkSearchGroups">{{ dingTalkGroupsLoading ? '搜索中...' : '搜索群聊' }}</button>
                  </div>
                  <select v-if="dingTalkGroups.length" v-model="dingTalkGroupId" class="input" aria-label="选择钉钉告警群聊">
                    <option value="">选择告警群聊</option>
                    <option v-for="group in dingTalkGroups" :key="group.id" :value="group.id">{{ group.name || '未命名群聊' }} ({{ group.id }})</option>
                  </select>
                  <input v-else v-model="dingTalkGroupId" class="input" placeholder="未找到群聊时输入 openConversationId" />
                  <div class="flex flex-wrap gap-2">
                    <select v-if="dingTalkRobots.length" v-model="dingTalkRobotCode" class="input min-w-0 flex-1" aria-label="选择钉钉机器人">
                      <option value="">选择机器人</option>
                      <option v-for="robot in dingTalkRobots" :key="robot.robot_code || robot.id" :value="robot.robot_code || robot.id">{{ robot.name || '未命名机器人' }} ({{ robot.robot_code || robot.id }})</option>
                    </select>
                    <input v-else v-model="dingTalkRobotCode" class="input min-w-0 flex-1" placeholder="未找到机器人时输入 robotCode" />
                    <button class="btn btn-secondary" :disabled="dingTalkRobotsLoading" @click="dingTalkLoadRobots">{{ dingTalkRobotsLoading ? '查找中...' : '刷新机器人' }}</button>
                  </div>
                  <p v-if="!dingTalkRobotsLoading && !dingTalkRobots.length && dingTalkCreatePhase !== 'robot_created'" class="text-xs text-amber-700 dark:text-amber-300">没有发现已有机器人，可以使用上面的按钮创建新的机器人。</p>
                  <div class="flex flex-wrap gap-2">
                    <button class="btn btn-secondary" :disabled="!dingTalkGroupId.trim() || !dingTalkRobotCode.trim() || dingTalkTargetSaving || dingTalkCreating" @click="dingTalkSaveTarget">{{ dingTalkTargetSaving ? '保存中...' : '保存已有机器人目标' }}</button>
                    <button v-if="dingTalkCreatePhase === 'robot_created'" class="btn btn-primary" :disabled="!dingTalkGroupId.trim() || !dingTalkRobotCode.trim() || dingTalkTargetSaving" @click="dingTalkBindTarget">{{ dingTalkTargetSaving ? '绑定中...' : '绑定群并加入机器人' }}</button>
                  </div>
                </div>
              </div>
            </div>

            <div class="space-y-4">
              <div>
                <h3 class="mb-2 text-sm font-medium">3. 测试推送</h3>
                <div class="space-y-2">
                  <input v-model="dingTalkTestMessage" class="input" placeholder="输入测试消息（可选）" :disabled="!dingTalkCanSend" />
                  <button class="btn btn-primary" :disabled="!dingTalkCanSend || dingTalkSending" @click="dingTalkSendTest">{{ dingTalkSending ? '发送中...' : '发送测试消息' }}</button>
                  <p v-if="!dingTalkCanSend && dingTalkConnected" class="text-xs text-gray-500 dark:text-dark-400">先选择并保存群聊和机器人，测试按钮才会启用。</p>
                </div>
              </div>
              <p class="rounded border border-gray-200 p-3 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">4. 测试成功后，上游监控告警会通过 dws CLI 发送到已绑定的钉钉群。</p>
            </div>
          </div>
        </div>
	      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import ModelReasoningEffortTool from './plugins/model_reasoning_effort/ModelReasoningEffortTool.vue'
import LandingHomeTool from './landing/LandingHomeTool.vue'
import QRCode from 'qrcode'
import { adminAPI } from '@/api/admin'
import type { TLSFingerprintProfile } from '@/api/admin'
import { apiClient } from '@/api/client'
import { useAppStore } from '@/stores/app'
import {
  getCodexQuotaGuardStatus,
  previewClashImport,
  releaseCodexQuotaGuard,
  scanCodexQuotaGuard,
  startCodexQuotaGuard,
  stopCodexQuotaGuard,
  type ClashProxyPreviewRow,
  type ClashProxyPreviewResponse,
  type CodexQuotaGuardScanResponse,
  type CodexQuotaGuardStatus
} from './api'
import type { AdminDataImportResult, CreateAccountRequest, Proxy, ProxyProtocol } from '@/types'

type ToolTab = 'proxy' | 'reasoning' | 'landing' | 'rt' | 'guard' | 'feishu'
type IconName = InstanceType<typeof Icon>['$props']['name']
type FeishuPhase = 'idle' | 'starting' | 'awaiting_authorization' | 'connected' | 'ready' | 'sending' | 'success' | 'error'
type DingTalkPhase = FeishuPhase | 'submitting_robot' | 'awaiting_robot_result' | 'robot_created'

const feishuPhases = new Set<FeishuPhase>(['idle', 'starting', 'awaiting_authorization', 'connected', 'ready', 'sending', 'success', 'error'])
const dingTalkPhases = new Set<DingTalkPhase>([...feishuPhases, 'submitting_robot', 'awaiting_robot_result', 'robot_created'])

function resolveNotificationPhase<T extends string>(status: { error?: string; phase?: string; done?: boolean; connected?: boolean; connecting?: boolean; verification_url?: string; target_configured?: boolean }, phases: Set<T>, fallback: T, ready: T, connected: T, awaitingAuthorization: T, starting: T): T {
  if (status.error) return 'error' as T
  if (status.phase && phases.has(status.phase as T)) return status.phase as T
  if (status.done || status.connected) return status.target_configured ? ready : connected
  if (status.connecting) return status.verification_url ? awaitingAuthorization : starting
  return fallback
}

function resolveFeishuPhase(status: Parameters<typeof resolveNotificationPhase>[0]): FeishuPhase {
  return resolveNotificationPhase(status, feishuPhases, 'idle', 'ready', 'connected', 'awaiting_authorization', 'starting')
}

function resolveDingTalkPhase(status: Parameters<typeof resolveNotificationPhase>[0]): DingTalkPhase {
  return resolveNotificationPhase(status, dingTalkPhases, 'idle', 'ready', 'connected', 'awaiting_authorization', 'starting')
}

function resolveWechatPhase(status: Parameters<typeof resolveNotificationPhase>[0]): FeishuPhase {
  return resolveNotificationPhase(status, feishuPhases, 'idle', 'ready', 'connected', 'awaiting_authorization', 'starting')
}

function createFeishuQRCode(url: string): Promise<string> {
  if (!url) return Promise.resolve('')
  return QRCode.toDataURL(url, { width: 192, margin: 2, color: { dark: '#111827', light: '#ffffff' } })
}

type RtStatus = 'pending' | 'running' | 'success' | 'failed'
type RtProxyMode = 'none' | 'single' | 'round_robin'
type RtFingerprintProfileRaw = 'default' | 'generated' | 'random' | string

interface ProxyTestResult {
  success: boolean
  message: string
  latency_ms?: number
  ip_address?: string
  city?: string
  region?: string
  country?: string
  country_code?: string
}

interface RtRow {
  id: string
  index: number
  email: string
  refreshToken: string
  proxyId: number | null
  status: RtStatus
	message: string
}

interface QuotaGuardPolicyDraft {
	id: string
	name: string
	enabled: boolean
	interval_seconds: number
	account_ids_text: string
	daily_spend_limit_usd: number
	daily_token_limit: number
	weekly_spend_limit_usd: number
	weekly_token_limit: number
	daily_spend_timezone: string
	dry_run: boolean
}

const createQuotaGuardPolicyDraft = (index: number): QuotaGuardPolicyDraft => ({
	id: `limiter-${index}`,
	name: `限制器 ${index}`,
	enabled: true,
	interval_seconds: 60,
	account_ids_text: '',
	daily_spend_limit_usd: 0,
	daily_token_limit: 0,
	weekly_spend_limit_usd: 0,
	weekly_token_limit: 0,
	daily_spend_timezone: 'Asia/Shanghai',
	dry_run: false
})

const appStore = useAppStore()

const tabs: Array<{ id: ToolTab; label: string; icon: IconName }> = [
  { id: 'proxy', label: '代理导入', icon: 'server' },
  { id: 'reasoning', label: '模型推理强度', icon: 'brain' },
  { id: 'landing', label: '首页落地页', icon: 'home' },
  { id: 'rt', label: 'OpenAI RT 导入', icon: 'key' },
  { id: 'guard', label: '上游额度保护', icon: 'shield' },
  { id: 'feishu', label: '通知渠道', icon: 'bell' },
]

const activeTab = ref<ToolTab>('proxy')

function selectToolTab(tab: ToolTab) {
  activeTab.value = tab
  if (tab === 'feishu') void loadNotificationProvider()
}

const proxySubscriptionUrl = ref('')
const proxyYaml = ref('')
const proxyPreview = ref<ClashProxyPreviewResponse | null>(null)
const proxyPreviewLoading = ref(false)
const proxyImporting = ref(false)
const proxyImportResult = ref<AdminDataImportResult | null>(null)
const proxyBatchResult = ref<{ created: number; skipped: number } | null>(null)
const selectedProxyRowKeys = ref<string[]>([])
const importedProxyRowKeys = ref<string[]>([])
const proxyTestingRows = ref<Set<string>>(new Set())
const proxyBatchTesting = ref(false)
const proxyTestResults = ref<Record<string, ProxyTestResult>>({})

const selectableProxyRows = computed(() =>
  proxyPreview.value?.rows.filter((row) => row.valid && !isProxyRowImported(row)) ?? []
)
const selectedImportableProxyRows = computed(() => {
  const selected = new Set(selectedProxyRowKeys.value)
  return selectableProxyRows.value.filter((row) => selected.has(proxyRowKey(row)))
})
const allImportableProxyRowsSelected = computed(
  () => selectableProxyRows.value.length > 0 && selectedImportableProxyRows.value.length === selectableProxyRows.value.length
)
const canImportProxies = computed(() => selectedImportableProxyRows.value.length > 0)
const displayedProxyRows = computed(() => proxyPreview.value?.rows.slice(0, 300) ?? [])

const activeProxies = ref<Proxy[]>([])
const rtInput = ref('')
const lastParsedRtInput = ref('')
const rtRows = ref<RtRow[]>([])
const importingRt = ref(false)
const rtProxyMode = ref<RtProxyMode>('none')
const rtProxyId = ref<number | null>(null)
const selectedRtProxyIds = ref<number[]>([])
const rtProxySearch = ref('')
const rtProxyTestResults = ref<Record<number, ProxyTestResult>>({})
const rtTestingProxyIds = ref<Set<number>>(new Set())
const rtBatchTestingProxies = ref(false)
const rtClientId = ref('')
const rtConcurrency = ref(10)
const rtImportConcurrency = 3
const openAIMobileClientId = 'app_LlGpXReQgckcGGUo2JrYvtJK'
const tlsFingerprintProfiles = ref<TLSFingerprintProfile[]>([])
const rtEnableFingerprint = ref(false)
const rtFingerprintProfileRaw = ref<RtFingerprintProfileRaw>('default')
const generatingRtFingerprint = ref(false)
const codexQuotaGuardLoading = ref(false)
const codexQuotaGuardOperating = ref(false)
const codexQuotaGuardStatus = ref<CodexQuotaGuardStatus | null>(null)
const codexQuotaGuardLastAction = ref<CodexQuotaGuardScanResponse | null>(null)
const codexQuotaGuardPolicies = ref<QuotaGuardPolicyDraft[]>([createQuotaGuardPolicyDraft(1)])
const codexQuotaGuardAPIKey = ref('')

const selectedRtProxyId = computed(() => (rtProxyMode.value === 'single' ? rtProxyId.value : null))
const normalizedRtConcurrency = computed(() => Math.min(100, Math.max(1, Number(rtConcurrency.value) || 10)))
const rtSuccessCount = computed(() => rtRows.value.filter((row) => row.status === 'success').length)
const rtFailedCount = computed(() => rtRows.value.filter((row) => row.status === 'failed').length)

const selectedRtProxies = computed(() => {
  const selected = new Set(selectedRtProxyIds.value)
  return activeProxies.value.filter((proxy) => selected.has(proxy.id))
})

const filteredRtProxies = computed(() => {
  const query = rtProxySearch.value.trim().toLowerCase()
  if (!query) return activeProxies.value
  return activeProxies.value.filter((proxy) => {
    return (
      proxy.name.toLowerCase().includes(query) ||
      proxy.host.toLowerCase().includes(query) ||
      String(proxy.id).includes(query)
    )
  })
})

const rtProxyAssignmentLabel = computed(() => {
  if (rtProxyMode.value === 'single') {
    return selectedRtProxyId.value ? `指定代理 #${selectedRtProxyId.value}` : '不指定代理'
  }
  if (rtProxyMode.value === 'round_robin') {
    return selectedRtProxyIds.value.length > 0 ? `轮询 ${selectedRtProxyIds.value.length} 个代理` : '轮询代理未选择'
  }
  return '不指定代理'
})

const rtFingerprintLabel = computed(() => (rtEnableFingerprint.value ? '启用 TLS 指纹' : '不设置指纹'))
const rtFingerprintSourceLabel = computed(() => {
  if (!rtEnableFingerprint.value) return '-'
  if (rtFingerprintProfileRaw.value === 'default') return '内置默认'
  if (rtFingerprintProfileRaw.value === 'generated') return '系统生成 Codex 模板'
  if (rtFingerprintProfileRaw.value === 'random') return '随机已有模板'
  const id = Number.parseInt(rtFingerprintProfileRaw.value, 10)
  const profile = tlsFingerprintProfiles.value.find((item) => item.id === id)
  return profile ? profile.name : `模板 #${rtFingerprintProfileRaw.value}`
})
const codexQuotaGuardSourceLabel = computed(() => {
  const source = codexQuotaGuardStatus.value?.admin_api_key_source || 'missing'
  switch (source) {
    case 'provided':
      return 'x-api-key'
    case 'auto_created':
      return '自动创建'
    default:
      return '缺失'
  }
})
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

// --- 飞书通知 ---
interface FeishuChat {
  chat_id: string
  name: string
  chat_mode?: string
  chat_status?: string
}

const feishuSteps = [
  { index: 1, key: 'authorization', label: '应用授权' },
  { index: 2, key: 'target', label: '选择群聊' },
  { index: 3, key: 'test', label: '测试推送' },
  { index: 4, key: 'monitor', label: '监控通知' }
] as const
const feishuConnecting = ref(false)
const feishuConnected = ref(false)
const feishuPhase = ref<FeishuPhase>('idle')
const feishuVerificationUrl = ref('')
const feishuQRBase64 = ref('')
const feishuConnectError = ref('')
const feishuStatusMessage = ref('')
const feishuNotice = ref('')
const feishuChatId = ref('')
const feishuChats = ref<FeishuChat[]>([])
const feishuChatsLoading = ref(false)
const feishuStatusLoading = ref(false)
const feishuTargetSaving = ref(false)
const feishuSending = ref(false)
const feishuTestMessage = ref('')
const feishuDoctorOut = ref('')
let feishuQRGeneration = 0

type NotificationProvider = 'feishu' | 'dingtalk' | 'wechat'
interface DingTalkItem {
  id: string
  name: string
  robot_code?: string
}

const notificationProvider = ref<NotificationProvider>('feishu')
const providerLoading = ref(false)
const dingTalkConnecting = ref(false)
const dingTalkConnected = ref(false)
const dingTalkPhase = ref<DingTalkPhase>('idle')
const dingTalkVerificationUrl = ref('')
const dingTalkAuthorizationCode = ref('')
const dingTalkQRBase64 = ref('')
const dingTalkConnectError = ref('')
const dingTalkStatusMessage = ref('')
const dingTalkNotice = ref('')
const dingTalkGroupQuery = ref('')
const dingTalkGroupId = ref('')
const dingTalkRobotCode = ref('')
const dingTalkGroups = ref<DingTalkItem[]>([])
const dingTalkRobots = ref<DingTalkItem[]>([])
const dingTalkGroupsLoading = ref(false)
const dingTalkRobotsLoading = ref(false)
const dingTalkStatusLoading = ref(false)
const dingTalkTargetSaving = ref(false)
const dingTalkSending = ref(false)
const dingTalkTestMessage = ref('')
const dingTalkCreating = ref(false)
const dingTalkCreatePhase = ref<DingTalkPhase>('idle')
const dingTalkCreateTaskID = ref('')
const dingTalkCreateStatusMessage = ref('')
const dingTalkCreateAppName = ref('Sub2API 告警应用')
const dingTalkCreateRobotName = ref('Sub2API 告警机器人')
const dingTalkCreateDescription = ref('用于发送 Sub2API 上游监控告警')
let dingTalkQRGeneration = 0

const wechatSteps = [
  { index: 1, key: 'authorization', label: '扫码授权' },
  { index: 2, key: 'target', label: '绑定用户' },
  { index: 3, key: 'test', label: '测试推送' },
  { index: 4, key: 'monitor', label: '监控通知' }
] as const
const wechatConnecting = ref(false)
const wechatConnected = ref(false)
const wechatPhase = ref<FeishuPhase>('idle')
const wechatVerificationUrl = ref('')
const wechatQRBase64 = ref('')
const wechatConnectError = ref('')
const wechatStatusMessage = ref('')
const wechatNotice = ref('')
const wechatUserID = ref('')
const wechatStatusLoading = ref(false)
const wechatSending = ref(false)
const wechatTestMessage = ref('')
let wechatQRGeneration = 0

const wechatPhaseLabels: Record<FeishuPhase, string> = {
  idle: '未连接',
  starting: '启动中',
  awaiting_authorization: '等待扫码',
  connected: '已授权',
  ready: '已就绪',
  sending: '发送中',
  success: '测试成功',
  error: '连接失败'
}
const wechatPhaseLabel = computed(() => wechatPhaseLabels[wechatPhase.value])
const wechatPhaseClass = computed(() => {
  if (wechatPhase.value === 'error') return 'status-error'
  if (wechatPhase.value === 'ready' || wechatPhase.value === 'success') return 'status-ok'
  if (wechatPhase.value === 'connected') return 'status-warning'
  if (wechatPhase.value === 'starting' || wechatPhase.value === 'awaiting_authorization' || wechatPhase.value === 'sending') return 'status-running'
  return 'status-muted'
})
const wechatStepKey = computed(() => {
  if (wechatPhase.value === 'idle' || wechatPhase.value === 'starting' || wechatPhase.value === 'awaiting_authorization' || wechatPhase.value === 'error') return 'authorization'
  if (wechatPhase.value === 'connected') return 'target'
  if (wechatPhase.value === 'sending' || wechatPhase.value === 'success') return 'test'
  return 'monitor'
})
const wechatCanSend = computed(() => wechatConnected.value && (wechatPhase.value === 'ready' || wechatPhase.value === 'success'))

function wechatStepHint(key: string) {
  if (key === 'authorization') return wechatPhase.value === 'awaiting_authorization' ? '等待手机微信扫码' : '生成微信授权二维码'
  if (key === 'target') return wechatUserID.value ? '已绑定扫码用户' : '扫码后自动绑定用户'
  if (key === 'test') return wechatPhase.value === 'success' ? '消息已送达接口' : '发送一条验证消息'
  return wechatCanSend.value ? '监控告警将发送到微信' : '测试成功后启用'
}

const dingTalkPhaseLabels: Record<DingTalkPhase, string> = {
  idle: '未连接',
  starting: '启动中',
  awaiting_authorization: '等待授权',
  connected: '已授权，待选目标',
  submitting_robot: '提交创建任务',
  awaiting_robot_result: '等待机器人创建',
  robot_created: '机器人已创建，待绑定群',
  ready: '已就绪',
  sending: '发送中',
  success: '测试成功',
  error: '连接失败'
}
const dingTalkPhaseLabel = computed(() => dingTalkPhaseLabels[dingTalkPhase.value])
const dingTalkPhaseClass = computed(() => {
  if (dingTalkPhase.value === 'error') return 'status-error'
  if (dingTalkPhase.value === 'ready' || dingTalkPhase.value === 'success') return 'status-ok'
  if (dingTalkPhase.value === 'connected' || dingTalkPhase.value === 'robot_created') return 'status-warning'
  if (dingTalkPhase.value === 'starting' || dingTalkPhase.value === 'awaiting_authorization' || dingTalkPhase.value === 'submitting_robot' || dingTalkPhase.value === 'awaiting_robot_result' || dingTalkPhase.value === 'sending') return 'status-running'
  return 'status-muted'
})
const dingTalkStepKey = computed(() => {
  if (dingTalkPhase.value === 'idle' || dingTalkPhase.value === 'starting' || dingTalkPhase.value === 'awaiting_authorization' || dingTalkPhase.value === 'error') return 'authorization'
  if (dingTalkPhase.value === 'connected' || dingTalkPhase.value === 'submitting_robot' || dingTalkPhase.value === 'awaiting_robot_result' || dingTalkPhase.value === 'robot_created') return 'target'
  if (dingTalkPhase.value === 'sending' || dingTalkPhase.value === 'success') return 'test'
  return 'monitor'
})
const dingTalkCanSend = computed(() =>
  dingTalkConnected.value && Boolean(dingTalkGroupId.value.trim()) && Boolean(dingTalkRobotCode.value.trim()) && (dingTalkPhase.value === 'ready' || dingTalkPhase.value === 'success')
)

function dingTalkStepHint(key: string) {
  if (key === 'authorization') return dingTalkPhase.value === 'awaiting_authorization' ? '等待扫码完成' : '设备流授权'
  if (key === 'target') return dingTalkGroupId.value && dingTalkRobotCode.value ? '群聊和机器人已选择' : '选择群聊和机器人'
  if (key === 'test') return dingTalkPhase.value === 'success' ? '消息已送达接口' : '发送一条验证消息'
  return dingTalkPhase.value === 'ready' || dingTalkPhase.value === 'success' ? '监控告警将发送到目标群' : '测试成功后启用'
}

watch(dingTalkVerificationUrl, async (url) => {
  const generation = ++dingTalkQRGeneration
  dingTalkQRBase64.value = ''
  if (!url) return
  try {
    const dataURL = await createFeishuQRCode(url)
    if (generation === dingTalkQRGeneration) dingTalkQRBase64.value = dataURL
  } catch (error) {
    if (generation === dingTalkQRGeneration) dingTalkConnectError.value = errorMessage(error, '二维码生成失败，请使用下方链接完成授权')
  }
})

watch(wechatVerificationUrl, async (url) => {
  const generation = ++wechatQRGeneration
  wechatQRBase64.value = ''
  if (!url) return
  try {
    const dataURL = await createFeishuQRCode(url)
    if (generation === wechatQRGeneration) wechatQRBase64.value = dataURL
  } catch (error) {
    if (generation === wechatQRGeneration) wechatConnectError.value = errorMessage(error, '二维码生成失败，请使用下方链接完成授权')
  }
})

const feishuPhaseLabels: Record<FeishuPhase, string> = {
  idle: '未连接',
  starting: '启动中',
  awaiting_authorization: '等待授权',
  connected: '已授权，待选群',
  ready: '已就绪',
  sending: '发送中',
  success: '测试成功',
  error: '连接失败'
}

const feishuPhaseLabel = computed(() => feishuPhaseLabels[feishuPhase.value])
const feishuPhaseClass = computed(() => {
  if (feishuPhase.value === 'error') return 'status-error'
  if (feishuPhase.value === 'ready' || feishuPhase.value === 'success') return 'status-ok'
  if (feishuPhase.value === 'connected') return 'status-warning'
  if (feishuPhase.value === 'starting' || feishuPhase.value === 'awaiting_authorization' || feishuPhase.value === 'sending') return 'status-running'
  return 'status-muted'
})
const feishuStepKey = computed(() => {
  if (feishuPhase.value === 'idle' || feishuPhase.value === 'starting' || feishuPhase.value === 'awaiting_authorization' || feishuPhase.value === 'error') return 'authorization'
  if (feishuPhase.value === 'connected') return 'target'
  if (feishuPhase.value === 'sending' || feishuPhase.value === 'success') return 'test'
  return 'monitor'
})
const feishuCanSend = computed(() =>
  feishuConnected.value && Boolean(feishuChatId.value.trim()) && (feishuPhase.value === 'ready' || feishuPhase.value === 'success')
)

function stepHint(key: string) {
  if (key === 'authorization') return feishuPhase.value === 'awaiting_authorization' ? '等待扫码完成' : '创建并授权应用'
  if (key === 'target') return feishuChatId.value ? '目标已选择' : '选择机器人所在群'
  if (key === 'test') return feishuPhase.value === 'success' ? '消息已送达接口' : '发送一条验证消息'
  return feishuPhase.value === 'ready' || feishuPhase.value === 'success' ? '监控告警将发送到目标群' : '测试成功后启用'
}

watch(feishuVerificationUrl, async (url) => {
  const generation = ++feishuQRGeneration
  feishuQRBase64.value = ''
  if (!url) return

  try {
    const dataURL = await createFeishuQRCode(url)
    if (generation === feishuQRGeneration) feishuQRBase64.value = dataURL
  } catch (error) {
    if (generation !== feishuQRGeneration) return
    feishuConnectError.value = errorMessage(error, '二维码生成失败，请使用下方链接完成授权')
  }
})

const previewProxyFromUrl = async () => {
  await previewProxy({ url: proxySubscriptionUrl.value.trim() })
}

const previewProxyFromYaml = async () => {
  await previewProxy({ content: proxyYaml.value })
}

const proxyRowKey = (row: ClashProxyPreviewRow) => row.proxy.proxy_key || `${row.proxy.protocol}|${row.proxy.host}|${row.proxy.port}|${row.proxy.username || ''}|${row.proxy.password || ''}`

const isProxyRowImported = (row: ClashProxyPreviewRow) => importedProxyRowKeys.value.includes(proxyRowKey(row))
const isProxyRowSelected = (row: ClashProxyPreviewRow) => selectedProxyRowKeys.value.includes(proxyRowKey(row))

const toggleProxyRow = (row: ClashProxyPreviewRow, event: Event) => {
  if (!row.valid || isProxyRowImported(row)) return
  const key = proxyRowKey(row)
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  const selected = new Set(selectedProxyRowKeys.value)
  if (checked) {
    selected.add(key)
  } else {
    selected.delete(key)
  }
  selectedProxyRowKeys.value = [...selected]
}

const toggleAllProxyRows = (event: Event) => {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  selectedProxyRowKeys.value = checked ? selectableProxyRows.value.map(proxyRowKey) : []
}

const setProxyRowTesting = (key: string, testing: boolean) => {
  const next = new Set(proxyTestingRows.value)
  if (testing) {
    next.add(key)
  } else {
    next.delete(key)
  }
  proxyTestingRows.value = next
}

const proxyTestLabel = (result: ProxyTestResult) => {
  if (!result.success) return result.message || '失败'
  const parts = [result.latency_ms ? `${result.latency_ms}ms` : '', result.country || result.ip_address || '可用'].filter(Boolean)
  return parts.join(' · ')
}

const isProxyProtocol = (value: unknown): value is ProxyProtocol => {
  return value === 'http' || value === 'https' || value === 'socks5' || value === 'socks5h'
}

const testProxyRow = async (row: ClashProxyPreviewRow, notify = true) => {
  if (!row.valid || isProxyRowImported(row)) return
  if (!isProxyProtocol(row.proxy.protocol)) {
    const result = { success: false, message: '代理协议无效' }
    proxyTestResults.value = { ...proxyTestResults.value, [proxyRowKey(row)]: result }
    if (notify) appStore.showError(result.message)
    return
  }
  const key = proxyRowKey(row)
  if (proxyTestingRows.value.has(key)) return
  setProxyRowTesting(key, true)
  let temporaryProxyId: number | null = null
  try {
    const created = await adminAPI.proxies.create({
      name: `test-${row.proxy.name || row.name || row.index}`,
      protocol: row.proxy.protocol,
      host: row.proxy.host || '',
      port: row.proxy.port || 0,
      username: row.proxy.username || null,
      password: row.proxy.password || null
    })
    temporaryProxyId = created.id
    const result = await adminAPI.proxies.testProxy(created.id)
    proxyTestResults.value = { ...proxyTestResults.value, [key]: result }
    if (notify) {
      result.success ? appStore.showSuccess(`代理可用：${proxyTestLabel(result)}`) : appStore.showError(result.message || '代理测试失败')
    }
  } catch (error) {
    const result = { success: false, message: errorMessage(error, '代理测试失败') }
    proxyTestResults.value = { ...proxyTestResults.value, [key]: result }
    if (notify) appStore.showError(result.message)
  } finally {
    if (temporaryProxyId) {
      try {
        await adminAPI.proxies.delete(temporaryProxyId)
      } catch (cleanupError) {
        console.warn('Failed to delete temporary proxy after test:', cleanupError)
      }
    }
    setProxyRowTesting(key, false)
  }
}

const testSelectedProxyRows = async () => {
  if (proxyBatchTesting.value || selectedImportableProxyRows.value.length === 0) return
  proxyBatchTesting.value = true
  try {
    await runWithConcurrency(selectedImportableProxyRows.value, 4, async (row) => {
      await testProxyRow(row, false)
    })
    const passed = selectedImportableProxyRows.value.filter((row) => proxyTestResults.value[proxyRowKey(row)]?.success).length
    appStore.showSuccess(`测试完成：可用 ${passed}/${selectedImportableProxyRows.value.length}`)
  } finally {
    proxyBatchTesting.value = false
  }
}

const previewProxy = async (payload: { url?: string; content?: string }) => {
  proxyPreviewLoading.value = true
  proxyImportResult.value = null
  proxyBatchResult.value = null
  selectedProxyRowKeys.value = []
  importedProxyRowKeys.value = []
  proxyTestResults.value = {}
  try {
    proxyPreview.value = await previewClashImport(payload)
    selectedProxyRowKeys.value = proxyPreview.value.rows.filter((row) => row.valid).map(proxyRowKey)
    appStore.showSuccess(`解析完成：有效 ${proxyPreview.value.summary.valid} 条`)
  } catch (error) {
    appStore.showError(errorMessage(error, '解析失败'))
  } finally {
    proxyPreviewLoading.value = false
  }
}

const importProxyData = async () => {
  if (!proxyPreview.value || !canImportProxies.value) return
  const selectedKeys = new Set(selectedImportableProxyRows.value.map(proxyRowKey))
  proxyImporting.value = true
  proxyBatchResult.value = null
  try {
    proxyImportResult.value = await adminAPI.proxies.importData({
      data: {
        ...proxyPreview.value.data_payload,
        proxies: proxyPreview.value.data_payload.proxies.filter((proxy) => selectedKeys.has(proxy.proxy_key))
      }
    })
    importedProxyRowKeys.value = [...new Set([...importedProxyRowKeys.value, ...selectedKeys])]
    selectedProxyRowKeys.value = selectedProxyRowKeys.value.filter((key) => !selectedKeys.has(key))
    appStore.showSuccess(`导入完成：创建 ${proxyImportResult.value.proxy_created}，复用 ${proxyImportResult.value.proxy_reused}`)
  } catch (error) {
    appStore.showError(errorMessage(error, '导入失败'))
  } finally {
    proxyImporting.value = false
  }
}

const resetProxyImport = () => {
  proxySubscriptionUrl.value = ''
  proxyYaml.value = ''
  proxyPreview.value = null
  proxyImportResult.value = null
  proxyBatchResult.value = null
  selectedProxyRowKeys.value = []
  importedProxyRowKeys.value = []
  proxyTestResults.value = {}
}

const parseRtInput = () => {
  const rows = extractRtRows(rtInput.value)
  rtRows.value = rows
  lastParsedRtInput.value = rtInput.value
  if (rows.length === 0) {
    appStore.showWarning('没有提取到 rt_ 或 rt- token')
    return
  }
  if (rtProxyMode.value === 'round_robin' && selectedRtProxyIds.value.length > 0) {
    assignRtProxies(false)
  }
  appStore.showSuccess(`提取到 ${rows.length} 条 RT`)
}

const importRtRows = async () => {
  if (rtRows.value.length === 0 || rtInput.value !== lastParsedRtInput.value) {
    parseRtInput()
  }
  const rows = rtRows.value
  if (rows.length === 0) return
  if (rtProxyMode.value === 'round_robin') {
    if (selectedRtProxyIds.value.length === 0) {
      appStore.showWarning('请选择至少一个代理用于自动分配')
      return
    }
    assignRtProxies(false)
  }

  importingRt.value = true
  rows.forEach((row) => {
    row.status = 'pending'
    row.message = ''
  })
  rtRows.value = [...rows]

  try {
    const fingerprintProfileId = await resolveRtFingerprintProfileId()
    await runWithConcurrency(rows, rtImportConcurrency, (row, index) =>
      importOneRtRow(row, index, fingerprintProfileId)
    )
    const success = rtSuccessCount.value
    const failed = rtFailedCount.value
    if (success > 0 && failed === 0) {
      appStore.showSuccess(`RT 导入完成：成功 ${success} 条`)
    } else if (success > 0) {
      appStore.showWarning(`RT 部分导入完成：成功 ${success}，失败 ${failed}`)
    } else {
      appStore.showError('RT 导入失败')
    }
  } catch (error) {
    appStore.showError(errorMessage(error, 'RT 导入失败'))
  } finally {
    importingRt.value = false
  }
}

const importOneRtRow = async (row: RtRow, index: number, fingerprintProfileId: number | null) => {
  updateRtRow(row, { status: 'running', message: '刷新 token 中...' })
  try {
    const clientId = rtClientId.value.trim() || undefined
    const proxyId = resolveRtRowProxyId(row, index)
    const tokenInfo = await adminAPI.accounts.refreshOpenAIToken(
      row.refreshToken,
      proxyId,
      '/admin/openai/refresh-token',
      clientId
    )
    const credentials = buildOpenAICredentials(tokenInfo, row.refreshToken, clientId)
    const extra = buildOpenAIExtra(tokenInfo, fingerprintProfileId)
    const email = readString(tokenInfo, 'email') || row.email
    const accountName = email || `OpenAI OAuth Account #${row.index}`
    const payload: CreateAccountRequest = {
      name: accountName,
      notes: null,
      platform: 'openai',
      type: 'oauth',
      credentials,
      extra,
      proxy_id: proxyId,
      concurrency: normalizedRtConcurrency.value,
      priority: 1,
      rate_multiplier: 1,
      group_ids: [],
      expires_at: null,
      auto_pause_on_expired: true
    }
    await adminAPI.accounts.create(payload)
    const proxySuffix = proxyId ? ` · 代理 #${proxyId}` : ''
    updateRtRow(row, { status: 'success', message: `已创建 ${accountName}${proxySuffix}` })
  } catch (error) {
    updateRtRow(row, { status: 'failed', message: errorMessage(error, '导入失败') })
  }
}

const loadActiveProxies = async () => {
  try {
    activeProxies.value = await adminAPI.proxies.getAllWithCount()
  } catch {
    activeProxies.value = []
  }
}

const loadTLSFingerprintProfiles = async () => {
  try {
    tlsFingerprintProfiles.value = await adminAPI.tlsFingerprintProfiles.list()
  } catch {
    tlsFingerprintProfiles.value = []
  }
}

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
      account_ids_text: (policy.account_ids || []).join(', '),
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

const parseCodexQuotaGuardAccountIDs = (value: string): number[] => {
  const ids = value
    .split(',')
    .map((item) => Number.parseInt(item.trim(), 10))
    .filter((id) => Number.isInteger(id) && id > 0)
  return [...new Set(ids)].slice(0, 500)
}

const addCodexQuotaGuardPolicy = () => {
  codexQuotaGuardPolicies.value.push(createQuotaGuardPolicyDraft(codexQuotaGuardPolicies.value.length + 1))
}

const removeCodexQuotaGuardPolicy = (index: number) => {
  if (codexQuotaGuardPolicies.value.length <= 1) return
  codexQuotaGuardPolicies.value.splice(index, 1)
}

const startCodexQuotaGuardTask = async () => {
  codexQuotaGuardOperating.value = true
  try {
    codexQuotaGuardStatus.value = await startCodexQuotaGuard(
      {
        policies: codexQuotaGuardPolicies.value.map((policy) => ({
          id: policy.id.trim(),
          name: policy.name.trim(),
          enabled: policy.enabled,
          interval_seconds: policy.interval_seconds,
          account_ids: parseCodexQuotaGuardAccountIDs(policy.account_ids_text),
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
    await loadCodexQuotaGuardStatus()
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
    await loadCodexQuotaGuardStatus()
    appStore.showSuccess('已释放 Guard 封禁账号')
  } catch (error) {
    appStore.showError(errorMessage(error, '释放失败'))
  } finally {
    codexQuotaGuardOperating.value = false
  }
}

const extractRtRows = (input: string): RtRow[] => {
  const emailRE = /[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}/gi
  const rtRE = /rt[_-][A-Za-z0-9._-]+/g
  const rows: RtRow[] = []
  const seen = new Set<string>()

  input.split(/\r?\n/).forEach((line) => {
    const emails = line.match(emailRE) || []
    const tokens = line.match(rtRE) || []
    for (const token of tokens) {
      if (seen.has(token)) continue
      seen.add(token)
      const email = emails[0] || ''
      rows.push({
        id: `${rows.length + 1}-${token.slice(0, 10)}`,
        index: rows.length + 1,
        email,
        refreshToken: token,
        proxyId: null,
        status: 'pending',
        message: ''
      })
    }
  })

  if (rows.length > 0) return rows

  const allEmails = input.match(emailRE) || []
  const allTokens = input.match(rtRE) || []
  allTokens.forEach((token, index) => {
    if (seen.has(token)) return
    seen.add(token)
    rows.push({
      id: `${rows.length + 1}-${token.slice(0, 10)}`,
      index: rows.length + 1,
      email: allEmails[index] || '',
      refreshToken: token,
      proxyId: null,
      status: 'pending',
      message: ''
    })
  })
  return rows
}

const runWithConcurrency = async <T,>(
  items: T[],
  concurrency: number,
  worker: (item: T, index: number) => Promise<void>
) => {
  let next = 0
  const runners = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (next < items.length) {
      const index = next++
      await worker(items[index], index)
    }
  })
  await Promise.all(runners)
}

const updateRtRow = (row: RtRow, patch: Partial<RtRow>) => {
  Object.assign(row, patch)
  rtRows.value = [...rtRows.value]
}

const toggleRtProxySelection = (proxyId: number, event: Event) => {
  const checked = (event.target as HTMLInputElement | null)?.checked ?? false
  const selected = new Set(selectedRtProxyIds.value)
  if (checked) {
    selected.add(proxyId)
  } else {
    selected.delete(proxyId)
  }
  selectedRtProxyIds.value = activeProxies.value
    .map((proxy) => proxy.id)
    .filter((id) => selected.has(id))
  if (rtProxyMode.value === 'round_robin' && rtRows.value.length > 0) {
    assignRtProxies(false)
  }
}

const setRtProxyTesting = (proxyId: number, testing: boolean) => {
  const next = new Set(rtTestingProxyIds.value)
  if (testing) {
    next.add(proxyId)
  } else {
    next.delete(proxyId)
  }
  rtTestingProxyIds.value = next
}

const testRtProxy = async (proxyId: number, notify = true) => {
  if (rtTestingProxyIds.value.has(proxyId)) return
  setRtProxyTesting(proxyId, true)
  try {
    const result = await adminAPI.proxies.testProxy(proxyId)
    rtProxyTestResults.value = {
      ...rtProxyTestResults.value,
      [proxyId]: result
    }
    if (notify) {
      if (result.success) {
        appStore.showSuccess(result.latency_ms ? `代理可用：${result.latency_ms}ms` : '代理可用')
      } else {
        appStore.showError(result.message || '代理测试失败')
      }
    }
  } catch (error) {
    const result = { success: false, message: errorMessage(error, '代理测试失败') }
    rtProxyTestResults.value = {
      ...rtProxyTestResults.value,
      [proxyId]: result
    }
    if (notify) {
      appStore.showError(result.message)
    }
  } finally {
    setRtProxyTesting(proxyId, false)
  }
}

const testAllRtProxies = async () => {
  if (rtBatchTestingProxies.value || activeProxies.value.length === 0) return
  const proxyIds = activeProxies.value.map((proxy) => proxy.id)
  rtBatchTestingProxies.value = true
  try {
    await runWithConcurrency(proxyIds, 3, async (proxyId) => {
      await testRtProxy(proxyId, false)
    })
    const passed = proxyIds.filter((id) => rtProxyTestResults.value[id]?.success).length
    appStore.showSuccess(`代理测试完成：可用 ${passed}/${proxyIds.length}`)
  } finally {
    rtBatchTestingProxies.value = false
  }
}

const assignRtProxies = (notify: boolean) => {
  const proxies = selectedRtProxies.value
  if (proxies.length === 0 || rtRows.value.length === 0) return
  rtRows.value.forEach((row, index) => {
    row.proxyId = proxies[index % proxies.length].id
  })
  rtRows.value = [...rtRows.value]
  if (notify) {
    appStore.showSuccess(`已按顺序分配 ${proxies.length} 个代理到 ${rtRows.value.length} 条 RT`)
  }
}

const resolveRtRowProxyId = (row: RtRow, index: number): number | null => {
  if (rtProxyMode.value === 'single') return selectedRtProxyId.value
  if (rtProxyMode.value !== 'round_robin') return null
  if (row.proxyId) return row.proxyId
  const proxies = selectedRtProxies.value
  if (proxies.length === 0) return null
  return proxies[index % proxies.length].id
}

const buildOpenAICredentials = (
  tokenInfo: Record<string, unknown>,
  fallbackRefreshToken: string,
  clientId?: string
): Record<string, unknown> => {
  const accessToken = readString(tokenInfo, 'access_token')
  if (!accessToken) {
    throw new Error('refresh-token response missing access_token')
  }

  const credentials: Record<string, unknown> = {
    access_token: accessToken
  }
  const expiresAt = normalizeExpiresAt(tokenInfo.expires_at, tokenInfo.expires_in)
  if (expiresAt) {
    credentials.expires_at = expiresAt
  }

  const refreshToken = readString(tokenInfo, 'refresh_token') || fallbackRefreshToken
  if (refreshToken) credentials.refresh_token = refreshToken

  for (const key of [
    'id_token',
    'email',
    'chatgpt_account_id',
    'chatgpt_user_id',
    'organization_id',
    'plan_type',
    'subscription_expires_at'
  ]) {
    const value = readString(tokenInfo, key)
    if (value) credentials[key] = value
  }
  if (clientId) credentials.client_id = clientId
  return credentials
}

const buildOpenAIExtra = (
  tokenInfo: Record<string, unknown>,
  fingerprintProfileId: number | null
): Record<string, unknown> | undefined => {
  const extra: Record<string, unknown> = {}
  for (const key of ['email', 'name', 'privacy_mode']) {
    const value = readString(tokenInfo, key)
    if (value) extra[key] = value
  }
  if (rtEnableFingerprint.value) {
    extra.enable_tls_fingerprint = true
    if (fingerprintProfileId !== null) {
      extra.tls_fingerprint_profile_id = fingerprintProfileId
    }
  }
  return Object.keys(extra).length > 0 ? extra : undefined
}

const resolveRtFingerprintProfileId = async (): Promise<number | null> => {
  if (!rtEnableFingerprint.value) return null
  if (rtFingerprintProfileRaw.value === 'default') return null
  if (rtFingerprintProfileRaw.value === 'random') return -1
  if (rtFingerprintProfileRaw.value === 'generated') {
    const profile = await ensureGeneratedCodexFingerprintProfile()
    return profile.id
  }
  const id = Number.parseInt(rtFingerprintProfileRaw.value, 10)
  return Number.isFinite(id) && id > 0 ? id : null
}

const codexGeneratedTLSProfileName = 'Codex reqwest rustls (generated)'

const codexGeneratedTLSProfile = {
  name: codexGeneratedTLSProfileName,
  description: 'Generated from Codex source: reqwest 0.12 + rustls 0.23 using the ring crypto provider.',
  enable_grease: false,
  cipher_suites: [4865, 4866, 4867, 49195, 49199, 49196, 49200, 52393, 52392],
  curves: [29, 23, 24],
  point_formats: [0],
  signature_algorithms: [1027, 1283, 1539, 2052, 2053, 2054, 1025, 1281, 1537],
  alpn_protocols: ['h2', 'http/1.1'],
  supported_versions: [772, 771],
  key_share_groups: [29],
  psk_modes: [1],
  extensions: [0, 10, 13, 16, 5, 51, 45, 43]
}

const ensureGeneratedCodexFingerprintProfile = async (): Promise<TLSFingerprintProfile> => {
  const existing = tlsFingerprintProfiles.value.find((profile) => profile.name === codexGeneratedTLSProfileName)
  if (existing) return existing

  generatingRtFingerprint.value = true
  try {
    const created = await adminAPI.tlsFingerprintProfiles.create(codexGeneratedTLSProfile)
    tlsFingerprintProfiles.value = [...tlsFingerprintProfiles.value, created]
    return created
  } finally {
    generatingRtFingerprint.value = false
  }
}

const generateAndSelectCodexFingerprint = async () => {
  if (!rtEnableFingerprint.value) return
  try {
    const profile = await ensureGeneratedCodexFingerprintProfile()
    rtFingerprintProfileRaw.value = String(profile.id)
    appStore.showSuccess(`已选择指纹模板：${profile.name}`)
  } catch (error) {
    appStore.showError(errorMessage(error, '生成指纹失败'))
  }
}

const readString = (source: Record<string, unknown>, key: string): string => {
  const value = source[key]
  return typeof value === 'string' ? value.trim() : ''
}

const normalizeExpiresAt = (expiresAt: unknown, expiresIn: unknown): string => {
  if (typeof expiresAt === 'number' && expiresAt > 0) {
    return new Date(expiresAt * 1000).toISOString()
  }
  if (typeof expiresAt === 'string' && expiresAt.trim()) {
    const numeric = Number(expiresAt)
    if (Number.isFinite(numeric) && numeric > 0) {
      return new Date(numeric * 1000).toISOString()
    }
    return expiresAt.trim()
  }
  if (typeof expiresIn === 'number' && expiresIn > 0) {
    return new Date(Date.now() + expiresIn * 1000).toISOString()
  }
  return new Date(Date.now() + 3600 * 1000).toISOString()
}

const proxyLabel = (proxy: Proxy) => {
  const count = typeof proxy.account_count === 'number' ? ` · ${proxy.account_count} 账号` : ''
  return `#${proxy.id} ${proxy.name} · ${proxy.protocol}://${proxy.host}:${proxy.port}${count}`
}

const rtProxyTestLabel = (proxyId: number) => {
  const result = rtProxyTestResults.value[proxyId]
  if (!result) return '未测试'
  if (!result.success) return '失败'
  const parts = [result.country, result.latency_ms ? `${result.latency_ms}ms` : '可用'].filter(Boolean)
  return parts.join(' · ')
}

const rtRowProxyLabel = (row: RtRow) => {
  const proxyId = rtProxyMode.value === 'round_robin'
    ? row.proxyId
    : rtProxyMode.value === 'single'
      ? selectedRtProxyId.value
      : null
  if (!proxyId) return '-'
  const proxy = activeProxies.value.find((item) => item.id === proxyId)
  return proxy ? proxyLabel(proxy) : `#${proxyId}`
}

const maskToken = (token: string) => {
  if (token.length <= 14) return token
  return `${token.slice(0, 8)}...${token.slice(-6)}`
}

const rtStatusLabel = (status: RtStatus) => {
  switch (status) {
    case 'running':
      return '处理中'
    case 'success':
      return '成功'
    case 'failed':
      return '失败'
    default:
      return '待处理'
  }
}

const rtStatusClass = (status: RtStatus) => {
  switch (status) {
    case 'running':
      return 'status-running'
    case 'success':
      return 'status-ok'
    case 'failed':
      return 'status-error'
    default:
      return 'status-muted'
  }
}

const errorMessage = (error: unknown, fallback: string) => {
  if (error && typeof error === 'object') {
    const maybe = error as { message?: unknown; response?: { data?: { detail?: unknown; message?: unknown } } }
    const detail = maybe.response?.data?.detail || maybe.response?.data?.message || maybe.message
    if (typeof detail === 'string' && detail.trim()) return detail
  }
  return fallback
}

onMounted(() => {
  void loadActiveProxies()
  void loadTLSFingerprintProfiles()
  void loadCodexQuotaGuardStatus()
})
// --- 飞书通知 API ---
async function feishuStartConnect() {
  feishuConnecting.value = true
  feishuPhase.value = 'starting'
  feishuConnectError.value = ''
  feishuNotice.value = ''
  feishuStatusMessage.value = '正在启动飞书应用授权...'
  feishuVerificationUrl.value = ''
  feishuQRBase64.value = ''
  try {
    await apiClient.post('/admin/feishu/connect/start', {})
    feishuConnected.value = false
  } catch (e: any) {
    feishuConnectError.value = e?.response?.data?.message || e.message
    feishuConnecting.value = false
    feishuPhase.value = 'error'
    feishuStatusMessage.value = '连接启动失败，请检查容器日志后重试。'
  }
  if (feishuConnecting.value) pollFeishuStatus()
}

async function pollFeishuStatus() {
  while (feishuConnecting.value) {
    await new Promise(r => setTimeout(r, 3000))
    if (!feishuConnecting.value) return
    try {
      const { data } = await apiClient.get('/admin/feishu/connect/status')
      if (data.verification_url) feishuVerificationUrl.value = data.verification_url
      feishuPhase.value = resolveFeishuPhase(data)
      feishuStatusMessage.value = data.status_message || (feishuPhase.value === 'awaiting_authorization' ? '请扫描二维码完成应用授权。' : feishuStatusMessage.value)
      if (data.error) {
        feishuConnectError.value = data.error
        feishuConnecting.value = false
        feishuConnected.value = false
        feishuPhase.value = 'error'
        return
      }
      if (data.done) {
        feishuConnecting.value = false
        feishuConnected.value = true
        feishuPhase.value = 'connected'
        await feishuRefreshStatus()
        return
      }
    } catch (e: any) {
      feishuConnectError.value = e?.response?.data?.message || e.message
      feishuConnecting.value = false
      feishuPhase.value = 'error'
      feishuStatusMessage.value = '连接状态获取失败，请刷新状态重试。'
    }
  }
}

function feishuCancelConnect() {
  feishuConnecting.value = false
  feishuPhase.value = 'idle'
  feishuStatusMessage.value = '连接已取消，可以重新开始授权。'
  feishuConnectError.value = ''
  apiClient.post('/admin/feishu/connect/cancel', {}).catch(() => {})
}

async function feishuRefreshStatus() {
  feishuStatusLoading.value = true
  try {
    const { data } = await apiClient.get('/admin/feishu/status')
    feishuConnected.value = !!data.connected
    feishuPhase.value = resolveFeishuPhase(data)
    feishuStatusMessage.value = data.status_message || (feishuConnected.value ? '应用已授权，请选择机器人所在群聊。' : '尚未完成飞书应用授权。')
    feishuDoctorOut.value = data.config || data.doctor || ''
    feishuChatId.value = data.chat_id || ''
    if (feishuConnected.value) await feishuLoadChats()
  } catch (e: any) {
    feishuConnected.value = false
    feishuPhase.value = 'error'
    feishuStatusMessage.value = '无法读取飞书连接状态。'
  } finally {
    feishuStatusLoading.value = false
  }
}

async function feishuLoadChats() {
  feishuChatsLoading.value = true
  try {
    const { data } = await apiClient.get('/admin/feishu/chats')
    feishuChats.value = Array.isArray(data.chats) ? data.chats : []
    if (!feishuChatId.value && !feishuChats.value.length) {
      feishuStatusMessage.value = '应用已授权，但还没有发现机器人所在的群，请先把机器人加入群后刷新。'
    } else if (!feishuChatId.value) {
      feishuStatusMessage.value = '应用已授权，请选择机器人所在群聊并保存告警目标。'
    }
  } catch (e: any) {
    feishuChats.value = []
    feishuConnectError.value = e?.response?.data?.message || e.message
  } finally {
    feishuChatsLoading.value = false
  }
}

async function feishuSaveTarget() {
  feishuTargetSaving.value = true
  feishuConnectError.value = ''
  try {
    await apiClient.put('/admin/feishu/target', { chat_id: feishuChatId.value.trim() })
    feishuPhase.value = 'ready'
    feishuStatusMessage.value = '告警目标已保存，现在可以发送测试消息。'
    feishuNotice.value = `已绑定群聊 ${feishuChatId.value.trim()}`
  } catch (e: any) {
    feishuConnectError.value = e?.response?.data?.message || e.message
  } finally {
    feishuTargetSaving.value = false
  }
}

async function feishuSendTest() {
  feishuSending.value = true
  feishuPhase.value = 'sending'
  feishuConnectError.value = ''
  feishuNotice.value = ''
  try {
    await apiClient.post('/admin/feishu/send-test', { markdown: feishuTestMessage.value || undefined })
    feishuTestMessage.value = ''
    feishuPhase.value = 'success'
    feishuStatusMessage.value = '测试消息已发送，飞书通知链路已打通。'
    feishuNotice.value = '测试发送成功，后续上游监控告警会发送到这个群。'
  } catch (e: any) {
    feishuConnectError.value = e?.response?.data?.message || e.message
    feishuPhase.value = 'ready'
    feishuStatusMessage.value = '测试发送失败，请检查机器人是否仍在群内及群 ID 是否正确。'
  } finally {
    feishuSending.value = false
  }
}

async function wechatStartConnect() {
  wechatConnecting.value = true
  wechatConnected.value = false
  wechatPhase.value = 'starting'
  wechatConnectError.value = ''
  wechatNotice.value = ''
  wechatStatusMessage.value = '正在获取微信授权二维码...'
  wechatVerificationUrl.value = ''
  wechatQRBase64.value = ''
  try {
    await apiClient.post('/admin/wechat/connect/start', {})
  } catch (error) {
    wechatConnecting.value = false
    wechatPhase.value = 'error'
    wechatConnectError.value = errorMessage(error, '微信授权启动失败。')
    wechatStatusMessage.value = '连接启动失败，请检查容器中的 wxclawbot CLI 后重试。'
    return
  }
  void pollWechatStatus()
}

async function pollWechatStatus() {
  while (wechatConnecting.value) {
    await new Promise((resolve) => setTimeout(resolve, 1500))
    if (!wechatConnecting.value) return
    try {
      const { data } = await apiClient.get('/admin/wechat/connect/status')
      if (data.verification_url) wechatVerificationUrl.value = data.verification_url
      if (data.user_id) wechatUserID.value = data.user_id
      wechatPhase.value = resolveWechatPhase(data)
      wechatStatusMessage.value = data.status_message || wechatStatusMessage.value
      if (data.error) {
        wechatConnecting.value = false
        wechatConnected.value = false
        wechatPhase.value = 'error'
        wechatConnectError.value = data.error
        return
      }
      if (data.done) {
        wechatConnecting.value = false
        wechatConnected.value = true
        wechatPhase.value = 'connected'
        await wechatRefreshStatus()
        return
      }
    } catch (error) {
      wechatConnecting.value = false
      wechatPhase.value = 'error'
      wechatConnectError.value = errorMessage(error, '微信连接状态获取失败，请刷新状态重试。')
      return
    }
  }
}

function wechatCancelConnect() {
  wechatConnecting.value = false
  wechatConnected.value = false
  wechatPhase.value = 'idle'
  wechatVerificationUrl.value = ''
  wechatQRBase64.value = ''
  wechatStatusMessage.value = '连接已取消，可以重新开始扫码。'
  wechatConnectError.value = ''
  void apiClient.post('/admin/wechat/connect/cancel', {}).catch(() => {})
}

async function wechatRefreshStatus() {
  wechatStatusLoading.value = true
  try {
    const { data } = await apiClient.get('/admin/wechat/status')
    wechatConnected.value = !!data.connected
    wechatPhase.value = resolveWechatPhase(data)
    wechatStatusMessage.value = data.status_message || (wechatConnected.value ? '微信已连接，可以发送测试消息。' : '尚未完成微信扫码授权。')
    wechatUserID.value = data.user_id || ''
  } catch (error) {
    wechatConnected.value = false
    wechatPhase.value = 'error'
    wechatConnectError.value = errorMessage(error, '无法读取微信连接状态。')
  } finally {
    wechatStatusLoading.value = false
  }
}

async function wechatSendTest() {
  wechatSending.value = true
  wechatPhase.value = 'sending'
  wechatConnectError.value = ''
  wechatNotice.value = ''
  try {
    await apiClient.post('/admin/wechat/send-test', { text: wechatTestMessage.value || undefined })
    wechatTestMessage.value = ''
    wechatPhase.value = 'success'
    wechatStatusMessage.value = '测试消息已发送，微信通知链路已打通。'
    wechatNotice.value = '测试发送成功，后续上游监控告警会发送到这个微信用户。'
  } catch (error) {
    wechatConnectError.value = errorMessage(error, '微信测试发送失败，请重新扫码或检查网络。')
    wechatPhase.value = 'ready'
    wechatStatusMessage.value = '测试发送失败，请检查微信授权状态。'
  } finally {
    wechatSending.value = false
  }
}

async function loadNotificationProvider() {
  providerLoading.value = true
  try {
    const { data } = await apiClient.get('/admin/notifications/providers')
    if (data.selected === 'dingtalk' || data.selected === 'feishu' || data.selected === 'wechat') notificationProvider.value = data.selected
    if (notificationProvider.value === 'dingtalk') await dingTalkRefreshStatus()
    else if (notificationProvider.value === 'wechat') await wechatRefreshStatus()
    else await feishuRefreshStatus()
  } catch (error) {
    feishuStatusMessage.value = errorMessage(error, '无法读取通知通道配置。')
  } finally {
    providerLoading.value = false
  }
}

async function selectNotificationProvider(provider: NotificationProvider) {
  if (provider === notificationProvider.value) return
  providerLoading.value = true
  try {
    await apiClient.put('/admin/notifications/provider', { provider })
    notificationProvider.value = provider
    if (provider === 'dingtalk') await dingTalkRefreshStatus()
    else if (provider === 'wechat') await wechatRefreshStatus()
    else await feishuRefreshStatus()
  } catch (error) {
    dingTalkConnectError.value = errorMessage(error, '切换通知方案失败。')
  } finally {
    providerLoading.value = false
  }
}

async function dingTalkStartConnect() {
  dingTalkConnecting.value = true
  dingTalkPhase.value = 'starting'
  dingTalkConnectError.value = ''
  dingTalkNotice.value = ''
  dingTalkStatusMessage.value = '正在启动钉钉设备授权...'
  dingTalkVerificationUrl.value = ''
  dingTalkQRBase64.value = ''
  try {
    await apiClient.post('/admin/dingtalk/connect/start', {})
  } catch (error) {
    dingTalkConnecting.value = false
    dingTalkPhase.value = 'error'
    dingTalkConnectError.value = errorMessage(error, '钉钉授权启动失败。')
    dingTalkStatusMessage.value = '连接启动失败，请检查容器中的 dws CLI 后重试。'
    return
  }
  void pollDingTalkStatus()
}

async function pollDingTalkStatus() {
  while (dingTalkConnecting.value) {
    await new Promise((resolve) => setTimeout(resolve, 3000))
    if (!dingTalkConnecting.value) return
    try {
      const { data } = await apiClient.get('/admin/dingtalk/connect/status')
      if (data.verification_url) dingTalkVerificationUrl.value = data.verification_url
      if (data.authorization_code) dingTalkAuthorizationCode.value = data.authorization_code
      dingTalkPhase.value = resolveDingTalkPhase(data)
      dingTalkStatusMessage.value = data.status_message || dingTalkStatusMessage.value
      if (data.error) {
        dingTalkConnecting.value = false
        dingTalkConnected.value = false
        dingTalkPhase.value = 'error'
        dingTalkConnectError.value = data.error
        return
      }
      if (data.done) {
        dingTalkConnecting.value = false
        dingTalkConnected.value = true
        dingTalkPhase.value = 'connected'
        await dingTalkRefreshStatus()
        return
      }
    } catch (error) {
      dingTalkConnecting.value = false
      dingTalkPhase.value = 'error'
      dingTalkConnectError.value = errorMessage(error, '连接状态获取失败，请刷新状态重试。')
      return
    }
  }
}

function dingTalkCancelConnect() {
  dingTalkConnecting.value = false
  dingTalkPhase.value = 'idle'
  dingTalkVerificationUrl.value = ''
  dingTalkQRBase64.value = ''
  dingTalkStatusMessage.value = '连接已取消，可以重新开始授权。'
  dingTalkConnectError.value = ''
  void apiClient.post('/admin/dingtalk/connect/cancel', {}).catch(() => {})
}

async function dingTalkRefreshStatus() {
  dingTalkStatusLoading.value = true
  try {
    const { data } = await apiClient.get('/admin/dingtalk/status')
    dingTalkConnected.value = !!data.connected
    dingTalkPhase.value = resolveDingTalkPhase(data)
    dingTalkStatusMessage.value = data.status_message || (dingTalkConnected.value ? '钉钉已授权，请选择群聊和机器人。' : '尚未完成钉钉授权。')
    dingTalkGroupId.value = data.group_id || ''
    dingTalkRobotCode.value = data.robot_code || ''
    dingTalkCreatePhase.value = data.create_phase || 'idle'
    dingTalkCreateTaskID.value = data.create_task_id || ''
    dingTalkCreateStatusMessage.value = data.create_status_message || ''
    if (data.app_name) dingTalkCreateAppName.value = data.app_name
    if (data.robot_name) dingTalkCreateRobotName.value = data.robot_name
    if (dingTalkConnected.value) await dingTalkLoadRobots()
  } catch (error) {
    dingTalkConnected.value = false
    dingTalkPhase.value = 'error'
    dingTalkConnectError.value = errorMessage(error, '无法读取钉钉连接状态。')
  } finally {
    dingTalkStatusLoading.value = false
  }
}

async function dingTalkCreateRobot() {
  if (!window.confirm(`确认创建钉钉应用机器人“${dingTalkCreateRobotName.value.trim()}”吗？这会在当前钉钉企业中创建一个新的应用和机器人。`)) return
  dingTalkCreating.value = true
  dingTalkCreatePhase.value = 'submitting_robot'
  dingTalkConnectError.value = ''
  dingTalkNotice.value = ''
  try {
    await apiClient.post('/admin/dingtalk/robot/create', {
      app_name: dingTalkCreateAppName.value.trim(),
      robot_name: dingTalkCreateRobotName.value.trim(),
      description: dingTalkCreateDescription.value.trim()
    })
    dingTalkCreateStatusMessage.value = '正在提交钉钉机器人创建任务...'
    void pollDingTalkRobotCreate()
  } catch (error) {
    dingTalkCreating.value = false
    dingTalkCreatePhase.value = 'error'
    dingTalkConnectError.value = errorMessage(error, '创建钉钉机器人失败。')
  }
}

async function pollDingTalkRobotCreate() {
  while (dingTalkCreating.value) {
    await new Promise((resolve) => setTimeout(resolve, 2000))
    if (!dingTalkCreating.value) return
    try {
      const { data } = await apiClient.get('/admin/dingtalk/robot/create/status')
      dingTalkCreatePhase.value = data.phase || 'idle'
      dingTalkCreateTaskID.value = data.task_id || ''
      dingTalkCreateStatusMessage.value = data.status_message || ''
      if (data.error) {
        dingTalkCreating.value = false
        dingTalkCreatePhase.value = 'error'
        dingTalkConnectError.value = data.error
        return
      }
      if (data.robot_code) {
        dingTalkRobotCode.value = data.robot_code
        if (data.robot_name) dingTalkCreateRobotName.value = data.robot_name
      }
      if (data.phase === 'robot_created') {
        dingTalkCreating.value = false
        dingTalkConnected.value = true
        dingTalkPhase.value = 'robot_created'
        dingTalkStatusMessage.value = data.status_message || '机器人已创建，请选择群聊并绑定。'
        return
      }
    } catch (error) {
      dingTalkCreating.value = false
      dingTalkCreatePhase.value = 'error'
      dingTalkConnectError.value = errorMessage(error, '获取机器人创建状态失败。')
      return
    }
  }
}

async function dingTalkSearchGroups() {
  const query = dingTalkGroupQuery.value.trim()
  if (!query) return
  dingTalkGroupsLoading.value = true
  dingTalkConnectError.value = ''
  try {
    const { data } = await apiClient.get('/admin/dingtalk/groups', { params: { query } })
    dingTalkGroups.value = Array.isArray(data.groups) ? data.groups : []
    dingTalkStatusMessage.value = dingTalkGroups.value.length ? '请选择告警群聊，再选择机器人。' : '没有找到匹配群聊，请检查群名关键词或手动输入 openConversationId。'
  } catch (error) {
    dingTalkGroups.value = []
    dingTalkConnectError.value = errorMessage(error, '搜索钉钉群聊失败。')
  } finally {
    dingTalkGroupsLoading.value = false
  }
}

async function dingTalkLoadRobots() {
  dingTalkRobotsLoading.value = true
  dingTalkConnectError.value = ''
  try {
    const { data } = await apiClient.get('/admin/dingtalk/robots')
    dingTalkRobots.value = Array.isArray(data.robots) ? data.robots : []
  } catch (error) {
    dingTalkRobots.value = []
    dingTalkConnectError.value = errorMessage(error, '获取钉钉机器人失败。')
  } finally {
    dingTalkRobotsLoading.value = false
  }
}

async function dingTalkSaveTarget() {
  dingTalkTargetSaving.value = true
  dingTalkConnectError.value = ''
  try {
    await apiClient.put('/admin/dingtalk/target', {
      group_id: dingTalkGroupId.value.trim(),
      robot_code: dingTalkRobotCode.value.trim()
    })
    dingTalkPhase.value = 'ready'
    dingTalkStatusMessage.value = '告警群和机器人已保存，现在可以发送测试消息。'
    dingTalkNotice.value = `已绑定钉钉群 ${dingTalkGroupId.value.trim()}`
  } catch (error) {
    dingTalkConnectError.value = errorMessage(error, '保存钉钉告警目标失败。')
  } finally {
    dingTalkTargetSaving.value = false
  }
}

async function dingTalkBindTarget() {
  dingTalkTargetSaving.value = true
  dingTalkConnectError.value = ''
  try {
    await apiClient.post('/admin/dingtalk/target/bind', {
      group_id: dingTalkGroupId.value.trim(),
      robot_code: dingTalkRobotCode.value.trim()
    })
    dingTalkCreatePhase.value = 'robot_created'
    dingTalkPhase.value = 'ready'
    dingTalkStatusMessage.value = '机器人已加入告警群，可以发送测试消息。'
    dingTalkNotice.value = `已绑定钉钉群 ${dingTalkGroupId.value.trim()}`
  } catch (error) {
    dingTalkConnectError.value = errorMessage(error, '机器人加入钉钉群失败。')
  } finally {
    dingTalkTargetSaving.value = false
  }
}

async function dingTalkSendTest() {
  dingTalkSending.value = true
  dingTalkPhase.value = 'sending'
  dingTalkConnectError.value = ''
  dingTalkNotice.value = ''
  try {
    await apiClient.post('/admin/dingtalk/send-test', { markdown: dingTalkTestMessage.value || undefined })
    dingTalkTestMessage.value = ''
    dingTalkPhase.value = 'success'
    dingTalkStatusMessage.value = '测试消息已发送，钉钉通知链路已打通。'
    dingTalkNotice.value = '测试发送成功，后续上游监控告警会发送到这个群。'
  } catch (error) {
    dingTalkConnectError.value = errorMessage(error, '测试发送失败，请检查机器人、群 ID 和 dws 授权。')
    dingTalkPhase.value = 'ready'
    dingTalkStatusMessage.value = '测试发送失败，请检查钉钉机器人是否仍在群内。'
  } finally {
    dingTalkSending.value = false
  }
}
</script>

<style scoped>
.tools-surface {
  @apply min-w-0 rounded-lg border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-900/70;
}

.tools-section-header {
  @apply mb-4 flex min-w-0 flex-col gap-3 border-b border-gray-100 pb-4 dark:border-dark-800 lg:flex-row lg:items-start lg:justify-between;
}

.tools-title {
  @apply text-base font-semibold text-gray-900 dark:text-white;
}

.tools-description {
  @apply mt-1 text-sm text-gray-500 dark:text-dark-400;
}

.tools-panel {
  @apply rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900/40;
}

.tools-tab {
  @apply inline-flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm font-medium text-gray-600 shadow-sm transition-colors hover:border-gray-300 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300 dark:hover:border-dark-600 dark:hover:bg-dark-800;
}

.tools-tab-active {
  @apply border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-800 dark:bg-primary-950/40 dark:text-primary-300;
}

.tools-badges {
  @apply flex flex-wrap items-center gap-2;
}

.tools-badge {
  @apply inline-flex items-center rounded-full border border-gray-200 bg-gray-50 px-2.5 py-1 text-xs font-medium text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300;
}

.tools-empty {
  @apply flex min-h-[220px] items-center justify-center px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400;
}

.tools-th {
  @apply whitespace-nowrap px-3 py-2 text-left font-semibold;
}

.tools-td {
  @apply whitespace-nowrap px-3 py-2 align-middle text-gray-700 dark:text-dark-200;
}

.tools-code {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-700 dark:bg-dark-800 dark:text-dark-200;
}

.tools-readout {
  @apply flex min-h-[38px] items-center rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200;
}

.status-ok {
  @apply rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300;
}

.status-error {
  @apply rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium text-red-700 dark:bg-red-950/40 dark:text-red-300;
}

.status-muted {
  @apply rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300;
}

.status-running {
  @apply rounded-full bg-sky-50 px-2 py-0.5 text-xs font-medium text-sky-700 dark:bg-sky-950/40 dark:text-sky-300;
}

.status-warning {
  @apply rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-950/40 dark:text-amber-300;
}
</style>
