package admin

import (
	"strconv"

	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// CNProviderHandler 暴露国产供应商（kimi/zhipu/deepseek）的额度与余额查询端点。
//
//   - GET /admin/cn-providers/accounts/:id/quota   Coding Plan 滚动窗口用量（kimi/zhipu）
//   - GET /admin/cn-providers/accounts/:id/balance  payg 账号余额（kimi/deepseek）
//
// 智谱（zhipu）无余额端点，故同一账号仅 quota 或 balance 其一可用：服务端按账号
// platform + account_mode 校验并返回明确错误（见 CNProvider*Service 的 load*Account）。
type CNProviderHandler struct {
	quotaService   *service.CNProviderQuotaService
	balanceService *service.CNProviderBalanceService
}

func NewCNProviderHandler(
	quotaService *service.CNProviderQuotaService,
	balanceService *service.CNProviderBalanceService,
) *CNProviderHandler {
	return &CNProviderHandler{
		quotaService:   quotaService,
		balanceService: balanceService,
	}
}

// QueryQuota 查询 Coding Plan 滚动窗口用量（5h + weekly）。
func (h *CNProviderHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.quotaService == nil {
		response.BadRequest(c, "cn provider quota service is not enabled")
		return
	}
	result, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// QueryBalance 查询 payg 账号余额。
func (h *CNProviderHandler) QueryBalance(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.balanceService == nil {
		response.BadRequest(c, "cn provider balance service is not enabled")
		return
	}
	result, err := h.balanceService.QueryBalance(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetZhipuResetCards 查询智谱重置卡列表（实时拉取 + 落快照）。
// GET /admin/cn-providers/accounts/:id/zhipu-reset-cards
func (h *CNProviderHandler) GetZhipuResetCards(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.quotaService == nil {
		response.BadRequest(c, "cn provider quota service is not enabled")
		return
	}
	result, err := h.quotaService.QueryZhipuResetCards(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// UseZhipuResetCard 消耗一张智谱重置卡并刷新用量快照。
// POST /admin/cn-providers/accounts/:id/zhipu-reset-cards/use
// body: {"window": "5h"|"week", "recordId": 0=自动选最早过期}
func (h *CNProviderHandler) UseZhipuResetCard(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.quotaService == nil {
		response.BadRequest(c, "cn provider quota service is not enabled")
		return
	}
	var req struct {
		Window   string `json:"window"`
		RecordID int64  `json:"recordId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body: "+err.Error())
		return
	}
	var operator string
	if s, ok := middleware.GetAuthSubjectFromContext(c); ok {
		operator = fmt.Sprintf("admin:%d", s.UserID)
	} else {
		operator = "admin:unknown"
	}
	result, err := h.quotaService.UseZhipuResetCard(c.Request.Context(), accountID, req.Window, req.RecordID, operator)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
