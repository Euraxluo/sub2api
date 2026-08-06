---
name: sub2api-release
description: 发布 Sub2API 当前改动：按精确范围验证并提交代码，生成北京时间 YYYYMMDDHHMMSS 标签，构建并推送 registry.cn-hangzhou.aliyuncs.com/euraxluo/sub2api 的 linux/amd64 与 linux/arm64 镜像，只重建本地 Sub2API Compose 服务，并验证清单、版本、健康状态、Node.js 与 Admin API。用于用户要求 commit、发布时间戳容器、推送 amd64+arm64、更新本地服务，或处理 Docker Hub、Alpine CDN 导致的发布失败时。
---

# Sub2API Release

## 原则

- 在仓库根目录执行命令，并按仓库要求为工具命令添加 `rtk` 前缀。
- 保留用户的无关改动；只暂存本次发布涉及的明确路径。
- 不读取或输出 `deploy/.env` 内容；只通过 `docker compose --env-file` 使用它。
- 不提交 `deploy/data/`、数据库目录、构建产物或本地 Compose override。
- 除非用户明确要求，否则只创建本地 Git commit，不推送 Git 分支。
- 默认镜像仓库为 `registry.cn-hangzhou.aliyuncs.com/euraxluo/sub2api`。
- 默认标签必须是北京时间 `YYYYMMDDHHMMSS`，不要使用 `latest` 或示例占位符。
- 本地更新时只重建 `sub2api`，不要重建 PostgreSQL、Redis 或删除数据卷。

## 发布流程

### 1. 审查范围

运行：

```bash
rtk git status --short
rtk git diff --check
rtk git diff --stat
```

确认改动属于用户要求。存在无关改动时绕开它们，不要清理、还原或暂存。

### 2. 验证代码

根据改动范围运行验证；同时修改前后端时执行全部命令：

```bash
rtk go test ./...
```

在 `frontend/` 目录执行：

```bash
rtk pnpm run typecheck
rtk pnpm run build
```

再次运行 `rtk git diff --check`。任何验证失败都先诊断，不要提交或发布失败产物。

### 3. 精确提交

使用明确路径执行 `rtk git add <paths...>`，然后检查：

```bash
rtk git diff --cached --check
rtk git diff --cached --stat
rtk git status --short
```

提交后记录：

```bash
rtk git rev-parse --short=12 HEAD
rtk git status --short
```

工作区必须干净，再进入镜像发布。不要 amend 既有 commit，除非用户明确要求。

### 4. 构建、推送并部署

首选调用 bundled script：

```bash
rtk bash skills/sub2api-release/scripts/release.sh
```

脚本将：

1. 生成北京时间标签。
2. 构建并推送 `linux/amd64,linux/arm64`。
3. 校验远端 manifest 同时包含两种架构。
4. 写入被 Git 忽略的 `deploy/docker-compose.override.yml`。
5. 拉取新镜像并只重建本地 `sub2api` 服务。
6. 等待 healthy，并检查 `/health`、Node.js 与二进制版本。

常用参数：

```bash
# 指定已有时间戳
rtk bash skills/sub2api-release/scripts/release.sh --tag 20260805170949

# 只发布镜像，不更新本地服务
rtk bash skills/sub2api-release/scripts/release.sh --no-deploy

# 只查看计划，不修改任何状态
rtk bash skills/sub2api-release/scripts/release.sh --dry-run
```

## 网络失败处理

先根据错误分层处理，不要连续重复相同命令。

- `auth.docker.io` OAuth 连接重置：让 BuildKit 端获取令牌。脚本默认设置 `BUILDKIT_NO_CLIENT_TOKEN=true`。
- `apk` 或 npm TLS/index 下载中断：脚本自动读取 builder 的 HTTPS proxy，并作为 Docker 预定义构建参数传入；代理值不写入最终镜像层。
- 外部包源仍反复失败：仅当本次提交没有修改 Dockerfile、入口脚本、运行时 CLI、`backend/resources/` 或其他运行时依赖时，使用 overlay 兜底。

Overlay 模式会从当前本地 `sub2api` 容器取得上一版多架构基础镜像，重新构建当前提交的前端和两个架构的静态 Go 二进制，再替换基础镜像中的 `/app/sub2api`：

```bash
rtk bash skills/sub2api-release/scripts/release.sh --mode overlay
```

也可显式指定基础镜像：

```bash
rtk bash skills/sub2api-release/scripts/release.sh \
  --mode overlay \
  --base-image registry.cn-hangzhou.aliyuncs.com/euraxluo/sub2api:20260805151720
```

使用 overlay 时，在最终交付中明确说明复用了哪一个未变更的运行时基础镜像。

## 发布后验证

脚本完成后，再执行一次只读检查：

```bash
rtk docker buildx imagetools inspect <image:tag>
rtk docker compose -f deploy/docker-compose.local.yml \
  -f deploy/docker-compose.override.yml \
  --env-file deploy/.env ps
rtk curl -fsS http://127.0.0.1:18080/health
rtk git status --short
```

如需验证 Admin Jobs 等受保护接口，使用本地管理员登录获取临时 Access Token；只输出 HTTP 状态、结果数量或功能 ID，不输出密码、Refresh Token 或 Access Token。

## 交付格式

向用户报告：

- commit hash 与提交标题；
- 完整镜像标签与 manifest digest；
- `linux/amd64`、`linux/arm64` 校验结果；
- 本地地址、容器状态与运行版本；
- 测试和 Admin API 冒烟结果；
- 是否使用 overlay 兜底及其基础镜像；
- 工作区是否干净。
