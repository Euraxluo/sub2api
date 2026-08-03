package model_reasoning_effort

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// RegisterAdminRoutes exposes the plugin-owned configuration, refresh, and
// statistics endpoints without putting their handlers in the host router.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	registerQuotaGuardRoutes(adminGroup)
	adminGroup.PUT("/model-reasoning-effort/config/batch", updateConfigBatch)
	adminGroup.GET("/model-reasoning-effort/config/:account_id", getConfig)
	adminGroup.PUT("/model-reasoning-effort/config/:account_id", updateConfig)
	adminGroup.GET("/model-reasoning-effort/auto", getAuto)
	adminGroup.PUT("/model-reasoning-effort/auto", updateAuto)
	adminGroup.POST("/model-reasoning-effort/auto/refresh", refreshAuto)
	adminGroup.GET("/model-reasoning-effort/stats", getStats)
}

const maxBatchConfigAccounts = 500

type batchConfigRequest struct {
	AccountIDs []int64       `json:"account_ids"`
	Config     AccountConfig `json:"config"`
}

type batchConfigResponse struct {
	UpdatedAccountIDs []int64       `json:"updated_account_ids"`
	Config            AccountConfig `json:"config"`
}

func updateConfigBatch(c *gin.Context) {
	var req batchConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid batch model reasoning config: "+err.Error())
		return
	}
	accountIDs, err := normalizeBatchAccountIDs(req.AccountIDs)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin config")
		return
	}
	if config.Accounts == nil {
		config.Accounts = map[string]AccountConfig{}
	}
	for _, accountID := range accountIDs {
		config.Accounts[strconv.FormatInt(accountID, 10)] = req.Config
	}
	if err := SaveConfig(config); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, batchConfigResponse{
		UpdatedAccountIDs: accountIDs,
		Config:            config.Accounts[strconv.FormatInt(accountIDs[0], 10)],
	})
}

func normalizeBatchAccountIDs(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, &invalidAccountIDError{}
	}
	seen := make(map[int64]struct{}, len(values))
	ids := make([]int64, 0, len(values))
	for _, accountID := range values {
		if accountID <= 0 {
			return nil, &invalidAccountIDError{}
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		ids = append(ids, accountID)
	}
	if len(ids) > maxBatchConfigAccounts {
		return nil, &tooManyBatchAccountsError{}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

type tooManyBatchAccountsError struct{}

func (*tooManyBatchAccountsError) Error() string {
	return "too many accounts in batch model reasoning config"
}

func getConfig(c *gin.Context) {
	accountID, err := parseAccountID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin config")
		return
	}
	accountConfig := config.Accounts[strconv.FormatInt(accountID, 10)]
	response.Success(c, accountConfig)
}

func updateConfig(c *gin.Context) {
	accountID, err := parseAccountID(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	var accountConfig AccountConfig
	if err := c.ShouldBindJSON(&accountConfig); err != nil {
		response.BadRequest(c, "invalid model reasoning config: "+err.Error())
		return
	}
	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin config")
		return
	}
	if config.Accounts == nil {
		config.Accounts = map[string]AccountConfig{}
	}
	key := strconv.FormatInt(accountID, 10)
	config.Accounts[key] = accountConfig
	if err := SaveConfig(config); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, config.Accounts[key])
}

type autoResponse struct {
	Config   AutoRoutingConfig   `json:"config"`
	Status   AutoRoutingSnapshot `json:"status"`
	Mappings []Mapping           `json:"mappings"`
}

func getAuto(c *gin.Context) {
	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin config")
		return
	}
	response.Success(c, autoResponse{
		Config:   config.Auto,
		Status:   AutoRoutingStatus(),
		Mappings: AutoMappings(),
	})
}

func updateAuto(c *gin.Context) {
	var autoConfig AutoRoutingConfig
	if err := c.ShouldBindJSON(&autoConfig); err != nil {
		response.BadRequest(c, "invalid automatic model reasoning config: "+err.Error())
		return
	}
	config, err := LoadConfig()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin config")
		return
	}
	config.Auto = autoConfig
	if err := SaveConfig(config); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, config.Auto)
}

func refreshAuto(c *gin.Context) {
	if err := RefreshAutoMappings(c.Request.Context()); err != nil {
		response.Error(c, http.StatusBadGateway, "failed to refresh Radar IQ mappings: "+err.Error())
		return
	}
	response.Success(c, AutoRoutingStatus())
}

func getStats(c *gin.Context) {
	stats, err := LoadMappingUsageStats()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to load model reasoning plugin stats")
		return
	}
	response.Success(c, stats)
}

func parseAccountID(c *gin.Context) (int64, error) {
	accountID, err := strconv.ParseInt(strings.TrimSpace(c.Param("account_id")), 10, 64)
	if err != nil || accountID <= 0 {
		return 0, &invalidAccountIDError{}
	}
	return accountID, nil
}

type invalidAccountIDError struct{}

func (*invalidAccountIDError) Error() string { return "invalid account id" }
