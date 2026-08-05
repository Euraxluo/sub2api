package admin_jobs

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RegisterAdminRoutes exposes the plugin-owned task center under /admin/admin-jobs.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	manager := DefaultManager()
	manager.Start()

	jobs := adminGroup.Group("/admin-jobs")
	jobs.GET("/builtins", func(c *gin.Context) {
		response.Success(c, BuiltinDefinitions())
	})
	jobs.GET("/tasks", func(c *gin.Context) {
		tasks, err := manager.ListTasks()
		if err != nil {
			response.InternalError(c, err.Error())
			return
		}
		response.Success(c, tasks)
	})
	jobs.GET("/tasks/:task_id", func(c *gin.Context) {
		task, err := manager.GetTask(c.Param("task_id"))
		if err != nil {
			writeTaskError(c, err)
			return
		}
		response.Success(c, task)
	})
	jobs.POST("/tasks", func(c *gin.Context) {
		var draft TaskDraft
		if err := c.ShouldBindJSON(&draft); err != nil {
			response.BadRequest(c, "invalid admin job: "+err.Error())
			return
		}
		task, err := manager.CreateTask(draft)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		response.Created(c, task)
	})
	jobs.PUT("/tasks/:task_id", func(c *gin.Context) {
		var draft TaskDraft
		if err := c.ShouldBindJSON(&draft); err != nil {
			response.BadRequest(c, "invalid admin job: "+err.Error())
			return
		}
		task, err := manager.UpdateTask(c.Param("task_id"), draft)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				response.NotFound(c, err.Error())
			} else {
				response.BadRequest(c, err.Error())
			}
			return
		}
		response.Success(c, task)
	})
	jobs.DELETE("/tasks/:task_id", func(c *gin.Context) {
		if err := manager.DeleteTask(c.Param("task_id")); err != nil {
			writeTaskError(c, err)
			return
		}
		response.Success(c, gin.H{"deleted": true})
	})
	jobs.POST("/tasks/:task_id/run", func(c *gin.Context) {
		var request struct {
			Notify bool `json:"notify"`
		}
		if c.Request.ContentLength > 0 {
			if err := c.ShouldBindJSON(&request); err != nil {
				response.BadRequest(c, "invalid run request: "+err.Error())
				return
			}
		}
		run, err := manager.Run(c.Request.Context(), c.Param("task_id"), TriggerManual, request.Notify)
		if err != nil {
			writeTaskError(c, err)
			return
		}
		response.Success(c, run)
	})
	jobs.GET("/runs", func(c *gin.Context) {
		limit := 50
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}
		runs, err := manager.ListRuns(c.Query("task_id"), limit)
		if err != nil {
			response.InternalError(c, err.Error())
			return
		}
		response.Success(c, runs)
	})
}

func writeTaskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, ErrTaskRunning):
		response.Error(c, http.StatusConflict, err.Error())
	default:
		response.InternalError(c, err.Error())
	}
}
