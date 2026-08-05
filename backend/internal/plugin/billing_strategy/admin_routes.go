package billing_strategy

import (
	"errors"
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
	adminGroup.GET("/billing-strategy/config", getConfig)
	adminGroup.PUT("/billing-strategy/account-multipliers", updateAccountMultipliers)
}

type updateAccountMultipliersRequest struct {
	AccountIDs []int64 `json:"account_ids"`
	Multiplier float64 `json:"multiplier"`
}

func getConfig(c *gin.Context) {
	rememberAccountSnapshotAdminRequest(c)
	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load billing strategy config")
		return
	}
	response.Success(c, config)
}

func updateAccountMultipliers(c *gin.Context) {
	rememberAccountSnapshotAdminRequest(c)
	var request updateAccountMultipliersRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "invalid account multiplier request")
		return
	}
	config, err := SetAccountRateMultiplierFactor(request.AccountIDs, request.Multiplier)
	if errors.Is(err, ErrInvalidAccountMultiplier) {
		response.BadRequest(c, "account_ids must be positive and multiplier must be in (0, 10000]")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to save billing strategy config")
		return
	}
	response.Success(c, config)
}

func listAudits(c *gin.Context) {
	rememberAccountSnapshotAdminRequest(c)
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
