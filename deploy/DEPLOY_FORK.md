# 生产部署注意事项（fork 版 sub2api）

镜像：`ghcr.io/euraxluo/sub2api:v0.2.20-zhipu`（multi-arch amd64+arm64）
对应代码：`Euraxluo/sub2api` main @ `21715bd61`，tag `v0.2.20-zhipu`

## 部署步骤

1. `cp deploy/.env.example .env`（若无则参考 `deploy/docker-compose.yml` 注释），必填：
   - `POSTGRES_PASSWORD=<强密码>`（compose 强校验）
   - `JWT_SECRET=<64+ 随机 hex>`（**必须显式固定**，否则重启后所有登录态失效）
   - `ADMIN_EMAIL` / `ADMIN_PASSWORD`（首启创建管理员）
2. `deploy/docker-compose.yml` 中 `image: weishaw/sub2api:latest` 改为
   `ghcr.io/euraxluo/sub2api:v0.2.20-zhipu`
3. `docker compose up -d`；首启 AutoSetup 自动迁移数据库
4. 前置 Caddy/Nginx 终结 TLS（参考 `deploy/Caddyfile`），配置
   `server.trusted_proxies`，否则限流取到的是代理 IP

## 首次登录

- 管理台会强制弹「部署与运营合规承诺」，接受前所有 admin API 返回 423——这是预期不是故障
- 管理台「Admin Tools」是空壳占位页：通知渠道（飞书/钉钉/微信/Claw163）、任务中心、
  上游余额监控、代理/RT 导入已随插件层移除

## zhipu（GLM）账号配置 runbook

1. 账号管理 → 添加账号 → Zhipu GLM → Coding Plan → 填 `<id>.<secret>` API key
2. **勾选「ZCode 客户端签名」**：请求按官方签约客户端计 1.5x 配额；fail-closed——
   签名失败直接报错，绝不降级为未签名请求
3. 创建后**必须**手动「更多 → 同步上游模型」，否则账号无可用模型，调度报
   `no available accounts`
4. 账号挂到 `platform=zhipu` 的分组（不是 default anthropic 分组）
5. 重置卡：账号行「更多 → 重置卡管理」。5h 卡清零 5 小时窗口，周卡同时清两个窗口；
   卡会过期作废（面板有倒计时）。list 为实时只读拉取；「使用」消耗真实卡片

## 监控基线

- 进程/端口探活 + `docker logs`（JSON 结构化，可直接进 Loki/ELK）
- zhipu 账号 5xx/429 告警（签名 fail-closed 下握手失败表现为 5xx）
- pg / redis 连接与磁盘容量
- 数据卷：`sub2api_data`、postgres 数据、redis 数据；镜像内自带版本对齐的
  pg_dump/psql

## 回滚

镜像回退 `weishaw/sub2api:v0.2.20`（上游官方）即可——本 fork 未改数据库 schema，
新增字段均为账号 credentials/extra 内的 JSON，向后兼容。
