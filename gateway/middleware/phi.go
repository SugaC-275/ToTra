package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// PHIType identifies a HIPAA Safe Harbor PHI category.
type PHIType string

const (
	PHITypeMRN          PHIType = "medical_record_number"
	PHITypeHealthPlan   PHIType = "health_plan_id"
	PHITypeNPI          PHIType = "npi"
	PHITypeICD          PHIType = "icd_code"
	PHITypeDEA          PHIType = "dea_number"
	PHITypeClinicalDate PHIType = "clinical_date"
)

type phiRule struct {
	phiType PHIType
	re      *regexp.Regexp
}

// phiPatterns covers healthcare-specific PHI identifiers not already handled
// by the general piiPatterns in pii.go. The existing PII scanner already
// catches DOB (en_dob), MRN keyword form (en_medical_record), and ICD-10
// codes with the "ICD" keyword prefix (icd_code).
var phiPatterns = []*phiRule{
	// Medical Record Numbers: bare "MRN" prefix form not caught by en_medical_record.
	{PHITypeMRN, regexp.MustCompile(`(?i)\bMRN[\s:#]*\d{4,20}\b`)},
	// Medical Record keyword long-form (catches "Medical Record: 1234567").
	{PHITypeMRN, regexp.MustCompile(`(?i)Medical\s+Record[\s:#]+\d{4,20}`)},

	// Health Plan / Insurance IDs: common payer format 3 uppercase + 9 digits.
	{PHITypeHealthPlan, regexp.MustCompile(`\b[A-Z]{3}\d{9}\b`)},
	// Health plan with keyword.
	{PHITypeHealthPlan, regexp.MustCompile(`(?i)(?:health\s+plan|insurance|member\s+id|policy\s+(?:id|number))[\s:#]+[A-Z0-9\-]{6,20}`)},

	// NPI (National Provider Identifier): 10-digit number starting 1 or 2.
	{PHITypeNPI, regexp.MustCompile(`(?i)\bNPI[\s:#]*[12]\d{9}\b`)},

	// ICD-10/ICD-11 codes in medical context (without "ICD" keyword prefix —
	// plain clinical format: letter + 2 digits + optional decimal + digits).
	{PHITypeICD, regexp.MustCompile(`(?i)(?:diagnosis|dx|condition|icd)[\s:#]+[A-Z]\d{2}\.?\d{0,4}\b`)},

	// DEA Registration Numbers: 2 uppercase letters + 7 digits.
	// First letter encodes registrant type (A-F, M, P-T); second is first letter
	// of registrant's last name.
	{PHITypeDEA, regexp.MustCompile(`\b[A-F,M,P-T][A-Z]\d{7}\b`)},

	// Clinical dates: admission and discharge dates (DOB is already in pii.go).
	{PHITypeClinicalDate, regexp.MustCompile(`(?i)(?:Admission|Discharge|Admit\s+Date|Discharge\s+Date)[\s:#]+\d{1,2}[\/\-]\d{1,2}[\/\-]\d{2,4}`)},
}

// ScanForPHI checks text for HIPAA PHI identifiers beyond what ScanForPII covers.
// Returns detected PHI types (empty slice = clean).
func ScanForPHI(text string) []PHIType {
	var found []PHIType
	seen := make(map[PHIType]bool)
	lower := strings.ToLower(text)
	_ = lower // used implicitly via regexp (?i) flags

	for _, rule := range phiPatterns {
		if rule.re.MatchString(text) && !seen[rule.phiType] {
			found = append(found, rule.phiType)
			seen[rule.phiType] = true
		}
	}
	return found
}

// NewPHIMiddleware runs PHI detection pre-call and sets:
//
//	c.Locals("phi_detected", true) if PHI is found
//	c.Locals("phi_types", []PHIType{...})
//
// Sends a SIEM event for each detection. Does NOT block — governance routing
// (router.go / makeProxyHandler) decides what to do with the signal.
func NewPHIMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		detected := ScanForPHI(body)
		if len(detected) == 0 {
			return c.Next()
		}

		c.Locals("phi_detected", true)
		c.Locals("phi_types", detected)

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			typeStrs := make([]string, len(detected))
			for i, t := range detected {
				typeStrs[i] = string(t)
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "phi_detection",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "phi_detection",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":   uid,
						"phi_types": typeStrs,
						"path":      c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return c.Next()
	}
}
