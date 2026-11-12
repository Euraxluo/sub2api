package routes

import "github.com/gin-gonic/gin"

// registerPluginAdminTools 是插件体系的管理路由挂点。
//
// 历史上的插件功能（代理导入、OpenAI RT 导入、飞书/钉钉/微信/Claw163 通知
// 通道、上游余额监控）已随插件实现一并移除；此挂点保留为空实现，供未来
// 插件注册管理端点，宿主路由表无需改动。
func registerPluginAdminTools(_ *gin.RouterGroup) {}
