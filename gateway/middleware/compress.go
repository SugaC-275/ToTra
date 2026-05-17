package middleware

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

var (
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
	reDashSep      = regexp.MustCompile(`-{4,}`)
	reEqualSep     = regexp.MustCompile(`={4,}`)
	reTrailingWS   = regexp.MustCompile(`[ \t]+\n`)
)

// CompressBody normalizes whitespace in the messages array of a JSON request
// body. Returns the (possibly rewritten) body, the original byte length, and
// the compressed byte length. If nothing can be compressed, the original body
// slice is returned and both sizes are equal.
func CompressBody(body []byte) (result []byte, originalLen, compressedLen int) {
	originalLen = len(body)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, originalLen, originalLen
	}
	msgsRaw, ok := raw["messages"]
	if !ok {
		return body, originalLen, originalLen
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(msgsRaw, &messages); err != nil {
		return body, originalLen, originalLen
	}

	changed := false
	for i, msgRaw := range messages {
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(msgRaw, &msg); err != nil {
			continue
		}
		// Only compress string content; skip array content (vision, tool results).
		var content string
		if err := json.Unmarshal(msg.Content, &content); err != nil {
			continue
		}

		compressed := compressText(content)
		if compressed == content {
			continue
		}

		var msgMap map[string]json.RawMessage
		if err := json.Unmarshal(msgRaw, &msgMap); err != nil {
			continue
		}
		newContent, err := json.Marshal(compressed)
		if err != nil {
			continue
		}
		msgMap["content"] = newContent
		newMsgRaw, err := json.Marshal(msgMap)
		if err != nil {
			continue
		}
		messages[i] = newMsgRaw
		changed = true
	}

	if !changed {
		return body, originalLen, originalLen
	}

	msgsJSON, err := json.Marshal(messages)
	if err != nil {
		return body, originalLen, originalLen
	}
	raw["messages"] = msgsJSON
	out, err := json.Marshal(raw)
	if err != nil {
		return body, originalLen, originalLen
	}
	return out, originalLen, len(out)
}

// compressText applies whitespace normalization rules to a single message
// content string. Operations are conservative: only trailing whitespace,
// excess blank lines, and long separator patterns are modified.
func compressText(s string) string {
	s = reTrailingWS.ReplaceAllString(s, "\n")
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	s = reDashSep.ReplaceAllString(s, "---")
	s = reEqualSep.ReplaceAllString(s, "===")
	return strings.TrimRight(s, " \t\n")
}

// NewCompressMiddleware returns a Fiber handler that compresses the request
// body's message contents before passing to the next handler. On successful
// compression it stores the byte savings in c.Locals("compression_saved_bytes")
// and sets the X-Compression-Saved-Bytes response header.
func NewCompressMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		if len(body) == 0 {
			return c.Next()
		}

		compressed, originalLen, compressedLen := CompressBody(body)
		saved := originalLen - compressedLen
		if saved > 0 {
			c.Request().SetBody(compressed)
			c.Locals("compression_original_bytes", originalLen)
			c.Locals("compression_saved_bytes", saved)
			c.Set("X-Compression-Saved-Bytes", strconv.Itoa(saved))
		}

		return c.Next()
	}
}
