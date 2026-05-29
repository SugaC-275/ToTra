package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// InsuranceDataType identifies a category of insurance-sensitive data.
type InsuranceDataType string

const (
	InsuranceClaim        InsuranceDataType = "claim"         // claim numbers/data
	InsurancePolicy       InsuranceDataType = "policy"        // policy numbers
	InsuranceActuarial    InsuranceDataType = "actuarial"     // mortality/loss tables
	InsuranceUnderwriting InsuranceDataType = "underwriting"  // risk assessment
	InsuranceFHA          InsuranceDataType = "fair_housing"  // property insurance discrimination
)

type insuranceRule struct {
	dataType InsuranceDataType
	re       *regexp.Regexp
}

var insurancePatterns = []*insuranceRule{
	// --- Claims data ---
	{InsuranceClaim, regexp.MustCompile(`(?i)\bclaim\s+(?:number|#|no\.?)[\s:]*[A-Z0-9\-]{3,20}`)},
	{InsuranceClaim, regexp.MustCompile(`(?i)\bloss\s+run\b`)},
	{InsuranceClaim, regexp.MustCompile(`(?i)\bclaim\s+reserve\b`)},
	{InsuranceClaim, regexp.MustCompile(`(?i)\bclaim\s+settlement\b`)},
	{InsuranceClaim, regexp.MustCompile(`(?i)\bclaimant\b`)},

	// --- Policy data ---
	{InsurancePolicy, regexp.MustCompile(`(?i)\bpolicy\s+(?:number|#|no\.?)[\s:]*[A-Z0-9\-]{3,20}`)},
	{InsurancePolicy, regexp.MustCompile(`(?i)\bpremium\b.*\$\s*[\d,]+`)},
	{InsurancePolicy, regexp.MustCompile(`(?i)\$\s*[\d,]+.*\bpremium\b`)},
	{InsurancePolicy, regexp.MustCompile(`(?i)\bcoverage\s+limit\b`)},
	{InsurancePolicy, regexp.MustCompile(`(?i)\bdeductible\b`)},

	// --- Actuarial data ---
	{InsuranceActuarial, regexp.MustCompile(`(?i)\bmortality\s+table\b`)},
	{InsuranceActuarial, regexp.MustCompile(`(?i)\bloss\s+ratio\b`)},
	{InsuranceActuarial, regexp.MustCompile(`(?i)\bexpense\s+ratio\b`)},
	{InsuranceActuarial, regexp.MustCompile(`(?i)\bactuarial\b`)},
	{InsuranceActuarial, regexp.MustCompile(`(?i)\breserve\s+calculation\b`)},
	{InsuranceActuarial, regexp.MustCompile(`\bIBNR\b`)},
	{InsuranceActuarial, regexp.MustCompile(`(?i)\bcombined\s+ratio\b`)},

	// --- Underwriting ---
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\bunderwriting\s+guidelines\b`)},
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\brisk\s+classification\b`)},
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\binsurance\s+score\b`)},
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\bpreferred\s+risk\b`)},
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\bstandard\s+risk\b`)},
	{InsuranceUnderwriting, regexp.MustCompile(`(?i)\bsubstandard\s+risk\b`)},

	// --- Fair Housing / redlining signals ---
	{InsuranceFHA, regexp.MustCompile(`(?i)\bredlining\b`)},
	{InsuranceFHA, regexp.MustCompile(`(?i)\bdenied.*(?:zip\s+code|neighborhood|demographic)\b`)},
	{InsuranceFHA, regexp.MustCompile(`(?i)\b(?:zip\s+code|neighborhood|demographic).*\bdenied\b`)},
	{InsuranceFHA, regexp.MustCompile(`(?i)\bdiscriminator[y]?\s+(?:pricing|rating|underwriting)\b`)},
}

// ScanForInsuranceData returns the distinct insurance data types detected in text.
// Returns an empty slice when no sensitive insurance content is found.
func ScanForInsuranceData(text string) []InsuranceDataType {
	seen := make(map[InsuranceDataType]struct{})
	lower := strings.ToLower(text)
	_ = lower // patterns use (?i); kept for potential future keyword checks

	for _, rule := range insurancePatterns {
		if rule.re.MatchString(text) {
			seen[rule.dataType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []InsuranceDataType{}
	}

	result := make([]InsuranceDataType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewInsuranceMiddleware returns a Fiber middleware that detects sensitive
// insurance data in request bodies. On detection it:
//   - Sets c.Locals("insurance_data_detected", true)
//   - Sets c.Locals("insurance_data_types", []InsuranceDataType{...})
//   - Adds the X-Insurance-Data-Signal: true response header
//   - Sends a non-blocking SIEMEvent with EventType "insurance_data_detected"
//
// The middleware does NOT block the request; enforcement is left to upstream policy.
func NewInsuranceMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForInsuranceData(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("insurance_data_detected", true)
		c.Locals("insurance_data_types", types)
		c.Set("X-Insurance-Data-Signal", "true")

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
				EventType: "insurance_data_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "insurance_data_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":          uid,
						"insurance_types":  typeStrs,
						"path":             c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return c.Next()
	}
}
