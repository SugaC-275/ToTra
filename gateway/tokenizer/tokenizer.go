package tokenizer

func ToSCU(promptTokens, completionTokens int, scuRate float64) float64 {
	return float64(promptTokens+completionTokens) * scuRate
}

func ToUSD(promptTokens, completionTokens int, pricePerMInput, pricePerMOutput float64) float64 {
	inputCost := float64(promptTokens) / 1_000_000 * pricePerMInput
	outputCost := float64(completionTokens) / 1_000_000 * pricePerMOutput
	return inputCost + outputCost
}
