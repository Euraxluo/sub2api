package claw163

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RegisterAdminRoutes keeps Claw163 HTTP details inside the plugin. The host
// router only calls this facade when it builds the admin route tree.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	group := adminGroup.Group("/claw163")
	group.GET("/status", claw163Status)
	group.GET("/connect/status", claw163Status)
	group.POST("/initialize", claw163Initialize)
	group.POST("/connect/start", claw163Initialize)
	group.POST("/connect/cancel", claw163Cancel)
	group.PUT("/target", claw163SetTarget)
	group.POST("/send-test", claw163SendTest)
}

func claw163Status(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	response.Success(c, globalManager.Status(ctx))
}

func claw163Initialize(c *gin.Context) {
	var request InitializeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid Claw163 initialization request: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	status, err := globalManager.Initialize(ctx, request.AuthURL, request.Recipients)
	if err != nil {
		response.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(c, status)
}

func claw163Cancel(c *gin.Context) {
	// Initialization is a bounded synchronous CLI operation. There is no
	// background process to kill, but the endpoint mirrors the other channels.
	response.Success(c, gin.H{"cancelled": false})
}

func claw163SetTarget(c *gin.Context) {
	var request TargetRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid Claw163 target: "+err.Error())
		return
	}
	status, err := globalManager.SetRecipients(request.Recipients)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, status)
}

func claw163SendTest(c *gin.Context) {
	var request SendRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			response.BadRequest(c, "invalid Claw163 test message: "+err.Error())
			return
		}
	}
	if strings.TrimSpace(request.Body) == "" {
		request.Body = "Sub2API Claw163 邮箱通知测试\n\n如果你看到这封邮件，说明通知通道已连通。"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := globalManager.Send(ctx, request.Subject, request.Body, request.Recipients); err != nil {
		response.Error(c, http.StatusBadGateway, err.Error())
		return
	}
	response.Success(c, gin.H{"sent": true})
}
