package tokenizer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/tokenizer"
)

func TestSCUConverter(t *testing.T) {
	tests := []struct {
		name        string
		scuRate     float64
		promptTok   int
		completeTok int
		wantSCU     float64
	}{
		{"openai gpt-4o", 2.0, 100, 50, 300.0},
		{"anthropic haiku", 0.5, 100, 50, 75.0},
		{"local model", 0.1, 100, 50, 15.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenizer.ToSCU(tt.promptTok, tt.completeTok, tt.scuRate)
			assert.Equal(t, tt.wantSCU, got)
		})
	}
}
