package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CUICategory represents a NARA CUI Registry category.
type CUICategory string

const (
	CUIPrivacy        CUICategory = "privacy"         // PII in government context
	CUILawEnforcement CUICategory = "law_enforcement" // law enforcement sensitive
	CUILegal          CUICategory = "legal"           // legal proceedings
	CUIExport         CUICategory = "export_control"  // ITAR/EAR controlled
	CUIInfrastructure CUICategory = "critical_infra"  // CIKR data
	CUIDefense        CUICategory = "defense"         // defense-related but not classified
	CUIFinancial      CUICategory = "financial_gov"   // government financial data
	CUIPatents        CUICategory = "patents"         // pre-patent info
)

type cuiRule struct {
	category CUICategory
	re       *regexp.Regexp
}

// cuiMarkerPatterns detect explicit CUI labels present in content.
var cuiMarkerPatterns = []*cuiRule{
	// FOUO / SBU / LES markers → privacy category (generic government PII context)
	{category: CUIPrivacy, re: regexp.MustCompile(`(?i)\bFOR\s+OFFICIAL\s+USE\s+ONLY\b`)},
	{category: CUIPrivacy, re: regexp.MustCompile(`(?i)\bFOUO\b`)},
	{category: CUIPrivacy, re: regexp.MustCompile(`(?i)\bSENSITIVE\s+BUT\s+UNCLASSIFIED\b`)},
	{category: CUIPrivacy, re: regexp.MustCompile(`(?i)\bSBU\b`)},
	{category: CUIPrivacy, re: regexp.MustCompile(`(?i)\bCONTROLLED\s+UNCLASSIFIED\b`)},
	// Bare "CUI" marker (word boundary, uppercase required to avoid common words)
	{category: CUIPrivacy, re: regexp.MustCompile(`\bCUI\b`)},

	// Law enforcement sensitive markers
	{category: CUILawEnforcement, re: regexp.MustCompile(`(?i)\bLAW\s+ENFORCEMENT\s+SENSITIVE\b`)},
	{category: CUILawEnforcement, re: regexp.MustCompile(`(?i)\bLES\b`)},
}

// exportControlPatterns detect ITAR/EAR references.
var exportControlPatterns = []*cuiRule{
	// ECCN code: digit + uppercase letter + two digits + lowercase letter (e.g., 5E002, 3A001)
	{category: CUIExport, re: regexp.MustCompile(`\b[0-9][A-Z][0-9]{2}[a-z]\b`)},
	// Regulatory references
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bITAR\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bEAR\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bexport\s+controlled\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\b22\s+CFR\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\b15\s+CFR\s+Part\s+7[0-9]{2}\b`)},
	// USML references
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bUSML\s+Category\s+[IVX]+\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bdefense\s+article\b`)},
	{category: CUIExport, re: regexp.MustCompile(`(?i)\bmunitions\s+list\b`)},
}

// criticalInfraPatterns detect CIKR content.
// "critical infrastructure" alone is checked, then SCADA/ICS standalone.
var criticalInfraPatterns = []*cuiRule{
	{category: CUIInfrastructure, re: regexp.MustCompile(`(?i)\bcritical\s+infrastructure\b`)},
	{category: CUIInfrastructure, re: regexp.MustCompile(`(?i)\bSCADA\b`)},
	{category: CUIInfrastructure, re: regexp.MustCompile(`(?i)\bICS\b`)},
	{category: CUIInfrastructure, re: regexp.MustCompile(`(?i)\bindustrial\s+control\s+system\b`)},
}

// infraSectorPatterns are secondary patterns — critical infrastructure is only
// confirmed when criticalInfraPatterns AND at least one sector keyword match.
var infraSectorKeywords = regexp.MustCompile(`(?i)\b(?:energy|water|grid|power\s+grid|electric\s+grid|water\s+treatment|pipeline)\b`)

// lawEnforcementPatterns detect LE-sensitive content beyond explicit markers.
var lawEnforcementPatterns = []*cuiRule{
	{category: CUILawEnforcement, re: regexp.MustCompile(`(?i)\bgrand\s+jury\b`)},
	{category: CUILawEnforcement, re: regexp.MustCompile(`(?i)\binformant\b`)},
	{category: CUILawEnforcement, re: regexp.MustCompile(`(?i)\bundercover\s+operation\b`)},
}

// classificationMap maps categories to their CUI portion marking suffixes.
// Order matters: most-specific suffix per category.
var classificationSuffix = map[CUICategory]string{
	CUIExport:         "CUI//SP-ITAR",
	CUILawEnforcement: "CUI//LES",
	CUIDefense:        "CUI//SP-DoD",
	CUIInfrastructure: "CUI//SP-CIKR",
	CUIPrivacy:        "CUI//SP-PRVCY",
	CUIFinancial:      "CUI//SP-FIN",
	CUILegal:          "CUI//SP-LEGAL",
	CUIPatents:        "CUI//SP-PATENT",
}

// suffixPriority determines which suffix wins when multiple categories match.
// Lower index = higher priority.
var suffixPriority = []CUICategory{
	CUIExport,
	CUILawEnforcement,
	CUIDefense,
	CUIInfrastructure,
	CUIPrivacy,
	CUIFinancial,
	CUILegal,
	CUIPatents,
}

// ScanForCUI returns detected CUI categories. Empty slice means clean.
func ScanForCUI(text string) []CUICategory {
	seen := make(map[CUICategory]bool)
	var result []CUICategory

	add := func(cat CUICategory) {
		if !seen[cat] {
			seen[cat] = true
			result = append(result, cat)
		}
	}

	upper := strings.ToUpper(text)
	_ = upper // used implicitly via regexp on original text

	// Check explicit CUI markers.
	for _, rule := range cuiMarkerPatterns {
		if rule.re.MatchString(text) {
			add(rule.category)
		}
	}

	// Check export control patterns.
	for _, rule := range exportControlPatterns {
		if rule.re.MatchString(text) {
			add(rule.category)
			break
		}
	}

	// Check critical infrastructure: SCADA/ICS are sufficient alone;
	// "critical infrastructure" requires a sector keyword to reduce false positives.
	scadaRe := regexp.MustCompile(`(?i)\b(?:SCADA|ICS|industrial\s+control\s+system)\b`)
	critInfraRe := regexp.MustCompile(`(?i)\bcritical\s+infrastructure\b`)
	if scadaRe.MatchString(text) || (critInfraRe.MatchString(text) && infraSectorKeywords.MatchString(text)) {
		add(CUIInfrastructure)
	}

	// Check law enforcement content patterns.
	for _, rule := range lawEnforcementPatterns {
		if rule.re.MatchString(text) {
			add(rule.category)
			break
		}
	}

	return result
}

// CUIClassification returns the highest-priority CUI portion marking for the
// given set of detected categories. Returns "CUI" when categories is non-empty
// but no specific suffix is mapped (should not occur in practice).
func CUIClassification(categories []CUICategory) string {
	if len(categories) == 0 {
		return ""
	}
	catSet := make(map[CUICategory]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}
	for _, cat := range suffixPriority {
		if catSet[cat] {
			if suffix, ok := classificationSuffix[cat]; ok {
				return suffix
			}
		}
	}
	return "CUI"
}

// NewGovernmentCUIMiddleware returns a Fiber middleware that scans the request
// body for CUI content. On detection it:
//   - Sets c.Locals("cui_detected", true)
//   - Sets c.Locals("cui_categories", []CUICategory{...})
//   - Sets c.Locals("cui_classification", <classification string>)
//   - Adds X-CUI-Detected: true response header
//   - Emits a SIEMEvent{EventType: "cui_detected"} on siemChan (non-blocking)
//
// The middleware does NOT block the request.
func NewGovernmentCUIMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		categories := ScanForCUI(body)

		if len(categories) > 0 {
			classification := CUIClassification(categories)

			c.Locals("cui_detected", true)
			c.Locals("cui_categories", categories)
			c.Locals("cui_classification", classification)
			c.Set("X-CUI-Detected", "true")

			if siemChan != nil {
				tid := ""
				uid := ""
				if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
					tid = user.TenantID
					uid = user.UserID
				}

				catStrings := make([]string, len(categories))
				for i, cat := range categories {
					catStrings[i] = string(cat)
				}

				select {
				case siemChan <- SIEMEvent{
					TenantID:  tid,
					EventType: "cui_detected",
					Payload: map[string]any{
						"source":         "totra",
						"tenant_id":      tid,
						"event_type":     "cui_detected",
						"occurred_at":    time.Now().UTC().Format(time.RFC3339),
						"classification": classification,
						"detail": map[string]any{
							"user_id":         uid,
							"cui_categories":  catStrings,
							"classification":  classification,
							"path":            c.Path(),
						},
					},
				}:
				default: // drop if channel is full
				}
			}
		}

		return c.Next()
	}
}
