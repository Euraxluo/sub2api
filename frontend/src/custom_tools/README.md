# custom_tools 插件层（空壳）

历史功能插件（模型映射/IQ 路由/额度保护/计费策略/任务中心/Claw163/飞书/
钉钉/微信通知、代理导入、OpenAI RT 导入、首页落地页）已移除。

保留的骨架：
- `routes.ts` + `init.ts`：`/custom/admin-tools` 路由与侧边栏注入
- `AdminTools.vue`：空壳页

新增插件：在 `plugins/<name>/` 建组件，挂到 AdminTools.vue 即可；
后端在 `internal/plugin/<name>/` 实现并在 `internal/plugin/init.go` 注册。
