package billing_strategy

import (
	"strings"

	model_reasoning_effort "github.com/Wei-Shaw/sub2api/internal/plugin/model_reasoning_effort"
)

const (
	BillingModelSourceRequested     = "requested"
	BillingModelSourceUpstream      = "upstream"
	BillingModelSourceChannelMapped = "channel_mapped"
	BillingModelSourceMaxCost       = "max_cost"
	BillingModelSourceInherit       = "inherit"

	AccountBillingModelSourceExtraKey = "billing_model_source"
)

// DecisionInput is the host-independent input to the max-cost strategy.
// Evaluate returns the raw, unmultiplied cost fields for one model.
type DecisionInput struct {
	AccountID     int64
	Effort        string
	MappingChain  string
	Models        []string
	PricingSource func(string) string
	Evaluate      func(string) (Candidate, error)
	Transform     func(string) string
}

// Decision contains the plugin's model-selection result and its audit data.
// The host resolves the selected model back to its own cost type.
type Decision struct {
	MappingChain  string
	SelectedModel string
	Candidates    []Candidate
}

// ResolveMaxCostDecision expands the request's model chain with plugin mapping
// results, evaluates each unique model, and selects the greatest raw cost.
// Group/account multipliers are intentionally outside this decision.
func ResolveMaxCostDecision(input DecisionInput) Decision {
	raw := NormalizeModels(input.Models...)
	mappingChain := strings.TrimSpace(input.MappingChain)
	for _, model := range raw {
		mapped := ResolveBillingModel(input.AccountID, model, input.Effort)
		mappingChain = appendMappingChain(mappingChain, model, mapped)
	}

	models := append([]string(nil), raw...)
	for _, model := range raw {
		mapped := ResolveBillingModel(input.AccountID, model, input.Effort)
		if !strings.EqualFold(mapped, model) {
			models = append(models, mapped)
		}
	}
	models = NormalizeModels(models...)
	if input.Transform != nil {
		for index, model := range models {
			models[index] = strings.TrimSpace(input.Transform(model))
		}
		models = NormalizeModels(models...)
	}

	candidates := make([]Candidate, 0, len(models))
	for _, model := range models {
		candidate := Candidate{Model: model}
		if input.PricingSource != nil {
			candidate.PricingSource = input.PricingSource(model)
		}
		var err error
		if input.Evaluate != nil {
			candidate, err = input.Evaluate(model)
			candidate.Model = model
			if candidate.PricingSource == "" && input.PricingSource != nil {
				candidate.PricingSource = input.PricingSource(model)
			}
		} else {
			candidate.Error = "cost is nil"
		}
		if err != nil {
			candidate.Available = false
			candidate.Error = err.Error()
		}
		candidates = append(candidates, candidate)
	}

	selected, ok := SelectMostExpensive(candidates)
	decision := Decision{MappingChain: mappingChain, Candidates: candidates}
	if ok {
		decision.SelectedModel = candidates[selected].Model
	}
	return decision
}

// ResolveBillingModel applies the model/effort plugin mapping used by the
// request transformer to a billing candidate.
func ResolveBillingModel(accountID int64, model, effort string) string {
	model = strings.TrimSpace(model)
	if accountID <= 0 || model == "" {
		return model
	}
	mapping := model_reasoning_effort.ResolveUsageWithEffort(accountID, model, effort)
	if mapping.Matched && strings.TrimSpace(mapping.Model) != "" {
		return strings.TrimSpace(mapping.Model)
	}
	return model
}

// NormalizeModels removes blank and case-insensitive duplicate model names.
func NormalizeModels(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	models := make([]string, 0, len(values))
	for _, value := range values {
		model := strings.TrimSpace(value)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		models = append(models, model)
	}
	return models
}

// ApplyAccountBillingModelSource applies the optional account-level source
// override to a channel source. Invalid values are ignored.
func ApplyAccountBillingModelSource(extra map[string]any, source *string) {
	if source == nil {
		return
	}
	if override := AccountBillingModelSourceOverride(extra); override != "" {
		*source = override
	}
}

// AccountBillingModelSourceOverride returns a valid account-level source.
func AccountBillingModelSourceOverride(extra map[string]any) string {
	if extra == nil {
		return ""
	}
	source, ok := extra[AccountBillingModelSourceExtraKey].(string)
	if !ok {
		return ""
	}
	switch source = strings.ToLower(strings.TrimSpace(source)); source {
	case BillingModelSourceRequested, BillingModelSourceUpstream,
		BillingModelSourceChannelMapped, BillingModelSourceMaxCost:
		return source
	default:
		return ""
	}
}

// EffectiveBillingModelSource returns the account override when present.
func EffectiveBillingModelSource(extra map[string]any, channelSource string) string {
	if source := AccountBillingModelSourceOverride(extra); source != "" {
		return source
	}
	return channelSource
}

// BillingRate selects the rate bucket for a selected raw cost.
func BillingRate(billingMode string, tokenRate, imageRate, videoRate, searchRate float64, imageCount, videoCount, searchCalls int) float64 {
	switch {
	case searchCalls > 0:
		return searchRate
	case billingMode == "token":
		return tokenRate
	case videoCount > 0:
		return videoRate
	case imageCount > 0:
		return imageRate
	default:
		return tokenRate
	}
}

// CalculateUserChargeCost applies the selected rate to an unmultiplied cost.
func CalculateUserChargeCost(rawCost, rateMultiplier float64) float64 {
	if rawCost < 0 {
		return 0
	}
	if rateMultiplier < 0 {
		rateMultiplier = 0
	}
	return rawCost * rateMultiplier
}

func appendMappingChain(chain, before, after string) string {
	chain, before, after = strings.TrimSpace(chain), strings.TrimSpace(before), strings.TrimSpace(after)
	if after == "" || strings.EqualFold(before, after) {
		return chain
	}
	if chain == "" {
		if before == "" {
			return after
		}
		return before + "->" + after
	}
	return chain + "->" + after
}
