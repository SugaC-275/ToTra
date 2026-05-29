package handlers

import "encoding/json"

// estimateTokens returns a rough token count for a string.
// Uses len(text)/4, a good approximation for English (~10% error).
// We avoid importing tiktoken to keep CGO-free builds.
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// EstimateChatTokens counts tokens in a chat/completions request body.
// Extracts messages[].content and adds ~4 tokens per message for format overhead.
func EstimateChatTokens(body []byte) int {
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, m := range req.Messages {
		total += estimateTokens(m.Content) + 4
	}
	return total + 3 // reply priming
}
