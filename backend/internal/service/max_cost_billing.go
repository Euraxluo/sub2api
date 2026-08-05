package service

import (
	"context"
	"strings"

	pluginruntime "github.com/Wei-Shaw/sub2api/internal/plugin"
)

type maxCostBillingDecision struct {
	pluginruntime.BillingStrategyDecision
	SelectedCost *CostBreakdown
	RawCost      *CostBreakdown
}

// resolveMaxCostBillingDecision adapts the service cost type to the plugin's
// host-independent candidate type. Selection and mapping remain plugin-owned.
func resolveMaxCostBillingDecision(
	accountID int64,
	effort, mappingChain string,
	candidates []string,
	evaluate func(string) (*CostBreakdown, error),
	pricingSource func(string) string,
	decisionContexts ...context.Context,
) *maxCostBillingDecision {
	costs := make(map[string]*CostBreakdown, len(candidates))
	var decisionContext context.Context
	if len(decisionContexts) > 0 {
		decisionContext = decisionContexts[0]
	}
	decision := pluginruntime.ResolveMaxCostBillingDecision(pluginruntime.BillingStrategyInput{
		Context:       decisionContext,
		AccountID:     accountID,
		Effort:        effort,
		MappingChain:  mappingChain,
		Models:        candidates,
		PricingSource: pricingSource,
		Evaluate: func(model string) (pluginruntime.BillingAuditCandidate, error) {
			if evaluate == nil {
				return pluginruntime.BillingAuditCandidate{Model: model, Error: "cost is nil"}, nil
			}
			cost, err := evaluate(model)
			costs[strings.ToLower(strings.TrimSpace(model))] = cost
			return billingAuditCandidateFromCost(model, cost), err
		},
	})
	selectedCostKey := strings.ToLower(strings.TrimSpace(decision.SelectedModel))
	rawSelectedCost := costs[selectedCostKey]
	if decision.AccountSnapshotResolved && rawSelectedCost != nil {
		virtualSelectedCost := *rawSelectedCost
		pluginruntime.ApplyVirtualBillingCost(rawSelectedCost.TotalCost, decision.VirtualModelCost,
			&virtualSelectedCost.TotalCost, &virtualSelectedCost.ActualCost,
			&virtualSelectedCost.InputCost, &virtualSelectedCost.ImageInputCost,
			&virtualSelectedCost.OutputCost, &virtualSelectedCost.ImageOutputCost,
			&virtualSelectedCost.CacheCreationCost, &virtualSelectedCost.CacheReadCost)
		costs[selectedCostKey] = &virtualSelectedCost
	}
	return &maxCostBillingDecision{
		BillingStrategyDecision: decision,
		SelectedCost:            costs[strings.ToLower(strings.TrimSpace(decision.SelectedModel))],
		RawCost:                 rawSelectedCost,
	}
}

func billingAuditCandidateFromCost(model string, cost *CostBreakdown) pluginruntime.BillingAuditCandidate {
	candidate := pluginruntime.BillingAuditCandidate{Model: model}
	if cost == nil {
		candidate.Error = "cost is nil"
		return candidate
	}
	candidate.Available = true
	candidate.BillingMode = cost.BillingMode
	candidate.InputCost = cost.InputCost
	candidate.ImageInputCost = cost.ImageInputCost
	candidate.OutputCost = cost.OutputCost
	candidate.ImageOutputCost = cost.ImageOutputCost
	candidate.CacheWriteCost = cost.CacheCreationCost
	candidate.CacheReadCost = cost.CacheReadCost
	candidate.TotalCost = cost.TotalCost
	return candidate
}

func resolveBillingPricingSource(ctx context.Context, resolver *ModelPricingResolver, apiKey *APIKey, model string) string {
	if resolver == nil {
		return ""
	}
	var groupID *int64
	if apiKey != nil && apiKey.Group != nil {
		id := apiKey.Group.ID
		groupID = &id
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	if resolved == nil {
		return ""
	}
	return resolved.Source
}

func usageCostRate(result *ForwardResult, cost *CostBreakdown, tokenRate, imageRate float64) float64 {
	imageCount := 0
	if result != nil {
		imageCount = result.ImageCount
	}
	return pluginruntime.BillingRate(costBillingMode(cost), tokenRate, imageRate, 0, 0, imageCount, 0, 0)
}

func openAIUsageCostRate(result *OpenAIForwardResult, cost *CostBreakdown, tokenRate, imageRate, videoRate, searchRate float64) float64 {
	imageCount, videoCount, searchCalls := 0, 0, 0
	if result != nil {
		imageCount, videoCount, searchCalls = result.ImageCount, result.VideoCount, result.WebSearchCalls
	}
	return pluginruntime.BillingRate(costBillingMode(cost), tokenRate, imageRate, videoRate, searchRate, imageCount, videoCount, searchCalls)
}

func costBillingMode(cost *CostBreakdown) string {
	if cost == nil {
		return ""
	}
	return cost.BillingMode
}

func applySelectedBillingCostToUsageLog(usageLog *UsageLog, rawCost *CostBreakdown, actualCost, rateMultiplier float64) {
	if usageLog == nil || rawCost == nil {
		return
	}
	usageLog.InputCost = rawCost.InputCost
	usageLog.ImageInputCost = rawCost.ImageInputCost
	usageLog.OutputCost = rawCost.OutputCost
	usageLog.ImageOutputCost = rawCost.ImageOutputCost
	usageLog.CacheCreationCost = rawCost.CacheCreationCost
	usageLog.CacheReadCost = rawCost.CacheReadCost
	usageLog.TotalCost = rawCost.TotalCost
	usageLog.ActualCost = actualCost
	usageLog.RateMultiplier = rateMultiplier
	usageLog.LongContextBillingApplied = rawCost.LongContextBillingApplied
	if rawCost.BillingMode != "" {
		mode := rawCost.BillingMode
		usageLog.BillingMode = &mode
	}
}

func (p *postUsageBillingParams) userChargeCost() float64 {
	if p == nil || p.Cost == nil {
		return 0
	}
	if p.UserChargeCost != nil {
		return *p.UserChargeCost
	}
	return p.Cost.ActualCost
}

func (p *postUsageBillingParams) accountStatsBaseCost() float64 {
	if p == nil || p.Cost == nil {
		return 0
	}
	if p.AccountStatsCost != nil {
		return *p.AccountStatsCost
	}
	return p.Cost.TotalCost
}

func recordMaxCostBillingAudit(usageLog *UsageLog, account *Account, requestedModel, strategy string, decision *maxCostBillingDecision, accountRate float64, fallbackStatsCost ...float64) {
	if usageLog == nil || account == nil || decision == nil || decision.SelectedModel == "" {
		return
	}
	accountStatsCost := usageLog.TotalCost
	if len(fallbackStatsCost) > 0 {
		accountStatsCost = fallbackStatsCost[0]
	}
	if usageLog.AccountStatsCost != nil {
		accountStatsCost = *usageLog.AccountStatsCost
	}
	pluginruntime.RecordBillingAudit(pluginruntime.BillingAuditRecord{
		RequestID: usageLog.RequestID, AccountID: account.ID, RequestedModel: requestedModel,
		MappingChain: decision.MappingChain, Strategy: strategy, SelectedModel: decision.SelectedModel,
		InputTokens: usageLog.InputTokens, OutputTokens: usageLog.OutputTokens,
		CacheCreationTokens: usageLog.CacheCreationTokens, CacheReadTokens: usageLog.CacheReadTokens,
		ImageInputTokens: usageLog.ImageInputTokens, ImageOutputTokens: usageLog.ImageOutputTokens,
		ImageCount: usageLog.ImageCount, GroupRateMultiplier: usageLog.RateMultiplier,
		AccountRateMultiplier: accountRate, TotalCost: usageLog.TotalCost, ActualCost: usageLog.ActualCost,
		AccountStatsCost: usageLog.AccountStatsCost, AccountBilledCost: accountStatsCost * accountRate,
		Candidates: decision.Candidates,
	})
}
