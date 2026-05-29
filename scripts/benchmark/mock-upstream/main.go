// mock-upstream — lightweight HTTP server simulating an LLM provider.
//
// Returns a valid OpenAI-compatible chat completion response after a
// configurable delay. Use this to benchmark gateway overhead in isolation,
// without spending real LLM tokens or introducing network variance.
//
// Usage:
//   go run main.go --latency 100 --port 8080
//
// Flags:
//   --latency  Simulated upstream latency in milliseconds (default: 100)
//   --port     TCP port to listen on (default: 8080)
//   --jitter   Random jitter added to latency in ms (default: 0)
//              Useful for simulating realistic upstream variance.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Response types (OpenAI-compatible)
// ---------------------------------------------------------------------------

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	latency := flag.Int("latency", 100, "simulated upstream latency in ms")
	jitter   := flag.Int("jitter", 0, "random jitter added to latency in ms")
	port     := flag.String("port", "8080", "listen port")
	flag.Parse()

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Simulate LLM latency (+ optional jitter)
		delay := time.Duration(*latency) * time.Millisecond
		if *jitter > 0 {
			//nolint:gosec // benchmark tool, crypto rand not needed
			delay += time.Duration(rand.Intn(*jitter)) * time.Millisecond
		}
		time.Sleep(delay)

		resp := ChatCompletionResponse{
			ID:      "chatcmpl-mock-" + fmt.Sprintf("%d", time.Now().UnixNano()),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "gpt-4o-mini",
			Choices: []Choice{
				{
					Index:        0,
					Message:      Message{Role: "assistant", Content: "hello"},
					FinishReason: "stop",
				},
			},
			Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 1,
				TotalTokens:      11,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})

	// Health endpoint so the gateway (and CI) can wait for readiness
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	addr := ":" + *port
	fmt.Printf("mock-upstream listening on %s  (latency=%dms, jitter=%dms)\n",
		addr, *latency, *jitter)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
