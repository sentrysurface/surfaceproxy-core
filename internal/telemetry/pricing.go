package telemetry

// Pricing holds the cost per 1000 tokens for a given model tier.
// These are used to compute the dollar value of tokens saved.
type Pricing struct {
	// ModelName is a human-readable label shown in the terminal summary.
	ModelName string
	// InputCostPer1K is the cost in USD per 1,000 input tokens.
	InputCostPer1K float64
}

// Well-known model price tiers. These are used as defaults.
// Operators can override via config.
var WellKnownPricing = map[string]Pricing{
	"gpt-4o":             {ModelName: "GPT-4o", InputCostPer1K: 0.005},
	"gpt-4o-mini":        {ModelName: "GPT-4o mini", InputCostPer1K: 0.00015},
	"claude-3-5-sonnet":  {ModelName: "Claude 3.5 Sonnet", InputCostPer1K: 0.003},
	"claude-3-haiku":     {ModelName: "Claude 3 Haiku", InputCostPer1K: 0.00025},
	"gemini-2.0-flash":   {ModelName: "Gemini 2.0 Flash", InputCostPer1K: 0.0001},
}

// DefaultPricing is what the telemetry engine uses when no model is specified.
var DefaultPricing = WellKnownPricing["claude-3-5-sonnet"]

// DollarsSaved computes how many USD were saved by not sending rawTokens–prunedTokens
// worth of tokens to the model, at the given pricing.
func DollarsSaved(tokensReduced int64, p Pricing) float64 {
	if p.InputCostPer1K == 0 {
		p = DefaultPricing
	}
	return float64(tokensReduced) / 1000.0 * p.InputCostPer1K
}
