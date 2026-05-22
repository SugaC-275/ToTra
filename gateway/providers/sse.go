package providers

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// readSSEChunks reads an SSE stream line by line and calls onChunk for each
// non-empty data line that is not the terminal "[DONE]" marker. It returns the
// first error returned by onChunk, or any scanner error.
func readSSEChunks(r io.Reader, onChunk func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// SSE lines that carry payload begin with "data: "
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			continue
		}
		if err := onChunk([]byte(line + "\n")); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// injectStreamTrue returns a copy of body with "stream" set to true.
// If the body is not valid JSON, it is returned unchanged so the upstream
// can respond with a natural error.
func injectStreamTrue(body []byte) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	m["stream"] = json.RawMessage("true")
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}
