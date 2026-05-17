package tokenizer

func ToSCU(promptTokens, completionTokens int, scuRate float64) float64 {
	return float64(promptTokens+completionTokens) * scuRate
}

func ToUSD(promptTokens, completionTokens int, pricePerMInput, pricePerMOutput float64) float64 {
	inputCost := float64(promptTokens) / 1_000_000 * pricePerMInput
	outputCost := float64(completionTokens) / 1_000_000 * pricePerMOutput
	return inputCost + outputCost
}

// EstimateTokensFromBody estimates token count from raw request body.
// Uses ~4 chars/token heuristic. Clamped to [50, 50000].
func EstimateTokensFromBody(body []byte) int {
	estimate := len(body) / 4
	if estimate < 50 {
		return 50
	}
	if estimate > 50000 {
		return 50000
	}
	return estimate
}
