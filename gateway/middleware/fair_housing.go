package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FairHousingViolationType identifies a category of Fair Housing Act violation.
type FairHousingViolationType string

const (
	FHVRace          FairHousingViolationType = "race_color"
	FHVFamilialStatus FairHousingViolationType = "familial_status"
	FHVDisability    FairHousingViolationType = "disability"
	FHVReligion      FairHousingViolationType = "religion"
	FHVSteering      FairHousingViolationType = "steering"
)

type fairHousingRule struct {
	violationType FairHousingViolationType
	re            *regexp.Regexp
}

var fairHousingPatterns = []*fairHousingRule{
	// --- Familial status violations ---
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\bperfect\s+for\s+singles?\b`),
	},
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\badult[\s-](?:only|community|living)\b`),
	},
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\bno\s+children\b`),
	},
	// "55+" is only lawful for qualifying senior housing under HOPA; flag as signal
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\b55\+\s+(?:community|living|housing|development|complex)\b`),
	},
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\bquiet\s+adults[\s-]only\b`),
	},
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\bideal\s+for\s+(?:couples?\s+without\s+kids?|childless\s+(?:couple|professional))\b`),
	},
	{
		violationType: FHVFamilialStatus,
		re:            regexp.MustCompile(`(?i)\bbachelor\s+pad\b`),
	},

	// --- Steering signals ---
	// Religious institution proximity as a demographic selling point
	{
		violationType: FHVSteering,
		re:            regexp.MustCompile(`(?i)\bnear\s+(?:a\s+)?(?:church|mosque|synagogue|temple|cathedral|chapel|parish|st\.\s+\w+|saint\s+\w+)\b`),
	},
	// Explicit racial/ethnic neighbourhood composition reference
	{
		violationType: FHVSteering,
		re:            regexp.MustCompile(`(?i)\bpredominantly\s+(?:white|black|hispanic|latino|asian|jewish|christian|muslim|arab)\b`),
	},
	// "good schools" paired with a zip code or neighbourhood is a steering signal
	{
		violationType: FHVSteering,
		re:            regexp.MustCompile(`(?i)\bgood\s+schools\b.*\b\d{5}\b|\b\d{5}\b.*\bgood\s+schools\b`),
	},
	// Implicit steering via "safe neighbourhood" as demographic code
	{
		violationType: FHVSteering,
		re:            regexp.MustCompile(`(?i)\bsafe\s+(?:neighborhood|neighbourhood|community|area)\b`),
	},

	// --- Disability violations ---
	{
		violationType: FHVDisability,
		re:            regexp.MustCompile(`(?i)\bno\s+(?:wheelchair|disability|disabled\s+person)\s+(?:access|accommodat(?:ion|ed))\b`),
	},
	{
		violationType: FHVDisability,
		re:            regexp.MustCompile(`(?i)\bnot\s+(?:wheelchair\s+)?accessible\b`),
	},
	// Refusal to discuss or dismissal of accessibility features
	{
		violationType: FHVDisability,
		re:            regexp.MustCompile(`(?i)\bno\s+accommodations?\s+(?:available|provided|offered|made)\b`),
	},

	// --- Race / color violations ---
	{
		violationType: FHVRace,
		re:            regexp.MustCompile(`(?i)\b(?:whites?\s+only|no\s+(?:minorities|blacks?|hispanics?|asians?|arabs?)|minority[\s-]free)\b`),
	},

	// --- Religion violations ---
	{
		violationType: FHVReligion,
		re:            regexp.MustCompile(`(?i)\b(?:christians?\s+only|muslims?\s+only|jews?\s+only|no\s+(?:muslims?|jews?|christians?|non[\s-]believers?))\b`),
	},
}

// ScanForFairHousingViolation returns the distinct Fair Housing Act violation
// types detected in text. Returns an empty slice when none are found.
func ScanForFairHousingViolation(text string) []FairHousingViolationType {
	lower := strings.ToLower(text)
	_ = lower // patterns use (?i); kept for future keyword-only checks

	seen := make(map[FairHousingViolationType]struct{})
	for _, rule := range fairHousingPatterns {
		if rule.re.MatchString(text) {
			seen[rule.violationType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []FairHousingViolationType{}
	}

	result := make([]FairHousingViolationType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewFairHousingMiddleware returns a Fiber middleware that detects Fair Housing
// Act violation signals in request bodies. On detection it:
//   - Sets c.Locals("fair_housing_violation", true)
//   - Sets c.Locals("fair_housing_types", []FairHousingViolationType{...})
//   - Adds the X-Fair-Housing-Signal response header listing violation types
//   - Sends a non-blocking SIEMEvent with EventType "fair_housing_violation"
//
// The middleware does NOT block requests; enforcement is advisory (human review).
func NewFairHousingMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForFairHousingViolation(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("fair_housing_violation", true)
		c.Locals("fair_housing_types", types)

		typeStrs := make([]string, len(types))
		for i, t := range types {
			typeStrs[i] = string(t)
		}
		c.Set("X-Fair-Housing-Signal", strings.Join(typeStrs, ","))

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "fair_housing_violation",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "fair_housing_violation",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":         uid,
						"violation_types": typeStrs,
						"path":            c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
