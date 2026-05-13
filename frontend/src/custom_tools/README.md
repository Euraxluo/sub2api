# Admin Tools Custom 插件开发说明

这个目录用于承载本仓库的自定义 Admin Tools 功能。原则是：新增自动化能力优先放在插件层，不直接修改上游原有页面、服务、仓储、DTO 或网关代码。

## 边界原则

- 前端自定义功能只改 `frontend/src/custom_tools/` 下的文件。
- 后端确实需要新增接口时，只在 `backend/internal/server/routes/plugin_admin_tools.go` 增加插件端点。
- 优先复用已有 Admin API，例如 `adminAPI.accounts.*`、`adminAPI.proxies.*`、`adminAPI.tlsFingerprintProfiles.*`。
- 不修改上游页面组件，例如 `frontend/src/views/admin/*`、`frontend/src/components/account/*`。
- 不修改上游核心后端代码，例如 `backend/internal/service/*`、`backend/internal/handler/admin/*`、`backend/internal/repository/*`。
- `frontend/src/router/index.ts` 和 `backend/internal/server/routes/admin.go` 只保留插件挂载入口，后续功能不要继续往里面塞业务逻辑。

如果一个需求必须改上游核心逻辑，先停下来确认设计，不要顺手改。

## 目录职责

- `AdminTools.vue`：Admin Tools 主页面。小功能可以先放这里；功能变大后拆成同目录子组件。
- `api.ts`：只放插件自有后端端点的前端封装。已有 Admin API 继续从 `@/api/admin` 调用。
- `routes.ts`：只注册自定义页面路由。
- `init.ts`：只做插件初始化，包括路由注入、菜单注入、少量 i18n key 注入。

## 新增自动化功能流程

1. 先确认现有 Admin API 是否已经能完成写入动作。
   如果可以，插件前端直接组合调用已有 API，不新增后端。

2. 如果需要后端辅助，只新增“插件辅助接口”。
   典型用途包括预览、解析、聚合、批量规划、订阅抓取等。接口放在 `plugin_admin_tools.go`，返回可预览的数据，不绕开已有正式写入 API。

3. UI 必须先预览，再执行。
   自动化功能默认应展示待处理列表、状态、错误信息和可确认动作，避免用户点一下就发生不可逆批量写入。

4. 写入时逐项更新状态。
   每条记录建议有 `pending`、`running`、`success`、`failed` 状态，并显示失败原因。

5. 区分“操作并发”和“账号配置”。
   例如 RT 导入里：
   - 批量刷新/创建的前端操作并发可以固定为小值，避免打爆上游。
   - 创建账号时传给后端的 `concurrency` 是账号自身并发配置，默认保持和上游创建账号一致。

6. 测试类功能尽量一键化。
   例如代理测试应提供“测试全部”，测试结果回填到每个代理行；勾选只用于选择参与后续自动分配的对象。

## 推荐实现方案

以后添加自动化能力时，优先按下面的结构拆：

- 数据准备：解析输入、拉取候选资源、生成预览列表。
- 自动规划：在前端计算每一条记录将要执行的动作，例如代理分配、账号并发、指纹配置。
- 用户确认：展示计划和风险信息，让用户确认后再进入写入阶段。
- 批量执行：使用有限并发逐条调用已有 Admin API，并把成功、失败、跳过原因写回对应行。
- 结果复盘：保留批量结果，让用户能重试失败项，不需要重新导入全部数据。

如果功能需要复用一段复杂逻辑，优先在 `frontend/src/custom_tools/` 内拆出组合函数或子组件。例如：

- `useBatchRunner.ts`：统一处理有限并发、行状态、错误收集。
- `proxyAutomation.ts`：只放代理测试、代理选择、轮询分配相关逻辑。
- `fingerprintAutomation.ts`：只放指纹模板选择、随机选择、生成配置相关逻辑。

这些文件仍然属于插件层，可以自由演进；不要为了复用而把逻辑抽到上游全局目录。

## RT 自动化功能约定

RT 导入类功能后续继续按当前模式扩展：

- 代理测试：页面提供“测试全部”，自动测试可用代理并把结果写回代理列表；代理勾选只表示后续分配时允许使用。
- 多代理分配：用户可以选择多个代理，前端按策略生成账号到代理的映射，再调用账号创建 API。
- 账号并发：默认值保持 `10`，创建账号时作为 `concurrency` 传给后端；它不是前端批量请求并发。
- 批量请求并发：属于前端执行控制，应保持较小固定值或显式配置，避免一次性打爆服务。
- 指纹设置：前端可以在账号 `extra` 里写入指纹启用状态和 profile 信息；当前不要把它理解成所有 provider 的全局开关，真正生效逻辑必须由对应 provider 的后端请求链路消费。

一个 RT 自动化功能的推荐数据流是：

```text
输入 RT -> 解析预览 -> 测试全部代理 -> 选择可用代理 -> 生成账号计划 -> 可选生成指纹 -> 确认执行 -> 逐条创建账号 -> 展示结果
```

## 未来可能支持的指纹策略

TLS 指纹后续应该按 provider/platform 维度扩展，而不是做成一个笼统的“开启后全部自动有”的开关。建议方向：

- `anthropic` / Claude Code：继续使用现有 Node.js / Claude Code 风格 profile。
- `openai` / Codex：未来可以接入 Codex / reqwest / rustls 风格 profile，但需要对应 OpenAI 请求链路真正读取并应用账号 `extra`。
- `gemini` 或其他 provider：独立定义自己的默认 profile、可选 profile 和兼容性说明。

插件层可以先保存用户的指纹选择和 profile 绑定，但如果要让某个 provider 真正使用该指纹，必须新增对应 provider 的后端接入方案。实现时优先保持非侵入式：能走插件辅助接口就走插件辅助接口；确实需要改上游请求链路时，先确认设计和影响范围，不要直接顺手改核心代码。

## 插件后端接口约定

插件端点注册在：

```go
func registerPluginAdminTools(adminGroup *gin.RouterGroup) {
    adminGroup.POST("/proxies/import/clash", previewClashImport)
}
```

新增端点时遵循：

- 路径挂在已有 admin group 下，复用 Admin 鉴权。
- 只做插件需要的辅助逻辑，不改核心业务服务。
- 返回结构化 JSON，前端负责预览和确认。
- 写入类动作优先调用已有上游 Admin API；除非明确设计为插件专属写入，否则不要在插件端点里直接写数据库。

## 前端实现约定

- 复用已有类型和 API：`@/types`、`@/api/admin`。
- 插件自有 API 放在 `./api.ts`，不要混到全局 API barrel。
- 批量流程使用有限并发，避免无上限 `Promise.all`。
- token、RT、API key 等敏感内容只展示掩码。
- 错误信息要落到对应行，顶层 toast 只做摘要。
- 新功能文案优先用中文，保持当前 Admin Tools 风格。

## 构建与重启

前端改动会被构建到 `backend/internal/web/dist`，服务运行的是 embed 后端二进制。改完 Admin Tools 后至少执行：

```bash
rtk npm run typecheck
rtk npm run build
```

本地 Docker dev 服务需要重新嵌入前端资源并重启。当前项目常用流程是：

```bash
rtk sh -lc 'VERSION="$(tr -d "\r\n" < ./cmd/server/VERSION)"; DATE_VALUE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; mkdir -p bin; CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags embed -ldflags="-s -w -X main.Version=${VERSION} -X main.Commit=local-admin-tools -X main.Date=${DATE_VALUE} -X main.BuildType=release" -trimpath -o bin/sub2api-linux-arm64 ./cmd/server'
```

然后更新 `deploy-sub2api` 镜像并重建 `sub2api-dev` 容器。重启后确认：

```bash
rtk curl -fsS http://127.0.0.1:8080/health
rtk docker ps
```

## 提交前检查

- `rtk git status --short` 只包含预期文件。
- `rtk git diff --check` 无空白错误。
- 没有误改上游核心文件。
- 自动化功能至少覆盖正常路径、空数据、失败提示。
- 如果服务使用 embed 前端，确认已经重新构建并重启运行服务。
