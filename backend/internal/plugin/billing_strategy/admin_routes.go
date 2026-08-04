package billing_strategy

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RegisterAdminRoutes registers the billing audit endpoints owned by this
// plugin. The ordinary usage endpoints remain unchanged.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	adminGroup.GET("/billing-strategy/audits", listAudits)
}
func listAudits(c *gin.Context) {
	accountID := int64(0)
	if raw := strings.TrimSpace(c.Query("account_id")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			response.Error(c, http.StatusBadRequest, "invalid account_id")
			return
		}
		accountID = parsed
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.Error(c, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	records, err := List(accountID, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load billing audits")
		return
	}
	response.Success(c, records)
}
