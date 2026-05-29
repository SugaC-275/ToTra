package middleware

import (
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
)

// COPPASignal identifies a category of COPPA-relevant signal in request content.
type COPPASignal string

const (
	COPPAMinorContext   COPPASignal = "minor_context"    // text about children/minors
	COPPAMinorDataPoint COPPASignal = "minor_data_point" // data point for a child
	COPPAChildContent   COPPASignal = "child_content"    // age-inappropriate content request
)

type coppaRule struct {
	signal COPPASignal
	re     *regexp.Regexp
}

var coppaPatterns = []*coppaRule{
	// Age mentions for children under 13 (ages 1-12)
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)age[\s:]*(?:1[0-2]|[1-9])\b`),
	},
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)(?:1[0-2]|[1-9])\s+years?\s+old`),
	},
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)years?\s+old.*\b(?:1[0-2]|[1-9])\b`),
	},

	// Explicit minor labels
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)\b(?:minor|under\s+13|under\s+thirteen)\b`),
	},
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)\bchild(?:ren)?\b`),
	},
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)\bage\s+1[0-2]\b`),
	},
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)\b(?:kindergarten|1st\s+grade|2nd\s+grade|3rd\s+grade|4th\s+grade|5th\s+grade|6th\s+grade|K[-–]6|elementary\s+school\s+student)\b`),
	},

	// Parental consent context
	{
		signal: COPPAMinorContext,
		re:     regexp.MustCompile(`(?i)(?:parent(?:al)?\s+(?:consent|permission)|guardian(?:\'?s)?\s+consent)`),
	},

	// Data points specifically for a child
	{
		signal: COPPAMinorDataPoint,
		re:     regexp.MustCompile(`(?i)(?:child(?:ren)?(?:\'?s)?\s+(?:name|address|email|phone|location|data|information)|data\s+(?:for|about|of)\s+(?:a\s+)?(?:child|minor))`),
	},

	// Age-inappropriate content requests
	{
		signal: COPPAChildContent,
		re:     regexp.MustCompile(`(?i)(?:explicit|graphic)\s+(?:violence|content|material)`),
	},
	{
		signal: COPPAChildContent,
		re:     regexp.MustCompile(`(?i)adult\s+(?:content|material|themes?)`),
	},
}

// ScanForCOPPA returns the distinct COPPA signals detected in text.
// Returns an empty slice when no COPPA-relevant content is found.
func ScanForCOPPA(text string) []COPPASignal {
	seen := make(map[COPPASignal]struct{})

	for _, rule := range coppaPatterns {
		if rule.re.MatchString(text) {
			seen[rule.signal] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []COPPASignal{}
	}

	result := make([]COPPASignal, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	return result
}

// NewCOPPAMiddleware returns a Fiber middleware that detects COPPA-relevant signals
// in request bodies. On detection it:
//   - Sets c.Locals("coppa_signal", true)
//   - Sets c.Locals("coppa_types", []COPPASignal{...})
//   - Adds the X-COPPA-Signal: minor_context response header
//   - Sends a non-blocking SIEMEvent with EventType "coppa_signal"
//
// The middleware does NOT block requests. Whether to enforce COPPA restrictions
// is determined by the model's coppa_compliant flag in model_configs.
func NewCOPPAMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		signals := ScanForCOPPA(body)

		if len(signals) == 0 {
			return c.Next()
		}

		c.Locals("coppa_signal", true)
		c.Locals("coppa_types", signals)
		c.Set("X-COPPA-Signal", "minor_context")

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			sigStrs := make([]string, len(signals))
			for i, s := range signals {
				sigStrs[i] = string(s)
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "coppa_signal",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "coppa_signal",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":  uid,
						"signals":  sigStrs,
						"path":     c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
