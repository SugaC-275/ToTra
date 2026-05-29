package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CopyrightSignalType identifies a copyright or DMCA signal category
// regulated under 17 U.S.C.
type CopyrightSignalType string

const (
	CopyrightNotice  CopyrightSignalType = "copyright_notice"    // explicit © or (c) markers
	CopyrightLyrics  CopyrightSignalType = "lyrics_reproduction" // song lyrics being reproduced
	CopyrightExcerpt CopyrightSignalType = "book_excerpt"        // book/article reproduction
	CopyrightCode    CopyrightSignalType = "code_license"        // GPL/MIT/proprietary code
	DMCASignal       CopyrightSignalType = "dmca_signal"         // DMCA takedown context
	SyntheticMedia   CopyrightSignalType = "synthetic_media"     // AI likeness/deepfake request
)

type copyrightRule struct {
	signalType CopyrightSignalType
	re         *regexp.Regexp
}

var copyrightPatterns = []*copyrightRule{
	// --- Copyright notices ---
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`©`),
	},
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`(?i)\(c\)\s*\d{4}`),
	},
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`(?i)all\s+rights\s+reserved`),
	},
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`(?i)copyright\s+\d{4}`),
	},
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`®\s+[A-Z]`),
	},
	{
		signalType: CopyrightNotice,
		re:         regexp.MustCompile(`(?i)licensed\s+under\s+(?:MIT|GPL|Apache|CC|Creative\s+Commons|BSD|LGPL|MPL|AGPL)`),
	},

	// --- Lyrics reproduction ---
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)write\s+out\s+the\s+(?:full\s+)?lyrics`),
	},
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)full\s+lyrics\s+(?:of|to|for)`),
	},
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)lyrics\s+to\s+\S`),
	},
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)transcribe\s+this\s+song`),
	},
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)copy\s+these\s+lyrics`),
	},
	{
		signalType: CopyrightLyrics,
		re:         regexp.MustCompile(`(?i)reproduce\s+(?:the\s+)?(?:entire|whole|complete)\s+(?:song|track|lyrics)`),
	},

	// --- Book / article excerpts ---
	{
		signalType: CopyrightExcerpt,
		re:         regexp.MustCompile(`(?i)copy\s+this\s+chapter`),
	},
	{
		signalType: CopyrightExcerpt,
		re:         regexp.MustCompile(`(?i)reproduce\s+this\s+article`),
	},
	{
		signalType: CopyrightExcerpt,
		re:         regexp.MustCompile(`(?i)paste\s+the\s+full\s+text`),
	},
	// Long quoted block with attribution (quote > 500 chars surrounded by quotes)
	{
		signalType: CopyrightExcerpt,
		re:         regexp.MustCompile(`"[^"]{500,}"`),
	},

	// --- Code licensing ---
	{
		signalType: CopyrightCode,
		re:         regexp.MustCompile(`SPDX-License-Identifier:`),
	},
	{
		signalType: CopyrightCode,
		re:         regexp.MustCompile(`(?i)GNU\s+General\s+Public\s+License`),
	},
	{
		signalType: CopyrightCode,
		re:         regexp.MustCompile(`(?i)(?:CONFIDENTIAL).{0,80}(?:PROPRIETARY)|(?:PROPRIETARY).{0,80}(?:CONFIDENTIAL)`),
	},

	// --- DMCA signals ---
	{
		signalType: DMCASignal,
		re:         regexp.MustCompile(`(?i)takedown\s+notice`),
	},
	{
		signalType: DMCASignal,
		re:         regexp.MustCompile(`\bDMCA\b`),
	},
	{
		signalType: DMCASignal,
		re:         regexp.MustCompile(`(?i)copyright\s+infringement`),
	},
	{
		signalType: DMCASignal,
		re:         regexp.MustCompile(`(?i)cease\s+and\s+desist.{0,60}(?:content|material|work|song|image|video|audio|text)`),
	},
	{
		signalType: DMCASignal,
		re:         regexp.MustCompile(`(?i)fair\s+use`),
	},

	// --- Synthetic media / likeness ---
	{
		signalType: SyntheticMedia,
		re:         regexp.MustCompile(`(?i)voice\s+clone`),
	},
	{
		signalType: SyntheticMedia,
		re:         regexp.MustCompile(`(?i)sound\s+like\s+[A-Z][a-z]`),
	},
	{
		signalType: SyntheticMedia,
		re:         regexp.MustCompile(`(?i)deepfake`),
	},
	{
		signalType: SyntheticMedia,
		re:         regexp.MustCompile(`(?i)likeness\s+of\s+[A-Z][a-z]`),
	},
	// "generate image of [PersonName]" — requires a plausible proper noun after "of"
	{
		signalType: SyntheticMedia,
		re:         regexp.MustCompile(`(?i)generate\s+(?:an?\s+)?(?:image|photo|picture|video|audio)\s+of\s+[A-Z][a-z]`),
	},
}

// ScanForCopyrightSignals returns the distinct copyright signal types detected
// in text. Returns an empty slice when no signals are found.
func ScanForCopyrightSignals(text string) []CopyrightSignalType {
	seen := make(map[CopyrightSignalType]struct{})
	_ = strings.ToLower(text) // patterns use (?i); kept for future keyword expansion

	for _, rule := range copyrightPatterns {
		if rule.re.MatchString(text) {
			seen[rule.signalType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []CopyrightSignalType{}
	}

	result := make([]CopyrightSignalType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewMediaCopyrightMiddleware returns a Fiber middleware that detects copyright
// and DMCA signals in request bodies (17 U.S.C.). On detection it:
//   - Sets c.Locals("copyright_signal", true)
//   - Sets c.Locals("copyright_types", []CopyrightSignalType{...})
//   - Adds the X-Copyright-Signal: true response header
//   - For synthetic_media signals: also sets c.Locals("synthetic_media_request", true)
//   - Sends a non-blocking SIEMEvent with EventType "copyright_signal"
//
// The middleware does NOT block the request; legal teams review reproduction
// requests and AI can discuss copyright without reproducing protected works.
func NewMediaCopyrightMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForCopyrightSignals(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("copyright_signal", true)
		c.Locals("copyright_types", types)
		c.Set("X-Copyright-Signal", "true")

		// Flag synthetic media requests for special downstream handling
		for _, t := range types {
			if t == SyntheticMedia {
				c.Locals("synthetic_media_request", true)
				break
			}
		}

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			typeStrs := make([]string, len(types))
			for i, t := range types {
				typeStrs[i] = string(t)
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "copyright_signal",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "copyright_signal",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":          uid,
						"copyright_types":  typeStrs,
						"path":             c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
