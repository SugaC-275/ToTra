package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FERPAType identifies a category of protected student education record data.
type FERPAType string

const (
	FERPAStudentID    FERPAType = "student_id"    // student ID numbers
	FERPAGrades       FERPAType = "grades"         // grade information
	FERPADisciplinary FERPAType = "disciplinary"   // disciplinary records
	FERPAEnrollment   FERPAType = "enrollment"     // enrollment status
	FERPATranscript   FERPAType = "transcript"     // academic transcripts
	FERPAFinancialAid FERPAType = "financial_aid"  // financial aid records
)

type ferpaRule struct {
	ferpaType FERPAType
	re        *regexp.Regexp
}

var ferpaPatterns = []*ferpaRule{
	// Student identifiers
	{
		ferpaType: FERPAStudentID,
		re:        regexp.MustCompile(`(?i)(?:student\s+(?:ID|number)|SID[\s:]*[A-Z0-9]{6,12}|enrollment\s+ID|matriculation\s+number)`),
	},
	{
		ferpaType: FERPAStudentID,
		re:        regexp.MustCompile(`(?i)SID[\s:]*[A-Z0-9]{6,12}`),
	},

	// Enrollment status
	{
		ferpaType: FERPAEnrollment,
		re:        regexp.MustCompile(`(?i)(?:enrollment\s+(?:status|record)|enrolled\s+(?:student|in)|matriculat(?:ed|ion))`),
	},

	// Grade records: GPA patterns
	{
		ferpaType: FERPAGrades,
		re:        regexp.MustCompile(`(?i)GPA[\s:]*[0-4]\.[0-9]`),
	},
	{
		ferpaType: FERPAGrades,
		re:        regexp.MustCompile(`(?i)grade\s+point\s+average`),
	},
	{
		ferpaType: FERPAGrades,
		re:        regexp.MustCompile(`(?i)(?:grade|score)[\s:]*[ABCDF][+-]?(?:\s|$)`),
	},
	{
		ferpaType: FERPAGrades,
		re:        regexp.MustCompile(`\b[0-9]{1,3}%\s*(?:grade|score|mark)`),
	},

	// Transcripts / academic records
	{
		ferpaType: FERPATranscript,
		re:        regexp.MustCompile(`(?i)(?:transcript|academic\s+record|report\s+card)`),
	},

	// Disciplinary records
	{
		ferpaType: FERPADisciplinary,
		re:        regexp.MustCompile(`(?i)(?:disciplinary\s+action|academic\s+probation|expulsion)`),
	},
	{
		ferpaType: FERPADisciplinary,
		re:        regexp.MustCompile(`(?i)\bsuspension\b`),
	},

	// Financial aid
	{
		ferpaType: FERPAFinancialAid,
		re:        regexp.MustCompile(`(?i)(?:FAFSA|financial\s+aid\s+award|EFC|Expected\s+Family\s+Contribution|Pell\s+Grant)`),
	},
}

// ScanForFERPA returns the distinct FERPA data types detected in text.
// Returns an empty slice when no FERPA-protected content is found.
func ScanForFERPA(text string) []FERPAType {
	seen := make(map[FERPAType]struct{})
	lower := strings.ToLower(text)
	_ = lower // patterns use (?i) flag; kept for future keyword checks

	for _, rule := range ferpaPatterns {
		if rule.re.MatchString(text) {
			seen[rule.ferpaType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []FERPAType{}
	}

	result := make([]FERPAType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewFERPAMiddleware returns a Fiber middleware that detects FERPA-protected data
// in request bodies. On detection it:
//   - Sets c.Locals("ferpa_detected", true)
//   - Sets c.Locals("ferpa_types", []FERPAType{...})
//   - Adds the X-FERPA-Detected: true response header
//   - Sends a non-blocking SIEMEvent with EventType "ferpa_detected"
//
// The middleware does NOT block the request; enforcement is left to upstream policy.
func NewFERPAMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForFERPA(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("ferpa_detected", true)
		c.Locals("ferpa_types", types)
		c.Set("X-FERPA-Detected", "true")

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
				EventType: "ferpa_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "ferpa_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":     uid,
						"ferpa_types": typeStrs,
						"path":        c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
